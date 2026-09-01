package minioppai

import (
	"encoding/base64"
	"testing"
)

const fixtureSearchHTML = `<!DOCTYPE html><html><body>
<div class="main">
<a href="https://minioppai.org/anime/hajimete-no-hitozuma/" title="Hajimete no Hitozuma"><img src="https://minioppai.org/wp-content/uploads/h.jpg"></a>
<a href="https://minioppai.org/anime/kotowarenai/" title="Kotowarenai"><img src="https://minioppai.org/wp-content/uploads/k.jpg"></a>
<a href="https://minioppai.org/anime/no-title/">Sidebar link tanpa judul</a>
</div>
</body></html>`

func TestSearch(t *testing.T) {
	results, err := searchFromHTML(fixtureSearchHTML)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
	if results[0].Title != "Hajimete no Hitozuma" || results[0].Source != Provider {
		t.Errorf("bad first result: %+v", results[0])
	}
	if results[1].URL != "https://minioppai.org/anime/kotowarenai/" {
		t.Errorf("bad second url: %s", results[1].URL)
	}
}

func TestSearchEmptyURLPrefixedOnly(t *testing.T) {
	// Link yang bukan /anime/ harus diabaikan.
	html := `<a href="https://minioppai.org/genre/ecchi/">genre</a><a href="https://minioppai.org/anime/one/" title="One">x</a>`
	results, err := searchFromHTML(html)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Title != "One" {
		t.Fatalf("got %+v", results)
	}
}

func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func TestParseStreamOptions(t *testing.T) {
	html := `<select name="mirror">
<option value=""><option placeholder></option>
<option value="` + b64(`<iframe width="100%" height="100%" src="//streampai.my.id/v/XXX" frameborder="0"></iframe>`) + `">480p</option>
<option value="` + b64(`<iframe src="https://tv.streampai.my.id/v/ZZZ"></iframe>`) + `">1080p</option>
<option value="Pilih Server Video">Pilih Server Video</option>
</select>
<iframe width="100%" height="100%" src="//streampai.my.id/v/MAIN"></iframe>`
	opts := parseStreamOptions(html)
	if len(opts) != 2 {
		t.Fatalf("expected 2 opts, got %+v", opts)
	}
	if opts[0].Quality != "480P" || opts[0].URL != "https://streampai.my.id/v/XXX" {
		t.Errorf("bad 480p opt: %+v", opts[0])
	}
	if opts[1].Quality != "1080P" || opts[1].URL != "https://tv.streampai.my.id/v/ZZZ" {
		t.Errorf("bad 1080p opt: %+v", opts[1])
	}
}

func TestParseStreamOptionsFallback(t *testing.T) {
	// Tanpa dropdown mirror -> pakai iframe utama.
	html := `<iframe src="//streampai.my.id/v/MAIN"></iframe>`
	opts := parseStreamOptions(html)
	if len(opts) != 1 || opts[0].URL != "https://streampai.my.id/v/MAIN" {
		t.Fatalf("got %+v", opts)
	}
}

func TestExtractEpisodeNumber(t *testing.T) {
	cases := map[string]string{
		"hajimete-no-hitozuma-episode-6": "6",
		"seri-ep-12":                      "12",
		"tanpa-angka":                     "",
	}
	for in, want := range cases {
		if got := extractEpisodeNumber(in); got != want {
			t.Errorf("extractEpisodeNumber(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeQuality(t *testing.T) {
	got := normalizeQuality("480p")
	if got != "480P" {
		t.Errorf("normalize 480p = %q", got)
	}
	if got := normalizeQuality("Pilih Server Video"); got != "" {
		t.Errorf("empty label should be '', got %q", got)
	}
}

func TestChapterLess(t *testing.T) {
	if chapterLess("2", "10") == false {
		t.Error("2 should sort before 10 (numeric), got false")
	}
	if chapterLess("10", "2") != false {
		t.Error("10 should not sort before 2")
	}
}
