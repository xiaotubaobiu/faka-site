package web

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"faka-site/internal/epay"
	"faka-site/internal/payment"
)

// paymentConfigKeys are the config KV keys that influence provider
// construction. Changing any of them (or re-uploading a key file) must refresh
// the registry; changing anything else must NOT, so unrelated admin edits don't
// disturb in-flight payments.
var paymentConfigKeys = []string{
	"alipay_appid", "alipay_gateway", "alipay_sandbox",
	"wxpay_appid", "wxpay_mchid", "wxpay_serial_no",
}

// epayConfig builds an epay.Config from the faka-site config KV store.
// It is called on each request (passed as a provider to epay.New) so admin
// edits take effect without restart. Missing/empty keys fall back to defaults.
//
// Payment channel credentials are decrypted/loaded here and used to refresh the
// payment.Registry ONLY when the inputs have actually changed since the last
// call. This makes the per-request invocation cheap (a config read + a hash
// compare) and eliminates the race where a concurrent rebuild wiped the global
// provider table mid-precreate.
func (s *Server) epayConfig() epay.Config {
	m, _ := s.store.AllConfig(context.Background())

	cfg := epay.Config{
		NotifyBase:   m["recharge_notify_base"], // official callbacks share this base
		OrderTimeout: 0,                         // normalize fills default 5
		Admin: epay.Admin{
			Username:     m["epay_admin_user"],
			PasswordHash: m["epay_admin_pass_hash"],
		},
	}

	// Merchants: JSON array [{"pid":1001,"key":"..."}]. Tolerate empty/missing.
	if raw := m["epay_merchants"]; raw != "" {
		var ms []epay.Merchant
		if err := json.Unmarshal([]byte(raw), &ms); err == nil {
			cfg.Merchants = ms
		}
	}

	// Order timeout: int minutes, default 5.
	if raw := m["epay_order_timeout"]; raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			cfg.OrderTimeout = n
		}
	}

	cfg.Normalize()

	// Refresh the registry only if the credential inputs changed. Reading the
	// key files here is cheap (a few KB) and happens at most once per real
	// config edit rather than every request.
	s.refreshPaymentRegistry(m)

	return cfg
}

// refreshPaymentRegistry rebuilds the process-wide payment provider table only
// when the fingerprint of (relevant config keys + key-file contents) differs
// from the last build. Repeated calls with identical inputs are a no-op, which
// is what removes the per-request race that previously caused sporadic
// "未配置" errors during concurrent precreate + polling.
//
// Sensitive material (private keys, APIv3 key) is read from files in the key
// directory rather than the database, so it never appears in config tables or
// admin textareas. Non-sensitive IDs (app id, mch id, serial no) stay in the
// KV store since they're just public identifiers.
func (s *Server) refreshPaymentRegistry(m map[string]string) {
	keyFiles := map[string]string{}
	for _, spec := range payment.KeyFileSpecs {
		if content, _ := payment.ReadKeyFile(spec.Name); content != "" {
			keyFiles[spec.Name] = content
		}
	}
	fp := payment.ConfigFingerprint(m, keyFiles)
	payment.DefaultRegistry().SetProvidersIfChanged(fp, func() []payment.PaymentProvider {
		return []payment.PaymentProvider{
			buildAlipayProvider(m, keyFiles),
			buildWxpayProvider(m, keyFiles),
		}
	})
}

// buildAlipayProvider constructs the alipay provider from config + key files.
// Extracted so the registry rebuild closure stays readable.
func buildAlipayProvider(m map[string]string, keyFiles map[string]string) *payment.AlipayProvider {
	alipayCfg := payment.AlipayConfig{
		AppID:      m["alipay_appid"],
		PrivateKey: keyFiles["alipay_private.pem"],
		PublicKey:  keyFiles["alipay_public.pem"],
		Gateway:    m["alipay_gateway"],
		SignType:   "RSA2",
	}
	if sandbox := m["alipay_sandbox"]; sandbox == "1" || sandbox == "true" {
		alipayCfg.Gateway = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"
	}
	return payment.NewAlipayProvider(alipayCfg)
}

// buildWxpayProvider constructs the wxpay provider from config + key files.
func buildWxpayProvider(m map[string]string, keyFiles map[string]string) *payment.WxpayProvider {
	wxpayCfg := payment.WxpayConfig{
		AppID:       m["wxpay_appid"],
		MchID:       m["wxpay_mchid"],
		MchSerialNo: m["wxpay_serial_no"],
		APIv3Key:    keyFiles["wxpay_apiv3.key"],
		PrivateKey:  keyFiles["wxpay_private.pem"],
	}
	return payment.NewWxpayProvider(wxpayCfg)
}

// decryptOrPassthrough is retained for reading legacy encrypted config values
// during migration, but is no longer used for payment keys (those come from
// files now).
func decryptOrPassthrough(stored string) string {
	if stored == "" {
		return ""
	}
	plain, err := payment.Open(stored)
	if err != nil {
		return stored
	}
	return plain
}

// merchantsToLines converts the stored epay_merchants JSON array
// ([{"pid":N,"key":"K"}]) into newline-separated "pid,key" lines for
// editing. Empty/invalid JSON yields an empty string.
func merchantsToLines(jsonStr string) string {
	if jsonStr == "" {
		return ""
	}
	var ms []struct {
		PID int    `json:"pid"`
		Key string `json:"key"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &ms); err != nil {
		return ""
	}
	var b strings.Builder
	for _, m := range ms {
		b.WriteString(strconv.Itoa(m.PID))
		b.WriteByte(',')
		b.WriteString(m.Key)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// linesToMerchants converts newline-separated "pid,key" lines back into the
// stored epay_merchants JSON array string. Blank/malformed lines are skipped.
// Returns "" (empty, not "[]") when no valid merchants are present.
func linesToMerchants(lines string) string {
	var ms []struct {
		PID int    `json:"pid"`
		Key string `json:"key"`
	}
	for _, line := range strings.Split(lines, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ",", 2)
		if len(parts) != 2 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			continue
		}
		key := strings.TrimSpace(parts[1])
		ms = append(ms, struct {
			PID int    `json:"pid"`
			Key string `json:"key"`
		}{PID: pid, Key: key})
	}
	if len(ms) == 0 {
		return ""
	}
	out, _ := json.Marshal(ms)
	return string(out)
}
