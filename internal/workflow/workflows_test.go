package workflow_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readWorkflow(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", ".github", "workflows", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read workflow %s: %v", name, err)
	}
	return string(b)
}

func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read repository file %s: %v", name, err)
	}
	return string(b)
}

// Smoke: CI runs the repository-owned quality gate for PRs and main pushes.
func TestCIWorkflow_Smoke_UsesProjectGate(t *testing.T) {
	workflow := readWorkflow(t, "ci.yml")
	for _, want := range []string{
		"pull_request:",
		"branches: [main]",
		"go-version-file: go.mod",
		"golangci-lint@v2.12.2",
		"govulncheck@v1.6.0",
		"run: actionlint",
		"--new-from-rev=${{ github.event.pull_request.base.sha || github.event.before }}",
		"run: make check",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("CI workflow missing %q", want)
		}
	}
}

// Boundary: releases must cover every Linux architecture supported by deploy/manage.sh.
func TestReleaseWorkflow_Boundary_CoversSupportedArchitectures(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")
	for _, want := range []string{
		"tags:",
		`- "v*"`,
		"contents: write",
		"goarch: [amd64, arm64]",
		"VERIFY_BUILD=0",
		"if: matrix.goarch == 'amd64'",
		"run: cp deploy/manage.sh manage.sh",
		"manage.sh",
		`ARCHIVE="blog-server-linux-${{ matrix.goarch }}.tar.gz"`,
		"blog-server-linux-${{ matrix.goarch }}.tar.gz.sha256",
		"body_path: release.md",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("release workflow missing %q", want)
		}
	}
}

// Smoke: automated GitHub Releases use complete, user-facing release notes.
func TestReleaseNotes_Smoke_ContainsRequiredSections(t *testing.T) {
	notes := readRepoFile(t, "release.md")
	for _, want := range []string{"## 安装与升级", "## 本次更新", "## 发布附件", "manage.sh", "blog-server-linux-arm64.tar.gz"} {
		if !strings.Contains(notes, want) {
			t.Errorf("release notes missing %q", want)
		}
	}
}

// Boundary: automatic notes must not publish unresolved template placeholders.
func TestReleaseNotes_Boundary_HasNoTemplatePlaceholders(t *testing.T) {
	notes := readRepoFile(t, "release.md")
	for _, placeholder := range []string{"<VERSION>", "<PREV_VERSION>", "<功能标题", "<要点"} {
		if strings.Contains(notes, placeholder) {
			t.Errorf("release notes still contain template placeholder %q", placeholder)
		}
	}
}
