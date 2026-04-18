package render

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/penguin/blog-server/internal/middleware"
)

// Templates is a reusable html/template registry. Each page is stored as raw
// template source; per-request we clone the base layout, add the page source,
// and execute the layout so exactly one `content` block wins at a time.
type Templates struct {
	base   *template.Template // holds layout + helpers
	pages  map[string]string  // pageFile → raw template text
	md     *Markdown
	layout string
}

// LayoutName is the template entry point invoked by Render.
const LayoutName = "layout.html"

// NewTemplates parses layout.html into the base tree and captures the text of
// every other *.html for per-request composition.
func NewTemplates(fsys fs.FS, md *Markdown) (*Templates, error) {
	if md == nil {
		md = NewMarkdown()
	}
	files, err := fs.Glob(fsys, "*.html")
	if err != nil {
		return nil, fmt.Errorf("render: glob: %w", err)
	}
	var layoutText string
	pages := map[string]string{}
	for _, p := range files {
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, fmt.Errorf("render: read %s: %w", p, err)
		}
		if p == LayoutName {
			layoutText = string(b)
			continue
		}
		pages[p] = string(b)
	}
	if layoutText == "" {
		return nil, fmt.Errorf("render: %s not found", LayoutName)
	}
	base := template.New(LayoutName).Funcs(funcMap(md))
	if _, err := base.Parse(layoutText); err != nil {
		return nil, fmt.Errorf("render: parse layout: %w", err)
	}
	return &Templates{
		base:   base,
		pages:  pages,
		md:     md,
		layout: LayoutName,
	}, nil
}

// Render composes the layout with the page file's `content` block and streams
// HTML to w.
func (t *Templates) Render(w http.ResponseWriter, r *http.Request, status int, pageFile string, data any) error {
	src, ok := t.pages[pageFile]
	if !ok {
		return fmt.Errorf("render: unknown page %q", pageFile)
	}
	clone, err := t.base.Clone()
	if err != nil {
		return fmt.Errorf("render: clone: %w", err)
	}
	// New template to hold the page source; it shares FuncMap with the clone.
	pageTpl, err := clone.New(pageFile).Parse(src)
	if err != nil {
		return fmt.Errorf("render: parse %s: %w", pageFile, err)
	}
	_ = pageTpl

	payload := map[string]any{
		"Data":      data,
		"Banner":    middleware.DefaultPasswordBannerFrom(r.Context()),
		"RequestID": middleware.RequestIDFrom(r.Context()),
		"Now":       time.Now(),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	return clone.ExecuteTemplate(w, t.layout, payload)
}

// Markdown exposes the shared MD renderer.
func (t *Templates) Markdown() *Markdown { return t.md }

// --- FuncMap ---------------------------------------------------------------

func funcMap(md *Markdown) template.FuncMap {
	return template.FuncMap{
		"formatDate": func(t time.Time, layout string) string {
			if t.IsZero() {
				return ""
			}
			if layout == "" {
				layout = "2006-01-02"
			}
			return t.Format(layout)
		},
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"join":  strings.Join,
		"markdown": func(s string) (template.HTML, error) {
			return md.ToHTML(s)
		},
		"readingTime": func(s string) int {
			words := len(strings.Fields(s))
			if words == 0 {
				return 1
			}
			minutes := (words + 249) / 250
			if minutes < 1 {
				minutes = 1
			}
			return minutes
		},
		"excerpt": func(s string, n int) string {
			runes := []rune(strings.TrimSpace(s))
			if len(runes) <= n {
				return string(runes)
			}
			return strings.TrimSpace(string(runes[:n])) + "…"
		},
	}
}
