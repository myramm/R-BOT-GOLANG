package doodstream

import "testing"

func TestIsDoodURL(t *testing.T) {
	validURLs := []string{
		"https://doodstream.com/d/hF29zn",
		"https://dood.so/e/hF29zn",
		"https://ds2play.com/d/abc12345",
		"https://dood.la/e/test123",
	}

	for _, u := range validURLs {
		if !IsDoodURL(u) {
			t.Errorf("expected %s to be valid DoodURL", u)
		}
	}

	invalidURLs := []string{
		"https://youtube.com/watch?v=123",
		"https://instagram.com/p/123",
	}

	for _, u := range invalidURLs {
		if IsDoodURL(u) {
			t.Errorf("expected %s to be invalid DoodURL", u)
		}
	}
}

func TestExtractFileCode(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://doodstream.com/d/hF29zn", "hF29zn"},
		{"https://dood.so/e/abc123xyz", "abc123xyz"},
		{"https://dood.la/d/778899", "778899"},
	}

	for _, tt := range tests {
		got := ExtractFileCode(tt.url)
		if got != tt.want {
			t.Errorf("ExtractFileCode(%s) = %s, want %s", tt.url, got, tt.want)
		}
	}
}
