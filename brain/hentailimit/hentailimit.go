// Package hentailimit mengelola batas pemakaian fitur .hentai untuk user free.
// Premium/owner tidak melewati paket ini (dicek di layer cmd).
//
// Aturan:
//   - user free hanya bisa kualitas <= 480p (720p ke atas premium-only)
//   - di luar grup official : 2 download/hari
//   - di dalam grup official: 5 download/hari
package hentailimit

import (
	"fmt"
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
)

// Tier klasifikasi kualitas video.
const (
	TierLow  = "low"  // <= 480p, satu-satunya yang boleh untuk user free
	Tier720  = "720"  // 720p, premium-only
	TierHigh = "high" // >= 1080p, premium-only
)

var mu sync.Mutex

type usage struct {
	Date      string `json:"date"`
	Downloads int    `json:"downloads"`
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

// IsFreeQuality true bila kualitas masih boleh dipakai user free (<= 480p).
func IsFreeQuality(quality string) bool { return Tier(quality) == TierLow }

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
	}
}

// Check memeriksa izin download user free: kualitas harus <= 480p dan kuota
// harian belum habis. Return (boleh, pesanPenolakan); pesan kosong bila boleh.
func Check(userKey, quality string, dalamGrup bool) (bool, string) {
	if strings.TrimSpace(userKey) == "" {
		return true, ""
	}
	if !IsFreeQuality(quality) {
		return false, PesanPremium(quality)
	}

	mu.Lock()
	defer mu.Unlock()
	data := readAll()
	rec := data[userKey]
	if rec != nil {
		syncHarian(rec)
	}
	if rec != nil && rec.Downloads >= limitUntuk(dalamGrup) {
		return false, pesanLimit(dalamGrup)
	}
	return true, ""
}

// Record mencatat satu download yang benar-benar dimulai.
// Panggil hanya setelah Check mengizinkan; kualitas premium diabaikan.
func Record(userKey, quality string) {
	if strings.TrimSpace(userKey) == "" || !IsFreeQuality(quality) {
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
		return "🚫 *Limit download harian habis* (5x/hari di grup official).\n\nKuota reset besok. 💎 Premium bebas limit & semua kualitas: ketik *.premium*"
	}
	return "🚫 *Limit download habis* (2x/hari di luar grup).\n\nMasuk grup official dapat 5x/hari. 💎 Premium bebas limit & semua kualitas: ketik *.premium*"
}

// PesanPremium penolakan untuk kualitas di atas 480p pada user free.
func PesanPremium(quality string) string {
	return fmt.Sprintf("💎 *Kualitas %s Khusus User Premium!*\n\nUser Free hanya dapat mengunduh kualitas *480p*.\n\nUpgrade ke Premium untuk membuka semua kualitas & bebas limit:\nKetik *.premium*", quality)
}
