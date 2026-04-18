package public_test

import (
	"encoding/xml"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- RSS (WI-7.2, WI-7.3) --------------------------------------------------

func TestRSS_Smoke_ListsPublishedOnly(t *testing.T) {
	h := setup(t, map[string]string{
		"a": doc("a", "2026-04-10", "published", false, ""),
		"b": doc("b", "2026-04-08", "draft", false, ""),
		"c": doc("c", "2026-04-05", "archived", false, ""),
	}, nil)
	req := httptest.NewRequest("GET", "/rss.xml", nil)
	w := httptest.NewRecorder()
	h.RSS(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.Bytes()
	if !strings.HasPrefix(string(body), "<?xml") {
		t.Error("missing xml declaration")
	}
	// Valid XML
	var feed struct {
		XMLName xml.Name `xml:"rss"`
		Channel struct {
			Items []struct {
				Title string `xml:"title"`
				Link  string `xml:"link"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil {
		t.Fatalf("parse: %v body=%s", err, body)
	}
	if len(feed.Channel.Items) != 1 {
		t.Errorf("want 1 item (only published), got %d", len(feed.Channel.Items))
	}
	if feed.Channel.Items[0].Title != "A" {
		t.Errorf("wrong title: %q", feed.Channel.Items[0].Title)
	}
	if !strings.HasSuffix(feed.Channel.Items[0].Link, "/docs/a") {
		t.Errorf("link: %q", feed.Channel.Items[0].Link)
	}
}

func TestRSS_Edge_EmptyFeedValid(t *testing.T) {
	h := setup(t, nil, nil)
	req := httptest.NewRequest("GET", "/rss.xml", nil)
	w := httptest.NewRecorder()
	h.RSS(w, req)
	var feed struct {
		XMLName xml.Name `xml:"rss"`
		Channel struct {
			Title string `xml:"title"`
			Items []any  `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &feed); err != nil {
		t.Fatalf("empty RSS should still be valid XML: %v", err)
	}
	if len(feed.Channel.Items) != 0 {
		t.Errorf("want 0 items, got %d", len(feed.Channel.Items))
	}
	if feed.Channel.Title == "" {
		t.Error("channel title missing")
	}
}

func TestRSS_Edge_SpecialCharsEscaped(t *testing.T) {
	// Title with <, > and & — must appear escaped in XML.
	body := "---\ntitle: \"A < B & C > D\"\nslug: naughty\nupdated: 2026-04-10\nstatus: published\nexcerpt: \"说明 <tag> 与 & 符号\"\n---\nbody\n"
	h := setup(t, map[string]string{"naughty": body}, nil)
	req := httptest.NewRequest("GET", "/rss.xml", nil)
	w := httptest.NewRecorder()
	h.RSS(w, req)
	raw := w.Body.String()
	// Raw XML must not contain unescaped < after title opening tag.
	if strings.Contains(raw, "A < B") {
		t.Error("title should be escaped: still contains raw '<'")
	}
	// Should contain the escaped variant.
	if !strings.Contains(raw, "A &lt; B") {
		t.Errorf("escape form missing: %s", raw)
	}
	// Valid XML as a whole.
	var feed struct {
		XMLName xml.Name `xml:"rss"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &feed); err != nil {
		t.Errorf("not valid XML: %v", err)
	}
}

// --- Sitemap (WI-7.5) ----------------------------------------------------

func TestSitemap_Smoke_ListsPublicURLs(t *testing.T) {
	h := setup(t, map[string]string{
		"a": doc("a", "2026-04-10", "published", false, ""),
		"b": doc("b", "2026-04-08", "draft", false, ""),
	}, map[string]string{
		"proj1": proj("proj1", "2026-04-07", "active", ""),
	})
	req := httptest.NewRequest("GET", "/sitemap.xml", nil)
	w := httptest.NewRecorder()
	h.Sitemap(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	// Expect /docs/a, /projects/proj1, site root, /docs, /projects.
	for _, need := range []string{"/docs/a", "/projects/proj1", "<urlset"} {
		if !strings.Contains(body, need) {
			t.Errorf("sitemap missing %q", need)
		}
	}
	// Draft should NOT appear.
	if strings.Contains(body, "/docs/b") {
		t.Error("draft should not appear in sitemap")
	}
	// Valid XML.
	var set struct {
		XMLName xml.Name `xml:"urlset"`
		URLs    []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &set); err != nil {
		t.Fatalf("not valid XML: %v", err)
	}
	if len(set.URLs) < 4 {
		t.Errorf("want ≥4 urls, got %d", len(set.URLs))
	}
}

// --- OG (WI-7.7) ---------------------------------------------------------

func TestDocDetail_Smoke_OGMetaPresent(t *testing.T) {
	h := setup(t, map[string]string{
		"hello": doc("hello", "2026-04-10", "published", false, ""),
	}, nil)
	req := httptest.NewRequest("GET", "/docs/hello", nil)
	w := httptest.NewRecorder()
	h.DocDetail(w, req)
	body := w.Body.String()
	for _, need := range []string{
		`property="og:type"`,
		`property="og:title"`,
		`property="og:description"`,
		`name="twitter:card"`,
	} {
		if !strings.Contains(body, need) {
			t.Errorf("OG/Twitter meta missing: %s", need)
		}
	}
}

func TestHome_Smoke_DefaultOGPresent(t *testing.T) {
	h := setup(t, nil, nil)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.Home(w, req)
	body := w.Body.String()
	if !strings.Contains(body, `property="og:site_name"`) {
		t.Error("default og:site_name missing on home")
	}
}
