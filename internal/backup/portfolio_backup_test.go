package backup_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// WI-1.12 Smoke: backup archive includes content/portfolio/*.md files.
func TestRunNow_Smoke_IncludesPortfolio(t *testing.T) {
	s, dir := setup(t)
	pdir := filepath.Join(dir, "content", "portfolio")
	if err := os.MkdirAll(pdir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdir, "first.md"),
		[]byte("---\ntitle: First\nslug: first\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdir, "second.md"),
		[]byte("---\ntitle: Second\nslug: second\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.RunNow(context.Background()); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "backups"))
	if len(entries) != 1 {
		t.Fatalf("want 1 backup, got %d", len(entries))
	}
	members := unpackNames(t, filepath.Join(dir, "backups", entries[0].Name()))
	need := []string{"content/portfolio/first.md", "content/portfolio/second.md"}
	for _, n := range need {
		found := false
		for _, m := range members {
			if m == n {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("archive missing %s; members=%v", n, members)
		}
	}
}

// WI-1.13 Exception: portfolio dir missing is tolerated; archive still succeeds.
func TestRunNow_Exception_PortfolioDirMissing(t *testing.T) {
	s, _ := setup(t)
	// Intentionally do NOT create content/portfolio/
	if err := s.RunNow(context.Background()); err != nil {
		t.Fatalf("RunNow should tolerate missing portfolio dir: %v", err)
	}
}

// WI-1.13 Exception: empty portfolio dir still works; archive has no portfolio entries.
func TestRunNow_Exception_EmptyPortfolioDir(t *testing.T) {
	s, dir := setup(t)
	if err := os.MkdirAll(filepath.Join(dir, "content", "portfolio"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.RunNow(context.Background()); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "backups"))
	if len(entries) != 1 {
		t.Fatalf("want 1 backup, got %d", len(entries))
	}
	members := unpackNames(t, filepath.Join(dir, "backups", entries[0].Name()))
	for _, m := range members {
		if strings.HasPrefix(m, "content/portfolio/") && !strings.HasSuffix(m, "/") {
			t.Errorf("empty portfolio dir should not produce file members; got %s", m)
		}
	}
}
