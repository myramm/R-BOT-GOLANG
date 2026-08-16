package cmd

import (
	"context"
	"strings"

	"rbot/brain/command"
	"rbot/brain/jadibot"
)

func init() {
	command.Register(&command.Command{
		Name:        "stopjadibot",
		Category:    "jadibot",
		Alias:       []string{"deljadibot"},
		Description: "Hentikan dan hapus sesi jadibot",
		Handler: func(ctx context.Context, c *command.Ctx) error {
			target := c.Sender().User
			if len(c.Args) > 0 && strings.TrimSpace(c.Args[0]) != "" {
				target = strings.TrimSpace(c.ArgStr())
			}

			isOwner := command.IsOwner(c.Evt) && !c.SubBot
			err := jadibot.Stop(ctx, target, c.Sender(), isOwner)
			if err != nil {
				_, replyErr := c.Reply(ctx, "❌ "+err.Error())
				return replyErr
			}

			_, err = c.Reply(ctx, "✅ Sesi jadibot berhasil dihentikan dan dihapus.")
			return err
		},
	})
}
