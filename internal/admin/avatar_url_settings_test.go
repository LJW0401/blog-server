package admin

import "testing"

// Smoke：avatar_url 支持 http(s) 绝对 URL 和 / 开头的站内相对路径，都应通过校验。
func TestValidateSettings_Smoke_AvatarAccepted(t *testing.T) {
	cases := []struct {
		name, val string
	}{
		{"absolute https", "https://cdn.example.com/me.png"},
		{"absolute http", "http://example.com/me.png"},
		{"local images path", "/images/avatar.png"},
		{"empty means unset", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateSettings(map[string]string{
				"tagline":    "x",
				"avatar_url": c.val,
			})
			if err != nil {
				t.Errorf("avatar_url = %q 被拒：%v", c.val, err)
			}
		})
	}
}

// Edge（非法输入）：既不是 http(s) 也不是 / 开头的字符串应被拒，防止落到
// <img src> 时产生 javascript: 之类的伪协议或相对路径歧义。
func TestValidateSettings_Edge_AvatarRejectsBadSchemes(t *testing.T) {
	cases := []struct {
		name, val string
	}{
		{"javascript scheme", "javascript:alert(1)"},
		{"data uri", "data:image/png;base64,AAAA"},
		{"bare host", "example.com/me.png"},
		{"file scheme", "file:///etc/passwd"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateSettings(map[string]string{
				"tagline":    "x",
				"avatar_url": c.val,
			})
			if err == nil {
				t.Errorf("avatar_url = %q 被接受，但应拒绝", c.val)
			}
		})
	}
}
