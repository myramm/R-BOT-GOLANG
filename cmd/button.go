package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/settings"
)

func init() {
	command.Register(&command.Command{
		Name:        "button",
		Category:    "Owner",
		Alias:       []string{"btn", "buttons", "settheme"},
		Description: "Atur mode button global (0–4). Runtime Go memakai balasan teks bila interactive button belum tersedia.",
		OwnerOnly:   true,
		Handler:     buttonHandler,
	})
}

func buttonHandler(ctx context.Context, c *command.Ctx) error {
	prefix := config.MainPrefix()
	if len(c.Args) == 0 || strings.TrimSpace(c.Args[0]) == "" {
		return buttonHelp(ctx, c)
	}
	mode, err := strconv.Atoi(strings.TrimSpace(c.Args[0]))
	if err != nil || mode < 0 || mode > 4 {
		_, err := c.Reply(ctx, fmt.Sprintf("⚠️ Mode button tidak valid. Pilih 0–4.\nKetik *%sbutton* untuk bantuan.", prefix))
		return err
	}
	if err := settings.SetButtonMode(mode); err != nil {
		_, replyErr := c.Reply(ctx, "❌ Gagal menyimpan mode button: "+err.Error())
		return replyErr
	}
	description := map[int]string{
		0: "Bot memakai pesan teks biasa tanpa mode button.",
		1: "Mode Quick Reply dicatat untuk kompatibilitas konfigurasi.",
		2: "Mode Action Button dicatat untuk kompatibilitas konfigurasi.",
		3: "Mode Single Select dicatat untuk kompatibilitas konfigurasi.",
		4: "Mode Hybrid dicatat untuk kompatibilitas konfigurasi.",
	}[mode]
	_, err = c.Reply(ctx, fmt.Sprintf("✅ *Tema Button Berhasil Diperbarui*\n\nMode aktif: *Mode %d*\n%s\n\n_Catatan: renderer WhatsApp native button belum tersedia di engine Go ini._", mode, description))
	return err
}

func buttonHelp(ctx context.Context, c *command.Ctx) error {
	prefix := config.MainPrefix()
	text := fmt.Sprintf("🔘 *PENGATURAN TEMA BUTTON GLOBAL*\n\nStatus saat ini: *Mode %d*\n\nCara ganti: *%sbutton <0–4>*\n\n• *%sbutton 0* — teks biasa\n• *%sbutton 1* — Quick Reply\n• *%sbutton 2* — Action Button\n• *%sbutton 3* — Single Select\n• *%sbutton 4* — Hybrid\n\n_Mode disimpan untuk kompatibilitas; pesan Go saat ini tetap dikirim sebagai teks/media biasa._", settings.ButtonMode(), prefix, prefix, prefix, prefix, prefix, prefix)
	_, err := c.Reply(ctx, text)
	return err
}
