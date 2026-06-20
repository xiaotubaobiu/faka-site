package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"faka-site/internal/auth"
	"faka-site/internal/store"
)

// --- isValidHTTPSBase (Fix #1 part 2: config-page validation) ---

func TestIsValidHTTPSBase(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://faka.example.com", true},
		{"https://faka.example.com", true},
		{"http://faka.example.com", false},  // must be https
		{"https://faka.example.com/sub", false}, // no path
		{"faka.example.com", false},         // no scheme
		{"", false},                         // empty handled by caller; still invalid
		{"https://", false},                 // no host
		{"https://faka.example.com?q=1", false}, // query rejected by path check via Parse
		{"ftp://faka.example.com", false},   // wrong scheme
	}
	for _, c := range cases {
		if got := isValidHTTPSBase(c.in); got != c.want {
			t.Errorf("isValidHTTPSBase(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// --- officialNotifyURLForRequest (Fix #1 part 1: Host fallback) ---

// newRechargeServerNotify builds a Server for notify-URL tests, optionally
// seeding recharge_notify_base. It does NOT touch key files.
func newRechargeServerNotify(t *testing.T, notifyBase string) *Server {
	t.Helper()
	st, _ := store.OpenInMemory()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	if notifyBase != "" {
		if err := st.SetConfig(context.Background(), "recharge_notify_base", notifyBase); err != nil {
			t.Fatal(err)
		}
	}
	return &Server{store: st, throttle: auth.NewThrottle(100), now: nil}
}

func TestOfficialNotifyURL_UsesConfiguredBase(t *testing.T) {
	s := newRechargeServerNotify(t, "https://faka.example.com")
	req := httptest.NewRequest(http.MethodGet, "https://other.example.com/recharge/pay/1", nil)
	got := s.officialNotifyURLForRequest("alipay", req)
	want := "https://faka.example.com/notify/alipay"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestOfficialNotifyURL_FallsBackToRequestHost is the core Fix #1 regression:
// when RechargeNotifyBase is empty (the misconfiguration that caused
// "paid but not credited"), the per-request URL is built from the request's own
// scheme+host so Alipay always receives a non-empty notify_url.
func TestOfficialNotifyURL_FallsBackToRequestHost(t *testing.T) {
	s := newRechargeServerNotify(t, "") // no base configured

	// Behind Caddy: loopback RemoteAddr + X-Forwarded-Proto: https.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "faka.public.example.com"
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	got := s.officialNotifyURLForRequest("alipay", req)
	want := "https://faka.public.example.com/notify/alipay"
	if got != want {
		t.Fatalf("loopback+XFP fallback: got %q, want %q", got, want)
	}

	// Plain HTTP direct hit.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Host = "localhost:8080"
	req2.RemoteAddr = "192.168.1.5:1234"
	got2 := s.officialNotifyURLForRequest("alipay", req2)
	want2 := "http://localhost:8080/notify/alipay"
	if got2 != want2 {
		t.Fatalf("direct http fallback: got %q, want %q", got2, want2)
	}
}

// TestOfficialNotifyURL_LegacyNoRequestReturnsEmpty confirms the no-request
// helper still returns "" when unconfigured (used only by tests / non-request
// code paths), so we don't accidentally synthesise a bogus URL.
func TestOfficialNotifyURL_LegacyNoRequestReturnsEmpty(t *testing.T) {
	s := newRechargeServerNotify(t, "")
	if got := s.officialNotifyURL("alipay"); got != "" {
		t.Fatalf("legacy helper with empty base should return empty, got %q", got)
	}
}

// TestSchemeFromRequest exercises the scheme resolver used for the fallback.
func TestSchemeFromRequest(t *testing.T) {
	// TLS direct.
	req := httptest.NewRequest("GET", "https://x/", nil)
	req.RemoteAddr = "8.8.8.8:1"
	if got := schemeFromRequest(req); got != "https" {
		t.Fatalf("tls: got %q", got)
	}
	// Loopback + X-Forwarded-Proto.
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1"
	req.Header.Set("X-Forwarded-Proto", "https")
	if got := schemeFromRequest(req); got != "https" {
		t.Fatalf("loopback xfp: got %q", got)
	}
	// Non-loopback XFP must be ignored (anti-spoofing).
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "8.8.8.8:1"
	req.Header.Set("X-Forwarded-Proto", "https")
	if got := schemeFromRequest(req); got != "http" {
		t.Fatalf("non-loopback xfp must be ignored: got %q", got)
	}
}

// TestPostConfig_RejectsNonHTTPSNotifyBase verifies the save-time guardrail:
// a non-https / malformed notify base is rejected with cfgErr and the bad
// value is NOT persisted.
func TestPostConfig_RejectsNonHTTPSNotifyBase(t *testing.T) {
	st, _ := store.OpenInMemory()
	st.Migrate()
	// Bootstrap admin session via context (postConfig needs currentUser? no —
	// it doesn't, but the route is admin-gated; we call the handler directly).
	s := &Server{store: st, throttle: auth.NewThrottle(100)}

	body := "csrf=x&recharge_notify_base=http://insecure.example.com"
	req := httptest.NewRequest("POST", "/admin/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.postConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (re-render), got %d", rec.Code)
	}
	// cfgErr is rendered as an alert-error block in the page.
	if !strings.Contains(rec.Body.String(), "alert-error") {
		t.Fatalf("expected an error alert in the rendered page")
	}
	// Must not have been persisted.
	got, _ := st.GetConfig(context.Background(), "recharge_notify_base")
	if got != "" {
		t.Fatalf("bad notify base must not be persisted, got %q", got)
	}
}

// TestPostConfig_AcceptsHTTPSNotifyBase confirms a valid https base is saved.
func TestPostConfig_AcceptsHTTPSNotifyBase(t *testing.T) {
	st, _ := store.OpenInMemory()
	st.Migrate()
	s := &Server{store: st, throttle: auth.NewThrottle(100)}

	body := "csrf=x&recharge_notify_base=https://faka.example.com"
	req := httptest.NewRequest("POST", "/admin/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.postConfig(rec, req)

	got, _ := st.GetConfig(context.Background(), "recharge_notify_base")
	if got != "https://faka.example.com" {
		t.Fatalf("valid https base must be persisted, got %q", got)
	}
}
