package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
		s.render(w, r, "admin_create.html", ViewData{Title: "建账户", Data: map[string]any{"error": err.Error()}})
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
	amount, _ := strconv.ParseInt(r.PostFormValue("amount"), 10, 64)
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
