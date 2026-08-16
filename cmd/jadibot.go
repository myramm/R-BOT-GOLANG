package cmd

import (
	"context"
	"fmt"
	"strings"

	"rbot/brain/command"
	"rbot/brain/jadibot"
)

func init() {
	command.Register(&command.Command{
		Name:        "jadibot",
		Category:    "jadibot",
		Alias:       []string{"clonebot", "subbot"},
		Description: "Dapatkan kode pairing untuk clone bot ke akun WhatsApp kamu",
		Handler: func(ctx context.Context, c *command.Ctx) error {
			if c.SubBot {
				_, err := c.Reply(ctx, "⚠️ Fitur jadibot tidak dapat dijalankan dari sub-bot.")
				return err
			}

			phone := c.Sender().User
			if len(c.Args) > 0 && strings.TrimSpace(c.Args[0]) != "" {
				phone = strings.TrimSpace(c.ArgStr())
			}

			code, err := jadibot.StartPairing(ctx, phone, c.Sender())
			if err != nil {
				_, replyErr := c.Reply(ctx, "❌ "+err.Error())
				return replyErr
			}

			msg := fmt.Sprintf("🔑 *KODE PAIRING JADIBOT*\n\n"+
				"Kode Anda: *%s*\n\n"+
				"*Langkah-langkah menyambungkan:*\n"+
				"1. Buka WhatsApp di HP kamu\n"+
				"2. Ketuk *Titik Tiga* (kanan atas) / *Pengaturan*\n"+
				"3. Pilih *Perangkat Tertaut* (Linked Devices)\n"+
				"4. Pilih *Tautkan Perangkat* -> *Tautkan dengan nomor telepon*\n"+
				"5. Masukkan kode 8 digit di atas\n\n"+
				"⏱️ _Kode berlaku selama ~30 detik. Jangan bagikan kode ini kepada siapa pun!_", code)

			_, err = c.Reply(ctx, msg)
			return err
		},
	})
}
