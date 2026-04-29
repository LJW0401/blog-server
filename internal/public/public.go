// Package public wires all unauthenticated HTTP handlers: home, docs list,
// doc detail, etc. Handlers are composed under a chi router by the caller.
package public

import (
	"context"
	"encoding/json"
	"html/template"
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
	AvatarURL  string // 主页 hero 头像 URL；空字符串代表未上传
	AvatarShow bool   // 主页是否显示头像；false 时即使 URL 非空也不渲染
	ContactQQ  string
	OSSLinks   []MediaLink // 开源项目列：GitHub, Gitee
	MediaLinks []MediaLink // 媒体列：B站, 抖音, 小红书
	// HomeStyle 取值："minimal"（默认简约风）或 "galaxy"（三维星系导航）。
	// 任何非 "galaxy" 的值都按 minimal 处理。
	HomeStyle string
}

// MediaLink points at one of Penguin's social profiles.
type MediaLink struct {
	Platform string
	URL      string
}

// DefaultSettings returns placeholder values for the MVP before M5 lands.
func DefaultSettings() SiteSettings {
	return SiteSettings{
		Name:       "Penguin",
		Tagline:    "一名热衷于开源与技术写作的开发者，探索代码与思考之间的联系。",
		Location:   "中国",
		Direction:  "后端 / 工程化 / 开发者工具",
		Status:     "活跃维护若干个人项目",
		AvatarShow: true,
		HomeStyle:  "minimal",
		ContactQQ:  "772436864",
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
		// avatar_show: 只有显式 "false" 才关，其它（空、未存、"true"）都视为开；
		// DefaultSettings 默认已经是 true，这里只在显式关闭时覆盖为 false。
		if kv["avatar_show"] == "false" {
			s.AvatarShow = false
		}
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
		// home_style 仅接受 "galaxy"；其余值（含空、未知）一律按 minimal 处理。
		if kv["home_style"] == "galaxy" {
			s.HomeStyle = "galaxy"
		}
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
	portfolios := h.Content.Portfolios().List(content.KindPortfolio)

	// Home cards: only featured + published, fully sorted (Order ASC, Updated DESC).
	homePortfolios := make([]*content.Entry, 0, len(portfolios))
	for _, e := range portfolios {
		if e.Status == content.StatusPublished && e.Featured {
			homePortfolios = append(homePortfolios, e)
		}
	}
	sortPortfolios(homePortfolios)
	homeCards := make([]portfolioHomeCard, len(homePortfolios))
	for i, e := range homePortfolios {
		cover := e.Cover
		if strings.TrimSpace(cover) == "" {
			cover = PortfolioDefaultCover
		}
		homeCards[i] = portfolioHomeCard{Entry: e, Cover: cover}
	}

	settings := h.Settings()
	data := map[string]any{
		"Settings":         settings,
		"FeaturedDocs":     pickFeatured(docs, 4),
		"FeaturedProjects": pickFeatured(projs, 3),
		// Recently Active is a derived view merging content + github cache.
		"RecentRepos":        h.RecentlyActiveProjects(r.Context(), 3),
		"About":              h.about(),
		"FeaturedPortfolios": homeCards,
	}
	tpl := "home.html"
	if settings.HomeStyle == "galaxy" {
		tpl = "home_galaxy.html"
		// Galaxy 模板内嵌 importmap + module script + 从 unpkg 加载 three.js。
		// 默认 CSP `script-src 'self'` 会拦掉这些；这里只针对此路由放宽，
		// 管理员关掉 galaxy 模式后下一次访问立即恢复严格 CSP。
		w.Header().Set("Content-Security-Policy", galaxyCSP)
		data["GalaxyConfigJSON"] = buildGalaxyConfig(settings, h.about(),
			pickFeatured(projs, 6), pickFeatured(docs, 6), homePortfolios)
	}
	if err := h.Tpl.Render(w, r, http.StatusOK, tpl, data); err != nil {
		h.Logger.Error("home.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// galaxySectionItem 是一个行星标签：可点击，附带跳转 URL（外链或站内）。
//
// Label 是简单单行文本（开源/文档/作品集/联系等都用它）；当 Title + Details
// 不为空时，前端会渲染成"标题居中 + 右侧多行详情"的双栏样式（简介板块用）。
type galaxySectionItem struct {
	Label    string   `json:"label,omitempty"`
	Title    string   `json:"title,omitempty"`
	Subtitle string   `json:"subtitle,omitempty"`
	Details  []string `json:"details,omitempty"`
	Image    string   `json:"image,omitempty"`
	URL      string   `json:"url,omitempty"`
}

// galaxySection 描述星系上的一个板块（中心恒星 + 行星）。
type galaxySection struct {
	CN    string              `json:"cn"`
	EN    string              `json:"en"`
	Hue   float64             `json:"hue"`
	URL   string              `json:"url,omitempty"` // 板块整体回退 URL（行星无 URL 时使用）
	Items []galaxySectionItem `json:"items"`
}

type galaxyConfig struct {
	Sections []galaxySection `json:"sections"`
}

// linkURL 在一组 MediaLink 里查 platform 名匹配的 URL；找不到返回空串。
func linkURL(links []MediaLink, platform string) string {
	for _, l := range links {
		if l.Platform == platform {
			return l.URL
		}
	}
	return ""
}

// buildGalaxyConfig 把站点设置编织成 galaxy 模板需要的 JSON 配置。
// 每个板块的多个行星暂时都指向板块对应的内容主路由（用户后续可以替换为
// 真实的 featured 项目/文档/作品列表）。"联系" 板块从 settings 里取
// GitHub/Gitee/B站/小红书 链接；空 URL 用 "#" 占位，点击不跳。
//
// 返回 template.JS：模板里这段 JSON 写在 <script type="application/json">
// 内部，html/template 默认会把 .HTML 值按 JS 字符串字面量再包一层引号 +
// 反斜杠转义，导致 JSON.parse 拿到的是字符串而不是对象。template.JS 表示
// 内容已是 JS 安全字面量，直接原样输出。
func buildGalaxyConfig(s SiteSettings, a AboutData, openProjects, featuredDocs, featuredPortfolios []*content.Entry) template.JS {
	contactItems := []galaxySectionItem{
		{Label: "GitHub", URL: fallback(linkURL(s.OSSLinks, "GitHub"), "#")},
		{Label: "Gitee", URL: fallback(linkURL(s.OSSLinks, "Gitee"), "#")},
		{Label: "B站", URL: fallback(linkURL(s.MediaLinks, "B站"), "#")},
		{Label: "小红书", URL: fallback(linkURL(s.MediaLinks, "小红书"), "#")},
	}
	if s.ContactQQ != "" {
		contactItems = append(contactItems, galaxySectionItem{Label: "QQ:" + s.ContactQQ, URL: "#"})
	}
	cfg := galaxyConfig{Sections: []galaxySection{
		{CN: "简介", EN: "Intro", Hue: 0.62, URL: "/about", Items: []galaxySectionItem{
			introItem("坐标", s.Location),
			introItem("现状", s.Status),
			introItem("方向", s.Direction),
		}},
		{CN: "关于我", EN: "About", Hue: 0.72, URL: "/about", Items: []galaxySectionItem{
			detailItem("技能栈", a.Stack),
			detailItem("经历", experienceLines(a.Experience)),
			detailItem("兴趣", a.Interests),
			// 唯一显式带跳转 URL 的行星：进 /about 看详细的 markdown 页面。
			{Label: "查看详情 →", URL: "/about"},
		}},
		{CN: "开源", EN: "Open", Hue: 0.13, URL: "/projects",
			Items: entrySectionItems(openProjects, "/projects/", "全部项目 →", "/projects")},
		{CN: "文档", EN: "Docs", Hue: 0.55, URL: "/docs",
			Items: entrySectionItems(featuredDocs, "/docs/", "全部文档 →", "/docs")},
		{CN: "作品集", EN: "Portfolio", Hue: 0.05, URL: "/portfolio", Items: portfolioSectionItems(featuredPortfolios)},
		{CN: "联系", EN: "Contact", Hue: 0.95, URL: "#", Items: contactItems},
	}}
	b, err := json.Marshal(cfg)
	if err != nil {
		// 配置都是固定结构，几乎不会失败。回退一个最小 JSON 让前台不至于崩溃。
		return template.JS(`{"sections":[]}`)
	}
	return template.JS(b)
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// firstNonEmpty 返回第一个 trim 后非空的字符串；都空时返回空串。
// 给 entrySectionItems 在多个候选字段间挑可显示文本用。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

// portfolioSectionItems 把作品集首页卡片用的同一批 featured portfolios
// 渲染成 galaxy 行星：标题在上 + 下面横排（左封面 / 右两行简介），末尾补
// 一颗"全部作品 →"行星指向 /portfolio。Cover 字段留空时回退到默认 SVG。
func portfolioSectionItems(entries []*content.Entry) []galaxySectionItem {
	out := make([]galaxySectionItem, 0, len(entries)+1)
	for _, e := range entries {
		if e == nil {
			continue
		}
		name := firstNonEmpty(e.DisplayName, e.Title, e.Slug)
		desc := firstNonEmpty(e.Description, e.DisplayDesc, e.Excerpt)
		cover := strings.TrimSpace(e.Cover)
		if cover == "" {
			cover = PortfolioDefaultCover
		}
		out = append(out, galaxySectionItem{
			Title:    name,
			Subtitle: desc,
			Image:    cover,
			URL:      "/portfolio/" + e.Slug,
		})
	}
	out = append(out, galaxySectionItem{Label: "全部作品 →", URL: "/portfolio"})
	return out
}

// entrySectionItems 把首页 featured 出来的内容条目渲染成 galaxy 行星：
// 每颗行星名字 + 简介上下叠并跳到 <urlPrefix><slug>，末尾再补一颗
// "<allLabel>"行星指向 allURL。entries 为空时只剩 all 行星。
// 开源 / 文档板块都用它，避免重复维护两份几乎一样的逻辑。
func entrySectionItems(entries []*content.Entry, urlPrefix, allLabel, allURL string) []galaxySectionItem {
	out := make([]galaxySectionItem, 0, len(entries)+1)
	for _, e := range entries {
		if e == nil {
			continue
		}
		// 项目用 DisplayName / DisplayDesc；文档主要靠 Title / Excerpt；
		// portfolio 用 Title / Description。这里按优先级回落，让一份函数
		// 能同时服务三种 Kind 而不需要分支。
		name := firstNonEmpty(e.DisplayName, e.Title, e.Slug)
		desc := firstNonEmpty(e.DisplayDesc, e.Description, e.Excerpt)
		out = append(out, galaxySectionItem{
			Title:    name,
			Subtitle: desc,
			URL:      urlPrefix + e.Slug,
		})
	}
	out = append(out, galaxySectionItem{Label: allLabel, URL: allURL})
	return out
}

// detailItem 是直接拿现成的 details 切片去构造双栏行星，留给 a.Stack 这种
// 已经分好的 []string 用，避免再做一次 split。空切片 → 只保留标题。
func detailItem(title string, details []string) galaxySectionItem {
	cleaned := make([]string, 0, len(details))
	for _, d := range details {
		if t := strings.TrimSpace(d); t != "" {
			cleaned = append(cleaned, t)
		}
	}
	if len(cleaned) == 0 {
		return galaxySectionItem{Title: title}
	}
	return galaxySectionItem{Title: title, Details: cleaned}
}

// experienceLines 把 AboutExperience 渲染成 "标题 · 年份" 字符串列表，
// 用作经历行星的右侧多行详情。
func experienceLines(xs []AboutExperience) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		t := strings.TrimSpace(x.Title)
		y := strings.TrimSpace(x.Year)
		switch {
		case t != "" && y != "":
			out = append(out, t+" · "+y)
		case t != "":
			out = append(out, t)
		case y != "":
			out = append(out, y)
		}
	}
	return out
}

// introItem 把"标题 + 内容字符串"拆成 galaxy 行星的双栏数据。
// 内容按常见分隔符（中英逗号 / 顿号 / 中英分号 / 斜杠 / 换行）切，留给前端
// 渲染成右侧多行详情；空内容时只剩标题，提示用户去 /manage/settings 填值。
func introItem(title, value string) galaxySectionItem {
	value = strings.TrimSpace(value)
	if value == "" {
		return galaxySectionItem{Title: title}
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ',', '，', ';', '；', '/', '、', '\n':
			return true
		}
		return false
	})
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			cleaned = append(cleaned, t)
		}
	}
	if len(cleaned) == 0 {
		// 整段都是分隔符，回退为原值。
		cleaned = []string{value}
	}
	return galaxySectionItem{Title: title, Details: cleaned}
}

// galaxyCSP 是首页 galaxy 风格专用的放宽 CSP：
//   - script-src 加入 `'unsafe-inline'`（importmap + 内联 module 脚本）和
//     `https://unpkg.com`（three.js 模块来源）。
//   - 其他指令与 middleware.defaultCSP 保持一致。
//
// 仅在 home_style = "galaxy" 时被首页 handler 主动覆盖响应头；其他路由仍走默认 CSP。
const galaxyCSP = "default-src 'self'; " +
	"img-src 'self' data:; " +
	"script-src 'self' 'unsafe-inline' https://unpkg.com; " +
	"style-src 'self' 'unsafe-inline'; " +
	"font-src 'self'; " +
	"object-src 'none'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'"

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
