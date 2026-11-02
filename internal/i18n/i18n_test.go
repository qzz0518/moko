package i18n

import "testing"

// A Mac set to Chinese in Japan once showed English: the app asked MyGo for its
// locale, which only picks among the languages it was told the app has.
func TestSetFollowsThePreferredLanguages(t *testing.T) {
	for _, c := range []struct {
		setting   string
		preferred []string
		want      string
	}{
		{"", []string{"zh-Hans-JP", "ja-JP"}, "zh"},
		{"", []string{"ja-JP", "zh-Hans-JP"}, "ja"},
		{"", []string{"fr-FR", "ja-JP"}, "ja"},
		{"", []string{"fr-FR"}, "en"},
		{"", nil, "en"},
		{"en", []string{"zh-Hans-JP"}, "en"},
	} {
		Set(c.setting, c.preferred...)
		if got := Lang(); got != c.want {
			t.Errorf("Set(%q, %v): %s, want %s", c.setting, c.preferred, got, c.want)
		}
	}
}
