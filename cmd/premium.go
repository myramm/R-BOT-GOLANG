package cmd

import (
	"context"
	"fmt"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/premium"
)

const premiumBenefits = "✨ *Keuntungan Premium:*\n" +
	"• ⚡ Energi tanpa batas — bebas limit harian command berat\n" +
	"• 🎬 Kualitas lebih tinggi — HD s/d 4K, smooth s/d 120fps, durasi lebih panjang\n" +
	"• 🚫 Bebas iklan/sponsor"

func init() {
	command.Register(&command.Command{
		Name:        "premium",
		Category:    "Info",
		Alias:       []string{"prem"},
		Description: "Cek status & keuntungan premium kamu",
		Handler:     premiumHandler,
	})
}

func premiumHandler(ctx context.Context, c *command.Ctx) error {
	if premium.IsPremium(c.Evt) {
		if command.IsOwner(c.Evt) {
			_, err := c.Reply(ctx, "👑 *Kamu OWNER — Premium selamanya* ✅\n\n"+premiumBenefits)
			return err
		}
		const day = int64(24 * 3600 * 1000)
		ms := premium.Remaining(c.Evt)
		days, hours := ms/day, (ms%day)/(3600*1000)
		sisa := fmt.Sprintf("%d jam", hours)
		if days > 0 {
			sisa = fmt.Sprintf("%d hari %d jam", days, hours)
		}
		_, err := c.Reply(ctx, fmt.Sprintf("💎 *Kamu PREMIUM* ✅\nSisa masa aktif: *%s*\n\n%s", sisa, premiumBenefits))
		return err
	}

	_, err := c.Reply(ctx, fmt.Sprintf(
		"💎 *Status: Gratis*\n\n%s\n\n_Mau premium? Hubungi owner:_ *%sowner*",
		premiumBenefits, config.MainPrefix()))
	return err
}
