package econ

import (
	"fmt"

	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/store"
)

// ternak.go: katalog hewan + operasi beternak. Port lib/ternak.js. Store "ternak"
// hanya menyimpan array hewan; koin & inventory (pakan/produk/daging) tetap di
// store "farming" dan dimanipulasi lewat helper inv*/coin* pada *FarmRec.

// HewanDef mendeskripsikan satu jenis hewan ternak.
//
//	Pakan   : item hasil tani yang dimakan hewan (rantai tani → ternak)
//	Kenyang : lama sekali makan bertahan (ms); lewat ini hewan lapar & stop produksi
//	Siklus  : jeda antar panen produk (ms)
//	Dewasa  : umur minimum sebelum boleh dipotong jadi daging (ms)
type HewanDef struct {
	Name     string
	Emoji    string
	Harga    int
	Pakan    string
	Porsi    int
	Kenyang  int64
	Siklus   int64
	Dewasa   int64
	ProdukID string
	ProdukQ  [2]int
	DagingID string
	DagingQ  [2]int
}

// Hewan adalah katalog hewan ternak.
var Hewan = map[string]HewanDef{
	"ayam":    {Name: "Ayam", Emoji: "🐔", Harga: 120, Pakan: "jagung", Porsi: 1, Kenyang: 4 * 60 * 60 * 1000, Siklus: 45 * 60 * 1000, Dewasa: 3 * 60 * 60 * 1000, ProdukID: "telur", ProdukQ: [2]int{1, 3}, DagingID: "daging_ayam", DagingQ: [2]int{2, 3}},
	"bebek":   {Name: "Bebek", Emoji: "🦆", Harga: 180, Pakan: "bayam", Porsi: 2, Kenyang: 5 * 60 * 60 * 1000, Siklus: 60 * 60 * 1000, Dewasa: 4 * 60 * 60 * 1000, ProdukID: "telur", ProdukQ: [2]int{2, 3}, DagingID: "daging_bebek", DagingQ: [2]int{2, 4}},
	"kambing": {Name: "Kambing", Emoji: "🐐", Harga: 400, Pakan: "bayam", Porsi: 3, Kenyang: 8 * 60 * 60 * 1000, Siklus: 2 * 60 * 60 * 1000, Dewasa: 8 * 60 * 60 * 1000, ProdukID: "susu", ProdukQ: [2]int{1, 2}, DagingID: "daging_kambing", DagingQ: [2]int{3, 5}},
	"sapi":    {Name: "Sapi", Emoji: "🐄", Harga: 900, Pakan: "jagung", Porsi: 4, Kenyang: 10 * 60 * 60 * 1000, Siklus: 3 * 60 * 60 * 1000, Dewasa: 12 * 60 * 60 * 1000, ProdukID: "susu", ProdukQ: [2]int{2, 4}, DagingID: "daging_sapi", DagingQ: [2]int{5, 8}},
}

// ProdukDef mendeskripsikan produk hewan (telur/susu/daging).
type ProdukDef struct {
	Name          string
	Emoji         string
	Sell          int
	EnergyRestore int
}

// Produk adalah katalog produk ternak.
var Produk = map[string]ProdukDef{
	"telur":          {Name: "Telur", Emoji: "🥚", Sell: 20, EnergyRestore: 4},
	"susu":           {Name: "Susu", Emoji: "🥛", Sell: 35, EnergyRestore: 6},
	"daging_ayam":    {Name: "Daging Ayam", Emoji: "🍗", Sell: 60, EnergyRestore: 9},
	"daging_bebek":   {Name: "Daging Bebek", Emoji: "🍖", Sell: 75, EnergyRestore: 10},
	"daging_kambing": {Name: "Daging Kambing", Emoji: "🥩", Sell: 130, EnergyRestore: 14},
	"daging_sapi":    {Name: "Daging Sapi", Emoji: "🥩", Sell: 180, EnergyRestore: 18},
}

const KandangMax = 15

// HewanOrder & ProdukOrder menjaga urutan tampilan katalog (map Go tak berurut).
var HewanOrder = []string{"ayam", "bebek", "kambing", "sapi"}
var ProdukOrder = []string{"telur", "susu", "daging_ayam", "daging_bebek", "daging_kambing", "daging_sapi"}

// Ternak adalah satu ekor hewan milik user (record store "ternak").
type Ternak struct {
	ID            string `json:"id"`
	Jenis         string `json:"jenis"`
	Sejak         int64  `json:"sejak"`
	KenyangSampai int64  `json:"kenyangSampai"`
	PanenBerikut  int64  `json:"panenBerikut"`
}

