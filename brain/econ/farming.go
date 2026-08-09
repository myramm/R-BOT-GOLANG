package econ

import (
	"fmt"

	"go.mau.fi/whatsmeow/types/events"
)

// farming.go: katalog tanaman + operasi bertani. Port lib/farming.js.

// Crop mendeskripsikan satu jenis tanaman.
type Crop struct {
	ID            string
	Name          string
	Emoji         string
	Type          string // "sayur" | "buah"
	GrowTime      int64  // ms
	Seed          int    // harga bibit
	Sell          int    // harga jual per unit
	EnergyRestore int    // energi dipulihkan bila dimakan
	Yield         [2]int // hasil panen [min,max]
}

// Crops adalah katalog tanaman (harga bibit < harga jual agar bertani untung).
// growTime disusun seperti Node: sayur murah & cepat, buah lebih lama.
var Crops = map[string]Crop{
	"wortel":   {ID: "wortel", Name: "Wortel", Emoji: "🥕", Type: "sayur", GrowTime: 30 * 60 * 1000, Seed: 10, Sell: 18, EnergyRestore: 3, Yield: [2]int{2, 4}},
	"bayam":    {ID: "bayam", Name: "Bayam", Emoji: "🥬", Type: "sayur", GrowTime: 20 * 60 * 1000, Seed: 6, Sell: 12, EnergyRestore: 2, Yield: [2]int{2, 5}},
	"tomat":    {ID: "tomat", Name: "Tomat", Emoji: "🍅", Type: "sayur", GrowTime: 45 * 60 * 1000, Seed: 14, Sell: 26, EnergyRestore: 4, Yield: [2]int{2, 4}},
	"jagung":   {ID: "jagung", Name: "Jagung", Emoji: "🌽", Type: "sayur", GrowTime: 60 * 60 * 1000, Seed: 20, Sell: 38, EnergyRestore: 5, Yield: [2]int{1, 3}},
	"pisang":   {ID: "pisang", Name: "Pisang", Emoji: "🍌", Type: "buah", GrowTime: 90 * 60 * 1000, Seed: 25, Sell: 48, EnergyRestore: 6, Yield: [2]int{2, 3}},
	"apel":     {ID: "apel", Name: "Apel", Emoji: "🍎", Type: "buah", GrowTime: 2 * 60 * 60 * 1000, Seed: 35, Sell: 70, EnergyRestore: 7, Yield: [2]int{1, 3}},
	"semangka": {ID: "semangka", Name: "Semangka", Emoji: "🍉", Type: "buah", GrowTime: 3 * 60 * 60 * 1000, Seed: 50, Sell: 105, EnergyRestore: 9, Yield: [2]int{1, 2}},
}

const (
	PlotAwal = 3  // lahan awal user baru
	PlotMax  = 12 // lahan maksimum
)

// CropOrder menjaga urutan tampilan katalog (map Go tak berurut; Node memakai
// urutan penyisipan Object.entries).
var CropOrder = []string{"wortel", "bayam", "tomat", "jagung", "pisang", "apel", "semangka"}

// HargaLahan: harga lahan ke-n naik progresif 150, 300, 450, ...
func HargaLahan(jumlahSekarang int) int { return 150 * (jumlahSekarang - PlotAwal + 1) }

