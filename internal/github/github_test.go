package github_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/penguin/blog-server/internal/github"
	"github.com/penguin/blog-server/internal/storage"
)

// --- Mock helpers ----------------------------------------------------------

type mockBehavior struct {
	repoStatus    int
	repoPayload   string
	repoEtag      string
	readmeStatus  int
	readmePayload string
	retryAfterSec int
	rlRemaining   string
	rlReset       string
	delay         time.Duration
	repoCalls     int32
	readmeCalls   int32
}

func newMock(t *testing.T, mb *mockBehavior) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		if mb.delay > 0 {
			time.Sleep(mb.delay)
		}
		if r.URL.Path[len(r.URL.Path)-7:] == "/readme" {
			atomic.AddInt32(&mb.readmeCalls, 1)
			w.WriteHeader(mb.readmeStatus)
			_, _ = io.WriteString(w, mb.readmePayload)
			return
		}
		atomic.AddInt32(&mb.repoCalls, 1)
		if mb.repoEtag != "" {
			w.Header().Set("ETag", mb.repoEtag)
			if r.Header.Get("If-None-Match") == mb.repoEtag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		if mb.rlRemaining != "" {
			w.Header().Set("X-RateLimit-Remaining", mb.rlRemaining)
		}
		if mb.rlReset != "" {
			w.Header().Set("X-RateLimit-Reset", mb.rlReset)
		}
		if mb.retryAfterSec > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(mb.retryAfterSec))
		}
		w.WriteHeader(mb.repoStatus)
		_, _ = io.WriteString(w, mb.repoPayload)
	})
	return httptest.NewServer(mux)
}

const sampleRepoJSON = `{
  "full_name": "penguin/blog-server",
  "description": "demo",
  "html_url": "https://example.invalid/penguin/blog-server",
  "stargazers_count": 42,
  "forks_count": 7,
  "language": "Go",
  "pushed_at": "2026-04-15T10:00:00Z",
  "updated_at": "2026-04-15T10:00:00Z",
  "archived": false,
  "private": false
}`

func sampleReadmeJSON() string {
	content := base64.StdEncoding.EncodeToString([]byte("# Hi\n\nThis is a sample README with **bold** and `code`.\n"))
	return `{"encoding": "base64", "content": "` + content + `"}`
}

// --- Smoke: client ---------------------------------------------------------

func TestClient_Smoke_GetRepo(t *testing.T) {
	mb := &mockBehavior{repoStatus: 200, repoPayload: sampleRepoJSON, repoEtag: `"abc"`}
	srv := newMock(t, mb)
	t.Cleanup(srv.Close)
	c := github.NewClient(github.WithBaseURL(srv.URL))
	res, err := c.GetRepo(context.Background(), "penguin", "blog-server", "")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if res.Info.FullName != "penguin/blog-server" || res.Info.Stars != 42 || res.ETag != `"abc"` {
		t.Errorf("unexpected: %+v etag=%q", res.Info, res.ETag)
	}
}

func TestClient_Smoke_GetRepo_NotModified(t *testing.T) {
	mb := &mockBehavior{repoStatus: 200, repoPayload: sampleRepoJSON, repoEtag: `"v1"`}
	srv := newMock(t, mb)
	t.Cleanup(srv.Close)
	c := github.NewClient(github.WithBaseURL(srv.URL))
	res, err := c.GetRepo(context.Background(), "penguin", "blog-server", `"v1"`)
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if !res.NotModified {
		t.Error("expected NotModified")
	}
	if res.Info != nil {
		t.Error("info should be nil on 304")
	}
}

