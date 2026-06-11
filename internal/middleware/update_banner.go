// update_banner.go injects "a newer release is available" state into the
// request context for the admin area, mirroring WithDefaultPasswordBanner.
// Applied only around the /manage subtree so public pages never carry it.
package middleware

import (
	"context"
	"net/http"
	"time"
)

// UpdateBannerState is the data the admin layout/dashboard need to render the
// update banner and the dashboard's version bar.
type UpdateBannerState struct {
	Available bool
	Current   string
	Latest    string
	// Enabled reports whether one-click update is configured (update_command
	// set) — the dashboard shows an update button only when true.
	Enabled bool
	// CheckedAt is the time of the last successful release check; zero if never.
	CheckedAt time.Time
}

// WithUpdateBanner stores the result of fn() in the request context. fn is
// evaluated per request so the banner reflects the latest check.
func WithUpdateBanner(fn func() UpdateBannerState) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), ctxUpdateBanner, fn())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UpdateBannerFrom returns the stored state, or a zero (Available=false) value
// when none was injected — e.g. on public pages.
func UpdateBannerFrom(ctx context.Context) UpdateBannerState {
	if v, ok := ctx.Value(ctxUpdateBanner).(UpdateBannerState); ok {
		return v
	}
	return UpdateBannerState{}
}
