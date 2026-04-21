package admin_test

import (
	"strings"
	"testing"
)

// WI-3.15 Smoke: Dashboard 作品集入口卡。
// 豁免异常：纯聚合计数无外部输入。未登录 → middleware（WI-3.6 覆盖）；
// store 异常 → WI-1.3 覆盖；0 条边界 → 本 smoke 覆盖。
func TestDashboard_Smoke_PortfolioCardAndCounts(t *testing.T) {
	b := crudSetup(t)
	// Seed 1 published, 1 draft, 1 archived.
	seedPortfolioFile(t, b, "p-pub", portfolioMD("p-pub", "P Pub", false, 0))
	seedPortfolioFile(t, b, "p-draft", strings.Replace(
		portfolioMD("p-draft", "P Draft", false, 0),
		"status: published", "status: draft", 1))
	seedPortfolioFile(t, b, "p-arc", strings.Replace(
		portfolioMD("p-arc", "P Arc", false, 0),
		"status: published", "status: archived", 1))
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	// b.Admin.Content is not wired in crudSetup — inject for this test.
	b.Admin.Content = b.Content

	w := b.authedGet(t, "/manage", b.Admin.Dashboard)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	body := w.Body.String()
	// Card heading + link present
	if !strings.Contains(body, "作品集</h3>") {
		t.Error("dashboard card heading missing")
	}
	if !strings.Contains(body, `href="/manage/portfolio/new"`) {
		t.Error("'新建作品' link missing")
	}
	if !strings.Contains(body, `href="/manage/portfolio"`) {
		t.Error("'作品集管理' nav link missing")
	}
	// Counts match seeded data
	for _, want := range []string{
		"published <strong>1</strong>",
		"draft <strong>1</strong>",
		"archived <strong>1</strong>",
		"共 3 条",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing count: %q; body:\n%s", want, body)
		}
	}
}

func TestDashboard_Smoke_PortfolioCardRendersWithZeroEntries(t *testing.T) {
	b := crudSetup(t)
	b.Admin.Content = b.Content
	w := b.authedGet(t, "/manage", b.Admin.Dashboard)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	body := w.Body.String()
	// Card still renders with 0s.
	if !strings.Contains(body, "作品集</h3>") {
		t.Error("card should render even with 0 entries")
	}
	if !strings.Contains(body, "共 0 条") {
		t.Error("zero total not shown")
	}
}
