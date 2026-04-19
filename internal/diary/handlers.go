package diary

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/penguin/blog-server/internal/auth"
	"github.com/penguin/blog-server/internal/render"
)

// Handlers 持有 diary 相关 HTTP 入口需要的依赖。字段公开以便 main.go 在组装
// 时直接注入，方向参考 internal/admin.Handlers。
type Handlers struct {
	Store  *Store
	Tpl    *render.Templates
	Auth   *auth.Store
	Logger *slog.Logger
	// DocsRoot 指向 content/docs/ 的绝对路径；APIPromote 直接 os.WriteFile
	// 到这个目录，避免侵入 content 包（架构 §2"不改 internal/content/" 决策）。
	DocsRoot string
	// Now 允许测试注入固定时间；生产下为 nil 时走 time.Now()。
	Now func() time.Time
}

// New 构造 Handlers，确保 logger 非 nil，Now 有默认。
func New(store *Store, tpl *render.Templates, authStore *auth.Store, logger *slog.Logger, docsRoot string) *Handlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handlers{
		Store:    store,
		Tpl:      tpl,
		Auth:     authStore,
		Logger:   logger,
		DocsRoot: docsRoot,
		Now:      func() time.Time { return time.Now() },
	}
}

// session 提取 + 认证拒绝。GET 场景只要求登录态；POST 还要求 CSRF 匹配。
// 未登录返回 (zero, false)；handler 按语义自己决定 302 (HTML) / 401 (JSON API)。
func (h *Handlers) session(r *http.Request) (auth.Session, bool) {
	return h.Auth.ParseSession(r)
}

// writeJSON 统一 JSON 响应头 + 编码。传入 status=0 表示 200。
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if status > 0 {
		w.WriteHeader(status)
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// APIDay 处理 GET /diary/api/day?date=YYYY-MM-DD。未登录 401（JSON 而非 302，
// 因为是 XHR 调用点，客户端需要明确错误码而不是跟随跳转）。
func (h *Handlers) APIDay(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(r); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	date := r.URL.Query().Get("date")
	if _, err := h.Store.Validate(date); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_date"})
		return
	}
	body, _, err := h.Store.Get(date)
	if err != nil {
		h.Logger.Error("diary.api.day.read", slog.String("date", date), slog.String("err", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "read_failed"})
		return
	}
	writeJSON(w, 0, map[string]any{"body": body})
}

// APISave 处理 POST /diary/api/save。form 字段：date、content、csrf。
// 空 content 按 Store 约定等同 Delete（清空这一天的快捷路径）。
func (h *Handlers) APISave(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad_form"})
		return
	}
	if !auth.CSRFValid(sess, r.Form.Get("csrf")) {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "csrf"})
		return
	}
	date := r.Form.Get("date")
	if _, err := h.Store.Validate(date); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_date"})
		return
	}
	content := r.Form.Get("content")
	if err := h.Store.Put(date, content); err != nil {
		h.Logger.Error("diary.api.save.put", slog.String("date", date), slog.String("err", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "save_failed"})
		return
	}
	writeJSON(w, 0, map[string]any{
		"ok":      true,
		"savedAt": h.Now().Format(time.RFC3339),
	})
}

// APIDelete 处理 POST /diary/api/delete。form: date + csrf。
// Store.Delete 本身幂等 → 删除不存在的日期也返回 ok:true。
func (h *Handlers) APIDelete(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad_form"})
		return
	}
	if !auth.CSRFValid(sess, r.Form.Get("csrf")) {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "csrf"})
		return
	}
	date := r.Form.Get("date")
	if _, err := h.Store.Validate(date); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_date"})
		return
	}
	if err := h.Store.Delete(date); err != nil {
		h.Logger.Error("diary.api.delete", slog.String("date", date), slog.String("err", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "delete_failed"})
		return
	}
	writeJSON(w, 0, map[string]any{"ok": true})
}

