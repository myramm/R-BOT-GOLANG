package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/energy"
	cmdfunc "rbot/cmd/func"
)

func init() {
	command.Register(&command.Command{
		Name:        "addenergi",
		Category:    "Owner",
		Alias:       []string{"addenergy", "resetenergi", "resetenergy"},
		Description: "Kelola energi user (owner). .addenergi <tag/nomor> <jumlah>, .resetenergi <tag/nomor|--all>",
		OwnerOnly:   true,
		Handler:     addEnergiHandler,
	})
}

func addEnergiHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()
	isReset := strings.HasPrefix(c.InvokedAs, "reset")

	if isReset && cmdfunc.FirstArgMatch(c.Args, cmdfunc.ReAll) != "" {
		if count := energy.ResetAll(); count > 0 {
			_, err := c.Reply(ctx, fmt.Sprintf("♻️ Energi *%d user* di-reset — jatah harian penuh lagi, semua bonus dihapus.", count))
			return err
		}
		_, err := c.Reply(ctx, "♻️ Belum ada data energi yang perlu di-reset.")
		return err
	}

	target := cmdfunc.ResolveTargetID(c)
	if target == "" {
		_, err := c.Reply(ctx, fmt.Sprintf(
			"Pakai:\n• *%saddenergi @tag 50* — kasih bonus energi (atau reply / nomor)\n• *%sresetenergi @tag* — reset energi user jadi penuh\n• *%sresetenergi --all* — reset energi SEMUA user",
			mp, mp, mp))
		return err
	}

	if isReset {
		energy.Reset(target)
		_, err := c.Reply(ctx, fmt.Sprintf("♻️ Energi *%s* di-reset — jatah harian penuh lagi, bonus dihapus.", target))
		return err
	}

	amount, _ := strconv.Atoi(cmdfunc.FirstArgMatch(c.Args, cmdfunc.ReSignedDigits))
	if amount == 0 {
		_, err := c.Reply(ctx, fmt.Sprintf("Berapa energi? Contoh: *%saddenergi @tag 50* (boleh minus buat mengurangi).", mp))
		return err
	}
	newBonus, _ := energy.AddBonus(target, amount)
	verb := strconv.Itoa(amount)
	if amount > 0 {
		verb = "+" + verb
	}
	_, err := c.Reply(ctx, fmt.Sprintf("⚡ Bonus energi *%s* diubah %s.\nTotal bonus sekarang: *%d* ⚡ (di atas jatah harian).", target, verb, newBonus))
	return err
}
