package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// AlipayConfig holds the credentials for Alipay 当面付 (face-to-face / precreate).
// PrivateKey and PublicKey are PKCS#1 or PKCS#8 PEM strings (already decrypted
// from the config store by the caller).
type AlipayConfig struct {
	AppID      string
	PrivateKey string // 应用私钥 PEM
	PublicKey  string // 支付宝公钥 PEM(用于验签回调)
	Gateway    string // 默认 https://openapi.alipay.com/gateway.do
	SignType   string // RSA2(默认)或 RSA
}

// defaultAlipayGateway is the production Alipay openapi gateway.
const defaultAlipayGateway = "https://openapi.alipay.com/gateway.do"

// AlipayProvider implements PaymentProvider using alipay.trade.precreate (扫码).
type AlipayProvider struct {
	cfg        AlipayConfig
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	httpClient *http.Client
}

// NewAlipayProvider builds a provider from config. Returns a provider with
// Configured()==false (rather than an error) when credentials are missing,
// so the registry can always hold an entry.
func NewAlipayProvider(cfg AlipayConfig) *AlipayProvider {
	if cfg.Gateway == "" {
		cfg.Gateway = defaultAlipayGateway
	}
	if cfg.SignType == "" {
		cfg.SignType = "RSA2"
	}
	p := &AlipayProvider{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
	p.privateKey, _ = parseRSAPrivateKey(cfg.PrivateKey)
	p.publicKey, _ = parseRSAPublicKey(cfg.PublicKey)
	return p
}

func (p *AlipayProvider) Name() string { return "alipay" }

func (p *AlipayProvider) Configured() bool {
	return p.cfg.AppID != "" && p.privateKey != nil && p.publicKey != nil
}

func (p *AlipayProvider) NotifyOKResponse() string { return "success" }

// CreatePayment calls alipay.trade.precreate to get a dynamic QR code.
func (p *AlipayProvider) CreatePayment(ctx context.Context, req PaymentRequest) (PaymentResult, error) {
	if !p.Configured() {
		return PaymentResult{}, ErrNotConfigured
	}
	biz := map[string]any{
		"out_trade_no": req.OutTradeNo,
		"total_amount": fenToYuan(req.AmountFen),
		"subject":      req.Subject,
	}
	// timeout_express tells Alipay to auto-close the trade when unpaid past the
	// window (e.g. "5m"), so a stale QR can't be paid after we expire the order.
	if req.TimeoutExpress != "" {
		biz["timeout_express"] = req.TimeoutExpress
	}
	bizJSON, _ := json.Marshal(biz)

	form := url.Values{}
	form.Set("app_id", p.cfg.AppID)
	form.Set("method", "alipay.trade.precreate")
	form.Set("charset", "utf-8")
	form.Set("sign_type", p.cfg.SignType)
	form.Set("timestamp", time.Now().Format("2006-01-02 15:04:05"))
	form.Set("version", "1.0")
	form.Set("biz_content", string(bizJSON))
	if req.NotifyURL != "" {
		form.Set("notify_url", req.NotifyURL)
	}
	form.Set("sign", alipaySign(form, p.privateKey, p.cfg.SignType))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.Gateway, strings.NewReader(form.Encode()))
	if err != nil {
		return PaymentResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return PaymentResult{}, fmt.Errorf("alipay precreate: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Alipay wraps the response under a key matching the method with _response.
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return PaymentResult{}, fmt.Errorf("alipay precreate: bad response: %s", truncate(body))
	}
	key := "alipay_trade_precreate_response"
	raw, ok := envelope[key]
	if !ok {
		return PaymentResult{}, fmt.Errorf("alipay precreate: missing response: %s", truncate(body))
	}

	// Verify the response signature before trusting it.
	if sig, _ := base64.StdEncoding.DecodeString(stringValue(envelope["sign"])); len(sig) > 0 && p.publicKey != nil {
		if !rsaVerify(raw, sig, p.publicKey, hashFor(p.cfg.SignType)) {
			return PaymentResult{}, errors.New("alipay precreate: response sign mismatch")
		}
	}

	var pre struct {
		OutTradeNo string `json:"out_trade_no"`
		QRCode     string `json:"qr_code"`
		Code       string `json:"code"`
		Msg        string `json:"msg"`
		SubCode    string `json:"sub_code"`
		SubMsg     string `json:"sub_msg"`
	}
	if err := json.Unmarshal(raw, &pre); err != nil {
		return PaymentResult{}, fmt.Errorf("alipay precreate: decode: %w", err)
	}
	if pre.Code != "10000" {
		return PaymentResult{}, fmt.Errorf("alipay precreate failed: code=%s msg=%s sub=%s %s",
			pre.Code, pre.Msg, pre.SubCode, pre.SubMsg)
	}
	return PaymentResult{QRCode: pre.QRCode}, nil
}

// ParseNotify verifies the Alipay async callback (POST form, sign field) and
// extracts the trade info. Only trade_status == TRADE_SUCCESS is honored.
func (p *AlipayProvider) ParseNotify(r *http.Request) (NotifyInfo, error) {
	if !p.Configured() {
		return NotifyInfo{}, ErrNotConfigured
	}
	if err := r.ParseForm(); err != nil {
		return NotifyInfo{}, err
	}
	form := r.PostForm
	if len(form) == 0 {
		form = r.Form
	}
	sig, err := base64.StdEncoding.DecodeString(form.Get("sign"))
	if err != nil || len(sig) == 0 {
		return NotifyInfo{}, errors.New("alipay notify: missing sign")
	}
	payload := alipaySignPayload(form)
	if !rsaVerify([]byte(payload), sig, p.publicKey, hashFor(p.cfg.SignType)) {
		return NotifyInfo{}, errors.New("alipay notify: sign verification failed")
	}
	if form.Get("trade_status") != "TRADE_SUCCESS" {
		return NotifyInfo{}, fmt.Errorf("alipay notify: status %s", form.Get("trade_status"))
	}
	fen, _ := yuanToFenInt(form.Get("total_amount"))
	return NotifyInfo{
		OutTradeNo: form.Get("out_trade_no"),
		TradeNo:    form.Get("trade_no"),
		AmountFen:  fen,
	}, nil
}

// ---------- RSA helpers ----------

// hashFor maps the Alipay sign_type to a crypto.Hash.
func hashFor(signType string) crypto.Hash {
	if signType == "RSA" {
		return crypto.SHA1
	}
	return crypto.SHA256
}

// hashBytes hashes data with the chosen algorithm.
func hashBytes(data []byte, h crypto.Hash) []byte {
	switch h {
	case crypto.SHA1:
		s := sha1.Sum(data)
		return s[:]
	default:
		s := sha256.Sum256(data)
		return s[:]
	}
}

// alipaySign produces the base64 RSA signature for a set of form params.
func alipaySign(v url.Values, key *rsa.PrivateKey, signType string) string {
	if key == nil {
		return ""
	}
	payload := alipayRequestSignPayload(v)
	h := hashFor(signType)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, h, hashBytes([]byte(payload), h))
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(sig)
}