// TernakRec adalah isi kandang seorang user.
type TernakRec struct {
	Hewan []Ternak `json:"hewan"`
}

// FormatDurasiJM memformat sisa waktu jadi "Xj Ym" atau "Xm" (varian ternak.js,
// tanpa detik).
func FormatDurasiJM(ms int64) string {
	if ms <= 0 {
		return "siap"
	}
	j := ms / 3600000
	m := (ms % 3600000) / 60000
	if j > 0 {
		return fmt.Sprintf("%dj %dm", j, m)
	}
	return fmt.Sprintf("%dm", m)
}

func loadTernak() map[string]*TernakRec {
	data := map[string]*TernakRec{}
	_, _ = store.Get(keyTernak, &data)
	if data == nil {
		data = map[string]*TernakRec{}
	}
	return data
}

func saveTernak(data map[string]*TernakRec) { _ = store.Set(keyTernak, data) }

// recsOf memuat record farming (dompet/inventory) & ternak (kandang) untuk user
// dengan kunci kanonik yang sama (kunci diambil dari store farming, seperti Node).
// Tidak mengunci — pemanggil memegang mu.
func recsOf(evt *events.Message, fdata map[string]*FarmRec, tdata map[string]*TernakRec) (string, *FarmRec, *TernakRec) {
	key, frec := farmRecOf(evt, fdata)
	if key == "" {
		return "", nil, nil
	}
	if tdata[key] == nil {
		tdata[key] = &TernakRec{Hewan: []Ternak{}}
	}
	return key, frec, tdata[key]
}

// HewanView adalah status satu hewan yang sudah dihitung.
type HewanView struct {
	ID          string
	Jenis       string
	Nama        string
	Emoji       string
	Lapar       bool
	Dewasa      bool
	SiapPanen   bool
	SisaDewasa  int64
	SisaPanen   int64
	SisaKenyang int64
	ProdukID    string
}

func statusHewan(h Ternak, now int64) (HewanView, bool) {
	def, ok := Hewan[h.Jenis]
	if !ok {
		return HewanView{}, false
	}
	umur := now - h.Sejak
	lapar := now >= h.KenyangSampai
	sisaPanen := h.PanenBerikut - now
	if sisaPanen < 0 {
		sisaPanen = 0
	}
	sisaDewasa := def.Dewasa - umur
	if sisaDewasa < 0 {
		sisaDewasa = 0
	}
	sisaKenyang := h.KenyangSampai - now
	if sisaKenyang < 0 {
		sisaKenyang = 0
	}
	return HewanView{
		ID: h.ID, Jenis: h.Jenis, Nama: def.Name, Emoji: def.Emoji,
		Lapar: lapar, Dewasa: umur >= def.Dewasa,
		SiapPanen: !lapar && sisaPanen == 0, SisaPanen: sisaPanen,
		SisaDewasa: sisaDewasa, SisaKenyang: sisaKenyang, ProdukID: def.ProdukID,
	}, true
}

// KandangView adalah status kandang + kapasitas + dompet.
type KandangView struct {
	Hewan     []HewanView
	Kapasitas int
	Coins     int
}

// LihatKandang mengembalikan status semua hewan user (nil bila id tak terbaca).
func LihatKandang(evt *events.Message) *KandangView {
	mu.Lock()
	defer mu.Unlock()
	fdata, tdata := loadFarming(), loadTernak()
	_, frec, trec := recsOf(evt, fdata, tdata)
	if frec == nil {
		return nil
	}
	now := nowMS()
	views := make([]HewanView, 0, len(trec.Hewan))
	for _, h := range trec.Hewan {
		if v, ok := statusHewan(h, now); ok {
			views = append(views, v)
		}
	}
	saveFarming(fdata)
	saveTernak(tdata)
	return &KandangView{Hewan: views, Kapasitas: KandangMax, Coins: frec.Coins}
}

// BeliHewanHasil: hasil BeliHewan.
type BeliHewanHasil struct {
	OK     bool
	Err    string
	Def    HewanDef
	Jumlah int
	Total  int
	Coins  int
	Isi    int
}

