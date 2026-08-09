// Package store adalah penyimpanan key -> nilai JSON, port dari lib/store.js
// yang di bot Node memakai LMDB. Versi Go ini juga memakai LMDB (via cgo,
// github.com/PowerDNS/lmdb-go) supaya baca/tulis cepat lewat memory-map.
// Satu named-DB "kv": key = nama store, value = JSON.
package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"

	"github.com/PowerDNS/lmdb-go/lmdb"
)

const (
	// mapSize adalah batas atas ukuran DB (bukan alokasi awal). LMDB butuh ukuran
	// map tetap; 1 GiB jauh lebih dari cukup untuk setting/energi/premium.
	mapSize = 1 << 30
	dbName  = "kv"
)

var (
	env *lmdb.Env
	dbi lmdb.DBI
	mu  sync.Mutex
)

// Open membuka/membuat DB LMDB di dataDir/bot.lmdb (file tunggal, NoSubdir).
func Open(dataDir string) error {
	e, err := lmdb.NewEnv()
	if err != nil {
		return err
	}
	if err := e.SetMapSize(mapSize); err != nil {
		e.Close()
		return err
	}
	if err := e.SetMaxDBs(1); err != nil {
		e.Close()
		return err
	}
	// NoSubdir: perlakukan path sebagai file tunggal (data/bot.lmdb + -lock),
	// selaras dengan lokasi lama di bot Node. WriteMap: mmap yang bisa ditulis
	// langsung → tulis lebih cepat; fsync tetap dilakukan saat commit (durable).
	path := filepath.Join(dataDir, "bot.lmdb")
	if err := e.Open(path, lmdb.NoSubdir|lmdb.WriteMap, 0o600); err != nil {
		e.Close()
		return err
	}
	if err := e.Update(func(txn *lmdb.Txn) error {
		var e2 error
		dbi, e2 = txn.CreateDBI(dbName)
		return e2
	}); err != nil {
		e.Close()
		return err
	}
	env = e
	return nil
}

// Close menutup database.
func Close() error {
	if env != nil {
		return env.Close()
	}
	return nil
}

// Get meng-unmarshal nilai key ke out. Mengembalikan (false, nil) bila key tak ada.
func Get(key string, out any) (bool, error) {
	if env == nil {
		return false, errors.New("store belum dibuka")
	}
	var raw []byte
	err := env.View(func(txn *lmdb.Txn) error {
		v, e := txn.Get(dbi, []byte(key))
		if lmdb.IsNotFound(e) {
			return nil
		}
		if e != nil {
			return e
		}
		// v menunjuk memory mmap yang hanya valid selama txn; salin keluar.
		raw = append([]byte(nil), v...)
		return nil
	})
	if err != nil {
		return false, err
	}
	if raw == nil {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}

// GetOr meng-unmarshal key ke out; bila key tak ada, out dibiarkan (fallback pemanggil).
func GetOr(key string, out any) error {
	_, err := Get(key, out)
	return err
}

// Set menyimpan val sebagai JSON di key.
func Set(key string, val any) error {
	if env == nil {
		return errors.New("store belum dibuka")
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	return env.Update(func(txn *lmdb.Txn) error {
		return txn.Put(dbi, []byte(key), raw, 0)
	})
}

// Delete menghapus key (aman bila key sudah tidak ada).
func Delete(key string) error {
	if env == nil {
		return errors.New("store belum dibuka")
	}
	mu.Lock()
	defer mu.Unlock()
	return env.Update(func(txn *lmdb.Txn) error {
		e := txn.Del(dbi, []byte(key), nil)
		if lmdb.IsNotFound(e) {
			return nil
		}
		return e
	})
}
