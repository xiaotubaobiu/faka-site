package web

import (
	"net/http"
	"strings"

	"faka-site/internal/auth"
)

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "login.html", ViewData{Title: "登录"})
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.PostFormValue("email"))
	ip := clientIP(r)
	if !s.throttle.Allow(ip + "|" + email) {
		s.render(w, r, "login.html", ViewData{Title: "登录", Data: map[string]any{"error": "尝试过多,稍后再试"}})
		return
	}
	u, err := s.store.UserByEmail(email)
	if err != nil || u.Status != 1 || !auth.VerifyPassword(u.PasswordHash, r.PostFormValue("password")) {
		s.render(w, r, "login.html", ViewData{Title: "登录", Data: map[string]any{"error": "邮箱或密码错误"}})
		return
	}
	sess, _ := auth.NewSession(u.ID, u.Role, s.now().Unix())
	cookie, _ := auth.EncodeSession(sess, s.secret)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: cookie, Path: "/", HttpOnly: true,
		Secure: s.secureCookie, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) postLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// mailSender 抽象发信,便于 /forgot 注入假实现做单测。
type mailSender interface {
	Send(to, subject, body string) error
}

// smtpMailer 返回注入的 mailSender;未注入时由站点配置构建(未配 SMTP 则 nil)。
func (s *Server) smtpMailer() mailSender {
	if s.mailSender != nil {
		return s.mailSender
	}
	// 注意:不能直接 `return s.mailer()` —— mailer() 返回 *mailer.Mailer,
	// 若为 nil 具体指针,包成 mailSender 接口后会变成「非 nil 接口」,
	// 导致调用方 `ms == nil` 失效、ms.Send 触发空指针 panic。必须显式返回 nil 接口。
	if m := s.mailer(); m != nil {
		return m
	}
	return nil
}

func (s *Server) getForgot(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "forgot.html", ViewData{Title: "忘记密码"})
}

func (s *Server) postForgot(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.PostFormValue("email"))
	ip := clientIP(r)
	if !s.throttle.Allow(ip + "|" + email) {
		s.render(w, r, "forgot.html", ViewData{Title: "忘记密码", Data: map[string]any{"error": "尝试过多,稍后再试"}})
		return
	}
	ms := s.smtpMailer()
	if ms == nil {
		s.render(w, r, "forgot.html", ViewData{Title: "忘记密码", Data: map[string]any{"msg": "SMTP 未配置,请联系管理员"}})
		return
	}
	if u, err := s.store.UserByEmail(email); err == nil {
		pw := randPassword()
		hash, _ := auth.HashPassword(pw)
		s.store.SetUserPassword(u.ID, hash)
		_ = ms.Send(u.Email, "发卡站密码重置", "你的新密码:"+pw)
	}
	// 统一文案,防止用户枚举
	s.render(w, r, "forgot.html", ViewData{Title: "忘记密码", Data: map[string]any{"msg": "如该邮箱已注册,新密码已发送至该邮箱"}})
}
