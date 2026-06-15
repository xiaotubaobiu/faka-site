package web

import (
	"context"
	"net/http"

	"faka-site/internal/auth"
)

type ctxKey string

const ctxSessionKey ctxKey = "session"
const sessionCookie = "faka_session"

func (s *Server) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil {
			if sess, err := auth.DecodeSession(c.Value, s.secret, s.now().Unix()); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), ctxSessionKey, sess))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(ctxSessionKey) == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, _ := r.Context().Value(ctxSessionKey).(*auth.Session)
		if sess == nil || sess.Role != "admin" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) csrfCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		sess, _ := r.Context().Value(ctxSessionKey).(*auth.Session)
		if sess == nil {
			next.ServeHTTP(w, r) // pre-auth POST (login) not routed here anyway
			return
		}
		if err := r.ParseForm(); err != nil || r.PostFormValue("csrf") != sess.CSRF {
			http.Error(w, "csrf check failed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
