package kamino

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	cases := map[string]string{
		"https://vt.tiktok.com/abc":        "tiktok",
		"https://www.instagram.com/p/x":    "instagram",
		"https://x.com/u/status/1":         "twitter",
		"https://youtu.be/abc":             "youtube",
		"https://open.spotify.com/track/x": "spotify",
		"https://pin.it/abc":               "pinterest",
		"https://example.com/foo":          "",
	}
	for url, want := range cases {
		if got := Detect(url); got != want {
			t.Errorf("Detect(%q) = %q, mau %q", url, got, want)
		}
	}
}

func TestIsYoutubePlaylist(t *testing.T) {
	if !isYoutubePlaylist("https://www.youtube.com/playlist?list=PL123") {
		t.Error("list tanpa v harus playlist")
	}
	if isYoutubePlaylist("https://www.youtube.com/watch?v=abc&list=PL123") {
		t.Error("ada v → bukan playlist (single video)")
	}
	if isYoutubePlaylist("https://youtu.be/abc") {
		t.Error("youtu.be tanpa list bukan playlist")
	}
}

// TestSolvePow memverifikasi solver PoW: hasilnya harus benar-benar membuat
// hash berawalan d buah '0' sesuai scheme, dan itu index terkecil.
func TestSolvePow(t *testing.T) {
	sc := &scheme{D: 2, Parts: []string{"n", "i"}, Sep: "", Rounds: 1}
	nonce := "testnonce"
	sol := solvePow(nonce, sc)

	// Rekonstruksi hash untuk solusi & pastikan berawalan "00".
	got := sha256hex(nonce + sol)
	if !strings.HasPrefix(got, "00") {
		t.Fatalf("solusi %q → hash %q tidak berawalan 00", sol, got)
	}
	// Pastikan minimal: semua index sebelum sol TIDAK memenuhi.
	solIdx := atoiTest(t, sol)
	for i := 0; i < solIdx; i++ {
		if strings.HasPrefix(sha256hex(nonce+itoa(i)), "00") {
			t.Fatalf("index %d lebih kecil juga valid; solver tidak ambil terkecil", i)
		}
	}
}

func TestParseScheme(t *testing.T) {
	// Bungkus {"scheme":{...}} jadi base64 lalu tambahkan ".sig" seperti challenge asli.
	jsonBody := `{"scheme":{"d":3,"parts":["s","n","i"],"sep":"-","rounds":2,"salt":"xyz"}}`
	c := base64.StdEncoding.EncodeToString([]byte(jsonBody)) + ".signaturepart"
	sc := parseScheme(c)
	if sc == nil {
		t.Fatal("parseScheme mengembalikan nil untuk challenge valid")
	}
	if sc.D != 3 || sc.Rounds != 2 || sc.Sep != "-" || sc.Salt != "xyz" || len(sc.Parts) != 3 {
		t.Errorf("scheme ter-parse salah: %+v", sc)
	}
	if parseScheme("bukan base64 valid!!!") != nil {
		t.Error("input rusak harus nil")
	}
}

func atoiTest(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			t.Fatalf("solusi bukan angka: %q", s)
		}
		n = n*10 + int(ch-'0')
	}
	return n
}
