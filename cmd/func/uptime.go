// Package cmdfunc berisi fungsi bersama (helper & factory) yang dipakai banyak
// file command di paket cmd — mis. gerbang admin grup, factory handler
// kick/promote, resolusi target user, format uptime/tanggal. Dipisah ke
// cmd/func/ supaya paket cmd hanya berisi pendaftaran command (satu file per
// command) dan tetap rapi.
//
// Direktori bernama "func"; karena "func" kata-kunci Go, nama paketnya
// "cmdfunc" dan diimpor sebagai: cmdfunc "rbot/cmd/func".
package cmdfunc

import (
	"fmt"
	"time"
)

// StartTime dipakai untuk uptime (setara startTime di lib/utils.js).
var StartTime = time.Now()

// FormatUptime memformat durasi jadi "Xj Xm Xd" (jam/menit/detik).
func FormatUptime(d time.Duration) string {
	sec := int(d.Seconds())
	return fmt.Sprintf("%dj %dm %dd", sec/3600, (sec%3600)/60, sec%60)
}
