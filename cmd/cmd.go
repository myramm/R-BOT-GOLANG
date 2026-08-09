// Package cmd berisi implementasi tiap command bot — satu file per command
// (setara lib/commands/*.js di bot Node lama). Tiap file mendaftarkan command-nya
// lewat init() -> command.Register, jadi menambah command = menambah satu file.
//
// Aturan pembagian tanggung jawab supaya rapi (tidak amburadul):
//   - brain/command : kerangka/engine (registry, dispatcher, Ctx).
//   - cmd           : pendaftaran command — tipis, hanya parse argumen & balas.
//   - cmd/func      : fungsi bersama antar-command (gerbang admin grup, factory
//     kick/promote, resolve target user, format uptime/tanggal) — paket cmdfunc.
//   - lib/*         : logika berat/scraping (ytsearch, download, dsb).
//
// Command yang butuh scraping (play, download, hd, ...) memanggil paket di
// lib; helper lintas-command memanggil cmd/func. Bukan menaruh logikanya di sini.
package cmd
