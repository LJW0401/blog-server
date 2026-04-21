package admin_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/penguin/blog-server/internal/admin"
)

// coverBundle wires a fully-prepared PortfolioCoverHandlers on top of crudBundle.
type coverBundle struct {
	*crudBundle
	Cover *admin.PortfolioCoverHandlers
}

func coverSetup(t *testing.T) *coverBundle {
	t.Helper()
	b := crudSetup(t)
	return &coverBundle{crudBundle: b, Cover: &admin.PortfolioCoverHandlers{Parent: b.Admin, DataDir: b.DataDir}}
}

// multipartCoverBody produces a multipart form body with slug, csrf and cover
// file fields, returning the body reader + content-type header value.
func multipartCoverBody(t *testing.T, csrf, slug, filename string, contents []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("csrf", csrf)
	_ = mw.WriteField("slug", slug)
	fw, err := mw.CreateFormFile("cover", filename)
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

func postCover(t *testing.T, b *coverBundle, ct string, body io.Reader, withCookie bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/manage/portfolio/cover/upload", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("User-Agent", "test/ua")
	if withCookie {
		req.AddCookie(b.Cookie)
	}
	w := httptest.NewRecorder()
	b.Cover.Upload(w, req)
	return w
}

// --- WI-3.12 Smoke ---------------------------------------------------------

func TestPortfolioCover_Smoke_UploadPNG(t *testing.T) {
	b := coverSetup(t)
	body, ct := multipartCoverBody(t, b.CSRF, "foo-slug", "cover.png", pngBytes(t, 32, 32))
	w := postCover(t, b, ct, body, true)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["url"] == "" || !strings.HasPrefix(resp["url"], "/images/portfolio-foo-slug-cover") {
		t.Errorf("url missing / malformed: %q", resp["url"])
	}
	path := filepath.Join(b.DataDir, "images", "portfolio-foo-slug-cover.png")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("cover file not written: %v", err)
	}
}

func TestPortfolioCover_Smoke_ReplacesOldCoverDifferentExt(t *testing.T) {
	b := coverSetup(t)
	// Pre-seed a JPG for the same slug.
	oldPath := filepath.Join(b.DataDir, "images", "portfolio-shared-cover.jpg")
	_ = os.MkdirAll(filepath.Dir(oldPath), 0o700)
	_ = os.WriteFile(oldPath, []byte("old jpg"), 0o644)
	// Upload a PNG — old jpg should be gone; new png should land.
	body, ct := multipartCoverBody(t, b.CSRF, "shared", "new.png", pngBytes(t, 16, 16))
	w := postCover(t, b, ct, body, true)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old .jpg not cleaned up: %v", err)
	}
	newPath := filepath.Join(b.DataDir, "images", "portfolio-shared-cover.png")
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("new .png missing: %v", err)
	}
}

// --- WI-3.13 Exception ------------------------------------------------------

func TestPortfolioCover_Exception_Unauthenticated(t *testing.T) {
	b := coverSetup(t)
	body, ct := multipartCoverBody(t, b.CSRF, "x", "c.png", pngBytes(t, 8, 8))
	w := postCover(t, b, ct, body, false) // no cookie
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthed status=%d want 401", w.Code)
	}
}

func TestPortfolioCover_Exception_CSRFMissing(t *testing.T) {
	b := coverSetup(t)
	body, ct := multipartCoverBody(t, "", "x", "c.png", pngBytes(t, 8, 8))
	w := postCover(t, b, ct, body, true)
	if w.Code != http.StatusForbidden {
		t.Errorf("CSRF missing status=%d want 403", w.Code)
	}
}

func TestPortfolioCover_Exception_InvalidSlug(t *testing.T) {
	b := coverSetup(t)
	for _, bad := range []string{"", "Bad Slug", "../evil", strings.Repeat("a", 200)} {
		body, ct := multipartCoverBody(t, b.CSRF, bad, "c.png", pngBytes(t, 8, 8))
		w := postCover(t, b, ct, body, true)
		if w.Code != http.StatusBadRequest {
			t.Errorf("slug=%q status=%d want 400", bad, w.Code)
		}
	}
}

func TestPortfolioCover_Exception_OversizedFile(t *testing.T) {
	b := coverSetup(t)
	// Build a 3MB file that starts with PNG magic so MIME sniffing would
	// pass, then let size limit reject it.
	big := make([]byte, 3*1024*1024)
	copy(big, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	body, ct := multipartCoverBody(t, b.CSRF, "slug", "big.png", big)
	w := postCover(t, b, ct, body, true)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize status=%d want 413, body=%s", w.Code, w.Body.String())
	}
}

func TestPortfolioCover_Exception_UnsupportedMIME(t *testing.T) {
	b := coverSetup(t)
	// Plain text disguised with .png extension
	body, ct := multipartCoverBody(t, b.CSRF, "slug", "fake.png", []byte("definitely not an image, just plain text"))
	w := postCover(t, b, ct, body, true)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("bogus mime status=%d want 415", w.Code)
	}
}

func TestPortfolioCover_Exception_EmptyFile(t *testing.T) {
	b := coverSetup(t)
	body, ct := multipartCoverBody(t, b.CSRF, "slug", "empty.png", []byte{})
	w := postCover(t, b, ct, body, true)
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty status=%d want 400", w.Code)
	}
}

func TestPortfolioCover_Exception_ConcurrentUploadsNoHalfFile(t *testing.T) {
	b := coverSetup(t)
	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, ct := multipartCoverBody(t, b.CSRF, "concurrent", "c.png", pngBytes(t, 32, 32))
			w := postCover(t, b, ct, body, true)
			if w.Code != 200 {
				mu.Lock()
				errCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	// Final state: exactly one cover file, no .tmp leftovers.
	dir := filepath.Join(b.DataDir, "images")
	entries, _ := os.ReadDir(dir)
	pngCount := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "portfolio-concurrent-cover") {
			if strings.HasSuffix(e.Name(), ".tmp") {
				t.Errorf(".tmp leftover: %s", e.Name())
			}
			if strings.HasSuffix(e.Name(), ".png") {
				pngCount++
			}
		}
	}
	if pngCount != 1 {
		t.Errorf("expected 1 final cover png, found %d", pngCount)
	}
}

func TestPortfolioCover_Exception_WrongFieldName(t *testing.T) {
	// Form field has wrong name ("image" instead of "cover") → 400
	b := coverSetup(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("csrf", b.CSRF)
	_ = mw.WriteField("slug", "slug")
	fw, _ := mw.CreateFormFile("image", "c.png") // wrong name
	_, _ = io.Copy(fw, bytes.NewReader(pngBytes(t, 8, 8)))
	_ = mw.Close()
	w := postCover(t, b, mw.FormDataContentType(), &body, true)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", w.Code)
	}
}
