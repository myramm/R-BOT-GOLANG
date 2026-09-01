package minioppai

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
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

// fixtureDownloadSection meniru struktur bagian "Download" episode minioppai
// (dikonfirmasi dari minioppai.org/hajimete-no-hitozuma-episode-6/).
const fixtureDownloadSection = `<!DOCTYPE html><html><head><title>Hajimete no Hitozuma Episode 6 – MiniOppai</title></head><body>
<div class="entry-content">
<p>Download <b>Hajimete no Hitozuma Episode 6</b> ...</p>
<div class="bixbox"><div class="releases"><h3>Download Hajimete no Hitozuma Episode 6</h3></div><div class="mctnx"><div class="soraddlx soradlg"><div class="sorattlx"><h3>Hajimete no Hitozuma Episode 6</h3></div><div class="soraurlx"><strong>1080p</strong>
<a href="https://www.mediafire.com/file/mcn6y30mm5uvdcz" target="_blank" rel="nofollow noopener noreferrer">MediaFire</a>
<a href="https://krakenfiles.com/view/6hBvcPsUZs/file.html" target="_blank" rel="nofollow noopener noreferrer">KrakenFiles</a>
<a href="https://pixeldrain.com/u/pyR23pcW" target="_blank" rel="nofollow noopener noreferrer">PixelDrain</a></div><div class="soraurlx"><strong>720p</strong>
<a href="https://www.mediafire.com/file/9xhuvxo7so7m834" target="_blank" rel="nofollow noopener noreferrer">MediaFire</a>
<a href="https://krakenfiles.com/view/tn2oblYYxg/file.html" target="_blank" rel="nofollow noopener noreferrer">KrakenFiles</a>
<a href="https://pixeldrain.com/u/58Azghwf" target="_blank" rel="nofollow noopener noreferrer">PixelDrain</a></div><div class="soraurlx"><strong>480p</strong>
<a href="https://www.mediafire.com/file/77s54951ulv0t0x" target="_blank" rel="nofollow noopener noreferrer">MediaFire</a>
<a href="https://krakenfiles.com/view/Q1ABSfAMNF/file.html" target="_blank" rel="nofollow noopener noreferrer">KrakenFiles</a>
<a href="https://pixeldrain.com/u/nw9SB2qN" target="_blank" rel="nofollow noopener noreferrer">PixelDrain</a></div><div class="soraurlx"><strong>360p</strong>
<a href="https://www.mediafire.com/file/bynyfi6ae0u4lef" target="_blank" rel="nofollow noopener noreferrer">MediaFire</a>
<a href="https://krakenfiles.com/view/ApWRCnM7h7/file.html" target="_blank" rel="nofollow noopener noreferrer">KrakenFiles</a>
<a href="https://pixeldrain.com/u/vQTKQCW8" target="_blank" rel="nofollow noopener noreferrer">PixelDrain</a></div></div></div></div>
</div>
</body></html>`

func parseFixture(t *testing.T, html string) (*goquery.Document, string) {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return doc, html
}

// TestParseDownloadSection_EpisodeTitle (Test 3) memastikan judul episode diekstrak.
func TestParseDownloadSection_EpisodeTitle(t *testing.T) {
	doc, body := parseFixture(t, fixtureDownloadSection)
	title, _ := parseDownloadSection(doc, body)
	if title != "Hajimete no Hitozuma Episode 6" {
		t.Errorf("title = %q, want 'Hajimete no Hitozuma Episode 6'", title)
	}
}

// TestParseDownloadSection_QualityOrder (Test 4 & 10) memastikan 4 kualitas
// terdeteksi dan dipertahankan urutannya: 1080p -> 720p -> 480p -> 360p.
func TestParseDownloadSection_QualityOrder(t *testing.T) {
	doc, body := parseFixture(t, fixtureDownloadSection)
	_, groups := parseDownloadSection(doc, body)
	if len(groups) != 4 {
		t.Fatalf("expected 4 quality groups, got %d: %+v", len(groups), groups)
	}
	wantQ := []string{"1080p", "720p", "480p", "360p"}
	for i, w := range wantQ {
		if groups[i].Quality != w {
			t.Errorf("groups[%d].Quality = %q, want %q", i, groups[i].Quality, w)
		}
	}
}

