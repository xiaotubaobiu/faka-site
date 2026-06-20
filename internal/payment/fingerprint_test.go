package payment

import (
	"context"
	"net/http"
	"sync"
	"testing"
)

// fakeProvider is a minimal PaymentProvider for registry tests.
type fakeProvider struct{ name string }

func (f *fakeProvider) Name() string                                  { return f.name }
func (f *fakeProvider) Configured() bool                              { return true }
func (f *fakeProvider) CreatePayment(context.Context, PaymentRequest) (PaymentResult, error) {
	return PaymentResult{QRCode: "qr-" + f.name}, nil
}
func (f *fakeProvider) ParseNotify(*http.Request) (NotifyInfo, error) { return NotifyInfo{}, nil }
func (f *fakeProvider) NotifyOKResponse() string                      { return "success" }

// TestRegistry_FingerprintShortCircuits verifies the hot-path guarantee that
// drives Fix #2: when the fingerprint is unchanged, the builder is NOT called
// and the existing providers stay put — so a per-request call can't wipe the
// table while a precreate is in flight.
func TestRegistry_FingerprintShortCircuits(t *testing.T) {
	r := &Registry{providers: map[string]PaymentProvider{}}
	calls := 0
	build := func() []PaymentProvider {
		calls++
		return []PaymentProvider{&fakeProvider{name: "alipay"}}
	}

	// First call: builds.
	if !r.SetProvidersIfChanged("fp1", build) {
		t.Fatal("first call with new fingerprint must rebuild")
	}
	if calls != 1 {
		t.Fatalf("builder called %d times, want 1", calls)
	}
	p, ok := r.Get("alipay")
	if !ok || !p.Configured() {
		t.Fatalf("provider missing after build: ok=%v", ok)
	}

	// Second call with same fingerprint: must NOT rebuild.
	if r.SetProvidersIfChanged("fp1", build) {
		t.Fatal("identical fingerprint must not rebuild")
	}
	if calls != 1 {
		t.Fatalf("builder called %d times after identical fp, want 1", calls)
	}

	// Third call with a different fingerprint: rebuilds.
	if !r.SetProvidersIfChanged("fp2", build) {
		t.Fatal("different fingerprint must rebuild")
	}
	if calls != 2 {
		t.Fatalf("builder called %d times after changed fp, want 2", calls)
	}
}

// TestRegistry_ConcurrentSafe exercises the lock under concurrency to ensure
// no data race between readers and the conditional writer (run with -race).
func TestRegistry_ConcurrentSafe(t *testing.T) {
	r := &Registry{providers: map[string]PaymentProvider{}}
	build := func() []PaymentProvider {
		return []PaymentProvider{&fakeProvider{name: "alipay"}}
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			r.SetProvidersIfChanged("fp", build)
		}()
		go func() {
			defer wg.Done()
			r.Get("alipay")
			r.MustGet("alipay").Configured()
		}()
	}
	wg.Wait()
}

// TestConfigFingerprint_StableAndSensitive checks that the fingerprint is
// deterministic for identical inputs, changes when any relevant key changes,
// and is INSENSITIVE to irrelevant keys (so unrelated admin edits don't
// trigger a registry rebuild).
func TestConfigFingerprint_StableAndSensitive(t *testing.T) {
	base := map[string]string{
		"alipay_appid": "APP1", "alipay_gateway": "https://g", "alipay_sandbox": "1",
		"wxpay_appid": "WX1", "wxpay_mchid": "M1", "wxpay_serial_no": "S1",
		"smtp_host": "should-not-matter",
	}
	keys := map[string]string{"alipay_private.pem": "PRIV", "alipay_public.pem": "PUB"}

	fp1 := ConfigFingerprint(base, keys)
	fp2 := ConfigFingerprint(base, keys)
	if fp1 != fp2 {
		t.Fatal("fingerprint must be deterministic for identical inputs")
	}

	// Sensitive to a relevant config key.
	changed := map[string]string{}
	for k, v := range base {
		changed[k] = v
	}
	changed["alipay_appid"] = "APP2"
	if ConfigFingerprint(changed, keys) == fp1 {
		t.Fatal("fingerprint must change when alipay_appid changes")
	}

	// Sensitive to a key-file content change.
	keys2 := map[string]string{"alipay_private.pem": "DIFFERENT", "alipay_public.pem": "PUB"}
	if ConfigFingerprint(base, keys2) == fp1 {
		t.Fatal("fingerprint must change when a key file changes")
	}

	// Insensitive to an irrelevant key.
	irr := map[string]string{}
	for k, v := range base {
		irr[k] = v
	}
	irr["smtp_host"] = "changed"
	irr["newapi_base_url"] = "https://other"
	if ConfigFingerprint(irr, keys) != fp1 {
		t.Fatal("fingerprint must be insensitive to non-payment config keys")
	}
}
