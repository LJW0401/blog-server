package stats_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/penguin/blog-server/internal/stats"
	"github.com/penguin/blog-server/internal/storage"
)

func newStore(t *testing.T) *stats.Store {
	t.Helper()
	st, err := storage.Open(t.TempDir())
	if err != nil && !errors.Is(err, storage.ErrCorruptDB) {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return stats.New(st.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// --- Smoke (WI-6.3) --------------------------------------------------------

func TestRecordRead_Smoke_FirstVisitCounts(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	s.RecordRead(ctx, "doc-a", "1.2.3.4", "Chrome/1.0")
	if n := s.Count(ctx, "doc-a"); n != 1 {
		t.Errorf("got %d", n)
	}
}

func TestRecordRead_Smoke_DedupWithinWindow(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		s.RecordRead(ctx, "doc-a", "1.2.3.4", "Chrome/1.0")
	}
	if n := s.Count(ctx, "doc-a"); n != 1 {
		t.Errorf("got %d, want 1 (dedup)", n)
	}
}

// --- Edge (WI-6.4) --------------------------------------------------------

func TestRecordRead_Edge_DifferentUASameIP(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	s.RecordRead(ctx, "x", "10.0.0.1", "UA-A")
	s.RecordRead(ctx, "x", "10.0.0.1", "UA-B")
	if n := s.Count(ctx, "x"); n != 2 {
		t.Errorf("different UA same IP should count as 2, got %d", n)
	}
}

func TestRecordRead_Edge_DifferentSlugsIndependent(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	s.RecordRead(ctx, "a", "1.1.1.1", "X")
	s.RecordRead(ctx, "b", "1.1.1.1", "X")
	if n := s.Count(ctx, "a"); n != 1 {
		t.Errorf("a=%d", n)
	}
	if n := s.Count(ctx, "b"); n != 1 {
		t.Errorf("b=%d", n)
	}
}

func TestRecordRead_Edge_CrawlerExcluded(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	for _, ua := range []string{
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
		"Mozilla/5.0 (compatible; bingbot/2.0)",
		"Mozilla/5.0 (compatible; DuckDuckBot-Https/1.1)",
		"Baiduspider+(+http://www.baidu.com/search/spider.htm)",
	} {
		s.RecordRead(ctx, "doc-a", "7.7.7.7", ua)
	}
	if n := s.Count(ctx, "doc-a"); n != 0 {
		t.Errorf("crawlers should not count, got %d", n)
	}
}

func TestRecordRead_Edge_EmptyUAStillCounts(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	s.RecordRead(ctx, "x", "9.9.9.9", "")
	if n := s.Count(ctx, "x"); n != 1 {
		t.Errorf("empty UA should still count, got %d", n)
	}
}

func TestRecordRead_Edge_EmptySlugSkipped(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	s.RecordRead(ctx, "", "1.1.1.1", "UA")
	if n := s.Count(ctx, ""); n != 0 {
		t.Errorf("empty slug should not count")
	}
}

func TestCounts_Smoke_BatchLookup(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	s.RecordRead(ctx, "a", "1", "UA")
	s.RecordRead(ctx, "b", "2", "UA")
	m := s.Counts(ctx, []string{"a", "b", "c"})
	if m["a"] != 1 || m["b"] != 1 || m["c"] != 0 {
		t.Errorf("got %+v", m)
	}
}

func TestIsCrawler_Cases(t *testing.T) {
	cases := map[string]bool{
		"Mozilla/5.0 (compatible; Googlebot/2.1)":   true,
		"Mozilla/5.0 (compatible; bingbot/2.0)":     true,
		"Mozilla/5.0 AppleWebKit/537.36 Chrome/120": false,
		"curl/7.88.1":             false,
		"":                        false,
		"facebookexternalhit/1.1": true,
		"Twitterbot/1.0":          true,
	}
	for ua, want := range cases {
		got := stats.IsCrawler(ua)
		if got != want {
			t.Errorf("IsCrawler(%q) = %v, want %v", ua, got, want)
		}
	}
}
