package deploy_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// managePath locates deploy/manage.sh relative to this test file.
func managePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "manage.sh")
}

// runManage executes manage.sh with args in a fresh INSTALL_DIR and returns
// stdout/stderr combined + exit error. Root/systemctl are bypassed via env.
func runManage(t *testing.T, installDir string, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{managePath(t)}, args...)...)
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"MANAGE_SKIP_ROOT=1",
		"MANAGE_SKIP_SYSTEMCTL=1",
		"INSTALL_DIR=" + installDir,
		"SERVICE_USER=nobody",
	}
	env = append(env, extraEnv...)
	cmd.Env = env
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// seedInstallDir creates a realistic INSTALL_DIR layout for export tests.
func seedInstallDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	for _, sub := range []string{"content/docs", "content/projects", "images"} {
		if err := os.MkdirAll(filepath.Join(d, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writes := map[string]string{
		"content/docs/hello.md":  "---\ntitle: Hello\n---\nbody\n",
		"content/projects/p1.md": "# project 1\n",
		"images/a.png":           "\x89PNG fakebinary",
		"data.sqlite":            "SQLite format 3\x00 fake",
		"config.yaml":            "admin_password_bcrypt: \"$2a$10$fakehash\"\n",
	}
	for p, body := range writes {
		full := filepath.Join(d, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// listTar returns the list of entries in a tar.gz via `tar tzf`.
func listTar(t *testing.T, path string) []string {
	t.Helper()
	out, err := exec.Command("tar", "tzf", path).Output()
	if err != nil {
		t.Fatalf("tar tzf %s: %v", path, err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func contains(xs []string, sub string) bool {
	for _, x := range xs {
		if strings.Contains(x, sub) {
			return true
		}
	}
	return false
}

// Smoke: export → 产出 tar.gz，包含 MANIFEST + 所有预期文件 + sha256。
func TestManage_Smoke_ExportBundlesData(t *testing.T) {
	src := seedInstallDir(t)
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if logs, err := runManage(t, src, nil, "export", out); err != nil {
		t.Fatalf("export failed: %v\n%s", err, logs)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output tarball missing: %v", err)
	}
	entries := listTar(t, out)
	for _, want := range []string{
		"blog-server-export/MANIFEST",
		"blog-server-export/data.sqlite",
		"blog-server-export/config.yaml",
		"blog-server-export/content/docs/hello.md",
		"blog-server-export/content/projects/p1.md",
		"blog-server-export/images/a.png",
	} {
		if !contains(entries, want) {
			t.Errorf("bundle missing entry %q; got %v", want, entries)
		}
	}
	if _, err := os.Stat(out + ".sha256"); err != nil {
		t.Errorf("sha256 sidecar not generated: %v", err)
	}
}

// Smoke: export → import 在另一台"机器"（另一个空 INSTALL_DIR）上完整恢复。
func TestManage_Smoke_RoundtripRestoresAllFiles(t *testing.T) {
	src := seedInstallDir(t)
	bundleDir := t.TempDir()
	out := filepath.Join(bundleDir, "bundle.tar.gz")
	if logs, err := runManage(t, src, nil, "export", out); err != nil {
		t.Fatalf("export: %v\n%s", err, logs)
	}

	// Second "machine"
	dst := t.TempDir()
	if logs, err := runManage(t, dst, nil, "import", out); err != nil {
		t.Fatalf("import: %v\n%s", err, logs)
	}

	for relpath, wantSubstr := range map[string]string{
		"content/docs/hello.md":  "title: Hello",
		"content/projects/p1.md": "project 1",
		"images/a.png":           "PNG",
		"data.sqlite":            "SQLite format 3",
		"config.yaml":            "fakehash",
	} {
		b, err := os.ReadFile(filepath.Join(dst, relpath))
		if err != nil {
			t.Errorf("restored file missing: %s: %v", relpath, err)
			continue
		}
		if !strings.Contains(string(b), wantSubstr) {
			t.Errorf("%s content mismatch; expected to contain %q, got %q", relpath, wantSubstr, string(b))
		}
	}
}

// Edge（权限/认证 + 边界值）：--no-config 排除 config.yaml。
func TestManage_Edge_ExportNoConfigExcludesConfig(t *testing.T) {
	src := seedInstallDir(t)
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if logs, err := runManage(t, src, nil, "export", "--no-config", out); err != nil {
		t.Fatalf("export: %v\n%s", err, logs)
	}
	entries := listTar(t, out)
	if contains(entries, "config.yaml") {
		t.Errorf("--no-config should exclude config.yaml; got %v", entries)
	}
	if !contains(entries, "blog-server-export/data.sqlite") {
		t.Errorf("--no-config should still include data.sqlite")
	}
}

// Edge（非法输入）：import 一个不是 manage.sh 产出的 tar.gz → 拒绝，不污染目标目录。
func TestManage_Edge_ImportRejectsTarballWithoutManifest(t *testing.T) {
	dst := t.TempDir()
	// 建一个假 tar.gz：只有一个随机文件，没 MANIFEST 没 blog-server-export/ 顶层
	fakeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeDir, "evil.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(t.TempDir(), "fake.tar.gz")
	if out, err := exec.Command("tar", "czf", fake, "-C", fakeDir, "evil.sh").CombinedOutput(); err != nil {
		t.Fatalf("tar pack: %v\n%s", err, out)
	}
	logs, err := runManage(t, dst, nil, "import", fake)
	if err == nil {
		t.Fatalf("expected import to reject non-manage tarball; logs: %s", logs)
	}
	if _, statErr := os.Stat(filepath.Join(dst, "evil.sh")); statErr == nil {
		t.Errorf("rejected tarball nonetheless leaked evil.sh into INSTALL_DIR")
	}
}

// Edge（边界值）：import 到非空目录 → 先把现有数据备份到 /tmp/blog-server-preimport-*.tar.gz。
func TestManage_Edge_ImportPreBackupsExistingData(t *testing.T) {
	src := seedInstallDir(t)
	bundle := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if logs, err := runManage(t, src, nil, "export", bundle); err != nil {
		t.Fatalf("export: %v\n%s", err, logs)
	}

	// 目标目录已有旧内容，值不同于 bundle 里的
	dst := seedInstallDir(t)
	preExistingSignature := "THIS_IS_ORIGINAL_CONTENT"
	if err := os.WriteFile(filepath.Join(dst, "content/docs/marker.md"), []byte(preExistingSignature), 0o644); err != nil {
		t.Fatal(err)
	}

	if logs, err := runManage(t, dst, nil, "import", bundle); err != nil {
		t.Fatalf("import: %v\n%s", err, logs)
	}

	// import 后，目标目录的 marker.md 应该没了（被 bundle 覆盖）
	if _, err := os.Stat(filepath.Join(dst, "content/docs/marker.md")); err == nil {
		t.Errorf("marker.md from pre-import state leaked through; content/ was not replaced")
	}

	// 预备份归档放在 $INSTALL_DIR/tmp/ 下，而不是系统 /tmp。
	archives, _ := filepath.Glob(filepath.Join(dst, "tmp", "blog-server-preimport-*.tar.gz"))
	if len(archives) == 0 {
		t.Errorf("no preimport backup created under %s/tmp/", dst)
	}
}

// Edge（失败依赖）：INSTALL_DIR 不存在 → export 明确报错，不静默产出空包。
func TestManage_Edge_ExportMissingInstallDirErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	logs, err := runManage(t, missing, nil, "export", out)
	if err == nil {
		t.Fatalf("expected error for missing INSTALL_DIR; logs: %s", logs)
	}
	if !strings.Contains(logs, "INSTALL_DIR") {
		t.Errorf("error message should reference INSTALL_DIR; got: %s", logs)
	}
}
