// Package premium mengelola status premium user. Port dari lib/premium.js.
// Store: key "premium" -> { bareId: expiryMs }.
package premium

import (
	"math"
	"sort"
	"time"

	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/config"
	"rbot/brain/identity"
	"rbot/brain/store"
)

const storeKey = "premium"

func readAll() map[string]int64 {
	data := map[string]int64{}
	_, _ = store.Get(storeKey, &data)
	if data == nil {
		data = map[string]int64{}
	}
	return data
}

func writeAll(data map[string]int64) { _ = store.Set(storeKey, data) }

func nowMs() int64 { return time.Now().UnixMilli() }

// prune membuang entri kadaluarsa; menulis balik bila ada perubahan.
func prune(data map[string]int64) map[string]int64 {
	now := nowMs()
	changed := false
	for id, exp := range data {
		if exp <= 0 || exp <= now {
			delete(data, id)
			changed = true
		}
	}
	if changed {
		writeAll(data)
	}
	return data
}

// IsPremium true bila owner, atau salah satu ID pengirim punya premium aktif.
func IsPremium(evt *events.Message) bool {
	if identity.IsOwner(evt) {
		return true
	}
	data := prune(readAll())
	now := nowMs()
	for _, id := range identity.Candidates(evt) {
		if exp, ok := data[id]; ok && exp > now {
			return true
		}
	}
	return false
}

// Add menambah/memperpanjang premium id sebanyak days hari. Bila masih aktif,
// diperpanjang dari expiry lama; jika tidak, dari sekarang. Return expiry baru.
func Add(id string, days float64) (int64, bool) {
	key := config.BareNumber(id)
	if key == "" {
		return 0, false
	}
	data := readAll()
	now := nowMs()
	base := now
	if exp, ok := data[key]; ok && exp > now {
		base = exp
	}
	data[key] = base + int64(math.Round(days*24*3600*1000))
	writeAll(data)
	return data[key], true
}

// Remove mencabut premium id. Return true bila ada yang dihapus.
func Remove(id string) bool {
	key := config.BareNumber(id)
	data := readAll()
	if _, ok := data[key]; !ok {
		return false
	}
	delete(data, key)
	writeAll(data)
	return true
}

// Entry adalah satu baris daftar premium.
type Entry struct {
	ID     string
	Expiry int64
}

// List mengembalikan premium aktif terurut naik berdasarkan expiry.
func List() []Entry {
	data := prune(readAll())
	out := make([]Entry, 0, len(data))
	for id, exp := range data {
		out = append(out, Entry{ID: id, Expiry: exp})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Expiry < out[j].Expiry })
	return out
}

// Remaining mengembalikan sisa durasi premium terpanjang (ms) untuk pengirim.
func Remaining(evt *events.Message) int64 {
	data := prune(readAll())
	now := nowMs()
	var max int64
	for _, id := range identity.Candidates(evt) {
		if exp, ok := data[id]; ok && exp-now > max {
			max = exp - now
		}
	}
	return max
}
