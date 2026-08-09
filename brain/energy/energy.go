// Package energy mengelola ekonomi energi bot. Port dari lib/energy.js.
// Store: key "energy" -> { bareId: { bank, date, prem, bonus } }.
//
//	bank  : energi tersimpan (boleh > jatah harian bila sisa tabungan premium)
//	date  : tanggal klaim harian terakhir (YYYY-MM-DD lokal)
//	prem  : status premium saat terakhir dilihat — untuk deteksi premium habis
//	bonus : bonus manual dari owner, persist lintas hari
package energy

import (
	"math"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/config"
	"rbot/brain/identity"
	"rbot/brain/premium"
	"rbot/brain/store"
)

const storeKey = "energy"

// mu menjaga operasi read-modify-write agar atomik (whatsmeow bisa mengantar
// event dari beberapa chat secara paralel; Node aslinya single-thread).
var mu sync.Mutex

type record struct {
	Bank  int    `json:"bank"`
	Date  string `json:"date"`
	Prem  bool   `json:"prem"`
	Bonus int    `json:"bonus"`

	// Used hanya untuk migrasi format lama { used, date, bonus }. Tidak ditulis
	// balik (json omitempty) setelah migrate mengubahnya jadi model bank.
	Used *int `json:"used,omitempty"`
}

func readAll() map[string]*record {
	data := map[string]*record{}
	_, _ = store.Get(storeKey, &data)
	if data == nil {
		data = map[string]*record{}
	}
	return data
}

func writeAll(data map[string]*record) { _ = store.Set(storeKey, data) }

// today mengembalikan tanggal lokal YYYY-MM-DD sebagai penanda klaim harian.
func today() string { return time.Now().Format("2006-01-02") }

// === Konfigurasi (pintasan ke accessor config) ===

func dailyLimit() int       { return config.C.Energy.DailyLimit() }
func isUnlimited() bool     { return config.C.Energy.IsUnlimited() }
func premiumDaily() int     { return config.C.Energy.PremiumDaily() }
func pajakExpired() float64 { return config.C.Energy.PajakExpired() }
func diskonGrup() float64   { return config.C.Energy.DiskonGrup() }

// EnergyCost mengembalikan biaya dasar sebuah command (tak terdaftar = 1).
func EnergyCost(cmd string) int { return config.C.Energy.EnergyCost(cmd) }

// BiayaEfektif menghitung biaya setelah diskon member grup official. Minimal
// tetap 1 supaya tidak gratis.
func BiayaEfektif(cmd string, memberGrup bool) int {
	dasar := EnergyCost(cmd)
	if !memberGrup || dasar <= 0 {
		return dasar
	}
	return int(math.Max(1, math.Ceil(float64(dasar)*(1-diskonGrup()))))
}

// === Record ===

func blank(limit int) *record {
	return &record{Bank: limit, Date: today(), Prem: false, Bonus: 0}
}

// migrate mengubah format lama { used, date, bonus } menjadi model bank.
// Idempoten: record yang sudah punya bank tidak diubah.
func migrate(rec *record, limit int) {
	if rec.Used == nil {
		return // sudah model bank
	}
	used := *rec.Used
	rec.Bank = int(math.Max(0, float64(limit+rec.Bonus-used)))
	rec.Prem = false
	rec.Used = nil
}

// EventKind membedakan jenis kejadian sinkronisasi energi.
type EventKind string

const (
	EventPajak        EventKind = "pajak"         // premium habis, sisa energi dipotong
	EventKlaimPremium EventKind = "klaim-premium" // jatah premium harian ditumpuk
	EventIsiHarian    EventKind = "isi-harian"    // energi user biasa diisi ulang
)

// Event mencatat satu kejadian sinkronisasi (union field per jenis).
type Event struct {
	Kind         EventKind
	Potong, Sisa int // pajak
	Jatah, Bank  int // klaim-premium
	Dari, Ke     int // isi-harian
}

