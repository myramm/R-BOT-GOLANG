package cmd

import (
	"context"
	"fmt"
	"time"

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

			phone := jadibot.NormalizePhone(c.SenderPhone())
			if len(c.Args) > 0 {
				argPhone := jadibot.NormalizePhone(c.ArgStr())
				if len(argPhone) >= 10 && len(argPhone) <= 15 {
					phone = argPhone
				}
			}

			code, err := jadibot.StartPairing(ctx, phone, c.Sender())
			if err != nil {
				_, replyErr := c.Reply(ctx, "❌ "+err.Error())
				return replyErr
			}

			msg := fmt.Sprintf("🔑 *KODE PAIRING JADIBOT*\n\n"+
				"📱 *Nomor Target:* +%s\n"+
				"🔑 *Kode Pairing:* *%s*\n\n"+
				"*Langkah-langkah menyambungkan:*\n"+
				"1. Buka WhatsApp di HP nomor +%s\n"+
				"2. Ketuk *Titik Tiga* (kanan atas) / *Pengaturan*\n"+
				"3. Pilih *Perangkat Tertaut* (Linked Devices)\n"+
				"4. Pilih *Tautkan Perangkat* -> *Tautkan dengan nomor telepon*\n"+
				"5. Masukkan kode 8 digit di atas\n\n"+
				"⏱️ _Kode berlaku selama ~60 detik. Pastikan nomor HP sama persis!_", phone, code, phone)

			_, err = c.Reply(ctx, msg)

			// Cek apakah pairing berhasil dalam 60 detik; bila belum, infokan bahwa kode telah kadaluarsa
			targetPhone := phone
			requesterJID := c.Sender()
			go func() {
				time.Sleep(60 * time.Second)
				if !jadibot.IsConnected(targetPhone) {
					_, _ = jadibot.Stop(context.Background(), targetPhone, requesterJID, true)
					_, _ = c.Reply(context.Background(), "⏱️ *KODE PAIRING KADALUARSA*\n\n"+
						"Kode pairing sebelumnya telah kadaluarsa karena tidak dimasukkan dalam kurun waktu 60 detik.\n\n"+
						"Silakan ketik *.jadibot* kembali untuk mendapatkan kode pairing baru.")
				}
			}()

			return err
		},
	})
}