func TestClient_Smoke_GetReadmeExcerpt(t *testing.T) {
	mb := &mockBehavior{readmeStatus: 200, readmePayload: sampleReadmeJSON()}
	srv := newMock(t, mb)
	t.Cleanup(srv.Close)
	c := github.NewClient(github.WithBaseURL(srv.URL))
	excerpt, err := c.GetReadmeExcerpt(context.Background(), "penguin", "blog-server", 200)
	if err != nil {
		t.Fatalf("GetReadmeExcerpt: %v", err)
	}
	if excerpt == "" {
		t.Error("expected non-empty excerpt")
	}
}

// --- Edge: client ---------------------------------------------------------

func TestClient_Edge_NotFound(t *testing.T) {
	mb := &mockBehavior{repoStatus: 404, repoPayload: `{}`}
	srv := newMock(t, mb)
	t.Cleanup(srv.Close)
	c := github.NewClient(github.WithBaseURL(srv.URL))
	_, err := c.GetRepo(context.Background(), "foo", "nope", "")
	if !errors.Is(err, github.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestClient_Edge_Unauthorized(t *testing.T) {
	mb := &mockBehavior{repoStatus: 401, repoPayload: `{}`}
	srv := newMock(t, mb)
	t.Cleanup(srv.Close)
	c := github.NewClient(github.WithBaseURL(srv.URL))
	_, err := c.GetRepo(context.Background(), "foo", "x", "")
	if !errors.Is(err, github.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestClient_Edge_RateLimit(t *testing.T) {
	mb := &mockBehavior{repoStatus: 429, repoPayload: `{}`, retryAfterSec: 20}
	srv := newMock(t, mb)
	t.Cleanup(srv.Close)
	c := github.NewClient(github.WithBaseURL(srv.URL))
	_, err := c.GetRepo(context.Background(), "foo", "x", "")
	var rl *github.RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("expected RateLimitError, got %v", err)
	}
	if rl.RetryAfter != 20*time.Second {
		t.Errorf("retry after: %s", rl.RetryAfter)
	}
	if !errors.Is(err, github.ErrRateLimited) {
		t.Error("errors.Is(ErrRateLimited) should match")
	}
}

func TestClient_Edge_RateLimit_403WithZero(t *testing.T) {
	mb := &mockBehavior{repoStatus: 403, repoPayload: `{}`, rlRemaining: "0", rlReset: strconv.FormatInt(time.Now().Add(30*time.Second).Unix(), 10)}
	srv := newMock(t, mb)
	t.Cleanup(srv.Close)
	c := github.NewClient(github.WithBaseURL(srv.URL))
	_, err := c.GetRepo(context.Background(), "foo", "x", "")
	if !errors.Is(err, github.ErrRateLimited) {
		t.Errorf("403 + remaining=0 should be rate-limit: %v", err)
	}
}

func TestClient_Edge_Upstream5xx(t *testing.T) {
	mb := &mockBehavior{repoStatus: 502, repoPayload: `{}`}
	srv := newMock(t, mb)
	t.Cleanup(srv.Close)
	c := github.NewClient(github.WithBaseURL(srv.URL))
	_, err := c.GetRepo(context.Background(), "foo", "x", "")
	if !errors.Is(err, github.ErrUpstream) {
		t.Errorf("expected ErrUpstream, got %v", err)
	}
}

func TestClient_Edge_Timeout(t *testing.T) {
	mb := &mockBehavior{repoStatus: 200, repoPayload: sampleRepoJSON, delay: 150 * time.Millisecond}
	srv := newMock(t, mb)
	t.Cleanup(srv.Close)
	c := github.NewClient(
		github.WithBaseURL(srv.URL),
		github.WithTimeout(50*time.Millisecond),
		github.WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}),
	)
	_, err := c.GetRepo(context.Background(), "foo", "x", "")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, github.ErrNetwork) {
		t.Errorf("expected ErrNetwork, got %v", err)
	}
}

// --- Cache + Syncer --------------------------------------------------------

type fixedRepos []string

func (f fixedRepos) Repos() []string { return []string(f) }

