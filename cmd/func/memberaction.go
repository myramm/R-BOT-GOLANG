// memberaction.go: factory handler kick/promote/demote — aksi admin ke anggota
// grup. Port makeMemberAction (group.js): gerbang admin → resolusi target →
// buang target-bot → UpdateGroupParticipants.
package cmdfunc

import (
	"context"

	"go.mau.fi/whatsmeow"

	"rbot/brain/command"
)

// RegisterMemberAction mendaftarkan satu command aksi-anggota (kick/promote/demote).
func RegisterMemberAction(name string, alias []string, verb string, action whatsmeow.ParticipantChange, desc string) {
	command.Register(&command.Command{
		Name:        name,
		Category:    "Grup",
		Alias:       alias,
		Description: desc,
		Handler:     memberActionHandler(name, verb, action),
	})
}

func memberActionHandler(name, verb string, action whatsmeow.ParticipantChange) command.Handler {
	return func(ctx context.Context, c *command.Ctx) error {
		gate := EnsureAdminContext(ctx, c)
		if gate.Err != "" {
			_, err := c.Reply(ctx, gate.Err)
			return err
		}

		targets := ResolveMemberTargets(c, gate.Info)
		if len(targets) == 0 {
			_, err := c.Reply(ctx, "Tag / reply / tulis nomor orang yang mau di-"+name+". Contoh: ."+name+" @user")
			return err
		}

		// Buang bot sendiri dari daftar target.
		bot := BotJIDs(c)
		clean := targets[:0:0]
		for _, t := range targets {
			if !bot[BareID(t)] {
				clean = append(clean, t)
			}
		}
		if len(clean) == 0 {
			_, err := c.Reply(ctx, "Gak bisa melakukan aksi itu ke bot sendiri.")
			return err
		}

		if _, err := c.Client.UpdateGroupParticipants(ctx, c.Chat(), clean, action); err != nil {
			_, e := c.Reply(ctx, "Gagal "+verb+": "+err.Error())
			return e
		}
		_, err := c.ReplyMentions(ctx, "✅ Berhasil "+verb+": "+MentionList(clean), clean)
		return err
	}
}

// RegisterAnnounceToggle mendaftarkan command open/close (mode announce grup).
// announce=true → hanya admin bisa kirim (tutup); false → semua bisa (buka).
func RegisterAnnounceToggle(name string, alias []string, announce bool, okText, desc string) {
	command.Register(&command.Command{
		Name:        name,
		Category:    "Grup",
		Alias:       alias,
		Description: desc,
		Handler: func(ctx context.Context, c *command.Ctx) error {
			gate := EnsureAdminContext(ctx, c)
			if gate.Err != "" {
				_, err := c.Reply(ctx, gate.Err)
				return err
			}
			if err := c.Client.SetGroupAnnounce(ctx, c.Chat(), announce); err != nil {
				_, e := c.Reply(ctx, "Gagal mengubah pengaturan grup: "+err.Error())
				return e
			}
			_, err := c.Reply(ctx, okText)
			return err
		},
	})
}
