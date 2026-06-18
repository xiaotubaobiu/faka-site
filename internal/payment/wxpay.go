package payment

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// WxpayConfig holds WeChat Pay v3 credentials for Native (扫码) payments.
// APIv3Key and PrivateKey are plaintext (already decrypted by the caller).
type WxpayConfig struct {
	AppID       string // 应用/公众号 AppID
	MchID       string // 商户号
	MchSerialNo string // 商户证书序列号
	APIv3Key    string // APIv3 密钥(32字节,用于解密回调)
	PrivateKey  string // 商户私钥 PEM(RSA2048)
	BaseURL     string // 默认 https://api.mch.weixin.qq.com
}

const defaultWxpayBaseURL = "https://api.mch.weixin.qq.com"

// WxpayProvider implements PaymentProvider using /v3/pay/transactions/native.
type WxpayProvider struct {
	cfg        WxpayConfig
	privateKey *rsa.PrivateKey
	httpClient *http.Client
}

// NewWxpayProvider builds a provider. As with Alipay, a missing-credential
// provider is returned (Configured()==false) rather than an error.
func NewWxpayProvider(cfg WxpayConfig) *WxpayProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultWxpayBaseURL
	}
	p := &WxpayProvider{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
	p.privateKey, _ = parseRSAPrivateKey(cfg.PrivateKey)
	return p
}

func (p *WxpayProvider) Name() string { return "wxpay" }

func (p *WxpayProvider) Configured() bool {
	return p.cfg.AppID != "" && p.cfg.MchID != "" && p.cfg.MchSerialNo != "" &&
		p.cfg.APIv3Key != "" && p.privateKey != nil
}

func (p *WxpayProvider) NotifyOKResponse() string {
	return `{"code":"SUCCESS","message":"成功"}`
}

// CreatePayment calls POST /v3/pay/transactions/native and returns code_url.
func (p *WxpayProvider) CreatePayment(ctx context.Context, req PaymentRequest) (PaymentResult, error) {
	if !p.Configured() {
		return PaymentResult{}, ErrNotConfigured
	}
	body := map[string]any{
		"appid":        p.cfg.AppID,
		"mchid":        p.cfg.MchID,
		"description":  req.Subject,
		"out_trade_no": req.OutTradeNo,
		"notify_url":   req.NotifyURL,
		"amount": map[string]any{
			"total":    req.AmountFen, // 分,整数
			"currency": "CNY",
		},
	}
	payload, _ := json.Marshal(body)

	endpoint := p.cfg.BaseURL + "/v3/pay/transactions/native"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return PaymentResult{}, err
	}
	timestamp := time.Now().Unix()
	nonce, err := randNonce()
	if err != nil {
		return PaymentResult{}, err
	}
	signature := wxpaySign(p.privateKey, http.MethodPost, wxPath(endpoint), timestamp, nonce, string(payload))

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "faka-site")
	httpReq.Header.Set("Authorization", wxpayAuthorization(p.cfg.MchID, p.cfg.MchSerialNo, nonce, timestamp, signature))

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return PaymentResult{}, fmt.Errorf("wxpay native: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return PaymentResult{}, fmt.Errorf("wxpay native: http %d: %s", resp.StatusCode, truncate(respBody))
	}
	var out struct {
		CodeURL string `json:"code_url"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return PaymentResult{}, fmt.Errorf("wxpay native: decode: %w", err)
	}
	if out.CodeURL == "" {
		return PaymentResult{}, fmt.Errorf("wxpay native: empty code_url: %s", truncate(respBody))
	}
	return PaymentResult{QRCode: out.CodeURL}, nil
}

// ParseNotify decrypts the WeChat Pay v3 callback. The request body is JSON
// with an encrypted "resource" field; we decrypt with APIv3Key (AES-256-GCM)
// and extract trade info.
func (p *WxpayProvider) ParseNotify(r *http.Request) (NotifyInfo, error) {
	if !p.Configured() {
		return NotifyInfo{}, ErrNotConfigured
	}
	var env struct {
		Resource struct {
			Ciphertext     string `json:"ciphertext"`
			Nonce          string `json:"nonce"`
			AssociatedData string `json:"associated_data"`
		} `json:"resource"`
	}
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		return NotifyInfo{}, fmt.Errorf("wxpay notify: decode envelope: %w", err)
	}
	plaintext, err := wxpayDecrypt(env.Resource.Ciphertext, env.Resource.Nonce, env.Resource.AssociatedData, p.cfg.APIv3Key)
	if err != nil {
		return NotifyInfo{}, fmt.Errorf("wxpay notify: decrypt: %w", err)
	}
	var data struct {
		OutTradeNo string `json:"out_trade_no"`
		TradeState string `json:"trade_state"`
		TradeNo    string `json:"transaction_id"`
		Amount     struct {
			Total int64 `json:"total"` // 分
		} `json:"amount"`
	}
	if err := json.Unmarshal(plaintext, &data); err != nil {
		return NotifyInfo{}, fmt.Errorf("wxpay notify: decode resource: %w", err)
	}
	if data.TradeState != "SUCCESS" {
		return NotifyInfo{}, fmt.Errorf("wxpay notify: state %s", data.TradeState)
	}
	return NotifyInfo{
		OutTradeNo: data.OutTradeNo,
		TradeNo:    data.TradeNo,
		AmountFen:  data.Amount.Total,
	}, nil
}

// ---------- WeChat Pay v3 signing helpers ----------

// wxpayAuthorization builds the Authorization header value per the v3 spec:
//
//	WECHATPAY2-SHA256-RSA2048 mchid="...",nonce_str="...",timestamp="...",serial_no="...",signature="..."
func wxpayAuthorization(mchID, serialNo, nonce string, timestamp int64, signature string) string {
	return fmt.Sprintf(
		`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",timestamp="%d",serial_no="%s",signature="%s"`,
		mchID, nonce, timestamp, serialNo, signature,
	)
}

