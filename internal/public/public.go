// Package public wires all unauthenticated HTTP handlers: home, docs list,
// doc detail, etc. Handlers are composed under a chi router by the caller.
package public

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/penguin/blog-server/internal/content"
	"github.com/penguin/blog-server/internal/render"
)

// SiteSettings is the static/default view of the public site identity shown
// in the hero and footer until M4 admin-editable settings replace the source
// of truth.
type SiteSettings struct {
	Name       string
	Tagline    string
	Location   string
	Direction  string
	Status     string
	ContactQQ  string
	MediaLinks []MediaLink
}

// MediaLink points at one of Penguin's social profiles.
type MediaLink struct {
	Platform string
	URL      string
}

// DefaultSettings returns placeholder values for the MVP before M5 lands.
func DefaultSettings() SiteSettings {
	return SiteSettings{
		Name:      "Penguin",
		Tagline:   "一名热衷于开源与技术写作的开发者，探索代码与思考之间的联系。",
		Location:  "中国",
		Direction: "后端 / 工程化 / 开发者工具",
		Status:    "活跃维护若干个人项目",
		ContactQQ: "772436864",
		MediaLinks: []MediaLink{
			{Platform: "B站"},
			{Platform: "抖音"},
			{Platform: "小红书"},
		},
	}
}

// Handlers bundles the dependencies needed by all public routes.
type Handlers struct {
	Content     *content.Store
	Tpl         *render.Templates
	GitHubCache CacheReader
	Settings    func() SiteSettings
	Logger      *slog.Logger
}

// NewHandlers constructs a Handlers with safe defaults.
func NewHandlers(cs *content.Store, tpl *render.Templates, logger *slog.Logger) *Handlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handlers{
		Content:  cs,
		Tpl:      tpl,
		Logger:   logger,
		Settings: DefaultSettings,
	}
}

// --- Helpers ---------------------------------------------------------------

// pickFeatured returns up to `limit` entries following the Mixed rule:
// featured=true + status=published first (sorted by updated desc), then
// published non-featured to fill the remainder. Archived and draft are
// excluded. Stable.
func pickFeatured(all []*content.Entry, limit int) []*content.Entry {
	var featured, rest []*content.Entry
	for _, e := range all {
		if e.Status != content.StatusPublished {
			continue
		}
		if e.Featured {
			featured = append(featured, e)
		} else {
			rest = append(rest, e)
		}
	}
	out := make([]*content.Entry, 0, limit)
	for _, e := range featured {
		if len(out) == limit {
			return out
		}
		out = append(out, e)
	}
	for _, e := range rest {
		if len(out) == limit {
			return out
		}
		out = append(out, e)
	}
	return out
}

// --- Handlers --------------------------------------------------------------

// Home handles GET /.
func (h *Handlers) Home(w http.ResponseWriter, r *http.Request) {
	docs := h.Content.Docs().List(content.KindDoc)
	projs := h.Content.Projects().List(content.KindProject)

	data := map[string]any{
		"Settings":         h.Settings(),
		"FeaturedDocs":     pickFeatured(docs, 4),
		"FeaturedProjects": pickFeatured(projs, 3),
		// Recently Active is a derived view merging content + github cache.
		"RecentRepos": h.RecentlyActiveProjects(r.Context(), 3),
	}
	if err := h.Tpl.Render(w, r, http.StatusOK, "home.html", data); err != nil {
		h.Logger.Error("home.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// atoi parses a query-string integer defaulting to `def` on any issue.
func atoi(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v < 1 {
		return def
	}
	return v
}
