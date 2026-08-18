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
		Description: "Kirim status story ke grup WhatsApp (Group Status SWGC). Balas media/teks dengan .swgc atau .swgc <JID_Grup>",
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

func extractMediaAndCaption(ctx context.Context, c *command.Ctx, skipFirstArg bool) ([]byte, string, string, error) {
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

	caption := ""
	if skipFirstArg && len(c.Args) > 1 {
		caption = strings.TrimSpace(strings.Join(c.Args[1:], " "))
	} else if !skipFirstArg {
		caption = strings.TrimSpace(c.ArgStr())
	}

	var data []byte
	if mediaMsg != nil {
		var err error
		data, err = c.Client.DownloadAny(ctx, mediaMsg)
		if err != nil {
			return nil, "", "", fmt.Errorf("gagal mengunduh media dari pesan: %w", err)
		}
	} else if caption != "" {
		mime = "text"
	} else {
		return nil, "", "", fmt.Errorf("media atau caption tidak ditemukan")
	}

	return data, mime, caption, nil
}

func swgcHandler(ctx context.Context, c *command.Ctx) error {
	prefix := config.MainPrefix()

	// Cek apakah argumen pertama adalah JID grup langsung (.swgc 120363xxx@g.us)
	if len(c.Args) > 0 {
		firstArg := strings.TrimSpace(c.Args[0])
		if idx := strings.Index(firstArg, "|"); idx >= 0 {
			firstArg = strings.TrimSpace(firstArg[:idx])
		}
		if targetJID, err := types.ParseJID(firstArg); err == nil && targetJID.Server == types.GroupServer {
			// Langsung kirim ke JID grup jika media/teks tersedia di pesan/reply
			data, mime, caption, err := extractMediaAndCaption(ctx, c, true)
			if err == nil {
				c.React(ctx, "⏳")
				sendErr := swgc.SendGroupStatus(ctx, c.Client, targetJID, mime, data, caption)
				if sendErr != nil {
					c.React(ctx, "❌")
					_, replyErr := c.Reply(ctx, "❌ Gagal mengirim SWGC: "+sendErr.Error())
					return replyErr
				}
				c.React(ctx, "✅")
				_, replyErr := c.Reply(ctx, fmt.Sprintf("✅ *Berhasil mengirim Group Status (SWGC)!*\n\n🎯 *Target Grup:* %s", targetJID.String()))
				return replyErr
			}
		}
	}

	// Alur 2-step: Ekstrak media/teks dan simpan ke buffer
	data, mime, caption, err := extractMediaAndCaption(ctx, c, false)
	if err != nil {
		_, replyErr := c.Reply(ctx, fmt.Sprintf("⚠️ *Cara Penggunaan SWGC:*\n• Reply media/teks langsung: *%sswgc <JID_grup>*\n• Alur pilih grup: Kirim/reply media dengan *%sswgc* lalu pilih grup dari list.", prefix, prefix))
		return replyErr
	}

	senderKey := c.SenderPhone()
	swgc.SetBuffer(senderKey, data, mime, caption)

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
	if idx := strings.Index(targetJIDRaw, "|"); idx >= 0 {
		targetJIDRaw = strings.TrimSpace(targetJIDRaw[:idx])
	}

	targetJID, err := types.ParseJID(targetJIDRaw)
	if err != nil || targetJID.Server != types.GroupServer {
		_, replyErr := c.Reply(ctx, fmt.Sprintf("❌ JID grup *%s* tidak valid.", targetJIDRaw))
		return replyErr
	}

	var data []byte
	var mime string
	var caption string

	// 1. Coba ekstrak media langsung jika user melakukan reply saat menjalankan .swgc_process
	directData, directMime, directCap, directErr := extractMediaAndCaption(ctx, c, true)
	if directErr == nil && (len(directData) > 0 || directCap != "") {
		data = directData
		mime = directMime
		caption = directCap
	} else {
		// 2. Jika tidak ada reply media di pesan saat ini, gunakan buffer tersimpan
		senderKey := c.SenderPhone()
		buf, ok := swgc.GetBuffer(senderKey)
		if !ok {
			_, replyErr := c.Reply(ctx, fmt.Sprintf("❌ Media/teks status tidak tersedia di buffer. Kirim/reply media dulu dengan *%sswgc*", prefix))
			return replyErr
		}
		data = buf.Media
		mime = buf.Mime
		caption = buf.Caption
		swgc.ClearBuffer(senderKey)
	}

	c.React(ctx, "⏳")

	err = swgc.SendGroupStatus(ctx, c.Client, targetJID, mime, data, caption)
	if err != nil {
		c.React(ctx, "❌")
		_, replyErr := c.Reply(ctx, "❌ Gagal mengirim SWGC: "+err.Error())
		return replyErr
	}

	c.React(ctx, "✅")
	_, err = c.Reply(ctx, fmt.Sprintf("✅ *Berhasil mengirim Group Status (SWGC)!*\n\n🎯 *Target Grup:* %s", targetJID.String()))
	return err
}
