package web

import (
	"context"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"faka-site/internal/auth"
	"faka-site/internal/epay"
	"faka-site/internal/mailer"
	"faka-site/internal/newapi"
	"faka-site/internal/store"
)

type siteConfig struct {
	BaseURL     string
	AccessToken string
	AdminUserID string
	SMTPHost    string
	SMTPPort    string
	SMTPUser    string
	SMTPPass    string
	SMTPFrom    string

	EpayMerchants      string // 编辑用:"pid,key" 每行一个
	EpayQRAlipay       string
	EpayQRAlipayName   string
	EpayQRWxpay        string
	EpayQRWxpayName    string
	EpaySMSSecret      string
	EpayOrderTimeout   string
	EpayAdminUser      string
	RechargeRate       string // 每 ¥ 对应 quota,默认 500000
	RechargeNotifyBase string // 充值回调基地址,如 https://faka.example.com
	RechargeInternalPID string // 自身充值用的商户 pid
}

type Server struct {
	store        *store.Store
	secret       []byte
	throttle     *auth.Throttle
	now          func() time.Time
	secureCookie bool
	mailSender   mailSender // 可注入(测试用);nil 时由配置构建
}

func NewServer(st *store.Store, secret []byte, secureCookie bool) *Server {
	initTemplates()
	return &Server{store: st, secret: secret, throttle: auth.NewThrottle(5), now: time.Now, secureCookie: secureCookie}
}

func (s *Server) config() (siteConfig, error) {
	m, err := s.store.AllConfig(context.Background())
	if err != nil {
		return siteConfig{}, err
	}
	c := siteConfig{
		BaseURL: m["newapi_base_url"], AccessToken: m["newapi_access_token"], AdminUserID: m["newapi_admin_user_id"],
		SMTPHost: m["smtp_host"], SMTPPort: m["smtp_port"], SMTPUser: m["smtp_user"], SMTPPass: m["smtp_pass"], SMTPFrom: m["smtp_from"],
		EpayMerchants:     merchantsToLines(m["epay_merchants"]),
		EpayQRAlipay:      m["epay_qrcode_alipay"],
		EpayQRAlipayName:  m["epay_qrcode_alipay_name"],
		EpayQRWxpay:       m["epay_qrcode_wxpay"],
		EpayQRWxpayName:   m["epay_qrcode_wxpay_name"],
		EpaySMSSecret:     m["epay_sms_secret"],
		EpayOrderTimeout:  m["epay_order_timeout"],
		EpayAdminUser:     m["epay_admin_user"],
		RechargeRate:      m["recharge_rate"],
		RechargeNotifyBase: m["recharge_notify_base"],
		RechargeInternalPID: m["recharge_internal_pid"],
	}
	if c.RechargeRate == "" {
		c.RechargeRate = "500000"
	}
	return c, nil
}

func (s *Server) mustConfig() siteConfig { c, _ := s.config(); return c }

func (s *Server) mailer() *mailer.Mailer {
	c, err := s.config()
	if err != nil || c.SMTPHost == "" {
		return nil
	}
	port, _ := strconv.Atoi(c.SMTPPort)
	return mailer.New(mailer.SMTPConfig{Host: c.SMTPHost, Port: port, User: c.SMTPUser, Pass: c.SMTPPass, From: c.SMTPFrom})
}

func (s *Server) newapiClient() *newapi.Client {
	c := s.mustConfig()
	return newapi.New(newapi.Config{BaseURL: c.BaseURL, AccessToken: c.AccessToken, AdminUserID: c.AdminUserID})
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))

	mux.HandleFunc("GET /login", s.getLogin)
	mux.HandleFunc("POST /login", s.postLogin)
	mux.HandleFunc("GET /forgot", s.getForgot)
	mux.HandleFunc("POST /forgot", s.postForgot)
	mux.HandleFunc("GET /logout", s.postLogout)

	authed := http.NewServeMux()
	authed.HandleFunc("GET /", s.dashboard)
	authed.HandleFunc("GET /buy", s.getBuy)
	authed.HandleFunc("POST /buy", s.postBuy)
	authed.HandleFunc("GET /orders", s.orders)
	authed.HandleFunc("GET /orders/{id}", s.orderDetail)
	authed.HandleFunc("POST /logout", s.postLogout)

	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /users", s.adminUsers)
	adminMux.HandleFunc("GET /create", s.getCreate)
	adminMux.HandleFunc("POST /create", s.postCreate)
	adminMux.HandleFunc("GET /balance", s.getBalance)
	adminMux.HandleFunc("POST /balance", s.postBalance)
	adminMux.HandleFunc("GET /config", s.getConfig)
	adminMux.HandleFunc("POST /config", s.postConfig)
	adminMux.HandleFunc("POST /config/test", s.postConfigTest)
	adminMux.HandleFunc("POST /users/{id}/status", s.postUserStatus)
	adminMux.HandleFunc("GET /reset", s.getReset)
	adminMux.HandleFunc("POST /reset", s.postReset)

	mux.Handle("/", s.loadSession(s.csrfCheck(s.requireLogin(authed))))
	mux.Handle("/admin/", s.loadSession(s.csrfCheck(s.requireAdmin(http.StripPrefix("/admin", adminMux)))))

	// epay-gateway: protocol endpoints at root (what external merchants call),
	// management under /epay/ to avoid colliding with faka-site's /admin/.
	eh := epay.New(s.store, s.epayConfig)
	mux.HandleFunc("/mapi.php", eh.Mapi)
	mux.HandleFunc("/submit.php", eh.Submit)
	mux.HandleFunc("/api.php", eh.API)
	mux.HandleFunc("/sms/notify", eh.SmsNotify)
	mux.HandleFunc("/epay/confirm", eh.WithAdminAuth(eh.Confirm))
	mux.HandleFunc("/epay/admin", eh.WithAdminAuth(eh.Admin))
	mux.HandleFunc("/epay/admin/", eh.WithAdminAuth(eh.Admin))
	return s.securityHeaders(mux)
}
