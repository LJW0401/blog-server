package public_test

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func fixtureProjects() map[string]string {
	return map[string]string{
		"a": "---\nslug: a\nrepo: o/a\ndisplay_name: A\nstatus: active\ncategory: 后端服务\ncreated: 2026-04-01\nupdated: 2026-04-01\n---\n",
		"b": "---\nslug: b\nrepo: o/b\ndisplay_name: B\nstatus: active\ncategory: 开发者工具\ncreated: 2026-04-01\nupdated: 2026-04-01\n---\n",
		"c": "---\nslug: c\nrepo: o/c\ndisplay_name: C\nstatus: active\ncategory: 后端服务\ncreated: 2026-04-01\nupdated: 2026-04-01\n---\n",
	}
}

// Smoke: clicking category "后端服务" shows only projects in that category.
func TestProjectsList_Smoke_CategoryFiltersResults(t *testing.T) {
	h := setup(t, nil, fixtureProjects())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/projects?"+url.Values{"category": {"后端服务"}}.Encode(), nil)
	h.ProjectsList(rr, req)
	body := rr.Body.String()

	if !strings.Contains(body, `href="/projects/a"`) || !strings.Contains(body, `href="/projects/c"`) {
		t.Errorf("category filter dropped projects A/C in 后端服务")
	}
	if strings.Contains(body, `href="/projects/b"`) {
		t.Errorf("category filter did NOT drop project B in 开发者工具")
	}
	if !strings.Contains(body, "分类：后端服务") {
		t.Errorf("toolbar label missing category name")
	}
}

// Smoke: "全部项目" sidebar entry is active when no filters are applied.
func TestProjectsList_Smoke_AllProjectsActiveWithoutFilter(t *testing.T) {
	h := setup(t, nil, fixtureProjects())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/projects", nil)
	h.ProjectsList(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, `<a href="/projects" class="active">全部项目`) {
		t.Errorf("全部项目 not marked active when no filter applied")
	}
}

// Edge（非法/未知输入）：category 查询值不匹配任何项目时返回空结果，页面仍可用。
func TestProjectsList_Edge_UnknownCategoryZeroResults(t *testing.T) {
	h := setup(t, nil, fixtureProjects())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/projects?"+url.Values{"category": {"不存在"}}.Encode(), nil)
	h.ProjectsList(rr, req)
	body := rr.Body.String()
	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	if !strings.Contains(body, "暂无项目") {
		t.Errorf("empty state missing for unknown category")
	}
	// "全部项目" must NOT be active when an (even unmatched) category is set.
	if strings.Contains(body, `<a href="/projects" class="active">全部项目`) {
		t.Errorf("全部项目 should not be active when a filter is applied (even if empty)")
	}
}

// Edge（组合）：多 filter（category + status）共存时仍只返回同时满足的项目。
func TestProjectsList_Edge_CategoryAndStatusCombine(t *testing.T) {
	h := setup(t, nil, map[string]string{
		"a": "---\nslug: a\nrepo: o/a\ndisplay_name: A\nstatus: active\ncategory: 后端服务\ncreated: 2026-04-01\nupdated: 2026-04-01\n---\n",
		"c": "---\nslug: c\nrepo: o/c\ndisplay_name: C\nstatus: archived\ncategory: 后端服务\ncreated: 2026-04-01\nupdated: 2026-04-01\n---\n",
	})
	rr := httptest.NewRecorder()
	q := url.Values{"category": {"后端服务"}, "status": {"active"}}
	req := httptest.NewRequest("GET", "/projects?"+q.Encode(), nil)
	h.ProjectsList(rr, req)
	body := rr.Body.String()

	if !strings.Contains(body, `href="/projects/a"`) {
		t.Errorf("A (active+后端) should be included")
	}
	if strings.Contains(body, `href="/projects/c"`) {
		t.Errorf("C (archived) should be filtered out")
	}
}
