package econ

import (
	"fmt"
	"sort"

	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/energy"
)

// consume.go: makan hasil tani/ternak untuk isi energi. Port lib/consume.js.
// MAKANAN = gabungan katalog tani (Crops) & produk ternak (Produk); item dipotong
// dari inventory farming, energi diisi lewat paket energy.

// MakananDef adalah pandangan seragam item yang bisa dimakan (dari Crop/ProdukDef).
type MakananDef struct {
	ID            string
	Name          string
	Emoji         string
	EnergyRestore int
	Sell          int
}

// Makanan adalah katalog gabungan tani + ternak, dibangun sekali saat init.
var Makanan = map[string]MakananDef{}

func init() {
	for id, c := range Crops {
		Makanan[id] = MakananDef{ID: id, Name: c.Name, Emoji: c.Emoji, EnergyRestore: c.EnergyRestore, Sell: c.Sell}
	}
	for id, p := range Produk {
		Makanan[id] = MakananDef{ID: id, Name: p.Name, Emoji: p.Emoji, EnergyRestore: p.EnergyRestore, Sell: p.Sell}
	}
}

// BisaDimakan true bila item ada di katalog & memulihkan energi.
func BisaDimakan(itemID string) bool {
	item, ok := Makanan[itemID]
	return ok && item.EnergyRestore > 0
}

// ambilStok memotong n item dari inventory farming (locked). false bila kurang.
func ambilStok(evt *events.Message, itemID string, n int) bool {
	mu.Lock()
	defer mu.Unlock()
	data := loadFarming()
	_, rec := farmRecOf(evt, data)
	if rec == nil {
		return false
	}
	if !invTake(rec, itemID, n) {
		return false
	}
	saveFarming(data)
	return true
}

// MakanHasil: hasil Makan (menyertakan field RestoreResult, seperti spread Node).
type MakanHasil struct {
	OK              bool
	Err             string
	Item            MakananDef
	Jumlah          int
	TotalRestore    int
	Unlimited       bool
	Tambah          int
	Terbuang        int
	Bank            int
	Penuh           bool
	Plafon          int
	PlafonTakHingga bool
}

// Makan memakan n item → restore energi, item berkurang dari inventory.
func Makan(evt *events.Message, itemID string, jumlah int) MakanHasil {
	item, ok := Makanan[itemID]
	if !ok {
		return MakanHasil{Err: fmt.Sprintf("*%s* bukan makanan yang dikenal.", itemID)}
	}
	if item.EnergyRestore <= 0 {
		return MakanHasil{Err: fmt.Sprintf("%s %s tidak bisa dimakan (bukan makanan).", item.Emoji, item.Name)}
	}
	if jumlah < 1 {
		return MakanHasil{Err: "Jumlah minimal 1."}
	}

	punya := StokOf(evt, itemID)
	if punya < jumlah {
		return MakanHasil{Err: fmt.Sprintf("Kamu cuma punya %d %s %s.", punya, item.Emoji, item.Name)}
	}
	if !ambilStok(evt, itemID, jumlah) {
		return MakanHasil{Err: "Stok berubah saat diproses, coba lagi."}
	}

	totalRestore := item.EnergyRestore * jumlah
	hasil := energy.Restore(evt, totalRestore)

	return MakanHasil{
		OK: true, Item: item, Jumlah: jumlah, TotalRestore: totalRestore,
		Unlimited: hasil.Unlimited, Tambah: hasil.Tambah, Terbuang: hasil.Terbuang,
		Bank: hasil.Bank, Penuh: hasil.Penuh,
		Plafon: hasil.Plafon, PlafonTakHingga: hasil.PlafonTakHingga,
	}
}

// MakananView adalah item makanan di inventory user + jumlahnya.
type MakananView struct {
	MakananDef
	Jumlah int
}

// DaftarMakanan mengembalikan item bisa-dimakan di inventory user, terurut dari
// restore energi tertinggi (tie-break: id, supaya deterministik).
func DaftarMakanan(evt *events.Message) []MakananView {
	mu.Lock()
	defer mu.Unlock()
	data := loadFarming()
	_, rec := farmRecOf(evt, data)
	if rec == nil {
		return nil
	}
	var out []MakananView
	for id, def := range Makanan {
		if def.EnergyRestore <= 0 {
			continue
		}
		if q := rec.Inv[id]; q > 0 {
			out = append(out, MakananView{MakananDef: def, Jumlah: q})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].EnergyRestore != out[j].EnergyRestore {
			return out[i].EnergyRestore > out[j].EnergyRestore
		}
		return out[i].ID < out[j].ID
	})
	return out
}
