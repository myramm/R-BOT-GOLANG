package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/jadibot"
	cmdfunc "rbot/cmd/func"
)

func init() {
	command.Register(&command.Command{
		Name:        "listjadibot",
		Category:    "jadibot",
		Alias:       []string{"jadibots"},
		Description: "Tampilkan daftar sub-bot yang sedang aktif",
		Handler: func(ctx context.Context, c *command.Ctx) error {
			bots := jadibot.List()
			if len(bots) == 0 {
				_, err := c.Reply(ctx, "ℹ️ Belum ada sub-bot yang aktif saat ini.")
				return err
			}

			var sb strings.Builder
			maxLimit := config.C.MaxJadibot
			if maxLimit > 0 {
				fmt.Fprintf(&sb, "🤖 *DAFTAR SUB-BOT AKTIF* (%d/%d)\n\n", len(bots), maxLimit)
			} else {
				fmt.Fprintf(&sb, "🤖 *DAFTAR SUB-BOT AKTIF* (%d)\n\n", len(bots))
			}

			var mentions []types.JID
			for i, b := range bots {
				ownerStr := "-"
				if !b.OwnerJID.IsEmpty() {
					ownerStr = "@" + b.OwnerJID.User
					mentions = append(mentions, b.OwnerJID)
				}
				uptimeStr := cmdfunc.FormatUptime(time.Since(b.ConnectedAt))
				fmt.Fprintf(&sb, "%d. *Nomor:* %s\n   *Owner:* %s\n   *Uptime:* %s\n\n", i+1, b.Phone, ownerStr, uptimeStr)
			}

			text := strings.TrimSpace(sb.String())
			if len(mentions) > 0 {
				_, err := c.ReplyMentions(ctx, text, mentions)
				return err
			}
			_, err := c.Reply(ctx, text)
			return err
		},
	})
}
