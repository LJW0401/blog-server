// Package config loads and validates the application's static configuration
// (config.yaml). Runtime-editable data such as site settings is handled by
// internal/storage.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the full static configuration loaded from config.yaml.
type Config struct {
	ListenAddr            string     `yaml:"listen_addr"`
	AdminUsername         string     `yaml:"admin_username"`
	AdminPasswordBcrypt   string     `yaml:"admin_password_bcrypt"`
	PasswordChangedAt     *time.Time `yaml:"password_changed_at"`
	DataDir               string     `yaml:"data_dir"`
	GitHubToken           string     `yaml:"github_token"`
	GitHubSyncIntervalMin int        `yaml:"github_sync_interval_min"`
}

// Error types returned by Load. Tests rely on these being comparable via errors.Is.
var (
	ErrReadFile      = errors.New("config: read file")
	ErrParseYAML     = errors.New("config: parse yaml")
	ErrFieldRequired = errors.New("config: required field missing")
	ErrFieldInvalid  = errors.New("config: invalid field")
)

// Load reads and validates config from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrReadFile, path, err)
	}

	// Enforce strict decoding: reject unknown fields so typos surface early.
	cfg := &Config{
		GitHubSyncIntervalMin: 30, // default before unmarshal
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrParseYAML, path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if strings.TrimSpace(c.ListenAddr) == "" {
		return fmt.Errorf("%w: listen_addr", ErrFieldRequired)
	}
	if _, port, err := net.SplitHostPort(c.ListenAddr); err != nil {
		return fmt.Errorf("%w: listen_addr %q: %v", ErrFieldInvalid, c.ListenAddr, err)
	} else if p, perr := strconv.Atoi(port); perr != nil || p < 1 || p > 65535 {
		return fmt.Errorf("%w: listen_addr port %q", ErrFieldInvalid, port)
	}
	if strings.TrimSpace(c.AdminUsername) == "" {
		return fmt.Errorf("%w: admin_username", ErrFieldRequired)
	}
	if strings.TrimSpace(c.AdminPasswordBcrypt) == "" {
		return fmt.Errorf("%w: admin_password_bcrypt", ErrFieldRequired)
	}
	// Accept either $2a$ / $2b$ / $2y$ bcrypt prefixes.
	if !strings.HasPrefix(c.AdminPasswordBcrypt, "$2a$") &&
		!strings.HasPrefix(c.AdminPasswordBcrypt, "$2b$") &&
		!strings.HasPrefix(c.AdminPasswordBcrypt, "$2y$") {
		return fmt.Errorf("%w: admin_password_bcrypt does not look like a bcrypt hash", ErrFieldInvalid)
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return fmt.Errorf("%w: data_dir", ErrFieldRequired)
	}
	if c.GitHubSyncIntervalMin < 1 {
		return fmt.Errorf("%w: github_sync_interval_min must be >= 1", ErrFieldInvalid)
	}
	return nil
}

// DefaultPasswordUnchanged reports whether the default password has not yet
// been modified via the admin panel (i.e. the banner should render).
func (c *Config) DefaultPasswordUnchanged() bool {
	return c.PasswordChangedAt == nil
}
