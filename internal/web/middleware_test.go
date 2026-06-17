package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIP_IgnoresXFFWhenNotLoopback(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	req.Header.Set("X-Forwarded-For", "9.9.9.9")
	if got := clientIP(req); got != "203.0.113.5" {
		t.Fatalf("expected direct public IP, got %q", got)
	}
}

func TestClientIP_TrustsXFFWhenLoopback(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "9.9.9.9, 10.0.0.1")
	if got := clientIP(req); got != "9.9.9.9" {
		t.Fatalf("expected first XFF hop, got %q", got)
	}
}

func TestClientIP_FallsBackToRemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	if got := clientIP(req); got != "127.0.0.1" {
		t.Fatalf("expected loopback addr, got %q", got)
	}
}

var _ = http.MethodGet
