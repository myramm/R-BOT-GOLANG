package cmd

import "testing"

func TestIsNhentaiCode(t *testing.T) {
	valid := []string{"177013", "1", "987560"}
	invalid := []string{"", "haha", "solo leveling", "solo leveling 5", "12a", "177013 ", "-5"}
	for _, s := range valid {
		if !isNhentaiCode(s) {
			t.Errorf("isNhentaiCode(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if isNhentaiCode(s) {
			t.Errorf("isNhentaiCode(%q) = true, want false", s)
		}
	}
}
