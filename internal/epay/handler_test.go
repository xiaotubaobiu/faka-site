package epay

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"faka-site/internal/store"
)

func testConfig() Config {
	return Config{
		QRCodes: map[string]QRCode{
			"alipay": {URL: "https://qr.alipay.com/test", Name: "支付宝"},
			"wxpay":  {URL: "", Name: "微信支付"},
		},
		SMSSecret:    "",
		OrderTimeout: 5,
		Merchants:    []Merchant{{PID: 1001, Key: "testkey123"}},
	}
}

func newTestHandler(t *testing.T) (*Handler, *store.Store) {
	t.Helper()
	s, err := store.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return New(s, testConfig), s
}

func TestHandler_Mapi_SignedCreate(t *testing.T) {
	h, st := newTestHandler(t)

	out := "OUT-" + t.Name()
	params := url.Values{}
	params.Set("pid", "1001")
	params.Set("out_trade_no", out)
	params.Set("notify_url", "https://merchant.example/notify")
	params.Set("name", "测试商品")
	params.Set("money", "1.50")
	params.Set("type", "alipay")
	params.Set("sign", Sign(params, "testkey123"))

	form := params.Encode()
	req := httptest.NewRequest(http.MethodPost, "/mapi.php", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.Mapi(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"code":1`) {
		t.Fatalf("expected code:1, got %s", body)
	}
	if !strings.Contains(body, `"trade_no":"EP`) {
		t.Fatalf("expected trade_no in response, got %s", body)
	}
	if !strings.Contains(body, `"qrcode":"https://qr.alipay.com/test"`) {
		t.Fatalf("expected qrcode in response, got %s", body)
	}

	// order persisted and retrievable by out_trade_no
	got, err := st.EpayGetByOutTradeNo(1001, out)
	if err != nil || got == nil {
		t.Fatalf("order not found after Mapi: %v", err)
	}
	if got.Money != "1.50" || got.Name != "测试商品" || got.Status != 0 {
		t.Fatalf("unexpected stored order: %+v", got)
	}
}

func TestHandler_Mapi_BadSign(t *testing.T) {
	h, _ := newTestHandler(t)

	params := url.Values{}
	params.Set("pid", "1001")
	params.Set("out_trade_no", "OUT-BAD")
	params.Set("notify_url", "https://merchant.example/notify")
	params.Set("name", "x")
	params.Set("money", "1.00")
	params.Set("sign", "deadbeef") // wrong signature

	req := httptest.NewRequest(http.MethodPost, "/mapi.php", strings.NewReader(params.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.Mapi(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on bad sign, got %d body=%s", rec.Code, rec.Body.String())
	}
}
