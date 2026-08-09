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

// tani.go: bertani — tanam, panen, kelola lahan. Port tani.js.

func init() {
	command.Register(&command.Command{
		Name:        "tani",
		Category:    "Ekonomi",
		Alias:       []string{"farm", "kebun", "bertani"},
		Description: "Bertani: tanam, panen, dan kelola lahan",
		Handler:     taniHandler,
	})
}

// taniBar menggambar bar progres 10 petak dari persen (0–100).
func taniBar(persen int) string {
	isi := int(math.Round(float64(persen) / 100 * 10))
	if isi < 0 {
		isi = 0
	}
	if isi > 10 {
		isi = 10
	}
	return strings.Repeat("█", isi) + strings.Repeat("░", 10-isi)
}

func tampilKebun(v *econ.KebunView) string {
	lines := make([]string, len(v.Plots))
	for i, p := range v.Plots {
		no := fmt.Sprintf("%2d", p.I+1)
		switch {
		case p.Kosong:
			lines[i] = fmt.Sprintf("%s. 🟫 _kosong_", no)
		case p.Siap:
			lines[i] = fmt.Sprintf("%s. %s *%s* — ✅ SIAP PANEN", no, p.Emoji, p.Nama)
		default:
			lines[i] = fmt.Sprintf("%s. %s %s\n     %s %d%% · %s", no, p.Emoji, p.Nama, taniBar(p.Persen), p.Persen, econ.FormatDurasi(p.Sisa))
		}
	}
	return strings.Join(lines, "\n")
}

func taniHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()
	sub := ""
	if len(c.Args) > 0 {
		sub = strings.ToLower(c.Args[0])
	}

	// === tanam <no lahan> <bibit> ===
	if sub == "tanam" {
		plot := 0
		if len(c.Args) > 1 {
			plot, _ = strconv.Atoi(c.Args[1])
		}
		bibit := ""
		if len(c.Args) > 2 {
			bibit = strings.ToLower(c.Args[2])
		}
		if plot == 0 || bibit == "" {
			var daftar []string
			for _, id := range econ.CropOrder {
				cr := econ.Crops[id]
				daftar = append(daftar, fmt.Sprintf("%s *%s* — %d koin · %s", cr.Emoji, id, cr.Seed, econ.FormatDurasi(cr.GrowTime)))
			}
			_, err := c.Reply(ctx, fmt.Sprintf("🌱 *Cara tanam:* %stani tanam <no.lahan> <bibit>\n"+
				"Contoh: *%stani tanam 1 wortel*\n\n*Bibit tersedia:*\n%s", mp, mp, strings.Join(daftar, "\n")))
			return err
		}

		r := econ.Tanam(c.Evt, plot-1, bibit)
		if !r.OK {
			_, err := c.Reply(ctx, "❌ "+r.Err)
			return err
		}
		_, err := c.Reply(ctx, fmt.Sprintf("🌱 *%s* ditanam di lahan %d!\n\n"+
			"%s Siap panen dalam *%s*\n"+
			"💰 Koin: %d (-%d)\n\n"+
			"_Cek nanti: %stani_", r.Crop.Name, r.Plot, r.Crop.Emoji, econ.FormatDurasi(r.Sisa), r.Coins, r.Crop.Seed, mp))
		return err
	}

	// === panen [no] | panen all ===
	if sub == "panen" {
		target := ""
		if len(c.Args) > 1 {
			target = strings.ToLower(c.Args[1])
		}

		if target == "" || target == "all" || target == "semua" {
			r := econ.PanenSemua(c.Evt)
			if !r.OK {
				_, err := c.Reply(ctx, "❌ "+r.Err)
				return err
			}
			var rincian []string
			for _, h := range r.Hasil {
				rincian = append(rincian, fmt.Sprintf("%s %s ×%d _(lahan %d)_", h.Crop.Emoji, h.Crop.Name, h.Jumlah, h.Plot))
			}
			_, err := c.Reply(ctx, fmt.Sprintf("🧺 *Panen raya!*\n\n%s\n\n"+
				"Semua masuk inventory — cek *%sinv*\n"+
				"_Bisa dimakan (%smakan) atau dijual (%sjual)._", strings.Join(rincian, "\n"), mp, mp, mp))
			return err
		}

		no, _ := strconv.Atoi(target)
		r := econ.Panen(c.Evt, no-1)
		if !r.OK {
			_, err := c.Reply(ctx, "❌ "+r.Err)
			return err
		}
		_, err := c.Reply(ctx, fmt.Sprintf("🧺 *Panen berhasil!*\n\n"+
			"%s %s ×%d dari lahan %d\n"+
			"Total di inventory: *%d*", r.Crop.Emoji, r.Crop.Name, r.Jumlah, r.Plot, r.Total))
		return err
	}

	// === beli lahan baru ===
	if sub == "lahan" || sub == "belilahan" {
		r := econ.BeliLahan(c.Evt)
		if !r.OK {
			_, err := c.Reply(ctx, "❌ "+r.Err)
			return err
		}
		_, err := c.Reply(ctx, fmt.Sprintf("🟫 *Lahan baru dibeli!*\n\n"+
			"Total lahan: *%d/%d*\n"+
			"💰 Koin: %d (-%d)", r.Total, econ.PlotMax, r.Coins, r.Harga))
		return err
	}

	// === default: lihat kebun ===
	data := econ.LihatKebun(c.Evt)
	if data == nil {
		_, err := c.Reply(ctx, "❌ Data kebun tidak terbaca.")
		return err
	}

	siap, kosong := 0, 0
	for _, p := range data.Plots {
		if p.Kosong {
			kosong++
		} else if p.Siap {
			siap++
		}
	}
	var catatan []string
	if siap > 0 {
		catatan = append(catatan, fmt.Sprintf("✅ %d lahan siap panen — *%stani panen all*", siap, mp))
	}
	if kosong > 0 {
		catatan = append(catatan, fmt.Sprintf("🟫 %d lahan kosong — *%stani tanam <no> <bibit>*", kosong, mp))
	}
	catatanStr := ""
	if len(catatan) > 0 {
		catatanStr = strings.Join(catatan, "\n") + "\n"
	}
	lahanBerikut := ""
	if len(data.Plots) < econ.PlotMax {
		lahanBerikut = fmt.Sprintf("🟫 Lahan ke-%d: %d koin — *%stani lahan*\n", len(data.Plots)+1, econ.HargaLahan(len(data.Plots)), mp)
	}

	_, err := c.Reply(ctx, fmt.Sprintf("🌾 *Kebun Kamu*  💰 %d koin\n\n%s\n\n%s%s\n_Beli bibit: %stoko_",
		data.Coins, tampilKebun(data), catatanStr, lahanBerikut, mp))
	return err
}
