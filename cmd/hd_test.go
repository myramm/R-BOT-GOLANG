package cmd

import (
	"strings"
	"testing"
)

func TestHDLevelOnlySupportsFourK(t *testing.T) {
	for _, tc := range []struct {
		arg  string
		want int
		ok   bool
	}{
		{arg: "4k", want: 4, ok: true},
		{arg: "4K", want: 4, ok: true},
		{arg: "2k", want: 2, ok: false},
		{arg: "8k", want: 8, ok: false},
	} {
		got, ok := hdLevel(tc.arg)
		if got != tc.want || ok != tc.ok {
			t.Errorf("hdLevel(%q) = (%d, %v), want (%d, %v)", tc.arg, got, ok, tc.want, tc.ok)
		}
	}
}

func TestHDUsageDescribesImageAndVideoModes(t *testing.T) {
	usage := hdUsage()
	for _, want := range []string{"hd 4k", "Video otomatis", "iLoveIMG"} {
		if !strings.Contains(usage, want) {
			t.Errorf("hdUsage tidak memuat %q: %s", want, usage)
		}
	}
}
