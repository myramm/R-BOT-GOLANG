package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"rbot/brain/command"
)

// backupsc.go: backup seluruh source code bot + session jadi satu .tar.gz lalu
// kirim ke chat (owner). Port backupsc.js, disesuaikan untuk runtime Go:
//   - session WhatsApp ada di ./session (sqlite), IKUT dibackup — ini kredensial.
//   - ./data (LMDB runtime) TIDAK ikut, sama seperti Node meng-exclude ./data.
//   - binary hasil build & artefak lain di-exclude.

var backupExcludes = []string{
	"./data",        // KV store runtime (LMDB)
	"./*.tar.gz",    // hasil backup sebelumnya
	"./*.log",       // log
	"./.git",        // riwayat git
	"./bot-go",      // binary hasil `go build` (nama = folder modul)
	"./rbot",        // binary alternatif
	"./__debug_bin", // binary debugger (dlv)
}

func init() {
	command.Register(&command.Command{
		Name:        "backupsc",
		Category:    "Owner",
		Description: "Backup seluruh source code bot + session jadi satu file .tar.gz, dikirim ke chat (owner)",
		OwnerOnly:   true,
		Handler:     backupscHandler,
	})
}

func backupscHandler(ctx context.Context, c *command.Ctx) error {
	backupPath := filepath.Join(os.TempDir(), "botwa-backup-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".tar.gz")
	defer os.Remove(backupPath)

	args := []string{"-czf", backupPath, "-C", "."}
	for _, ex := range backupExcludes {
		args = append(args, "--exclude", ex)
	}
	// Kecualikan juga file backup yang sedang dibuat bila ia berada di dalam cwd.
	args = append(args, "--exclude", "./"+filepath.Base(backupPath))
	args = append(args, ".")

	if out, err := exec.CommandContext(ctx, "tar", args...).CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		_, e := c.Reply(ctx, "Gagal backup: "+msg)
		return e
	}

	data, err := os.ReadFile(backupPath)
	if err != nil {
		_, e := c.Reply(ctx, "Gagal backup: "+err.Error())
		return e
	}

	sizeMB := strconv.FormatFloat(float64(len(data))/1024/1024, 'f', 2, 64)
	caption := "Backup source code bot + session (" + sizeMB + " MB).\n\n" +
		"Isi: semua source code + folder session (./session). Folder data runtime & binary TIDAK ikut — " +
		"setelah extract, jalankan `go build` dulu.\n\n" +
		"SIMPAN BAIK-BAIK, jangan dibagikan — file ini berisi kredensial login " +
		"WhatsApp bot (folder session), siapa pun yang punya bisa login sebagai bot ini."

	if err := c.SendMediaBytes(ctx, data, command.MediaDocument, caption, "botwa-backup.tar.gz", "application/gzip"); err != nil {
		_, e := c.Reply(ctx, "Gagal backup: "+err.Error())
		return e
	}
	return nil
}
