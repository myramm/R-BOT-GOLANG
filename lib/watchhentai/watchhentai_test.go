package watchhentai

import (
	"strings"
	"testing"
)

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

const sampleDownloadHTML = `<div class="_4continuar"><p style="font-size:18px;">Select an option to start the download...</p><button onclick="window.location.href = 'https://xupload.org/download/K/kotowarenai-haha/kotowarenai-haha-1_1080p.mp4'"><i class="fas fa-download"></i> 1080p</button><button onclick="window.location.href = 'https://xupload.org/download/K/kotowarenai-haha/kotowarenai-haha-1_720p.mp4'"><i class="fas fa-download"></i> 720p</button><button onclick="window.location.href = 'https://xupload.org/download/K/kotowarenai-haha/kotowarenai-haha-1_480p.mp4'"><i class="fas fa-download"></i> 480p</button><center><button onclick="window.location.href = 'https://watchhentai.net/videos/kotowarenai-haha-episode-1-id-01/'"><i class="fas fa-play-circle"></i> WATCH ONLINE</button></center>`

func TestExtractDownloadOptions(t *testing.T) {
	opts := extractDownloadOptions(sampleDownloadHTML)
	if len(opts) != 3 {
		t.Fatalf("expected 3 download options (WATCH ONLINE diabaikan), got %d: %+v", len(opts), opts)
	}
	wantQ := []string{"1080p", "720p", "480p"}
	for i, w := range wantQ {
		if opts[i].Quality != w {
			t.Errorf("opts[%d].Quality = %q, want %q", i, opts[i].Quality, w)
		}
	}
	if !strings.HasSuffix(opts[0].URL, "kotowarenai-haha-1_1080p.mp4") {
		t.Errorf("opts[0].URL = %q, harus menunjuk file 1080p", opts[0].URL)
	}
}

func TestDownloadPageURL(t *testing.T) {
	cases := map[string]string{
		"kotowarenai-haha-episode-1-id-01":                          "https://watchhentai.net/download/kotowarenai-haha-episode-1-id-01/",
		"https://watchhentai.net/videos/kotowarenai-haha-episode-1-id-01/": "https://watchhentai.net/download/kotowarenai-haha-episode-1-id-01/",
		"https://watchhentai.net/download/kotowarenai-haha-episode-1-id-01/": "https://watchhentai.net/download/kotowarenai-haha-episode-1-id-01/",
	}
	for in, want := range cases {
		if got := DownloadPageURL(in); got != want {
			t.Errorf("DownloadPageURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTitleFromEpisodeURL(t *testing.T) {
	got := TitleFromEpisodeURL("https://watchhentai.net/videos/kotowarenai-haha-episode-1-id-01/")
	want := "Kotowarenai Haha Episode 1"
	if got != want {
		t.Errorf("TitleFromEpisodeURL() = %q, want %q", got, want)
	}
}

func TestCleanText(t *testing.T) {
	got := cleanText("Watch Hentai Furachi Episode 1 &#8211; Sub Indo")
	want := "Furachi Episode 1 – Sub Indo"
	if got != want {
		t.Errorf("cleanText() = %q, want %q", got, want)
	}
}
