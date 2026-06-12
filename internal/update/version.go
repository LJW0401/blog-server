// Package update implements version-check support: a periodic checker that
// polls the project's latest GitHub release and compares it against the running
// version, surfacing "a newer release is available" to the admin UI. version.go
// holds a dependency-free semantic-version comparison used to decide whether
// upstream is newer.
package update

import (
	"strconv"
	"strings"
)

// semver is the parsed MAJOR.MINOR.PATCH triple. Pre-release and build metadata
// (everything after the first '-' or '+') is ignored — release tags compared
// here are always clean vX.Y.Z, and ignoring the suffix means a dev build of
// vX.Y.Z (e.g. "v1.8.2-0.2026...") does not register as older than its own
// release.
type semver struct {
	major, minor, patch int
}

// parseSemver parses "vX.Y.Z" (the leading 'v' optional). It returns ok=false
// for anything that isn't three numeric components, e.g. "dev" or a bare commit
// hash — callers treat unparseable current versions as "unknown, no update".
func parseSemver(s string) (semver, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	// Cut pre-release / build metadata.
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, false
		}
		nums[i] = n
	}
	return semver{nums[0], nums[1], nums[2]}, true
}

// isNewer reports whether latest is a strictly higher release than current.
// If either side fails to parse as a clean release, it returns false — we never
// claim an update is available on uncertain version data (fail closed, no false
// "please update" prompts on dev builds).
func isNewer(latest, current string) bool {
	l, lok := parseSemver(latest)
	c, cok := parseSemver(current)
	if !lok || !cok {
		return false
	}
	switch {
	case l.major != c.major:
		return l.major > c.major
	case l.minor != c.minor:
		return l.minor > c.minor
	default:
		return l.patch > c.patch
	}
}
