package admin_test

import (
	"os"
	"strings"
	"testing"
)

// Smoke: /manage/docs 顶部展示扁平统计条；计数匹配种子数据。
// 豁免异常：纯聚合计数无外部输入；store 为空 → 0 条兜底由 portfolio 用例同结构覆盖。
func TestDocsList_Smoke_StatsBarAndCounts(t *testing.T) {
	b := crudSetup(t)
	seedDoc(t, b, "pub1", "published")
	seedDoc(t, b, "pub2", "published")
	seedDoc(t, b, "dr1", "draft")
	seedDoc(t, b, "arc1", "archived")
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	w := b.authedGet(t, "/manage/docs", b.Docs.DocsList)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `class="admin-stats-bar"`) {
		t.Error("admin-stats-bar markup missing")
	}
	for _, want := range []string{
		"published <strong>2</strong>",
		"draft <strong>1</strong>",
		"archived <strong>1</strong>",
		"共 4 条",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("docs list missing count: %q", want)
		}
	}
}

// Smoke: /manage/repos 顶部展示扁平统计条；使用 active/developing/archived 三态。
func TestReposList_Smoke_StatsBarAndCounts(t *testing.T) {
	b := crudSetup(t)
	seedProject(t, b, "svc", "active")
	seedProject(t, b, "beta", "developing")
	seedProject(t, b, "beta2", "developing")
	seedProject(t, b, "shelved", "archived")
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	w := b.authedGet(t, "/manage/repos", b.Projects.ReposList)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `class="admin-stats-bar"`) {
		t.Error("admin-stats-bar markup missing")
	}
	for _, want := range []string{
		"active <strong>1</strong>",
		"developing <strong>2</strong>",
		"archived <strong>1</strong>",
		"共 4 条",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("repos list missing count: %q", want)
		}
	}
}

// Smoke: 零条目时三块列表仍渲染扁平条且显示 "共 0 条"。
func TestAdminLists_Smoke_StatsBarRendersWithZeroEntries(t *testing.T) {
	b := crudSetup(t)
	cases := []struct {
		name string
		path string
		run  func()
	}{
		{"docs", "/manage/docs", func() {}},
		{"repos", "/manage/repos", func() {}},
	}
	for _, c := range cases {
		var w = func() string {
			switch c.path {
			case "/manage/docs":
				return b.authedGet(t, c.path, b.Docs.DocsList).Body.String()
			default:
				return b.authedGet(t, c.path, b.Projects.ReposList).Body.String()
			}
		}()
		if !strings.Contains(w, `class="admin-stats-bar"`) {
			t.Errorf("%s: bar missing", c.name)
		}
		if !strings.Contains(w, "共 0 条") {
			t.Errorf("%s: zero total not shown", c.name)
		}
	}
}

// --- seed helpers ---

func seedDoc(t *testing.T, b *crudBundle, slug, status string) {
	t.Helper()
	md := "---\ntitle: " + slug + "\nslug: " + slug + "\nstatus: " + status + "\nupdated: 2026-04-19\ncreated: 2026-04-19\n---\n正文\n"
	path := b.DataDir + "/content/docs/" + slug + ".md"
	if err := os.WriteFile(path, []byte(md), 0o600); err != nil {
		t.Fatal(err)
	}
}

func seedProject(t *testing.T, b *crudBundle, slug, status string) {
	t.Helper()
	md := "---\ntitle: " + slug + "\nslug: " + slug + "\nrepo: foo/" + slug + "\ndisplay_name: " + slug + "\nstatus: " + status + "\nupdated: 2026-04-19\ncreated: 2026-04-19\n---\n描述\n"
	path := b.DataDir + "/content/projects/" + slug + ".md"
	if err := os.WriteFile(path, []byte(md), 0o600); err != nil {
		t.Fatal(err)
	}
}
