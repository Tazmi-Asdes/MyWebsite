package app

import (
	"net/http"
	"strings"
)

const contentSecurityPolicy = "object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		if r.URL != nil && (r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/")) {
			header.Set("Cache-Control", "no-store")
		}
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		header.Set("Content-Security-Policy", contentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}