func openStore(t *testing.T) *storage.Store {
	t.Helper()
	st, err := storage.Open(t.TempDir())
	if err != nil && !errors.Is(err, storage.ErrCorruptDB) {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestSyncer_Smoke_SyncUpdatesCache(t *testing.T) {
	mb := &mockBehavior{repoStatus: 200, repoPayload: sampleRepoJSON, repoEtag: `"v1"`,
		readmeStatus: 200, readmePayload: sampleReadmeJSON()}
	srv := newMock(t, mb)
	t.Cleanup(srv.Close)

	st := openStore(t)
	cache := github.NewCache(st.DB)
	c := github.NewClient(github.WithBaseURL(srv.URL))
	sy := github.NewSyncer(c, cache, fixedRepos{"penguin/blog-server"},
		github.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	sy.SyncNow(context.Background())

	entry, err := cache.Get(context.Background(), "penguin/blog-server")
	if err != nil || entry == nil {
		t.Fatalf("cache miss after sync: %v", err)
	}
	if entry.Info == nil || entry.Info.Stars != 42 {
		t.Errorf("bad info: %+v", entry.Info)
	}
	if entry.ETag != `"v1"` {
		t.Errorf("etag: %s", entry.ETag)
	}
	if entry.Info.ReadmeExcerpt == "" {
		t.Error("readme excerpt missing")
	}
}

func TestSyncer_Smoke_ConditionalRequestSaves304(t *testing.T) {
	mb := &mockBehavior{repoStatus: 200, repoPayload: sampleRepoJSON, repoEtag: `"v1"`,
		readmeStatus: 200, readmePayload: sampleReadmeJSON()}
	srv := newMock(t, mb)
	t.Cleanup(srv.Close)
	st := openStore(t)
	cache := github.NewCache(st.DB)
	c := github.NewClient(github.WithBaseURL(srv.URL))
	sy := github.NewSyncer(c, cache, fixedRepos{"penguin/blog-server"},
		github.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	sy.SyncNow(context.Background())
	firstEtag := atomic.LoadInt32(&mb.repoCalls)
	sy.SyncNow(context.Background())
	// Second sync should hit 304 path; the repo handler still receives the
	// call (it's what returns 304), so calls double. But critically, the
	// cache should still hold the same payload.
	if atomic.LoadInt32(&mb.repoCalls) != firstEtag+1 {
		t.Errorf("expected 1 additional call, got %d -> %d",
			firstEtag, atomic.LoadInt32(&mb.repoCalls))
	}
	entry, _ := cache.Get(context.Background(), "penguin/blog-server")
	if entry == nil || entry.Info.Stars != 42 {
		t.Error("cache payload lost after 304")
	}
}

// Edge: one of two repos fails, the other succeeds; previous cache for the
// failing repo is preserved.
func TestSyncer_Edge_SingleFailKeepsOldCache(t *testing.T) {
	// First pass: both succeed.
	okMB := &mockBehavior{repoStatus: 200, repoPayload: sampleRepoJSON, repoEtag: `"v1"`,
		readmeStatus: 200, readmePayload: sampleReadmeJSON()}
	srv := newMock(t, okMB)
	st := openStore(t)
	cache := github.NewCache(st.DB)
	c := github.NewClient(github.WithBaseURL(srv.URL))
	sy := github.NewSyncer(c, cache, fixedRepos{"a/one", "b/two"},
		github.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	sy.SyncNow(context.Background())
	srv.Close()

	// Pre-check: both cached.
	e1, _ := cache.Get(context.Background(), "a/one")
	e2, _ := cache.Get(context.Background(), "b/two")
	if e1 == nil || e2 == nil {
		t.Fatal("initial sync failed to populate")
	}

	// Second pass: /repos/a/one -> 500; /repos/b/two -> 200
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/a/one", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	mux.HandleFunc("/repos/b/two", func(w http.ResponseWriter, r *http.Request) {
		// README request for "two"
		if r.URL.Path == "/repos/b/two/readme" {
			_, _ = io.WriteString(w, sampleReadmeJSON())
			return
		}
		_, _ = io.WriteString(w, sampleRepoJSON)
	})
	srv2 := httptest.NewServer(mux)
	t.Cleanup(srv2.Close)
	c2 := github.NewClient(github.WithBaseURL(srv2.URL))
	sy2 := github.NewSyncer(c2, cache, fixedRepos{"a/one", "b/two"},
		github.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	sy2.SyncNow(context.Background())

	// a/one: old info preserved + last_error populated.
	after1, _ := cache.Get(context.Background(), "a/one")
	if after1 == nil || after1.Info == nil || after1.Info.Stars != 42 {
		t.Errorf("a/one info lost after failure: %+v", after1)
	}
	if after1.LastError == "" {
		t.Error("a/one last_error empty")
	}
	after2, _ := cache.Get(context.Background(), "b/two")
	if after2 == nil || after2.LastError != "" {
		t.Errorf("b/two should succeed: %+v", after2)
	}
}

// Edge: 429 is recorded as last_error; old info preserved.
func TestSyncer_Edge_RateLimitBackoff(t *testing.T) {
	okMB := &mockBehavior{repoStatus: 200, repoPayload: sampleRepoJSON, repoEtag: `"v1"`,
		readmeStatus: 200, readmePayload: sampleReadmeJSON()}
	srv := newMock(t, okMB)
	t.Cleanup(srv.Close)
	st := openStore(t)
	cache := github.NewCache(st.DB)
	c := github.NewClient(github.WithBaseURL(srv.URL))
	sy := github.NewSyncer(c, cache, fixedRepos{"a/one"},
		github.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	sy.SyncNow(context.Background())

	// Switch to 429.
	srv.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/a/one", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "20")
		w.WriteHeader(429)
	})
	srv2 := httptest.NewServer(mux)
	t.Cleanup(srv2.Close)
	c2 := github.NewClient(github.WithBaseURL(srv2.URL))
	sy2 := github.NewSyncer(c2, cache, fixedRepos{"a/one"},
		github.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	sy2.SyncNow(context.Background())

	e, _ := cache.Get(context.Background(), "a/one")
	if e == nil || e.LastError == "" {
		t.Error("expected last_error populated after 429")
	}
	if e.Info == nil || e.Info.Stars != 42 {
		t.Error("old info must be preserved through 429")
	}
}

func TestSyncer_Edge_InvalidRepoIdentifier(t *testing.T) {
	st := openStore(t)
	cache := github.NewCache(st.DB)
	c := github.NewClient()
	sy := github.NewSyncer(c, cache, fixedRepos{"not-a-valid-repo"},
		github.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	sy.SyncNow(context.Background())
	e, _ := cache.Get(context.Background(), "not-a-valid-repo")
	if e == nil || e.LastError == "" {
		t.Errorf("expected cached error row, got %+v", e)
	}
}

// Smoke: empty repo list is a no-op (no crash).
func TestSyncer_Edge_NoReposNoop(t *testing.T) {
	st := openStore(t)
	cache := github.NewCache(st.DB)
	sy := github.NewSyncer(github.NewClient(), cache, fixedRepos{},
		github.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	sy.SyncNow(context.Background())
	list, _ := cache.List(context.Background())
	if len(list) != 0 {
		t.Errorf("empty repos should leave cache empty, got %d", len(list))
	}
}

// Storage sanity check: the integration test creates files inside the temp
// data dir without leaking into cwd.
func TestSyncer_TempDirSideEffects(t *testing.T) {
	before, _ := os.ReadDir(".")
	st := openStore(t)
	_ = github.NewCache(st.DB)
	after, _ := os.ReadDir(".")
	if len(before) != len(after) {
		t.Errorf("test leaked files in cwd: %v vs %v", before, after)
	}
	_ = filepath.Base(".")
}
