package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// gzipWriterPool reuses gzip.Writer instances across requests to keep
// per-request allocations minimal.
var gzipWriterPool = sync.Pool{
	New: func() any { return gzip.NewWriter(io.Discard) },
}

// gzippedResponseWriter wraps http.ResponseWriter with a gzip-buffered body.
type gzippedResponseWriter struct {
	http.ResponseWriter
	gz       *gzip.Writer
	compress bool // decided on first WriteHeader / Write based on Content-Type
	sniffed  bool
}

func (g *gzippedResponseWriter) WriteHeader(code int) {
	if !g.sniffed {
		g.decideCompression()
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzippedResponseWriter) Write(p []byte) (int, error) {
	if !g.sniffed {
		g.decideCompression()
	}
	if g.compress {
		return g.gz.Write(p)
	}
	return g.ResponseWriter.Write(p)
}

func (g *gzippedResponseWriter) decideCompression() {
	g.sniffed = true
	h := g.Header()
	if h.Get("Content-Encoding") != "" {
		return // already encoded
	}
	ct := h.Get("Content-Type")
	if ct == "" {
		// If caller hasn't set CT yet, defer — we'll re-check on next Write
		// by resetting sniffed (it's set true so we only skip this once).
		g.sniffed = false
		return
	}
	if !shouldCompress(ct) {
		return
	}
	h.Set("Content-Encoding", "gzip")
	h.Set("Vary", "Accept-Encoding")
	h.Del("Content-Length")
	g.compress = true
}

// Gzip wraps responses with gzip when Accept-Encoding requests it and the
// content type is compressible. Mutually respects existing Content-Encoding.
func Gzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(w)
		defer func() {
			_ = gz.Close()
			gzipWriterPool.Put(gz)
		}()
		grw := &gzippedResponseWriter{ResponseWriter: w, gz: gz}
		next.ServeHTTP(grw, r)
	})
}

func shouldCompress(ct string) bool {
	// Strip the parameters (e.g. "; charset=utf-8").
	if i := strings.IndexByte(ct, ';'); i > 0 {
		ct = ct[:i]
	}
	ct = strings.TrimSpace(strings.ToLower(ct))
	switch ct {
	case "text/html", "text/css", "text/plain", "text/xml",
		"application/xml", "application/rss+xml",
		"application/javascript", "application/json":
		return true
	}
	return false
}
