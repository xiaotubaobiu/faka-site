package auth

import "testing"

func TestSessionRoundTrip(t *testing.T) {
	secret := []byte("k")
	s, _ := NewSession(42, "admin", 1000)
	c, err := EncodeSession(s, secret)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeSession(c, secret, 1000)
	if err != nil || got.UserID != 42 || got.CSRF != s.CSRF {
		t.Fatalf("roundtrip failed: %+v %v", got, err)
	}
	if _, err := DecodeSession(c, []byte("wrong"), 1000); err != ErrInvalidSession {
		t.Fatal("bad secret should fail")
	}
	if _, err := DecodeSession(c, secret, s.Exp+1); err != ErrInvalidSession {
		t.Fatal("expired should fail")
	}
}
