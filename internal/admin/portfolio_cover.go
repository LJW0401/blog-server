package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/penguin/blog-server/internal/auth"
)

// PortfolioCoverHandlers handles multipart cover uploads for portfolio
// entries. Files are written to <DataDir>/images/portfolio-<slug>-cover<ext>;
// any previously uploaded cover with a different extension for the same slug
// is removed, so each portfolio carries at most one on-disk cover file.
type PortfolioCoverHandlers struct {
	Parent  *Handlers
	DataDir string
}

const maxPortfolioCoverBytes = 2 * 1024 * 1024 // 2MB

// portfolioCoverMIMEs are the MIME types accepted by the cover upload
// endpoint. Kept intentionally narrower than allowedImageMIMEs (no GIF) —
// portfolio covers are static and we don't want animation by default.
var portfolioCoverMIMEs = map[string]string{
	"image/png":     ".png",
	"image/jpeg":    ".jpg",
	"image/webp":    ".webp",
	"image/svg+xml": ".svg",
}

// Upload handles POST /manage/portfolio/cover/upload.
// multipart form: slug=<slug>, cover=<file>, csrf=<token>
// Returns JSON { "url": "/images/portfolio-<slug>-cover.<ext>?v=<ts>" } on
// success, or { "error": "..." } with 4xx/5xx on failure.
func (p *PortfolioCoverHandlers) Upload(w http.ResponseWriter, r *http.Request) {
	sess, ok := p.Parent.Auth.ParseSession(r)
	if !ok {
		portfolioCoverJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		portfolioCoverJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPortfolioCoverBytes+512*1024)
	if err := r.ParseMultipartForm(maxPortfolioCoverBytes + 256*1024); err != nil {
		portfolioCoverJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "文件过大或格式错误"})
		return
	}
	if !auth.CSRFValid(sess, r.FormValue("csrf")) {
		portfolioCoverJSON(w, http.StatusForbidden, map[string]string{"error": "csrf"})
		return
	}
	slug := strings.TrimSpace(r.FormValue("slug"))
	if !isSafeSlug(slug) {
		portfolioCoverJSON(w, http.StatusBadRequest, map[string]string{"error": "slug 非法"})
		return
	}
	file, header, err := r.FormFile("cover")
	if err != nil {
		portfolioCoverJSON(w, http.StatusBadRequest, map[string]string{"error": "请选择文件"})
		return
	}
	defer func() { _ = file.Close() }()
	if header.Size == 0 {
		portfolioCoverJSON(w, http.StatusBadRequest, map[string]string{"error": "文件为空"})
		return
	}
	if header.Size > maxPortfolioCoverBytes {
		portfolioCoverJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "文件超过 2MB 限制"})
		return
	}
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	sniff := strings.ToLower(strings.Split(http.DetectContentType(buf[:n]), ";")[0])
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		portfolioCoverJSON(w, http.StatusInternalServerError, map[string]string{"error": "文件无法 seek"})
		return
	}
	ext, allowed := portfolioCoverMIMEs[sniff]
	if !allowed {
		// SVG often sniffs as text/xml or text/plain; accept if filename
		// extension is .svg AND it contains <svg so we don't widen to any
		// XML/text payload.
		if (strings.HasPrefix(sniff, "text/xml") || strings.HasPrefix(sniff, "text/plain")) &&
			strings.HasSuffix(strings.ToLower(header.Filename), ".svg") &&
			strings.Contains(strings.ToLower(string(buf[:n])), "<svg") {
			ext = ".svg"
			allowed = true
		}
	}
	if !allowed {
		portfolioCoverJSON(w, http.StatusUnsupportedMediaType,
			map[string]string{"error": "不支持的文件类型（仅 png/jpeg/webp/svg）"})
		return
	}
	dir := filepath.Join(p.DataDir, "images")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		portfolioCoverJSON(w, http.StatusInternalServerError, map[string]string{"error": "mkdir 失败"})
		return
	}
	basePrefix := "portfolio-" + slug + "-cover"
	// Clean up old covers with other extensions for this slug.
	for _, oldExt := range portfolioCoverMIMEs {
		if oldExt == ext {
			continue
		}
		_ = os.Remove(filepath.Join(dir, basePrefix+oldExt))
	}
	outName := basePrefix + ext
	outPath := filepath.Join(dir, outName)
	// Write through a temp file then rename for atomicity — partial writes
	// must never leave a half-file at the target.
	tmp := outPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		portfolioCoverJSON(w, http.StatusInternalServerError, map[string]string{"error": "写入失败：" + err.Error()})
		return
	}
	if _, err := io.Copy(f, file); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		portfolioCoverJSON(w, http.StatusInternalServerError, map[string]string{"error": "写入失败"})
		return
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		portfolioCoverJSON(w, http.StatusInternalServerError, map[string]string{"error": "close 失败"})
		return
	}
	if err := os.Rename(tmp, outPath); err != nil {
		_ = os.Remove(tmp)
		portfolioCoverJSON(w, http.StatusInternalServerError, map[string]string{"error": "rename 失败"})
		return
	}
	url := fmt.Sprintf("/images/%s?v=%d", outName, time.Now().Unix())
	portfolioCoverJSON(w, http.StatusOK, map[string]string{"url": url})
}

func portfolioCoverJSON(w http.ResponseWriter, status int, payload map[string]string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