// alipayRequestSignPayload builds the canonical string to sign for an openapi
// GATEWAY REQUEST: sorted, non-empty params joined by &, excluding only sign.
// Unlike async-notify verification, gateway requests sign over sign_type too;
// omitting it makes Alipay reject with sub_code isv.invalid-signature.
func alipayRequestSignPayload(v url.Values) string {
	keys := make([]string, 0, len(v))
	for k, vs := range v {
		if k == "sign" {
			continue
		}
		if len(vs) == 0 || vs[0] == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+v.Get(k))
	}
	return strings.Join(parts, "&")
}

// alipaySignPayload builds the canonical string to sign: sorted, non-empty
// params joined by &, excluding sign and sign_type. Used for async-notify
// verification (notify omits sign_type from the signed set).
func alipaySignPayload(v url.Values) string {
	keys := make([]string, 0, len(v))
	for k, vs := range v {
		if k == "sign" || k == "sign_type" {
			continue
		}
		if len(vs) == 0 || vs[0] == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+v.Get(k))
	}
	return strings.Join(parts, "&")
}

func rsaVerify(payload, sig []byte, pub *rsa.PublicKey, h crypto.Hash) bool {
	if pub == nil {
		return false
	}
	return rsa.VerifyPKCS1v15(pub, h, hashBytes(payload, h), sig) == nil
}

// parseRSAPrivateKey parses a PEM-encoded PKCS#1 or PKCS#8 RSA private key.
func parseRSAPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("no PEM block in private key")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("not an RSA private key")
	}
	return rk, nil
}

// parseRSAPublicKey parses a PEM-encoded PKCS#1 or PKIX RSA public key.
func parseRSAPublicKey(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("no PEM block in public key")
	}
	if k, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rk, ok := k.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not an RSA public key")
	}
	return rk, nil
}

// ---------- small helpers ----------

func fenToYuan(fen int64) string {
	return fmt.Sprintf("%.2f", float64(fen)/100)
}

func yuanToFenInt(yuan string) (int64, error) {
	f, err := parseFloat(yuan)
	if err != nil {
		return 0, err
	}
	return int64(math.Round(f * 100)), nil
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

func stringValue(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func truncate(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "..."
	}
	return string(b)
}
