package diary_test

import (
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func promoteReq(t *testing.T, form url.Values, cookie interface{}) *httptest.ResponseRecorder {
	t.Helper()
	// 占位：实际由调用者构造，见下方用例
	return nil
}

// --- Smoke (WI-3.6) --------------------------------------------------------

func TestPromote_Smoke_CopiesDiaryIntoDocsAsDraft(t *testing.T) {
	h, dir, cookie, csrf := setupHandlersWithCSRF(t)
	_ = h.Store.Put("2026-04-19", "关于可靠性的随记\n\n第二段文字")

	form := url.Values{
		"date":     {"2026-04-19"},
		"title":    {"关于可靠性"},
		"slug":     {"on-reliability"},
		"category": {"工程笔记"},
		"csrf":     {csrf},
	}
	req := httptest.NewRequest("POST", "/diary/api/promote", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	h.APIPromote(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d, body: %s", rr.Code, rr.Body.String())
	}

	// content/docs/on-reliability.md 应该存在
	docPath := filepath.Join(dir, "content", "docs", "on-reliability.md")
	b, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("promoted doc missing: %v", err)
	}
	got := string(b)
	for _, want := range []string{
		`title: 关于可靠性`,
		`slug: on-reliability`,
		`category: 工程笔记`,
		`status: draft`,
		`关于可靠性的随记`,
		`第二段文字`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("promoted doc missing fragment %q; full:\n%s", want, got)
		}
	}

	// 日记原件未变
	diaryBody, exists, _ := h.Store.Get("2026-04-19")
	if !exists {
		t.Error("original diary should still exist")
	}
	if !strings.Contains(diaryBody, "关于可靠性的随记") {
		t.Errorf("diary body corrupted: %q", diaryBody)
	}
}

// --- 异常 / 边界 (WI-3.7) --------------------------------------------------

