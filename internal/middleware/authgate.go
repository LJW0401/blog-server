package middleware

import (
	"net/http"
	"net/url"

	"github.com/penguin/blog-server/internal/auth"
)

// AuthGate redirects unauthenticated requests to /manage/login?next=<current>
// and lets authenticated requests through. Intended to wrap /manage/* routes
// (other than /manage/login itself).
func AuthGate(store *auth.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := store.ParseSession(r); ok {
				next.ServeHTTP(w, r)
				return
			}
			target := "/manage/login?next=" + url.QueryEscape(r.URL.Path)
			http.Redirect(w, r, target, http.StatusSeeOther)
		})
	}
}
