package epay

// Merchant is an upstream merchant (pid) allowed to call the gateway.
type Merchant struct {
	PID int    `json:"pid"`
	Key string `json:"key"`
}

// Admin holds Basic-Auth credentials for the management endpoints.
type Admin struct {
	Username     string
	PasswordHash string
}

// Config is the gateway configuration, loaded from the faka-site KV store
// (no yaml). It is rebuilt on each request via a provider so admin edits take
// effect without restart.
//
// Payment channel credentials (alipay appid/key, wxpay mchid/v3key) are NOT
// stored here — they live in the payment.Registry, built by the web layer.
// This Config only carries the epay-protocol-level settings.
type Config struct {
	NotifyBase   string // 公网回调基地址,官方支付异步通知用(必须 https)
	OrderTimeout int    // minutes
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

// normalize applies defaults (OrderTimeout) so callers never see a zero.
func (c *Config) normalize() {
	if c.OrderTimeout == 0 {
		c.OrderTimeout = 5
	}
}

// Normalize is exported so the KV-loader can finalize a freshly-built Config.
func (c *Config) Normalize() { c.normalize() }
