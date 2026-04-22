package public

import (
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/penguin/blog-server/internal/content"
)

// PortfolioDefaultCover is the site-hosted fallback cover served when an
// entry has no cover field set. Keep in sync with
// internal/assets/static/images/portfolio-default.svg.
const PortfolioDefaultCover = "/static/images/portfolio-default.svg"

var portfolioSlugRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// PortfolioPageSize is the number of entries per portfolio list page.
const PortfolioPageSize = 20

// PortfolioList handles GET /portfolio.
//
// Query params:
//   - tag=<name>: filter to entries tagged with <name> (exact match)
//   - page=<n>:   pagination; invalid values normalize to 1
//
// Draft entries are visible only when the caller is "logged in" (preview
// header), matching the docs list behavior.
func (h *Handlers) PortfolioList(w http.ResponseWriter, r *http.Request) {
	loggedIn := isLoggedIn(r)
	all := h.Content.Portfolios().List(content.KindPortfolio)
	// Status filter
	visible := make([]*content.Entry, 0, len(all))
	for _, e := range all {
		switch e.Status {
		case content.StatusPublished, content.StatusArchived:
			visible = append(visible, e)
		case content.StatusDraft:
			if loggedIn {
				visible = append(visible, e)
			}
		}
	}
	// Tag filter (single tag exact match keeps the UI simple)
	q := r.URL.Query()
	tag := strings.TrimSpace(q.Get("tag"))
	if tag != "" {
		filtered := make([]*content.Entry, 0, len(visible))
		for _, e := range visible {
			for _, t := range e.Tags {
				if t == tag {
					filtered = append(filtered, e)
					break
				}
			}
		}
		visible = filtered
	}
	sortPortfolios(visible)

	// Pagination
	page := atoi(q.Get("page"), 1)
	if page < 1 {
		page = 1
	}
	total := len(visible)
	start := (page - 1) * PortfolioPageSize
	end := start + PortfolioPageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	items := visible[start:end]
	totalPages := (total + PortfolioPageSize - 1) / PortfolioPageSize
	if totalPages == 0 {
		totalPages = 1
	}

	// Decorate items with resolved cover URL for the template.
	views := make([]portfolioListView, len(items))
	for i, e := range items {
		cv := e.Cover
		if strings.TrimSpace(cv) == "" {
			cv = PortfolioDefaultCover
		}
		views[i] = portfolioListView{Entry: e, Cover: cv}
	}

	// Build the tag cloud independent of docs' tag pool.
	tagCounts := map[string]int{}
	for _, e := range visible {
		for _, t := range e.Tags {
			tagCounts[t]++
		}
	}

	data := map[string]any{
		"Items":      views,
		"Page":       page,
		"TotalPages": totalPages,
		"Total":      total,
		"Tag":        tag,
		"TagCounts":  tagCounts,
		"HasPrev":    page > 1,
		"HasNext":    page < totalPages,
		"PrevPage":   page - 1,
		"NextPage":   page + 1,
	}
	if err := h.Tpl.Render(w, r, http.StatusOK, "portfolio_list.html", data); err != nil {
		h.Logger.Error("portfolio.list.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

type portfolioListView struct {
	Entry *content.Entry
	Cover string
}

// portfolioHomeCard is the precomputed data for a homepage featured card:
// entry fields plus the resolved cover URL (fallback to default SVG when
// unset). The template consumes .Entry.Intro for the bottom-half body.
type portfolioHomeCard struct {
	Entry *content.Entry
	Cover string
}

// PortfolioDetail handles GET /portfolio/:slug.
func (h *Handlers) PortfolioDetail(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/portfolio/")
	slug = strings.Trim(slug, "/")
	if slug == "" || strings.Contains(slug, "/") {
		h.NotFound(w, r)
		return
	}
	if !portfolioSlugRe.MatchString(slug) {
		h.NotFound(w, r)
		return
	}
	e, ok := h.Content.Portfolios().Get(content.KindPortfolio, slug)
	if !ok {
		h.NotFound(w, r)
		return
	}
	loggedIn := isLoggedIn(r)
	switch e.Status {
	case content.StatusDraft:
		if !loggedIn {
			h.NotFound(w, r)
			return
		}
	case content.StatusPublished, content.StatusArchived:
		// ok
	default:
		h.NotFound(w, r)
		return
	}

	cover := e.Cover
	if strings.TrimSpace(cover) == "" {
		cover = PortfolioDefaultCover
	}

	data := map[string]any{
		"Entry":    e,
		"Cover":    cover,
		"IsDraft":  e.Status == content.StatusDraft,
		"Archived": e.Status == content.StatusArchived,
	}
	if err := h.Tpl.Render(w, r, http.StatusOK, "portfolio_detail.html", data); err != nil {
		h.Logger.Error("portfolio.detail.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// publishedPortfolios returns only published portfolios sorted by (Order ASC,
// Updated DESC). Callers needing draft preview should overlay their own
// filter.
func publishedPortfolios(all []*content.Entry) []*content.Entry {
	out := make([]*content.Entry, 0, len(all))
	for _, e := range all {
		if e.Status == content.StatusPublished {
			out = append(out, e)
		}
	}
	sortPortfolios(out)
	return out
}

// sortPortfolios applies the canonical portfolio ordering in place.
func sortPortfolios(list []*content.Entry) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Order != list[j].Order {
			return list[i].Order < list[j].Order
		}
		return list[i].Updated.After(list[j].Updated)
	})
}
