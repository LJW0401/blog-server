package public_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Smoke: category view renders the tree and article counts.
func TestDocsView_Smoke_CategoryTreeRenders(t *testing.T) {
	h := setup(t, map[string]string{
		"a": "---\ntitle: A\nslug: a\nupdated: 2026-04-10\nstatus: published\ncategory: 后端\n---\nbody\n",
		"b": "---\ntitle: B\nslug: b\nupdated: 2026-04-11\nstatus: published\ncategory: 后端/Go\n---\nbody\n",
		"c": "---\ntitle: C\nslug: c\nupdated: 2026-04-12\nstatus: published\ncategory: 前端\n---\nbody\n",
	}, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/docs?view=category", nil)
	h.DocsList(rr, req)
	body := rr.Body.String()
	for _, want := range []string{
		`class="tree-view"`,
		"后端",  // category
		"Go",  // nested
		"前端",  // sibling category
		"2 篇", // 后端 subtree has 2
		"1 篇", // 前端 + nested Go each 1
		`href="/docs/a"`,
		`href="/docs/b"`,
		`href="/docs/c"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("category view missing %q", want)
		}
	}
}

// Smoke: tag view shows every tag with count.
func TestDocsView_Smoke_TagCatalogRenders(t *testing.T) {
	h := setup(t, map[string]string{
		"a": "---\ntitle: A\nslug: a\nupdated: 2026-04-10\nstatus: published\ntags: [go, web]\n---\nbody\n",
		"b": "---\ntitle: B\nslug: b\nupdated: 2026-04-11\nstatus: published\ntags: [go]\n---\nbody\n",
	}, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/docs?view=tag", nil)
	h.DocsList(rr, req)
	body := rr.Body.String()
	for _, want := range []string{
		`class="tag-catalog"`,
		"go", "web",
		"2 篇", // go appears twice
		"1 篇", // web appears once
	} {
		if !strings.Contains(body, want) {
			t.Errorf("tag view missing %q", want)
		}
	}
}

// Smoke: archive view renders year > month nesting with <details>.
func TestDocsView_Smoke_ArchiveTreeRenders(t *testing.T) {
	h := setup(t, map[string]string{
		"a": "---\ntitle: A\nslug: a\nupdated: 2026-04-10\nstatus: published\n---\nbody\n",
		"b": "---\ntitle: B\nslug: b\nupdated: 2026-03-11\nstatus: published\n---\nbody\n",
		"c": "---\ntitle: C\nslug: c\nupdated: 2025-12-01\nstatus: published\n---\nbody\n",
	}, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/docs?view=archive", nil)
	h.DocsList(rr, req)
	body := rr.Body.String()
	for _, want := range []string{
		`class="archive-view"`,
		"2026", "2025", "4月", "3月", "12月",
		`<details class="archive-year">`,
		`<details class="archive-month">`,
		`href="/docs/a"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("archive view missing %q", want)
		}
	}
	// Year ordering: 2026 must appear before 2025.
	i1 := strings.Index(body, "2026")
	i2 := strings.Index(body, "2025")
	if i1 < 0 || i2 < 0 || i1 > i2 {
		t.Errorf("archive year ordering wrong: 2026@%d, 2025@%d", i1, i2)
	}
}

// Edge (空/缺失输入)：无分类的文档归入"未分类"，无文档时每个 view 显示占位。
func TestDocsView_Edge_UncategorizedDocsBucket(t *testing.T) {
	h := setup(t, map[string]string{
		"a": "---\ntitle: A\nslug: a\nupdated: 2026-04-10\nstatus: published\n---\nbody\n",
	}, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/docs?view=category", nil)
	h.DocsList(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "未分类") {
		t.Errorf("uncategorized bucket missing")
	}
}

// Edge (边界值 + 无数据)：三个 view 在没有任何 published 文档时显示友好占位，不爆。
func TestDocsView_Edge_EmptyContent(t *testing.T) {
	h := setup(t, nil, nil)
	for _, v := range []string{"category", "tag", "archive"} {
		t.Run(v, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/docs?view="+v, nil)
			h.DocsList(rr, req)
			if rr.Code != 200 {
				t.Fatalf("status %d", rr.Code)
			}
			if !strings.Contains(rr.Body.String(), "暂无") {
				t.Errorf("empty state missing 暂无 placeholder for view=%s", v)
			}
		})
	}
}
