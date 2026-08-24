package cmd

import "testing"

func TestIsHentaiSlug(t *testing.T) {
	valid := []string{"furachi-episode-1-id-01", "some-series-episode-12"}
	invalid := []string{"furachi", "two words-ep", "WatchHentai.net/videos/x"}
	for _, s := range valid {
		if !isHentaiSlug(s) {
			t.Errorf("isHentaiSlug(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if isHentaiSlug(s) {
			t.Errorf("isHentaiSlug(%q) = true, want false", s)
		}
	}
}

func TestIsWatchHentaiInput(t *testing.T) {
	if !isWatchHentaiInput("https://watchhentai.net/videos/furachi-episode-1-id-01/") {
		t.Error("full URL harus dikenali sebagai input watchhentai")
	}
	if !isWatchHentaiInput("furachi-episode-1-id-01") {
		t.Error("slug episode harus dikenali sebagai input watchhentai")
	}
	if isWatchHentaiInput("touhou big breasts") {
		t.Error("query pencarian biasa tidak boleh dianggap slug/link")
	}
}

func TestSafeHentaiFileName(t *testing.T) {
	got := safeHentaiFileName(`Furachi Episode 1: "Sub Indo"?`)
	want := "Furachi_Episode_1_Sub_Indo.mp4"
	if got != want {
		t.Errorf("safeHentaiFileName() = %q, want %q", got, want)
	}
}
