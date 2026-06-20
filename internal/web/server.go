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

	// 支付宝官方(当面付)— 仅存非敏感标识,密钥走文件
	AlipayAppID   string
	AlipayGateway string
	AlipaySandbox string

	// 微信支付官方(v3 Native)— 仅存非敏感标识,密钥走文件
	WxpayAppID       string
	WxpayMchID       string
	WxpayMchSerialNo string

	// epay 对外网关
	EpayOrderTimeout string
	EpayAdminUser    string

	// 充值设置
	RechargeRate        string // 每 ¥ 对应 quota,默认 500000
	RechargeNotifyBase  string // 公网回调基地址(官方支付异步通知,需 https)
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

		AlipayAppID:   m["alipay_appid"],
		AlipayGateway: m["alipay_gateway"],
		AlipaySandbox: m["alipay_sandbox"],

		WxpayAppID:       m["wxpay_appid"],
		WxpayMchID:       m["wxpay_mchid"],
		WxpayMchSerialNo: m["wxpay_serial_no"],

		EpayOrderTimeout: m["epay_order_timeout"],
		EpayAdminUser:    m["epay_admin_user"],

		RechargeRate:        m["recharge_rate"],
		RechargeNotifyBase:  m["recharge_notify_base"],
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
	// PUBLIC: epay gateway calls this to confirm a recharge payment.
	mux.HandleFunc("GET /recharge/notify", s.rechargeNotify)

	authed := http.NewServeMux()
	authed.HandleFunc("GET /", s.dashboard)
	authed.HandleFunc("GET /buy", s.getBuy)
	authed.HandleFunc("POST /buy", s.postBuy)
	authed.HandleFunc("GET /orders", s.orders)
	authed.HandleFunc("GET /orders/{id}", s.orderDetail)
	authed.HandleFunc("GET /recharge", s.getRecharge)
	authed.HandleFunc("POST /recharge", s.postRecharge)
	authed.HandleFunc("GET /recharge/pay/{id}", s.rechargePay)
	authed.HandleFunc("GET /recharge/qr/{id}", s.rechargeQR)
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
	adminMux.HandleFunc("POST /config/key-upload", s.postKeyUpload)
	adminMux.HandleFunc("POST /config/key-delete", s.postKeyDelete)
	adminMux.HandleFunc("GET /merchants", s.getMerchants)
	adminMux.HandleFunc("POST /merchants/add", s.postMerchantAdd)
	adminMux.HandleFunc("POST /merchants/delete", s.postMerchantDelete)
	adminMux.HandleFunc("POST /merchants/reset-key", s.postMerchantResetKey)
	adminMux.HandleFunc("GET /docs", s.getAdminDocs)
	adminMux.HandleFunc("POST /users/{id}/status", s.postUserStatus)
	adminMux.HandleFunc("GET /reset", s.getReset)
	adminMux.HandleFunc("POST /reset", s.postReset)

	mux.Handle("/", s.loadSession(s.csrfCheck(s.requireLogin(authed))))
	mux.Handle("/admin/", s.loadSession(s.csrfCheck(s.requireAdmin(http.StripPrefix("/admin", adminMux)))))

	// epay-gateway: protocol endpoints at root (what external merchants call),
	// management under /epay/ to avoid colliding with faka-site's /admin/.
	// /notify/{alipay,wxpay} are the PUBLIC official payment callbacks.
	eh := epay.New(s.store, s.epayConfig)
	mux.HandleFunc("/mapi.php", eh.Mapi)
	mux.HandleFunc("/submit.php", eh.Submit)
	mux.HandleFunc("/api.php", eh.API)
	mux.HandleFunc("/notify/alipay", eh.OfficialNotify("alipay"))
	mux.HandleFunc("/notify/wxpay", eh.OfficialNotify("wxpay"))
	mux.HandleFunc("/epay/admin", eh.WithAdminAuth(eh.Admin))
	mux.HandleFunc("/epay/admin/", eh.WithAdminAuth(eh.Admin))
	return s.securityHeaders(mux)
}
