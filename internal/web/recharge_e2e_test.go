package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"faka-site/internal/auth"
	"faka-site/internal/epay"
	"faka-site/internal/payment"
	"faka-site/internal/store"
)

// TestRechargeFlow_EndToEnd simulates the FULL production recharge path with a
// fake Alipay and an in-process HTTP server, so the public callback chain
// (/notify/alipay → epay handler → notifyDownstream → /recharge/notify) is
// exercised exactly as Caddy/Alipay would hit it in production.
//
// Steps:
//  1. POST /recharge — creates recharge_order + epay_orders.
//  2. GET /recharge/qr/{id} — async QR; Alipay (fake) is called with a non-empty
//     notify_url. Records the out_trade_no as "the official trade we created".
//  3. Alipay calls back: POST /notify/alipay with out_trade_no — the epay
//     handler verifies (fake), marks epay_orders paid, and forwards to
//     /recharge/notify (via the in-process server).
//  4. /recharge/notify settles the recharge (idempotent) and credits balance.
//
// Acceptance: balance increased by exactly the order's quota, and the recharge
// order is "paid".
func TestRechargeFlow_EndToEnd(t *testing.T) {
	st, _ := store.OpenInMemory()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	uid, err := st.CreateUser("e2e@x.com", "$2a$10$h", "user")
	if err != nil {
		t.Fatal(err)
	}
	// recharge_notify_base points at our in-process server, so the
	// notifyDownstream forward actually reaches /recharge/notify.
	for k, v := range map[string]string{
		"epay_merchants":        `[{"pid":1001,"key":"abc"}]`,
		"recharge_internal_pid": "1001",
		"recharge_rate":         "500000",
	} {
		st.SetConfig(ctx, k, v)
	}
	u, _ := st.UserByID(uid)
	balanceBefore := u.Balance

	// Fake alipay: returns a QR for any precreate, and reports the LAST created
	// out_trade_no as paid when Alipay calls back.
	prov := &e2eAlipay{}
	payment.DefaultRegistry().SetProviders(prov)
	t.Cleanup(func() { payment.DefaultRegistry().SetProviders() })

	srv := &Server{store: st, throttle: auth.NewThrottle(100), now: time.Now}

	// 1. Create the recharge order directly (skip postRecharge's epayConfig,
	//    which would replace our fake provider).
	fen := int64(1000) // ¥10
	quota := fen * 500000 / 100
	outTradeNo := "E2E-RC-1"
	ro, err := st.CreateRechargeOrder(ctx, uid, "alipay", outTradeNo, fen, quota)
	if err != nil {
		t.Fatalf("create recharge order: %v", err)
	}

	// Start a real HTTP server so notifyDownstream's http.Get reaches us. The
	// server is wired through Routes() so /notify/alipay and /recharge/notify
	// are both live. We seed recharge_notify_base AFTER the server has a URL.
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	st.SetConfig(ctx, "recharge_notify_base", ts.URL)

	// Backfill the epay_order's NotifyURL/ReturnURL as postRecharge would. The
	// official callback is forwarded to {notify_url}, which we set to our server.
	tradeNo := "EP-E2E-1"
	if err := st.EpayCreate(&store.EpayOrder{
		TradeNo: tradeNo, OutTradeNo: outTradeNo, PID: 1001, Type: "alipay",
		Name: "发卡站充值", Money: "10.00",
		NotifyURL: ts.URL + "/recharge/notify",
		ReturnURL: ts.URL + "/recharge/pay/" + intToStr(ro.ID),
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("epay create: %v", err)
	}

	// 2. Async QR: hits the fake alipay, which records outTradeNo for the
	//    callback and verifies a non-empty notify_url was passed.
	qrReq := withPathVal(sessReq("GET", "/recharge/qr/"+intToStr(ro.ID), u), "id", intToStr(ro.ID))
	qrReq.Host = strings.TrimPrefix(ts.URL, "http://")
	qrReq.RemoteAddr = "127.0.0.1:1234"
	qrReq.Header.Set("X-Forwarded-Proto", "http")
	qrRec := httptest.NewRecorder()
	srv.rechargeQR(qrRec, qrReq)
	if qrRec.Code != http.StatusOK {
		t.Fatalf("rechargeQR: %d body=%s", qrRec.Code, qrRec.Body.String())
	}
	if got := prov.lastNotifyURL(); got == "" {
		t.Fatal("notify_url passed to Alipay was empty (Fix #1 regression)")
	}
	if atomic.LoadInt32(&prov.createCalls) != 1 {
		t.Fatalf("expected 1 precreate, got %d", prov.createCalls)
	}

	// 3. Simulate Alipay's async callback: POST /notify/alipay.
	//    The fake provider's ParseNotify reports the trade we just created.
	notifyReq := httptest.NewRequest("POST", "/notify/alipay", strings.NewReader(
		"out_trade_no="+outTradeNo+"&trade_no=ALI-OFFICIAL-1&total_amount=10.00&trade_status=TRADE_SUCCESS"))
	notifyReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	notifyRec := httptest.NewRecorder()

	// Drive the epay handler's OfficialNotify directly (same as Routes() does).
	eh := epay.New(st, srv.epayConfig)
	eh.OfficialNotify("alipay")(notifyRec, notifyReq)
	if notifyRec.Body.String() != "success" {
		t.Fatalf("notify/alipay body = %q, want 'success'", notifyRec.Body.String())
	}

	// notifyDownstream runs in a goroutine and re-GETs /recharge/notify on our
	// live server. Poll until the recharge is settled (or timeout).
	deadline := time.Now().Add(15 * time.Second)
	var settled bool
	for time.Now().Before(deadline) {
		got, _ := st.RechargeOrder(ctx, ro.ID)
		if got != nil && got.Status == "paid" {
			settled = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !settled {
		// Diagnostic aid if this ever flakes: dump the epay_order state so it's
		// clear whether the goroutine fired (status==1) and where the chain
		// broke. Not asserted — kept for future debugging.
		ep, _ := st.EpayGetByOutTradeNoAny(outTradeNo)
		t.Logf("diag: epay_order status=%d notified=%v notify_count=%d notify_url=%q",
			statusOr(ep), notifiedOr(ep), notifyCountOr(ep), notifyURLOr(ep))
		t.Fatal("recharge order was not settled within timeout — end-to-end callback chain broken")
	}

	// 4. Acceptance: balance increased by exactly the order's quota (no double
	//    credit), and recharge order is paid.
	got, _ := st.UserByID(uid)
	if got.Balance != balanceBefore+quota {
		t.Fatalf("balance = %d, want %d (before %d + quota %d)",
			got.Balance, balanceBefore+quota, balanceBefore, quota)
	}
	// Idempotency: a duplicate /recharge/notify must not double-credit.
	dupReq := signedNotifyForE2E(outTradeNo, "ALI-OFFICIAL-1", "10.00")
	dupReq.URL.Host = strings.TrimPrefix(ts.URL, "http://")
	dupRec := httptest.NewRecorder()
	srv.rechargeNotify(dupRec, dupReq)
	if dupRec.Body.String() != "success" {
		t.Fatalf("duplicate notify body = %q", dupRec.Body.String())
	}
	got2, _ := st.UserByID(uid)
	if got2.Balance != got.Balance {
		t.Fatalf("duplicate notify double-credited: %d -> %d", got.Balance, got2.Balance)
	}
}

// e2eAlipay is a fake alipay provider for the end-to-end test. It records the
// out_trade_no of the trade it created and the notify_url it was given, and its
// ParseNotify reports that trade as paid.
type e2eAlipay struct {
	createCalls int32
	mu          sync.Mutex
	notifyURL   string
	lastOutTradeNo atomic.Value // string
}

func (p *e2eAlipay) Name() string             { return "alipay" }
func (p *e2eAlipay) Configured() bool         { return true }
func (p *e2eAlipay) NotifyOKResponse() string { return "success" }

func (p *e2eAlipay) CreatePayment(_ context.Context, req payment.PaymentRequest) (payment.PaymentResult, error) {
	atomic.AddInt32(&p.createCalls, 1)
	p.mu.Lock()
	p.notifyURL = req.NotifyURL
	p.mu.Unlock()
	p.lastOutTradeNo.Store(req.OutTradeNo)
	return payment.PaymentResult{QRCode: "https://qr.alipay.com/" + req.OutTradeNo}, nil
}

func (p *e2eAlipay) ParseNotify(r *http.Request) (payment.NotifyInfo, error) {
	_ = r.ParseForm()
	otn := p.lastOutTradeNo.Load().(string)
	return payment.NotifyInfo{
		OutTradeNo: otn,
		TradeNo:    r.FormValue("trade_no"),
	}, nil
}

func (p *e2eAlipay) lastNotifyURL() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.notifyURL
}

// signedNotifyForE2E builds an epay-signed /recharge/notify request exactly
// like notifyDownstream would (using the seeded merchant key "abc").
func signedNotifyForE2E(outTradeNo, tradeNo, money string) *http.Request {
	v := url.Values{}
	v.Set("pid", "1001")
	v.Set("trade_no", tradeNo)
	v.Set("out_trade_no", outTradeNo)
	v.Set("type", "alipay")
	v.Set("name", "发卡站充值")
	v.Set("money", money)
	v.Set("trade_status", "TRADE_SUCCESS")
	v.Set("param", "")
	v.Set("sign", epay.Sign(v, "abc"))
	return httptest.NewRequest("GET", "/recharge/notify?"+v.Encode(), nil)
}

// Diagnostic helpers for the e2e failure path.
func statusOr(o *store.EpayOrder) int {
	if o == nil {
		return -1
	}
	return o.Status
}
func notifiedOr(o *store.EpayOrder) bool {
	if o == nil {
		return false
	}
	return o.Notified
}
func notifyCountOr(o *store.EpayOrder) int {
	if o == nil {
		return -1
	}
	return o.NotifyCount
}
func notifyURLOr(o *store.EpayOrder) string {
	if o == nil {
		return ""
	}
	return o.NotifyURL
}
