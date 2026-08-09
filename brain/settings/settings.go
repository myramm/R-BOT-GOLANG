// Package settings menyimpan pengaturan bot yang bisa diubah saat runtime lewat
// command (mis. .set autoread on). Berbeda dari config (read-only dari config.json),
// setting di sini persisten di store (bbolt) dan di-cache di memori supaya cepat
// dibaca di jalur pesan masuk.
package settings

import (
	"sync"

	"rbot/brain/store"
)

const storeKey = "settings"

// data adalah bentuk tersimpan di store (JSON). Tambah field baru di sini saat
// menambah setting runtime lain.
type data struct {
	AutoRead bool `json:"autoRead"`
}

var (
	mu     sync.RWMutex
	cache  data
	loaded bool
)

// Load membaca setting dari store ke cache. Aman dipanggil sekali saat start;
// bila key belum ada, cache tetap nilai default (semua false).
func Load() error {
	mu.Lock()
	defer mu.Unlock()
	var d data
	if err := store.GetOr(storeKey, &d); err != nil {
		return err
	}
	cache = d
	loaded = true
	return nil
}

// AutoRead mengembalikan status autoread (default false bila belum di-Load).
func AutoRead() bool {
	mu.RLock()
	defer mu.RUnlock()
	return cache.AutoRead
}

// SetAutoRead mengubah status autoread lalu mem-persist ke store.
func SetAutoRead(on bool) error {
	mu.Lock()
	cache.AutoRead = on
	snapshot := cache
	mu.Unlock()
	return store.Set(storeKey, snapshot)
}
