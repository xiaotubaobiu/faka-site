package payment

import (
	"strings"
	"sync"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	t.Setenv("PAY_SECRET", strings.Repeat("k", 32))
	// Reset the once so the new env takes effect for this test.
	secretOnce = sync.Once{}
	secretKey = nil
	secretErr = nil

	cases := []string{
		"hello",
		"-----BEGIN PRIVATE KEY-----\nMIIE...\n-----END PRIVATE KEY-----",
		"中文密钥测试",
		strings.Repeat("x", 1024),
	}
	for _, in := range cases {
		ct, err := Seal(in)
		if err != nil {
			t.Fatalf("Seal(%q): %v", in, err)
		}
		if !strings.HasPrefix(ct, encPrefix) {
			t.Fatalf("ciphertext missing prefix: %q", ct)
		}
		if ct == encPrefix+in {
			t.Fatalf("ciphertext not actually encrypted")
		}
		pt, err := Open(ct)
		if err != nil {
			t.Fatalf("Open(%q): %v", ct, err)
		}
		if pt != in {
			t.Fatalf("round-trip mismatch: got %q want %q", pt, in)
		}
	}
}

func TestSealEmpty(t *testing.T) {
	ct, err := Seal("")
	if err != nil || ct != "" {
		t.Fatalf("Seal(\"\") = (%q,%v), want (\"\",nil)", ct, err)
	}
	pt, err := Open("")
	if err != nil || pt != "" {
		t.Fatalf("Open(\"\") = (%q,%v), want (\"\",nil)", pt, err)
	}
}

func TestOpenLegacyPlaintext(t *testing.T) {
	// Values without the enc: prefix pass through unchanged so old config
	// keeps working during migration.
	pt, err := Open("legacy-plaintext-key")
	if err != nil || pt != "legacy-plaintext-key" {
		t.Fatalf("legacy passthrough: got (%q,%v)", pt, err)
	}
}

func TestOpenRejectsWrongSecret(t *testing.T) {
	t.Setenv("PAY_SECRET", strings.Repeat("a", 32))
	secretOnce = sync.Once{}
	secretKey = nil
	secretErr = nil
	ct, _ := Seal("secret")

	t.Setenv("PAY_SECRET", strings.Repeat("b", 32))
	secretOnce = sync.Once{}
	secretKey = nil
	secretErr = nil
	if _, err := Open(ct); err == nil {
		t.Fatal("Open with wrong key must fail")
	}
}

func TestHexSecretAccepted(t *testing.T) {
	t.Setenv("PAY_SECRET", strings.Repeat("ab", 32)) // 64 hex chars
	secretOnce = sync.Once{}
	secretKey = nil
	secretErr = nil
	ct, err := Seal("x")
	if err != nil {
		t.Fatalf("Seal with hex key: %v", err)
	}
	if _, err := Open(ct); err != nil {
		t.Fatalf("Open with hex key: %v", err)
	}
}

func TestBadSecretRejected(t *testing.T) {
	t.Setenv("PAY_SECRET", "tooshort")
	secretOnce = sync.Once{}
	secretKey = nil
	secretErr = nil
	if _, err := Seal("x"); err == nil {
		t.Fatal("Seal with short secret must fail")
	}
}
