package web

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math"
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
	base := cfg.RechargeNotifyBase
	if base == "" {
		base = "http://" + r.Host
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

	// Look up the gateway-side epay order that was created for this recharge.
	// Its out_trade_no equals the recharge's out_trade_no.
	epayOrder, _ := s.store.EpayGetByOutTradeNoAny(recharge.OutTradeNo)

	// Resolve the official provider for this channel and (lazily) obtain the
	// real dynamic QR code. Once the order is paid we don't need the QR.
	qr := ""
	var payErr string
	if recharge.Status != "paid" {
		provider := payment.DefaultRegistry().MustGet(recharge.Provider)
		if !provider.Configured() {
			payErr = "支付渠道(" + methodLabel(recharge.Provider) + ")未配置,请联系管理员"
		} else {
			res, perr := provider.CreatePayment(ctx, payment.PaymentRequest{
				OutTradeNo: recharge.OutTradeNo,
				AmountFen:  recharge.AmountFen,
				Subject:    "发卡站充值",
				NotifyURL:  s.officialNotifyURL(recharge.Provider),
			})
			if perr != nil {
				log.Printf("recharge/pay: create payment otn=%s: %v", recharge.OutTradeNo, perr)
				payErr = "下单失败,请稍后重试"
			} else {
				qr = res.QRCode
			}
		}
	}

	u, _ := s.store.UserByID(uid)
	var balance int64
	if u != nil {
		balance = u.Balance
	}
	data := map[string]any{
		"order":     recharge,
		"qr":        qr,
		"payErr":    payErr,
		"method":    methodLabel(recharge.Provider),
		"balance":   balance,
		"epayOrder": epayOrder,
	}
	// HTMX poll asks for just the status fragment; returning the full page here
	// would nest the whole layout inside the polled <div> ("page tearing").
	if r.Header.Get("HX-Request") == "true" {
		s.renderBlock(w, "recharge_pay.html", "pay_status", ViewData{Data: data})
		return
	}
	s.render(w, r, "recharge_pay.html", ViewData{Title: "充值", Data: data})
}

// officialNotifyURL returns the public callback URL for a payment channel,
// used as the notify_url passed to the official payment API.
func (s *Server) officialNotifyURL(channel string) string {
	cfg := s.mustConfig()
	if cfg.RechargeNotifyBase != "" {
		return cfg.RechargeNotifyBase + "/notify/" + channel
	}
	return ""
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
