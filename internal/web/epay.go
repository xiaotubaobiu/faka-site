package web

import (
	"context"
	"encoding/json"
	"strconv"

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
