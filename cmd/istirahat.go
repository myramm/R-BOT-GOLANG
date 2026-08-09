package cmd

import (
	"context"
	"fmt"
	"strings"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/econ"
)

// istirahat.go: isi ulang energi lewat istirahat/tidur. Port istirahat.js.

func init() {
	command.Register(&command.Command{
		Name:        "istirahat",
		Category:    "Energi",
		Alias:       []string{"rest", "tidur", "sleep"},
		Description: "Isi ulang energi dengan istirahat atau tidur",
		Handler:     istirahatHandler,
	})
}

func istirahatHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()

	if config.C.Energy.IsUnlimited() {
		_, err := c.Reply(ctx, "⚡ Energi sedang *tak terbatas* — belum perlu istirahat.")
		return err
	}

	// .tidur → mode tidur, .istirahat → mode singkat. Bisa juga .istirahat tidur.
	argMode := ""
	if len(c.Args) > 0 {
		argMode = strings.ToLower(c.Args[0])
	}
	modeID := "istirahat"
	if c.InvokedAs == "tidur" || c.InvokedAs == "sleep" || argMode == "tidur" {
		modeID = "tidur"
	}

	hasil := econ.Istirahat(c.Evt, modeID)

	if !hasil.OK && hasil.Err == "cooldown" {
		saran := fmt.Sprintf("\n\n_Sambil menunggu, makan hasil tani/ternak: %smakan_", mp)
		for _, m := range econ.StatusRest(c.Evt) {
			if m.ID != modeID && m.Siap {
				saran = fmt.Sprintf("\n\n%s *%s%s* sudah bisa dipakai sekarang.", m.Emoji, mp, m.ID)
				break
			}
		}
		_, err := c.Reply(ctx, fmt.Sprintf("%s Kamu baru saja %s.\n"+
			"Bisa lagi dalam *%s*.%s",
			hasil.Mode.Emoji, strings.ToLower(hasil.Mode.Nama), econ.FormatDurasi(hasil.Sisa), saran))
		return err
	}

	if !hasil.OK {
		_, err := c.Reply(ctx, "❌ "+hasil.Err)
		return err
	}

	if hasil.Unlimited {
		_, err := c.Reply(ctx, "⚡ Energi kamu tak terbatas — istirahat tidak diperlukan.")
		return err
	}

	if hasil.Penuh {
		_, err := c.Reply(ctx, fmt.Sprintf("%s Energi kamu sudah penuh (*%d* ⚡).\n"+
			"_Cooldown tidak dipakai, jadi bisa istirahat nanti saat benar-benar butuh._", hasil.Mode.Emoji, hasil.Bank))
		return err
	}

	plafon := ""
	if !hasil.PlafonTakHingga {
		plafon = fmt.Sprintf("/%d", hasil.Plafon)
	}
	terbuang := ""
	if hasil.Terbuang > 0 {
		terbuang = fmt.Sprintf("\n_%d ⚡ terbuang karena sudah penuh._", hasil.Terbuang)
	}
	namaLower := strings.ToLower(hasil.Mode.Nama)

	_, err := c.Reply(ctx, fmt.Sprintf("%s *%s selesai*\n\n"+
		"⚡ Energi +%d\n"+
		"Sekarang: *%d%s* ⚡%s\n\n"+
		"Bisa %s lagi dalam %s.\n"+
		"_Mau lebih cepat? Makan hasil panen: %smakan_",
		hasil.Mode.Emoji, hasil.Mode.Nama, hasil.Tambah, hasil.Bank, plafon, terbuang,
		namaLower, econ.FormatDurasi(hasil.Cooldown), mp))
	return err
}
