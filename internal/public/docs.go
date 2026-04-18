package public

import (
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/penguin/blog-server/internal/content"
)

const docsPerPage = 10

// --- List data shapes -------------------------------------------------------

type tagItem struct {
	Name   string
	Count  int
	Active bool
	HREF   string
}

type yearItem struct {
	Year  int
	Count int
}

// catNode is a single node in the category tree. Categories can use '/' to
// nest (e.g. "后端/Go" → root '后端' with child 'Go'). Leaf nodes carry
// the actual docs; intermediate nodes can also have docs when the full path
// matches (e.g. a doc with category="后端" AND another with "后端/Go" both
// render under the 后端 subtree).
type catNode struct {
	Name     string
	FullPath string // full category string users would match, e.g. "后端/Go"
	Count    int    // docs at or below this node (subtree total)
	Own      int    // docs with Category exactly equal to FullPath
	Docs     []*content.Entry
	Children []*catNode
}

type archiveMonth struct {
	Month int
	Label string // "1月"
	Count int
	Docs  []*content.Entry
}

type archiveYear struct {
	Year   int
	Count  int
	Months []archiveMonth
}

type pagerItem struct {
	N       int
	Current bool
}

type pager struct {
	Show    bool
	Pages   []pagerItem
	Prev    int
	Next    int
	HasPrev bool
	HasNext bool
}

type docsListData struct {
	View           string // all|category|tag|archive
	ViewLabel      string
	Docs           []*content.Entry
	ResultCount    int
	TotalPublished int
	AllTags        []tagItem
	Categories     []string
	Years          []yearItem
	Pager          pager

	// View-specific payloads, populated only for their owning view so
	// templates don't need to compute these every render path.
	CategoryTree []*catNode    // view=category
	ArchiveTree  []archiveYear // view=archive
}

// --- Handler ---------------------------------------------------------------

