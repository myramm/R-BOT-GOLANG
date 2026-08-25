package cmd

import (
	"strings"
	"testing"
)

func TestInfoHentai(t *testing.T) {
	got := infoHentai()
	for _, want := range []string{"100", "300", "2x", "5x", "480p", ".premium"} {
		if !strings.Contains(got, want) {
			t.Errorf("infoHentai() harus mengandung %q\ndapat:\n%s", want, got)
		}
	}
}
