package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/penguin/blog-server/internal/auth"
)

const (
	maxImageBytes = 5 * 1024 * 1024
)

var allowedImageMIMEs = map[string]string{
	"image/png":     ".png",
	"image/jpeg":    ".jpg",
	"image/webp":    ".webp",
	"image/gif":     ".gif",
	"image/svg+xml": ".svg",
}

// ImageHandlers groups list + upload handlers under /manage/images*.
type ImageHandlers struct {
	Parent  *Handlers
	DataDir string
}

// ImagesList handles GET /manage/images.
func (ih *ImageHandlers) ImagesList(w http.ResponseWriter, r *http.Request) {
	sess, _ := ih.Parent.Auth.ParseSession(r)
	files := listImages(filepath.Join(ih.DataDir, "images"))
	data := map[string]any{
		"Files": files,
		"CSRF":  sess.CSRF,
		"Error": r.URL.Query().Get("e"),
		"Info":  r.URL.Query().Get("m"),
	}
	if err := ih.Parent.Tpl.Render(w, r, http.StatusOK, "admin_images.html", data); err != nil {
		ih.Parent.Logger.Error("admin.images.list", slog.String("err", err.Error()))
		http.Error(w, "internal", http.StatusInternalServerError)
	}
}

// ImageItem is a single row shown in the UI.
type ImageItem struct {
	Name string
	URL  string
	Size int64
}

func listImages(dir string) []ImageItem {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]ImageItem, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, ImageItem{
			Name: e.Name(),
			URL:  "/images/" + e.Name(),
			Size: info.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ImagesUpload handles POST /manage/images/upload.
func (ih *ImageHandlers) ImagesUpload(w http.ResponseWriter, r *http.Request) {
	sess, ok := ih.Parent.Auth.ParseSession(r)
	if !ok {
		http.Redirect(w, r, "/manage/login", http.StatusSeeOther)
		return
	}
	// 5MB + form overhead budget.
	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes+1024*1024)
	if err := r.ParseMultipartForm(maxImageBytes + 512*1024); err != nil {
		redirectImages(w, r, "上传文件过大或格式错误", "")
		return
	}
	if !auth.CSRFValid(sess, r.FormValue("csrf")) {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		redirectImages(w, r, "请选择文件", "")
		return
	}
	defer func() { _ = file.Close() }()

	if header.Size == 0 {
		redirectImages(w, r, "文件为空", "")
		return
	}
	if header.Size > maxImageBytes {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		redirectImages(w, r, "文件超过 5MB 限制", "")
		return
	}
	// Sniff MIME from the first 512 bytes to reduce lying-extension abuse.
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	sniff := http.DetectContentType(buf[:n])
	// Rewind to the beginning.
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		redirectImages(w, r, "文件无法 seek", "")
		return
	}
	ext, ok := allowedImageMIMEs[strings.ToLower(strings.Split(sniff, ";")[0])]
	if !ok {
		// Allow svg explicitly — DetectContentType identifies it as text/xml.
		if strings.HasPrefix(sniff, "text/xml") || strings.HasPrefix(sniff, "text/plain") {
			if strings.HasSuffix(strings.ToLower(header.Filename), ".svg") {
				ext = ".svg"
				ok = true
			}
		}
	}
	if !ok {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		redirectImages(w, r, "不支持的文件类型（仅 png/jpeg/webp/gif/svg）", "")
		return
	}

	// Hash content for stable dedup naming.
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		redirectImages(w, r, "读取文件失败", "")
		return
	}
	sum := hex.EncodeToString(h.Sum(nil))[:16]
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		redirectImages(w, r, "seek 失败", "")
		return
	}

	outName := sum + ext
	dir := filepath.Join(ih.DataDir, "images")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		redirectImages(w, r, "mkdir 失败", "")
		return
	}
	outPath := filepath.Join(dir, outName)
	// Same content → same filename → idempotent: if exists, just reuse.
	if _, err := os.Stat(outPath); err == nil {
		redirectImages(w, r, "", "已存在：/images/"+outName)
		return
	}
	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		redirectImages(w, r, "写入失败："+err.Error(), "")
		return
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, file); err != nil {
		_ = os.Remove(outPath)
		redirectImages(w, r, "写入失败", "")
		return
	}
	redirectImages(w, r, "", fmt.Sprintf("已上传：/images/%s", outName))
}

func redirectImages(w http.ResponseWriter, r *http.Request, errMsg, info string) {
	target := "/manage/images"
	if errMsg != "" {
		target += "?e=" + URLEscape(errMsg)
	} else if info != "" {
		target += "?m=" + URLEscape(info)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
