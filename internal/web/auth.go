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
