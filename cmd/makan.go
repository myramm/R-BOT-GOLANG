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

// makan.go: makan hasil tani/ternak untuk isi energi. Port makan.js.

func init() {
	command.Register(&command.Command{
		Name:        "makan",
		Category:    "Energi",
		Alias:       []string{"eat", "santap"},
		Description: "Makan hasil tani/ternak untuk mengisi energi",
		Handler:     makanHandler,
	})
}

func makanHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()
	itemID := ""
	if len(c.Args) > 0 {
		itemID = strings.ToLower(c.Args[0])
	}

	// Tanpa argumen → tampilkan isi kulkas.
	if itemID == "" {
		daftar := econ.DaftarMakanan(c.Evt)
		if len(daftar) == 0 {
			_, err := c.Reply(ctx, fmt.Sprintf("🍽️ *Kulkas kamu kosong.*\n\n"+
				"Cara dapat makanan:\n"+
				"🌱 *%stani* — tanam sayur & buah\n"+
				"🐔 *%sternak* — pelihara hewan, ambil telur/susu/daging\n"+
				"🛒 *%stoko* — beli bibit & hewan", mp, mp, mp))
			return err
		}
		var list []string
		for _, m := range daftar {
			list = append(list, fmt.Sprintf("%s *%s* ×%d — +%d ⚡/porsi", m.Emoji, m.ID, m.Jumlah, m.EnergyRestore))
		}
		_, err := c.Reply(ctx, fmt.Sprintf("🍽️ *Makanan kamu*\n\n%s\n\n"+
			"Cara makan: *%smakan <nama> [jumlah]*\n"+
			"Contoh: *%smakan %s 2*", strings.Join(list, "\n"), mp, mp, daftar[0].ID))
		return err
	}

	if config.C.Energy.IsUnlimited() {
		_, err := c.Reply(ctx, "⚡ Energi sedang *tak terbatas* — makan belum menambah apa-apa sekarang.")
		return err
	}

	jumlah := 1
	if len(c.Args) > 1 {
		jumlah, _ = strconv.Atoi(c.Args[1]) // gagal → 0 → ditolak "Jumlah minimal 1."
	}
	hasil := econ.Makan(c.Evt, itemID, jumlah)
	if !hasil.OK {
		_, err := c.Reply(ctx, "❌ "+hasil.Err)
		return err
	}

	if hasil.Unlimited {
		_, err := c.Reply(ctx, fmt.Sprintf("%s Kamu makan %d× %s. Energi kamu memang tak terbatas ⚡", hasil.Item.Emoji, hasil.Jumlah, hasil.Item.Name))
		return err
	}

	plafon := ""
	if !hasil.PlafonTakHingga {
		plafon = fmt.Sprintf("/%d", hasil.Plafon)
	}
	terbuang := ""
	if hasil.Terbuang > 0 {
		terbuang = fmt.Sprintf("\n_%d ⚡ terbuang karena energi sudah penuh._", hasil.Terbuang)
	}

	_, err := c.Reply(ctx, fmt.Sprintf("%s *Nyam!* Kamu makan %d× %s\n\n"+
		"⚡ Energi +%d\n"+
		"Sekarang: *%d%s* ⚡%s", hasil.Item.Emoji, hasil.Jumlah, hasil.Item.Name, hasil.Tambah, hasil.Bank, plafon, terbuang))
	return err
}
