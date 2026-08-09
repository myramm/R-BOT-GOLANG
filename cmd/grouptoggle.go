package cmd

import cmdfunc "rbot/cmd/func"

// grouptoggle.go: daftarkan open/close (mode announce grup). Factory di cmd/func.

func init() {
	cmdfunc.RegisterAnnounceToggle("open", []string{"buka"}, false,
		"🔓 Grup dibuka. Semua anggota bisa mengirim pesan.",
		"Buka grup: semua anggota bisa kirim pesan (admin).")
	cmdfunc.RegisterAnnounceToggle("close", []string{"tutup"}, true,
		"🔒 Grup ditutup. Hanya admin yang bisa mengirim pesan.",
		"Tutup grup: hanya admin yang bisa kirim pesan (admin).")
}
