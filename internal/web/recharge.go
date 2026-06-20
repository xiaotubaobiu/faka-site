package web

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"

	"faka-site/internal/epay"
	"faka-site/internal/payment"
	"faka-site/internal/store"
)

// supportedRechargeMethods lists the payable channels (must match epay QR keys).
var supportedRechargeMethods = map[string]bool{"alipay": true, "wxpay": true}

// randHex returns n bytes of crypto-random hex.
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// extremely unlikely; fall back to a time-based value so the trade-no
		// is still unique enough within the process.
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

// yuanToFen parses a CNY amount string (yuan) into integer fen.
func yuanToFen(s string) (int64, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("invalid amount")
	}
	return int64(math.Round(f * 100)), nil
}

// fenToYuan formats fen as a 2-decimal yuan string.
func fenToYuan(fen int64) string {
	return strconv.FormatFloat(float64(fen)/100, 'f', 2, 64)
}

// methodLabel returns a friendly channel name for display.
func methodLabel(method string) string {
	switch method {
	case "alipay":
		return "支付宝"
	case "wxpay":
		return "微信支付"
	default:
		return method
	}
}

func (s *Server) getRecharge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := currentUser(r).UserID
	cfg := s.mustConfig()
	rate, _ := strconv.ParseInt(cfg.RechargeRate, 10, 64)
	if rate <= 0 {
		rate = 500000
	}
	orders, _ := s.store.RechargeOrdersByUser(ctx, uid)
	s.render(w, r, "recharge.html", ViewData{
		Title: "充值",
		Data: map[string]any{
			"rate":   rate,
			"orders": orders,
		},
	})
}

func (s *Server) postRecharge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := currentUser(r).UserID
	cfg := s.mustConfig()

	method := r.PostFormValue("method")
	if !supportedRechargeMethods[method] {
		s.render(w, r, "recharge.html", ViewData{Title: "充值", Data: map[string]any{"error": "请选择支付方式"}})
		return
	}
	fen, err := yuanToFen(r.PostFormValue("amount"))
	if err != nil || fen <= 0 {
		s.render(w, r, "recharge.html", ViewData{Title: "充值", Data: map[string]any{"error": "请输入正确的金额"}})
		return
	}
	rate, _ := strconv.ParseInt(cfg.RechargeRate, 10, 64)
	if rate <= 0 {
		rate = 500000
	}
	quota := fen * rate / 100

	now := s.now().Unix()
	outTradeNo := fmt.Sprintf("RC%d%d%s", uid, now, randHex(4))
	recharge, err := s.store.CreateRechargeOrder(ctx, uid, method, outTradeNo, fen, quota)
	if err != nil {
		log.Printf("recharge: create order failed uid=%d: %v", uid, err)
		s.render(w, r, "recharge.html", ViewData{Title: "充值", Data: map[string]any{"error": "创建订单失败,请稍后重试"}})
		return
	}

	// Create the corresponding epay_order (this gateway's own order record).
	ec := s.epayConfig()
	pid, _ := strconv.Atoi(cfg.RechargeInternalPID)
	if pid == 0 {
		pid = ec.PrimaryMerchant().PID
	}
	// The official payment's async callback lands at /notify/<channel> and is
	// forwarded back to /recharge/notify by the gateway. The base used here is
	// the configured public one (required in production) so the callback can
	// actually reach us through Caddy.
	base := cfg.RechargeNotifyBase
	if base == "" {
		base = schemeFromRequest(r) + "://" + r.Host
	}
	tradeNo := fmt.Sprintf("EP%d%03d", time.Now().UnixNano()/1e6, uid%1000)
	epayOrder := &store.EpayOrder{
		TradeNo:    tradeNo,
		OutTradeNo: outTradeNo,
		PID:        pid,
		Type:       method,
		Name:       "发卡站充值",
		Money:      fenToYuan(fen),
		NotifyURL:  base + "/recharge/notify",
		ReturnURL:  base + "/recharge/pay/" + strconv.FormatInt(recharge.ID, 10),
		CreatedAt:  time.Now(),
	}
	if err := s.store.EpayCreate(epayOrder); err != nil {
		log.Printf("recharge: create epay order failed uid=%d otn=%s: %v", uid, outTradeNo, err)
	}
	http.Redirect(w, r, "/recharge/pay/"+strconv.FormatInt(recharge.ID, 10), http.StatusSeeOther)
}

func (s *Server) rechargePay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := currentUser(r).UserID
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	recharge, err := s.store.RechargeOrder(ctx, id)
	if err != nil || recharge == nil || recharge.UserID != uid {
		http.NotFound(w, r)
		return
	}

	isHX := r.Header.Get("HX-Request") == "true"

	// HTMX poll: only report payment status — never regenerate the QR. While
	// unpaid we return 204 so htmx skips the swap and the existing QR stays put
	// (no more flicker / "跳来跳去"), and we stop hitting Alipay every 5s. The
	// QR is generated once via the dedicated async endpoint /recharge/qr/{id}.
	// Once paid we render the success fragment which (having no hx-get) also
	// stops the polling.
	if isHX {
		if recharge.Status != "paid" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		u, _ := s.store.UserByID(uid)
		var balance int64
		if u != nil {
			balance = u.Balance
		}
		s.renderBlock(w, "recharge_pay.html", "pay_status", ViewData{Data: map[string]any{
			"order":   recharge,
			"method":  methodLabel(recharge.Provider),
			"balance": balance,
		}})
		return
	}

	// Full page load: render the skeleton immediately with an hx-get pointing at
	// the async QR endpoint. The slow Alipay precreate (up to ~1s + network) now
	// happens OUT of the critical page-render path, so the user sees the card
	// and a "正在生成二维码…" placeholder within milliseconds, and the real QR
	// swaps in as soon as precreate returns. Paid orders skip the QR entirely.
	u, _ := s.store.UserByID(uid)
	var balance int64
	if u != nil {
		balance = u.Balance
	}
	s.render(w, r, "recharge_pay.html", ViewData{
		Title: "充值",
		Data: map[string]any{
			"order":   recharge,
			"method":  methodLabel(recharge.Provider),
			"balance": balance,
		},
	})
}

