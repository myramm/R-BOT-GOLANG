package econ

import (
	"fmt"
	"math"

	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/config"
	"rbot/brain/energy"
	"rbot/brain/store"
)

// rest.go: isi ulang energi lewat istirahat/tidur. Port lib/rest.js. Store "rest"
// menyimpan timestamp terakhir tiap mode; cooldown-nya yang bikin energi tetap
// berharga (tanpa itu user bisa spam sampai penuh). Pengisian energi didelegasikan
// ke paket energy (energy.Restore), jadi rest cuma mengatur cooldown.

// ModeDef mendeskripsikan satu mode istirahat.
type ModeDef struct {
	ID       string
	Nama     string
	Emoji    string
	Porsi    float64 // fraksi plafon harian yang dipulihkan (dibulatkan ke atas, min 1)
	Cooldown int64   // ms
	Field    string  // nama field timestamp di record rest
}

// Modes adalah katalog mode istirahat.
var Modes = map[string]ModeDef{
	"istirahat": {ID: "istirahat", Nama: "Istirahat singkat", Emoji: "😌", Porsi: 0.4, Cooldown: 30 * 60 * 1000, Field: "lastIstirahat"},
	"tidur":     {ID: "tidur", Nama: "Tidur", Emoji: "😴", Porsi: 1, Cooldown: 3 * 60 * 60 * 1000, Field: "lastTidur"},
}

// ModeOrder menjaga urutan tampilan (iterasi map Go tak berurut).
var ModeOrder = []string{"istirahat", "tidur"}

// restRec adalah timestamp istirahat/tidur terakhir seorang user.
type restRec struct {
	LastIstirahat int64 `json:"lastIstirahat,omitempty"`
	LastTidur     int64 `json:"lastTidur,omitempty"`
}

func (r *restRec) get(field string) int64 {
	if field == "lastTidur" {
		return r.LastTidur
	}
	return r.LastIstirahat
}

func (r *restRec) set(field string, v int64) {
	if field == "lastTidur" {
		r.LastTidur = v
		return
	}
	r.LastIstirahat = v
}

func loadRest() map[string]*restRec {
	data := map[string]*restRec{}
	_, _ = store.Get(keyRest, &data)
	if data == nil {
		data = map[string]*restRec{}
	}
	return data
}

func saveRest(data map[string]*restRec) { _ = store.Set(keyRest, data) }

// restRecOf mengambil record rest kanonik user (kunci sendiri seperti Node
// recordOf: id pertama yang sudah punya data, atau id pertama). Tidak mengunci.
func restRecOf(evt *events.Message, data map[string]*restRec) (string, *restRec) {
	ids := idsOf(evt)
	key := canonKey(ids, func(id string) bool { return data[id] != nil })
	if key == "" {
		return "", nil
	}
	if data[key] == nil {
		data[key] = &restRec{}
	}
	return key, data[key]
}

// RestStatus adalah sisa cooldown satu mode untuk ditampilkan.
type RestStatus struct {
	ModeDef
	Siap bool
	Sisa int64
}

// StatusRest mengembalikan sisa cooldown tiap mode (terurut), nil bila id tak
// terbaca.
func StatusRest(evt *events.Message) []RestStatus {
	mu.Lock()
	defer mu.Unlock()
	data := loadRest()
	key, rec := restRecOf(evt, data)
	if key == "" {
		return nil
	}
	now := nowMS()
	out := make([]RestStatus, 0, len(ModeOrder))
	for _, id := range ModeOrder {
		mode := Modes[id]
		sisa := rec.get(mode.Field) + mode.Cooldown - now
		if sisa < 0 {
			sisa = 0
		}
		out = append(out, RestStatus{ModeDef: mode, Siap: sisa == 0, Sisa: sisa})
	}
	return out
}

// IstirahatHasil: hasil Istirahat. Err=="cooldown" berarti belum boleh (Sisa terisi).
type IstirahatHasil struct {
	OK              bool
	Err             string
	Mode            ModeDef
	Sisa            int64 // sisa cooldown (saat Err=="cooldown")
	Cooldown        int64
	Penuh           bool
	Unlimited       bool
	Tambah          int
	Terbuang        int
	Bank            int
	Plafon          int
	PlafonTakHingga bool
}

// Istirahat menjalankan satu mode istirahat: isi energi + pasang cooldown.
func Istirahat(evt *events.Message, modeID string) IstirahatHasil {
	mode, ok := Modes[modeID]
	if !ok {
		return IstirahatHasil{Err: fmt.Sprintf("Mode istirahat *%s* tidak dikenal.", modeID)}
	}

	mu.Lock()
	defer mu.Unlock()
	data := loadRest()
	key, rec := restRecOf(evt, data)
	if key == "" {
		return IstirahatHasil{Err: "Data user tidak terbaca."}
	}
	now := nowMS()
	if siapPada := rec.get(mode.Field) + mode.Cooldown; now < siapPada {
		return IstirahatHasil{Err: "cooldown", Mode: mode, Sisa: siapPada - now}
	}

	limit := config.C.Energy.DailyLimit()
	jumlah := int(math.Max(1, math.Ceil(float64(limit)*mode.Porsi)))
	hasil := energy.Restore(evt, jumlah)
	if !hasil.OK {
		errMsg := hasil.Err
		if errMsg == "" {
			errMsg = "Gagal mengisi energi."
		}
		return IstirahatHasil{Err: errMsg}
	}

	// Kalau energi sudah penuh (atau unlimited), jangan bakar cooldown-nya.
	if hasil.Unlimited || hasil.Penuh {
		return IstirahatHasil{OK: true, Mode: mode, Penuh: true, Unlimited: hasil.Unlimited, Tambah: hasil.Tambah, Terbuang: hasil.Terbuang, Bank: hasil.Bank}
	}

	rec.set(mode.Field, now)
	saveRest(data)

	return IstirahatHasil{OK: true, Mode: mode, Cooldown: mode.Cooldown, Tambah: hasil.Tambah, Terbuang: hasil.Terbuang, Bank: hasil.Bank, Plafon: hasil.Plafon, PlafonTakHingga: hasil.PlafonTakHingga}
}
