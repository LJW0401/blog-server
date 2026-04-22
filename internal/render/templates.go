package render

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

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

	// SettingsFn, if set, returns site settings made available to the layout
	// (and any page) as {{ .Settings }} at the payload root. Admin / public
	// callers both benefit without having to stuff Settings into every Data
	// map. Called on every Render; caller is responsible for any caching.
	SettingsFn func() any
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
	if t.SettingsFn != nil {
		payload["Settings"] = t.SettingsFn()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	return clone.ExecuteTemplate(w, t.layout, payload)
}

// Markdown exposes the shared MD renderer.
func (t *Templates) Markdown() *Markdown { return t.md }

// --- FuncMap ---------------------------------------------------------------

func funcMap(md *Markdown) template.FuncMap {
	mdUnsafe := NewMarkdownUnsafe()
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
		"markdownUnsafe": func(s string) (template.HTML, error) {
			return mdUnsafe.ToHTML(s)
		},
		"readingTime": func(s string) int {
			return readingMinutes(s)
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

// 预编译正则，先剥掉 markdown 里与阅读量无关的噪声，再分别按 CJK 字 / 非 CJK 词计时长。
var (
	readingFencedCodeRE = regexp.MustCompile("(?s)```.*?```")
	readingInlineCodeRE = regexp.MustCompile("`[^`]*`")
	readingHTMLTagRE    = regexp.MustCompile(`<[^>]+>`)
	readingLinkURLRE    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`) // [text](url) → text
	readingImageRE      = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
)

// readingMinutes 估算阅读时长（分钟，最少 1）。
//
// 策略：先剥代码块/行内代码/HTML 标签/图片/链接 URL 等噪声，再把剩余文本
// 按 CJK（中日韩统一表意）字符与非 CJK 词分开计数：
//   - CJK 按 400 字/分钟（成年中文读者的常见基准）
//   - 非 CJK 按 250 词/分钟（strings.Fields 切分）
//
// 原实现只做 strings.Fields，对纯中文永远得到 ≈1 词 → 恒为 1 分钟。
func readingMinutes(s string) int {
	s = readingImageRE.ReplaceAllString(s, "")
	s = readingFencedCodeRE.ReplaceAllString(s, "")
	s = readingInlineCodeRE.ReplaceAllString(s, "")
	s = readingHTMLTagRE.ReplaceAllString(s, "")
	s = readingLinkURLRE.ReplaceAllString(s, "$1")

	cjkChars := 0
	var nonCJK strings.Builder
	nonCJK.Grow(len(s))
	for _, r := range s {
		if unicode.Is(unicode.Han, r) ||
			unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) ||
			unicode.Is(unicode.Hangul, r) {
			cjkChars++
			nonCJK.WriteByte(' ') // 让 CJK 字符不拼进旁边的英文词
			continue
		}
		nonCJK.WriteRune(r)
	}
	nonCJKWords := len(strings.Fields(nonCJK.String()))

	// minutes = ceil(cjkChars/400) + ceil(nonCJKWords/250)，再做最小值 1 兜底。
	// 两段独立向上取整，确保"400 字 + 250 词"这种混排不会误判为 1 分钟。
	cjkMin := (cjkChars + 399) / 400
	wordMin := (nonCJKWords + 249) / 250
	total := cjkMin + wordMin
	if total < 1 {
		total = 1
	}
	return total
}
