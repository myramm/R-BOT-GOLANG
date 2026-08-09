package cmd

import (
	"context"

	"rbot/brain/command"
	"rbot/brain/lifecycle"
)

// restart.go: restart bot (owner). Port restart.js. Node memakai
// scheduleRestart({jid}) → tulis penanda + process.exit(42) di bawah supervisor.
// Runtime Go tak punya supervisor, jadi restart dilakukan lewat re-exec biner:
// handler cuma memicu lifecycle.Request — main yang menutup store dengan rapi
// (semua defer jalan) lalu meng-exec ulang. Lihat brain/lifecycle & main.go.

func init() {
	command.Register(&command.Command{
		Name:        "restart",
		Category:    "Owner",
		Alias:       []string{"reboot"},
		Description: "Restart bot (owner). Bot mati sebentar lalu online lagi otomatis & mengabari chat ini.",
		OwnerOnly:   true,
		Handler:     restartHandler,
	})
}

func restartHandler(ctx context.Context, c *command.Ctx) error {
	c.React(ctx, "♻️")
	// Reply dulu (sinkron — sudah di-ack server saat balik) supaya pesan pasti
	// terkirim sebelum koneksi ditutup.
	if _, err := c.Reply(ctx, "♻️ Merestart bot... aku kabari lagi begitu online."); err != nil {
		return err
	}
	// Tandai chat ini sebagai tujuan notif "sudah online", lalu picu shutdown rapi.
	lifecycle.Request(c.Chat().String())
	return nil
}