// syncRecord menyelaraskan record ke kondisi hari ini + status premium.
// Mengembalikan daftar kejadian supaya pemanggil bisa memberi tahu user.
func syncRecord(rec *record, limit int, prem bool) []Event {
	t := today()
	var evs []Event

	migrate(rec, limit)

	// Premium baru habis → sisa energi kena pajak, sisanya tetap boleh dipakai.
	if rec.Prem && !prem {
		potong := int(math.Floor(float64(rec.Bank) * pajakExpired()))
		rec.Bank = int(math.Max(0, float64(rec.Bank-potong)))
		rec.Prem = false
		if potong > 0 {
			evs = append(evs, Event{Kind: EventPajak, Potong: potong, Sisa: rec.Bank})
		}
	}

	if rec.Date != t {
		if prem {
			// Premium: jatah harian ditumpuk, tidak menimpa saldo lama.
			jatah := premiumDaily()
			rec.Bank += jatah
			evs = append(evs, Event{Kind: EventKlaimPremium, Jatah: jatah, Bank: rec.Bank})
		} else {
			// Biasa: diisi ulang penuh, tanpa memangkas sisa tabungan premium.
			penuh := limit + rec.Bonus
			if rec.Bank < penuh {
				evs = append(evs, Event{Kind: EventIsiHarian, Dari: rec.Bank, Ke: penuh})
				rec.Bank = penuh
			}
		}
		rec.Date = t
	}

	rec.Prem = prem
	return evs
}

// found adalah hasil recordOf.
type found struct {
	key string
	rec *record
	evs []Event
}

// recordOf mengambil record kanonik user (id pertama yang sudah punya data,
// kalau tidak id pertama), lalu menyinkronkannya. Return nil bila tak ada id.
func recordOf(evt *events.Message, data map[string]*record, limit int, prem bool) *found {
	ids := identity.Candidates(evt)
	if len(ids) == 0 {
		return nil
	}
	key := ids[0]
	for _, id := range ids {
		if _, ok := data[id]; ok {
			key = id
			break
		}
	}
	if data[key] == nil {
		data[key] = blank(limit)
	}
	evs := syncRecord(data[key], limit, prem)
	return &found{key: key, rec: data[key], evs: evs}
}

// Info adalah ringkasan energi user.
type Info struct {
	Bank      int
	Limit     int
	Bonus     int
	Remaining int
	Max       int
	Unlimited bool
	Premium   bool
	Events    []Event
}

// Get mengembalikan info energi user, sekaligus menyinkronkan record (klaim
// harian/pajak) bila perlu. Untuk owner/unlimited: Unlimited=true.
func Get(evt *events.Message) Info {
	limit := dailyLimit()
	owner := identity.IsOwner(evt)

	if limit == 0 || owner {
		return Info{Limit: limit, Unlimited: true, Premium: owner}
	}

	prem := premium.IsPremium(evt)

	mu.Lock()
	defer mu.Unlock()
	data := readAll()
	f := recordOf(evt, data, limit, prem)
	if f == nil {
		return Info{Bank: limit, Limit: limit, Remaining: limit, Premium: prem}
	}
	writeAll(data)

	maxBank := limit + f.rec.Bonus
	if prem {
		maxBank = f.rec.Bank
	}
	return Info{
		Bank:      f.rec.Bank,
		Limit:     limit,
		Bonus:     f.rec.Bonus,
		Remaining: f.rec.Bank,
		Max:       maxBank,
		Premium:   prem,
		Events:    f.evs,
	}
}

// HasEnergy true bila user mampu membayar cost (owner/unlimited selalu true).
func HasEnergy(evt *events.Message, cost int) bool {
	if isUnlimited() || identity.IsOwner(evt) {
		return true
	}
	return Get(evt).Remaining >= cost
}

