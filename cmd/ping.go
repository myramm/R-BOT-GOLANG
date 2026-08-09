package cmd

import (
	"context"
	"fmt"
	"time"

	"rbot/brain/command"
)

func init() {
	command.Register(&command.Command{
		Name:        "ping",
		Category:    "Info",
		Alias:       []string{"p"},
		Description: "Cek bot masih hidup & seberapa cepat responnya",
		Handler: func(ctx context.Context, c *command.Ctx) error {
			lat := time.Since(c.Evt.Info.Timestamp).Milliseconds()
			if lat < 0 {
				lat = 0
			}
			_, err := c.Reply(ctx, fmt.Sprintf("🏓 Pong! %dms", lat))
			return err
		},
	})
}
