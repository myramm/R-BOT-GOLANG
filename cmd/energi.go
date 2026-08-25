package cmd

import (
	"context"
	"fmt"
	"math"
	"strings"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/energy"
	"rbot/brain/hentailimit"
	"rbot/brain/premium"
)

// energi.go: cek sisa energi + cara mengisinya.
var heavyEnergi = []string{"hd", "smooth", "ai", "play", "download", "sticker"}

// Kuota harian fitur .hentai untuk user premium (info di pesan .energi).
const (
	premHentaiLuarGrup = 100
	premHentaiDiGrup   = 300
)

// infoHentai menyusun blok info kuota fitur .hentai (free vs premium).
func infoHentai() string {
	mp := config.MainPrefix()
	return fmt.Sprintf(
		"━━━━━━━━━━━━━━━━━━\n"+
			"🎬 *INFO FITUR HENTAI* 🎬\n"+
			"━━━━━━━━━━━━━━━━━━\n"+
			"🆓 Free: %dx/hari luar grup • %dx/hari grup official (max 480p)\n"+
			"💎 Premium: %dx/hari luar grup • %dx/hari grup official + semua kualitas!\n\n"+
			"_Biarkan nonton tanpa henti! Gabung_ *Prem* _sekarang →_ *%spremium* 🚀",
		hentailimit.LimitLuarGrup, hentailimit.LimitDiGrup,
		premHentaiLuarGrup, premHentaiDiGrup, mp)
}

func init() {
	command.Register(&command.Command{
		Name:        "energi",
		Category:    "Info",
		Alias:       []string{"energy", "limit"},
		Description: "Cek sisa energi kamu dan cara mengisinya",
		Handler:     energiHandler,
	})
}

func energiHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()
	e := energy.Get(c.Evt)

	member := false
	if command.MemberGrupHook != nil {
		member = command.MemberGrupHook(ctx, c.Client, c.Evt)
	}

	if command.IsOwner(c.Evt) {
		_, err := c.Reply(ctx, "👑 *Kamu OWNER* — energi tak terbatas ⚡")
		return err
	}

	if e.Unlimited {
		_, err := c.Reply(ctx, "⚡ *Energi tak terbatas* — semua fitur bebas dipakai.")
		return err
	}

	maks := e.Max
	if maks <= 0 {
		maks = e.Bank
	}
	isi := 0
	if maks > 0 {
		isi = int(math.Round(float64(e.Bank) / float64(maks) * 10))
	}
	bar := strings.Repeat("█", min(10, max(0, isi))) + strings.Repeat("░", max(0, 10-isi))

	baris := []string{"⚡ *Energi Kamu*", "", fmt.Sprintf("%s  *%d* ⚡", bar, e.Bank)}

	if e.Premium {
		sisaHari := int(math.Ceil(float64(premium.Remaining(c.Evt)) / 86400000))
		baris = append(baris, "",
			fmt.Sprintf("💎 *PREMIUM* — sisa %d hari", sisaHari),
			fmt.Sprintf("Jatah harian: +%d ⚡ *ditumpuk*, tidak hangus.", config.C.Energy.PremiumDaily()),
			fmt.Sprintf("_Saat premium habis, sisa energi dipotong pajak %d%%._", int(math.Round(config.C.Energy.PajakExpired()*100))))
	} else {
		bonus := ""
		if e.Bonus != 0 {
			bonus = fmt.Sprintf(" (+%d bonus owner)", e.Bonus)
		}
		baris = append(baris, "", fmt.Sprintf("Plafon harian: %d%s ⚡", e.Limit, bonus))
	}

	if member {
		baris = append(baris, "", fmt.Sprintf("🎁 *Member grup official* — hemat %d%% energi", int(math.Round(config.C.Energy.DiskonGrup()*100))))
	}

	costLines := make([]string, 0, len(heavyEnergi))
	for _, name := range heavyEnergi {
		dasar := energy.EnergyCost(name)
		bayar := energy.BiayaEfektif(name, member)
		tanda := ""
		if bayar < dasar {
			tanda = fmt.Sprintf(" ~%d~ →", dasar)
		}
		costLines = append(costLines, fmt.Sprintf("• %s%s —%s %d ⚡", mp, name, tanda, bayar))
	}
	baris = append(baris, "", fmt.Sprintf("💤 *Isi Ulang Energi:* Ketik *%sistirahat* (pilihan: nap, tidur, nyenyak)", mp))

	if !e.Premium {
		baris = append(baris, "", fmt.Sprintf("💎 Mau %d ⚡/hari yang ditumpuk? Ketik *%spremium*.", config.C.Energy.PremiumDaily(), mp))
	}
	if url := config.C.GrupOfficial.Invite; !member && url != "" {
		baris = append(baris, "", fmt.Sprintf("🎁 Join grup official buat hemat energi:\n%s", url))
	}

	baris = append(baris, "", infoHentai())

	_, err := c.Reply(ctx, strings.Join(baris, "\n"))
	return err
}
