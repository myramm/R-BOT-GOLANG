package watchhentai

import "testing"

const samplePageHTML = `<!doctype html>
<html><head><title>Watch Hentai Furachi Episode 1 Sub Indo &#8211; WatchHentai</title>
<meta property="og:image" content="https://watchhentai.net/wp-content/uploads/2026/01/furachi.jpg">
</head><body>
<h1>Watch Hentai Furachi Episode 1 Sub Indo</h1>
<div class="wp-content"><p>Sinopsis furachi episode 1.</p></div>
<iframe data-litespeed-src="https://watchhentai.net/jwplayer/?source=https%3A%2F%2Fhstorage.xyz%2Ffile%2Ffurachi-1.mp4&amp;other=1"></iframe>
</body></html>`

func TestExtractVideoURLFromJWPlayer(t *testing.T) {
	got := extractVideoURL(samplePageHTML)
	want := "https://hstorage.xyz/file/furachi-1.mp4"
	if got != want {
		t.Errorf("extractVideoURL() = %q, want %q", got, want)
	}
}

func TestExtractVideoURLFallbackDirectMP4(t *testing.T) {
	html := `<video src="https://cdn.example.com/video.mp4"></video>`
	got := extractVideoURL(html)
	want := "https://cdn.example.com/video.mp4"
	if got != want {
		t.Errorf("extractVideoURL() fallback = %q, want %q", got, want)
	}
}

func TestExtractVideoURLEmpty(t *testing.T) {
	if got := extractVideoURL("<html><body>kosong</body></html>"); got != "" {
		t.Errorf("extractVideoURL() = %q, want empty", got)
	}
}

func TestSlugFromURL(t *testing.T) {
	cases := map[string]string{
		"https://watchhentai.net/videos/furachi-episode-1-id-01/": "furachi-episode-1-id-01",
		"https://watchhentai.net/videos/furachi-episode-1-id-01":  "furachi-episode-1-id-01",
	}
	for in, want := range cases {
		if got := slugFromURL(in); got != want {
			t.Errorf("slugFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanText(t *testing.T) {
	got := cleanText("Watch Hentai Furachi Episode 1 &#8211; Sub Indo")
	want := "Furachi Episode 1 – Sub Indo"
	if got != want {
		t.Errorf("cleanText() = %q, want %q", got, want)
	}
}
