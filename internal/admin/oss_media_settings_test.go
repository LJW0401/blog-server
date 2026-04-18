package admin

import (
	"testing"
)

// Smoke: valid GitHub + Gitee URLs pass validation.
func TestValidateSettings_Smoke_OSSURLsAccepted(t *testing.T) {
	err := validateSettings(map[string]string{
		"tagline":      "x",
		"media_github": "https://github.com/penguin",
		"media_gitee":  "https://gitee.com/penguin",
	})
	if err != nil {
		t.Fatalf("valid OSS URLs rejected: %v", err)
	}
}

// Edge (非法输入)：GitHub/Gitee 给了非 http(s) 开头的字符串必须拒绝，
// 防止 XSS（如 javascript:…）或误存相对路径。
func TestValidateSettings_Edge_OSSURLsRejectNonHTTP(t *testing.T) {
	cases := []struct {
		name, key, val string
	}{
		{"github javascript scheme", "media_github", "javascript:alert(1)"},
		{"github bare host", "media_github", "github.com/penguin"},
		{"gitee file scheme", "media_gitee", "file:///etc/passwd"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateSettings(map[string]string{
				"tagline": "x",
				c.key:     c.val,
			})
			if err == nil {
				t.Errorf("%s = %q accepted but should be rejected", c.key, c.val)
			}
		})
	}
}

// Edge (边界值)：空字符串合法——表示"未填写"，模板降级为纯文本。
func TestValidateSettings_Edge_OSSURLsAllowEmpty(t *testing.T) {
	err := validateSettings(map[string]string{
		"tagline":      "x",
		"media_github": "",
		"media_gitee":  "",
	})
	if err != nil {
		t.Errorf("empty OSS URLs should be allowed: %v", err)
	}
}