// FormatDurasi memformat sisa waktu jadi "Xj Ym"/"Xm Yd"/"Xd" (setara Node).
func FormatDurasi(ms int64) string {
	if ms <= 0 {
		return "siap"
	}
	j := ms / 3600000
	m := (ms % 3600000) / 60000
	d := (ms % 60000) / 1000
	if j > 0 {
		return fmt.Sprintf("%dj %dm", j, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %dd", m, d)
	}
	return fmt.Sprintf("%dd", d)
}

// === View kebun ===

// PlotView adalah status satu lahan yang sudah dihitung progresnya.
type PlotView struct {
	I      int
	Kosong bool
	ID     string
	Nama   string
	Emoji  string
	Persen int
	Siap   bool
	Sisa   int64
}

// KebunView adalah status seluruh kebun + dompet.
type KebunView struct {
	Plots []PlotView
	Inv   map[string]int
	Coins int
}

// LihatKebun mengembalikan status semua lahan user (nil bila id tak terbaca).
func LihatKebun(evt *events.Message) *KebunView {
	mu.Lock()
	defer mu.Unlock()
	data := loadFarming()
	_, rec := farmRecOf(evt, data)
	if rec == nil {
		return nil
	}
	now := nowMS()

	plots := make([]PlotView, len(rec.Plots))
	for i, p := range rec.Plots {
		crop, ok := Crops[p.Crop]
		if p.Crop == "" || !ok {
			plots[i] = PlotView{I: i, Kosong: true}
			continue
		}
		lewat := now - p.PlantedAt
		sisa := crop.GrowTime - lewat
		if sisa < 0 {
			sisa = 0
		}
		persen := int(float64(lewat) / float64(crop.GrowTime) * 100)
		if persen > 100 {
			persen = 100
		}
		if persen < 0 {
			persen = 0
		}
		plots[i] = PlotView{
			I: i, ID: p.Crop, Nama: crop.Name, Emoji: crop.Emoji,
			Persen: persen, Siap: sisa == 0, Sisa: sisa,
		}
	}

	saveFarming(data)
	inv := make(map[string]int, len(rec.Inv))
	for k, v := range rec.Inv {
		inv[k] = v
	}
	return &KebunView{Plots: plots, Inv: inv, Coins: rec.Coins}
}

// === Operasi (semua return struct hasil dengan OK/Err seperti pola Node) ===

// TanamHasil: hasil operasi Tanam.
type TanamHasil struct {
	OK    bool
	Err   string
	Crop  Crop
	Plot  int
	Sisa  int64
	Coins int
}

// Tanam menanam bibit di lahan plotIndex (0-based). Memotong koin senilai bibit.
func Tanam(evt *events.Message, plotIndex int, cropID string) TanamHasil {
	mu.Lock()
	defer mu.Unlock()
	data := loadFarming()
	_, rec := farmRecOf(evt, data)
	if rec == nil {
		return TanamHasil{Err: "Data user tidak terbaca."}
	}
	crop, ok := Crops[cropID]
	if !ok {
		return TanamHasil{Err: fmt.Sprintf("Bibit *%s* tidak ada di katalog.", cropID)}
	}
	if plotIndex < 0 || plotIndex >= len(rec.Plots) {
		return TanamHasil{Err: fmt.Sprintf("Nomor lahan harus 1–%d.", len(rec.Plots))}
	}
	if rec.Plots[plotIndex].Crop != "" {
		return TanamHasil{Err: fmt.Sprintf("Lahan %d masih terisi. Panen dulu.", plotIndex+1)}
	}
	if rec.Coins < crop.Seed {
		return TanamHasil{Err: fmt.Sprintf("Koin kurang. Bibit %s %d koin, kamu punya %d.", crop.Name, crop.Seed, rec.Coins)}
	}

	coinAdd(rec, -crop.Seed)
	rec.Plots[plotIndex] = Plot{Crop: cropID, PlantedAt: nowMS()}
	saveFarming(data)

	return TanamHasil{OK: true, Crop: crop, Plot: plotIndex + 1, Sisa: crop.GrowTime, Coins: rec.Coins}
}

// PanenHasil: hasil operasi Panen (satu lahan).
type PanenHasil struct {
	OK     bool
	Err    string
	Crop   Crop
	Jumlah int
	Total  int
	Plot   int
}

// Panen memanen satu lahan yang sudah matang.
func Panen(evt *events.Message, plotIndex int) PanenHasil {
	mu.Lock()
	defer mu.Unlock()
	data := loadFarming()
	_, rec := farmRecOf(evt, data)
	if rec == nil {
		return PanenHasil{Err: "Data user tidak terbaca."}
	}
	if plotIndex < 0 || plotIndex >= len(rec.Plots) {
		return PanenHasil{Err: fmt.Sprintf("Nomor lahan harus 1–%d.", len(rec.Plots))}
	}
	p := rec.Plots[plotIndex]
	crop, ok := Crops[p.Crop]
	if p.Crop == "" || !ok {
		return PanenHasil{Err: fmt.Sprintf("Lahan %d masih kosong.", plotIndex+1)}
	}
	sisa := crop.GrowTime - (nowMS() - p.PlantedAt)
	if sisa > 0 {
		return PanenHasil{Err: fmt.Sprintf("%s %s belum matang — %s lagi.", crop.Emoji, crop.Name, FormatDurasi(sisa))}
	}

	jumlah := randInt(crop.Yield[0], crop.Yield[1])
	total := invAdd(rec, p.Crop, jumlah)
	rec.Plots[plotIndex] = Plot{}
	saveFarming(data)

	return PanenHasil{OK: true, Crop: crop, Jumlah: jumlah, Total: total, Plot: plotIndex + 1}
}

// PanenItem: satu baris hasil PanenSemua.
type PanenItem struct {
	Crop   Crop
	Jumlah int
	Plot   int
}

// PanenSemuaHasil: hasil PanenSemua.
type PanenSemuaHasil struct {
	OK    bool
	Err   string
	Hasil []PanenItem
}

// PanenSemua memanen semua lahan yang sudah matang sekaligus.
func PanenSemua(evt *events.Message) PanenSemuaHasil {
	mu.Lock()
	defer mu.Unlock()
	data := loadFarming()
	_, rec := farmRecOf(evt, data)
	if rec == nil {
		return PanenSemuaHasil{Err: "Data user tidak terbaca."}
	}
	now := nowMS()
	var hasil []PanenItem
	for i, p := range rec.Plots {
		crop, ok := Crops[p.Crop]
		if p.Crop == "" || !ok || now-p.PlantedAt < crop.GrowTime {
			continue
		}
		jumlah := randInt(crop.Yield[0], crop.Yield[1])
		invAdd(rec, p.Crop, jumlah)
		rec.Plots[i] = Plot{}
		hasil = append(hasil, PanenItem{Crop: crop, Jumlah: jumlah, Plot: i + 1})
	}
	if len(hasil) == 0 {
		return PanenSemuaHasil{Err: "Belum ada tanaman yang matang."}
	}
	saveFarming(data)
	return PanenSemuaHasil{OK: true, Hasil: hasil}
}

// BeliBibitHasil: hasil BeliBibit.
type BeliBibitHasil struct {
	OK     bool
	Err    string
	Crop   Crop
	Jumlah int
	Total  int
	Coins  int
}

// BeliBibit membeli stok bibit (disimpan sebagai "bibit:<id>" di inventory).
func BeliBibit(evt *events.Message, cropID string, jumlah int) BeliBibitHasil {
	mu.Lock()
	defer mu.Unlock()
	data := loadFarming()
	_, rec := farmRecOf(evt, data)
	if rec == nil {
		return BeliBibitHasil{Err: "Data user tidak terbaca."}
	}
	crop, ok := Crops[cropID]
	if !ok {
		return BeliBibitHasil{Err: fmt.Sprintf("Bibit *%s* tidak ada di katalog.", cropID)}
	}
	if jumlah < 1 {
		return BeliBibitHasil{Err: "Jumlah minimal 1."}
	}
	total := crop.Seed * jumlah
	if rec.Coins < total {
		return BeliBibitHasil{Err: fmt.Sprintf("Koin kurang. Butuh %d, kamu punya %d.", total, rec.Coins)}
	}

	coinAdd(rec, -total)
	invAdd(rec, "bibit:"+cropID, jumlah)
	saveFarming(data)

	return BeliBibitHasil{OK: true, Crop: crop, Jumlah: jumlah, Total: total, Coins: rec.Coins}
}

// JualHasil: hasil JualTani.
type JualHasil struct {
	OK     bool
	Err    string
	Crop   Crop
	Jumlah int
	Dapat  int
	Coins  int
}

// JualTani menjual hasil tani. jumlah<0 berarti "all".
func JualTani(evt *events.Message, cropID string, jumlah int) JualHasil {
	mu.Lock()
	defer mu.Unlock()
	data := loadFarming()
	_, rec := farmRecOf(evt, data)
	if rec == nil {
		return JualHasil{Err: "Data user tidak terbaca."}
	}
	crop, ok := Crops[cropID]
	if !ok {
		return JualHasil{Err: fmt.Sprintf("*%s* bukan hasil tani.", cropID)}
	}
	punya := invCount(rec, cropID)
	n := jumlah
	if jumlah < 0 { // all
		n = punya
	}
	if n < 1 {
		return JualHasil{Err: "Jumlah minimal 1."}
	}
	if punya < n {
		return JualHasil{Err: fmt.Sprintf("Stok %s cuma %d.", crop.Name, punya)}
	}

	invTake(rec, cropID, n)
	dapat := crop.Sell * n
	coinAdd(rec, dapat)
	saveFarming(data)

	return JualHasil{OK: true, Crop: crop, Jumlah: n, Dapat: dapat, Coins: rec.Coins}
}

// BeliLahanHasil: hasil BeliLahan.
type BeliLahanHasil struct {
	OK    bool
	Err   string
	Harga int
	Total int
	Coins int
}

// BeliLahan menambah satu lahan baru (harga progresif).
func BeliLahan(evt *events.Message) BeliLahanHasil {
	mu.Lock()
	defer mu.Unlock()
	data := loadFarming()
	_, rec := farmRecOf(evt, data)
	if rec == nil {
		return BeliLahanHasil{Err: "Data user tidak terbaca."}
	}
	jumlah := len(rec.Plots)
	if jumlah >= PlotMax {
		return BeliLahanHasil{Err: fmt.Sprintf("Lahan sudah maksimal (%d).", PlotMax)}
	}
	harga := HargaLahan(jumlah)
	if rec.Coins < harga {
		return BeliLahanHasil{Err: fmt.Sprintf("Koin kurang. Lahan ke-%d harga %d, kamu punya %d.", jumlah+1, harga, rec.Coins)}
	}

	coinAdd(rec, -harga)
	rec.Plots = append(rec.Plots, Plot{})
	saveFarming(data)

	return BeliLahanHasil{OK: true, Harga: harga, Total: len(rec.Plots), Coins: rec.Coins}
}

// StokOf mengembalikan jumlah item di inventory user (0 bila tak ada).
func StokOf(evt *events.Message, itemID string) int {
	mu.Lock()
	defer mu.Unlock()
	data := loadFarming()
	_, rec := farmRecOf(evt, data)
	if rec == nil {
		return 0
	}
	return invCount(rec, itemID)
}
