package hentailimit

import (
	"strings"
	"testing"
	"time"

	"rbot/brain/store"
)

func openStore(t *testing.T) {
	t.Helper()
	if err := store.Open(t.TempDir()); err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
}

func TestTierKlasifikasi(t *testing.T) {
	cases := map[string]string{
		"480p":  TierLow,
		"360p":  TierLow,
		"MP4":   TierLow,
		"720p":  Tier720,
		"1080p": TierHigh,
		"4K":    TierHigh,
	}
	for q, want := range cases {
		if got := Tier(q); got != want {
			t.Errorf("Tier(%q) = %q, mau %q", q, got, want)
		}
	}
}

func TestFreeUserLuarGrupDuaKali(t *testing.T) {
	openStore(t)

	for i := 0; i < LimitLuarGrup; i++ {
		ok, msg := Check("62801", "480p", false)
		if !ok {
			t.Fatalf("download ke-%d harus boleh, dapat tolak: %q", i+1, msg)
		}
		Record("62801", "480p", false)
	}

	ok, msg := Check("62801", "480p", false)
	if ok {
		t.Fatal("download ke-3 di luar grup harus ditolak")
	}
	if !strings.Contains(msg, "2") {
		t.Errorf("pesan tolak harus menyebut limit 2, dapat: %q", msg)
	}
}

func TestFreeUserDiGrupLimaKali(t *testing.T) {
	openStore(t)

	for i := 0; i < LimitDiGrup; i++ {
		ok, msg := Check("62802", "480p", true)
		if !ok {
			t.Fatalf("download ke-%d di grup harus boleh, dapat tolak: %q", i+1, msg)
		}
		Record("62802", "480p", true)
	}

	ok, msg := Check("62802", "480p", true)
	if ok {
		t.Fatal("download ke-6 di grup harus ditolak")
	}
	if !strings.Contains(msg, "5") {
		t.Errorf("pesan tolak harus menyebut limit 5, dapat: %q", msg)
	}
}

func Test720LuarGrupDitolak(t *testing.T) {
	openStore(t)

	ok, msg := Check("62803", "720p", false)
	if ok {
		t.Fatal("720p di luar grup harus ditolak untuk free user")
	}
	if msg == "" {
		t.Error("pesan tolak tidak boleh kosong")
	}
}

func Test720DiGrupHanyaSekali(t *testing.T) {
	openStore(t)

	ok, _ := Check("62804", "720p", true)
	if !ok {
		t.Fatal("720p pertama di grup harus boleh")
	}
	Record("62804", "720p", true)

	ok, msg := Check("62804", "720p", true)
	if ok {
		t.Fatal("720p kedua di grup harus ditolak (kuota 1x)")
	}
	if msg == "" {
		t.Error("pesan tolak 720 tidak boleh kosong")
	}
}

func Test720TetapMemakaiSlotDownload(t *testing.T) {
	openStore(t)

	// 4 download 480p + 1 download 720p = 5 slot habis.
	for i := 0; i < LimitDiGrup-1; i++ {
		Record("62805", "480p", true)
	}
	if ok, _ := Check("62805", "720p", true); !ok {
		t.Fatal("720p sebagai download ke-5 di grup harus boleh")
	}
	Record("62805", "720p", true)

	if ok, _ := Check("62805", "360p", true); ok {
		t.Fatal("download ke-6 harus ditolak meski 720 sudah habis")
	}
}

func Test720TanpaSlotTersisaDitolak(t *testing.T) {
	openStore(t)

	// Slot habis dulu tanpa memakai 720.
	for i := 0; i < LimitDiGrup; i++ {
		Record("62806", "480p", true)
	}
	if ok, _ := Check("62806", "720p", true); ok {
		t.Fatal("720p harus ditolak bila slot download harian sudah habis")
	}
}

func TestResetHarian(t *testing.T) {
	openStore(t)

	Record("62807", "480p", false)
	Record("62807", "480p", false)

	// Backdate record ke kemarin → hari baru, kuota kembali penuh.
	mu.Lock()
	data := readAll()
	if rec, ok := data["62807"]; ok {
		rec.Date = time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	}
	writeAll(data)
	mu.Unlock()

	if ok, _ := Check("62807", "480p", false); !ok {
		t.Fatal("kuota harus reset di hari baru")
	}
}

func TestUserBerbedaIndependen(t *testing.T) {
	openStore(t)

	Record("62808", "480p", false)
	Record("62808", "480p", false)

	if ok, _ := Check("62809", "480p", false); !ok {
		t.Fatal("user lain tidak boleh terpengaruh kuota user pertama")
	}
}
