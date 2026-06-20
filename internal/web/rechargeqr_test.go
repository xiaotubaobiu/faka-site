package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"faka-site/internal/auth"
	"faka-site/internal/payment"
	"faka-site/internal/store"
)

// fakeAlipayProvider is a controllable alipay provider for end-to-end tests.
// It records every CreatePayment call (incl. the NotifyURL it was asked to use)
// and lets the test trigger the official callback path via fireCallback.
type fakeAlipayProvider struct {
	mu          sync.Mutex
	lastReq     payment.PaymentRequest
	createCalls int32
	delay       time.Duration // simulated precreate latency
	failCreate  bool
	qrPrefix    string
}

func (f *fakeAlipayProvider) Name() string             { return "alipay" }
func (f *fakeAlipayProvider) Configured() bool         { return true }
func (f *fakeAlipayProvider) NotifyOKResponse() string { return "success" }

func (f *fakeAlipayProvider) CreatePayment(_ context.Context, req payment.PaymentRequest) (payment.PaymentResult, error) {
	atomic.AddInt32(&f.createCalls, 1)
	f.mu.Lock()
	f.lastReq = req
	f.mu.Unlock()
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.failCreate {
		return payment.PaymentResult{}, errString("simulated precreate failure")
	}
	qr := req.OutTradeNo
	if f.qrPrefix != "" {
		qr = f.qrPrefix + req.OutTradeNo
	}
	return payment.PaymentResult{QRCode: "https://qr.alipay.com/" + qr}, nil
}

func (f *fakeAlipayProvider) ParseNotify(r *http.Request) (payment.NotifyInfo, error) {
	_ = r.ParseForm()
	return payment.NotifyInfo{
		OutTradeNo: r.FormValue("out_trade_no"),
		TradeNo:    r.FormValue("trade_no"),
	}, nil
}

func (f *fakeAlipayProvider) lastNotifyURL() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastReq.NotifyURL
}

// errString local to this test file (mirrors the package-level type but kept
// here so we don't depend on private names exported elsewhere).
type errString string

func (e errString) Error() string { return string(e) }

// newRechargeQRSpec builds a server + fake alipay provider, registers it in the
// process-wide registry, and seeds the recharge config (without notify_base, so
// the Host fallback path is exercised). Returns everything the test needs.
func newRechargeQRSpec(t *testing.T) (*Server, *store.User, *fakeAlipayProvider) {
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
		// NOTE: recharge_notify_base deliberately absent → exercises Fix #1
		// fallback in rechargeQR.
		"recharge_rate": "500000",
	}
	for k, v := range seed {
		if err := st.SetConfig(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	u, _ := st.UserByID(uid)

	prov := &fakeAlipayProvider{qrPrefix: "b28_"}
	// Replace the registry with ONLY our fake alipay so we control behaviour.
	payment.DefaultRegistry().SetProviders(prov)
	t.Cleanup(func() { payment.DefaultRegistry().SetProviders() })

	srv := &Server{store: st, throttle: auth.NewThrottle(100), now: time.Now}
	return srv, u, prov
}

// createRechargeOrderDirect inserts a pending recharge order without going
// through postRecharge (which calls epayConfig and would overwrite our fake
// provider with a real, unconfigured AlipayProvider built from the empty test
// key directory). The order is otherwise identical to what postRecharge would
// create, so handlers downstream behave the same.
func createRechargeOrderDirect(t *testing.T, s *Server, u *store.User, amount string) *store.RechargeOrder {
	t.Helper()
	fen, err := yuanToFen(amount)
	if err != nil {
		t.Fatalf("bad amount %q: %v", amount, err)
	}
	cfg := s.mustConfig()
	rate, _ := parseRate(cfg.RechargeRate)
	quota := fen * rate / 100
	now := s.now().Unix()
	outTradeNo := "RC" + intToStr(u.ID) + intToStr(now) + "test"
	ro, err := s.store.CreateRechargeOrder(context.Background(), u.ID, "alipay", outTradeNo, fen, quota)
	if err != nil {
		t.Fatalf("create recharge order: %v", err)
	}
	return ro
}

func parseRate(s string) (int64, error) {
	if s == "" {
		return 500000, nil
	}
	var n int64
	var sign int64 = 1
	i := 0
	if i < len(s) && s[i] == '-' {
		sign = -1
		i++
	}
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n = n*10 + int64(s[i]-'0')
		i++
	}
	if n == 0 {
		return 500000, nil
	}
	return n * sign, nil
}

// sessReq builds a request pre-wired with the user's session (mimics
// loadSession middleware for direct handler invocation). For handlers that
// read path values, pass a non-empty pathVal to set via r.SetPathValue.
func sessReq(method, target string, u *store.User) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxSessionKey, &auth.Session{UserID: u.ID, Role: "user", CSRF: "x"}))
	return req
}

// withPathVal sets a path parameter on req (the router normally does this).
func withPathVal(req *http.Request, key, val string) *http.Request {
	req.SetPathValue(key, val)
	return req
}

