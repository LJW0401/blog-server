package admin_test

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/admin"
)

// setupTrash returns the trash handler wired on top of the shared crud bundle.
func setupTrash(t *testing.T) (*admin.TrashHandlers, *crudBundle) {
	t.Helper()
	b := crudSetup(t)
	th := &admin.TrashHandlers{Parent: b.Admin, Content: b.Content, DataDir: b.DataDir}
	return th, b
}

// seedTrash writes a fake soft-deleted file into $DataDir/trash/ with the
// given filename + body, returning the full path.
func seedTrash(t *testing.T, b *crudBundle, name, body string) string {
	t.Helper()
	trashDir := filepath.Join(b.DataDir, "trash")
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(trashDir, name)
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return full
}

// --- Smoke ----------------------------------------------------------------

func TestTrash_Smoke_EmptyList(t *testing.T) {
	th, b := setupTrash(t)
	w := b.authedGet(t, "/manage/trash", th.TrashList)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	out := w.Body.String()
	if !strings.Contains(out, "回收站为空") {
		t.Errorf("empty state missing: %s", out)
	}
}

func TestTrash_Smoke_ListShowsDocAndProject(t *testing.T) {
	th, b := setupTrash(t)
	seedTrash(t, b, "20260101-120000-alpha.md", docMD("alpha"))
	seedTrash(t, b, "20260102-120000-proj-gamma.md", projMD("gamma"))
	w := b.authedGet(t, "/manage/trash", th.TrashList)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	out := w.Body.String()
	// doc 显示 "文档"，project 显示 "项目"；两个 slug 都应该在
	for _, want := range []string{"alpha", "gamma", "文档", "项目",
		"20260101-120000-alpha.md", "20260102-120000-proj-gamma.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q", want)
		}
	}
	// 时间前缀被格式化成 "2026-01-01 12:00:00 UTC"
	if !strings.Contains(out, "2026-01-01 12:00:00 UTC") {
		t.Errorf("trashed-at not formatted: %s", out)
	}
}

func TestTrash_Smoke_RestoreDoc(t *testing.T) {
	th, b := setupTrash(t)
	src := seedTrash(t, b, "20260101-120000-alpha.md", docMD("alpha"))
	w := b.authedPost(t, "/manage/trash/restore",
		url.Values{"csrf": {b.CSRF}, "filename": {"20260101-120000-alpha.md"}},
		th.Restore)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d body=%s", w.Code, w.Body.String())
	}
	// trash 文件应该没了
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("trash file should be gone: %v", err)
	}
	// content/docs/alpha.md 应该出现
	restored := filepath.Join(b.DataDir, "content", "docs", "alpha.md")
	if _, err := os.Stat(restored); err != nil {
		t.Errorf("restored doc missing: %v", err)
	}
}

func TestTrash_Smoke_RestoreProject(t *testing.T) {
	th, b := setupTrash(t)
	seedTrash(t, b, "20260102-120000-proj-gamma.md", projMD("gamma"))
	w := b.authedPost(t, "/manage/trash/restore",
		url.Values{"csrf": {b.CSRF}, "filename": {"20260102-120000-proj-gamma.md"}},
		th.Restore)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d body=%s", w.Code, w.Body.String())
	}
	restored := filepath.Join(b.DataDir, "content", "projects", "gamma.md")
	if _, err := os.Stat(restored); err != nil {
		t.Errorf("restored project missing: %v", err)
	}
}

func TestTrash_Smoke_Purge(t *testing.T) {
	th, b := setupTrash(t)
	src := seedTrash(t, b, "20260101-120000-alpha.md", docMD("alpha"))
	w := b.authedPost(t, "/manage/trash/purge",
		url.Values{"csrf": {b.CSRF}, "filename": {"20260101-120000-alpha.md"}},
		th.Purge)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d", w.Code)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("file should be deleted: %v", err)
	}
}

// --- 异常：非法输入（path traversal） ---------------------------------------

