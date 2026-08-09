package cmd

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/sponsor"
)

// setsponsor.go: atur media promosi/sponsor (owner). Port setsponsor.js.
// Teks saja, atau gambar/video (dari pesan ini atau yang di-reply) + caption.
// Media diunduh dari server WhatsApp lalu disimpan lewat sponsor.Set.

func init() {
	command.Register(&command.Command{
		Name:        "setsponsor",
		Category:    "Owner",
		Alias:       []string{"setpromo"},
		Description: "Atur media promosi/sponsor (owner). Kirim teks/gambar/video dengan caption .setsponsor. .setsponsor off untuk hapus.",
		OwnerOnly:   true,
		Handler:     setsponsorHandler,
	})
}

var reLink = regexp.MustCompile(`(?i)https?://\S+`)

// mediaSponsor mencari gambar/video untuk sponsor: dari pesan saat ini, atau dari
// pesan yang di-reply. Mengembalikan pesan sumber (untuk DownloadAny), tipe
// ("image"/"video"), dan ekstensi. type kosong berarti tak ada media.
func mediaSponsor(c *command.Ctx) (src *waE2E.Message, typ, ext string) {
	m := c.Evt.Message
	if m.GetImageMessage() != nil {
		return m, "image", "jpg"
	}
	if m.GetVideoMessage() != nil {
		return m, "video", "mp4"
	}
	if ci := c.ContextInfo(); ci != nil {
		if q := ci.GetQuotedMessage(); q != nil {
			if q.GetImageMessage() != nil {
				return q, "image", "jpg"
			}
			if q.GetVideoMessage() != nil {
				return q, "video", "mp4"
			}
		}
	}
	return nil, "", ""
}

func setsponsorHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()
	mode := ""
	if len(c.Args) > 0 {
		mode = strings.ToLower(c.Args[0])
	}

	// === off | hapus | reset ===
	if mode == "off" || mode == "hapus" || mode == "reset" {
		sponsor.Clear()
		_, err := c.Reply(ctx, "🗑️ Sponsor dihapus. Promosi tidak akan muncul lagi di menu/hasil.")
		return err
	}

	// === cek | info | status ===
	if mode == "cek" || mode == "info" || mode == "status" {
		s := sponsor.Get()
		if s == nil {
			_, err := c.Reply(ctx, "ℹ️ Belum ada sponsor. Atur dengan mengirim teks/gambar/video + caption *"+mp+"setsponsor*.")
			return err
		}
		mediaAda := "tidak"
		if s.MediaFile != "" {
			mediaAda = "ada"
		}
		sisip := "OFF"
		if s.InResults {
			sisip = "ON"
		}
		teks := s.Text
		if teks == "" {
			teks = "-"
		}
		link := s.Link
		if link == "" {
			link = "-"
		}
		_, err := c.Reply(ctx, fmt.Sprintf("📣 *Sponsor aktif*\nTipe: %s\nTeks: %s\nLink: %s\nMedia: %s\nSisip di hasil: %s",
			s.Type, teks, link, mediaAda, sisip))
		return err
	}

	// === sisip | inresults on|off ===
	if mode == "sisip" || mode == "inresults" {
		sub := ""
		if len(c.Args) > 1 {
			sub = strings.ToLower(c.Args[1])
		}
		if sub != "on" && sub != "off" {
			_, err := c.Reply(ctx, fmt.Sprintf("Pakai: *%ssetsponsor sisip on* atau *%ssetsponsor sisip off*", mp, mp))
			return err
		}
		val, ada := sponsor.SetInResults(sub == "on")
		if !ada {
			_, err := c.Reply(ctx, "ℹ️ Belum ada sponsor. Atur dulu dengan *"+mp+"setsponsor*.")
			return err
		}
		var msg string
		if val {
			msg = "✅ Sisip promosi di hasil command: *ON* (muncul sesekali, cooldown 6 jam/chat)."
		} else {
			msg = fmt.Sprintf("✅ Sisip promosi di hasil command: *OFF*. Sponsor tetap muncul di *%ssponsor* & footer *%smenu*.", mp, mp)
		}
		_, err := c.Reply(ctx, msg)
		return err
	}

	// === default: simpan sponsor (teks / gambar / video) ===
	caption := strings.TrimSpace(c.ArgStr())
	link := reLink.FindString(caption)
	text := strings.TrimSpace(reLink.ReplaceAllString(caption, ""))

	src, typ, ext := mediaSponsor(c)

	if src == nil && caption == "" {
		_, err := c.Reply(ctx, fmt.Sprintf("📣 *Atur Sponsor / Promosi*\n\n"+
			"*Teks saja:*\n%ssetsponsor Promo toko A, diskon 50%%! https://tokoa.com\n\n"+
			"*Gambar/Video:*\nKirim gambar/video dengan caption:\n%ssetsponsor Promo toko A https://tokoa.com\n\n"+
			"*Hapus:* %ssetsponsor off\n*Cek:* %ssetsponsor cek\n"+
			"*Sisip di hasil command:* %ssetsponsor sisip on/off _(default OFF)_\n\n"+
			"_Default: sponsor muncul di %ssponsor & footer %smenu saja._",
			mp, mp, mp, mp, mp, mp, mp))
		return err
	}

	if src != nil {
		buf, err := c.Client.DownloadAny(ctx, src)
		if err != nil || len(buf) == 0 {
			reason := "media mungkin sudah expired/terhapus di server WhatsApp"
			if err != nil {
				reason = err.Error()
			}
			_, e := c.Reply(ctx, "❌ Gagal mengunduh media sponsor: "+reason)
			return e
		}
		sponsor.Set(sponsor.SetInput{Type: typ, Text: text, Link: link, MediaBuffer: buf, Ext: ext})
		_, err = c.Reply(ctx, fmt.Sprintf("✅ Sponsor (%s) disimpan. Muncul di *%ssponsor* & footer *%smenu*.\n_Sisip di hasil command OFF — nyalakan dgn %ssetsponsor sisip on._",
			typ, mp, mp, mp))
		return err
	}

	sponsor.Set(sponsor.SetInput{Type: "text", Text: text, Link: link})
	_, err := c.Reply(ctx, fmt.Sprintf("✅ Sponsor (teks) disimpan. Muncul di *%ssponsor* & footer *%smenu*.\n_Sisip di hasil command OFF — nyalakan dgn %ssetsponsor sisip on._",
		mp, mp, mp))
	return err
}
