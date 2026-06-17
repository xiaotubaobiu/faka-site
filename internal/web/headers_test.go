package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders_SetsAllHeadersAndNonce(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest("GET", "/login", nil)
	rec := httptest.NewRecorder()
	s.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := nonceFromContext(r)
		if n == "" {
			t.Fatal("nonce missing from request context")
		}
		csp := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "'nonce-"+n+"'") {
			t.Fatalf("CSP must contain context nonce; got %q", csp)
		}
	})).ServeHTTP(rec, req)

	for _, h := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options", "Referrer-Policy"} {
		if rec.Header().Get(h) == "" {
			t.Fatalf("missing security header %q", h)
		}
	}
}

func TestSecurityHeaders_NonceUniquePerRequest(t *testing.T) {
	s := &Server{}
	grab := func() string {
		var n string
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/login", nil)
		s.securityHeaders(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			n = nonceFromContext(r)
		})).ServeHTTP(rec, req)
		return n
	}
	a, b := grab(), grab()
	if a == "" || a == b {
		t.Fatalf("nonces must be non-empty and unique: %q %q", a, b)
	}
}
