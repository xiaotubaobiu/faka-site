package payment

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestWxpayConfigured(t *testing.T) {
	p := NewWxpayProvider(WxpayConfig{})
	if p.Configured() {
		t.Fatal("empty config should be unconfigured")
	}
}

func TestWxpaySignRoundTrip(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	sig := wxpaySign(key, "POST", "/v3/pay/transactions/native", 1700000000, "abc123", `{"k":"v"}`)
	if sig == "" {
		t.Fatal("empty signature")
	}
	sigBytes, _ := base64.StdEncoding.DecodeString(sig)

	// Reconstruct the canonical message and verify.
	message := "POST\n/v3/pay/transactions/native\n1700000000\nabc123\n{\"k\":\"v\"}\n"
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, 0, sha256Bytes(message), sigBytes); err != nil {
		t.Fatalf("verify failed: %v", err)
	}

	// Tampered message must fail.
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, 0, sha256Bytes(message+"x"), sigBytes); err == nil {
		t.Fatal("tampered message should fail verification")
	}
}

func sha256Bytes(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func TestWxPath(t *testing.T) {
	cases := map[string]string{
		"https://api.mch.weixin.qq.com/v3/pay/transactions/native": "/v3/pay/transactions/native",
		"https://api.mch.weixin.qq.com/v3/pay?a=1":                 "/v3/pay?a=1",
		"/local/path": "/local/path",
	}
	for in, want := range cases {
		if got := wxPath(in); got != want {
			t.Errorf("wxPath(%q)=%q want %q", in, got, want)
		}
	}
}

func TestWxpayNotifyDecrypt(t *testing.T) {
	privPEM, _ := genTestRSA(t)
	key := strings.Repeat("k", 32) // APIv3Key
	p := NewWxpayProvider(WxpayConfig{
		AppID: "a", MchID: "m", MchSerialNo: "s", APIv3Key: key, PrivateKey: privPEM,
	})

	// Build a realistic encrypted callback payload.
	nonce := "0123456789ab" // 12-byte nonce (AES-GCM standard nonce size)
	associated := "transaction"
	plaintext, _ := json.Marshal(map[string]any{
		"out_trade_no":   "OUT1",
		"transaction_id": "WX1",
		"trade_state":    "SUCCESS",
		"amount":         map[string]any{"total": 2500},
	})

	block, _ := aes.NewCipher([]byte(key))
	gcm, _ := cipher.NewGCM(block)
	ct := gcm.Seal(nil, []byte(nonce), plaintext, []byte(associated))
	env := map[string]any{
		"resource": map[string]any{
			"ciphertext":      base64.StdEncoding.EncodeToString(ct),
			"nonce":           nonce,
			"associated_data": associated,
		},
	}
	body, _ := json.Marshal(env)

	req, _ := http.NewRequest("POST", "/notify/wxpay", strings.NewReader(string(body)))
	info, err := p.ParseNotify(req)
	if err != nil {
		t.Fatalf("ParseNotify: %v", err)
	}
	if info.OutTradeNo != "OUT1" || info.TradeNo != "WX1" {
		t.Fatalf("got %+v", info)
	}
	if info.AmountFen != 2500 {
		t.Fatalf("amount = %d want 2500", info.AmountFen)
	}
}

func TestWxpayNotifyRejectsBadKey(t *testing.T) {
	privPEM, _ := genTestRSA(t)
	p := NewWxpayProvider(WxpayConfig{
		AppID: "a", MchID: "m", MchSerialNo: "s", APIv3Key: strings.Repeat("k", 32), PrivateKey: privPEM,
	})
	// encrypted with a different key
	other := strings.Repeat("z", 32)
	block, _ := aes.NewCipher([]byte(other))
	gcm, _ := cipher.NewGCM(block)
	ct := gcm.Seal(nil, []byte("0123456789ab"), []byte(`{}`), nil)
	env := map[string]any{
		"resource": map[string]any{
			"ciphertext": base64.StdEncoding.EncodeToString(ct),
			"nonce":      "0123456789ab",
		},
	}
	body, _ := json.Marshal(env)
	req, _ := http.NewRequest("POST", "/notify/wxpay", strings.NewReader(string(body)))
	if _, err := p.ParseNotify(req); err == nil {
		t.Fatal("ParseNotify must reject ciphertext encrypted with wrong key")
	}
}

func TestWxpayNotifyNonSuccessIgnored(t *testing.T) {
	privPEM, _ := genTestRSA(t)
	key := strings.Repeat("k", 32)
	p := NewWxpayProvider(WxpayConfig{
		AppID: "a", MchID: "m", MchSerialNo: "s", APIv3Key: key, PrivateKey: privPEM,
	})
	nonce := "0123456789ab"
	plaintext, _ := json.Marshal(map[string]any{
		"out_trade_no": "OUT1", "trade_state": "NOTPAY",
	})
	block, _ := aes.NewCipher([]byte(key))
	gcm, _ := cipher.NewGCM(block)
	ct := gcm.Seal(nil, []byte(nonce), plaintext, nil)
	env := map[string]any{
		"resource": map[string]any{
			"ciphertext": base64.StdEncoding.EncodeToString(ct),
			"nonce":      nonce,
		},
	}
	body, _ := json.Marshal(env)
	req, _ := http.NewRequest("POST", "/notify/wxpay", strings.NewReader(string(body)))
	if _, err := p.ParseNotify(req); err == nil {
		t.Fatal("non-SUCCESS trade_state should be rejected")
	}
}

// keep the time import referenced; helps spot dead imports quickly.
var _ = time.Now
