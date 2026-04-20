package admin_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/admin"
	"github.com/penguin/blog-server/internal/settings"
	"github.com/penguin/blog-server/internal/storage"
)

// avatarBundle extends the admin test harness with a fully wired
// AvatarHandlers including a real settings.Store so we can assert
// avatar_url is persisted after upload.
type avatarBundle struct {
	*crudBundle
	Avatar      *admin.AvatarHandlers
	Settings    *settings.Store
	Invalidated int
}

func avatarUploadSetup(t *testing.T) *avatarBundle {
	t.Helper()
	b := crudSetup(t)
	// crudSetup opens its own *storage.Store. Open another for this test
	// so our Settings is live on the same DB file.
	st, err := storage.Open(b.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sett := settings.New(st.DB)
	ab := &avatarBundle{crudBundle: b, Settings: sett}
	ab.Avatar = &admin.AvatarHandlers{
		Parent:     b.Admin,
		DataDir:    b.DataDir,
		Settings:   sett,
		Invalidate: func() { ab.Invalidated++ },
	}
	return ab
}

// pngBytes builds a tiny valid PNG so http.DetectContentType returns image/png.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// multipartBody builds an application/x-multipart body with a `csrf` field
// and a single file field. Returns body bytes + content-type header value.
func multipartBody(t *testing.T, csrf, fieldName, filename string, contents []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("csrf", csrf)
	fw, err := mw.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(fw, bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, mw.FormDataContentType()
}

func postAvatar(t *testing.T, b *avatarBundle, ct string, body io.Reader, withCookie bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/manage/avatar/upload", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("User-Agent", "test/ua")
	if withCookie {
		req.AddCookie(b.Cookie)
	}
	w := httptest.NewRecorder()
	b.Avatar.Upload(w, req)
	return w
}

// Smoke：合法 PNG 上传 → 200 JSON 带 url；文件落到 DataDir/images/avatar.png；
// settings.avatar_url 被写入；Invalidate 被回调。
func TestAvatarUpload_Smoke_WritesFileAndSetting(t *testing.T) {
	b := avatarUploadSetup(t)
	body, ct := multipartBody(t, b.CSRF, "avatar", "me.png", pngBytes(t, 16, 16))
	w := postAvatar(t, b, ct, body, true)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if !strings.HasPrefix(resp["url"], "/images/avatar.png?v=") {
		t.Errorf("url = %q, want /images/avatar.png?v=...", resp["url"])
	}
	if _, err := os.Stat(filepath.Join(b.DataDir, "images", "avatar.png")); err != nil {
		t.Errorf("avatar.png 未落盘：%v", err)
	}
	if got, ok := b.Settings.Get("avatar_url"); !ok || !strings.HasPrefix(got, "/images/avatar.png?v=") {
		t.Errorf("avatar_url setting = %q ok=%v", got, ok)
	}
	if b.Invalidated != 1 {
		t.Errorf("Invalidate 应被调用 1 次，实际 %d", b.Invalidated)
	}
}

// Smoke：换扩展名（先传 png 再传 jpg）时，旧的 avatar.png 应被清掉，避免孤儿文件。
func TestAvatarUpload_Smoke_SwitchExtensionRemovesOld(t *testing.T) {
	b := avatarUploadSetup(t)
	// 先传 PNG
	body1, ct1 := multipartBody(t, b.CSRF, "avatar", "me.png", pngBytes(t, 8, 8))
	if w := postAvatar(t, b, ct1, body1, true); w.Code != 200 {
		t.Fatalf("first upload failed: %d", w.Code)
	}
	// 手工构造一个 JPEG magic header 的最小 blob（DetectContentType 只看前 512 字节的签名）
	jpgSig := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}
	body2, ct2 := multipartBody(t, b.CSRF, "avatar", "me.jpg", jpgSig)
	w := postAvatar(t, b, ct2, body2, true)
	if w.Code != 200 {
		t.Fatalf("second upload failed: %d, body=%s", w.Code, w.Body.String())
	}
	// 旧 png 应被删
	if _, err := os.Stat(filepath.Join(b.DataDir, "images", "avatar.png")); !os.IsNotExist(err) {
		t.Errorf("旧 avatar.png 未被清理：err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(b.DataDir, "images", "avatar.jpg")); err != nil {
		t.Errorf("新 avatar.jpg 未落盘：%v", err)
	}
}

// Edge（权限认证）：没 cookie → 401 JSON。
func TestAvatarUpload_Edge_NoCookieUnauthorized(t *testing.T) {
	b := avatarUploadSetup(t)
	body, ct := multipartBody(t, b.CSRF, "avatar", "x.png", pngBytes(t, 4, 4))
	w := postAvatar(t, b, ct, body, false)
	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// Edge（权限认证 / CSRF）：CSRF 错 → 403，文件不得落盘。
func TestAvatarUpload_Edge_BadCSRFForbidden(t *testing.T) {
	b := avatarUploadSetup(t)
	body, ct := multipartBody(t, "wrong-token", "avatar", "x.png", pngBytes(t, 4, 4))
	w := postAvatar(t, b, ct, body, true)
	if w.Code != 403 {
		t.Errorf("status = %d, want 403", w.Code)
	}
	if _, err := os.Stat(filepath.Join(b.DataDir, "images", "avatar.png")); err == nil {
		t.Errorf("CSRF 错不应落盘")
	}
}

// Edge（非法输入 / 错 MIME）：上传一个纯文本（sniff 到 text/plain）→ 415。
func TestAvatarUpload_Edge_WrongMIMERejected(t *testing.T) {
	b := avatarUploadSetup(t)
	body, ct := multipartBody(t, b.CSRF, "avatar", "notice.txt", []byte("这不是图片"))
	w := postAvatar(t, b, ct, body, true)
	if w.Code != 415 {
		t.Errorf("status = %d, want 415", w.Code)
	}
}

// Edge（非法输入 / 没带文件）：form 里根本没 avatar 字段 → 400。
func TestAvatarUpload_Edge_NoFileField(t *testing.T) {
	b := avatarUploadSetup(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("csrf", b.CSRF)
	_ = mw.Close()
	w := postAvatar(t, b, mw.FormDataContentType(), &body, true)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// Edge（边界值 / 空文件）：字段在但 size=0 → 400。
func TestAvatarUpload_Edge_EmptyFileRejected(t *testing.T) {
	b := avatarUploadSetup(t)
	body, ct := multipartBody(t, b.CSRF, "avatar", "empty.png", []byte{})
	w := postAvatar(t, b, ct, body, true)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// deleteAvatar posts the minimal form body used by /manage/avatar/delete.
func deleteAvatar(t *testing.T, b *avatarBundle, csrf string, withCookie bool) *httptest.ResponseRecorder {
	t.Helper()
	body := strings.NewReader("csrf=" + csrf)
	req := httptest.NewRequest("POST", "/manage/avatar/delete", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	if withCookie {
		req.AddCookie(b.Cookie)
	}
	w := httptest.NewRecorder()
	b.Avatar.Delete(w, req)
	return w
}

// Smoke：先上传再删除 → 文件被清、avatar_url 被置空、Invalidate 回调。
func TestAvatarDelete_Smoke_RemovesFileAndClearsSetting(t *testing.T) {
	b := avatarUploadSetup(t)
	// 先上传一张
	ub, uct := multipartBody(t, b.CSRF, "avatar", "me.png", pngBytes(t, 16, 16))
	if w := postAvatar(t, b, uct, ub, true); w.Code != 200 {
		t.Fatalf("upload failed: %d", w.Code)
	}
	before := b.Invalidated
	w := deleteAvatar(t, b, b.CSRF, true)
	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if resp["url"] != "" {
		t.Errorf("url = %q, want empty", resp["url"])
	}
	if _, err := os.Stat(filepath.Join(b.DataDir, "images", "avatar.png")); !os.IsNotExist(err) {
		t.Errorf("avatar.png 未被清理：err=%v", err)
	}
	if got, _ := b.Settings.Get("avatar_url"); got != "" {
		t.Errorf("avatar_url 应被置空，实得 %q", got)
	}
	if b.Invalidated != before+1 {
		t.Errorf("Invalidate 应被回调，调用次数 before=%d after=%d", before, b.Invalidated)
	}
}

// Edge（幂等）：从未设过头像也能正常返回 200，不报错。
func TestAvatarDelete_Edge_IdempotentWhenNothingExists(t *testing.T) {
	b := avatarUploadSetup(t)
	w := deleteAvatar(t, b, b.CSRF, true)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// Edge（权限认证）：没 cookie → 401。
func TestAvatarDelete_Edge_NoCookieUnauthorized(t *testing.T) {
	b := avatarUploadSetup(t)
	w := deleteAvatar(t, b, b.CSRF, false)
	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// Edge（权限认证 / CSRF）：CSRF 错 → 403；且文件不能被删。
func TestAvatarDelete_Edge_BadCSRFForbidden(t *testing.T) {
	b := avatarUploadSetup(t)
	// 先放一张
	ub, uct := multipartBody(t, b.CSRF, "avatar", "me.png", pngBytes(t, 8, 8))
	if w := postAvatar(t, b, uct, ub, true); w.Code != 200 {
		t.Fatalf("upload failed: %d", w.Code)
	}
	w := deleteAvatar(t, b, "wrong", true)
	if w.Code != 403 {
		t.Errorf("status = %d, want 403", w.Code)
	}
	if _, err := os.Stat(filepath.Join(b.DataDir, "images", "avatar.png")); err != nil {
		t.Errorf("CSRF 错不应删文件：%v", err)
	}
}
