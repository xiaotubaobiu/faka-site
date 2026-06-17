package epay

import "strings"

// Merchant is an upstream merchant (pid) allowed to call the gateway.
type Merchant struct {
	PID int    `json:"pid"`
	Key string `json:"key"`
}

// QRCode is a static pay-code (URL) shown to the buyer for a given channel.
type QRCode struct {
	URL  string
	Name string
}

// Admin holds Basic-Auth credentials for the management endpoints.
type Admin struct {
	Username     string
	PasswordHash string
}

// Config is the gateway configuration, loaded from the faka-site KV store
// (no yaml). It is rebuilt on each request via a provider so admin edits take
// effect without restart.
type Config struct {
	QRCodes      map[string]QRCode // keys "alipay","wxpay"
	SMSSecret    string
	OrderTimeout int // minutes
	Merchants    []Merchant
	Admin        Admin
}

func (c *Config) FindMerchant(pid int) string {
	for _, m := range c.Merchants {
		if m.PID == pid {
			return m.Key
		}
	}
	return ""
}

func (c *Config) PrimaryMerchant() Merchant {
	if len(c.Merchants) == 0 {
		return Merchant{}
	}
	return c.Merchants[0]
}

func (c *Config) HasAdminAuth() bool {
	return c.Admin.Username != "" && c.Admin.PasswordHash != ""
}

// normalize applies defaults: OrderTimeout, and ensures the two known QR-code
// channels exist (even with empty URLs) with friendly display names.
func (c *Config) normalize() {
	if c.OrderTimeout == 0 {
		c.OrderTimeout = 5
	}
	if c.QRCodes == nil {
		c.QRCodes = map[string]QRCode{}
	}
	c.ensureQRCode("alipay", "支付宝")
	c.ensureQRCode("wxpay", "微信支付")
}

func (c *Config) ensureQRCode(code string, defaultName string) {
	qr := c.QRCodes[code]
	qr.URL = strings.TrimSpace(qr.URL)
	if qr.Name == "" {
		qr.Name = defaultName
	}
	c.QRCodes[code] = qr
}

// Normalize is exported so the KV-loader can finalize a freshly-built Config.
func (c *Config) Normalize() { c.normalize() }
