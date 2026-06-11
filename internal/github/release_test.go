// release_test.go covers GetLatestRelease: parsing the tag from a 200, and the
// 304 conditional-request path.
package github_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	github "github.com/penguin/blog-server/internal/github"
)

func TestGetLatestRelease_Smoke(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/blog/releases/latest" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("ETag", `"abc"`)
		_, _ = w.Write([]byte(`{"tag_name":"v1.8.2","name":"Release v1.8.2"}`))
	}))
	defer srv.Close()

	c := github.NewClient(github.WithBaseURL(srv.URL))
	res, err := c.GetLatestRelease(context.Background(), "acme", "blog", "")
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if res.TagName != "v1.8.2" || res.ETag != `"abc"` {
		t.Fatalf("got %+v", res)
	}
}

func TestGetLatestRelease_NotModified(t *testing.T) {
	// 边界：携带 If-None-Match 命中 304,返回 NotModified 且不解析 body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != `"abc"` {
			t.Errorf("expected If-None-Match, got %q", r.Header.Get("If-None-Match"))
		}
		w.Header().Set("ETag", `"abc"`)
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	c := github.NewClient(github.WithBaseURL(srv.URL))
	res, err := c.GetLatestRelease(context.Background(), "acme", "blog", `"abc"`)
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if !res.NotModified || res.TagName != "" {
		t.Fatalf("want NotModified with empty tag, got %+v", res)
	}
}
