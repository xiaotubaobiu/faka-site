package payment

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// genTestRSA returns a PEM-encoded 2048-bit RSA keypair for tests.
func genTestRSA(t *testing.T) (privPEM, pubPEM string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privBytes := x509.MarshalPKCS1PrivateKey(key)
	privPEM = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes}))

	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
	return
}

func TestAlipaySignVerifyRoundTrip(t *testing.T) {
	privPEM, pubPEM := genTestRSA(t)
	p := NewAlipayProvider(AlipayConfig{AppID: "app1", PrivateKey: privPEM, PublicKey: pubPEM})
	if !p.Configured() {
		t.Fatal("provider should be configured")
	}

	form := url.Values{
		"app_id":      {"app1"},
		"method":      {"alipay.trade.precreate"},
		"biz_content": {`{"out_trade_no":"T1","total_amount":"0.01"}`},
		"sign_type":   {"RSA2"},
	}
	sig := alipaySign(form, p.privateKey, "RSA2")
	if sig == "" {
		t.Fatal("sign returned empty")
	}
	sigBytes, _ := base64.StdEncoding.DecodeString(sig)

	// Verifying the exact signed payload must succeed. Gateway requests sign
	// over sign_type, so verify against alipayRequestSignPayload (matches alipaySign).
	if !rsaVerify([]byte(alipayRequestSignPayload(form)), sigBytes, p.publicKey, crypto.SHA256) {
		t.Fatal("verify failed on round-trip")
	}

	// Tampering must fail.
	form.Set("biz_content", `{"out_trade_no":"TAMPERED"}`)
	if rsaVerify([]byte(alipayRequestSignPayload(form)), sigBytes, p.publicKey, crypto.SHA256) {
		t.Fatal("verify should fail on tampered payload")
	}
}

func TestAlipaySignPayloadExcludesSignFields(t *testing.T) {
	form := url.Values{
		"a":         {"1"},
		"sign":      {"should-be-ignored"},
		"sign_type": {"RSA2"},
		"empty":     {""},
		"b":         {"2"},
	}
	got := alipaySignPayload(form)
	want := "a=1&b=2"
	if got != want {
		t.Fatalf("payload = %q, want %q", got, want)
	}
}

func TestAlipayParseNotifyVerifies(t *testing.T) {
	privPEM, pubPEM := genTestRSA(t)
	p := NewAlipayProvider(AlipayConfig{AppID: "app1", PrivateKey: privPEM, PublicKey: pubPEM})

	form := url.Values{
		"out_trade_no": {"OUT123"},
		"trade_no":     {"ALI456"},
		"total_amount": {"10.00"},
		"trade_status": {"TRADE_SUCCESS"},
	}
	sig := alipaySign(form, p.privateKey, "RSA2")

	body := form
	body.Set("sign", sig)
	req, _ := http.NewRequest("POST", "/notify/alipay", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	info, err := p.ParseNotify(req)
	if err != nil {
		t.Fatalf("ParseNotify: %v", err)
	}
	if info.OutTradeNo != "OUT123" || info.TradeNo != "ALI456" {
		t.Fatalf("got %+v", info)
	}
	if info.AmountFen != 1000 {
		t.Fatalf("amount = %d, want 1000", info.AmountFen)
	}
}

func TestAlipayParseNotifyRejectsBadSign(t *testing.T) {
	privPEM, pubPEM := genTestRSA(t)
	p := NewAlipayProvider(AlipayConfig{AppID: "app1", PrivateKey: privPEM, PublicKey: pubPEM})

	form := url.Values{
		"out_trade_no": {"OUT123"},
		"trade_status": {"TRADE_SUCCESS"},
		"total_amount": {"10.00"},
		"sign":         {"AAAAinvalidbase64sigAAAA"},
	}
	req, _ := http.NewRequest("POST", "/notify/alipay", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := p.ParseNotify(req); err == nil {
		t.Fatal("ParseNotify must reject bad sign")
	}
}

func TestAlipayNotConfigured(t *testing.T) {
	p := NewAlipayProvider(AlipayConfig{})
	if p.Configured() {
		t.Fatal("empty config should be unconfigured")
	}
	if _, err := p.ParseNotify(&http.Request{}); err == nil {
		t.Fatal("ParseNotify on unconfigured must error")
	}
}

func TestFenYuanConversion(t *testing.T) {
	if fenToYuan(1000) != "10.00" {
		t.Fatalf("fenToYuan(1000)=%q", fenToYuan(1000))
	}
	fen, _ := yuanToFenInt("10.00")
	if fen != 1000 {
		t.Fatalf("yuanToFenInt=%d want 1000", fen)
	}
}
