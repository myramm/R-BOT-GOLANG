package cmd

import (
	"context"
	"fmt"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/lifecycle"
)

func init() {
	command.Register(&command.Command{
		Name:        "reload",
		Category:    "Owner",
		Alias:       []string{"reloadcmd"},
		Description: "Muat ulang binary command Go dengan restart proses (owner)",
		OwnerOnly:   true,
		Handler:     reloadHandler,
	})
}

func reloadHandler(ctx context.Context, c *command.Ctx) error {
	prefix := config.MainPrefix()
	_, err := c.Reply(ctx, fmt.Sprintf("🔁 Command Go dikompilasi ke dalam binary, jadi tidak bisa hot-reload seperti Node.\n\nUntuk memuat perubahan terbaru, jalankan *%supdate* atau restart proses secara manual.", prefix))
	if err != nil {
		return err
	}
	// Restart dilakukan setelah balasan diantrekan; lifecycle menutup koneksi
	// secara rapi dan main akan menjalankan binary yang sama kembali.
	lifecycle.Request(c.Chat().String())
	return nil
}