// BeliHewan membeli jumlah ekor hewan jenis tertentu ke kandang.
func BeliHewan(evt *events.Message, jenis string, jumlah int) BeliHewanHasil {
	mu.Lock()
	defer mu.Unlock()
	fdata, tdata := loadFarming(), loadTernak()
	key, frec, trec := recsOf(evt, fdata, tdata)
	if key == "" {
		return BeliHewanHasil{Err: "Data user tidak terbaca."}
	}
	def, ok := Hewan[jenis]
	if !ok {
		return BeliHewanHasil{Err: fmt.Sprintf("Hewan *%s* tidak dijual.", jenis)}
	}
	if jumlah < 1 {
		return BeliHewanHasil{Err: "Jumlah minimal 1."}
	}
	if len(trec.Hewan)+jumlah > KandangMax {
		return BeliHewanHasil{Err: fmt.Sprintf("Kandang penuh. Sisa slot: %d/%d.", KandangMax-len(trec.Hewan), KandangMax)}
	}
	total := def.Harga * jumlah
	if frec.Coins < total {
		return BeliHewanHasil{Err: fmt.Sprintf("Koin kurang. %d %s = %d koin, kamu punya %d.", jumlah, def.Name, total, frec.Coins)}
	}

	now := nowMS()
	for n := 0; n < jumlah; n++ {
		trec.Hewan = append(trec.Hewan, Ternak{
			ID:            fmt.Sprintf("%s-%d-%d", jenis, now, len(trec.Hewan)),
			Jenis:         jenis,
			Sejak:         now,
			KenyangSampai: now + def.Kenyang, // hewan baru datang sudah kenyang
			PanenBerikut:  now + def.Siklus,
		})
	}
	coins := coinAdd(frec, -total)
	saveTernak(tdata)
	saveFarming(fdata)

	return BeliHewanHasil{OK: true, Def: def, Jumlah: jumlah, Total: total, Coins: coins, Isi: len(trec.Hewan)}
}

// PakanKurang mencatat kekurangan satu jenis pakan.
type PakanKurang struct {
	Item  string
	Butuh int
	Punya int
}

// BeriPakanHasil: hasil BeriPakan. Kurang != nil berarti pakan tak cukup.
type BeriPakanHasil struct {
	OK     bool
	Err    string
	Jumlah int            // hewan yang diberi pakan
	Butuh  map[string]int // pakan terpakai per item
	Kurang []PakanKurang
}

// BeriPakan memberi pakan semua hewan lapar; pakan diambil dari hasil bertani.
func BeriPakan(evt *events.Message) BeriPakanHasil {
	mu.Lock()
	defer mu.Unlock()
	fdata, tdata := loadFarming(), loadTernak()
	key, frec, trec := recsOf(evt, fdata, tdata)
	if key == "" {
		return BeriPakanHasil{Err: "Data user tidak terbaca."}
	}
	if len(trec.Hewan) == 0 {
		return BeriPakanHasil{Err: "Kandang masih kosong."}
	}
	now := nowMS()

	// Kumpulkan hewan lapar + hitung total kebutuhan pakan per item.
	var lapar []*Ternak
	butuh := map[string]int{}
	for i := range trec.Hewan {
		h := &trec.Hewan[i]
		def, ok := Hewan[h.Jenis]
		if !ok || now < h.KenyangSampai {
			continue
		}
		lapar = append(lapar, h)
		butuh[def.Pakan] += def.Porsi
	}
	if len(lapar) == 0 {
		return BeriPakanHasil{Err: "Semua hewan masih kenyang."}
	}

	// Cek kecukupan stok sebelum memotong apa pun.
	var kurang []PakanKurang
	for item, n := range butuh {
		if punya := invCount(frec, item); punya < n {
			kurang = append(kurang, PakanKurang{Item: item, Butuh: n, Punya: punya})
		}
	}
	if len(kurang) > 0 {
		return BeriPakanHasil{Err: "pakan-kurang", Kurang: kurang, Butuh: butuh}
	}

	for item, n := range butuh {
		invTake(frec, item, n)
	}
	for _, h := range lapar {
		def := Hewan[h.Jenis]
		h.KenyangSampai = now + def.Kenyang
		if h.PanenBerikut < now {
			h.PanenBerikut = now + def.Siklus
		}
	}
	saveTernak(tdata)
	saveFarming(fdata)

	return BeriPakanHasil{OK: true, Jumlah: len(lapar), Butuh: butuh}
}

// PanenProdukHasil: hasil PanenProduk.
type PanenProdukHasil struct {
	OK    bool
	Err   string
	Hasil map[string]int
	Lapar int
}

