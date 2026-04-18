// Package middleware provides the standard HTTP middleware chain: security
// response headers, request ID, structured access logging, and panic recovery.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxDefaultPasswordBanner
)

// Default CSP compatible with CodeMirror 6 (requires inline styles).
// Refined to nonce-based in a later phase.
const defaultCSP = "default-src 'self'; " +
	"img-src 'self' data:; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"font-src 'self'; " +
	"object-src 'none'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'"

// SecurityHeaders injects the project's baseline security response headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", defaultCSP)
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// RequestID attaches a short hex id to each request, available via RequestID(ctx).
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), ctxRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFrom returns the request ID or "" if missing.
func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxRequestID).(string); ok {
		return v
	}
	return ""
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "norand"
	}
	return hex.EncodeToString(b[:])
}

// statusRecorder captures the status code for access log purposes.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

// AccessLog records method/path/status/duration via slog.
func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			logger.LogAttrs(r.Context(), slog.LevelInfo, "access",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Duration("duration", time.Since(start)),
				slog.String("request_id", RequestIDFrom(r.Context())),
			)
		})
	}
}

// PanicRecover catches handler panics, logs the stack and responds 500.
func PanicRecover(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.LogAttrs(r.Context(), slog.LevelError, "panic",
						slog.Any("panic", rec),
						slog.String("request_id", RequestIDFrom(r.Context())),
						slog.String("stack", string(debug.Stack())),
					)
					// Only write status if nothing was written yet.
					if sr, ok := w.(*statusRecorder); ok && sr.status != 0 {
						return
					}
					w.Header().Set("Content-Type", "text/plain; charset=utf-8")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = fmt.Fprint(w, "500 Internal Server Error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// WithDefaultPasswordBanner injects a flag into the request context that
// templates consult to decide whether to render the yellow warning banner.
// The flag source is the config.DefaultPasswordUnchanged() result at the time
// of request.
func WithDefaultPasswordBanner(fn func() bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), ctxDefaultPasswordBanner, fn())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// DefaultPasswordBannerFrom reports whether the banner should render.
func DefaultPasswordBannerFrom(ctx context.Context) bool {
	if v, ok := ctx.Value(ctxDefaultPasswordBanner).(bool); ok {
		return v
	}
	return false
}

// Chain composes middlewares left-to-right (first is outermost).
func Chain(mws ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(final http.Handler) http.Handler {
		h := final
		for i := len(mws) - 1; i >= 0; i-- {
			h = mws[i](h)
		}
		return h
	}
}
