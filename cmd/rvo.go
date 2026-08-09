package cmd

import (
	"context"
	"strings"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"

	"rbot/brain/command"
	"rbot/brain/config"
)

// rvo.go: buka & kirim ulang pesan Sekali Lihat (View Once). Port rvo.js.
// Balas/reply pesan View Once dengan .rvo — media diunduh dari server WhatsApp
// lalu dikirim ulang sebagai media biasa (tanpa flag view-once).

func init() {
	command.Register(&command.Command{
		Name:        "rvo",
		Category:    "Info",
		Alias:       []string{"readviewonce", "viewonce", "rviewonce"},
		Description: "Buka & simpan pesan Sekali Lihat (View Once) gambar/video/audio. Balas pesan View Once dengan .rvo",
		Handler:     rvoHandler,
	})
}

// unwrapViewOnce membuka lapisan viewOnce (semua varian) hingga pesan media
// telanjang. whatsmeow tak otomatis membuka viewOnce pada pesan yang di-quote,
// jadi dilakukan manual seperti rvo.js.
func unwrapViewOnce(m *waE2E.Message) *waE2E.Message {
	for m != nil {
		switch {
		case m.GetViewOnceMessage().GetMessage() != nil:
			m = m.GetViewOnceMessage().GetMessage()
		case m.GetViewOnceMessageV2().GetMessage() != nil:
			m = m.GetViewOnceMessageV2().GetMessage()
		case m.GetViewOnceMessageV2Extension().GetMessage() != nil:
			m = m.GetViewOnceMessageV2Extension().GetMessage()
		default:
			return m
		}
	}
	return m
}

func rvoHandler(ctx context.Context, c *command.Ctx) error {
	ci := c.ContextInfo()
	quoted := ci.GetQuotedMessage()
	if quoted == nil {
		_, err := c.Reply(ctx, "Balas (reply) pesan Sekali Lihat (View Once) yang ingin dibuka. Contoh:\n*"+config.MainPrefix()+"rvo*")
		return err
	}

	quoted = unwrapViewOnce(quoted)
	img := quoted.GetImageMessage()
	vid := quoted.GetVideoMessage()
	aud := quoted.GetAudioMessage()
	if img == nil && vid == nil && aud == nil {
		_, err := c.Reply(ctx, "Pesan yang kamu balas bukan media View Once (gambar/video/audio).")
		return err
	}

	c.React(ctx, "🔓")

	data, err := c.Client.DownloadAny(ctx, quoted)
	if err != nil || len(data) == 0 {
		c.React(ctx, "❌")
		reason := "media mungkin sudah expired/terhapus di server WhatsApp"
		if err != nil {
			reason = err.Error()
		}
		_, e := c.Reply(ctx, "Gagal membuka View Once: "+reason)
		return e
	}

	// Kirim ulang sesuai tipe. Caption ikut caption asli bila ada (image/video).
	switch {
	case img != nil:
		caption := "🔓 *View Once Unlocked*"
		if cap := strings.TrimSpace(img.GetCaption()); cap != "" {
			caption += "\n\n" + cap
		}
		err = c.SendMediaBytes(ctx, data, command.MediaImage, caption, "", img.GetMimetype())
	case vid != nil:
		caption := "🔓 *View Once Unlocked*"
		if cap := strings.TrimSpace(vid.GetCaption()); cap != "" {
			caption += "\n\n" + cap
		}
		err = c.SendMediaBytes(ctx, data, command.MediaVideo, caption, "", vid.GetMimetype())
	default: // aud != nil
		mimetype := aud.GetMimetype()
		if mimetype == "" {
			mimetype = "audio/mp4"
		}
		err = c.SendMediaBytes(ctx, data, command.MediaAudio, "", "", mimetype)
	}
	if err != nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, "Gagal mengirim media: "+err.Error())
		return e
	}

	c.React(ctx, "✅")
	return nil
}
