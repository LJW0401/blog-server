// version_test.go covers semver parsing/comparison, especially the boundary
// and malformed inputs that decide whether an update banner shows.
package update

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
		note            string
	}{
		{"v1.9.0", "v1.8.2", true, "minor bump"},
		{"v2.0.0", "v1.9.9", true, "major bump"},
		{"v1.8.3", "v1.8.2", true, "patch bump"},
		{"v1.8.2", "v1.8.2", false, "equal"},
		{"v1.8.1", "v1.8.2", false, "older latest"},
		{"1.9.0", "1.8.2", true, "no v prefix"},
		// 边界：当前是开发构建（pseudo-version），基线相同则不应提示更新
		{"v1.8.2", "v1.8.2-0.20260506164655-9b2ae1b4d2e3+dirty", false, "dev build of same release"},
		{"v1.9.0", "v1.8.2-0.20260506164655-9b2ae1b4d2e3+dirty", true, "newer than dev build"},
		// 非法输入：无法解析的版本一律判否（fail closed，开发构建不误报）
		{"v1.9.0", "dev", false, "current unparseable"},
		{"latest", "v1.8.2", false, "latest unparseable"},
		{"v1.8", "v1.8.2", false, "latest missing patch"},
		{"", "v1.8.2", false, "empty latest"},
		{"vX.Y.Z", "v1.8.2", false, "non-numeric"},
	}
	for _, c := range cases {
		if got := isNewer(c.latest, c.current); got != c.want {
			t.Errorf("isNewer(%q,%q)=%v want %v (%s)", c.latest, c.current, got, c.want, c.note)
		}
	}
}

func TestParseSemverNegative(t *testing.T) {
	// 边界：负数分量必须判非法，避免 "v1.-1.0" 之类绕过比较
	if _, ok := parseSemver("v1.-1.0"); ok {
		t.Error("negative component should not parse")
	}
}
