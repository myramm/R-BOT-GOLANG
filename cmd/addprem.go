package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/premium"
	cmdfunc "rbot/cmd/func"
)

func init() {
	command.Register(&command.Command{
		Name:        "addprem",
		Category:    "Owner",
		Alias:       []string{"delprem", "listprem"},
		Description: "Kelola premium (owner). .addprem <tag/nomor> <hari>, .delprem <tag/nomor>, .listprem",
		OwnerOnly:   true,
		Handler:     addPremHandler,
	})
}

func addPremHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()

	if c.InvokedAs == "listprem" {
		list := premium.List()
		if len(list) == 0 {
			_, err := c.Reply(ctx, "📋 Belum ada user premium.")
			return err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "💎 *Daftar Premium (%d)*\n\n", len(list))
		for i, p := range list {
			if i > 0 {
				b.WriteByte('\n')
			}
			fmt.Fprintf(&b, "%d. %s — s/d %s", i+1, p.ID, cmdfunc.FmtDate(p.Expiry))
		}
		_, err := c.Reply(ctx, b.String())
		return err
	}

	target := cmdfunc.ResolveTargetID(c)
	if target == "" {
		_, err := c.Reply(ctx, fmt.Sprintf(
			"Pakai:\n• *%saddprem @tag 30* (atau reply / nomor)\n• *%sdelprem @tag*\n• *%slistprem*",
			mp, mp, mp))
		return err
	}

	if c.InvokedAs == "delprem" {
		if premium.Remove(target) {
			_, err := c.Reply(ctx, fmt.Sprintf("✅ Premium %s dicabut.", target))
			return err
		}
		_, err := c.Reply(ctx, fmt.Sprintf("ℹ️ %s bukan premium.", target))
		return err
	}

	days, _ := strconv.Atoi(cmdfunc.FirstArgMatch(c.Args, cmdfunc.ReDigits))
	if days < 1 || days > 3650 {
		_, err := c.Reply(ctx, fmt.Sprintf("Berapa hari? Contoh: *%saddprem @tag 30* (1–3650 hari).", mp))
		return err
	}
	expiry, _ := premium.Add(target, float64(days))
	_, err := c.Reply(ctx, fmt.Sprintf("💎 %s jadi premium %d hari.\nAktif s/d *%s*.", target, days, cmdfunc.FmtDate(expiry)))
	return err
}