// DocsList handles GET /docs.
func (h *Handlers) DocsList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	all := h.Content.Docs().List(content.KindDoc)

	// Start from published entries; archived only surface under the archive view.
	published := filterByStatus(all, content.StatusPublished)

	wantTags := q["tag"]
	category := strings.TrimSpace(q.Get("category"))
	yearStr := strings.TrimSpace(q.Get("year"))
	view := strings.TrimSpace(q.Get("view"))
	if view == "" {
		view = "all"
	}

	// Build tag cloud over the entire published set before filtering.
	tagCounts := map[string]int{}
	for _, e := range published {
		for _, t := range e.Tags {
			tagCounts[t]++
		}
	}
	cats := map[string]bool{}
	yearCounts := map[int]int{}
	for _, e := range published {
		if e.Category != "" {
			cats[e.Category] = true
		}
		yearCounts[e.Updated.Year()]++
	}

	// Apply filters. AND semantics for tags.
	filtered := published
	if len(wantTags) > 0 {
		filtered = filterByAllTags(filtered, wantTags)
	}
	if category != "" {
		filtered = filterByCategory(filtered, category)
	}
	if yearStr != "" {
		filtered = filterByYear(filtered, yearStr)
	}
	// Archive view: include archived entries as well when year chosen.
	if view == "archive" {
		filtered = filterByStatus(all, content.StatusPublished)
		if yearStr != "" {
			filtered = filterByYear(filtered, yearStr)
		}
	}

	// Pagination.
	page := atoi(q.Get("page"), 1)
	totalPages := (len(filtered) + docsPerPage - 1) / docsPerPage
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = 1
	}
	start := (page - 1) * docsPerPage
	end := start + docsPerPage
	if end > len(filtered) {
		end = len(filtered)
	}
	sliced := filtered[start:end]

	data := docsListData{
		View:           view,
		ViewLabel:      viewLabel(view, category, yearStr, wantTags),
		Docs:           sliced,
		ResultCount:    len(filtered),
		TotalPublished: len(published),
		AllTags:        buildTagItems(tagCounts, wantTags, q),
		Categories:     mapKeys(cats),
		Years:          buildYears(yearCounts),
		Pager:          buildPager(page, totalPages, docsPerPage, len(filtered)),
	}
	// Build the heavier per-view payloads only when that view is active.
	switch view {
	case "category":
		data.CategoryTree = buildCategoryTree(published)
	case "archive":
		data.ArchiveTree = buildArchiveTree(published)
	}

	if err := h.Tpl.Render(w, r, http.StatusOK, "docs_list.html", data); err != nil {
		h.Logger.Error("docs.list.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// --- Filters ---------------------------------------------------------------

func filterByStatus(in []*content.Entry, st content.Status) []*content.Entry {
	out := make([]*content.Entry, 0, len(in))
	for _, e := range in {
		if e.Status == st {
			out = append(out, e)
		}
	}
	return out
}

func filterByAllTags(in []*content.Entry, tags []string) []*content.Entry {
	want := map[string]bool{}
	for _, t := range tags {
		want[t] = true
	}
	out := make([]*content.Entry, 0, len(in))
next:
	for _, e := range in {
		etags := map[string]bool{}
		for _, t := range e.Tags {
			etags[t] = true
		}
		for t := range want {
			if !etags[t] {
				continue next
			}
		}
		out = append(out, e)
	}
	return out
}

func filterByCategory(in []*content.Entry, cat string) []*content.Entry {
	out := make([]*content.Entry, 0, len(in))
	for _, e := range in {
		if e.Category == cat {
			out = append(out, e)
		}
	}
	return out
}

func filterByYear(in []*content.Entry, y string) []*content.Entry {
	want := atoi(y, 0)
	if want == 0 {
		return in
	}
	out := make([]*content.Entry, 0, len(in))
	for _, e := range in {
		if e.Updated.Year() == want {
			out = append(out, e)
		}
	}
	return out
}

// --- Builders --------------------------------------------------------------

func viewLabel(view, category, year string, tags []string) string {
	switch view {
	case "category":
		if category != "" {
			return "目录：" + category
		}
		return "目录"
	case "tag":
		if len(tags) > 0 {
			return "标签：" + strings.Join(tags, " + ")
		}
		return "标签"
	case "archive":
		if year != "" {
			return "归档：" + year
		}
		return "归档"
	default:
		if category != "" {
			return "目录：" + category
		}
		if len(tags) > 0 {
			return "标签：" + strings.Join(tags, " + ")
		}
		if year != "" {
			return "年份：" + year
		}
		return "全部"
	}
}

func buildTagItems(counts map[string]int, active []string, q url.Values) []tagItem {
	activeSet := map[string]bool{}
	for _, a := range active {
		activeSet[a] = true
	}
	names := mapKeys(counts)
	sort.Strings(names)
	out := make([]tagItem, 0, len(names))
	for _, n := range names {
		// Toggle link for this tag.
		next := url.Values{}
		for k, vs := range q {
			if k == "tag" {
				continue
			}
			for _, v := range vs {
				next.Add(k, v)
			}
		}
		for _, t := range active {
			if t != n {
				next.Add("tag", t)
			}
		}
		if !activeSet[n] {
			next.Add("tag", n)
		}
		href := "/docs"
		if s := next.Encode(); s != "" {
			href += "?" + s
		}
		out = append(out, tagItem{
			Name:   n,
			Count:  counts[n],
			Active: activeSet[n],
			HREF:   href,
		})
	}
	return out
}

func buildYears(counts map[int]int) []yearItem {
	out := make([]yearItem, 0, len(counts))
	for y, c := range counts {
		out = append(out, yearItem{Year: y, Count: c})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Year > out[j].Year })
	return out
}

func buildPager(page, total, perPage, count int) pager {
	if total <= 1 {
		return pager{}
	}
	pages := make([]pagerItem, 0, total)
	for i := 1; i <= total; i++ {
		pages = append(pages, pagerItem{N: i, Current: i == page})
	}
	return pager{
		Show:    true,
		Pages:   pages,
		Prev:    page - 1,
		Next:    page + 1,
		HasPrev: page > 1,
		HasNext: page < total,
	}
}

func mapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// buildCategoryTree groups published docs into a hierarchy keyed by Category,
// splitting on '/' so "后端/Go" nests under "后端". Docs without a category
// are bucketed under a synthetic "未分类" node at the root.
func buildCategoryTree(published []*content.Entry) []*catNode {
	const unsorted = "未分类"
	roots := map[string]*catNode{}
	// Walk each doc; ensure each path prefix exists and attach the doc at
	// its exact full-path node.
	for _, e := range published {
		cat := strings.TrimSpace(e.Category)
		if cat == "" {
			cat = unsorted
		}
		segs := strings.Split(cat, "/")
		for i := range segs {
			segs[i] = strings.TrimSpace(segs[i])
		}
		// Walk/create.
		var prev *catNode
		for i, seg := range segs {
			if seg == "" {
				continue
			}
			full := strings.Join(segs[:i+1], "/")
			var node *catNode
			if prev == nil {
				node = roots[seg]
				if node == nil {
					node = &catNode{Name: seg, FullPath: full}
					roots[seg] = node
				}
			} else {
				for _, c := range prev.Children {
					if c.Name == seg {
						node = c
						break
					}
				}
				if node == nil {
					node = &catNode{Name: seg, FullPath: full}
					prev.Children = append(prev.Children, node)
				}
			}
			node.Count++
			if i == len(segs)-1 {
				node.Own++
				node.Docs = append(node.Docs, e)
			}
			prev = node
		}
	}
	// Flatten + sort by name at each level.
	out := make([]*catNode, 0, len(roots))
	for _, n := range roots {
		sortCatNode(n)
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func sortCatNode(n *catNode) {
	sort.Slice(n.Docs, func(i, j int) bool { return n.Docs[i].Updated.After(n.Docs[j].Updated) })
	for _, c := range n.Children {
		sortCatNode(c)
	}
	sort.Slice(n.Children, func(i, j int) bool { return n.Children[i].Name < n.Children[j].Name })
}

// buildArchiveTree groups published docs by Updated year/month, newest first.
// Each level carries a Count so the template can surface "N 篇" at year,
// month, and doc levels without recomputing.
func buildArchiveTree(published []*content.Entry) []archiveYear {
	byYM := map[int]map[int][]*content.Entry{}
	for _, e := range published {
		y, m := e.Updated.Year(), int(e.Updated.Month())
		if byYM[y] == nil {
			byYM[y] = map[int][]*content.Entry{}
		}
		byYM[y][m] = append(byYM[y][m], e)
	}
	years := make([]archiveYear, 0, len(byYM))
	for y, months := range byYM {
		ay := archiveYear{Year: y}
		for m, docs := range months {
			sort.Slice(docs, func(i, j int) bool { return docs[i].Updated.After(docs[j].Updated) })
			ay.Months = append(ay.Months, archiveMonth{
				Month: m, Label: monthLabel(m), Count: len(docs), Docs: docs,
			})
			ay.Count += len(docs)
		}
		sort.Slice(ay.Months, func(i, j int) bool { return ay.Months[i].Month > ay.Months[j].Month })
		years = append(years, ay)
	}
	sort.Slice(years, func(i, j int) bool { return years[i].Year > years[j].Year })
	return years
}

func monthLabel(m int) string {
	if m < 1 || m > 12 {
		return ""
	}
	return [...]string{"1月", "2月", "3月", "4月", "5月", "6月", "7月", "8月", "9月", "10月", "11月", "12月"}[m-1]
}
