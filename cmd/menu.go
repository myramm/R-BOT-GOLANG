package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"rbot/brain/command"
	"rbot/brain/config"
	cmdfunc "rbot/cmd/func"
)

func init() {
	command.Register(&command.Command{
		Name:        "menu",
		Category:    "Info",
		Alias:       []string{"allmenu", "help", "m"},
		Description: "Menampilkan daftar menu dan perintah bot",
		Handler:     menuHandler,
	})
}

var menuCategoryTitle = map[string]string{
	"ai":         "🤖 AI & SMART BOT",
	"downloader": "📥 DOWNLOADER & MEDIA",
	"converter":  "🎨 MEDIA & CONVERTER",
	"info":       "ℹ️ INFORMASI & UTILITY",
	"grup":       "👥 PENGELOLAAN GRUP",
	"jadibot":    "🧬 JADIBOT CLONING",
	"owner":      "🔒 PERINTAH OWNER",
}

var menuCategoryOrder = []string{"ai", "downloader", "converter", "info", "grup", "jadibot", "owner"}

func mapCategoryKey(cat string) string {
	c := strings.ToLower(cat)
	switch {
	case strings.Contains(c, "ai"):
		return "ai"
	case strings.Contains(c, "download") || strings.Contains(c, "media"):
		return "downloader"
	case strings.Contains(c, "sticker") || strings.Contains(c, "convert") || strings.Contains(c, "maker"):
		return "converter"
	case strings.Contains(c, "grup") || strings.Contains(c, "group"):
		return "grup"
	case strings.Contains(c, "jadibot"):
		return "jadibot"
	case strings.Contains(c, "owner"):
		return "owner"
	default:
		return "info"
	}
}

func menuHandler(ctx context.Context, c *command.Ctx) error {
	owner := command.IsOwner(c.Evt) && !c.SubBot
	prefix := config.MainPrefix()

	groups := map[string][]*command.Command{}
	general, ownerCount := 0, 0
	for _, cmd := range command.All() {
		if cmd.OwnerOnly {
			ownerCount++
		} else {
			general++
		}
		if cmd.OwnerOnly && !owner {
			continue
		}
		k := mapCategoryKey(cmd.Category)
		groups[k] = append(groups[k], cmd)
	}
	for k := range groups {
		sort.Slice(groups[k], func(i, j int) bool { return groups[k][i].Name < groups[k][j].Name })
	}

	pushName := c.Evt.Info.PushName
	if pushName == "" {
		pushName = "Pengguna WhatsApp"
	}
	status := "👤 Public User"
	if owner {
		status = "👑 Owner Bot"
	}

	var b strings.Builder
	b.WriteString("╭━━━━━━━━━━━━━━━━━━━━━━━━━━╮\n")
	b.WriteString("┃  ✦ *" + config.C.BotName + " OFFICIAL DASHBOARD* ✦\n")
	b.WriteString("├──────────────────────────┤\n")
	b.WriteString("┃ 👤 *Pengguna:* " + pushName + "\n")
	b.WriteString("┃ 🔰 *Status:* " + status + "\n")
	b.WriteString("┃ ⏱️ *Uptime:* " + cmdfunc.FormatUptime(time.Since(cmdfunc.StartTime)) + "\n")
	ownerExtra := ""
	if owner {
		ownerExtra = fmt.Sprintf(" (+%d owner)", ownerCount)
	}
	b.WriteString(fmt.Sprintf("┃ 🧩 *Total Perintah:* %d%s\n", general, ownerExtra))
	b.WriteString("┃ ⌨️ *Prefix:* " + strings.Join(config.C.Prefix, " ") + "\n")
	b.WriteString("╰━━━━━━━━━━━━━━━━━━━━━━━━━━╯\n\n")

	for _, key := range menuCategoryOrder {
		list := groups[key]
		if len(list) == 0 {
			continue
		}
		b.WriteString("╭───〔 " + menuCategoryTitle[key] + " 〕\n")
		for _, cmd := range list {
			aliasStr := ""
			if len(cmd.Alias) > 0 {
				n := cmd.Alias
				if len(n) > 2 {
					n = n[:2]
				}
				parts := make([]string, len(n))
				for i, a := range n {
					parts[i] = prefix + a
				}
				aliasStr = " _(" + strings.Join(parts, ", ") + ")_"
			}
			b.WriteString("│ ✦ *" + prefix + cmd.Name + "*" + aliasStr + "\n")
			if cmd.Description != "" {
				b.WriteString("│   └ _" + cmd.Description + "_\n")
			}
		}
		b.WriteString("╰──────────────────────────\n\n")
	}
	b.WriteString("_Ketik perintah langsung, contoh: *" + prefix + "ping*_")

	_, err := c.Reply(ctx, b.String())
	return err
}
