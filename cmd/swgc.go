package cmd

import (
	"context"
	"fmt"
	"strings"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/swgc"
)

func init() {
	command.Register(&command.Command{
		Name:        "swgc",
		Category:    "Owner",
		Alias:       []string{"statusswgc", "swg"},
		Description: "Kirim status story ke grup WhatsApp (Group Status SWGC). Balas media/teks dengan .swgc",
		OwnerOnly:   true,
		Handler:     swgcHandler,
	})

	command.Register(&command.Command{
		Name:        "swgc_process",
		Category:    "Owner",
		Alias:       []string{"swgcprocess", "swgcproc"},
		Description: "Proses pengiriman SWGC ke JID grup pilihan",
		OwnerOnly:   true,
		Handler:     swgcProcessHandler,
	})
}

func getMediaMessage(m *waE2E.Message) (*waE2E.Message, string) {
	if m == nil {
		return nil, ""
	}
	// Unwrap ephemeral / viewOnce jika ada
	for m != nil {
		switch {
		case m.GetEphemeralMessage().GetMessage() != nil:
			m = m.GetEphemeralMessage().GetMessage()
		case m.GetViewOnceMessage().GetMessage() != nil:
			m = m.GetViewOnceMessage().GetMessage()
		case m.GetViewOnceMessageV2().GetMessage() != nil:
			m = m.GetViewOnceMessageV2().GetMessage()
		default:
			goto unwrapped
		}
	}
unwrapped:
	if img := m.GetImageMessage(); img != nil {
		mime := img.GetMimetype()
		if mime == "" {
			mime = "image/jpeg"
		}
		return m, mime
	}
	if vid := m.GetVideoMessage(); vid != nil {
		mime := vid.GetMimetype()
		if mime == "" {
			mime = "video/mp4"
		}
		return m, mime
	}
	if aud := m.GetAudioMessage(); aud != nil {
		mime := aud.GetMimetype()
		if mime == "" {
			mime = "audio/mp4"
		}
		return m, mime
	}
	return nil, ""
}

func swgcHandler(ctx context.Context, c *command.Ctx) error {
	prefix := config.MainPrefix()
	caption := strings.TrimSpace(c.ArgStr())

	ci := c.ContextInfo()
	var mediaMsg *waE2E.Message
	var mime string

	// 1. Cek media di quoted message
	if ci != nil && ci.GetQuotedMessage() != nil {
		mediaMsg, mime = getMediaMessage(ci.GetQuotedMessage())
	}
	// 2. Cek media di pesan pemicu jika tidak ada di quoted
	if mediaMsg == nil {
		mediaMsg, mime = getMediaMessage(c.Evt.Message)
	}

	var data []byte
	if mediaMsg != nil {
		var err error
		data, err = c.Client.DownloadAny(ctx, mediaMsg)
		if err != nil || len(data) == 0 {
			_, replyErr := c.Reply(ctx, "❌ Gagal mengunduh media dari pesan: "+err.Error())
			return replyErr
		}
	} else if caption != "" {
		mime = "text"
	} else {
		_, err := c.Reply(ctx, fmt.Sprintf("⚠️ *Cara Penggunaan SWGC:*\nKirim atau balas gambar/video/audio dengan caption *%sswgc <caption?>*\natau ketik *%sswgc <teks status>* untuk status berupa teks.", prefix, prefix))
		return err
	}

	// Simpan media/teks ke buffer sender
	senderKey := c.SenderPhone()
	swgc.SetBuffer(senderKey, data, mime, caption)

	// Ambil daftar grup yang diikuti bot
	groups, err := c.Client.GetJoinedGroups(ctx)
	if err != nil {
		_, replyErr := c.Reply(ctx, "❌ Gagal mengambil daftar grup: "+err.Error())
		return replyErr
	}

	if len(groups) == 0 {
		_, replyErr := c.Reply(ctx, "❌ Bot tidak berada di dalam grup manapun.")
		return replyErr
	}

	var b strings.Builder
	b.WriteString("📲 *PILIH GRUP UNTUK SWGC*\n\n")
	b.WriteString("Status berhasil disimpan di buffer. Silakan pilih grup tujuan:\n\n")

	for i, g := range groups {
		groupName := g.Name
		if groupName == "" {
			groupName = "Grup Tanpa Nama"
		}
		announceStatus := "Terbuka"
		if g.IsAnnounce {
			announceStatus = "Hanya Admin"
		}
		b.WriteString(fmt.Sprintf("%d. *%s*\n", i+1, groupName))
		b.WriteString(fmt.Sprintf("   • Anggota: %d | Status: %s\n", len(g.Participants), announceStatus))
		b.WriteString(fmt.Sprintf("   • Kirim: *%sswgc_process %s*\n\n", prefix, g.JID.String()))
	}

	b.WriteString(fmt.Sprintf("_Salin/klik perintah %sswgc_process <JID> di atas untuk mengirim status._", prefix))

	_, err = c.Reply(ctx, b.String())
	return err
}

func swgcProcessHandler(ctx context.Context, c *command.Ctx) error {
	prefix := config.MainPrefix()
	if len(c.Args) == 0 || strings.TrimSpace(c.Args[0]) == "" {
		_, err := c.Reply(ctx, fmt.Sprintf("❌ Format salah. Contoh: *%sswgc_process 120363041234567890@g.us*", prefix))
		return err
	}

	targetJIDRaw := strings.TrimSpace(c.Args[0])
	// Split jika ada pipe '|' dari format custom
	if idx := strings.Index(targetJIDRaw, "|"); idx >= 0 {
		targetJIDRaw = strings.TrimSpace(targetJIDRaw[:idx])
	}

	targetJID, err := types.ParseJID(targetJIDRaw)
	if err != nil || targetJID.Server != types.GroupServer {
		_, replyErr := c.Reply(ctx, fmt.Sprintf("❌ JID grup *%s* tidak valid.", targetJIDRaw))
		return replyErr
	}

	senderKey := c.SenderPhone()
	buf, ok := swgc.GetBuffer(senderKey)
	if !ok {
		_, replyErr := c.Reply(ctx, fmt.Sprintf("❌ Media/teks status tidak tersedia di buffer. Kirim/reply media dulu dengan *%sswgc*", prefix))
		return replyErr
	}

	c.React(ctx, "⏳")

	err = swgc.SendGroupStatus(ctx, c.Client, targetJID, buf.Mime, buf.Media, buf.Caption)
	if err != nil {
		c.React(ctx, "❌")
		_, replyErr := c.Reply(ctx, "❌ Gagal mengirim SWGC: "+err.Error())
		return replyErr
	}

	swgc.ClearBuffer(senderKey)
	c.React(ctx, "✅")
	_, err = c.Reply(ctx, fmt.Sprintf("✅ *Berhasil mengirim Group Status (SWGC)!*\n\n🎯 *Target Grup:* %s", targetJID.String()))
	return err
}
