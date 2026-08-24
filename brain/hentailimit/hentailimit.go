// Package hentailimit mengelola kuota download harian fitur .hentai untuk
// user free. Premium/owner tidak melewati paket ini (dicek di layer cmd).
//
// Aturan:
//   - di luar grup official : 2 download/hari, hanya kualitas <= 480
//   - di dalam grup official: 5 download/hari, 720p boleh 1x/hari
//   - kualitas >= 1080      : tetap premium-only (dicek cmd/hentai.go)
package hentailimit

import (
	"strings"
	"sync"
	"time"

	"rbot/brain/store"
)

const storeKey = "hentailimit"

// Kuota harian user free.
const (
	LimitLuarGrup = 2
	LimitDiGrup   = 5
	Kuota720      = 1
)

// Tier klasifikasi kualitas video.
const (
	TierLow  = "low"  // <= 480p
	Tier720  = "720"  // 720p
	TierHigh = "high" // >= 1080p
)

var mu sync.Mutex

type usage struct {
	Date      string `json:"date"`
	Downloads int    `json:"downloads"`
	Q720      int    `json:"q720"`
}

func today() string { return time.Now().Format("2006-01-02") }

func readAll() map[string]*usage {
	data := map[string]*usage{}
	_, _ = store.Get(storeKey, &data)
	if data == nil {
		data = map[string]*usage{}
	}
	return data
}

func writeAll(data map[string]*usage) { _ = store.Set(storeKey, data) }

// Tier mengklasifikasi string kualitas (contoh: "480p", "720p HDR", "MP4").
// Yang tidak dikenali dianggap TierLow.
func Tier(quality string) string {
	q := strings.ToLower(quality)
	switch {
	case strings.Contains(q, "1080"), strings.Contains(q, "2k"),
		strings.Contains(q, "4k"), strings.Contains(q, "fullhd"), strings.Contains(q, "x265"):
		return TierHigh
	case strings.Contains(q, "720"):
		return Tier720
	default:
		return TierLow
	}
}

func limitUntuk(dalamGrup bool) int {
	if dalamGrup {
		return LimitDiGrup
	}
	return LimitLuarGrup
}

// syncHarian mengosongkan counter bila record berasal dari hari sebelumnya.
func syncHarian(rec *usage) {
	if rec.Date != today() {
		rec.Date = today()
		rec.Downloads = 0
		rec.Q720 = 0
	}
}

// Check memeriksa apakah user free boleh download kualitas tertentu.
// Return (boleh, pesanPenolakan); pesan kosong bila boleh.
func Check(userKey, quality string, dalamGrup bool) (bool, string) {
	if strings.TrimSpace(userKey) == "" {
		return true, ""
	}
	tier := Tier(quality)

	mu.Lock()
	defer mu.Unlock()
	data := readAll()
	rec := data[userKey]
	if rec != nil {
		syncHarian(rec)
	}

	switch tier {
	case TierHigh:
		// Premium-only; tidak diblok di sini (cmd layer yang menolak).
		return true, ""
	case Tier720:
		if !dalamGrup {
			return false, pesan720LuarGrup()
		}
		if rec != nil && rec.Q720 >= Kuota720 {
			return false, pesan720Habis()
		}
		if rec != nil && rec.Downloads >= limitUntuk(true) {
			return false, pesanLimit(true)
		}
		return true, ""
	default: // TierLow
		if rec != nil && rec.Downloads >= limitUntuk(dalamGrup) {
			return false, pesanLimit(dalamGrup)
		}
		return true, ""
	}
}

// Record mencatat satu download yang benar-benar dimulai.
// Panggil hanya setelah Check mengizinkan.
func Record(userKey, quality string, dalamGrup bool) {
	if strings.TrimSpace(userKey) == "" {
		return
	}

	mu.Lock()
	defer mu.Unlock()
	data := readAll()
	rec := data[userKey]
	if rec == nil {
		rec = &usage{}
		data[userKey] = rec
	}
	syncHarian(rec)
	rec.Downloads++
	if Tier(quality) == Tier720 {
		rec.Q720++
	}
	writeAll(data)
}

// ResetAll menghapus seluruh data kuota. Return jumlah record dihapus.
func ResetAll() int {
	mu.Lock()
	defer mu.Unlock()
	data := readAll()
	n := len(data)
	writeAll(map[string]*usage{})
	return n
}

func pesanLimit(dalamGrup bool) string {
	if dalamGrup {
		return "🚫 *Limit download hentai harian habis* (5x/hari di grup).\n\nKuota reset besok. 💎 Premium bebas limit: ketik *.premium*"
	}
	return "🚫 *Limit download hentai habis* (2x/hari di luar grup).\n\nMasuk grup official dapat 5x/hari + 720p 1x. 💎 Premium bebas limit: ketik *.premium*"
}

func pesan720LuarGrup() string {
	return "🚫 *Kualitas 720p hanya bisa dipakai di grup official* (1x/hari).\n\nMasuk grup official untuk membuka kuota 720p."
}

func pesan720Habis() string {
	return "🚫 *Kuota 720p harian habis* (1x/hari di grup).\n\nKuota reset besok. 💎 Premium bebas semua kualitas: ketik *.premium*"
}
