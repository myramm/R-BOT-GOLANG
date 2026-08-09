package cmd

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/types"

	"rbot/brain/command"
	"rbot/brain/stats"
	cmdfunc "rbot/cmd/func"
)

func init() {
	command.Register(&command.Command{
		Name:        "totalchat",
		Category:    "Info",
		Alias:       []string{"chatstats", "statistik"},
		Description: "Menampilkan statistik chat di grup/bot",
		Handler:     totalchatHandler,
	})
}

func totalchatHandler(ctx context.Context, c *command.Ctx) error {
	sub := ""
	if len(c.Args) > 0 {
		sub = strings.ToLower(strings.TrimSpace(c.Args[0]))
	}
	owner := command.IsOwner(c.Evt) && !c.SubBot

	if owner && sub == "grup" {
		groups := stats.TopGroups()
		if len(groups) == 0 {
			_, err := c.Reply(ctx, "Belum ada data grup.")
			return err
		}
		var b strings.Builder
		b.WriteString("🏆 *TOP 30 GRUP PENGGUNA BOT*\n\n")
		for i, group := range groups {
			name := group.JID
			if jid, err := types.ParseJID(group.JID); err == nil {
				if info, err := c.Client.GetGroupInfo(ctx, jid); err == nil && info.Name != "" {
					name = info.Name
				}
			}
			fmt.Fprintf(&b, "%d. *%s*\n   └ 🤖 Total Cmd: %d\n", i+1, name, group.Cmds)
		}
		_, err := c.Reply(ctx, b.String())
		return err
	}

	if owner && sub == "user" {
		users := stats.TopUsers()
		if len(users) == 0 {
			_, err := c.Reply(ctx, "Belum ada data user.")
			return err
		}
		var b strings.Builder
		mentions := make([]types.JID, 0, len(users))
		b.WriteString("🏆 *TOP 30 USER GLOBAL*\n\n")
		for i, user := range users {
			jid, err := types.ParseJID(user.JID)
			if err != nil {
				continue
			}
			mentions = append(mentions, jid)
			fmt.Fprintf(&b, "%d. @%s\n   └ 💬 Chat: %d | 🤖 Cmd: %d\n", i+1, cmdfunc.BareID(jid), user.Chats, user.Cmds)
		}
		_, err := c.ReplyMentions(ctx, b.String(), mentions)
		return err
	}

	if c.IsGroup() {
		info, infoErr := c.Client.GetGroupInfo(ctx, c.Chat())
		if infoErr != nil && !owner {
			_, err := c.Reply(ctx, "❌ Gagal mengambil data grup.")
			return err
		}
		if !owner && infoErr == nil {
			if !cmdfunc.SenderIsAdmin(c, info) {
				_, err := c.Reply(ctx, "❌ Fitur ini hanya dapat digunakan oleh Admin Grup.")
				return err
			}
		}
		users := stats.TopGroupUsers(c.Evt)
		if len(users) == 0 {
			_, err := c.Reply(ctx, "Belum ada data chat di grup ini.")
			return err
		}
		var b strings.Builder
		groupName := c.Chat().String()
		if infoErr == nil && info != nil && info.Name != "" {
			groupName = info.Name
		}
		b.WriteString("📊 *STATISTIK GRUP " + groupName + "*\n\n")
		mentions := make([]types.JID, 0, len(users))
		for i, user := range users {
			jid, err := types.ParseJID(user.JID)
			if err != nil {
				continue
			}
			mentions = append(mentions, jid)
			fmt.Fprintf(&b, "%d. @%s\n   └ 💬 Chat: %d | 🤖 Cmd: %d\n", i+1, cmdfunc.BareID(jid), user.Chats, user.Cmds)
		}
		_, err := c.ReplyMentions(ctx, b.String(), mentions)
		return err
	}

	mine := stats.UserStats(c.Evt)
	_, err := c.Reply(ctx, fmt.Sprintf("📊 *STATISTIK PRIBADI KAMU*\n\n💬 Total Chat: %d\n🤖 Total Command: %d", mine.Chats, mine.Cmds))
	return err
}