// --- Fix #3: async QR endpoint + skeleton ---

// TestRechargeQR_RendersSkeletonFast verifies the page-load path no longer
// blocks on the (slow) Alipay precreate: rechargePay returns the skeleton
// immediately and contains an hx-get to /recharge/qr/{id}, regardless of how
// slow precreate would be.
func TestRechargeQR_RendersSkeletonFast(t *testing.T) {
	s, u, prov := newRechargeQRSpec(t)
	prov.delay = 3 * time.Second // simulate a sluggish precreate

	ro := createRechargeOrderDirect(t, s, u, "10")
	req := withPathVal(sessReq("GET", "/recharge/pay/"+itoa(ro.ID), u), "id", itoa(ro.ID))
	rec := httptest.NewRecorder()
	start := time.Now()
	s.rechargePay(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// The page MUST render well under the simulated 2s precreate latency,
	// proving the QR is no longer in the critical render path.
	if elapsed > 1*time.Second {
		t.Fatalf("skeleton page took %v (precreate leaked into render path)", elapsed)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "正在生成二维码") {
		t.Errorf("skeleton placeholder missing in body")
	}
	if !strings.Contains(body, "/recharge/qr/"+itoa(ro.ID)) {
		t.Errorf("async hx-get to /recharge/qr/{id} missing in body")
	}
	// And precreate must NOT have been called during page render.
	if atomic.LoadInt32(&prov.createCalls) != 0 {
		t.Errorf("precreate must not run during page render; got %d calls", prov.createCalls)
	}
}

// TestRechargeQR_AsyncEndpointReturnsQR verifies the async endpoint actually
// calls precreate and swaps in the QR fragment, and that the notify_url it
// passed to Alipay is non-empty (the Fix #1 regression guard).
func TestRechargeQR_AsyncEndpointReturnsQR(t *testing.T) {
	s, u, prov := newRechargeQRSpec(t)
	ro := createRechargeOrderDirect(t, s, u, "10")

	req := withPathVal(sessReq("GET", "/recharge/qr/"+itoa(ro.ID), u), "id", itoa(ro.ID))
	req.Host = "faka.public.example.com"
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	s.rechargeQR(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if atomic.LoadInt32(&prov.createCalls) != 1 {
		t.Fatalf("expected exactly 1 precreate call, got %d", prov.createCalls)
	}
	// notify_url must be non-empty (root cause of "paid but not credited").
	if got := prov.lastNotifyURL(); got == "" {
		t.Fatal("notify_url passed to Alipay was empty")
	} else if !strings.HasPrefix(got, "https://faka.public.example.com/notify/alipay") {
		t.Fatalf("notify_url fallback wrong: %q", got)
	}
	body := rec.Body.String()
	// The QR is rendered as a base64 PNG <img>; the raw qr.alipay.com URL is
	// embedded inside the PNG, so we assert the img tag is present (the actual
	// QR swap target). The wrapper id must match what the skeleton targets.
	if !strings.Contains(body, `<img src="data:image/png;base64,`) {
		t.Errorf("QR img missing in fragment: %s", body)
	}
}

// TestRechargeQR_ShowErrorOnPrecreateFailure verifies a failed precreate swaps
// in a friendly error with a retry link, instead of leaving the placeholder.
func TestRechargeQR_ShowErrorOnPrecreateFailure(t *testing.T) {
	s, u, prov := newRechargeQRSpec(t)
	prov.failCreate = true
	ro := createRechargeOrderDirect(t, s, u, "10")

	req := withPathVal(sessReq("GET", "/recharge/qr/"+itoa(ro.ID), u), "id", itoa(ro.ID))
	rec := httptest.NewRecorder()
	s.rechargeQR(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "alert-error") || !strings.Contains(body, "下单失败") {
		t.Fatalf("expected friendly error, got: %s", body)
	}
	if !strings.Contains(body, "/recharge/pay/"+itoa(ro.ID)) {
		t.Errorf("retry link should point back to pay page")
	}
}

// TestRechargeQR_PaidOrderRedirectsBack: once paid, the async endpoint nudges
// the page to reload so the success branch of the status poll takes over.
func TestRechargeQR_PaidOrderRedirectsBack(t *testing.T) {
	s, u, _ := newRechargeQRSpec(t)
	ro := createRechargeOrderDirect(t, s, u, "10")

	// Manually settle the order so it's "paid".
	if _, err := s.store.SettleRecharge(context.Background(), ro.OutTradeNo, "EP-TEST"); err != nil {
		t.Fatal(err)
	}

	req := withPathVal(sessReq("GET", "/recharge/qr/"+itoa(ro.ID), u), "id", itoa(ro.ID))
	rec := httptest.NewRecorder()
	s.rechargeQR(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "/recharge/pay/"+itoa(ro.ID) {
		t.Fatalf("HX-Redirect = %q", got)
	}
}

// TestRechargeQR_DoesNotRegenerateOnHTMXStatusPoll verifies the status-poll
// branch of rechargePay never triggers a precreate (no more per-poll Alipay
// hits — the original "二维码慢/偶发失败" symptom).
func TestRechargeQR_DoesNotRegenerateOnHTMXStatusPoll(t *testing.T) {
	s, u, prov := newRechargeQRSpec(t)
	ro := createRechargeOrderDirect(t, s, u, "10")

	// First generate the QR once.
	qrReq := withPathVal(sessReq("GET", "/recharge/qr/"+itoa(ro.ID), u), "id", itoa(ro.ID))
	s.rechargeQR(httptest.NewRecorder(), qrReq)

	// Now simulate 5 status polls. None should trigger precreate.
	for i := 0; i < 5; i++ {
		req := withPathVal(sessReq("GET", "/recharge/pay/"+itoa(ro.ID), u), "id", itoa(ro.ID))
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		s.rechargePay(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("poll %d: expected 204 (skip swap), got %d", i, rec.Code)
		}
	}
	if got := atomic.LoadInt32(&prov.createCalls); got != 1 {
		t.Fatalf("expected exactly 1 precreate total, got %d (poll triggered rebuild!)", got)
	}
}

// --- Fix #2: registry not rebuilt per request ---

// TestEpayConfig_DoesNotRebuildRegistryPerRequest proves the race fix: calling
// epayConfig() many times (as the live handler chain does) rebuilds the registry
// at most ONCE (the first time inputs are seen) and never again while inputs
// are stable — so concurrent precreate + poll can't observe a half-wiped table.
//
// We assert by counting the number of times the (real) alipay provider is
// constructed: each rebuild produces a NEW *AlipayProvider. After the first
// epayConfig() establishes a provider, 199 subsequent calls must return the
// SAME instance.
func TestEpayConfig_DoesNotRebuildRegistryPerRequest(t *testing.T) {
	st, _ := store.OpenInMemory()
	st.Migrate()
	ctx := context.Background()
	// Seed config so the fingerprint is stable AND a provider can be built.
	// (No key files in the test dir, so the alipay provider will be
	// unconfigured — but it's still a stable instance we can compare.)
	for k, v := range map[string]string{
		"alipay_appid":   "APP1",
		"alipay_sandbox": "1",
		"epay_merchants": `[{"pid":1001,"key":"k"}]`,
	} {
		st.SetConfig(ctx, k, v)
	}
	srv := &Server{store: st, throttle: auth.NewThrottle(100), now: time.Now}
	t.Cleanup(func() { payment.DefaultRegistry().SetProviders() })

	// First call: establishes the alipay provider from current inputs.
	srv.epayConfig()
	first, ok := payment.DefaultRegistry().Get("alipay")
	if !ok {
		t.Fatal("first epayConfig did not register alipay")
	}

	// 199 more calls with identical inputs: registry must NOT be rebuilt, so the
	// registered provider instance stays the same pointer.
	for i := 0; i < 199; i++ {
		srv.epayConfig()
		cur, ok := payment.DefaultRegistry().Get("alipay")
		if !ok {
			t.Fatalf("iter %d: alipay vanished", i)
		}
		if providerPtr(cur) != providerPtr(first) {
			t.Fatalf("iter %d: alipay provider instance changed — registry rebuilt per request", i)
		}
	}
}

// TestEpayConfig_RebuildsWhenConfigChanges confirms the flip side: editing a
// credential DOES refresh the registry, so admin edits still take effect.
func TestEpayConfig_RebuildsWhenConfigChanges(t *testing.T) {
	st, _ := store.OpenInMemory()
	st.Migrate()
	ctx := context.Background()
	st.SetConfig(ctx, "alipay_appid", "APP1")
	st.SetConfig(ctx, "epay_merchants", `[{"pid":1001,"key":"k"}]`)
	srv := &Server{store: st, throttle: auth.NewThrottle(100), now: time.Now}
	t.Cleanup(func() { payment.DefaultRegistry().SetProviders() })

	srv.epayConfig()
	first, _ := payment.DefaultRegistry().Get("alipay")

	// Change a credential → next epayConfig must rebuild.
	st.SetConfig(ctx, "alipay_appid", "APP2")
	srv.epayConfig()
	cur, _ := payment.DefaultRegistry().Get("alipay")
	if providerPtr(cur) == providerPtr(first) {
		t.Fatal("registry must rebuild when a credential changes")
	}
}

// providerPtr returns a stable identity (the interface's data pointer) so two
// reads of the same instance compare equal. Uses reflect because providers are
// pointers hidden behind the interface.
func providerPtr(p payment.PaymentProvider) uintptr {
	if p == nil {
		return 0
	}
	return reflectPointer(p)
}

// reflectPointer returns the pointer behind an interface value when it holds a
// pointer (chan/func/map/ptr/slice/unsafe.Pointer), else 0. Used only to test
// instance identity of providers.
func reflectPointer(v any) uintptr {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr && !rv.IsNil() {
		return rv.Pointer()
	}
	return 0
}

// itoa is a tiny strconv-free helper to avoid importing strconv just for one call.
func itoa(i int64) string { return intToStr(i) }

func intToStr(i int64) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
