package cmd

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/types"

	"rbot/brain/command"
	"rbot/brain/config"
)

func init() {
	command.Register(&command.Command{
		Name:        "idch",
		Category:    "Info",
		Alias:       []string{"getidch", "idchannel", "channelinfo", "saluran", "cekch"},
		Description: "Ambil ID Saluran (JID Newsletter) dan informasi lengkap WhatsApp Channel berdasarkan tautan atau pesan forward saluran.",
		Handler:     idchHandler,
	})
}

func idchHandler(ctx context.Context, c *command.Ctx) error {
	arg := strings.TrimSpace(c.ArgStr())

	// 1. Cek apakah ada link / query di argumen
	var targetQuery string
	if arg != "" {
		targetQuery = arg
	} else if c.Evt != nil && c.Evt.Message != nil {
		// Cek quoted message (misal user me-reply pesan yang diforward dari saluran)
		ctxInfo := c.Evt.Message.GetExtendedTextMessage().GetContextInfo()
		if ctxInfo != nil {
			if fwd := ctxInfo.GetForwardedNewsletterMessageInfo(); fwd != nil && fwd.GetNewsletterJID() != "" {
				targetQuery = fwd.GetNewsletterJID()
			} else if qMsg := ctxInfo.GetQuotedMessage(); qMsg != nil {
				if qExt := qMsg.GetExtendedTextMessage(); qExt != nil {
					targetQuery = strings.TrimSpace(qExt.GetText())
				} else if qConv := qMsg.GetConversation(); qConv != "" {
					targetQuery = strings.TrimSpace(qConv)
				}
			}
		}
	}

	if targetQuery == "" {
		_, err := c.Reply(ctx, "📢 *Cara Menggunakan .idch:*\n\n1. Kirim perintah beserta link saluran:\n   `"+config.MainPrefix()+"idch https://whatsapp.com/channel/xxxx`\n\n2. Atau reply/quote pesan yang di-forward dari Saluran WhatsApp dengan perintah `"+config.MainPrefix()+"idch`.")
		return err
	}

	c.React(ctx, "⏳")

	var meta *types.NewsletterMetadata
	var err error

	if strings.HasSuffix(targetQuery, "@newsletter") {
		jid, parseErr := types.ParseJID(targetQuery)
		if parseErr != nil {
			c.React(ctx, "❌")
			_, e := c.Reply(ctx, "❌ Format JID saluran tidak valid: "+parseErr.Error())
			return e
		}
		meta, err = c.Client.GetNewsletterInfo(ctx, jid)
	} else {
		inviteCode := extractNewsletterCode(targetQuery)
		if strings.HasSuffix(inviteCode, "@newsletter") {
			jid, _ := types.ParseJID(inviteCode)
			meta, err = c.Client.GetNewsletterInfo(ctx, jid)
		} else {
			meta, err = c.Client.GetNewsletterInfoWithInvite(ctx, inviteCode)
		}
	}

	if err != nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, fmt.Sprintf("❌ *Gagal Mengambil Info Saluran:*\n\nPastikan tautan atau kode saluran benar.\nDetail error: `%v`", err))
		return e
	}

	c.React(ctx, "📢")

	name := meta.ThreadMeta.Name.Text
	if name == "" {
		name = "Saluran WhatsApp"
	}
	desc := meta.ThreadMeta.Description.Text
	if desc == "" {
		desc = "Tidak ada deskripsi."
	}
	if len(desc) > 300 {
		desc = desc[:300] + "..."
	}

	subs := meta.ThreadMeta.SubscriberCount
	verifiedStr := "Tidak"
	if meta.ThreadMeta.VerificationState == types.NewsletterVerificationStateVerified {
		verifiedStr = "Verified ✅"
	}

	inviteLink := "-"
	if meta.ThreadMeta.InviteCode != "" {
		inviteLink = "https://whatsapp.com/channel/" + meta.ThreadMeta.InviteCode
	}

	response := fmt.Sprintf(
		"📢 *INFORMASI SALURAN WHATSAPP*\n\n"+
			"• *Nama:* %s\n"+
			"• *ID Saluran (JID):*\n`%s`\n"+
			"• *Jumlah Pengikut:* %s\n"+
			"• *Status Verifikasi:* %s\n"+
			"• *Tautan Undangan:* %s\n\n"+
			"📝 *Deskripsi:*\n%s",
		name,
		meta.ID.String(),
		formatNumber(int64(subs))+" subscriber",
		verifiedStr,
		inviteLink,
		desc,
	)

	_, sendErr := c.Reply(ctx, response)
	return sendErr
}

func extractNewsletterCode(input string) string {
	input = strings.TrimSpace(input)
	if idx := strings.Index(input, "/channel/"); idx != -1 {
		code := input[idx+len("/channel/"):]
		if qIdx := strings.IndexAny(code, "?#/ \r\n"); qIdx != -1 {
			code = code[:qIdx]
		}
		return strings.TrimSpace(code)
	}
	for _, word := range strings.Fields(input) {
		if idx := strings.Index(word, "/channel/"); idx != -1 {
			code := word[idx+len("/channel/"):]
			if qIdx := strings.IndexAny(code, "?#/ "); qIdx != -1 {
				code = code[:qIdx]
			}
			return strings.TrimSpace(code)
		}
	}
	return input
}

func formatNumber(n int64) string {
	in := fmt.Sprintf("%d", n)
	if len(in) <= 3 {
		return in
	}
	var out []byte
	rem := len(in) % 3
	if rem > 0 {
		out = append(out, in[:rem]...)
		if len(in) > rem {
			out = append(out, '.')
		}
	}
	for i := rem; i < len(in); i += 3 {
		out = append(out, in[i:i+3]...)
		if i+3 < len(in) {
			out = append(out, '.')
		}
	}
	return string(out)
}
