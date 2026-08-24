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
		Record("62801", "480p")
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
		Record("62802", "480p")
	}

	ok, msg := Check("62802", "480p", true)
	if ok {
		t.Fatal("download ke-6 di grup harus ditolak")
	}
	if !strings.Contains(msg, "5") {
		t.Errorf("pesan tolak harus menyebut limit 5, dapat: %q", msg)
	}
}

func TestKualitasDiAtas480PremiumOnly(t *testing.T) {
	openStore(t)

	for _, q := range []string{"720p", "1080p", "4K"} {
		ok, msg := Check("62803", q, false)
		if ok {
			t.Errorf("%s di luar grup harus ditolak untuk free user", q)
		}
		if !strings.Contains(msg, "480") {
			t.Errorf("pesan tolak %s harus menyebut batas 480p, dapat: %q", q, msg)
		}

		ok, _ = Check("62803", q, true)
		if ok {
			t.Errorf("%s di dalam grup juga harus ditolak untuk free user", q)
		}
	}
}

func TestResetHarian(t *testing.T) {
	openStore(t)

	Record("62807", "480p")
	Record("62807", "480p")

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

	Record("62808", "480p")
	Record("62808", "480p")

	if ok, _ := Check("62809", "480p", false); !ok {
		t.Fatal("user lain tidak boleh terpengaruh kuota user pertama")
	}
}

func TestRecordAbaikanKualitasPremium(t *testing.T) {
	openStore(t)

	Record("62810", "720p")
	Record("62810", "4K")

	data := readAll()
	if rec := data["62810"]; rec != nil {
		t.Fatal("kualitas premium tidak boleh tercatat di kuota free")
	}
}
