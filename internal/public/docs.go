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