// PanenProduk mengambil telur/susu dari hewan yang siklusnya jalan & tidak lapar.
func PanenProduk(evt *events.Message) PanenProdukHasil {
	mu.Lock()
	defer mu.Unlock()
	fdata, tdata := loadFarming(), loadTernak()
	key, frec, trec := recsOf(evt, fdata, tdata)
	if key == "" {
		return PanenProdukHasil{Err: "Data user tidak terbaca."}
	}
	now := nowMS()
	hasil := map[string]int{}
	lapar := 0
	for i := range trec.Hewan {
		h := &trec.Hewan[i]
		def, ok := Hewan[h.Jenis]
		if !ok {
			continue
		}
		if now >= h.KenyangSampai {
			lapar++
			continue
		}
		if now < h.PanenBerikut {
			continue
		}
		hasil[def.ProdukID] += randInt(def.ProdukQ[0], def.ProdukQ[1])
		h.PanenBerikut = now + def.Siklus
	}
	if len(hasil) == 0 {
		err := "Belum ada produk yang siap dipanen."
		if lapar > 0 {
			err = fmt.Sprintf("Belum ada yang bisa dipanen — %d hewan lapar, beri pakan dulu.", lapar)
		}
		return PanenProdukHasil{Err: err}
	}
	for item, n := range hasil {
		invAdd(frec, item, n)
	}
	saveTernak(tdata)
	saveFarming(fdata)

	return PanenProdukHasil{OK: true, Hasil: hasil, Lapar: lapar}
}

// PotongHasil: hasil PotongHewan.
type PotongHasil struct {
	OK     bool
	Err    string
	Def    HewanDef
	Jumlah int
	Item   ProdukDef
	Sisa   int
}

// PotongHewan memotong satu hewan dewasa → daging masuk inventory.
func PotongHewan(evt *events.Message, jenis string) PotongHasil {
	mu.Lock()
	defer mu.Unlock()
	fdata, tdata := loadFarming(), loadTernak()
	key, frec, trec := recsOf(evt, fdata, tdata)
	if key == "" {
		return PotongHasil{Err: "Data user tidak terbaca."}
	}
	def, ok := Hewan[jenis]
	if !ok {
		return PotongHasil{Err: fmt.Sprintf("Hewan *%s* tidak dikenal.", jenis)}
	}
	now := nowMS()
	idx := -1
	punyaMuda := false
	for i, h := range trec.Hewan {
		if h.Jenis != jenis {
			continue
		}
		punyaMuda = true
		if now-h.Sejak >= def.Dewasa {
			idx = i
			break
		}
	}
	if idx == -1 {
		if punyaMuda {
			return PotongHasil{Err: fmt.Sprintf("%s %s kamu belum cukup umur untuk dipotong.", def.Emoji, def.Name)}
		}
		return PotongHasil{Err: fmt.Sprintf("Kamu tidak punya %s.", def.Name)}
	}

	jumlah := randInt(def.DagingQ[0], def.DagingQ[1])
	trec.Hewan = append(trec.Hewan[:idx], trec.Hewan[idx+1:]...)
	invAdd(frec, def.DagingID, jumlah)
	saveTernak(tdata)
	saveFarming(fdata)

	sisa := 0
	for _, h := range trec.Hewan {
		if h.Jenis == jenis {
			sisa++
		}
	}
	return PotongHasil{OK: true, Def: def, Jumlah: jumlah, Item: Produk[def.DagingID], Sisa: sisa}
}

// JualProdukHasil: hasil JualProduk.
type JualProdukHasil struct {
	OK     bool
	Err    string
	Item   ProdukDef
	Jumlah int
	Dapat  int
	Coins  int
}

// JualProduk menjual produk ternak (telur/susu/daging). jumlah<0 berarti "all".
func JualProduk(evt *events.Message, itemID string, jumlah int) JualProdukHasil {
	mu.Lock()
	defer mu.Unlock()
	fdata := loadFarming()
	_, frec := farmRecOf(evt, fdata)
	if frec == nil {
		return JualProdukHasil{Err: "Data user tidak terbaca."}
	}
	item, ok := Produk[itemID]
	if !ok {
		return JualProdukHasil{Err: fmt.Sprintf("*%s* bukan produk ternak.", itemID)}
	}
	punya := invCount(frec, itemID)
	n := jumlah
	if jumlah < 0 { // all
		n = punya
	}
	if n < 1 {
		return JualProdukHasil{Err: "Jumlah minimal 1."}
	}
	if punya < n {
		return JualProdukHasil{Err: fmt.Sprintf("Stok %s cuma %d.", item.Name, punya)}
	}

	invTake(frec, itemID, n)
	dapat := item.Sell * n
	coins := coinAdd(frec, dapat)
	saveFarming(fdata)

	return JualProdukHasil{OK: true, Item: item, Jumlah: n, Dapat: dapat, Coins: coins}
}