// TestParseDownloadSection_Providers (Test 5) memastikan provider dikenali.
func TestParseDownloadSection_Providers(t *testing.T) {
	doc, body := parseFixture(t, fixtureDownloadSection)
	_, groups := parseDownloadSection(doc, body)
	first := groups[0]
	wantServers := []string{"MediaFire", "KrakenFiles", "PixelDrain"}
	for i, w := range wantServers {
		if i >= len(first.Links) {
			t.Fatalf("missing provider %s", w)
		}
		if first.Links[i].Server != w {
			t.Errorf("Links[%d].Server = %q, want %q", i, first.Links[i].Server, w)
		}
	}
}

// TestParseDownloadSection_URLExtraction (Test 6) memastikan URL berasal dari
// <a href> yang valid, bukan placeholder/palsu.
func TestParseDownloadSection_URLExtraction(t *testing.T) {
	doc, body := parseFixture(t, fixtureDownloadSection)
	_, groups := parseDownloadSection(doc, body)
	first := groups[0]
	if !strings.HasPrefix(first.Links[0].URL, "https://www.mediafire.com/file/") {
		t.Errorf("bad mediafire url: %q", first.Links[0].URL)
	}
	if !strings.HasPrefix(first.Links[1].URL, "https://krakenfiles.com/view/") {
		t.Errorf("bad kraken url: %q", first.Links[1].URL)
	}
	if !strings.HasPrefix(first.Links[2].URL, "https://pixeldrain.com/u/") {
		t.Errorf("bad pixeldrain url: %q", first.Links[2].URL)
	}
}

// TestParseDownloadSection_MissingProvider (Test 7) memastikan provider yang
// tidak tersedia tidak menghasilkan URL palsu; baris tetap valid, bukan crash.
func TestParseDownloadSection_MissingProvider(t *testing.T) {
	html := `<!DOCTYPE html><html><body><div class="entry-content">
<div class="bixbox"><div class="releases"><h3>Download Fake Episode 1</h3></div><div class="soraddlx soradlg"><div class="sorattlx"><h3>Fake Episode 1</h3></div>
<div class="soraurlx"><strong>1080p</strong>
<a href="https://www.mediafire.com/file/aaa" target="_blank">MediaFire</a>
<a href="#" target="_blank">KrakenFiles</a>
<a href="javascript:void(0)" target="_blank">PixelDrain</a></div>
<div class="soraurlx"><strong>720p</strong>
<a href="https://www.mediafire.com/file/bbb" target="_blank">MediaFire</a></div>
</div></div></div></body></html>`
	doc, body := parseFixture(t, html)
	_, groups := parseDownloadSection(doc, body)
	if len(groups) == 0 {
		t.Fatal("expected at least one quality group despite missing providers")
	}
	// 1080p: hanya MediaFire (href valid); dua provider lain harus ABSEN, bukan URL palsu.
	q1080 := groups[0]
	if q1080.Quality != "1080p" {
		t.Fatalf("expected 1080p first, got %q", q1080.Quality)
	}
	if len(q1080.Links) != 1 || q1080.Links[0].Server != "MediaFire" {
		t.Fatalf("1080p links = %+v, want only MediaFire", q1080.Links)
	}
	// 720p: hanya MediaFire.
	q720 := groups[1]
	if len(q720.Links) != 1 || q720.Links[0].Server != "MediaFire" {
		t.Fatalf("720p links = %+v, want only MediaFire", q720.Links)
	}
}

