// Package auth handles session cookies, CSRF tokens, and login rate limiting
// for the admin area. The zero value of its types is not usable; construct
// through the provided helpers.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Errors classified at this level. Callers use errors.Is.
var (
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrRateLimited        = errors.New("auth: rate limited")
	ErrInvalidSession     = errors.New("auth: invalid session")
	ErrCSRFMismatch       = errors.New("auth: csrf mismatch")
)

const (
	sessionCookieName = "blog_sess"
	sessionTTL        = 7 * 24 * time.Hour
	loginWindow       = 10 * time.Minute
	loginMaxFailures  = 5
)

// Session represents a verified user session. Zero value means anonymous.
type Session struct {
	SID       string
	Username  string
	IssuedAt  time.Time
	ExpiresAt time.Time
	CSRF      string
	UAFP      string // first chars of User-Agent when the session was issued
}

type sessionPayload struct {
	SID  string `json:"s"`
	U    string `json:"u"`
	Iat  int64  `json:"iat"`
	Exp  int64  `json:"exp"`
	CSRF string `json:"c"`
	UA   string `json:"ua"`
}

// SessionRecord is the server-side row for a session. Exposed to handlers that
// render the "已登陆设备" list; the raw cookie is never shown.
type SessionRecord struct {
	SID       string
	Username  string
	UserAgent string
	IP        string
	IssuedAt  time.Time
	RevokedAt *time.Time // nil 表示活跃
}

// Store bundles the dependencies needed by the admin login / password change
// handlers: database handle, session signing secret, and bcrypt hash source.
type Store struct {
	db     *sql.DB
	secret []byte
}

// NewStore returns a Store wired to the given DB and session HMAC secret.
func NewStore(db *sql.DB, secret []byte) *Store {
	return &Store{db: db, secret: secret}
}

// LoadOrCreateSecret reads the session HMAC secret from the site_settings
// table; generates a fresh 32-byte random key on first boot.
func LoadOrCreateSecret(db *sql.DB) ([]byte, error) {
	const key = "session_hmac_secret"
	var hexed string
	row := db.QueryRow(`SELECT v FROM site_settings WHERE k = ?`, key)
	if err := row.Scan(&hexed); err == nil {
		decoded, err := hex.DecodeString(hexed)
		if err == nil && len(decoded) >= 16 {
			return decoded, nil
		}
	}
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return nil, fmt.Errorf("auth: rand: %w", err)
	}
	_, err := db.Exec(`INSERT INTO site_settings (k, v, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(k) DO UPDATE SET v=excluded.v, updated_at=excluded.updated_at`,
		key, hex.EncodeToString(buf[:]), time.Now().Unix())
	if err != nil {
		return nil, err
	}
	return buf[:], nil
}

// --- Password verification -------------------------------------------------

// VerifyPassword compares raw against the stored bcrypt hash. Returns
// ErrInvalidCredentials on mismatch; other errors bubble up.
func VerifyPassword(hash, raw string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(raw))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return ErrInvalidCredentials
		}
		return err
	}
	return nil
}

// HashPassword returns a bcrypt hash of raw at cost >= 10.
func HashPassword(raw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(raw), 10)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// --- Session issuance & verification ---------------------------------------

