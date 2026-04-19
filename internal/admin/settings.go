package admin

import (
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/penguin/blog-server/internal/auth"
	"github.com/penguin/blog-server/internal/settings"
)

// SettingsHandlers owns the /manage/settings page.
type SettingsHandlers struct {
	Parent   *Handlers
	Settings *settings.Store
	// Invalidate is called after a successful save so downstream consumers
	// (e.g. public handlers' 30s site-settings cache) drop their stale copy
	// immediately instead of waiting out the TTL. Optional; nil is a no-op.
	Invalidate func()
}

// SettingsKeys defines the canonical set of user-editable keys. The about_*
// keys drive the "关于我" card block on the homepage; empty values fall back
// to the hardcoded defaults baked into public.Handlers.about().
var SettingsKeys = []string{
	// Hero + footer contact.
	"name", "tagline", "location", "direction", "status",
	"qq_group",
	"media_github", "media_gitee",
	"media_bilibili", "media_douyin", "media_xiaohongshu",
	// About-me cards.
	"about_bio",
	"about_stack",      // comma/newline-separated list
	"about_experience", // lines of "标题 | 年份"
	"about_interests",  // comma/newline-separated list
}

var urlRe = regexp.MustCompile(`^https?://[^\s]+$`)
var qqRe = regexp.MustCompile(`^\d{5,12}$`)

// SettingsPage renders the editor form.
func (sh *SettingsHandlers) SettingsPage(w http.ResponseWriter, r *http.Request) {
	sess, _ := sh.Parent.Auth.ParseSession(r)
	values := sh.Settings.All()
	data := map[string]any{
		"Values": values,
		"CSRF":   sess.CSRF,
		"Error":  r.URL.Query().Get("e"),
		"Info":   r.URL.Query().Get("m"),
	}
	if err := sh.Parent.Tpl.Render(w, r, http.StatusOK, "admin_settings.html", data); err != nil {
		sh.Parent.Logger.Error("admin.settings.render", slog.String("err", err.Error()))
		http.Error(w, "internal", http.StatusInternalServerError)
	}
}

// SettingsSubmit validates + persists settings.
func (sh *SettingsHandlers) SettingsSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := sh.Parent.Auth.ParseSession(r)
	if !ok {
		http.Redirect(w, r, "/manage/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !auth.CSRFValid(sess, r.Form.Get("csrf")) {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}
	pending := map[string]string{}
	for _, k := range SettingsKeys {
		pending[k] = strings.TrimSpace(r.Form.Get(k))
	}
	if err := validateSettings(pending); err != nil {
		http.Redirect(w, r, "/manage/settings?e="+URLEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := sh.Settings.SetMany(pending); err != nil {
		sh.Parent.Logger.Error("admin.settings.save", slog.String("err", err.Error()))
		http.Redirect(w, r, "/manage/settings?e="+URLEscape("保存失败："+err.Error()), http.StatusSeeOther)
		return
	}
	if sh.Invalidate != nil {
		sh.Invalidate()
	}
	http.Redirect(w, r, "/manage/settings?m="+URLEscape("已保存"), http.StatusSeeOther)
}

func validateSettings(v map[string]string) error {
	if v["tagline"] == "" {
		return errMsg("tagline 不能为空")
	}
	for _, k := range []string{
		"media_github", "media_gitee",
		"media_bilibili", "media_douyin", "media_xiaohongshu",
	} {
		val := v[k]
		if val == "" {
			continue
		}
		if !urlRe.MatchString(val) {
			return errMsg(k + " 必须以 http:// 或 https:// 开头")
		}
	}
	if qq := v["qq_group"]; qq != "" && !qqRe.MatchString(qq) {
		return errMsg("QQ 群号必须为 5–12 位纯数字")
	}
	return nil
}

type simpleErr string

func (s simpleErr) Error() string { return string(s) }
func errMsg(s string) error       { return simpleErr(s) }