// rechargeQR is the async QR endpoint. The skeleton pay page polls it once
// (hx-trigger="load") right after the page renders, so the heavy Alipay
// precreate runs here — outside the initial page load — and the resulting QR
// fragment swaps into the placeholder. Returns:
//   - the QR/payErr fragment on first call for an unpaid order
//   - a redirect to the pay page if the order is already paid (so the polling
//     status logic takes over), and 204 on subsequent calls once a QR exists.
//
// The QR is generated at most once per order: the official trade is created
// against recharge.OutTradeNo, and Alipay treats a repeated out_trade_no for an
// active trade as idempotent, so even a reload is safe.
func (s *Server) rechargeQR(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := currentUser(r).UserID
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	recharge, err := s.store.RechargeOrder(ctx, id)
	if err != nil || recharge == nil || recharge.UserID != uid {
		http.NotFound(w, r)
		return
	}

	// Already paid: nudge the page back to the status poll path.
	if recharge.Status == "paid" {
		w.Header().Set("HX-Redirect", "/recharge/pay/"+strconv.FormatInt(id, 10))
		w.WriteHeader(http.StatusNoContent)
		return
	}

	qr := ""
	var payErr string
	provider := payment.DefaultRegistry().MustGet(recharge.Provider)
	if !provider.Configured() {
		payErr = "支付渠道(" + methodLabel(recharge.Provider) + ")未配置,请联系管理员"
	} else {
		res, perr := provider.CreatePayment(ctx, payment.PaymentRequest{
			OutTradeNo:     recharge.OutTradeNo,
			AmountFen:      recharge.AmountFen,
			Subject:        "发卡站充值",
			NotifyURL:      s.officialNotifyURLForRequest(recharge.Provider, r),
			TimeoutExpress: s.precreateTimeout(),
		})
		if perr != nil {
			log.Printf("recharge/qr: create payment otn=%s: %v", recharge.OutTradeNo, perr)
			payErr = "下单失败,请稍后重试"
		} else {
			qr = res.QRCode
		}
	}

	s.renderBlock(w, "recharge_pay.html", "qr", ViewData{Data: map[string]any{
		"order":  recharge,
		"qr":     qr,
		"payErr": payErr,
	}})
}

// precreateTimeout returns the configured epay order timeout (minutes) as an
// Alipay timeout_express string, so an unpaid trade auto-closes at Alipay's
// side in lockstep with our own order sweeper.
func (s *Server) precreateTimeout() string {
	cfg := s.mustConfig()
	n, _ := strconv.Atoi(cfg.EpayOrderTimeout)
	if n <= 0 {
		n = 5
	}
	return fmt.Sprintf("%dm", n)
}

// officialNotifyURL returns the public callback URL for a payment channel,
// used as the notify_url passed to the official payment API when no request is
// in scope (e.g. tests). Returns "" when no base is configured.
func (s *Server) officialNotifyURL(channel string) string {
	cfg := s.mustConfig()
	if cfg.RechargeNotifyBase != "" {
		return cfg.RechargeNotifyBase + "/notify/" + channel
	}
	return ""
}

// officialNotifyURLForRequest returns the official callback URL, falling back
// to the request's own scheme://host when RechargeNotifyBase is unset. This is
// the per-request variant used by the live recharge flow: in production the
// admin MUST set RechargeNotifyBase (validated on save), but the fallback keeps
// local/sandbox testing working without a public URL and guarantees Alipay is
// never handed an empty notify_url (which was the root cause of "paid but not
// credited" — Alipay simply had nowhere to call back).
func (s *Server) officialNotifyURLForRequest(channel string, r *http.Request) string {
	cfg := s.mustConfig()
	if cfg.RechargeNotifyBase != "" {
		return cfg.RechargeNotifyBase + "/notify/" + channel
	}
	return schemeFromRequest(r) + "://" + r.Host + "/notify/" + channel
}

// schemeFromRequest reports the request scheme, trusting X-Forwarded-Proto
// when behind the Caddy reverse proxy (loopback) and falling back to TLS state.
func schemeFromRequest(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if xfp := r.Header.Get("X-Forwarded-Proto"); xfp != "" {
		if isLoopback(remoteHost(r)) {
			return xfp
		}
	}
	return "http"
}

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rechargeNotify is the PUBLIC callback endpoint the epay gateway calls.
// It verifies the signed GET query, settles the recharge (idempotent), and
// always replies with the literal body "success" as the gateway expects.
func (s *Server) rechargeNotify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	pid, _ := strconv.Atoi(q.Get("pid"))
	outTradeNo := q.Get("out_trade_no")
	tradeNo := q.Get("trade_no")
	ec := s.epayConfig()
	key := ec.FindMerchant(pid)

	ok := key != "" &&
		epay.Sign(q, key) == q.Get("sign") &&
		q.Get("trade_status") == "TRADE_SUCCESS" &&
		outTradeNo != ""

	if !ok {
		log.Printf("recharge/notify: verify failed pid=%d otn=%s", pid, outTradeNo)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
		return
	}

	credited, err := s.store.SettleRecharge(ctx, outTradeNo, tradeNo)
	if err != nil {
		log.Printf("recharge/notify: settle error otn=%s: %v", outTradeNo, err)
	} else {
		log.Printf("recharge/notify: settled otn=%s credited=%v", outTradeNo, credited)
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("success"))
}
