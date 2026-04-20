package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/penguin/blog-server/internal/auth"
	"github.com/penguin/blog-server/internal/settings"
)

// AvatarHandlers serves POST /manage/avatar/upload — a one-shot endpoint that
// accepts an image, writes it to a fixed filename under images/, sets
// `avatar_url` in site_settings and invalidates the public Handlers cache.
// Returns a tiny JSON body so the settings page can refresh its thumbnail
// without a full redirect / reload.
type AvatarHandlers struct {
	Parent     *Handlers
	DataDir    string
	Settings   *settings.Store
	Invalidate func()
}

// Upload writes the posted image to <DataDir>/images/avatar<ext>, persists
// avatar_url=/images/avatar<ext>?v=<ts> (timestamp busts browser cache), and
// replies `{"url":"..."}` on success. All errors reply `{"error":"..."}`
// with an appropriate status so the JS can surface them.
func (a *AvatarHandlers) Upload(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.Parent.Auth.ParseSession(r)
	if !ok {
		avatarJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		avatarJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes+1024*1024)
	if err := r.ParseMultipartForm(maxImageBytes + 512*1024); err != nil {
		avatarJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "文件过大或格式错误"})
		return
	}
	if !auth.CSRFValid(sess, r.FormValue("csrf")) {
		avatarJSON(w, http.StatusForbidden, map[string]string{"error": "csrf"})
		return
	}
	file, header, err := r.FormFile("avatar")
	if err != nil {
		avatarJSON(w, http.StatusBadRequest, map[string]string{"error": "请选择文件"})
		return
	}
	defer func() { _ = file.Close() }()
	if header.Size == 0 {
		avatarJSON(w, http.StatusBadRequest, map[string]string{"error": "文件为空"})
		return
	}
	if header.Size > maxImageBytes {
		avatarJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "文件超过 5MB 限制"})
		return
	}
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	sniff := http.DetectContentType(buf[:n])
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		avatarJSON(w, http.StatusInternalServerError, map[string]string{"error": "文件无法 seek"})
		return
	}
	ext, ok := allowedImageMIMEs[strings.ToLower(strings.Split(sniff, ";")[0])]
	if !ok {
		if strings.HasPrefix(sniff, "text/xml") || strings.HasPrefix(sniff, "text/plain") {
			if strings.HasSuffix(strings.ToLower(header.Filename), ".svg") {
				ext = ".svg"
				ok = true
			}
		}
	}
	if !ok {
		avatarJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "不支持的文件类型（仅 png/jpeg/webp/gif/svg）"})
		return
	}
	dir := filepath.Join(a.DataDir, "images")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		avatarJSON(w, http.StatusInternalServerError, map[string]string{"error": "mkdir 失败"})
		return
	}
	name := "avatar" + ext
	// 清掉同名不同扩展的老头像（比如之前传的是 png、这次传 jpg），防止孤儿文件
	// 和 /images/avatar.xxx 路径错配。
	for _, oldExt := range allowedImageMIMEs {
		if oldExt == ext {
			continue
		}
		_ = os.Remove(filepath.Join(dir, "avatar"+oldExt))
	}
	outPath := filepath.Join(dir, name)
	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		avatarJSON(w, http.StatusInternalServerError, map[string]string{"error": "写入失败：" + err.Error()})
		return
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, file); err != nil {
		_ = os.Remove(outPath)
		avatarJSON(w, http.StatusInternalServerError, map[string]string{"error": "写入失败"})
		return
	}
	url := fmt.Sprintf("/images/%s?v=%d", name, time.Now().Unix())
	if a.Settings != nil {
		if err := a.Settings.Set("avatar_url", url); err != nil {
			a.Parent.Logger.Error("admin.avatar.set_setting", slog.String("err", err.Error()))
			// 文件已落盘但 setting 写失败；前端仍能收到 url 让 JS 展示，保存按钮
			// 也能把当前输入框值重新提交。不删文件。
		}
	}
	if a.Invalidate != nil {
		a.Invalidate()
	}
	avatarJSON(w, http.StatusOK, map[string]string{"url": url})
}

// Delete handles POST /manage/avatar/delete — wipes the on-disk avatar.*
// files (all supported extensions) and clears the avatar_url setting.
// Idempotent: succeeds even if nothing is there.
func (a *AvatarHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.Parent.Auth.ParseSession(r)
	if !ok {
		avatarJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		avatarJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if err := r.ParseForm(); err != nil {
		avatarJSON(w, http.StatusBadRequest, map[string]string{"error": "bad form"})
		return
	}
	if !auth.CSRFValid(sess, r.Form.Get("csrf")) {
		avatarJSON(w, http.StatusForbidden, map[string]string{"error": "csrf"})
		return
	}
	dir := filepath.Join(a.DataDir, "images")
	for _, ext := range allowedImageMIMEs {
		_ = os.Remove(filepath.Join(dir, "avatar"+ext))
	}
	if a.Settings != nil {
		if err := a.Settings.Set("avatar_url", ""); err != nil {
			a.Parent.Logger.Error("admin.avatar.clear_setting", slog.String("err", err.Error()))
		}
	}
	if a.Invalidate != nil {
		a.Invalidate()
	}
	avatarJSON(w, http.StatusOK, map[string]string{"url": ""})
}

func avatarJSON(w http.ResponseWriter, status int, payload map[string]string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
