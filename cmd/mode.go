package cmd

import (
	"context"
	"fmt"
	"strings"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/settings"
)

func init() {
	command.Register(&command.Command{
		Name:        "mode",
		Category:    "Owner",
		Alias:       []string{"self", "public", "botmode"},
		Description: "Ubah mode akses bot: self (hanya owner) atau public (semua orang)",
		OwnerOnly:   true,
		Handler:     modeHandler,
	})
}

func modeHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()
	args := strings.Fields(c.ArgStr())

	// Jika dipanggil via alias .self
	if c.InvokedAs == "self" {
		if err := settings.SetSelfMode(true); err != nil {
			return replyModeError(ctx, c, "Gagal mengubah mode: "+err.Error())
		}
		_, err := c.Reply(ctx, "🔒 *Mode Bot: SELF*\n\nBot sekarang hanya merespon perintah dari Owner bot.")
		return err
	}

	// Jika dipanggil via alias .public
	if c.InvokedAs == "public" {
		if err := settings.SetSelfMode(false); err != nil {
			return replyModeError(ctx, c, "Gagal mengubah mode: "+err.Error())
		}
		_, err := c.Reply(ctx, "🌐 *Mode Bot: PUBLIC*\n\nBot sekarang dapat digunakan oleh semua pengguna.")
		return err
	}

	if len(args) == 0 {
		currentMode := "🌐 PUBLIC (Semua Orang)"
		if settings.IsSelfMode() {
			currentMode = "🔒 SELF (Hanya Owner)"
		}
		msg := fmt.Sprintf("🤖 *PENGATURAN MODE BOT*\n\n"+
			"• *Mode Saat Ini:* %s\n\n"+
			"*Perintah:* (Khusus Owner)\n"+
			"• `%smode self` (atau `%sself`) — Aktifkan mode private/self\n"+
			"• `%smode public` (atau `%spublic`) — Aktifkan mode public\n"+
			"• `%smode status` — Cek status mode saat ini",
			currentMode, mp, mp, mp, mp, mp)
		_, err := c.Reply(ctx, msg)
		return err
	}

	action := strings.ToLower(args[0])
	switch action {
	case "self", "private", "owner", "1", "on":
		if err := settings.SetSelfMode(true); err != nil {
			return replyModeError(ctx, c, "Gagal mengubah mode: "+err.Error())
		}
		_, err := c.Reply(ctx, "🔒 *Mode Bot Diubah ke SELF*\n\nBot sekarang hanya merespon perintah dari Owner bot.")
		return err

	case "public", "publik", "all", "0", "off":
		if err := settings.SetSelfMode(false); err != nil {
			return replyModeError(ctx, c, "Gagal mengubah mode: "+err.Error())
		}
		_, err := c.Reply(ctx, "🌐 *Mode Bot Diubah ke PUBLIC*\n\nBot sekarang dapat digunakan oleh semua pengguna.")
		return err

	case "status":
		currentMode := "🌐 PUBLIC (Semua Orang)"
		if settings.IsSelfMode() {
			currentMode = "🔒 SELF (Hanya Owner)"
		}
		_, err := c.Reply(ctx, fmt.Sprintf("🤖 *Status Mode Bot:* %s", currentMode))
		return err

	default:
		_, err := c.Reply(ctx, fmt.Sprintf("❌ Pilihan mode tidak dikenal. Gunakan:\n• `%smode self`\n• `%smode public`", mp, mp))
		return err
	}
}

func replyModeError(ctx context.Context, c *command.Ctx, message string) error {
	c.ReportErrorMessage(ctx, message)
	c.React(ctx, "❌")
	_, err := c.Reply(ctx, "❌ "+message)
	return err
}
