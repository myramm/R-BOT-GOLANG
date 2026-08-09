package cmd

import (
	"context"

	"go.mau.fi/whatsmeow/types"

	"rbot/brain/command"
	cmdfunc "rbot/cmd/func"
)

// del.go: hapus (revoke) pesan yang di-reply. Pesan bot sendiri selalu bisa;
// pesan orang lain butuh pengirim admin & bot admin. Port del.js.
// whatsmeow BuildRevoke(chat, sender, id): sender kosong → hapus pesan sendiri;
// sender = JID pengirim asli → hapus pesan orang lain (butuh bot admin).

func init() {
	command.Register(&command.Command{
		Name:        "del",
		Category:    "Grup",
		Alias:       []string{"delete", "unsend"},
		Description: "Hapus pesan yang di-reply. Pesan bot sendiri selalu bisa; pesan orang lain butuh bot admin.",
		Handler:     delHandler,
	})
}

func delHandler(ctx context.Context, c *command.Ctx) error {
	if c.SubBot {
		_, err := c.Reply(ctx, "Command grup tidak tersedia lewat sub-bot.")
		return err
	}

	ci := c.ContextInfo()
	stanzaID := ci.GetStanzaID()
	if stanzaID == "" {
		_, err := c.Reply(ctx, "Reply dulu pesan yang mau dihapus, lalu ketik .del")
		return err
	}

	// Tentukan pengirim pesan yang di-reply; kosong/berisi-bot → pesan sendiri.
	var target types.JID
	isOwn := true
	if p := ci.GetParticipant(); p != "" {
		if j, err := types.ParseJID(p); err == nil {
			if !cmdfunc.BotJIDs(c)[cmdfunc.BareID(j)] {
				target = j
				isOwn = false
			}
		}
	}

	// Untuk pesan orang lain di grup: butuh pengirim admin & bot admin.
	if c.IsGroup() && !isOwn {
		info, err := c.Client.GetGroupInfo(ctx, c.Chat())
		if err != nil {
			_, e := c.Reply(ctx, "Gagal mengambil data grup. Coba lagi sebentar lagi.")
			return e
		}
		if !cmdfunc.SenderIsAdmin(c, info) {
			_, e := c.Reply(ctx, "Khusus admin grup ya (untuk menghapus pesan orang lain).")
			return e
		}
		if !cmdfunc.BotIsAdmin(c, info) {
			_, e := c.Reply(ctx, "Jadikan bot admin dulu biar bisa menghapus pesan anggota lain. (Pesan bot sendiri tetap bisa dihapus tanpa admin.)")
			return e
		}
	}

	revoke := c.Client.BuildRevoke(c.Chat(), target, stanzaID)
	if _, err := c.Client.SendMessage(ctx, c.Chat(), revoke); err != nil {
		_, e := c.Reply(ctx, "Gagal menghapus pesan: "+err.Error())
		return e
	}
	return nil
}
