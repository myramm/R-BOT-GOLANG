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
		Name:        "blacklist",
		Category:    "Owner",
		Alias:       []string{"blocklist", "unblacklist", "unblocklist", "listblacklist", "listblocklist"},
		Description: "Blacklist global user 100% dari pemakaian bot di mana pun (Owner Only).",
		OwnerOnly:   true,
		Handler:     blacklistHandler,
	})
}

func blacklistHandler(ctx context.Context, c *command.Ctx) error {
	invoked := strings.ToLower(c.InvokedAs)
	argStr := strings.ToLower(strings.TrimSpace(c.ArgStr()))

	// Jika meminta list blacklist
	if invoked == "listblacklist" || invoked == "listblocklist" || argStr == "list" {
		listMap := settings.GetGlobalBlacklist()
		if len(listMap) == 0 {
			_, err := c.Reply(ctx, "📋 *Global Blacklist:* Tidak ada user yang di-blacklist.")
			return err
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "⛔ *Global Blacklist User (%d):*\n\n", len(listMap))
		i := 1
		var mentions []types.JID
		for id := range listMap {
			fmt.Fprintf(&sb, "%d. @%s\n", i, id)
			mentions = append(mentions, types.NewJID(id, types.DefaultUserServer))
			i++
		}
		_, err := c.ReplyMentions(ctx, sb.String(), mentions)
		return err
	}

	isUnblacklisted := invoked == "unblacklist" || invoked == "unblocklist" ||
		argStr == "off" || argStr == "unblacklist" || argStr == "unblocklist"

	// Ambil target user dari mention, reply, atau args
	var targetIDs []string
	var mentions []types.JID

	if c.IsGroup() {
		info, err := c.Client.GetGroupInfo(ctx, c.Chat())
		if err == nil {
			targetJIDs := cmdfunc.ResolveMemberTargets(c, info)
			for _, jid := range targetJIDs {
				want := map[string]bool{cmdfunc.BareID(jid): true}
				if p := cmdfunc.FindParticipant(info, want); p != nil {
					if id := cmdfunc.BareID(p.JID); id != "" {
						targetIDs = append(targetIDs, id)
					}
					if id := cmdfunc.BareID(p.PhoneNumber); id != "" {
						targetIDs = append(targetIDs, id)
					}
					if id := cmdfunc.BareID(p.LID); id != "" {
						targetIDs = append(targetIDs, id)
					}
				} else {
					if id := cmdfunc.BareID(jid); id != "" {
						targetIDs = append(targetIDs, id)
					}
				}
				mentions = append(mentions, jid)
			}
		}
	}

	if len(targetIDs) == 0 {
		phoneJIDs := cmdfunc.ResolvePhoneTargets(c)
		for _, j := range phoneJIDs {
			if id := cmdfunc.BareID(j); id != "" {
				targetIDs = append(targetIDs, id)
				mentions = append(mentions, j)
			}
		}
	}

	if len(targetIDs) == 0 {
		cmdName := config.MainPrefix() + invoked
		_, err := c.Reply(ctx, fmt.Sprintf("⚠️ Tag, balas pesan user, atau masukkan nomor HP yang ingin di-blacklist.\n\n*Contoh:*\n• `%s @user`\n• `%s 628123456789`\n• Balas pesan user lalu ketik `%s`", cmdName, cmdName, cmdName))
		return err
	}

	// Dedup IDs
	seen := map[string]bool{}
	var uniqueIDs []string
	for _, id := range targetIDs {
		if id != "" && !seen[id] {
			seen[id] = true
			uniqueIDs = append(uniqueIDs, id)
		}
	}

	if isUnblacklisted {
		for _, id := range uniqueIDs {
			_ = settings.SetUserBlacklisted(id, false)
		}
		c.React(ctx, "✅")
		var tags []string
		for _, j := range mentions {
			tags = append(tags, cmdfunc.MentionTag(j))
		}
		tagText := strings.Join(tags, " ")
		if tagText == "" {
			tagText = strings.Join(uniqueIDs, ", ")
		}
		_, err := c.ReplyMentions(ctx, fmt.Sprintf("✅ *User %s Berhasil Dihapus dari Global Blacklist!*\nUser tersebut dapat menggunakan bot kembali.", tagText), mentions)
		return err
	}

	// Blacklist target IDs
	for _, id := range uniqueIDs {
		_ = settings.SetUserBlacklisted(id, true)
	}
	c.React(ctx, "⛔")

	var sb strings.Builder
	// Pisahkan phone number dan LID dari uniqueIDs
	var phones []string
	var lids []string
	for _, id := range uniqueIDs {
		if len(id) >= 15 {
			lids = append(lids, id)
		} else {
			phones = append(phones, id)
		}
	}

	if len(phones) > 0 {
		for i, ph := range phones {
			if i > 0 {
				sb.WriteString("\n\n")
			}
			fmt.Fprintf(&sb, "%s ⛔ Blacklisted (100%%)", ph)
			if i < len(lids) {
				fmt.Fprintf(&sb, "\n||%s||", lids[i])
			}
		}
	} else if len(lids) > 0 {
		for i, lid := range lids {
			if i > 0 {
				sb.WriteString("\n\n")
			}
			fmt.Fprintf(&sb, "%s ⛔ Blacklisted (100%%)\n||%s||", lid, lid)
		}
	} else {
		var tags []string
		for _, j := range mentions {
			tags = append(tags, cmdfunc.MentionTag(j))
		}
		tagText := strings.Join(tags, " ")
		if tagText == "" {
			tagText = strings.Join(uniqueIDs, ", ")
		}
		fmt.Fprintf(&sb, "%s ⛔ Blacklisted (100%%)", tagText)
	}

	_, err := c.ReplyMentions(ctx, sb.String(), mentions)
	return err
}