// slug 冲突：目标已存在 → 409，不覆盖原文件。
func TestPromote_Edge_SlugConflictReturns409AndDoesNotOverwrite(t *testing.T) {
	h, dir, cookie, csrf := setupHandlersWithCSRF(t)
	_ = h.Store.Put("2026-04-19", "new body")

	// 预先放一份同 slug 的 docs
	docsDir := filepath.Join(dir, "content", "docs")
	_ = os.MkdirAll(docsDir, 0o755)
	conflictPath := filepath.Join(docsDir, "x.md")
	existing := "---\ntitle: 原有\nslug: x\nstatus: published\n---\nORIGINAL\n"
	_ = os.WriteFile(conflictPath, []byte(existing), 0o644)

	form := url.Values{
		"date":  {"2026-04-19"},
		"title": {"New"},
		"slug":  {"x"},
		"csrf":  {csrf},
	}
	req := httptest.NewRequest("POST", "/diary/api/promote", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	h.APIPromote(rr, req)

	if rr.Code != 409 {
		t.Errorf("status = %d, want 409", rr.Code)
	}
	// 原文件未变
	b, _ := os.ReadFile(conflictPath)
	if !strings.Contains(string(b), "ORIGINAL") {
		t.Errorf("existing doc overwritten; body: %s", b)
	}
	// 日记原件也未变
	diaryBody, exists, _ := h.Store.Get("2026-04-19")
	if !exists || !strings.Contains(diaryBody, "new body") {
		t.Errorf("diary body corrupted: exists=%v body=%q", exists, diaryBody)
	}
}

// 非法 slug：含 /、空格、大写、空字符串 → 400。
func TestPromote_Edge_InvalidSlugReturns400(t *testing.T) {
	h, _, cookie, csrf := setupHandlersWithCSRF(t)
	_ = h.Store.Put("2026-04-19", "body")

	for _, s := range []string{
		"",
		"has space",
		"UPPER",
		"slash/injection",
		"..",
		"-leading-dash",
		"trailing-",
		"中文",
	} {
		form := url.Values{
			"date":  {"2026-04-19"},
			"title": {"t"},
			"slug":  {s},
			"csrf":  {csrf},
		}
		req := httptest.NewRequest("POST", "/diary/api/promote", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "test/ua")
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		h.APIPromote(rr, req)
		if rr.Code != 400 {
			t.Errorf("slug=%q status = %d, want 400", s, rr.Code)
		}
	}
}

// 非法 title：空 → 400。
func TestPromote_Edge_EmptyTitleReturns400(t *testing.T) {
	h, _, cookie, csrf := setupHandlersWithCSRF(t)
	_ = h.Store.Put("2026-04-19", "body")

	form := url.Values{
		"date":  {"2026-04-19"},
		"title": {""},
		"slug":  {"x"},
		"csrf":  {csrf},
	}
	req := httptest.NewRequest("POST", "/diary/api/promote", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	h.APIPromote(rr, req)
	if rr.Code != 400 {
		t.Errorf("empty title status = %d, want 400", rr.Code)
	}
}

// 日记不存在 → 404。
func TestPromote_Edge_DiaryNotFoundReturns404(t *testing.T) {
	h, _, cookie, csrf := setupHandlersWithCSRF(t)
	form := url.Values{
		"date":  {"2026-04-19"},
		"title": {"t"},
		"slug":  {"x"},
		"csrf":  {csrf},
	}
	req := httptest.NewRequest("POST", "/diary/api/promote", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	h.APIPromote(rr, req)
	if rr.Code != 404 {
		t.Errorf("no-diary status = %d, want 404", rr.Code)
	}
}

// 重复转正同一日记到不同 slug → 两份独立的 docs；原日记不变；docs frontmatter
// 不含任何反向引用（需求 2.5.2 + 架构决策 6）。
func TestPromote_Edge_MultiplePromotionsProduceIndependentDocs(t *testing.T) {
	h, dir, cookie, csrf := setupHandlersWithCSRF(t)
	_ = h.Store.Put("2026-04-19", "shared body")

	for _, slug := range []string{"variant-a", "variant-b"} {
		form := url.Values{
			"date":  {"2026-04-19"},
			"title": {"Variant"},
			"slug":  {slug},
			"csrf":  {csrf},
		}
		req := httptest.NewRequest("POST", "/diary/api/promote", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "test/ua")
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		h.APIPromote(rr, req)
		if rr.Code != 200 {
			t.Fatalf("slug=%s status=%d body=%s", slug, rr.Code, rr.Body.String())
		}
	}

	for _, slug := range []string{"variant-a", "variant-b"} {
		b, err := os.ReadFile(filepath.Join(dir, "content", "docs", slug+".md"))
		if err != nil {
			t.Errorf("doc %s missing: %v", slug, err)
			continue
		}
		s := string(b)
		if !strings.Contains(s, "shared body") {
			t.Errorf("doc %s body missing: %s", slug, s)
		}
		// 验证 frontmatter 不包含任何指向日记的字段
		for _, forbidden := range []string{"source_diary_date", "source:", "diary:", "diary_date"} {
			if strings.Contains(s, forbidden) {
				t.Errorf("doc %s leaked reverse reference %q; frontmatter:\n%s", slug, forbidden, s)
			}
		}
	}

	// 日记原件未变
	diaryBody, exists, _ := h.Store.Get("2026-04-19")
	if !exists || diaryBody != "shared body" {
		t.Errorf("original diary mutated; exists=%v body=%q", exists, diaryBody)
	}
}

// 非法 date 或无 csrf 等复用 save 的拒绝路径
func TestPromote_Edge_InvalidDateOrCSRF(t *testing.T) {
	h, _, cookie, _ := setupHandlersWithCSRF(t)

	// 非法日期
	form := url.Values{
		"date":  {"../etc/passwd"},
		"title": {"t"},
		"slug":  {"x"},
		"csrf":  {"any"},
	}
	req := httptest.NewRequest("POST", "/diary/api/promote", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	h.APIPromote(rr, req)
	// csrf 先被拒（because invalid），也可能 date 先被拒——都应非 200
	if rr.Code == 200 {
		t.Errorf("malicious inputs accepted, status=200")
	}
	// 确保没有误创建文件
	matches, _ := filepath.Glob(filepath.Join(h.DocsRoot, "*.md"))
	if len(matches) != 0 {
		t.Errorf("malicious promote leaked file: %v", matches)
	}

	_ = promoteReq // avoid unused helper warning
}
