package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"faka-site/internal/auth"
	"faka-site/internal/payment"
)

func randPassword() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	users, _ := s.store.ListUsers()
	s.render(w, r, "admin_users.html", ViewData{Title: "用户管理", Data: map[string]any{"users": users}})
}

func (s *Server) getCreate(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "admin_create.html", ViewData{Title: "建账户"})
}

func (s *Server) postCreate(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.PostFormValue("email"))
	pw := r.PostFormValue("password")
	if msg := validateNewPassword(pw, r.PostFormValue("confirm")); msg != "" {
		s.render(w, r, "admin_create.html", ViewData{Title: "建账户", Data: map[string]any{"error": msg, "email": email}})
		return
	}
	hash, _ := auth.HashPassword(pw)
	if _, err := s.store.CreateUser(email, hash, "user"); err != nil {
		log.Printf("create user failed: email=%s: %v", email, err)
		s.render(w, r, "admin_create.html", ViewData{Title: "建账户", Data: map[string]any{"error": "创建失败,该邮箱可能已存在", "email": email}})
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (s *Server) getBalance(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	u, _ := s.store.UserByID(id)
	s.render(w, r, "admin_balance.html", ViewData{Title: "加余额", Data: map[string]any{"target": u}})
}

func (s *Server) postBalance(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PostFormValue("id"), 10, 64)
	amount, _ := usdToQuota(r.PostFormValue("amount"))
	s.store.AddBalance(context.Background(), id, currentUser(r).UserID, amount)
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg, _ := s.config()
	s.render(w, r, "admin_config.html", ViewData{Title: "配置", Data: map[string]any{
		"cfg":  cfg,
		"keys": payment.KeyFileStatuses(),
	}})
}

func (s *Server) postConfig(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	set := func(k, v string) { s.store.SetConfig(ctx, k, v) }

	set("newapi_base_url", r.PostFormValue("newapi_base_url"))
	if v := r.PostFormValue("newapi_access_token"); v != "" {
		set("newapi_access_token", v)
	}
	set("newapi_admin_user_id", r.PostFormValue("newapi_admin_user_id"))
	set("smtp_host", r.PostFormValue("smtp_host"))
	set("smtp_port", r.PostFormValue("smtp_port"))
	set("smtp_user", r.PostFormValue("smtp_user"))
	if v := r.PostFormValue("smtp_pass"); v != "" {
		set("smtp_pass", v)
	}
	set("smtp_from", r.PostFormValue("smtp_from"))

	// 支付宝官方
	set("alipay_appid", r.PostFormValue("alipay_appid"))
	set("alipay_gateway", r.PostFormValue("alipay_gateway"))
	set("alipay_sandbox", r.PostFormValue("alipay_sandbox"))

	// 微信支付官方(密钥走文件上传,不在此保存)
	set("wxpay_appid", r.PostFormValue("wxpay_appid"))
	set("wxpay_mchid", r.PostFormValue("wxpay_mchid"))
	set("wxpay_serial_no", r.PostFormValue("wxpay_serial_no"))

	// epay 对外网关
	set("epay_order_timeout", r.PostFormValue("epay_order_timeout"))
	set("epay_admin_user", r.PostFormValue("epay_admin_user"))
	if v := r.PostFormValue("epay_admin_pass"); v != "" {
		hash, _ := auth.HashPassword(v)
		set("epay_admin_pass_hash", hash)
	}

	// 充值设置
	rate := strings.TrimSpace(r.PostFormValue("recharge_rate"))
	if rate == "" {
		rate = "500000"
	}
	set("recharge_rate", rate)
	notifyBase := strings.TrimRight(strings.TrimSpace(r.PostFormValue("recharge_notify_base")), "/")
	// recharge_notify_base is the public URL Alipay/WeChat call back into. It
	// MUST be an https URL so the callback isn't downgraded to plaintext (and so
	// the official payment APIs accept it). Reject malformed / non-https values
	// at save time rather than silently storing a value that breaks recharge.
	if notifyBase != "" && !isValidHTTPSBase(notifyBase) {
		s.render(w, r, "admin_config.html", ViewData{Title: "配置", Data: map[string]any{
			"cfg":     s.mustConfig(),
			"keys":    payment.KeyFileStatuses(),
			"cfgErr":  "公网回调基地址必须为 https 开头的完整 URL,例如 https://faka.example.com",
		}})
		return
	}
	set("recharge_notify_base", notifyBase)
	set("recharge_internal_pid", r.PostFormValue("recharge_internal_pid"))
	s.render(w, r, "admin_config.html", ViewData{Title: "配置", Data: map[string]any{
		"cfg":  s.mustConfig(),
		"keys": payment.KeyFileStatuses(),
		"msg":  "已保存",
	}})
}

func (s *Server) postConfigTest(w http.ResponseWriter, r *http.Request) {
	err := s.newapiClient().TestConnection(r.Context())
	d := map[string]any{"cfg": s.mustConfig()}
	if err != nil {
		d["testErr"] = err.Error()
	} else {
		d["testOK"] = true
	}
	s.render(w, r, "admin_config.html", ViewData{Title: "配置", Data: d})
}

func (s *Server) postUserStatus(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	status, _ := strconv.Atoi(r.PostFormValue("status"))
	s.store.SetUserStatus(id, status)
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (s *Server) getReset(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	u, _ := s.store.UserByID(id)
	s.render(w, r, "admin_reset.html", ViewData{Title: "重置密码", Data: map[string]any{"target": u}})
}

func (s *Server) postReset(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PostFormValue("id"), 10, 64)
	pw := r.PostFormValue("password")
	if msg := validateNewPassword(pw, r.PostFormValue("confirm")); msg != "" {
		u, _ := s.store.UserByID(id)
		s.render(w, r, "admin_reset.html", ViewData{Title: "重置密码", Data: map[string]any{"target": u, "error": msg}})
		return
	}
	hash, _ := auth.HashPassword(pw)
	s.store.SetUserPassword(id, hash)
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// validateNewPassword 校验新密码:长度≥6 且两次一致。返回友好错误串,合法则空串。
func validateNewPassword(pw, confirm string) string {
	if len(pw) < 6 {
		return "密码至少 6 位"
	}
	if pw != confirm {
		return "两次密码不一致"
	}
	return ""
}

// isValidHTTPSBase reports whether s is an absolute https URL with a host and
// no path/query/fragment (a base used to build callback URLs). Used to validate
// recharge_notify_base at save time: the official payment APIs require an
// https callback, and a silently-wrong value here is the root cause of
// "paid but never credited" recharge orders.
func isValidHTTPSBase(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return false
	}
	// Reject any path/query/fragment: bases are like "https://faka.example.com",
	// not "https://x/sub" or "https://x?q=1". (Trailing slash is trimmed by caller.)
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return true
}
