package assets_test

import (
	"io"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/assets"
)

func readTheme(t *testing.T) string {
	t.Helper()
	f, err := assets.Static().Open("css/theme.css")
	if err != nil {
		t.Fatalf("open theme.css: %v", err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Smoke：.form-info 绑定了 fade-out-banner 动画，且 @keyframes 存在。
func TestTheme_Smoke_FormInfoAutoDismissAnimation(t *testing.T) {
	css := readTheme(t)
	// The animation rule must be on .form-info.
	// Find the .form-info block and check it declares the animation.
	idx := strings.Index(css, ".form-info {")
	if idx < 0 {
		t.Fatal(".form-info rule not found")
	}
	end := strings.Index(css[idx:], "}")
	if end < 0 {
		t.Fatal(".form-info rule not closed")
	}
	block := css[idx : idx+end]
	if !strings.Contains(block, "animation:") {
		t.Errorf(".form-info missing `animation:` declaration: %q", block)
	}
	if !strings.Contains(block, "fade-out-banner") {
		t.Errorf(".form-info animation name mismatch: %q", block)
	}
	if !strings.Contains(css, "@keyframes fade-out-banner") {
		t.Errorf("@keyframes fade-out-banner missing from theme.css")
	}
	// Must collapse vertical space at end, otherwise the page would leave a
	// blank gap where the banner was.
	kf := css[strings.Index(css, "@keyframes fade-out-banner"):]
	for _, want := range []string{"opacity: 0", "max-height: 0"} {
		if !strings.Contains(kf, want) {
			t.Errorf("keyframe missing %q", want)
		}
	}
}

// Edge（权限/语义类）：.form-err 不能带淡出动画——错误必须一直可见。
func TestTheme_Edge_FormErrHasNoAutoDismiss(t *testing.T) {
	css := readTheme(t)
	idx := strings.Index(css, ".form-err {")
	if idx < 0 {
		t.Fatal(".form-err rule not found")
	}
	end := strings.Index(css[idx:], "}")
	block := css[idx : idx+end]
	if strings.Contains(block, "animation") || strings.Contains(block, "fade-out-banner") {
		t.Errorf(".form-err accidentally auto-dismisses — error banners must persist: %q", block)
	}
}
