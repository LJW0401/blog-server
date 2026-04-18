package public

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/penguin/blog-server/internal/settings"
	"github.com/penguin/blog-server/internal/storage"
)

func newSettingsFixture(t *testing.T, kv map[string]string) (*Handlers, func()) {
	t.Helper()
	dir := t.TempDir()
	st, err := storage.Open(dir)
	if err != nil && !errors.Is(err, storage.ErrCorruptDB) {
		t.Fatal(err)
	}
	s := settings.New(st.DB)
	for k, v := range kv {
		if err := s.Set(k, v); err != nil {
			t.Fatalf("settings.Set(%s): %v", k, err)
		}
	}
	h := &Handlers{
		Logger:     slog.New(slog.NewTextHandler(nopWriter{}, nil)),
		SettingsDB: s,
	}
	h.Settings = h.resolveSettings
	return h, func() { _ = st.Close() }
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// Smoke: OSSLinks include GitHub + Gitee with URLs from settings when set.
func TestResolveSettings_Smoke_OSSLinksFromDB(t *testing.T) {
	h, cleanup := newSettingsFixture(t, map[string]string{
		"media_github": "https://github.com/penguin",
		"media_gitee":  "https://gitee.com/penguin",
	})
	t.Cleanup(cleanup)

	s := h.resolveSettings()
	if len(s.OSSLinks) != 2 {
		t.Fatalf("OSSLinks len=%d want 2", len(s.OSSLinks))
	}
	byPlatform := map[string]string{}
	for _, l := range s.OSSLinks {
		byPlatform[l.Platform] = l.URL
	}
	if got := byPlatform["GitHub"]; got != "https://github.com/penguin" {
		t.Errorf("GitHub URL = %q", got)
	}
	if got := byPlatform["Gitee"]; got != "https://gitee.com/penguin" {
		t.Errorf("Gitee URL = %q", got)
	}
}

// Edge (非法/缺失输入 + 边界值)：只填一个 OSS 链接，另一个的 URL 必须为空，
// 模板据此渲染成纯文本而非 <a>。
func TestResolveSettings_Edge_OSSLinkEmptyWhenUnset(t *testing.T) {
	h, cleanup := newSettingsFixture(t, map[string]string{
		"media_github": "https://github.com/penguin",
		// media_gitee intentionally unset
	})
	t.Cleanup(cleanup)

	s := h.resolveSettings()
	var gitee MediaLink
	for _, l := range s.OSSLinks {
		if l.Platform == "Gitee" {
			gitee = l
		}
	}
	if gitee.Platform != "Gitee" {
		t.Fatalf("Gitee entry missing from OSSLinks: %+v", s.OSSLinks)
	}
	if gitee.URL != "" {
		t.Errorf("Gitee URL = %q, want empty (unset)", gitee.URL)
	}
}
