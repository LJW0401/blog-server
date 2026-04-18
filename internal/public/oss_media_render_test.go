package public_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/public"
)

// E2E render: when a media URL is set, footer renders an <a href> link; when
// unset, it renders plain text. Wires Templates.SettingsFn exactly as
// cmd/server/main.go does in prod.
func TestFooter_Smoke_HyperlinksWhenURLsSet(t *testing.T) {
	h := setup(t, nil, nil)
	_ = h.SettingsDB.Set("media_github", "https://github.com/penguin")
	_ = h.SettingsDB.Set("media_gitee", "https://gitee.com/penguin")
	_ = h.SettingsDB.Set("media_bilibili", "https://space.bilibili.com/1")
	_ = h.SettingsDB.Set("qq_group", "12345678")
	_ = h.SettingsDB.Set("tagline", "x")
	// Wire Templates.SettingsFn like main.go does.
	setTemplatesSettingsFn(t, h)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.Home(rr, req)
	body := rr.Body.String()

	for _, want := range []string{
		`href="https://github.com/penguin"`,
		`>GitHub ›</a>`,
		`href="https://gitee.com/penguin"`,
		`>Gitee ›</a>`,
		`href="https://space.bilibili.com/1"`,
		`>B站 ›</a>`,
		`QQ：12345678`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("footer missing %q", want)
		}
	}
}

// Edge (非法/缺失输入)：URL 未填时必须渲染成纯文本 <li>Platform</li>，
// 不能渲染空 href="" 的 <a>（那是可点击的坏链接）。
func TestFooter_Edge_PlainTextWhenURLEmpty(t *testing.T) {
	h := setup(t, nil, nil)
	// Only GitHub filled; Gitee, 抖音, 小红书 left empty.
	_ = h.SettingsDB.Set("media_github", "https://github.com/penguin")
	_ = h.SettingsDB.Set("tagline", "x")
	setTemplatesSettingsFn(t, h)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.Home(rr, req)
	body := rr.Body.String()

	// Gitee should be plain text, not a link.
	if strings.Contains(body, `href=""`) {
		t.Error("footer has empty href — should render plain text when URL unset")
	}
	// Specifically: Gitee shouldn't appear as an anchor.
	if strings.Contains(body, `>Gitee ›</a>`) {
		t.Error("Gitee rendered as hyperlink despite empty URL")
	}
	if !strings.Contains(body, `<li>Gitee</li>`) {
		t.Error("Gitee plain-text entry not found in footer")
	}
	// GitHub still rendered as link.
	if !strings.Contains(body, `>GitHub ›</a>`) {
		t.Error("GitHub anchor missing despite URL set")
	}
}

// helper: grab the *render.Templates off a Handlers and install a SettingsFn
// that returns the same thing main.go does.
func setTemplatesSettingsFn(t *testing.T, h *public.Handlers) {
	t.Helper()
	h.Tpl.SettingsFn = func() any { return h.Settings() }
}
