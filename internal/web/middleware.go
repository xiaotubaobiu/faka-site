package web

import (
	"context"
	"net"
	"net/http"
	"strings"

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
		// PostFormValue handles both urlencoded and multipart bodies correctly
		// (it calls ParseMultipartForm when needed). We must NOT call ParseForm
		// first, since that consumes the urlencoded body and breaks multipart.
		if r.PostFormValue("csrf") != sess.CSRF {
			http.Error(w, "csrf check failed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP 返回可信客户端 IP。仅当直连来自回环(经本机 Caddy 反代)时才信任
// X-Forwarded-For 的首段;否则用 RemoteAddr,防止伪造 XFF 绕过限流。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if isLoopback(host) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
		}
	}
	return host
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
