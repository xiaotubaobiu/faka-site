package epay

import (
	"net/url"
	"testing"
)

func TestSign(t *testing.T) {
	params := url.Values{}
	params.Set("pid", "1001")
	params.Set("type", "alipay")
	params.Set("out_trade_no", "20240101000001")
	params.Set("notify_url", "https://example.com/notify")
	params.Set("return_url", "https://example.com/return")
	params.Set("name", "VIP")
	params.Set("money", "1.00")

	key := "test_secret_key"
	got := Sign(params, key)
	want := "24ca12b8ffa9fc9b12b137d16e4584ec"
	if got != want {
		t.Errorf("Sign() = %s, want %s", got, want)
	}
}

func TestVerify(t *testing.T) {
	params := url.Values{}
	params.Set("pid", "1001")
	params.Set("type", "alipay")
	params.Set("money", "1.00")
	params.Set("name", "VIP")
	params.Set("sign", "should_be_ignored")
	params.Set("sign_type", "MD5")

	key := "test_secret_key"
	signature := Sign(params, key)
	params.Set("sign", signature)

	if !Verify(params, key) {
		t.Error("Verify() = false, want true for valid signature")
	}

	params.Set("money", "999.00")
	if Verify(params, key) {
		t.Error("Verify() = true, want false for tampered params")
	}
}

func TestSign_IgnoresEmptyValues(t *testing.T) {
	params := url.Values{}
	params.Set("pid", "1001")
	params.Set("name", "")
	params.Set("money", "1.00")

	key := "testkey"
	got := Sign(params, key)

	params2 := url.Values{}
	params2.Set("pid", "1001")
	params2.Set("money", "1.00")
	want := Sign(params2, key)

	if got != want {
		t.Errorf("Sign with empty values = %s, want %s", got, want)
	}
}
