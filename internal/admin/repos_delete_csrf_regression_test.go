package admin_test

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/content"
	"github.com/penguin/blog-server/internal/storage"
)

// Reproduces: user clicks 删除 on a registered repo → page shows plain text
// "csrf" (403). Root cause is `{{ $.CSRF }}` inside a range inside a with —
// `$` is the root payload, not the `.Data` scope, so the hidden input renders
// with an empty value and the server rejects the submission.
func TestReposList_Bug_DeleteFormHasValidCSRF(t *testing.T) {
	b := crudSetup(t)
	// Register a project (write MD file directly, reload).
	md := "---\n" +
		"slug: demo\nrepo: o/demo\ndisplay_name: Demo\nstatus: developing\n" +
		"created: 2026-04-19\nupdated: 2026-04-19\n---\nbody\n"
	path := filepath.Join(b.DataDir, "content", "projects", "demo.md")
	if err := storage.AtomicWrite(path, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Content.Projects().Get(content.KindProject, "demo"); !ok {
		t.Fatal("precondition: demo project not in index")
	}

	// Render /manage/repos and extract the hidden csrf from the delete form.
	w := b.authedGet(t, "/manage/repos", b.Projects.ReposList)
	if w.Code != 200 {
		t.Fatalf("list status %d", w.Code)
	}
	body := w.Body.String()

	// Find all <form action=".../demo/delete"...> blocks and inspect the csrf.
	reForm := regexp.MustCompile(`(?s)<form[^>]*action="/manage/projects/demo/delete"[^>]*>.*?</form>`)
	formHTML := reForm.FindString(body)
	if formHTML == "" {
		t.Fatalf("delete form for /projects/demo not found in rendered HTML")
	}
	reCSRF := regexp.MustCompile(`name="csrf"\s+value="([^"]*)"`)
	m := reCSRF.FindStringSubmatch(formHTML)
	if m == nil {
		t.Fatalf("no csrf hidden input in delete form: %q", formHTML)
	}
	if m[1] == "" {
		t.Errorf("delete form hidden csrf is EMPTY — template likely uses $.CSRF in wrong scope")
	}
	if m[1] != b.CSRF {
		t.Errorf("delete form csrf = %q, want session csrf %q", m[1], b.CSRF)
	}

	// End-to-end: submit the form using the value pulled from the DOM.
	// If the rendered value is wrong, POST returns 403 "csrf".
	req := httptest.NewRequest("POST", "/manage/projects/demo/delete",
		strings.NewReader(url.Values{"csrf": {m[1]}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(b.Cookie)
	rr := httptest.NewRecorder()
	b.Projects.ReposDelete(rr, req)
	if rr.Code != 303 {
		t.Errorf("delete using rendered csrf: status %d, body=%q (expect 303 redirect)", rr.Code, rr.Body.String())
	}
}
