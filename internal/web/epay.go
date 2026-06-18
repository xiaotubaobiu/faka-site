package web

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"faka-site/internal/epay"
	"faka-site/internal/payment"
)

// epayConfig builds an epay.Config from the faka-site config KV store.
// It is called on each request (passed as a provider to epay.New) so admin
// edits take effect without restart. Missing/empty keys fall back to defaults.
//
// Payment channel credentials are decrypted here and used to (re)build the
// payment.Registry whenever the config is read for the first time in a process;
// the registry itself is then consulted fresh on each request by handlers.
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

	// Rebuild the payment registry from current credentials. This is cheap
	// (parsing a couple of PEM keys) and keeps handlers in sync with admin
	// edits without a restart.
	s.rebuildPaymentRegistry(m)

	return cfg
}

// rebuildPaymentRegistry constructs alipay/wxpay providers from the config map
// (non-sensitive fields) plus key FILES (sensitive PEMs/APIv3 key) and installs
// them into the process-wide registry.
//
// Sensitive material (private keys, APIv3 key) is read from files in the key
// directory rather than the database, so it never appears in config tables or
// admin textareas. Non-sensitive IDs (app id, mch id, serial no) stay in the
// KV store since they're just public identifiers.
func (s *Server) rebuildPaymentRegistry(m map[string]string) {
	alipayPriv, _ := payment.ReadKeyFile("alipay_private.pem")
	alipayPub, _ := payment.ReadKeyFile("alipay_public.pem")
	alipayCfg := payment.AlipayConfig{
		AppID:      m["alipay_appid"],
		PrivateKey: alipayPriv,
		PublicKey:  alipayPub,
		Gateway:    m["alipay_gateway"],
		SignType:   "RSA2",
	}
	if sandbox := m["alipay_sandbox"]; sandbox == "1" || sandbox == "true" {
		alipayCfg.Gateway = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"
	}

	wxpayPriv, _ := payment.ReadKeyFile("wxpay_private.pem")
	wxpayAPIv3, _ := payment.ReadKeyFile("wxpay_apiv3.key")
	wxpayCfg := payment.WxpayConfig{
		AppID:       m["wxpay_appid"],
		MchID:       m["wxpay_mchid"],
		MchSerialNo: m["wxpay_serial_no"],
		APIv3Key:    wxpayAPIv3,
		PrivateKey:  wxpayPriv,
	}

	payment.DefaultRegistry().SetProviders(
		payment.NewAlipayProvider(alipayCfg),
		payment.NewWxpayProvider(wxpayCfg),
	)
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
