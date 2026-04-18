package middleware_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/penguin/blog-server/internal/middleware"
)

// Helper: build a chain around a handler, returning a fresh test server.
func newServer(t *testing.T, banner bool, handler http.Handler) (*httptest.Server, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	chain := middleware.Chain(
		middleware.PanicRecover(logger),
		middleware.RequestID,
		middleware.AccessLog(logger),
		middleware.SecurityHeaders,
		middleware.WithDefaultPasswordBanner(func() bool { return banner }),
	)
	return httptest.NewServer(chain(handler)), buf
}

// Smoke: GET through chain returns 200 + all required headers + request id.
func TestChain_Smoke_HeadersAndRequestID(t *testing.T) {
	srv, _ := newServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !middleware.DefaultPasswordBannerFrom(r.Context()) {
			_, _ = io.WriteString(w, "ok")
		}
	}))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Errorf("status: %d", resp.StatusCode)
	}
	wantHeaders := []string{
		"Content-Security-Policy",
		"Strict-Transport-Security",
		"X-Content-Type-Options",
		"X-Frame-Options",
		"Referrer-Policy",
		"X-Request-ID",
	}
	for _, h := range wantHeaders {
		if resp.Header.Get(h) == "" {
			t.Errorf("missing header: %s", h)
		}
	}
	// Request ID is 16 hex chars.
	if len(resp.Header.Get("X-Request-ID")) != 16 {
		t.Errorf("unexpected request id: %q", resp.Header.Get("X-Request-ID"))
	}
}

// Smoke: banner flag propagates into context.
func TestChain_Smoke_BannerFlagPropagates(t *testing.T) {
	for _, want := range []bool{true, false} {
		want := want
		srv, _ := newServer(t, want, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if middleware.DefaultPasswordBannerFrom(r.Context()) {
				_, _ = io.WriteString(w, "banner")
			} else {
				_, _ = io.WriteString(w, "no-banner")
			}
		}))
		t.Cleanup(srv.Close)
		resp, err := http.Get(srv.URL + "/")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if want && string(b) != "banner" {
			t.Errorf("expected banner, got %q", b)
		}
		if !want && string(b) != "no-banner" {
			t.Errorf("expected no-banner, got %q", b)
		}
	}
}

// --- Edge (WI-1.11) ---------------------------------------------------------

func TestChain_Edge_PanicRecovered(t *testing.T) {
	cases := map[string]func(){
		"panic string": func() { panic("boom") },
		"panic error":  func() { panic(&customErr{"e"}) },
		"panic nil":    func() { panic(nil) },
		"panic int":    func() { panic(42) },
	}
	for name, trigger := range cases {
		trigger := trigger
		t.Run(name, func(t *testing.T) {
			srv, buf := newServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				trigger()
			}))
			t.Cleanup(srv.Close)
			resp, err := http.Get(srv.URL + "/")
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusInternalServerError {
				t.Errorf("status: %d want 500", resp.StatusCode)
			}
			// Response still has security headers.
			if resp.Header.Get("Content-Security-Policy") == "" {
				t.Error("CSP missing on 500")
			}
			// Log must include the panic event.
			if !strings.Contains(buf.String(), `"msg":"panic"`) {
				t.Errorf("panic not logged: %s", buf.String())
			}
		})
	}
}

type customErr struct{ s string }

func (e *customErr) Error() string { return e.s }

func TestChain_Edge_UnknownRouteStill404WithHeaders(t *testing.T) {
	srv, _ := newServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/nope")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 404 {
		t.Errorf("status %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Security-Policy") == "" {
		t.Error("CSP missing on 404")
	}
}

func TestChain_Edge_LongPathLoggedAndHeadersPresent(t *testing.T) {
	long := strings.Repeat("a", 2048)
	srv, buf := newServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/" + long)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if !strings.Contains(buf.String(), `"method":"GET"`) {
		t.Errorf("access log missing")
	}
}

// --- Gzip middleware (WI-7.9) ----------------------------------------------

func TestGzip_Smoke_CompressesHTML(t *testing.T) {
	payload := strings.Repeat("hello ", 200)
	srv := httptest.NewServer(middleware.Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, payload)
	})))
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ce := resp.Header.Get("Content-Encoding"); ce != "gzip" {
		t.Errorf("Content-Encoding: %q", ce)
	}
	if v := resp.Header.Get("Vary"); !strings.Contains(v, "Accept-Encoding") {
		t.Errorf("Vary missing Accept-Encoding: %q", v)
	}
	// Body smaller than original (compression actually applied).
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer func() { _ = gz.Close() }()
	body, _ := io.ReadAll(gz)
	if string(body) != payload {
		t.Errorf("decoded body mismatch")
	}
}

func TestGzip_Edge_NoAcceptEncodingSkipped(t *testing.T) {
	srv := httptest.NewServer(middleware.Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "hi")
	})))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Header.Get("Content-Encoding") == "gzip" {
		t.Error("should not gzip when client didn't ask")
	}
}

func TestGzip_Edge_NonCompressibleSkipped(t *testing.T) {
	srv := httptest.NewServer(middleware.Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, "binary-ish")
	})))
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Header.Get("Content-Encoding") == "gzip" {
		t.Error("image/png should not be gzipped")
	}
}

// Concurrent requests each get unique request IDs.
func TestChain_Edge_ConcurrentRequestIDsUnique(t *testing.T) {
	const n = 32
	srv, _ := newServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, middleware.RequestIDFrom(r.Context()))
	}))
	t.Cleanup(srv.Close)

	ids := make([]string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			resp, err := http.Get(srv.URL + "/")
			if err != nil {
				t.Errorf("GET: %v", err)
				return
			}
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			ids[i] = string(b)
		}()
	}
	wg.Wait()
	seen := make(map[string]bool)
	for _, id := range ids {
		if id == "" {
			t.Error("empty id")
		}
		if seen[id] {
			t.Errorf("duplicate id: %s", id)
		}
		seen[id] = true
	}
}