// IssueSession creates a new session cookie for the given username & request.
// Returns the session plus the *http.Cookie to Set.
//
// 同步在 sessions 表里插入一条记录 —— 用于后台"已登陆设备"列表与撤销。
// cookie payload 里带 sid，ParseSession 时会查表比对；表里缺这行或被
// 撤销，session 视为无效，用户重新走登陆流程。
func (s *Store) IssueSession(username, userAgent, ip string) (Session, *http.Cookie, error) {
	now := time.Now().UTC()
	var csrfBytes [24]byte
	if _, err := rand.Read(csrfBytes[:]); err != nil {
		return Session{}, nil, err
	}
	var sidBytes [16]byte
	if _, err := rand.Read(sidBytes[:]); err != nil {
		return Session{}, nil, err
	}
	sess := Session{
		SID:       hex.EncodeToString(sidBytes[:]),
		Username:  username,
		IssuedAt:  now,
		ExpiresAt: now.Add(sessionTTL),
		CSRF:      hex.EncodeToString(csrfBytes[:]),
		UAFP:      uaFingerprint(userAgent),
	}
	if _, err := s.db.Exec(
		`INSERT INTO sessions (sid, username, user_agent, ip, issued_at) VALUES (?, ?, ?, ?, ?)`,
		sess.SID, username, userAgent, ip, now.Unix(),
	); err != nil {
		return Session{}, nil, err
	}
	cookie, err := s.encode(sess)
	if err != nil {
		return Session{}, nil, err
	}
	return sess, cookie, nil
}

// ParseSession extracts and verifies a session from the request. Returns the
// zero Session (valid=false) when no or invalid cookie is present.
//
// 额外校验：cookie 里的 sid 必须命中 sessions 表且未被撤销。没有 sid 的老
// cookie（v1.6.2 以前签发）一律视为无效。
func (s *Store) ParseSession(r *http.Request) (Session, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return Session{}, false
	}
	sess, err := s.decode(c.Value)
	if err != nil {
		return Session{}, false
	}
	if time.Now().UTC().After(sess.ExpiresAt) {
		return Session{}, false
	}
	// UA binding: if the request's UA has drifted significantly, reject.
	if sess.UAFP != uaFingerprint(r.UserAgent()) {
		return Session{}, false
	}
	if sess.SID == "" {
		return Session{}, false
	}
	var revokedAt sql.NullInt64
	row := s.db.QueryRow(`SELECT revoked_at FROM sessions WHERE sid = ?`, sess.SID)
	if err := row.Scan(&revokedAt); err != nil {
		return Session{}, false // 记录不存在 / 查询失败 —— 视为无效
	}
	if revokedAt.Valid {
		return Session{}, false
	}
	return sess, true
}

