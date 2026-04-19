package render_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/penguin/blog-server/internal/render"
)

func makeTemplatesFS(pages map[string]string) fstest.MapFS {
	out := fstest.MapFS{}
	for name, body := range pages {
		out[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return out
}

// --- Markdown ---------------------------------------------------------------

// Smoke: typical MD renders into expected HTML pieces.
func TestMarkdown_Smoke_RendersHTML(t *testing.T) {
	md := render.NewMarkdown()
	h, err := md.ToHTML("# Title\n\nHello **bold** and `code`.\n\n```go\nfunc f(){}\n```\n")
	if err != nil {
		t.Fatalf("ToHTML: %v", err)
	}
	s := string(h)
	for _, frag := range []string{"<h1", ">Title", "<strong>bold</strong>", "<code>code</code>", "<pre"} {
		if !strings.Contains(s, frag) {
			t.Errorf("missing fragment %q in %s", frag, s)
		}
	}
}

// --- Edge: XSS & weird input ------------------------------------------------

func TestMarkdown_Edge_ScriptEscaped(t *testing.T) {
	md := render.NewMarkdown()
	h, err := md.ToHTML("Before <script>alert(1)</script> after\n")
	if err != nil {
		t.Fatalf("ToHTML: %v", err)
	}
	if strings.Contains(string(h), "<script>") {
		t.Errorf("<script> not escaped: %s", h)
	}
}

func TestMarkdown_Edge_EmptyBody(t *testing.T) {
	md := render.NewMarkdown()
	h, err := md.ToHTML("")
	if err != nil {
		t.Fatalf("ToHTML: %v", err)
	}
	if string(h) != "" {
		t.Errorf("empty MD should yield empty HTML, got %q", h)
	}
}

func TestMarkdown_Edge_UnknownCodeLangPlain(t *testing.T) {
	md := render.NewMarkdown()
	h, err := md.ToHTML("```not-a-language\nxyz\n```\n")
	if err != nil {
		t.Fatalf("ToHTML: %v", err)
	}
	if !strings.Contains(string(h), "xyz") {
		t.Errorf("fallback rendering missing: %s", h)
	}
}

func TestMarkdown_Edge_JavascriptURLSanitized(t *testing.T) {
	md := render.NewMarkdown()
	h, err := md.ToHTML(`[click](javascript:alert(1))` + "\n")
	if err != nil {
		t.Fatalf("ToHTML: %v", err)
	}
	// Goldmark leaves javascript: URLs alone, but its default HTML renderer
	// already escapes the anchor target; we additionally verify no active
	// script is surfaced.
	if strings.Contains(string(h), `href="javascript:`) {
		t.Errorf("unsafe javascript: href present in output: %s", h)
	}
}

// --- MarkdownUnsafe --------------------------------------------------------

// Smoke: 允许原始 HTML 透传，用于 admin-authored bio 的颜色/span 注入。
func TestMarkdownUnsafe_Smoke_AllowsRawHTML(t *testing.T) {
	md := render.NewMarkdownUnsafe()
	h, err := md.ToHTML(`**粗** 和 <span style="color:red">红</span>`)
	if err != nil {
		t.Fatalf("ToHTML: %v", err)
	}
	s := string(h)
	if !strings.Contains(s, "<strong>粗</strong>") {
		t.Errorf("bold not rendered: %s", s)
	}
	if !strings.Contains(s, `<span style="color:red">红</span>`) {
		t.Errorf("raw span passed through but not verbatim: %s", s)
	}
}

// Edge（边界值）：空串 → 空 HTML，不应 panic。
func TestMarkdownUnsafe_Edge_Empty(t *testing.T) {
	md := render.NewMarkdownUnsafe()
	h, err := md.ToHTML("")
	if err != nil {
		t.Fatalf("ToHTML: %v", err)
	}
	if string(h) != "" {
		t.Errorf("empty input should yield empty HTML, got %q", h)
	}
}

// --- Templates --------------------------------------------------------------

func TestTemplates_Smoke_RenderLayoutWithPage(t *testing.T) {
	fsys := makeTemplatesFS(map[string]string{
		"layout.html": `<!doctype html><title>{{ block "title" . }}X{{ end }}</title>
<body>{{ if .Banner }}<div id="banner">BANNER</div>{{ end }}<main>{{ template "content" . }}</main></body>`,
		"home.html": `{{ define "title" }}Home{{ end }}
{{ define "content" }}<h1>Hi {{ .Data.Name }}</h1>{{ end }}`,
	})
	tpl, err := render.NewTemplates(fsys, nil)
	if err != nil {
		t.Fatalf("NewTemplates: %v", err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	if err := tpl.Render(w, req, 200, "home.html", map[string]any{"Name": "World"}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body, _ := io.ReadAll(w.Result().Body)
	s := string(body)
	if !strings.Contains(s, "Hi World") {
		t.Errorf("page slot missing: %s", s)
	}
	if !strings.Contains(s, "<title>Home</title>") {
		t.Errorf("title override missing: %s", s)
	}
	if strings.Contains(s, "BANNER") {
		t.Errorf("banner should not appear when context flag absent")
	}
}

// Edge: rendering an unknown page errors out cleanly.
func TestTemplates_Edge_UnknownPage(t *testing.T) {
	fsys := makeTemplatesFS(map[string]string{
		"layout.html": `<html><body>{{ template "content" . }}</body></html>`,
		"home.html":   `{{ define "content" }}x{{ end }}`,
	})
	tpl, err := render.NewTemplates(fsys, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	if err := tpl.Render(w, req, 200, "nope.html", nil); err == nil {
		t.Error("expected error for unknown page")
	}
}

// Edge: missing layout.html is a build-time error.
func TestTemplates_Edge_MissingLayout(t *testing.T) {
	fsys := makeTemplatesFS(map[string]string{
		"home.html": `{{ define "content" }}x{{ end }}`,
	})
	if _, err := render.NewTemplates(fsys, nil); err == nil {
		t.Error("expected error when layout missing")
	}
}

// Edge: invalid page syntax surfaces at render time (we parse lazily).
func TestTemplates_Edge_InvalidPageSyntax(t *testing.T) {
	fsys := makeTemplatesFS(map[string]string{
		"layout.html": `<html><body>{{ template "content" . }}</body></html>`,
		"bad.html":    `{{ define "content" }}{{ notAFunc }}{{ end }}`,
	})
	tpl, err := render.NewTemplates(fsys, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	if err := tpl.Render(w, req, 200, "bad.html", nil); err == nil {
		t.Error("expected parse error for bad template")
	}
}
