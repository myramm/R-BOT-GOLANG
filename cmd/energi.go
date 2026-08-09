package cmd

import (
	"context"
	"fmt"
	"math"
	"strings"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/econ"
	"rbot/brain/energy"
	"rbot/brain/premium"
)

// energi.go: cek sisa energi + cara mengisinya. Port energi.js. Command info
// (tidak menarik energi). Member grup official dilihat lewat command.MemberGrupHook
// (nil-safe: grupofficial belum diport → dianggap bukan member).

// heavyEnergi adalah fitur berat yang biayanya ditampilkan (urutan seperti Node).
var heavyEnergi = []string{"hd", "smooth", "ai", "play", "download", "sticker"}

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
		_, err := c.Reply(ctx, fmt.Sprintf("⚡ *Energi tak terbatas* — semua fitur bebas dipakai.\n\n"+
			"_Nanti saat mode energi diaktifkan: tiap user dapat jatah harian, "+
			"isi ulang lewat %sistirahat atau %smakan hasil tani/ternak._", mp, mp))
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
	baris = append(baris, "", "*Biaya per fitur:*", strings.Join(costLines, "\n"))

	var restLines []string
	for _, m := range econ.StatusRest(c.Evt) {
		status := "✅ siap"
		if !m.Siap {
			status = "⏳ " + econ.FormatDurasi(m.Sisa)
		}
		restLines = append(restLines, fmt.Sprintf("%s *%s%s* — %s", m.Emoji, mp, m.ID, status))
	}

	makanan := econ.DaftarMakanan(c.Evt)
	if len(makanan) > 4 {
		makanan = makanan[:4]
	}
	makanLine := fmt.Sprintf("_Kulkas kosong — panen dulu di %stani / %sternak_", mp, mp)
	if len(makanan) > 0 {
		var ml []string
		for _, m := range makanan {
			ml = append(ml, fmt.Sprintf("%s %s ×%d — +%d ⚡", m.Emoji, m.ID, m.Jumlah, m.EnergyRestore))
		}
		makanLine = strings.Join(ml, "\n")
	}
	baris = append(baris, "", "*Cara isi ulang:*", strings.Join(restLines, "\n"), "", "*🍽️ Makanan kamu:*", makanLine)

	if !e.Premium {
		baris = append(baris, "", fmt.Sprintf("💎 Mau %d ⚡/hari yang ditumpuk? Ketik *%spremium*.", config.C.Energy.PremiumDaily(), mp))
	}
	if url := config.C.GrupOfficial.Invite; !member && url != "" {
		baris = append(baris, "", fmt.Sprintf("🎁 Join grup official buat hemat energi + hadiah:\n%s", url))
	}

	_, err := c.Reply(ctx, strings.Join(baris, "\n"))
	return err
}