// APIPromote 把一条日记作为种子复制到 content/docs/<slug>.md（状态 draft）。
// 日记原件**不变**，docs frontmatter 里**不写反向引用**（架构 §7 决策 6）。
//
// 成功：200 {"ok":true,"slug":"..."}；
// slug 冲突：409 {"ok":false,"error":"slug_conflict"}；
// 非法字段：400；没找到日记：404。
func (h *Handlers) APIPromote(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad_form"})
		return
	}
	if !auth.CSRFValid(sess, r.Form.Get("csrf")) {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "csrf"})
		return
	}
	date := r.Form.Get("date")
	title := r.Form.Get("title")
	slug := r.Form.Get("slug")
	category := r.Form.Get("category")

	if _, err := h.Store.Validate(date); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_date"})
		return
	}
	if len(title) == 0 || len(title) > 200 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_title"})
		return
	}
	if !isValidDocSlug(slug) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_slug"})
		return
	}

	body, exists, err := h.Store.Get(date)
	if err != nil {
		h.Logger.Error("diary.api.promote.get", slog.String("date", date), slog.String("err", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "read_failed"})
		return
	}
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "diary_not_found"})
		return
	}

	docPath := filepath.Join(h.DocsRoot, slug+".md")
	if _, err := os.Stat(docPath); err == nil {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "slug_conflict"})
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		h.Logger.Error("diary.api.promote.stat", slog.String("err", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "stat_failed"})
		return
	}

	today := h.Now().Format("2006-01-02")
	out := fmt.Sprintf(
		`---
title: %s
slug: %s
%screated: %s
updated: %s
status: draft
---

%s
`,
		escapeYAML(title),
		slug,
		categoryLine(category),
		today,
		today,
		body,
	)
	if err := os.MkdirAll(h.DocsRoot, 0o755); err != nil {
		h.Logger.Error("diary.api.promote.mkdir", slog.String("err", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "mkdir_failed"})
		return
	}
	if err := os.WriteFile(docPath, []byte(out), 0o644); err != nil {
		h.Logger.Error("diary.api.promote.write", slog.String("err", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "write_failed"})
		return
	}
	writeJSON(w, 0, map[string]any{"ok": true, "slug": slug})
}

// isValidDocSlug 复用 internal/content 包的 slug 规则但本地实现，
// 避免 diary 包反向依赖 content 包（架构决策：diary 不侵入 content）。
func isValidDocSlug(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-'
		if !ok {
			return false
		}
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	return true
}

// categoryLine 仅在 category 非空时产生 "category: xxx\n"，否则空字符串。
// 这样 frontmatter 里不会出现 "category: " 空值污染。
func categoryLine(c string) string {
	if c == "" {
		return ""
	}
	return "category: " + escapeYAML(c) + "\n"
}

// escapeYAML 对 YAML 字符串做最小化的安全处理：含冒号/井号/引号时加双引号并转义。
// 日记转正进来的 title / category 经此处理，避免破坏 docs frontmatter。
func escapeYAML(s string) string {
	needsQuote := false
	for _, r := range s {
		if r == ':' || r == '#' || r == '"' || r == '\n' || r == '\'' {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return s
	}
	// 用双引号包裹，转义内部双引号和反斜杠
	escaped := ""
	for _, r := range s {
		switch r {
		case '"':
			escaped += `\"`
		case '\\':
			escaped += `\\`
		case '\n':
			escaped += `\n`
		default:
			escaped += string(r)
		}
	}
	return `"` + escaped + `"`
}

// Page 处理 GET /diary。未登录 302 到 /manage/login?next=/diary；否则根据
// ?year&month 渲染月视图日历。非法参数按需求 2.1.2 回落到当前月。
//
// 为什么这里重复做 session 检查：/diary 不走 AuthGate 中间件（那是 /manage/* 的
// 专属），但共享同一个 session cookie。直接在 handler 里 ParseSession 最简单。
func (h *Handlers) Page(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.Auth.ParseSession(r)
	if !ok {
		http.Redirect(w, r, "/manage/login?next=/diary", http.StatusSeeOther)
		return
	}

	now := h.Now()
	year, month := NormaliseMonth(atoiOr(r.URL.Query().Get("year"), 0), atoiOr(r.URL.Query().Get("month"), 0), now)

	entries, err := h.Store.DatesIn(year, month)
	if err != nil {
		h.Logger.Error("diary.page.dates_in", slog.String("err", err.Error()))
		// 读不到目录不致命，用空集合继续渲染
		entries = map[int]bool{}
	}

	grid := MonthGrid(year, month, now, entries)

	// 上月 / 下月 URL（简单做法：构造 time.Time 取它的前/后一个月）
	cursor := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	prevCursor := cursor.AddDate(0, -1, 0)
	nextCursor := cursor.AddDate(0, 1, 0)

	data := map[string]any{
		"Year":      year,
		"Month":     int(month),
		"MonthName": month.String(), // "April" 等；模板里可再本地化
		"Grid":      grid,
		"Today":     now.Format("2006-01-02"),
		"PrevURL":   monthURL(prevCursor.Year(), int(prevCursor.Month())),
		"NextURL":   monthURL(nextCursor.Year(), int(nextCursor.Month())),
		"ThisURL":   "/diary",
		"CSRF":      sess.CSRF,
	}

	if err := h.Tpl.Render(w, r, http.StatusOK, "diary.html", data); err != nil {
		h.Logger.Error("diary.page.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func monthURL(year, month int) string {
	return "/diary?year=" + strconv.Itoa(year) + "&month=" + strconv.Itoa(month)
}

// atoiOr 把 query 字符串转 int，失败返回 fallback。非法值走 NormaliseMonth 继续
// 回落到当月，符合"非法输入不 400，静默回落"的需求 2.1.2 约定。
func atoiOr(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}
