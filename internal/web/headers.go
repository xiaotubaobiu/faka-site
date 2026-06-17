package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type nonceCtxKey struct{}

// withNonce 把 nonce 放入 request context,供 render 注入模板。
func withNonce(r *http.Request, nonce string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), nonceCtxKey{}, nonce))
}

// nonceFromContext 取出当前请求的 CSP nonce;若无则空串。
func nonceFromContext(r *http.Request) string {
	if v, ok := r.Context().Value(nonceCtxKey{}).(string); ok {
		return v
	}
	return ""
}

// securityHeaders 为每个响应注入安全头;CSP 的 script-src 用 per-request nonce,
// 内联脚本(移动端抽屉开关)经 layout 的 <script nonce> 放行,其余内联脚本被拦。
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		nonce := hex.EncodeToString(buf)
		csp := "default-src 'self'; script-src 'self' 'nonce-" + nonce + "'; " +
			"style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'"
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, withNonce(r, nonce))
	})
}
