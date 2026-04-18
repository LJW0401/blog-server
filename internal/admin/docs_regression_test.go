package admin_test

import (
	"net/url"
	"strings"
	"testing"
)

// Reproduces: user clicks 新建文档 then 保存 without editing. Default frontmatter
// has empty slug → server returns 400 but browser sees generic 400 page
// instead of the editor form + error message.
func TestDocsEdit_Bug_DefaultFormSaveShowsEditorNot400(t *testing.T) {
	b := crudSetup(t)
	// Body matches what NewDoc() serves (default frontmatter — slug is empty).
	md := "---\n" +
		"title: \n" +
		"slug: \n" +
		"tags: []\n" +
		"category: \n" +
		"created: 2026-04-19\n" +
		"updated: 2026-04-19\n" +
		"status: draft\n" +
		"featured: false\n" +
		"---\n\n" +
		"正文从这里开始。\n"
	w := b.authedPost(t, "/manage/docs/new", url.Values{"csrf": {b.CSRF}, "body": {md}}, b.Docs.SaveDoc)

	// Status 400 is correct — this is a validation error.
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	// But Content-Type must be HTML, not empty/text.
	ct := w.Result().Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}

	// And body must be the rendered editor template, not empty/broken.
	body := w.Body.String()
	if !strings.Contains(body, "<form") {
		t.Errorf("response body missing <form>: %q", body[:min(200, len(body))])
	}
	if !strings.Contains(body, "slug 不能为空") {
		t.Errorf("response body missing error message: %q", body[:min(200, len(body))])
	}
}