func TestTrash_Edge_PathTraversalRejected(t *testing.T) {
	th, b := setupTrash(t)
	// 造一个站外敏感文件当靶子
	outside := filepath.Join(b.DataDir, "content", "docs", "victim.md")
	if err := os.MkdirAll(filepath.Dir(outside), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		"../content/docs/victim.md",
		"../../etc/passwd",
		"/etc/passwd",
		"20260101-120000-../../../etc/passwd.md",
		"..",
		".",
		"20260101-120000-.md", // regex 要求 slug 至少 1 个 \w 字符
		"not-a-trash-name",
	} {
		w := b.authedPost(t, "/manage/trash/purge",
			url.Values{"csrf": {b.CSRF}, "filename": {bad}},
			th.Purge)
		if w.Code != http.StatusBadRequest {
			t.Errorf("filename=%q should 400, got %d", bad, w.Code)
		}
	}
	// victim 文件必须还在
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("victim file gone after traversal attempt: %v", err)
	}
}

// --- 异常：边界值（slug 冲突 / 文件不存在 / 空输入） -----------------------

func TestTrash_Edge_RestoreCollisionRefuses(t *testing.T) {
	th, b := setupTrash(t)
	// 先在 content/docs/ 写一份和 trash 里同名的文件，模拟冲突
	existing := filepath.Join(b.DataDir, "content", "docs", "alpha.md")
	if err := os.MkdirAll(filepath.Dir(existing), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("live"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := seedTrash(t, b, "20260101-120000-alpha.md", docMD("alpha"))
	w := b.authedPost(t, "/manage/trash/restore",
		url.Values{"csrf": {b.CSRF}, "filename": {"20260101-120000-alpha.md"}},
		th.Restore)
	// 冲突时走重定向回 list 页 + query 带错误，不覆盖现有
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "e=") {
		t.Errorf("expected error flash in redirect; got %s", w.Header().Get("Location"))
	}
	// trash 文件没动
	if _, err := os.Stat(src); err != nil {
		t.Errorf("trash file should remain on collision: %v", err)
	}
	// 现有文件内容仍是 "live"
	if got, _ := os.ReadFile(existing); string(got) != "live" {
		t.Errorf("existing file overwritten: %q", got)
	}
}

func TestTrash_Edge_NonexistentFile404(t *testing.T) {
	th, b := setupTrash(t)
	w := b.authedPost(t, "/manage/trash/purge",
		url.Values{"csrf": {b.CSRF}, "filename": {"20260101-120000-ghost.md"}},
		th.Purge)
	if w.Code != http.StatusBadRequest {
		t.Errorf("nonexistent file should 400, got %d", w.Code)
	}
}

func TestTrash_Edge_EmptyFilename(t *testing.T) {
	th, b := setupTrash(t)
	w := b.authedPost(t, "/manage/trash/restore",
		url.Values{"csrf": {b.CSRF}, "filename": {""}}, th.Restore)
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty filename should 400, got %d", w.Code)
	}
}

// --- 异常：权限认证（CSRF 缺失） -------------------------------------------

func TestTrash_Edge_RestoreCSRFMissing(t *testing.T) {
	th, b := setupTrash(t)
	seedTrash(t, b, "20260101-120000-alpha.md", docMD("alpha"))
	w := b.authedPost(t, "/manage/trash/restore",
		url.Values{"csrf": {""}, "filename": {"20260101-120000-alpha.md"}},
		th.Restore)
	if w.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", w.Code)
	}
}

func TestTrash_Edge_PurgeCSRFMissing(t *testing.T) {
	th, b := setupTrash(t)
	seedTrash(t, b, "20260101-120000-alpha.md", docMD("alpha"))
	w := b.authedPost(t, "/manage/trash/purge",
		url.Values{"csrf": {""}, "filename": {"20260101-120000-alpha.md"}},
		th.Purge)
	if w.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", w.Code)
	}
}

// --- helpers ---

func docMD(slug string) string {
	return "---\ntitle: " + slug + "\nslug: " + slug + "\nstatus: published\nupdated: 2026-04-19\ncreated: 2026-04-19\n---\n正文\n"
}

func projMD(slug string) string {
	return "---\ntitle: " + slug + "\nslug: " + slug + "\nrepo: foo/bar\ndisplay_name: " + slug + "\nstatus: active\nupdated: 2026-04-19\ncreated: 2026-04-19\n---\n描述\n"
}
