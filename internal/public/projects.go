package public

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/penguin/blog-server/internal/content"
	"github.com/penguin/blog-server/internal/github"
)

const projectsPerPage = 12

// CacheReader is the subset of *github.Cache used by public handlers;
// narrows the coupling and simplifies testing.
type CacheReader interface {
	Get(ctx context.Context, repo string) (*github.CacheEntry, error)
	List(ctx context.Context) ([]*github.CacheEntry, error)
}

// ProjectView decorates a content.Entry with its live github.RepoInfo (via
// cache) for template consumption. Info can be nil if first sync hasn't
// landed yet; templates handle the placeholder.
type ProjectView struct {
	Entry      *content.Entry
	Info       *github.RepoInfo
	LastSynced time.Time
	SyncStale  bool   // true when last sync > 2h ago
	SyncLabel  string // human-friendly "synced X minutes ago"
	RemoteGone bool   // last sync returned not-found for the repo
	LastError  string
}

// --- List ------------------------------------------------------------------

type projectsListData struct {
	Views         []projectView // groups for the sidebar (not used in MVP)
	Views2        []string      // placeholder
	ResultCount   int
	ViewLabel     string
	Projects      []*ProjectView
	Featured      *ProjectView
	Categories    []categoryItem
	StackCloud    []tagItem
	Statuses      []statusItem
	Pager         pager
	SyncedRelTime string
}

type categoryItem struct {
	Name   string
	Count  int
	Active bool
	HREF   string
}

type statusItem struct {
	Name   string
	Value  string
	Count  int
	Active bool
	HREF   string
	Color  string
}

// just to satisfy unused-import errors if refactored later
type projectView = ProjectView

