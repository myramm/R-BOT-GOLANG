package cmd

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/types"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/settings"
	cmdfunc "rbot/cmd/func"
)

func init() {
	command.Register(&command.Command{
		Name:        "bangroup",
		Category:    "Group",
		Alias:       []string{"ban", "bangerup", "unban", "unbangerup", "listban"},
		Description: "Ban/unban user tertentu di grup ini (Admin/Owner Only). User yang di-ban tidak bisa memakai bot di grup ini.",
		Handler:     banGroupHandler,
	})
}

func banGroupHandler(ctx context.Context, c *command.Ctx) error {
	if !c.IsGroup() {
		_, err := c.Reply(ctx, "⚠️ Perintah ini cuma bisa dipakai di dalam grup.")
		return err
	}

	info, err := c.Client.GetGroupInfo(ctx, c.Chat())
	if err != nil {
		_, e := c.Reply(ctx, "❌ Gagal mengambil data grup. Coba lagi sebentar lagi.")
		return e
	}

	isOwner := command.IsOwner(c.Evt)
	isAdmin := cmdfunc.SenderIsAdmin(c, info)

	if !isOwner && !isAdmin {
		_, e := c.Reply(ctx, "⚠️ Khusus Admin Grup atau Owner Bot ya! Kamu bukan admin di grup ini.")
		return e
	}

	invoked := strings.ToLower(c.InvokedAs)
	argStr := strings.ToLower(strings.TrimSpace(c.ArgStr()))

	// Jika meminta list user yang di-ban di grup ini
	if invoked == "listban" || argStr == "list" {
		bannedIDs := settings.GetGroupBannedUsers(c.Chat().String())
		if len(bannedIDs) == 0 {
			_, e := c.Reply(ctx, "📋 *Daftar Ban Grup:*\nTidak ada user yang di-ban di grup ini.")
			return e
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "🚫 *Daftar User Di-ban di Grup Ini (%d):*\n\n", len(bannedIDs))
		for i, id := range bannedIDs {
			fmt.Fprintf(&sb, "%d. @%s\n", i+1, id)
		}
		_, e := c.Reply(ctx, sb.String())
		return e
	}

	isUnban := invoked == "unban" || invoked == "unbangerup" ||
		argStr == "off" || argStr == "unban"

	// Ambil target user dari mention, reply, atau nomor telepon
	targetJIDs := cmdfunc.ResolveMemberTargets(c, info)
	if len(targetJIDs) == 0 {
		cmdName := config.MainPrefix() + invoked
		_, e := c.Reply(ctx, fmt.Sprintf("⚠️ Tag atau balas pesan user yang ingin di-%s.\n\n*Contoh:*\n• `%s @user`\n• Balas pesan user lalu ketik `%s`", invoked, cmdName, cmdName))
		return e
	}

	// Kumpulkan seluruh kandidat bare ID (JID, LID, PhoneNumber) dari participant target
	var allTargetBareIDs []string
	var mentionTags []string

	for _, jid := range targetJIDs {
		want := map[string]bool{cmdfunc.BareID(jid): true}
		if p := cmdfunc.FindParticipant(info, want); p != nil {
			if id := cmdfunc.BareID(p.JID); id != "" {
				allTargetBareIDs = append(allTargetBareIDs, id)
			}
			if id := cmdfunc.BareID(p.PhoneNumber); id != "" {
				allTargetBareIDs = append(allTargetBareIDs, id)
			}
			if id := cmdfunc.BareID(p.LID); id != "" {
				allTargetBareIDs = append(allTargetBareIDs, id)
			}
		} else {
			if id := cmdfunc.BareID(jid); id != "" {
				allTargetBareIDs = append(allTargetBareIDs, id)
			}
		}
		mentionTags = append(mentionTags, cmdfunc.MentionTag(jid))
	}

	groupID := c.Chat().String()
	tagStr := strings.Join(mentionTags, " ")

	if isUnban {
		if err := settings.SetUserBannedInGroup(groupID, allTargetBareIDs, false); err != nil {
			_, e := c.Reply(ctx, "❌ Gagal meng-unban user.")
			return e
		}
		c.React(ctx, "✅")
		_, e := c.ReplyMentions(ctx, fmt.Sprintf("✅ *User %s Berhasil Di-unban di Grup Ini!*\n\nSekarang user tersebut dapat menggunakan perintah bot kembali di grup ini.", tagStr), targetJIDs)
		return e
	}

	// Jangan izinkan mem-ban bot owner atau admin grup sendiri
	for _, jid := range targetJIDs {
		want := map[string]bool{cmdfunc.BareID(jid): true}
		if p := cmdfunc.FindParticipant(info, want); p != nil && (p.IsAdmin || p.IsSuperAdmin) {
			_, e := c.Reply(ctx, "⚠️ Admin grup tidak bisa di-ban di grup ini!")
			return e
		}
	}

	if err := settings.SetUserBannedInGroup(groupID, allTargetBareIDs, true); err != nil {
		_, e := c.Reply(ctx, "❌ Gagal mem-ban user.")
		return e
	}

	c.React(ctx, "🚫")
	_, e := c.ReplyMentions(ctx, fmt.Sprintf("🚫 *User %s Berhasil Di-ban di Grup Ini!*\n\nUser tersebut tidak dapat menggunakan bot di grup ini.\n_(User masih bisa memakai bot di grup lain & PM)_", tagStr), targetJIDs)
	return e
}

// ensure JID parse helper
func parseJIDs(raws []string) []types.JID {
	var out []types.JID
	for _, r := range raws {
		if j, err := types.ParseJID(r); err == nil {
			out = append(out, j)
		}
	}
	return out
}
