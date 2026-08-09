package cmd

import (
	"context"

	"rbot/brain/command"
	"rbot/brain/config"
)

func init() {
	command.Register(&command.Command{
		Name:        "owner",
		Category:    "Info",
		Description: "Menampilkan kontak owner bot",
		Handler: func(ctx context.Context, c *command.Ctx) error {
			num := config.Digits(config.C.OwnerNumber)
			text := "👤 *Owner " + config.C.BotName + "*\n"
			if num != "" {
				text += "wa.me/" + num
			}
			_, err := c.Reply(ctx, text)
			return err
		},
	})
}