// ListSessions 返回 username 名下所有活跃会话（未撤销且未过期）。
// 按签发时间倒序，最新的在上面。
func (s *Store) ListSessions(username string) ([]SessionRecord, error) {
	cutoff := time.Now().UTC().Add(-sessionTTL).Unix()
	rows, err := s.db.Query(
		`SELECT sid, username, user_agent, ip, issued_at, revoked_at
		 FROM sessions
		 WHERE username = ? AND revoked_at IS NULL AND issued_at >= ?
		 ORDER BY issued_at DESC`,
		username, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SessionRecord
	for rows.Next() {
		var r SessionRecord
		var issued int64
		var revoked sql.NullInt64
		if err := rows.Scan(&r.SID, &r.Username, &r.UserAgent, &r.IP, &issued, &revoked); err != nil {
			return nil, err
		}
		r.IssuedAt = time.Unix(issued, 0).UTC()
		if revoked.Valid {
			t := time.Unix(revoked.Int64, 0).UTC()
			r.RevokedAt = &t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RevokeSession 把指定 sid 标记为已撤销；该 sid 下次 ParseSession 会被拒。
// 仅允许撤销 username 名下的 sid，防止越权。
// 返回影响行数：0 表示 sid 不存在或不属于该 user（此时静默无操作）。
func (s *Store) RevokeSession(sid, username string) (int64, error) {
	if sid == "" {
		return 0, nil
	}
	res, err := s.db.Exec(
		`UPDATE sessions SET revoked_at = ? WHERE sid = ? AND username = ? AND revoked_at IS NULL`,
		time.Now().UTC().Unix(), sid, username,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ClearCookie returns a cookie that deletes the session on the client.
func (s *Store) ClearCookie() *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	}
}

func (s *Store) encode(sess Session) (*http.Cookie, error) {
	p := sessionPayload{
		SID: sess.SID,
		U:   sess.Username, Iat: sess.IssuedAt.Unix(), Exp: sess.ExpiresAt.Unix(),
		CSRF: sess.CSRF, UA: sess.UAFP,
	}
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(body)
	sigB64 := s.sign([]byte(payloadB64))
	value := payloadB64 + "." + sigB64
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Expires:  sess.ExpiresAt,
	}, nil
}

func (s *Store) decode(raw string) (Session, error) {
	i := strings.IndexByte(raw, '.')
	if i <= 0 {
		return Session{}, ErrInvalidSession
	}
	payloadB64, gotSig := raw[:i], raw[i+1:]
	wantSig := s.sign([]byte(payloadB64))
	if !hmac.Equal([]byte(gotSig), []byte(wantSig)) {
		return Session{}, ErrInvalidSession
	}
	body, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return Session{}, ErrInvalidSession
	}
	var p sessionPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return Session{}, ErrInvalidSession
	}
	return Session{
		SID:       p.SID,
		Username:  p.U,
		IssuedAt:  time.Unix(p.Iat, 0),
		ExpiresAt: time.Unix(p.Exp, 0),
		CSRF:      p.CSRF,
		UAFP:      p.UA,
	}, nil
}

func (s *Store) sign(b []byte) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write(b)
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// CSRFValid reports whether the given token matches the session's CSRF.
func CSRFValid(sess Session, token string) bool {
	if sess.CSRF == "" || token == "" {
		return false
	}
	return hmac.Equal([]byte(sess.CSRF), []byte(token))
}

func uaFingerprint(ua string) string {
	if len(ua) > 64 {
		ua = ua[:64]
	}
	h := sha256.Sum256([]byte(ua))
	return hex.EncodeToString(h[:8])
}

// --- Login rate limit ------------------------------------------------------

// RegisterFailure records a failed login attempt for ip; returns
// (current_count, reset_at, rate_limited?).
func (s *Store) RegisterFailure(ip string) (int, time.Time, bool) {
	now := time.Now().UTC()
	end := now.Add(loginWindow)
	tx, err := s.db.Begin()
	if err != nil {
		return 0, time.Time{}, false
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRow(`SELECT count, window_end_at FROM login_failures WHERE ip=?`, ip)
	var count int
	var endUnix int64
	if err := row.Scan(&count, &endUnix); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, false
	}

	if count == 0 || time.Unix(endUnix, 0).Before(now) {
		count = 1
		endUnix = end.Unix()
	} else {
		count++
	}
	_, _ = tx.Exec(`INSERT INTO login_failures (ip, count, window_end_at) VALUES (?, ?, ?)
		ON CONFLICT(ip) DO UPDATE SET count=excluded.count, window_end_at=excluded.window_end_at`,
		ip, count, endUnix)
	_ = tx.Commit()
	return count, time.Unix(endUnix, 0), count >= loginMaxFailures
}

// IsRateLimited reports whether the given ip is currently blocked.
func (s *Store) IsRateLimited(ip string) (bool, time.Time) {
	row := s.db.QueryRow(`SELECT count, window_end_at FROM login_failures WHERE ip=?`, ip)
	var count int
	var endUnix int64
	if err := row.Scan(&count, &endUnix); err != nil {
		return false, time.Time{}
	}
	end := time.Unix(endUnix, 0)
	if time.Now().UTC().After(end) {
		return false, end
	}
	return count >= loginMaxFailures, end
}

// ClearFailures removes the failure record for ip (call on successful login).
func (s *Store) ClearFailures(ip string) {
	_, _ = s.db.Exec(`DELETE FROM login_failures WHERE ip=?`, ip)
}

// RemoteIP extracts a client IP best-effort from the request.
func RemoteIP(r *http.Request) string {
	// Prefer X-Forwarded-For first entry (set by the reverse proxy).
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		if i := strings.IndexByte(xf, ','); i > 0 {
			return strings.TrimSpace(xf[:i])
		}
		return strings.TrimSpace(xf)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 {
		return host[:i]
	}
	return host
}
