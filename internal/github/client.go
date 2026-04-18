// Package github provides a minimal HTTP client for fetching repository
// metadata from GitHub's REST API, a SQLite-backed cache and a periodic sync
// loop. ETag conditional requests keep steady-state usage under the
// unauthenticated rate limit (60/hour).
package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client talks to GitHub's REST v3 API. The zero value is not usable — use
// NewClient.
type Client struct {
	hc      *http.Client
	base    string // default: https://api.github.com; overridden in tests
	token   string // optional personal access token
	timeout time.Duration
}

// ClientOpt configures a Client via NewClient.
type ClientOpt func(*Client)

// WithBaseURL overrides the API base (tests).
func WithBaseURL(u string) ClientOpt { return func(c *Client) { c.base = strings.TrimRight(u, "/") } }

// WithToken attaches `Authorization: token <t>` when non-empty.
func WithToken(t string) ClientOpt { return func(c *Client) { c.token = t } }

// WithHTTPClient injects a custom HTTP client (tests).
func WithHTTPClient(hc *http.Client) ClientOpt { return func(c *Client) { c.hc = hc } }

// WithTimeout adjusts the per-request timeout (default 10s).
func WithTimeout(d time.Duration) ClientOpt { return func(c *Client) { c.timeout = d } }

// NewClient constructs a Client with sensible defaults.
func NewClient(opts ...ClientOpt) *Client {
	c := &Client{
		base:    "https://api.github.com",
		timeout: 10 * time.Second,
	}
	for _, o := range opts {
		o(c)
	}
	if c.hc == nil {
		c.hc = &http.Client{Timeout: c.timeout}
	}
	return c
}

// Errors returned by the Client. Tests rely on errors.Is.
var (
	ErrNotFound     = errors.New("github: not found")
	ErrUnauthorized = errors.New("github: unauthorized")
	ErrRateLimited  = errors.New("github: rate limited")
	ErrUpstream     = errors.New("github: upstream error")
	ErrNetwork      = errors.New("github: network")
)

// RateLimitError carries the Retry-After hint when returned together with
// ErrRateLimited. Callers can inspect it via errors.As.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("github: rate limited (retry after %s)", e.RetryAfter)
}

// RepoInfo holds the fields we store per repository.
type RepoInfo struct {
	FullName      string    `json:"full_name"`
	Description   string    `json:"description"`
	HTMLURL       string    `json:"html_url"`
	Stars         int       `json:"stargazers_count"`
	Forks         int       `json:"forks_count"`
	Language      string    `json:"language"`
	PushedAt      time.Time `json:"pushed_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Archived      bool      `json:"archived"`
	Private       bool      `json:"private"`
	ReadmeExcerpt string    `json:"readme_excerpt,omitempty"`
}

// GetRepoResult is the outcome of a GetRepo call.
type GetRepoResult struct {
	Info        *RepoInfo
	ETag        string
	NotModified bool // 304 — caller should keep their cached payload
}

// GetRepo fetches metadata for the given owner/name. If priorETag is non-empty
// it is sent as If-None-Match; 304 responses yield NotModified=true and nil
// Info so callers keep their cached copy.
func (c *Client) GetRepo(ctx context.Context, owner, name, priorETag string) (*GetRepoResult, error) {
	path := fmt.Sprintf("/repos/%s/%s", owner, name)
	body, hdr, err := c.do(ctx, path, priorETag)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return &GetRepoResult{NotModified: true, ETag: hdr.Get("ETag")}, nil
	}
	var info RepoInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("%w: decode repo: %v", ErrUpstream, err)
	}
	return &GetRepoResult{Info: &info, ETag: hdr.Get("ETag")}, nil
}

// GetReadmeExcerpt returns up to `limit` runes of the repository's README as
// plain UTF-8 text. A repository with no README yields ("", nil).
func (c *Client) GetReadmeExcerpt(ctx context.Context, owner, name string, limit int) (string, error) {
	path := fmt.Sprintf("/repos/%s/%s/readme", owner, name)
	body, _, err := c.do(ctx, path, "")
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	var payload struct {
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("%w: decode readme: %v", ErrUpstream, err)
	}
	raw := strings.ReplaceAll(payload.Content, "\n", "")
	decoded, derr := base64.StdEncoding.DecodeString(raw)
	if derr != nil {
		return "", nil // if encoding unexpected, swallow — not critical
	}
	text := strings.TrimSpace(string(decoded))
	runes := []rune(text)
	if len(runes) > limit {
		return strings.TrimSpace(string(runes[:limit])) + "…", nil
	}
	return text, nil
}

// do performs a GET and classifies outcomes. Returns (body, headers, err).
// body is nil for 304 responses; err is non-nil on 4xx/5xx or transport fails.
func (c *Client) do(ctx context.Context, path, priorETag string) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: new req: %v", ErrNetwork, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "blog-server/1.0")
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}
	if priorETag != "" {
		req.Header.Set("If-None-Match", priorETag)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNotModified:
		return nil, resp.Header, nil
	case http.StatusOK:
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: read: %v", ErrUpstream, err)
		}
		return b, resp.Header, nil
	case http.StatusNotFound:
		return nil, nil, fmt.Errorf("%w: %s", ErrNotFound, path)
	case http.StatusUnauthorized, http.StatusForbidden:
		// 403 with rate limit remaining=0 is really a rate-limit case.
		if rem := resp.Header.Get("X-RateLimit-Remaining"); rem == "0" {
			return nil, nil, &RateLimitError{RetryAfter: retryAfter(resp.Header)}
		}
		return nil, nil, fmt.Errorf("%w: %s", ErrUnauthorized, resp.Status)
	case http.StatusTooManyRequests:
		return nil, nil, &RateLimitError{RetryAfter: retryAfter(resp.Header)}
	default:
		if resp.StatusCode >= 500 {
			return nil, nil, fmt.Errorf("%w: %s", ErrUpstream, resp.Status)
		}
		return nil, nil, fmt.Errorf("%w: unexpected status %s", ErrUpstream, resp.Status)
	}
}

// Make *RateLimitError satisfy errors.Is against ErrRateLimited.
func (e *RateLimitError) Is(target error) bool { return target == ErrRateLimited }

func retryAfter(h http.Header) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	// Fallback: use X-RateLimit-Reset (epoch seconds) if present.
	if v := h.Get("X-RateLimit-Reset"); v != "" {
		if epoch, err := strconv.ParseInt(v, 10, 64); err == nil {
			delta := time.Until(time.Unix(epoch, 0))
			if delta > 0 {
				return delta
			}
		}
	}
	return 60 * time.Second
}
