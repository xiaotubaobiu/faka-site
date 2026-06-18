package epay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"faka-site/internal/payment"
	"faka-site/internal/store"
)

// notifyTestProvider simulates an official payment provider: it records the
// out_trade_no it was asked to create a payment for, and its ParseNotify
// returns that trade as paid so OfficialNotify can settle it.
type notifyTestProvider struct {
	outTradeNo string
	tradeNo    string
	amountFen  int64
}

func (n *notifyTestProvider) Name() string             { return "alipay" }
func (n *notifyTestProvider) Configured() bool         { return true }
func (n *notifyTestProvider) NotifyOKResponse() string { return "success" }
func (n *notifyTestProvider) CreatePayment(_ context.Context, req payment.PaymentRequest) (payment.PaymentResult, error) {
	n.outTradeNo = req.OutTradeNo
	n.amountFen = req.AmountFen
	return payment.PaymentResult{QRCode: "https://qr.alipay.com/" + req.OutTradeNo}, nil
}
func (n *notifyTestProvider) ParseNotify(r *http.Request) (payment.NotifyInfo, error) {
	return payment.NotifyInfo{
		OutTradeNo: n.outTradeNo,
		TradeNo:    "OFFICIAL-TX-123",
		AmountFen:  n.amountFen,
	}, nil
}

// TestOfficialNotify_SettlesOrder drives the full official-payment callback
// path: a merchant order exists → official callback arrives → provider verifies
// it → epay order is marked paid. Downstream notification is fire-and-forget
// (goroutine) so we only assert the order status here.
func TestOfficialNotify_SettlesOrder(t *testing.T) {
	s, _ := store.OpenInMemory()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	prov := &notifyTestProvider{tradeNo: "OFFICIAL-TX-123"}
	payment.DefaultRegistry().SetProviders(prov)
	t.Cleanup(func() { payment.DefaultRegistry().SetProviders() })

	// Seed a merchant + an unpaid epay order (as if a merchant placed it).
	if err := s.SetConfig(context.Background(), "epay_merchants", `[{"pid":1001,"key":"k"}]`); err != nil {
		t.Fatal(err)
	}
	otn := "ORDER-1"
	order := &store.EpayOrder{
		TradeNo:    "EP1001",
		OutTradeNo: otn,
		PID:        1001,
		Type:       "alipay",
		Name:       "test",
		Money:      "1.00",
		NotifyURL:  "http://127.0.0.1:0/never-called",
		CreatedAt:  time.Now(),
	}
	if err := s.EpayCreate(order); err != nil {
		t.Fatal(err)
	}
	// Simulate the provider having been asked to pay this order (so ParseNotify
	// knows which out_trade_no to report).
	prov.outTradeNo = otn
	prov.amountFen = 100

	h := New(s, func() Config { return Config{NotifyBase: "https://gw.example", OrderTimeout: 5} })

	req := httptest.NewRequest(http.MethodPost, "/notify/alipay", nil)
	rec := httptest.NewRecorder()
	h.OfficialNotify("alipay")(rec, req)

	if rec.Body.String() != "success" {
		t.Fatalf("notify OK body = %q, want 'success'", rec.Body.String())
	}

	got, err := s.EpayGetByOutTradeNoAny(otn)
	if err != nil || got == nil {
		t.Fatalf("order lookup: %v", err)
	}
	if got.Status != 1 {
		t.Fatalf("order status = %d, want 1 (paid)", got.Status)
	}
	if got.AlipayTradeNo != "OFFICIAL-TX-123" {
		t.Fatalf("alipay_trade_no = %q, want OFFICIAL-TX-123", got.AlipayTradeNo)
	}
}

// TestOfficialNotify_UnconfiguredChannel is a no-op (200, no settle) when the
// channel has no provider configured, so misrouted callbacks don't crash.
func TestOfficialNotify_UnconfiguredChannel(t *testing.T) {
	s, _ := store.OpenInMemory()
	s.Migrate()
	defer s.Close()
	payment.DefaultRegistry().SetProviders() // nothing configured
	t.Cleanup(func() { payment.DefaultRegistry().SetProviders() })

	h := New(s, func() Config { return Config{NotifyBase: "https://gw.example"} })
	req := httptest.NewRequest(http.MethodPost, "/notify/wxpay", nil)
	rec := httptest.NewRecorder()
	h.OfficialNotify("wxpay")(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
