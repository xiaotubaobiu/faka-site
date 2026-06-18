package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"faka-site/internal/auth"
	"faka-site/internal/epay"
	"faka-site/internal/store"
)

// newRechargeServer builds a Server backed by an in-memory store, seeded with
// one user and the recharge/epay config required by the flow.
func newRechargeServer(t *testing.T) (*Server, *store.User) {
	t.Helper()
	st, _ := store.OpenInMemory()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	uid, err := st.CreateUser("u@x.com", "$2a$10$hashplaceholder", "user")
	if err != nil {
		t.Fatal(err)
	}
	seed := map[string]string{
		"epay_merchants":        `[{"pid":1001,"key":"abc"}]`,
		"recharge_internal_pid": "1001",
		"recharge_notify_base":  "http://127.0.0.1:8102",
		"recharge_rate":         "500000",
	}
	for k, v := range seed {
		if err := st.SetConfig(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	u, err := st.UserByID(uid)
	if err != nil {
		t.Fatal(err)
	}
	return &Server{store: st, throttle: auth.NewThrottle(100), now: time.Now}, u
}

// createRechargeOrder drives POST /recharge to create an order for the user.
// Returns the created recharge order row.
func createRechargeOrder(t *testing.T, s *Server, u *store.User, amount string) *store.RechargeOrder {
	t.Helper()
	body := url.Values{"amount": {amount}, "method": {"alipay"}, "csrf": {"x"}}
	req := httptest.NewRequest("POST", "/recharge", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxSessionKey, &auth.Session{UserID: u.ID, Role: "user", CSRF: "x"}))
	rec := httptest.NewRecorder()
	s.postRecharge(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/recharge/pay/") {
		t.Fatalf("unexpected redirect %q", loc)
	}
	idStr := strings.TrimPrefix(loc, "/recharge/pay/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		t.Fatalf("bad id in redirect %q: %v", idStr, err)
	}
	ro, err := s.store.RechargeOrder(context.Background(), id)
	if err != nil {
		t.Fatalf("load recharge order: %v", err)
	}
	return ro
}

// signedNotify builds a signed /recharge/notify request just like the epay
// gateway would, using epay.Sign with the merchant key.
func signedNotify(outTradeNo, tradeNo, money string) *http.Request {
	v := url.Values{
		"pid":          {"1001"},
		"trade_no":     {tradeNo},
		"out_trade_no": {outTradeNo},
		"type":         {"alipay"},
		"name":         {"发卡站充值"},
		"money":        {money},
		"trade_status": {"TRADE_SUCCESS"},
		"param":        {""},
	}
	v.Set("sign", epay.Sign(v, "abc"))
	req := httptest.NewRequest("GET", "/recharge/notify?"+v.Encode(), nil)
	return req
}

func TestRechargeNotify_CreditsOnceAndIdempotent(t *testing.T) {
	s, u := newRechargeServer(t)

	before := u.Balance
	ro := createRechargeOrder(t, s, u, "10") // ¥10 → 1000 fen → 5,000,000 quota

	// First notify: should credit.
	req := signedNotify(ro.OutTradeNo, "EP111", "10.00")
	rec := httptest.NewRecorder()
	s.rechargeNotify(rec, req)
	if rec.Body.String() != "success" {
		t.Fatalf("notify body must be exactly 'success', got %q", rec.Body.String())
	}

	got, _ := s.store.UserByID(u.ID)
	want := before + ro.Quota
	if got.Balance != want {
		t.Fatalf("after first notify: balance=%d want=%d", got.Balance, want)
	}

	// Second notify (duplicate / replay): must NOT double-credit.
	req2 := signedNotify(ro.OutTradeNo, "EP111", "10.00")
	rec2 := httptest.NewRecorder()
	s.rechargeNotify(rec2, req2)
	if rec2.Body.String() != "success" {
		t.Fatalf("duplicate notify body must be 'success', got %q", rec2.Body.String())
	}
	got2, _ := s.store.UserByID(u.ID)
	if got2.Balance != want {
		t.Fatalf("after duplicate notify: balance=%d want=%d (double-credited!)", got2.Balance, want)
	}

	// Recharge order status should be paid.
	ro2, _ := s.store.RechargeOrder(context.Background(), ro.ID)
	if ro2.Status != "paid" {
		t.Fatalf("recharge order status=%q want paid", ro2.Status)
	}
}

func TestRechargeNotify_RejectsBadSignature(t *testing.T) {
	s, u := newRechargeServer(t)
	ro := createRechargeOrder(t, s, u, "10")

	v := url.Values{
		"pid":          {"1001"},
		"trade_no":     {"EP222"},
		"out_trade_no": {ro.OutTradeNo},
		"type":         {"alipay"},
		"name":         {"发卡站充值"},
		"money":        {"10.00"},
		"trade_status": {"TRADE_SUCCESS"},
		"param":        {""},
	}
	v.Set("sign", "deadbeef") // wrong signature
	req := httptest.NewRequest("GET", "/recharge/notify?"+v.Encode(), nil)
	rec := httptest.NewRecorder()
	s.rechargeNotify(rec, req)

	if rec.Body.String() != "success" {
		t.Fatalf("even rejected notify must reply 'success', got %q", rec.Body.String())
	}
	got, _ := s.store.UserByID(u.ID)
	if got.Balance != u.Balance {
		t.Fatalf("bad-signature notify must not credit: balance=%d want=%d", got.Balance, u.Balance)
	}
}
