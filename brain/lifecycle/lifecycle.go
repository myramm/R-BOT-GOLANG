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

// resetMarkerPath adalah penanda reset sesi: DB sesi WhatsApp bot utama
// dihapus saat restart agar proses baru otomatis meminta kode pairing baru.
var resetMarkerPath = filepath.Join("data", "reset_session.json")

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

// RequestSessionReset menandai permintaan restart dengan penghapusan sesi
// WhatsApp bot utama (fitur pairing ulang dari Web Dashboard). Idempoten.
func RequestSessionReset() {
	_ = os.WriteFile(resetMarkerPath, []byte("{}"), 0o600)
	Request("")
}

// TakePendingSessionReset membaca & menghapus penanda reset sesi. True bila
// restart terakhir diminta bersama reset sesi; main menghapus DB sesi WhatsApp
// sebelum re-exec agar pairing baru otomatis diminta saat boot.
func TakePendingSessionReset() bool {
	b, err := os.ReadFile(resetMarkerPath)
	if err != nil {
		return false
	}
	_ = os.Remove(resetMarkerPath)
	return len(b) > 0
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
