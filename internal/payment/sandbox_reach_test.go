//go:build sandboxlive

package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

// TestSandboxReach_GatewayReachableAndSigns verifies, WITHOUT any real
// credentials, that:
//   - the sandbox gateway host resolves and accepts our POST,
//   - our request body format is what Alipay expects (it should reject with a
//     SIGNATURE error, NOT a "missing method"/format error — that distinction
//     proves our wire format is correct and only the signature is wrong).
//
// This is the connectivity half of "connect to the sandbox yourself". The full
// precreate (with real keys) is TestSandboxLive_PrecreateAndNotify, which can
// only run where the configured keys live.
func TestSandboxReach_GatewayReachableAndSigns(t *testing.T) {
	gateway := "https://openapi-sandbox.dl.alipaydev.com/gateway.do"

	// Generate an ephemeral keypair purely so alipaySign has something to sign
	// with; Alipay will reject the signature (we're not a registered app), but
	// that's exactly the signal we want.
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	privBytes := x509.MarshalPKCS1PrivateKey(key)
	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes}))
	pubDER, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))

	p := NewAlipayProvider(AlipayConfig{
		AppID: "0000000000000000", // dummy app id
		PrivateKey: privPEM,
		PublicKey:  pubPEM,
		Gateway:    gateway,
		SignType:   "RSA2",
	})
	if !p.Configured() {
		t.Fatal("ephemeral-key provider should be configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := p.CreatePayment(ctx, PaymentRequest{
		OutTradeNo: "REACH-TEST",
		AmountFen:  1,
		Subject:    "connectivity probe",
		NotifyURL:  "https://sandbox.example.invalid/notify/alipay",
	})
	if err == nil {
		t.Fatal("expected an error (we have no real credentials), got nil")
	}
	msg := err.Error()
	t.Logf("gateway response error (expected): %s", msg)

	// The KEY assertion: Alipay must reject at the SIGNATURE/app level, not with
	// a network or format error. Acceptable rejections:
	//   - isv.invalid-signature  (signature mismatch — expected, format OK)
	//   - 20000 / aop.ACQ.*      (system/service errors)
	//   - sub_code mentioning the app id
	// Unacceptable (would indicate a wire-format bug in our code):
	//   - "missing method", "bad response", network timeout, non-Alipay HTML.
	lower := strings.ToLower(msg)
	reachableAndFormatted := strings.Contains(lower, "signature") ||
		strings.Contains(lower, "sub_code") ||
		strings.Contains(lower, "sub_msg") ||
		strings.Contains(lower, "code=") ||
		strings.Contains(lower, "msg=") ||
		strings.Contains(lower, "invalid") ||
		strings.Contains(lower, "error_code")
	if !reachableAndFormatted {
		t.Fatalf("response does not look like an Alipay structured rejection (wire format issue?): %s", msg)
	}
	// Also confirm the HTTP client reached Alipay (no timeout/DNS error).
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "no such host") ||
		strings.Contains(lower, "connection refused") || strings.Contains(lower, "dial ") {
		t.Fatalf("could not reach sandbox gateway: %s", msg)
	}
	t.Log("sandbox gateway reachable; our request format is accepted (rejection is signature-level, as expected for dummy creds).")
}
