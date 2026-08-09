# R-BOT Go

Port Go dari R-BOT Node.js.

## Setup lokal

1. Install Go sesuai versi yang ditentukan di `go.mod`.
2. Salin template konfigurasi:

   ```bash
   cp config.example.json config.json
   chmod 600 config.json
   ```

3. Isi `config.json` dengan nomor bot/owner dan API key milik deployment lokal.
4. Jalankan dari direktori `bot-go` agar `config.json`, `data/`, dan `session/` ditemukan:

   ```bash
   go run .
   ```

`config.json` sengaja di-ignore oleh Git. Jangan pernah menambahkan file itu dengan
`git add -f`, dan jangan menaruh API key di source code atau `config.example.json`.

## Command yang sudah dipindahkan

- `.update` / `.upgrade` — cek, tarik update, build ulang, dan restart.
- `.ai` serta alias Claude/AI — percakapan, model, mode, role, dan context.
- `.resetai` — hapus riwayat percakapan AI.
- `.setsponsor` / `.setpromo` — kelola sponsor teks, gambar, dan video.
- `.hd` / `.enhance` / `.upscale` — upscale foto 2K–16K dan video HD dengan AI; 8K/16K untuk premium.

## Keamanan GitHub

- `config.json`, database, sesi WhatsApp, media sponsor, dan credential lokal tidak boleh
  masuk commit.
- API key OpenRouter yang pernah tersimpan di repo Node.js harus di-*revoke* dan diganti,
  karena menghapusnya dari file terbaru tidak menghapusnya dari riwayat Git.
- Sebelum push, periksa staged files dan lakukan scan secret tanpa mencetak nilainya:

  ```bash
  git diff --cached --name-only
  git grep -n -E 'sk-or-v1-[A-Za-z0-9_-]{20,}' -- ':!go.sum' || true
  ```
# R-BOT-GOLANG
