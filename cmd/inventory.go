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

// inventory.go: lihat inventory, atau jual hasil tani/ternak (alias jual).
// Port inventory.js.

func init() {
	command.Register(&command.Command{
		Name:        "inventory",
		Category:    "Ekonomi",
		Alias:       []string{"inv", "tas", "jual"},
		Description: "Lihat inventory, atau jual hasil tani/ternak",
		Handler:     inventoryHandler,
	})
}

// asalTani true bila item berasal dari katalog tani (Crops).
func asalTani(id string) bool {
	_, ok := econ.Crops[id]
	return ok
}

func inventoryHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()

	// === .jual <item> [jumlah|all] ===
	if c.InvokedAs == "jual" {
		id := ""
		if len(c.Args) > 0 {
			id = strings.ToLower(c.Args[0])
		}
		if id == "" {
			data := econ.InventoriOf(c.Evt)
			var bisa []string
			if data != nil {
				for _, k := range append(append([]string{}, econ.CropOrder...), econ.ProdukOrder...) {
					if q := data.Inv[k]; q > 0 {
						if m, ok := econ.Makanan[k]; ok {
							bisa = append(bisa, fmt.Sprintf("%s *%s* ×%d — %d koin/pcs", m.Emoji, k, q, m.Sell))
						}
					}
				}
			}
			if len(bisa) == 0 {
				_, err := c.Reply(ctx, fmt.Sprintf("💵 Belum ada yang bisa dijual. Panen dulu: *%stani* / *%sternak*", mp, mp))
				return err
			}
			_, err := c.Reply(ctx, fmt.Sprintf("💵 *Jual:* %sjual <item> [jumlah|all]\n"+
				"Contoh: *%sjual wortel all*\n\n*Stok kamu:*\n%s", mp, mp, strings.Join(bisa, "\n")))
			return err
		}

		raw := "1"
		if len(c.Args) > 1 {
			raw = strings.ToLower(c.Args[1])
		}
		jumlah := -1 // all
		if raw != "all" && raw != "semua" {
			jumlah, _ = strconv.Atoi(raw)
		}

		var emoji, nama string
		var qty, dapat, coins int
		if asalTani(id) {
			r := econ.JualTani(c.Evt, id, jumlah)
			if !r.OK {
				_, err := c.Reply(ctx, "❌ "+r.Err)
				return err
			}
			emoji, nama, qty, dapat, coins = r.Crop.Emoji, r.Crop.Name, r.Jumlah, r.Dapat, r.Coins
		} else {
			r := econ.JualProduk(c.Evt, id, jumlah)
			if !r.OK {
				_, err := c.Reply(ctx, "❌ "+r.Err)
				return err
			}
			emoji, nama, qty, dapat, coins = r.Item.Emoji, r.Item.Name, r.Jumlah, r.Dapat, r.Coins
		}
		_, err := c.Reply(ctx, fmt.Sprintf("💵 *Terjual!*\n\n%s %s ×%d\n"+
			"💰 +%d koin → total *%d*", emoji, nama, qty, dapat, coins))
		return err
	}

	// === .inv ===
	data := econ.InventoriOf(c.Evt)
	if data == nil {
		_, err := c.Reply(ctx, "❌ Data inventory tidak terbaca.")
		return err
	}

	var sayur, buah, ternak, bibit []string
	for _, id := range econ.CropOrder {
		cr := econ.Crops[id]
		if q := data.Inv[id]; q > 0 {
			baris := fmt.Sprintf("%s *%s* ×%d — +%d ⚡", cr.Emoji, id, q, cr.EnergyRestore)
			if cr.Type == "buah" {
				buah = append(buah, baris)
			} else {
				sayur = append(sayur, baris)
			}
		}
		if q := data.Inv["bibit:"+id]; q > 0 {
			bibit = append(bibit, fmt.Sprintf("%s %s ×%d", cr.Emoji, cr.Name, q))
		}
	}
	for _, id := range econ.ProdukOrder {
		p := econ.Produk[id]
		if q := data.Inv[id]; q > 0 {
			baris := fmt.Sprintf("%s *%s* ×%d", p.Emoji, id, q)
			if p.EnergyRestore > 0 {
				baris += fmt.Sprintf(" — +%d ⚡", p.EnergyRestore)
			}
			ternak = append(ternak, baris)
		}
	}

	var bagian []string
	if len(sayur) > 0 {
		bagian = append(bagian, "*🥬 Sayuran*\n"+strings.Join(sayur, "\n"))
	}
	if len(buah) > 0 {
		bagian = append(bagian, "*🍎 Buah*\n"+strings.Join(buah, "\n"))
	}
	if len(ternak) > 0 {
		bagian = append(bagian, "*🥩 Hasil Ternak*\n"+strings.Join(ternak, "\n"))
	}
	if len(bibit) > 0 {
		bagian = append(bagian, "*🌱 Bibit*\n"+strings.Join(bibit, "\n"))
	}

	if len(bagian) == 0 {
		_, err := c.Reply(ctx, fmt.Sprintf("🎒 *Inventory kosong*  💰 %d koin\n\n"+
			"Mulai dari: *%stani tanam 1 wortel*", data.Coins, mp))
		return err
	}

	_, err := c.Reply(ctx, fmt.Sprintf("🎒 *Inventory*  💰 %d koin\n\n%s\n\n"+
		"_Makan: %smakan <item> · Jual: %sjual <item> all_", data.Coins, strings.Join(bagian, "\n\n"), mp, mp))
	return err
}
