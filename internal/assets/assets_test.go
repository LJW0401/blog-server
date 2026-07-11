package assets_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/assets"
)

// Smoke: required templates present in the embed FS.
func TestTemplates_Smoke_RequiredFilesEmbedded(t *testing.T) {
	want := []string{"layout.html", "home.html", "docs_list.html", "doc_detail.html"}
	for _, name := range want {
		if _, err := fs.Stat(assets.Templates(), name); err != nil {
			t.Errorf("missing template %q: %v", name, err)
		}
	}
}

// Smoke: theme.css is embedded and sized sensibly.
func TestStatic_Smoke_ThemeCSSReachable(t *testing.T) {
	b, err := fs.ReadFile(assets.Static(), "css/theme.css")
	if err != nil {
		t.Fatalf("read theme.css: %v", err)
	}
	if len(b) < 500 {
		t.Errorf("theme.css suspiciously small: %d bytes", len(b))
	}
}

// Smoke: Markdown images share the same maximum content width as body text.
func TestStatic_Smoke_ContentImagesMatchTextWidth(t *testing.T) {
	b, err := fs.ReadFile(assets.Static(), "css/theme.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	if !strings.Contains(css, ".doc-body img, .portfolio-body img { max-width: 100%;") {
		t.Error("document and portfolio images must not exceed the text content width")
	}
}

// Boundary: shrinking a large image must preserve its aspect ratio.
func TestStatic_Boundary_ContentImagesPreserveAspectRatio(t *testing.T) {
	b, err := fs.ReadFile(assets.Static(), "css/theme.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	if !strings.Contains(css, ".doc-body img, .portfolio-body img { max-width: 100%; height: auto; }") {
		t.Error("responsive content images must retain their intrinsic aspect ratio")
	}
}

// Smoke (WI-2.21 surrogate): dark mode rules exist and adjust orbs as per
// requirement 2.9 (降饱和/降亮度).
func TestStatic_Smoke_DarkModeRulesPresent(t *testing.T) {
	b, err := fs.ReadFile(assets.Static(), "css/theme.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	for _, frag := range []string{
		"@media (prefers-color-scheme: dark)",
		"--bg: #000",
		"saturate(0.55)",
		"brightness(0.7)",
	} {
		if !strings.Contains(css, frag) {
			t.Errorf("dark-mode rule missing: %q", frag)
		}
	}
}

// Smoke: reduced-motion respects the user preference per requirement 2.1.3.
func TestStatic_Smoke_ReducedMotionRule(t *testing.T) {
	b, _ := fs.ReadFile(assets.Static(), "css/theme.css")
	css := string(b)
	if !strings.Contains(css, "@media (prefers-reduced-motion: reduce)") {
		t.Error("reduced-motion rule missing")
	}
	if !strings.Contains(css, "animation: none !important") {
		t.Error("reduced-motion should disable animations")
	}
}
