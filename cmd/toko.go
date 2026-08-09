package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/econ"
)

// toko.go: katalog bibit, hewan, dan lahan. Port toko.js.

func init() {
	command.Register(&command.Command{
		Name:        "toko",
		Category:    "Ekonomi",
		Alias:       []string{"shop", "store"},
		Description: "Toko bibit, hewan ternak, dan lahan",
		Handler:     tokoHandler,
	})
}

func tokoHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()
	sub := ""
	if len(c.Args) > 0 {
		sub = strings.ToLower(c.Args[0])
	}

	// === toko bibit <nama> [jumlah] — stok disimpan sebagai "bibit:<id>" ===
	if sub == "bibit" {
		id := ""
		if len(c.Args) > 1 {
			id = strings.ToLower(c.Args[1])
		}
		jumlah := 1
		if len(c.Args) > 2 {
			jumlah, _ = strconv.Atoi(c.Args[2])
		}
		if id == "" {
			_, err := c.Reply(ctx, fmt.Sprintf("🌱 *Beli bibit:* %stoko bibit <nama> [jumlah]\n"+
				"Contoh: *%stoko bibit wortel 5*\n\n"+
				"_%stani tanam sudah otomatis memotong koin, jadi ini cuma buat stok di muka._", mp, mp, mp))
			return err
		}

		r := econ.BeliBibit(c.Evt, id, jumlah)
		if !r.OK {
			_, err := c.Reply(ctx, "❌ "+r.Err)
			return err
		}
		_, err := c.Reply(ctx, fmt.Sprintf("🌱 *%d× bibit %s* dibeli\n\n💰 Koin: %d (-%d)", r.Jumlah, r.Crop.Name, r.Coins, r.Total))
		return err
	}

	koin := 0
	if data := econ.InventoriOf(c.Evt); data != nil {
		koin = data.Coins
	}

	var listBibit []string
	for _, id := range econ.CropOrder {
		cr := econ.Crops[id]
		listBibit = append(listBibit, fmt.Sprintf("%s *%s* — %d koin · %s · jual %d · +%d ⚡", cr.Emoji, id, cr.Seed, econ.FormatDurasi(cr.GrowTime), cr.Sell, cr.EnergyRestore))
	}

	var listHewan []string
	for _, id := range econ.HewanOrder {
		h := econ.Hewan[id]
		listHewan = append(listHewan, fmt.Sprintf("%s *%s* — %d koin · pakan %s×%d · %s tiap %s", h.Emoji, id, h.Harga, h.Pakan, h.Porsi, h.ProdukID, econ.FormatDurasi(h.Siklus)))
	}

	_, err := c.Reply(ctx, fmt.Sprintf("🛒 *TOKO*  💰 %d koin\n\n"+
		"*🌱 Bibit Tanaman*\n%s\n\n"+
		"*🐔 Hewan Ternak*\n%s\n\n"+
		"*🟫 Lahan*\nLahan ke-4 mulai %d koin (maks %d)\n\n"+
		"*Cara beli:*\n"+
		"• *%stani tanam <no> <bibit>* — langsung tanam\n"+
		"• *%sternak beli <hewan> [n]*\n"+
		"• *%stani lahan* — tambah lahan",
		koin, strings.Join(listBibit, "\n"), strings.Join(listHewan, "\n"), econ.HargaLahan(3), econ.PlotMax, mp, mp, mp))
	return err
}
