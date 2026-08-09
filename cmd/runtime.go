package cmd

import (
	"context"
	"time"

	"rbot/brain/command"
	cmdfunc "rbot/cmd/func"
)

func init() {
	command.Register(&command.Command{
		Name:        "runtime",
		Category:    "Info",
		Description: "Menampilkan lama bot menyala (uptime)",
		Handler: func(ctx context.Context, c *command.Ctx) error {
			_, err := c.Reply(ctx, "⏱️ *Uptime:* "+cmdfunc.FormatUptime(time.Since(cmdfunc.StartTime)))
			return err
		},
	})
}