// Consume mengurangi energi user. Dipanggil setelah command sukses.
func Consume(evt *events.Message, cost int) {
	if isUnlimited() || identity.IsOwner(evt) || cost <= 0 {
		return
	}
	limit := dailyLimit()
	prem := premium.IsPremium(evt)

	mu.Lock()
	defer mu.Unlock()
	data := readAll()
	f := recordOf(evt, data, limit, prem)
	if f == nil {
		return
	}
	f.rec.Bank = int(math.Max(0, float64(f.rec.Bank-cost)))
	writeAll(data)
}

// RestoreResult adalah hasil Restore (makan/istirahat).
type RestoreResult struct {
	OK        bool
	Unlimited bool
	Err       string
	Tambah    int
	Terbuang  int
	Bank      int
	Penuh     bool

	// Plafon adalah batas atas energi efektif saat pengisian; PlafonTakHingga
	// true bila tak terbatas (owner/unlimited/premium) sehingga pemanggil tak
	// menampilkan "/plafon" (setara cek Number.isFinite(plafon) di Node).
	Plafon          int
	PlafonTakHingga bool
}

// Restore menambah energi. User biasa tidak bisa melewati plafon (limit+bonus),
// tapi sisa tabungan premium di atas plafon tidak ikut dipangkas.
func Restore(evt *events.Message, amount int) RestoreResult {
	if isUnlimited() || identity.IsOwner(evt) {
		return RestoreResult{OK: true, Unlimited: true, PlafonTakHingga: true}
	}
	jumlah := int(math.Max(0, float64(amount)))
	if jumlah == 0 {
		return RestoreResult{Err: "Jumlah energi tidak valid."}
	}
	limit := dailyLimit()
	prem := premium.IsPremium(evt)

	mu.Lock()
	defer mu.Unlock()
	data := readAll()
	f := recordOf(evt, data, limit, prem)
	if f == nil {
		return RestoreResult{Err: "Data user tidak terbaca."}
	}
	rec := f.rec
	plafon := math.MaxInt
	if !prem {
		plafon = limit + rec.Bonus
		if rec.Bank > plafon {
			plafon = rec.Bank
		}
	}
	sebelum := rec.Bank
	rec.Bank = min(plafon, rec.Bank+jumlah)
	writeAll(data)

	tambah := rec.Bank - sebelum
	return RestoreResult{
		OK:              true,
		Tambah:          tambah,
		Terbuang:        jumlah - tambah,
		Bank:            rec.Bank,
		Penuh:           tambah == 0,
		Plafon:          plafon,
		PlafonTakHingga: prem,
	}
}

// === Fungsi owner (pakai id bare, bukan evt) ===

// AddBonus menambah/mengurangi bonus energi manual untuk id. Return (bonusBaru,
// true) atau (0, false) bila id kosong.
func AddBonus(id string, amount int) (int, bool) {
	key := config.BareNumber(id)
	if key == "" {
		return 0, false
	}
	limit := dailyLimit()

	mu.Lock()
	defer mu.Unlock()
	data := readAll()
	if data[key] == nil {
		data[key] = blank(limit)
	}
	rec := data[key]
	migrate(rec, limit)
	rec.Bonus = int(math.Max(0, float64(rec.Bonus+amount)))
	// Bonus positif langsung bisa dipakai; bonus negatif tidak menyedot saldo.
	if amount > 0 {
		rec.Bank += amount
	}
	writeAll(data)
	return rec.Bonus, true
}

// Reset mengembalikan energi id ke kondisi blank. Return false bila id kosong.
func Reset(id string) bool {
	key := config.BareNumber(id)
	if key == "" {
		return false
	}
	mu.Lock()
	defer mu.Unlock()
	data := readAll()
	data[key] = blank(dailyLimit())
	writeAll(data)
	return true
}

// ResetAll menghapus seluruh data energi. Return jumlah record yang dihapus.
func ResetAll() int {
	mu.Lock()
	defer mu.Unlock()
	data := readAll()
	n := len(data)
	writeAll(map[string]*record{})
	return n
}
