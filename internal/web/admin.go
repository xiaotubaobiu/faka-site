package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"strconv"
	"strings"

	"faka-site/internal/auth"
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
	s.render(w, r, "admin_config.html", ViewData{Title: "配置", Data: map[string]any{"cfg": cfg}})
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
	set("epay_qrcode_alipay", r.PostFormValue("epay_qrcode_alipay"))
	set("epay_qrcode_alipay_name", r.PostFormValue("epay_qrcode_alipay_name"))
	set("epay_qrcode_wxpay", r.PostFormValue("epay_qrcode_wxpay"))
	set("epay_qrcode_wxpay_name", r.PostFormValue("epay_qrcode_wxpay_name"))
	set("epay_sms_secret", r.PostFormValue("epay_sms_secret"))
	set("epay_order_timeout", r.PostFormValue("epay_order_timeout"))
	set("epay_admin_user", r.PostFormValue("epay_admin_user"))
	if v := r.PostFormValue("epay_admin_pass"); v != "" {
		hash, _ := auth.HashPassword(v)
		set("epay_admin_pass_hash", hash)
	}
	set("epay_merchants", linesToMerchants(r.PostFormValue("epay_merchants")))
	rate := strings.TrimSpace(r.PostFormValue("recharge_rate"))
	if rate == "" {
		rate = "500000"
	}
	set("recharge_rate", rate)
	set("recharge_notify_base", r.PostFormValue("recharge_notify_base"))
	set("recharge_internal_pid", r.PostFormValue("recharge_internal_pid"))
	s.render(w, r, "admin_config.html", ViewData{Title: "配置", Data: map[string]any{"cfg": s.mustConfig(), "msg": "已保存"}})
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
