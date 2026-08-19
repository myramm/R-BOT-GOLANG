// Package settings menyimpan pengaturan bot yang bisa diubah saat runtime lewat
// command. Setting persisten disimpan di store dan di-cache di memori.
package settings

import (
	"sync"

	"rbot/brain/config"
	"rbot/brain/store"
)

const storeKey = "settings"

// data adalah bentuk tersimpan di store.
type data struct {
	AutoRead        bool                       `json:"autoRead"`
	ButtonMode      int                        `json:"buttonMode"`
	SelfMode        bool                       `json:"selfMode"`
	MutedGroups     map[string]bool            `json:"mutedGroups,omitempty"`
	GlobalBlacklist map[string]bool            `json:"globalBlacklist,omitempty"`
	GroupBans       map[string]map[string]bool `json:"groupBans,omitempty"`
	ContextInfo     *config.ContextInfoConfig  `json:"contextInfo,omitempty"`
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
	if d.MutedGroups == nil {
		d.MutedGroups = make(map[string]bool)
	}
	if d.GlobalBlacklist == nil {
		d.GlobalBlacklist = make(map[string]bool)
	}
	if d.GroupBans == nil {
		d.GroupBans = make(map[string]map[string]bool)
	}
	if d.ContextInfo != nil {
		config.C.ContextInfo = *d.ContextInfo
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

// IsSelfMode mengecek apakah bot dalam mode Self (hanya Owner yang bisa eksekusi command).
func IsSelfMode() bool {
	mu.RLock()
	defer mu.RUnlock()
	return cache.SelfMode
}

// SetSelfMode mengubah mode bot (true: Self/Private, false: Public).
func SetSelfMode(self bool) error {
	mu.Lock()
	cache.SelfMode = self
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

// IsGroupMuted mengecek apakah grup di-mute (bot admin-only mode di grup).
func IsGroupMuted(groupID string) bool {
	mu.RLock()
	defer mu.RUnlock()
	if cache.MutedGroups == nil {
		return false
	}
	return cache.MutedGroups[groupID]
}

// SetGroupMuted mengubah status mute/ban grup lalu mem-persist ke store.
func SetGroupMuted(groupID string, muted bool) error {
	mu.Lock()
	if cache.MutedGroups == nil {
		cache.MutedGroups = make(map[string]bool)
	}
	if muted {
		cache.MutedGroups[groupID] = true
	} else {
		delete(cache.MutedGroups, groupID)
	}
	snapshot := cache
	mu.Unlock()
	return store.Set(storeKey, snapshot)
}

// GetMutedGroups mengembalikan map salinan seluruh grup yang di-mute.
func GetMutedGroups() map[string]bool {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[string]bool, len(cache.MutedGroups))
	for k, v := range cache.MutedGroups {
		out[k] = v
	}
	return out
}

// IsUserBlacklisted mengecek apakah salah satu ID kandidat user ada di Global Blacklist.
func IsUserBlacklisted(cands []string) bool {
	mu.RLock()
	defer mu.RUnlock()
	if cache.GlobalBlacklist == nil || len(cands) == 0 {
		return false
	}
	for _, c := range cands {
		if cache.GlobalBlacklist[c] {
			return true
		}
	}
	return false
}

// SetUserBlacklisted memasukkan atau menghapus user bare ID dari Global Blacklist.
func SetUserBlacklisted(id string, blacklisted bool) error {
	mu.Lock()
	if cache.GlobalBlacklist == nil {
		cache.GlobalBlacklist = make(map[string]bool)
	}
	if blacklisted {
		cache.GlobalBlacklist[id] = true
	} else {
		delete(cache.GlobalBlacklist, id)
	}
	snapshot := cache
	mu.Unlock()
	return store.Set(storeKey, snapshot)
}

// GetGlobalBlacklist mengembalikan daftar seluruh ID user yang di-blacklist global.
func GetGlobalBlacklist() map[string]bool {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[string]bool, len(cache.GlobalBlacklist))
	for k, v := range cache.GlobalBlacklist {
		out[k] = v
	}
	return out
}

// IsUserBannedInGroup mengecek apakah user di-ban di grup tertentu.
func IsUserBannedInGroup(groupID string, cands []string) bool {
	mu.RLock()
	defer mu.RUnlock()
	if cache.GroupBans == nil || len(cands) == 0 {
		return false
	}
	userMap, exists := cache.GroupBans[groupID]
	if !exists || userMap == nil {
		return false
	}
	for _, c := range cands {
		if userMap[c] {
			return true
		}
	}
	return false
}

// SetUserBannedInGroup mem-ban atau mem-unban daftar ID user di grup tertentu.
func SetUserBannedInGroup(groupID string, userIDs []string, banned bool) error {
	mu.Lock()
	if cache.GroupBans == nil {
		cache.GroupBans = make(map[string]map[string]bool)
	}
	userMap, exists := cache.GroupBans[groupID]
	if !exists {
		userMap = make(map[string]bool)
		cache.GroupBans[groupID] = userMap
	}

	for _, id := range userIDs {
		if banned {
			userMap[id] = true
		} else {
			delete(userMap, id)
		}
	}

	if len(userMap) == 0 {
		delete(cache.GroupBans, groupID)
	}

	snapshot := cache
	mu.Unlock()
	return store.Set(storeKey, snapshot)
}

// GetGroupBannedUsers mengembalikan daftar ID user yang di-ban di grup tertentu.
func GetGroupBannedUsers(groupID string) []string {
	mu.RLock()
	defer mu.RUnlock()
	if cache.GroupBans == nil {
		return nil
	}
	userMap := cache.GroupBans[groupID]
	if userMap == nil {
		return nil
	}
	out := make([]string, 0, len(userMap))
	for k := range userMap {
		out = append(out, k)
	}
	return out
}

type invalidButtonMode struct{ mode int }

func (e *invalidButtonMode) Error() string { return "mode button harus antara 0 dan 4" }

// GetContextInfo mengembalikan konfigurasi ContextInfo yang aktif (dari store jika ada, atau fallback ke config.json).
func GetContextInfo() config.ContextInfoConfig {
	mu.RLock()
	defer mu.RUnlock()
	if cache.ContextInfo != nil {
		return *cache.ContextInfo
	}
	return config.C.ContextInfo
}

// SetContextInfo menyimpan kustomisasi ContextInfo ke store runtime dan config runtime.
func SetContextInfo(ci config.ContextInfoConfig) error {
	mu.Lock()
	cache.ContextInfo = &ci
	config.C.ContextInfo = ci
	snapshot := cache
	mu.Unlock()
	_ = config.Save("config.json")
	return store.Set(storeKey, snapshot)
}