// TestParseDownloadSection_NonDownloadIgnored (bagian dari Test 6): tautan dari
// bagian halaman lain (bukan download episode) tidak boleh ikut terbaca.
func TestParseDownloadSection_NonDownloadIgnored(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
<nav><a href="https://minioppai.org/">Home</a><a href="https://minioppai.org/anime/x/">Anime Series</a></nav>
<div class="entry-content">
<div class="bixbox"><div class="soraddlx soradlg"><div class="sorattlx"><h3>Fake Episode 1</h3></div>
<div class="soraurlx"><strong>1080p</strong><a href="https://www.mediafire.com/file/ccc">MediaFire</a></div>
</div></div></div></body></html>`
	doc, body := parseFixture(t, html)
	_, groups := parseDownloadSection(doc, body)
	if len(groups) != 1 || len(groups[0].Links) != 1 || groups[0].Links[0].Server != "MediaFire" {
		t.Fatalf("got %+v, want exactly 1 MediaFire link", groups)
	}
}

// TestGetEpisodeDownloads_Live (Test 8) menguji integrasi penuh GetEpisodeDownloads
// lewat server HTTP lokal yang menyajikan HTML download section.
func TestGetEpisodeDownloads_Live(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fixtureDownloadSection))
	}))
	defer srv.Close()

	ep, err := GetEpisodeDownloads(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("GetEpisodeDownloads: %v", err)
	}
	if ep.Title != "Hajimete no Hitozuma Episode 6" {
		t.Errorf("title = %q", ep.Title)
	}
	if len(ep.Qualities) != 4 {
		t.Fatalf("qualities = %d, want 4", len(ep.Qualities))
	}
	if ep.Qualities[0].Quality != "1080p" || ep.Qualities[0].Links[0].Server != "MediaFire" {
		t.Errorf("bad first quality: %+v", ep.Qualities[0])
	}
}

func TestNormalizeDownloadQuality(t *testing.T) {
	cases := map[string]string{
		"1080p":   "1080p",
		"720p":    "720p",
		"480p":    "480p",
		"360p":    "360p",
		" 1080P ": "1080p",
		"":        "",
		"Hentai":  "",
		"Stream":  "",
	}
	for in, want := range cases {
		if got := normalizeDownloadQuality(in); got != want {
			t.Errorf("normalizeDownloadQuality(%q) = %q, want %q", in, got, want)
		}
	}
}

// fixtureEpisodeListPage meniru halaman series dengan link episode + link
// legacy non-episode (Watch Now, Hentai Episodes) yang harus difilter.
const fixtureEpisodeListPage = `<!DOCTYPE html><html><body>
<div class="anime-info">
<h1>Kotowarenai Haha</h1>
</div>
<div class="episode-list">
<a href="https://minioppai.org/kotowarenai-haha-episode-1/">Kotowarenai Haha Episode 1</a>
<a href="https://minioppai.org/kotowarenai-haha-episode-2/">Kotowarenai Haha Episode 2</a>
<a href="https://minioppai.org/kotowarenai-haha-episode-3/">Kotowarenai Haha Episode 3</a>
<a href="https://minioppai.org/watch-now/">Watch Now</a>
<a href="https://minioppai.org/hentai-episodes/">Hentai Episodes</a>
<a href="https://minioppai.org/anime/genre/ecchi/">Genre Ecchi</a>
<a href="https://minioppai.org/kotowarenai-haha-episode-4/">Episode 4</a>
</div>
</body></html>`

func TestGetEpisodeList_FiltersLegacyLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fixtureEpisodeListPage))
	}))
	defer srv.Close()

	eps, err := GetEpisodeList(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("GetEpisodeList: %v", err)
	}
	if len(eps) != 4 {
		t.Fatalf("expected 4 episodes, got %d: %+v", len(eps), eps)
	}
	// Verify order: 1, 2, 3, 4
	for i, ep := range eps {
		expectedNum := strconv.Itoa(i + 1)
		if ep.Number != expectedNum {
			t.Errorf("eps[%d].Number = %q, want %q", i, ep.Number, expectedNum)
		}
	}
}
