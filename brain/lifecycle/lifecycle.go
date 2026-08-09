// Package lifecycle menangani sinyal daur-hidup proses (restart) lintas paket.
// Command (mis. restart.go) memicu Request; main menyeleksi Signal untuk keluar
// dari run() dengan rapi (semua defer/penutupan store jalan) lalu re-exec biner.
package lifecycle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// markerPath adalah penanda restart: menyimpan chat tujuan notif "sudah online".
var markerPath = filepath.Join("data", "restart.json")

var (
	restartCh = make(chan struct{})
	once      sync.Once
)

type marker struct {
	JID string `json:"jid"`
	TS  int64  `json:"ts"`
}

// Request menandai permintaan restart: tulis penanda (chat tujuan notif) lalu
// tutup channel sinyal. Idempoten — panggilan kedua diabaikan.
func Request(chatJID string) {
	if chatJID != "" {
		if b, err := json.Marshal(marker{JID: chatJID, TS: time.Now().UnixMilli()}); err == nil {
			_ = os.WriteFile(markerPath, b, 0o600)
		}
	}
	once.Do(func() { close(restartCh) })
}

// Signal ditutup ketika restart diminta. main menyeleksinya bersama ctx.Done().
func Signal() <-chan struct{} { return restartCh }

// Requested true bila restart sudah diminta (dipakai main setelah run() balik,
// untuk memutuskan re-exec vs keluar normal).
func Requested() bool {
	select {
	case <-restartCh:
		return true
	default:
		return false
	}
}

// TakePendingNotify membaca & menghapus penanda restart. Mengembalikan chat JID
// tujuan notif "sudah online" bila ada penanda dari restart sebelumnya.
func TakePendingNotify() (string, bool) {
	b, err := os.ReadFile(markerPath)
	if err != nil {
		return "", false
	}
	_ = os.Remove(markerPath)
	var m marker
	if json.Unmarshal(b, &m) != nil || m.JID == "" {
		return "", false
	}
	return m.JID, true
}
