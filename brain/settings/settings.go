// Package settings menyimpan pengaturan bot yang bisa diubah saat runtime lewat
// command. Setting persisten disimpan di store dan di-cache di memori.
package settings

import (
	"sync"

	"rbot/brain/store"
)

const storeKey = "settings"

// data adalah bentuk tersimpan di store.
type data struct {
	AutoRead   bool `json:"autoRead"`
	ButtonMode int  `json:"buttonMode"`
}

var (
	mu     sync.RWMutex
	cache  data
	loaded bool
)

// Load membaca setting dari store ke cache. Default mode button = 1.
func Load() error {
	mu.Lock()
	defer mu.Unlock()
	var d data
	found, err := store.Get(storeKey, &d)
	if err != nil {
		return err
	}
	if !found || d.ButtonMode < 0 || d.ButtonMode > 4 {
		d.ButtonMode = 1
	}
	cache = d
	loaded = true
	return nil
}

// AutoRead mengembalikan status autoread.
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

// ButtonMode mengembalikan mode tampilan button global (0..4).
func ButtonMode() int {
	mu.RLock()
	defer mu.RUnlock()
	if !loaded && cache.ButtonMode == 0 {
		return 1
	}
	return cache.ButtonMode
}

// SetButtonMode menyimpan mode tampilan button global (0..4).
func SetButtonMode(mode int) error {
	if mode < 0 || mode > 4 {
		return &invalidButtonMode{mode: mode}
	}
	mu.Lock()
	cache.ButtonMode = mode
	loaded = true
	snapshot := cache
	mu.Unlock()
	return store.Set(storeKey, snapshot)
}

type invalidButtonMode struct{ mode int }

func (e *invalidButtonMode) Error() string { return "mode button harus antara 0 dan 4" }
