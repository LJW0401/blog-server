package public

import (
	"encoding/xml"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/penguin/blog-server/internal/content"
)

// FeedLimit caps the number of RSS items emitted (most recent N).
const FeedLimit = 20

// BaseURL returns the site's public base URL for building absolute links in
// RSS/sitemap/OG. Falls back to a placeholder when not configured.
func (h *Handlers) BaseURL() string {
	if h.SettingsDB != nil {
		if v, ok := h.SettingsDB.Get("base_url"); ok && v != "" {
			return strings.TrimRight(v, "/")
		}
	}
	return "https://example.invalid"
}

// --- RSS (WI-7.1) ---------------------------------------------------------

type rssFeed struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Channel rssChan  `xml:"channel"`
}

type rssChan struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Language    string    `xml:"language"`
	PubDate     string    `xml:"pubDate"`
	Items       []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
	Description string `xml:"description"`
}

// RSS handles GET /rss.xml.
func (h *Handlers) RSS(w http.ResponseWriter, r *http.Request) {
	base := h.BaseURL()
	settings := h.Settings()
	docs := h.Content.Docs().List(content.KindDoc)
	pubDocs := make([]*content.Entry, 0, len(docs))
	for _, d := range docs {
		if d.Status == content.StatusPublished {
			pubDocs = append(pubDocs, d)
		}
	}
	if len(pubDocs) > FeedLimit {
		pubDocs = pubDocs[:FeedLimit]
	}
	items := make([]rssItem, 0, len(pubDocs))
	for _, d := range pubDocs {
		items = append(items, rssItem{
			Title:       d.Title,
			Link:        base + "/docs/" + d.Slug,
			GUID:        base + "/docs/" + d.Slug,
			PubDate:     d.Updated.Format(time.RFC1123Z),
			Description: d.Excerpt,
		})
	}
	feed := rssFeed{
		Version: "2.0",
		Channel: rssChan{
			Title:       settings.Name + " — 文档",
			Link:        base + "/docs",
			Description: settings.Tagline,
			Language:    "zh-CN",
			PubDate:     time.Now().UTC().Format(time.RFC1123Z),
			Items:       items,
		},
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		h.Logger.Warn("rss.write_header", slog.String("err", err.Error()))
		return
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(feed); err != nil {
		h.Logger.Error("rss.encode", slog.String("err", err.Error()))
	}
}

// --- Sitemap (WI-7.4) -----------------------------------------------------

type urlSet struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

// Sitemap handles GET /sitemap.xml.
func (h *Handlers) Sitemap(w http.ResponseWriter, r *http.Request) {
	base := h.BaseURL()
	urls := []sitemapURL{
		{Loc: base + "/"},
		{Loc: base + "/docs"},
		{Loc: base + "/projects"},
	}
	for _, d := range h.Content.Docs().List(content.KindDoc) {
		if d.Status != content.StatusPublished {
			continue
		}
		urls = append(urls, sitemapURL{
			Loc:     base + "/docs/" + d.Slug,
			LastMod: d.Updated.Format("2006-01-02"),
		})
	}
	for _, p := range h.Content.Projects().List(content.KindProject) {
		urls = append(urls, sitemapURL{
			Loc:     base + "/projects/" + p.Slug,
			LastMod: p.Updated.Format("2006-01-02"),
		})
	}
	set := urlSet{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		h.Logger.Warn("sitemap.write_header", slog.String("err", err.Error()))
		return
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(set); err != nil {
		h.Logger.Error("sitemap.encode", slog.String("err", err.Error()))
	}
}
