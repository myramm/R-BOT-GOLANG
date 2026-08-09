package cmd

import (
	"context"

	"rbot/brain/ai"
	"rbot/brain/command"
)

// resetai.go: hapus ingatan obrolan .ai milik pengirim. Port resetai.js.

func init() {
	command.Register(&command.Command{
		Name:        "resetai",
		Category:    "AI",
		Description: "Hapus ingatan obrolan kamu dengan .ai, biar mulai dari awal",
		Handler:     resetaiHandler,
	})
}

func resetaiHandler(ctx context.Context, c *command.Ctx) error {
	had := ai.Clear(senderIDAI(c))
	msg := "Belum ada percakapan yang perlu dihapus."
	if had {
		msg = "Oke, ingatan percakapan kita sudah dihapus."
	}
	_, err := c.Reply(ctx, msg)
	return err
}
