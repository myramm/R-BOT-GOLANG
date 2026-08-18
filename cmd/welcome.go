package cmd

import (
	"context"
	"fmt"
	"strings"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/welcome"
)

func init() {
	command.Register(&command.Command{
		Name:        "welcome",
		Category:    "Group",
		Alias:       []string{"welkom", "setwelcome"},
		Description: "Kelola pesan sambutan otomatis untuk member baru yang masuk ke grup",
		Handler:     welcomeHandler,
	})
}

func welcomeHandler(ctx context.Context, c *command.Ctx) error {
	if !c.IsGroup() {
		_, err := c.Reply(ctx, "❌ Perintah *welcome* hanya dapat digunakan di dalam grup.")
		return err
	}

	groupID := c.Chat().String()
	args := strings.Fields(c.ArgStr())
	mp := config.MainPrefix()

	if len(args) == 0 {
		return showWelcomeHelp(ctx, c, groupID, mp)
	}

	action := strings.ToLower(args[0])

	// Command alias .setwelcome <teks>
	if c.InvokedAs == "setwelcome" {
		customMsg := strings.TrimSpace(c.ArgStr())
		if customMsg == "" {
			_, err := c.Reply(ctx, fmt.Sprintf("❌ Format salah. Contoh:\n*%ssetwelcome Halo @user, selamat datang di {group}!*", mp))
			return err
		}
		if !checkWelcomePermission(ctx, c) {
			_, err := c.Reply(ctx, "❌ Hanya Admin grup atau Owner bot yang dapat mengatur pesan welcome.")
			return err
		}
		if err := welcome.SetTemplate(groupID, customMsg); err != nil {
			return replyWelcomeError(ctx, c, "Gagal menyimpan template welcome: "+err.Error())
		}
		_, err := c.Reply(ctx, "✅ *Template Pesan Welcome Berhasil Disimpan!*\n\n*Format pesan baru:*\n"+customMsg)
		return err
	}

	switch action {
	case "on", "enable", "1", "aktif":
		if !checkWelcomePermission(ctx, c) {
			_, err := c.Reply(ctx, "❌ Hanya Admin grup atau Owner bot yang dapat mengaktifkan fitur welcome.")
			return err
		}
		if err := welcome.SetEnabled(groupID, true); err != nil {
			return replyWelcomeError(ctx, c, "Gagal mengaktifkan welcome: "+err.Error())
		}
		_, err := c.Reply(ctx, "✅ *Pesan Welcome Diaktifkan!*\n\nBot akan otomatis menyambut setiap member baru yang bergabung ke grup ini.")
		return err

	case "off", "disable", "0", "mati", "nonaktif":
		if !checkWelcomePermission(ctx, c) {
			_, err := c.Reply(ctx, "❌ Hanya Admin grup atau Owner bot yang dapat menonaktifkan fitur welcome.")
			return err
		}
		if err := welcome.SetEnabled(groupID, false); err != nil {
			return replyWelcomeError(ctx, c, "Gagal menonaktifkan welcome: "+err.Error())
		}
		_, err := c.Reply(ctx, "🛑 *Pesan Welcome Dinonaktifkan!*\n\nBot tidak akan mengirim sambutan saat ada member baru masuk.")
		return err

	case "status":
		status := "🟢 AKTIF"
		if !welcome.IsEnabled(groupID) {
			status = "🔴 NONAKTIF"
		}
		tmpl := welcome.GetTemplate(groupID)
		isCustom := "Bawaan Sistem"
		if welcome.HasCustomTemplate(groupID) {
			isCustom = "Kustom Grup"
		}

		text := fmt.Sprintf("👋 *STATUS FITUR WELCOME GRUP*\n\n"+
			"• *Status:* %s\n"+
			"• *Tipe Template:* %s\n\n"+
			"📝 *Isi Pesan Sambutan:*\n%s\n\n"+
			"_Gunakan `%swelcome on/off` untuk ubah status atau `%swelcome set <pesan>` untuk kustomisasi._",
			status, isCustom, tmpl, mp, mp)
		_, err := c.Reply(ctx, text)
		return err

	case "set", "custom":
		if !checkWelcomePermission(ctx, c) {
			_, err := c.Reply(ctx, "❌ Hanya Admin grup atau Owner bot yang dapat mengatur pesan welcome.")
			return err
		}
		customMsg := strings.TrimSpace(c.ArgStr())
		if len(args) > 1 {
			customMsg = strings.TrimSpace(c.ArgStr()[len(args[0]):])
		} else {
			customMsg = ""
		}
		if customMsg == "" {
			_, err := c.Reply(ctx, fmt.Sprintf("❌ Masukkan format pesan welcome. Contoh:\n*%swelcome set Halo @user selamat datang di {group}!*", mp))
			return err
		}
		if err := welcome.SetTemplate(groupID, customMsg); err != nil {
			return replyWelcomeError(ctx, c, "Gagal menyimpan template welcome: "+err.Error())
		}
		_, err := c.Reply(ctx, "✅ *Template Pesan Welcome Berhasil Disimpan!*\n\n*Format pesan baru:*\n"+customMsg)
		return err

	case "reset":
		if !checkWelcomePermission(ctx, c) {
			_, err := c.Reply(ctx, "❌ Hanya Admin grup atau Owner bot yang dapat mereset pesan welcome.")
			return err
		}
		if err := welcome.ResetTemplate(groupID); err != nil {
			return replyWelcomeError(ctx, c, "Gagal reset template welcome: "+err.Error())
		}
		_, err := c.Reply(ctx, "🔄 *Template Pesan Welcome Berhasil Di-reset ke Bawaan!*")
		return err

	default:
		return showWelcomeHelp(ctx, c, groupID, mp)
	}
}

func checkWelcomePermission(ctx context.Context, c *command.Ctx) bool {
	if command.IsOwner(c.Evt) {
		return true
	}
	if command.IsGroupAdminHook != nil {
		return command.IsGroupAdminHook(ctx, c.Client, c.Evt)
	}
	return false
}

func showWelcomeHelp(ctx context.Context, c *command.Ctx, groupID, mp string) error {
	status := "🟢 AKTIF"
	if !welcome.IsEnabled(groupID) {
		status = "🔴 NONAKTIF"
	}
	help := fmt.Sprintf("👋 *PENGATURAN PESAN WELCOME GRUP*\n\n"+
		"Fitur ini akan menyambut setiap member baru yang bergabung ke dalam grup dengan mention.\n\n"+
		"*Status grup ini:* %s\n\n"+
		"*Perintah (Khusus Admin & Owner):*\n"+
		"• `%swelcome on` — Aktifkan sambutan member baru\n"+
		"• `%swelcome off` — Matikan sambutan member baru\n"+
		"• `%swelcome status` — Cek status & teks sambutan\n"+
		"• `%swelcome set <pesan>` — Kustomisasi isi pesan\n"+
		"• `%swelcome reset` — Kembalikan pesan ke bawaan\n\n"+
		"*Tag Variabel:* `@user` (mention member), `{group}` (nama grup), `{desc}` (deskripsi grup)",
		status, mp, mp, mp, mp, mp)
	_, err := c.Reply(ctx, help)
	return err
}

func replyWelcomeError(ctx context.Context, c *command.Ctx, message string) error {
	c.ReportErrorMessage(ctx, message)
	c.React(ctx, "❌")
	_, err := c.Reply(ctx, "❌ "+message)
	return err
}
