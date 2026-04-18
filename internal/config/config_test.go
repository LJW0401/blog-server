package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/penguin/blog-server/internal/config"
)

const validYAML = `
listen_addr: "127.0.0.1:8080"
admin_username: "admin"
admin_password_bcrypt: "$2a$10$7EqJtq98hPqEX7fNZaFWoO5p5Aj4n5qRQnhC2kLfq8VQ0SyO9Vdke"
password_changed_at: null
data_dir: "."
github_token: ""
github_sync_interval_min: 30
`

func writeFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// Smoke: loads the example config and asserts expected fields.
func TestLoad_WhenValidExample_ThenParsesAllFields(t *testing.T) {
	path := writeFile(t, validYAML)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:8080" {
		t.Errorf("listen_addr: got %q", cfg.ListenAddr)
	}
	if cfg.AdminUsername != "admin" {
		t.Errorf("admin_username: got %q", cfg.AdminUsername)
	}
	if cfg.GitHubSyncIntervalMin != 30 {
		t.Errorf("interval: got %d", cfg.GitHubSyncIntervalMin)
	}
	if !cfg.DefaultPasswordUnchanged() {
		t.Error("default password should be unchanged when password_changed_at is nil")
	}
}

// Boundary: password_changed_at set to a valid time makes banner disappear.
func TestLoad_WhenPasswordChangedAtSet_ThenBannerGone(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	yaml := validYAML + "\npassword_changed_at: \"" + now + "\"\n"
	// Remove the earlier null line by replacing; easier: write fresh
	content := `
listen_addr: "127.0.0.1:8080"
admin_username: "admin"
admin_password_bcrypt: "$2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123"
password_changed_at: "` + now + `"
data_dir: "."
github_sync_interval_min: 30
`
	_ = yaml
	path := writeFile(t, content)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultPasswordUnchanged() {
		t.Error("banner should be gone when password_changed_at has a value")
	}
}

// --- Edge cases (WI-1.4) ----------------------------------------------------

func TestLoad_Edge_FileNotFound(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if !errors.Is(err, config.ErrReadFile) {
		t.Errorf("expected ErrReadFile, got %v", err)
	}
}

func TestLoad_Edge_InvalidYAMLSyntax(t *testing.T) {
	path := writeFile(t, "listen_addr: [this is: broken]\n  admin_username: x\n")
	_, err := config.Load(path)
	if !errors.Is(err, config.ErrParseYAML) {
		t.Errorf("expected ErrParseYAML, got %v", err)
	}
}

func TestLoad_Edge_MissingRequiredField(t *testing.T) {
	cases := map[string]string{
		"missing listen_addr":    "admin_username: x\nadmin_password_bcrypt: $2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123\ndata_dir: .\n",
		"missing admin_username": "listen_addr: 127.0.0.1:8080\nadmin_password_bcrypt: $2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123\ndata_dir: .\n",
		"missing password":       "listen_addr: 127.0.0.1:8080\nadmin_username: x\ndata_dir: .\n",
		"missing data_dir":       "listen_addr: 127.0.0.1:8080\nadmin_username: x\nadmin_password_bcrypt: $2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeFile(t, body)
			_, err := config.Load(path)
			if !errors.Is(err, config.ErrFieldRequired) {
				t.Errorf("expected ErrFieldRequired, got %v", err)
			}
		})
	}
}

func TestLoad_Edge_InvalidField(t *testing.T) {
	cases := map[string]string{
		"bad port":      "listen_addr: 127.0.0.1:abc\nadmin_username: x\nadmin_password_bcrypt: $2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123\ndata_dir: .\n",
		"port zero":     "listen_addr: 127.0.0.1:0\nadmin_username: x\nadmin_password_bcrypt: $2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123\ndata_dir: .\n",
		"bad bcrypt":    "listen_addr: 127.0.0.1:8080\nadmin_username: x\nadmin_password_bcrypt: notahash\ndata_dir: .\n",
		"bad interval":  "listen_addr: 127.0.0.1:8080\nadmin_username: x\nadmin_password_bcrypt: $2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123\ndata_dir: .\ngithub_sync_interval_min: 0\n",
		"missing colon": "listen_addr: 127.0.0.1\nadmin_username: x\nadmin_password_bcrypt: $2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123\ndata_dir: .\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeFile(t, body)
			_, err := config.Load(path)
			if !errors.Is(err, config.ErrFieldInvalid) {
				t.Errorf("expected ErrFieldInvalid, got %v", err)
			}
		})
	}
}

func TestLoad_Edge_UnknownField(t *testing.T) {
	body := "listen_addr: 127.0.0.1:8080\nadmin_username: x\nadmin_password_bcrypt: $2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123\ndata_dir: .\nmystery_field: true\n"
	path := writeFile(t, body)
	_, err := config.Load(path)
	if !errors.Is(err, config.ErrParseYAML) {
		t.Errorf("unknown field should fail parse, got %v", err)
	}
}

func TestLoad_Edge_UnreadablePermission(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, chmod won't restrict reads")
	}
	path := writeFile(t, validYAML)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	_, err := config.Load(path)
	if !errors.Is(err, config.ErrReadFile) {
		t.Errorf("expected ErrReadFile, got %v", err)
	}
}
