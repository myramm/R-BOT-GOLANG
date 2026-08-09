package cmd

import (
	"context"
	"fmt"
	"strings"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/lifecycle"
	"rbot/brain/updater"
)

// update.go: tarik update terbaru dari GitHub lalu build ulang & restart (owner).
// Port update.js. Beda dengan Node (yang bisa reload file command tanpa restart),
// biner Go WAJIB di-build ulang (`go build`) lalu proses di-exec ulang, jadi
// setiap update selalu berujung restart. `update cek` cuma cek versi; `update
// force` buang perubahan lokal & samakan dengan GitHub. Lihat brain/updater.

func init() {
	command.Register(&command.Command{
		Name:        "update",
		Category:    "Owner",
		Alias:       []string{"upgrade"},
		Description: "Tarik update terbaru dari GitHub (owner): git pull → build ulang → restart. .update cek = cuma cek versi. .update force = buang perubahan lokal & samakan dgn GitHub.",
		OwnerOnly:   true,
		Handler:     updateHandler,
	})
}

// formatUpdateSummary menyusun ringkasan update (branch, jumlah commit, sampai 10
// subject, jumlah file berubah). Port formatUpdateSummary di update.js.
func formatUpdateSummary(info updater.CheckResult) string {
	lines := []string{
		fmt.Sprintf("🔄 *Update tersedia* (branch %s)", info.Branch),
		fmt.Sprintf("%d commit baru:", info.Behind),
	}
	for i, cm := range info.Commits {
		if i >= 10 {
			break
		}
		lines = append(lines, fmt.Sprintf("• %s _(%s)_", cm.Subject, cm.Hash))
	}
	if len(info.Commits) > 10 {
		lines = append(lines, fmt.Sprintf("…dan %d commit lagi", len(info.Commits)-10))
	}
	if len(info.ChangedFiles) > 0 {
		lines = append(lines, "", fmt.Sprintf("📄 %d file berubah", len(info.ChangedFiles)))
	}
	return strings.Join(lines, "\n")
}

func updateHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()
	mode := ""
	if len(c.Args) > 0 {
		mode = strings.ToLower(c.Args[0])
	}
	checkOnly := mode == "cek" || mode == "check"
	force := mode == "force" || mode == "paksa"

	if _, err := c.Reply(ctx, "🔎 Mengecek update dari GitHub..."); err != nil {
		return err
	}

	info := updater.CheckUpdate(ctx)
	if !info.OK {
		_, err := c.Reply(ctx, "❌ "+info.Err)
		return err
	}
	if info.UpToDate && !force {
		_, err := c.Reply(ctx, fmt.Sprintf("✅ Bot sudah versi terbaru (branch %s).", info.Branch))
		return err
	}

	if checkOnly {
		_, err := c.Reply(ctx, formatUpdateSummary(info)+fmt.Sprintf("\n\nKetik *%supdate* untuk menerapkan & restart.", mp))
		return err
	}

	// Terapkan update (force = reset --hard, selain itu pull --ff-only).
	var res updater.ApplyResult
	if force {
		if _, err := c.Reply(ctx, "⚠️ Mode paksa: membuang perubahan lokal & menyamakan dengan GitHub..."); err != nil {
			return err
		}
		res = updater.ForceUpdate(ctx)
	} else {
		if _, err := c.Reply(ctx, formatUpdateSummary(info)+"\n\n⏬ Menerapkan update..."); err != nil {
			return err
		}
		res = updater.ApplyUpdate(ctx)
	}
	if !res.OK {
		_, err := c.Reply(ctx, "❌ "+res.Err)
		return err
	}

	// Build ulang biner sebelum restart (go build otomatis menarik dependency baru).
	if _, err := c.Reply(ctx, "🔨 Membangun ulang biner..."); err != nil {
		return err
	}
	if err := updater.Rebuild(ctx); err != nil {
		_, e := c.Reply(ctx, fmt.Sprintf("❌ Update sudah ditarik, tapi build gagal:\n%s\n\nPerbaiki manual lalu *%srestart*.", err.Error(), mp))
		return e
	}

	c.React(ctx, "♻️")
	if _, err := c.Reply(ctx, "✅ Update selesai & biner terbangun.\n♻️ Merestart bot... aku kabari lagi begitu online."); err != nil {
		return err
	}
	// Picu shutdown rapi → main re-exec biner baru (lihat main.go).
	lifecycle.Request(c.Chat().String())
	return nil
}
