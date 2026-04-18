// Package public wires all unauthenticated HTTP handlers: home, docs list,
// doc detail, etc. Handlers are composed under a chi router by the caller.
package public

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/penguin/blog-server/internal/content"
	"github.com/penguin/blog-server/internal/render"
	"github.com/penguin/blog-server/internal/settings"
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
	SettingsDB  *settings.Store
	Settings    func() SiteSettings
	Logger      *slog.Logger

	mu       sync.Mutex
	cached   SiteSettings
	cachedAt time.Time
}

// NewHandlers constructs a Handlers with safe defaults. Settings resolves to
// DefaultSettings when SettingsDB is nil; otherwise it returns DB values
// overriding defaults, cached for 30 seconds (per requirement 2.4.5).
func NewHandlers(cs *content.Store, tpl *render.Templates, logger *slog.Logger) *Handlers {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handlers{Content: cs, Tpl: tpl, Logger: logger}
	h.Settings = h.resolveSettings
	return h
}

// resolveSettings merges DefaultSettings with DB overrides, cached for 30s.
func (h *Handlers) resolveSettings() SiteSettings {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.cachedAt.IsZero() && time.Since(h.cachedAt) < 30*time.Second {
		return h.cached
	}
	s := DefaultSettings()
	if h.SettingsDB != nil {
		kv := h.SettingsDB.All()
		if v := kv["name"]; v != "" {
			s.Name = v
		}
		if v := kv["tagline"]; v != "" {
			s.Tagline = v
		}
		if v := kv["location"]; v != "" {
			s.Location = v
		}
		if v := kv["direction"]; v != "" {
			s.Direction = v
		}
		if v := kv["status"]; v != "" {
			s.Status = v
		}
		if v := kv["qq_group"]; v != "" {
			s.ContactQQ = v
		}
		// Overwrite media links from DB where present.
		media := []struct{ Platform, Key string }{
			{"B站", "media_bilibili"},
			{"抖音", "media_douyin"},
			{"小红书", "media_xiaohongshu"},
		}
		filled := []MediaLink{}
		for _, m := range media {
			filled = append(filled, MediaLink{Platform: m.Platform, URL: kv[m.Key]})
		}
		s.MediaLinks = filled
	}
	h.cached = s
	h.cachedAt = time.Now()
	return s
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
