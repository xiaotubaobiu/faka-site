package web

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"faka-site/internal/epay"
)

// epayConfig builds an epay.Config from the faka-site config KV store.
// It is called on each request (passed as a provider to epay.New) so admin
// edits take effect without restart. Missing/empty keys fall back to defaults.
func (s *Server) epayConfig() epay.Config {
	m, _ := s.store.AllConfig(context.Background())

	cfg := epay.Config{
		QRCodes:      map[string]epay.QRCode{},
		SMSSecret:    m["epay_sms_secret"],
		OrderTimeout: 0, // normalize fills default 5
		Admin: epay.Admin{
			Username:     m["epay_admin_user"],
			PasswordHash: m["epay_admin_pass_hash"],
		},
	}

	// QR codes — name optional, defaults applied by normalize.
	wx := epay.QRCode{URL: m["epay_qrcode_wxpay"], Name: m["epay_qrcode_wxpay_name"]}
	ali := epay.QRCode{URL: m["epay_qrcode_alipay"], Name: m["epay_qrcode_alipay_name"]}
	cfg.QRCodes["wxpay"] = wx
	cfg.QRCodes["alipay"] = ali

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
	return cfg
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
