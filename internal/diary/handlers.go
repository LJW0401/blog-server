package diary

import (
	"encoding/json"
	"log/slog"
	"net/http"
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
	// Now 允许测试注入固定时间；生产下为 nil 时走 time.Now()。
	Now func() time.Time
}

// New 构造 Handlers，确保 logger 非 nil，Now 有默认。
func New(store *Store, tpl *render.Templates, authStore *auth.Store, logger *slog.Logger) *Handlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handlers{
		Store:  store,
		Tpl:    tpl,
		Auth:   authStore,
		Logger: logger,
		Now:    func() time.Time { return time.Now() },
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

// (APIPromote 已删除 — 原来的"3 连 prompt 弹窗 + server 直接写 docs"流程
// 被换成了"/manage/docs/new?diary_date=..." 的 SSR 预填路径，入口放在
// internal/admin/docs.go:NewDoc。这样用户可以在熟悉的文档编辑器里一次
// 填完 title/slug/category，避免串着弹 3 个 prompt。)

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
