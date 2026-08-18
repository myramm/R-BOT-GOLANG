package cmd

import (
	"context"
	"strings"

	"rbot/brain/command"
	"rbot/brain/settings"
	cmdfunc "rbot/cmd/func"
)

func init() {
	command.Register(&command.Command{
		Name:        "mute",
		Category:    "Group",
		Alias:       []string{"mutegc", "bangc", "mutebot", "unmute", "unmutegc", "unbangc"},
		Description: "Mute/unmute bot di grup (Admin / Owner Only). Saat di-mute, hanya Admin & Owner yang bisa menggunakan bot.",
		Handler:     muteGCHandler,
	})
}

func muteGCHandler(ctx context.Context, c *command.Ctx) error {
	if !c.IsGroup() {
		_, err := c.Reply(ctx, "⚠️ Perintah ini cuma bisa dipakai di dalam grup.")
		return err
	}

	isOwner := command.IsOwner(c.Evt)
	isAdmin := false

	if !isOwner {
		info, err := c.Client.GetGroupInfo(ctx, c.Chat())
		if err != nil {
			_, e := c.Reply(ctx, "❌ Gagal mengambil data grup. Coba lagi sebentar lagi.")
			return e
		}
		if !cmdfunc.SenderIsAdmin(c, info) {
			_, e := c.Reply(ctx, "⚠️ Khusus Admin Grup atau Owner Bot ya! Kamu bukan admin di grup ini.")
			return e
		}
		isAdmin = true
	}

	invoked := strings.ToLower(c.InvokedAs)
	arg := strings.ToLower(strings.TrimSpace(c.ArgStr()))

	// Cek apakah perintah untuk unmute
	isUnmute := invoked == "unmute" || invoked == "unmutegc" || invoked == "unbangc" ||
		arg == "off" || arg == "unmute" || arg == "disable" || arg == "buka" || arg == "0"

	groupID := c.Chat().String()

	if isUnmute {
		if err := settings.SetGroupMuted(groupID, false); err != nil {
			_, e := c.Reply(ctx, "❌ Gagal mengubah status mute grup.")
			return e
		}
		c.React(ctx, "🔊")
		roleStr := "Owner Bot"
		if isAdmin && !isOwner {
			roleStr = "Admin Grup"
		}
		_, err := c.Reply(ctx, "🔊 *Bot Berhasil Di-unmute!*\n\nOleh: *"+roleStr+"*\nSekarang seluruh anggota grup dapat menggunakan perintah bot kembali.")
		return err
	}

	// Mute group
	if err := settings.SetGroupMuted(groupID, true); err != nil {
		_, e := c.Reply(ctx, "❌ Gagal mengubah status mute grup.")
		return e
	}

	c.React(ctx, "🔇")
	roleStr := "Owner Bot"
	if isAdmin && !isOwner {
		roleStr = "Admin Grup"
	}
	_, err := c.Reply(ctx, "🔇 *Bot Berhasil Di-mute di Grup Ini!*\n\nOleh: *"+roleStr+"*\nAnggota biasa tidak dapat menggunakan perintah bot.\nHanya *Admin Grup* dan *Owner Bot* yang dapat menggunakan bot.")
	return err
}
