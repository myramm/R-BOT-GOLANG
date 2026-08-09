package cmd

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/econ"
)

// ternak.go: beternak — pelihara hewan, ambil produk, potong jadi daging.
// Port ternak.js.

func init() {
	command.Register(&command.Command{
		Name:        "ternak",
		Category:    "Ekonomi",
		Alias:       []string{"kandang", "beternak"},
		Description: "Beternak: pelihara hewan, ambil telur/susu, potong jadi daging",
		Handler:     ternakHandler,
	})
}

// ringkasKandang mengelompokkan hewan per jenis untuk ringkasan kandang.
func ringkasKandang(hewan []econ.HewanView) string {
	type grup struct {
		emoji, nama                string
		total, lapar, siap, dewasa int
		tercepat                   int64
		adaTercepat                bool
	}
	order := []string{}
	m := map[string]*grup{}
	for _, h := range hewan {
		g, ok := m[h.Jenis]
		if !ok {
			g = &grup{emoji: h.Emoji, nama: h.Nama, tercepat: math.MaxInt64}
			m[h.Jenis] = g
			order = append(order, h.Jenis)
		}
		g.total++
		if h.Lapar {
			g.lapar++
		}
		if h.SiapPanen {
			g.siap++
		}
		if h.Dewasa {
			g.dewasa++
		}
		if !h.Lapar && h.SisaPanen < g.tercepat {
			g.tercepat = h.SisaPanen
			g.adaTercepat = true
		}
	}

	lines := make([]string, 0, len(order))
	for _, j := range order {
		g := m[j]
		var tag []string
		if g.lapar > 0 {
			tag = append(tag, fmt.Sprintf("🍽️ %d lapar", g.lapar))
		}
		if g.siap > 0 {
			tag = append(tag, fmt.Sprintf("✅ %d siap panen", g.siap))
		}
		if g.dewasa > 0 {
			tag = append(tag, fmt.Sprintf("🔪 %d siap potong", g.dewasa))
		}
		if g.siap == 0 && g.lapar == 0 && g.adaTercepat {
			tag = append(tag, "⏳ "+econ.FormatDurasi(g.tercepat))
		}
		lines = append(lines, fmt.Sprintf("%s *%s* ×%d\n     %s", g.emoji, g.nama, g.total, strings.Join(tag, " · ")))
	}
	return strings.Join(lines, "\n")
}

func ternakHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()
	sub := ""
	if len(c.Args) > 0 {
		sub = strings.ToLower(c.Args[0])
	}

	// === beli <hewan> [jumlah] ===
	if sub == "beli" {
		jenis := ""
		if len(c.Args) > 1 {
			jenis = strings.ToLower(c.Args[1])
		}
		jumlah := 1
		if len(c.Args) > 2 {
			jumlah, _ = strconv.Atoi(c.Args[2])
		}
		if jenis == "" {
			var daftar []string
			for _, id := range econ.HewanOrder {
				h := econ.Hewan[id]
				daftar = append(daftar, fmt.Sprintf("%s *%s* — %d koin · pakan %s ×%d", h.Emoji, id, h.Harga, h.Pakan, h.Porsi))
			}
			_, err := c.Reply(ctx, fmt.Sprintf("🐔 *Beli hewan:* %sternak beli <hewan> [jumlah]\n"+
				"Contoh: *%sternak beli ayam 2*\n\n*Tersedia:*\n%s", mp, mp, strings.Join(daftar, "\n")))
			return err
		}

		r := econ.BeliHewan(c.Evt, jenis, jumlah)
		if !r.OK {
			_, err := c.Reply(ctx, "❌ "+r.Err)
			return err
		}
		_, err := c.Reply(ctx, fmt.Sprintf("%s *%d× %s* masuk kandang!\n\n"+
			"Kandang: %d/%d\n"+
			"💰 Koin: %d (-%d)\n\n"+
			"_Pakan: %s — tanam lewat %stani_", r.Def.Emoji, r.Jumlah, r.Def.Name, r.Isi, econ.KandangMax, r.Coins, r.Total, r.Def.Pakan, mp))
		return err
	}

	// === pakan ===
	if sub == "pakan" {
		r := econ.BeriPakan(c.Evt)
		if !r.OK && r.Err == "pakan-kurang" {
			var rincian []string
			for _, k := range r.Kurang {
				rincian = append(rincian, fmt.Sprintf("• *%s* — butuh %d, punya %d", k.Item, k.Butuh, k.Punya))
			}
			_, err := c.Reply(ctx, fmt.Sprintf("🌾 *Pakan kurang*\n\n%s\n\n_Tanam dulu: %stani tanam <no> <bibit>_", strings.Join(rincian, "\n"), mp))
			return err
		}
		if !r.OK {
			_, err := c.Reply(ctx, "❌ "+r.Err)
			return err
		}
		var dipakai []string
		for _, id := range econ.CropOrder {
			if n, ok := r.Butuh[id]; ok && n > 0 {
				dipakai = append(dipakai, fmt.Sprintf("%s ×%d", id, n))
			}
		}
		_, err := c.Reply(ctx, fmt.Sprintf("🌾 *%d hewan diberi pakan!*\n\nPakan: %s", r.Jumlah, strings.Join(dipakai, ", ")))
		return err
	}

	// === panen | ambil ===
	if sub == "panen" || sub == "ambil" {
		r := econ.PanenProduk(c.Evt)
		if !r.OK {
			_, err := c.Reply(ctx, "❌ "+r.Err)
			return err
		}
		var rincian []string
		for _, id := range econ.ProdukOrder {
			if n, ok := r.Hasil[id]; ok && n > 0 {
				p := econ.Produk[id]
				rincian = append(rincian, fmt.Sprintf("%s %s ×%d", p.Emoji, p.Name, n))
			}
		}
		peringatan := ""
		if r.Lapar > 0 {
			peringatan = fmt.Sprintf("\n\n⚠️ %d hewan lapar — *%sternak pakan*", r.Lapar, mp)
		}
		_, err := c.Reply(ctx, fmt.Sprintf("🧺 *Hasil ternak*\n\n%s%s", strings.Join(rincian, "\n"), peringatan))
		return err
	}

	// === potong <hewan> ===
	if sub == "potong" {
		jenis := ""
		if len(c.Args) > 1 {
			jenis = strings.ToLower(c.Args[1])
		}
		if jenis == "" {
			_, err := c.Reply(ctx, fmt.Sprintf("🔪 *Potong hewan:* %sternak potong <hewan>\n"+
				"Contoh: *%sternak potong ayam*\n\n"+
				"_Hanya hewan cukup umur yang bisa dipotong._", mp, mp))
			return err
		}
		r := econ.PotongHewan(c.Evt, jenis)
		if !r.OK {
			_, err := c.Reply(ctx, "❌ "+r.Err)
			return err
		}
		_, err := c.Reply(ctx, fmt.Sprintf("🔪 *%s dipotong*\n\n"+
			"%s %s ×%d masuk inventory\n"+
			"+%d ⚡/porsi\n"+
			"Sisa %s: %d", r.Def.Name, r.Item.Emoji, r.Item.Name, r.Jumlah, r.Item.EnergyRestore, r.Def.Name, r.Sisa))
		return err
	}

	// === default: lihat kandang ===
	data := econ.LihatKandang(c.Evt)
	if data == nil {
		_, err := c.Reply(ctx, "❌ Data kandang tidak terbaca.")
		return err
	}

	if len(data.Hewan) == 0 {
		var daftar []string
		for _, id := range econ.HewanOrder {
			h := econ.Hewan[id]
			daftar = append(daftar, fmt.Sprintf("%s *%s* — %d koin", h.Emoji, id, h.Harga))
		}
		_, err := c.Reply(ctx, fmt.Sprintf("🏚️ *Kandang kosong.*  💰 %d koin\n\n"+
			"*Hewan dijual:*\n%s\n\n"+
			"Beli: *%sternak beli ayam*", data.Coins, strings.Join(daftar, "\n"), mp))
		return err
	}

	lapar, siap := 0, 0
	for _, h := range data.Hewan {
		if h.Lapar {
			lapar++
		}
		if h.SiapPanen {
			siap++
		}
	}
	var aksi []string
	if lapar > 0 {
		aksi = append(aksi, fmt.Sprintf("🍽️ %d hewan lapar — *%sternak pakan*", lapar, mp))
	}
	if siap > 0 {
		aksi = append(aksi, fmt.Sprintf("✅ %d siap panen — *%sternak panen*", siap, mp))
	}
	aksiStr := ""
	if len(aksi) > 0 {
		aksiStr = strings.Join(aksi, "\n") + "\n\n"
	}

	_, err := c.Reply(ctx, fmt.Sprintf("🐄 *Kandang*  %d/%d  💰 %d koin\n\n%s\n\n%s_Potong: %sternak potong <hewan>_",
		len(data.Hewan), data.Kapasitas, data.Coins, ringkasKandang(data.Hewan), aksiStr, mp))
	return err
}
