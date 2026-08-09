package sponsor

import (
	"testing"
	"time"

	"rbot/brain/store"
)

func setup(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := store.Open(dir); err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	DataDir = dir
	// Reset throttle antar-test (state global paket).
	mu.Lock()
	lastShown = map[string]time.Time{}
	mu.Unlock()
}

func TestKosongSaatBelumDiset(t *testing.T) {
	setup(t)
	if Get() != nil {
		t.Error("Get sebelum diset harus nil")
	}
	if Footer() != "" || Text() != "" {
		t.Error("Footer/Text kosong saat belum ada sponsor")
	}
}

func TestSetGetTextFooter(t *testing.T) {
	setup(t)
	on := true
	Set(SetInput{Type: "text", Text: "Beli kopi\nenak", Link: "https://ex.co", InResults: &on})

	s := Get()
	if s == nil || s.Type != "text" || !s.InResults {
		t.Fatalf("Get = %+v, mau type text inResults true", s)
	}
	if Text() != "Beli kopi\nenak\n\n🔗 https://ex.co" {
		t.Errorf("Text = %q", Text())
	}
	// Footer pakai baris pertama teks sebagai label.
	f := Footer()
	if want := "\n\n━━━━━━━━━━━━━━\n📣 *Sponsor:* Beli kopi — https://ex.co"; f != want {
		t.Errorf("Footer = %q, mau %q", f, want)
	}
}

func TestInResultsDefaultPertahankan(t *testing.T) {
	setup(t)
	on := true
	Set(SetInput{Type: "text", Text: "A", InResults: &on})
	// Set ulang tanpa InResults → pertahankan true.
	Set(SetInput{Type: "text", Text: "B"})
	if s := Get(); s == nil || !s.InResults {
		t.Errorf("InResults harus dipertahankan true, dapat %+v", s)
	}
	// SetInResults eksplisit mematikan.
	if v, ok := SetInResults(false); !ok || v {
		t.Errorf("SetInResults(false) = (%v, %v), mau (false, true)", v, ok)
	}
}

func TestFooterThrottled(t *testing.T) {
	setup(t)
	on := true
	Set(SetInput{Type: "text", Text: "Promo", InResults: &on})

	now := time.Now()
	if FooterThrottled("chat@x", now) == "" {
		t.Error("footer pertama harus muncul")
	}
	// Dalam cooldown: kosong.
	if FooterThrottled("chat@x", now.Add(time.Hour)) != "" {
		t.Error("dalam cooldown 6j harus kosong")
	}
	// Setelah cooldown: muncul lagi.
	if FooterThrottled("chat@x", now.Add(7*time.Hour)) == "" {
		t.Error("setelah cooldown harus muncul lagi")
	}
	// Chat lain tidak terpengaruh throttle chat pertama.
	if FooterThrottled("lain@x", now) == "" {
		t.Error("chat berbeda harus dapat footer sendiri")
	}
}

func TestFooterThrottledTanpaInResults(t *testing.T) {
	setup(t)
	Set(SetInput{Type: "text", Text: "Diam"}) // inResults default false
	if FooterThrottled("chat@x", time.Now()) != "" {
		t.Error("inResults=false → tidak pernah muncul di hasil")
	}
}

func TestClear(t *testing.T) {
	setup(t)
	on := true
	Set(SetInput{Type: "text", Text: "X", InResults: &on})
	Clear()
	if Get() != nil {
		t.Error("setelah Clear, Get harus nil")
	}
}
