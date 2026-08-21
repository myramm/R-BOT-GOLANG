package cmd

import (
	"context"
	"strings"

	"rbot/brain/command"
	"rbot/brain/identity"
	"rbot/brain/jadibot"
)

func init() {
	command.Register(&command.Command{
		Name:        "stopjadibot",
		Category:    "jadibot",
		Alias:       []string{"deljadibot", "stopbot", "offjadibot", "offbot"},
		Description: "Hentikan dan hapus sesi jadibot/sub-bot",
		Handler: func(ctx context.Context, c *command.Ctx) error {
			target := ""

			if len(c.Args) > 0 && strings.TrimSpace(c.Args[0]) != "" {
				target = strings.TrimSpace(c.ArgStr())
			} else if c.SubBot {
				// Bila dipanggil langsung di dalam sub-bot tanpa argumen, targetkan sub-bot itu sendiri
				if c.Client != nil && c.Client.Store != nil && c.Client.Store.ID != nil {
					target = c.Client.Store.ID.User
				} else {
					target = c.SenderPhone()
				}
			}

			// Pengirim dianggap berhak (isOwner) jika:
			// 1. Owner utama bot (command.IsOwner)
			// 2. Pesan dikirim dari akun sub-bot itu sendiri (c.Evt.Info.IsFromMe)
			isOwner := command.IsOwner(c.Evt)
			if c.SubBot && c.Evt != nil && c.Evt.Info.IsFromMe {
				isOwner = true
			}

			cands := identity.Candidates(c.Evt)
			if phone := c.SenderPhone(); phone != "" {
				cands = append(cands, phone)
			}

			err := jadibot.Stop(ctx, target, c.Sender(), isOwner, cands...)
			if err != nil {
				_, replyErr := c.Reply(ctx, "❌ "+err.Error())
				return replyErr
			}

			_, err = c.Reply(ctx, "✅ Sesi jadibot berhasil dihentikan dan dihapus.")
			return err
		},
	})
}

