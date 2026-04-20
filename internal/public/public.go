// Package public wires all unauthenticated HTTP handlers: home, docs list,
// doc detail, etc. Handlers are composed under a chi router by the caller.
package public

import (
	"context"
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
	AvatarURL  string // 主页 hero 头像；空字符串代表不渲染 <img>
	ContactQQ  string
	OSSLinks   []MediaLink // 开源项目列：GitHub, Gitee
	MediaLinks []MediaLink // 媒体列：B站, 抖音, 小红书
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
		OSSLinks: []MediaLink{
			{Platform: "GitHub"},
			{Platform: "Gitee"},
		},
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
	Stats       StatsRecorder
	Settings    func() SiteSettings
	Logger      *slog.Logger
	// AboutPath points at a plain-markdown file (no frontmatter) read by
	// the /about handler. Empty or missing ⇒ 404. Lives outside the
	// content/docs tree so it is not indexed as a blog post.
	AboutPath string

	mu       sync.Mutex
	cached   SiteSettings
	cachedAt time.Time
}

// StatsRecorder is the subset of *stats.Store that DocDetail consumes. Kept
// as an interface to avoid an import cycle and to simplify test fakes.
type StatsRecorder interface {
	RecordRead(ctx context.Context, slug, ip, userAgent string)
	Count(ctx context.Context, slug string) int
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

// InvalidateSettings drops the in-memory site-settings cache so the next
// resolveSettings call re-reads from SettingsDB. Called by the admin
// SettingsSubmit handler after a successful write so front-end footer /
// hero values refresh immediately rather than waiting out the 30s TTL.
func (h *Handlers) InvalidateSettings() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cachedAt = time.Time{}
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
		if v := kv["avatar_url"]; v != "" {
			s.AvatarURL = v
		}
		if v := kv["qq_group"]; v != "" {
			s.ContactQQ = v
		}
		// Overwrite link columns from DB; URL empty means "show platform name
		// without hyperlink" — the template decides presentation.
		oss := []struct{ Platform, Key string }{
			{"GitHub", "media_github"},
			{"Gitee", "media_gitee"},
		}
		filledOSS := []MediaLink{}
		for _, m := range oss {
			filledOSS = append(filledOSS, MediaLink{Platform: m.Platform, URL: kv[m.Key]})
		}
		s.OSSLinks = filledOSS
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
// featured=true first (sorted by updated desc), then non-featured fill. The
// "visible" filter differs per kind — docs require status=published; projects
// are visible for active/developing (archived hidden); other kinds pass by
// default. Archived / draft are always excluded.
func pickFeatured(all []*content.Entry, limit int) []*content.Entry {
	var featured, rest []*content.Entry
	for _, e := range all {
		if !isFrontpageVisible(e) {
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

// isFrontpageVisible says whether an entry is eligible to show on the
// homepage carousel. Matches requirement 2.1.2's "mixed" rule semantics but
// honours the kind-specific status vocabulary we set up in P3.
func isFrontpageVisible(e *content.Entry) bool {
	switch e.Kind {
	case content.KindDoc:
		return e.Status == content.StatusPublished
	case content.KindProject:
		return e.Status == content.StatusActive || e.Status == content.StatusDeveloping
	default:
		return e.Status != content.StatusArchived && e.Status != content.StatusDraft
	}
}

// --- Handlers --------------------------------------------------------------

// NotFound renders the branded 404 page at StatusNotFound. Used by every
// public-facing "not found" path (unknown slug, root catch-all, etc.) so users
// hit a styled page with a "返回主页" button instead of plain text.
func (h *Handlers) NotFound(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{}
	if err := h.Tpl.Render(w, r, http.StatusNotFound, "404.html", data); err != nil {
		h.Logger.Error("notfound.render", slog.String("err", err.Error()))
		http.Error(w, "not found", http.StatusNotFound)
	}
}

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
		"About":       h.about(),
	}
	if err := h.Tpl.Render(w, r, http.StatusOK, "home.html", data); err != nil {
		h.Logger.Error("home.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// AboutData drives the "关于我" block on the homepage. Values are filled from
// site_settings where present, otherwise fall back to sensible defaults so a
// fresh deployment still renders a complete section.
type AboutData struct {
	Bio        string
	Stack      []string
	Experience []AboutExperience
	Interests  []string
}

// AboutExperience is one row in the Experience card.
type AboutExperience struct {
	Title string
	Year  string
}

func (h *Handlers) about() AboutData {
	defaults := AboutData{
		Bio: "",
		Stack: []string{
			"TypeScript", "Go", "Python", "Node.js",
			"PostgreSQL", "SQLite", "Linux", "Docker",
		},
		Experience: []AboutExperience{
			{Title: "后端 / 工具开发者", Year: "2023 – 现在"},
			{Title: "开始写开源项目", Year: "2021"},
			{Title: "计算机科学本科", Year: "2019 – 2023"},
		},
		Interests: []string{
			"开发者工具与工程化",
			"极简与克制的设计",
			"阅读与技术写作",
			"终端美学与 dotfiles",
		},
	}
	if h.SettingsDB == nil {
		return defaults
	}
	kv := h.SettingsDB.All()
	if v := strings.TrimSpace(kv["about_bio"]); v != "" {
		defaults.Bio = v
	}
	if v := strings.TrimSpace(kv["about_stack"]); v != "" {
		defaults.Stack = splitCommaList(v)
	}
	if v := strings.TrimSpace(kv["about_interests"]); v != "" {
		defaults.Interests = splitCommaList(v)
	}
	if v := strings.TrimSpace(kv["about_experience"]); v != "" {
		defaults.Experience = parseExperience(v)
	}
	return defaults
}

// splitCommaList tolerates comma, Chinese comma and newline separators.
func splitCommaList(s string) []string {
	replacer := strings.NewReplacer("，", ",", "\n", ",", "、", ",")
	s = replacer.Replace(s)
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseExperience parses lines of the form "标题 | 年份" into AboutExperience.
func parseExperience(s string) []AboutExperience {
	out := []AboutExperience{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		item := AboutExperience{Title: strings.TrimSpace(parts[0])}
		if len(parts) == 2 {
			item.Year = strings.TrimSpace(parts[1])
		}
		if item.Title != "" {
			out = append(out, item)
		}
	}
	return out
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