// ProjectsList handles GET /projects.
func (h *Handlers) ProjectsList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	all := h.Content.Projects().List(content.KindProject)

	wantCategory := strings.TrimSpace(q.Get("category"))
	wantStack := q["stack"]
	wantStatus := strings.TrimSpace(q.Get("status"))

	filtered := applyProjectFilters(all, wantCategory, wantStack, wantStatus)

	views := make([]*ProjectView, 0, len(filtered))
	for _, e := range filtered {
		views = append(views, h.makeProjectView(r.Context(), e))
	}

	page, totalPages, slice := paginateViews(views, atoi(q.Get("page"), 1), projectsPerPage)
	featured := firstFeatured(slice, page)
	cats, stacks, statuses := aggregateProjectFacets(all)

	data := projectsListData{
		ResultCount: len(views),
		ViewLabel:   projectsViewLabel(wantCategory, wantStatus, wantStack),
		Projects:    slice,
		Featured:    featured,
		Categories:  buildCategoryItems(cats, wantCategory),
		StackCloud:  buildStackItems(stacks, wantStack),
		Statuses:    buildStatusItems(statuses, wantStatus),
		Pager:       buildPager(page, totalPages, projectsPerPage, len(views)),
	}

	if err := h.Tpl.Render(w, r, http.StatusOK, "projects_list.html", data); err != nil {
		h.Logger.Error("projects.list.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func applyProjectFilters(in []*content.Entry, cat string, stacks []string, status string) []*content.Entry {
	out := in
	if cat != "" {
		out = filterProjectsByCategory(out, cat)
	}
	if len(stacks) > 0 {
		out = filterProjectsByStacks(out, stacks)
	}
	if status != "" {
		out = filterProjectsByStatus(out, status)
	}
	return out
}

func paginateViews(views []*ProjectView, page, perPage int) (int, int, []*ProjectView) {
	total := (len(views) + perPage - 1) / perPage
	if total == 0 {
		total = 1
	}
	if page > total {
		page = 1
	}
	start := (page - 1) * perPage
	end := start + perPage
	if end > len(views) {
		end = len(views)
	}
	return page, total, views[start:end]
}

func firstFeatured(slice []*ProjectView, page int) *ProjectView {
	if page != 1 {
		return nil
	}
	for _, pv := range slice {
		if pv.Entry.Featured {
			return pv
		}
	}
	return nil
}

func aggregateProjectFacets(all []*content.Entry) (map[string]int, map[string]int, map[string]int) {
	cats := map[string]int{}
	stacks := map[string]int{}
	statuses := map[string]int{}
	for _, e := range all {
		if e.Category != "" {
			cats[e.Category]++
		}
		for _, s := range e.Stack {
			stacks[s]++
		}
		if e.Status != "" {
			statuses[string(e.Status)]++
		}
	}
	return cats, stacks, statuses
}

// --- Detail ----------------------------------------------------------------

// ProjectDetail handles GET /projects/:slug.
func (h *Handlers) ProjectDetail(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/projects/")
	slug = strings.Trim(slug, "/")
	if slug == "" || strings.Contains(slug, "/") {
		http.NotFound(w, r)
		return
	}
	e, ok := h.Content.Projects().Get(content.KindProject, slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	pv := h.makeProjectView(r.Context(), e)

	data := map[string]any{
		"Project": pv,
	}
	if err := h.Tpl.Render(w, r, http.StatusOK, "project_detail.html", data); err != nil {
		h.Logger.Error("projects.detail.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// --- Helpers ---------------------------------------------------------------

func (h *Handlers) makeProjectView(ctx context.Context, e *content.Entry) *ProjectView {
	pv := &ProjectView{Entry: e}
	if h.GitHubCache == nil || e.Repo == "" {
		return pv
	}
	entry, err := h.GitHubCache.Get(ctx, e.Repo)
	if err != nil || entry == nil {
		return pv
	}
	pv.Info = entry.Info
	pv.LastSynced = entry.LastSyncedAt
	pv.LastError = entry.LastError
	pv.SyncLabel = relTime(entry.LastSyncedAt)
	if !entry.LastSyncedAt.IsZero() && time.Since(entry.LastSyncedAt) > 2*time.Hour {
		pv.SyncStale = true
	}
	if strings.Contains(strings.ToLower(entry.LastError), "not found") {
		pv.RemoteGone = true
	}
	return pv
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return "尚未同步"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return formatN(int(d.Minutes())) + " 分钟前"
	case d < 24*time.Hour:
		return formatN(int(d.Hours())) + " 小时前"
	default:
		return formatN(int(d.Hours()/24)) + " 天前"
	}
}

func formatN(n int) string {
	if n < 1 {
		n = 1
	}
	return intToString(n)
}

func intToString(n int) string {
	// Trivial but avoids strconv import in this file
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// --- Filters ---------------------------------------------------------------

func filterProjectsByCategory(in []*content.Entry, cat string) []*content.Entry {
	out := make([]*content.Entry, 0, len(in))
	for _, e := range in {
		if e.Category == cat {
			out = append(out, e)
		}
	}
	return out
}

func filterProjectsByStacks(in []*content.Entry, stacks []string) []*content.Entry {
	want := map[string]bool{}
	for _, s := range stacks {
		want[s] = true
	}
	out := make([]*content.Entry, 0, len(in))
next:
	for _, e := range in {
		have := map[string]bool{}
		for _, s := range e.Stack {
			have[s] = true
		}
		for s := range want {
			if !have[s] {
				continue next
			}
		}
		out = append(out, e)
	}
	return out
}

func filterProjectsByStatus(in []*content.Entry, status string) []*content.Entry {
	out := make([]*content.Entry, 0, len(in))
	for _, e := range in {
		if string(e.Status) == status {
			out = append(out, e)
		}
	}
	return out
}

// --- Sidebar builders ------------------------------------------------------

func projectsViewLabel(cat, status string, stacks []string) string {
	switch {
	case cat != "":
		return "分类：" + cat
	case len(stacks) > 0:
		return "技术栈：" + strings.Join(stacks, " + ")
	case status != "":
		return "状态：" + status
	default:
		return "全部项目"
	}
}

func buildCategoryItems(counts map[string]int, active string) []categoryItem {
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]categoryItem, 0, len(names))
	for _, n := range names {
		out = append(out, categoryItem{
			Name:   n,
			Count:  counts[n],
			Active: n == active,
			HREF:   "/projects?category=" + n,
		})
	}
	return out
}

func buildStackItems(counts map[string]int, active []string) []tagItem {
	activeSet := map[string]bool{}
	for _, a := range active {
		activeSet[a] = true
	}
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]tagItem, 0, len(names))
	for _, n := range names {
		out = append(out, tagItem{
			Name:   n,
			Count:  counts[n],
			Active: activeSet[n],
			HREF:   "/projects?stack=" + n,
		})
	}
	return out
}

func buildStatusItems(counts map[string]int, active string) []statusItem {
	items := []statusItem{
		{Name: "活跃维护", Value: "active", Color: "#35c759"},
		{Name: "正在开发", Value: "developing", Color: "#ff9f0a"},
		{Name: "已归档", Value: "archived", Color: "#8e8e93"},
	}
	for i := range items {
		items[i].Count = counts[items[i].Value]
		items[i].Active = items[i].Value == active
		items[i].HREF = "/projects?status=" + items[i].Value
	}
	return items
}

// Recently Active derivation for the home page right column.
// Top 3 non-archived projects by PushedAt (or Updated fallback) desc.
func (h *Handlers) RecentlyActiveProjects(ctx context.Context, limit int) []*ProjectView {
	all := h.Content.Projects().List(content.KindProject)
	views := make([]*ProjectView, 0, len(all))
	for _, e := range all {
		if e.Status == content.StatusArchived {
			continue
		}
		pv := h.makeProjectView(ctx, e)
		views = append(views, pv)
	}
	sort.Slice(views, func(i, j int) bool {
		ti := effectivePushedAt(views[i])
		tj := effectivePushedAt(views[j])
		return ti.After(tj)
	})
	if len(views) > limit {
		views = views[:limit]
	}
	return views
}

func effectivePushedAt(pv *ProjectView) time.Time {
	if pv.Info != nil && !pv.Info.PushedAt.IsZero() {
		return pv.Info.PushedAt
	}
	return pv.Entry.Updated
}
