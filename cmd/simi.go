package cmd

import (
	"context"
	"fmt"
	"strings"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/simi"
)

func init() {
	command.Register(&command.Command{
		Name:        "simi",
		Category:    "AI",
		Alias:       []string{"simisimi", "chatsimi"},
		Description: "Pengaturan fitur Simi-Simi AI (auto-reply saat quote pesan bot)",
		Handler:     simiHandler,
	})
}

func simiHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()
	chatID := c.Chat().String()
	arg := strings.ToLower(strings.TrimSpace(c.ArgStr()))

	// Di grup, hanya Admin atau Owner yang boleh mengubah setting
	if c.IsGroup() && (arg == "on" || arg == "off" || arg == "enable" || arg == "disable") {
		isOwner := command.IsOwner(c.Evt)
		isAdmin := false
		if command.IsGroupAdminHook != nil {
			isAdmin = command.IsGroupAdminHook(ctx, c.Client, c.Evt)
		}
		if !isOwner && !isAdmin {
			_, err := c.Reply(ctx, "❌ Hanya Admin grup atau Owner bot yang dapat mengubah pengaturan Simi-Simi.")
			return err
		}
	}

	switch arg {
	case "on", "enable", "1", "aktif":
		if err := simi.SetEnabled(chatID, true); err != nil {
			return replySimiError(ctx, c, "Gagal mengaktifkan Simi-Simi: "+err.Error())
		}
		_, err := c.Reply(ctx, "✅ *Simi-Simi AI Diaktifkan*\n\nBot akan otomatis membalas ketika ada yang me-reply pesan bot dengan gaya sarkas & gaul.")
		return err

	case "off", "disable", "0", "mati", "nonaktif":
		if err := simi.SetEnabled(chatID, false); err != nil {
			return replySimiError(ctx, c, "Gagal menonaktifkan Simi-Simi: "+err.Error())
		}
		_, err := c.Reply(ctx, "🛑 *Simi-Simi AI Dinonaktifkan*\n\nBot tidak akan merespon kutipan pesan santai.")
		return err

	case "status":
		status := "🟢 AKTIF"
		if !simi.IsEnabled(chatID) {
			status = "🔴 NONAKTIF"
		}
		msg := fmt.Sprintf("🤖 *Status Simi-Simi AI*\n\nStatus di chat ini: *%s*\n\nUbah status:\n• `%ssimi on` (Aktifkan)\n• `%ssimi off` (Matikan)", status, mp, mp)
		_, err := c.Reply(ctx, msg)
		return err

	case "reset", "clear", "bersihkan":
		sessionKey := simi.GetSessionKey(chatID, c.Sender().User, c.IsGroup())
		simi.ClearSession(sessionKey)
		_, err := c.Reply(ctx, "🔄 *Sesi Obrolan Simi Direset!*\n\nRiwayat obrolan sebelumnya telah dibersihkan. Memulai percakapan baru!")
		return err

	default:
		status := "🟢 AKTIF"
		if !simi.IsEnabled(chatID) {
			status = "🔴 NONAKTIF"
		}
		help := fmt.Sprintf("🤖 *PENGATURAN SIMI-SIMI AI*\n\n"+
			"Fitur Simi-Simi membuat bot otomatis membalas pesan santai dan me-reply obrolan dengan sesi berkelanjutan bergaya netizen sarkas.\n\n"+
			"*Status saat ini:* %s\n\n"+
			"*Perintah:*\n"+
			"• `%ssimi on` — Aktifkan auto-reply Simi\n"+
			"• `%ssimi off` — Matikan auto-reply Simi\n"+
			"• `%ssimi reset` — Bersihkan riwayat sesi obrolan saat ini\n"+
			"• `%ssimi status` — Cek status Simi di chat ini",
			status, mp, mp, mp, mp)
		_, err := c.Reply(ctx, help)
		return err
	}
}

func replySimiError(ctx context.Context, c *command.Ctx, message string) error {
	c.ReportErrorMessage(ctx, message)
	c.React(ctx, "❌")
	_, err := c.Reply(ctx, "❌ "+message)
	return err
}
