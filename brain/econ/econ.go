// Package econ adalah layer ekonomi/game bot: bertani, beternak, inventory,
// koin, serta istirahat & makan untuk isi ulang energi. Port dari lib/farming.js,
// lib/ternak.js, lib/rest.js, lib/consume.js.
//
// Store LMDB (via brain/store), tiga key JSON:
//
//	"farming" -> { bareId: { plots:[{crop,plantedAt}], inv:{item:n}, coins } }
//	"ternak"  -> { bareId: { hewan:[{id,jenis,sejak,kenyangSampai,panenBerikut}] } }
//	"rest"    -> { bareId: { lastIstirahat, lastTidur } }
//
// Kunci kanonik user disamakan dengan brain/energy & brain/identity: satu user
// bisa punya beberapa id (LID/PN); dipakai id pertama yang sudah punya data.
// Inventory & koin tinggal di store "farming"; ternak.go memanipulasinya lewat
// helper inv*/coin* pada *FarmRec yang sama supaya dompet tak terpecah.
package econ

import (
	"math/rand"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/identity"
	"rbot/brain/store"
)

// mu menjaga seluruh operasi read-modify-write econ tetap atomik (whatsmeow bisa
// mengantar event dari banyak chat paralel). Tidak reentrant: fungsi terekspor
// mengunci sekali, helper internal mengasumsikan lock sudah dipegang.
var mu sync.Mutex

const (
	keyFarming = "farming"
	keyTernak  = "ternak"
	keyRest    = "rest"

	coinAwal = 150 // koin awal user baru
)

// nowMS mengembalikan waktu sekarang dalam milidetik epoch (setara Date.now()).
func nowMS() int64 { return time.Now().UnixMilli() }

// randInt mengembalikan bilangan acak inklusif [min,max] (setara randInt Node).
func randInt(min, max int) int {
	if max <= min {
		return min
	}
	return min + rand.Intn(max-min+1)
}

// === Kunci kanonik ===

// canonKey memilih id kanonik: id pertama yang sudah punya data, atau id pertama.
func canonKey(ids []string, punya func(id string) bool) string {
	if len(ids) == 0 {
		return ""
	}
	for _, id := range ids {
		if punya(id) {
			return id
		}
	}
	return ids[0]
}

func idsOf(evt *events.Message) []string { return identity.Candidates(evt) }

// === Store farming (dompet + inventory + lahan) ===

type Plot struct {
	Crop      string `json:"crop,omitempty"`
	PlantedAt int64  `json:"plantedAt,omitempty"`
}

type FarmRec struct {
	Plots []Plot         `json:"plots"`
	Inv   map[string]int `json:"inv"`
	Coins int            `json:"coins"`
}

func loadFarming() map[string]*FarmRec {
	data := map[string]*FarmRec{}
	_, _ = store.Get(keyFarming, &data)
	if data == nil {
		data = map[string]*FarmRec{}
	}
	return data
}

func saveFarming(data map[string]*FarmRec) { _ = store.Set(keyFarming, data) }

func blankFarm() *FarmRec {
	return &FarmRec{
		Plots: make([]Plot, PlotAwal),
		Inv:   map[string]int{},
		Coins: coinAwal,
	}
}

// normFarm memastikan field record valid (plots ada, inv non-nil) — setara
// pembenahan di accountOf Node saat WA mengganti format jid.
func normFarm(rec *FarmRec) {
	if len(rec.Plots) == 0 {
		rec.Plots = make([]Plot, PlotAwal)
	}
	if rec.Inv == nil {
		rec.Inv = map[string]int{}
	}
}

// farmRecOf mengambil (key, rec) farming untuk evt, membuat blank bila belum ada.
// Tidak mengunci — pemanggil (fungsi terekspor) yang memegang mu.
func farmRecOf(evt *events.Message, data map[string]*FarmRec) (string, *FarmRec) {
	ids := idsOf(evt)
	key := canonKey(ids, func(id string) bool { return data[id] != nil })
	if key == "" {
		return "", nil
	}
	if data[key] == nil {
		data[key] = blankFarm()
	}
	normFarm(data[key])
	return key, data[key]
}

// === Inventory & koin (beroperasi in-memory di *FarmRec) ===

func invCount(rec *FarmRec, item string) int { return rec.Inv[item] }

func invAdd(rec *FarmRec, item string, n int) int {
	rec.Inv[item] += n
	return rec.Inv[item]
}

// invTake mengambil n item; false bila stok kurang.
func invTake(rec *FarmRec, item string, n int) bool {
	if rec.Inv[item] < n {
		return false
	}
	rec.Inv[item] -= n
	if rec.Inv[item] <= 0 {
		delete(rec.Inv, item)
	}
	return true
}

// coinAdd menambah/mengurangi koin (tidak pernah di bawah 0).
func coinAdd(rec *FarmRec, delta int) int {
	rec.Coins += delta
	if rec.Coins < 0 {
		rec.Coins = 0
	}
	return rec.Coins
}

// Inventori adalah ringkasan dompet+inventory untuk command inventory/toko.
type Inventori struct {
	Inv   map[string]int
	Coins int
}

// InventoriOf mengembalikan salinan inventory & koin user (nil bila id tak terbaca).
func InventoriOf(evt *events.Message) *Inventori {
	mu.Lock()
	defer mu.Unlock()
	data := loadFarming()
	_, rec := farmRecOf(evt, data)
	if rec == nil {
		return nil
	}
	saveFarming(data)
	inv := make(map[string]int, len(rec.Inv))
	for k, v := range rec.Inv {
		inv[k] = v
	}
	return &Inventori{Inv: inv, Coins: rec.Coins}
}
