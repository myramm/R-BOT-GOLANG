package cmd

import (
	"testing"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/config"
	"rbot/brain/hentailimit"
	"rbot/brain/store"
)

func openHentaiStore(t *testing.T) {
	t.Helper()
	if err := store.Open(t.TempDir()); err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
}

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
	got := safeHentaiFileName(`Furachi Episode 1: "Sub Indo"?`, "1080p")
	want := "Furachi_Episode_1_Sub_Indo_1080p.mp4"
	if got != want {
		t.Errorf("safeHentaiFileName() = %q, want %q", got, want)
	}
}

func TestHentaiIzinPremiumLolosSemua(t *testing.T) {
	openHentaiStore(t)
	for _, q := range []string{"360p", "480p", "720p", "1080p", "4K"} {
		if ok, msg := hentaiIzin("62801", q, false, true); !ok {
			t.Errorf("premium kualitas %q harus lolos, dapat tolak: %q", q, msg)
		}
	}
}

func TestHentaiIzinHighFreeDitolak(t *testing.T) {
	openHentaiStore(t)
	ok, msg := hentaiIzin("62802", "1080p", true, false)
	if ok {
		t.Fatal("1080p untuk free user harus ditolak")
	}
	if msg == "" {
		t.Error("pesan tolak 1080p tidak boleh kosong")
	}
}

func TestHentaiIzin480LuarGrupMaksDua(t *testing.T) {
	openHentaiStore(t)
	for i := 0; i < 2; i++ {
		ok, msg := hentaiIzin("62803", "480p", false, false)
		if !ok {
			t.Fatalf("download ke-%d (480p luar grup) harus boleh: %q", i+1, msg)
		}
		hentailimit.Record("62803", "480p")
	}
	if ok, _ := hentaiIzin("62803", "480p", false, false); ok {
		t.Fatal("download ke-3 (480p luar grup) harus ditolak")
	}
}

func TestHentaiIzin720FreeDitolakDiManaPun(t *testing.T) {
	openHentaiStore(t)
	if ok, msg := hentaiIzin("62804", "720p", false, false); ok || msg == "" {
		t.Fatal("720p luar grup untuk free user harus ditolak dengan pesan")
	}
	if ok, _ := hentaiIzin("62805", "720p", true, false); ok {
		t.Fatal("720p di grup official pun harus ditolak untuk free user")
	}
}

func TestIsGrupOfficialChat(t *testing.T) {
	orig := config.C
	t.Cleanup(func() { config.C = orig })

	config.C = config.Config{}
	evt := &events.Message{}
	evt.Info.IsGroup = true
	evt.Info.Chat = types.NewJID("12036302", types.DefaultUserServer)

	if isGrupOfficialChat(evt) {
		t.Fatal("jid kosong tidak boleh dianggap grup official")
	}

	config.C.GrupOfficial.JID = "12036302@g.us"
	if !isGrupOfficialChat(evt) {
		t.Fatal("chat yang cocok dengan grupOfficial.jid harus true")
	}

	evt.Info.IsGroup = false
	if isGrupOfficialChat(evt) {
		t.Fatal("chat pribadi tidak boleh dianggap grup official")
	}
}
