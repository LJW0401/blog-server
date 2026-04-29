package admin

import "testing"

// Smoke（正常路径）：home_style 接受空 / minimal / galaxy 三种值，均应通过校验。
// 空 = 默认（minimal），后台读取时会回落为简约风。
func TestValidateSettings_Smoke_HomeStyleAccepted(t *testing.T) {
	for _, v := range []string{"", "minimal", "galaxy"} {
		v := v
		t.Run("value="+v, func(t *testing.T) {
			err := validateSettings(map[string]string{
				"tagline":    "x",
				"home_style": v,
			})
			if err != nil {
				t.Errorf("home_style = %q 被拒：%v", v, err)
			}
		})
	}
}

// Edge（非法输入）：home_style 不在白名单内时必须拒绝，避免后台保存任意值
// 后前台拿到不认识的 style 还得做防御性兜底。
func TestValidateSettings_Edge_HomeStyleRejectsUnknown(t *testing.T) {
	cases := []string{"galaxyy", "MINIMAL", "neon", "<script>", "g a l a x y"}
	for _, v := range cases {
		v := v
		t.Run("value="+v, func(t *testing.T) {
			err := validateSettings(map[string]string{
				"tagline":    "x",
				"home_style": v,
			})
			if err == nil {
				t.Errorf("home_style = %q 被接受，但应拒绝", v)
			}
		})
	}
}
