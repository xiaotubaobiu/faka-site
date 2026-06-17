package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"strconv"

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
	email := r.PostFormValue("email")
	pw := randPassword()
	hash, _ := auth.HashPassword(pw)
	if _, err := s.store.CreateUser(email, hash, "user"); err != nil {
		log.Printf("create user failed: email=%s: %v", email, err)
		s.render(w, r, "admin_create.html", ViewData{Title: "建账户", Data: map[string]any{"error": "创建失败,该邮箱可能已存在"}})
		return
	}
	var mailErr string
	if m := s.mailer(); m != nil {
		if err := m.Send(email, "你的发卡站账户", "邮箱:"+email+"\n初始密码:"+pw+"\n请登录后修改。"); err != nil {
			mailErr = err.Error()
		}
	} else {
		mailErr = "SMTP 未配置"
	}
	s.render(w, r, "admin_create.html", ViewData{Title: "建账户", Data: map[string]any{
		"newEmail": email, "newPassword": pw, "mailErr": mailErr,
	}})
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

func (s *Server) postUserReset(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	u, err := s.store.UserByID(id)
	if err != nil {
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	pw := randPassword()
	hash, _ := auth.HashPassword(pw)
	s.store.SetUserPassword(id, hash)
	var mailErr string
	if m := s.mailer(); m != nil {
		if err := m.Send(u.Email, "发卡站密码已重置", "邮箱:"+u.Email+"\n新密码:"+pw); err != nil {
			mailErr = err.Error()
		}
	} else {
		mailErr = "SMTP 未配置"
	}
	users, _ := s.store.ListUsers()
	s.render(w, r, "admin_users.html", ViewData{Title: "用户管理", Data: map[string]any{
		"users": users, "resetEmail": u.Email, "resetPassword": pw, "mailErr": mailErr,
	}})
}