// wxpaySign computes the base64 RSA-SHA256 signature over the canonical string:
//
//	HTTP_METHOD\nURL_PATH\nTIMESTAMP\nNONCE\nBODY\n
func wxpaySign(key *rsa.PrivateKey, method, path string, timestamp int64, nonce, body string) string {
	if key == nil {
		return ""
	}
	message := fmt.Sprintf("%s\n%s\n%d\n%s\n%s\n", method, path, timestamp, nonce, body)
	hashed := sha256.Sum256([]byte(message))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, 0, hashed[:])
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(sig)
}

// wxPath extracts the URL path (+query) for signing, stripping scheme+host.
func wxPath(rawURL string) string {
	if i := strings.Index(rawURL, "://"); i >= 0 {
		rest := rawURL[i+3:]
		if j := strings.Index(rest, "/"); j >= 0 {
			return rest[j:]
		}
		return "/"
	}
	return rawURL
}

// wxpayDecrypt decrypts a WeChat Pay v3 resource ciphertext.
// Key=APIv3Key(32B), IV=nonce, AAD=associated_data, ciphertext is base64 and
// has the 16B GCM tag appended.
func wxpayDecrypt(b64Ciphertext, nonce, associatedData, apiV3Key string) ([]byte, error) {
	key := []byte(apiV3Key)
	if len(key) != 32 {
		return nil, fmt.Errorf("api v3 key must be 32 bytes, got %d", len(key))
	}
	ciphertext, err := base64.StdEncoding.DecodeString(b64Ciphertext)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("nonce must be %d bytes, got %d", gcm.NonceSize(), len(nonce))
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	return gcm.Open(nil, []byte(nonce), ciphertext, []byte(associatedData))
}

// randNonce returns a 32-char hex nonce from crypto/rand.
func randNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
