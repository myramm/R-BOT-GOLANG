package cmd

import (
	"context"
	"strings"
	"unicode/utf8"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"rbot/brain/command"
	"rbot/brain/config"
)

const maxReportRunes = 2000

func init() {
	command.Register(&command.Command{
		Name:        "report",
		Category:    "Info",
		Alias:       []string{"lapor"},
		Description: "Laporkan error atau fitur bermasalah ke owner. Contoh: .report fitur hd error",
		Handler:     reportHandler,
	})
}

func reportHandler(ctx context.Context, c *command.Ctx) error {
	text := strings.TrimSpace(c.ArgStr())
	if text == "" {
		_, err := c.Reply(ctx, "Mau lapor error apa? Contoh: "+config.MainPrefix()+"report fitur hd error")
		return err
	}
	text = truncateReport(text, maxReportRunes)

	ownerRaw := config.PrimaryOwnerAddress()
	if ownerRaw == "" {
		_, err := c.Reply(ctx, "Maaf, laporan gagal terkirim karena owner belum dikonfigurasi.")
		return err
	}
	if !strings.Contains(ownerRaw, "@") {
		ownerRaw += "@s.whatsapp.net"
	}
	ownerJID, err := types.ParseJID(ownerRaw)
	if err != nil {
		_, replyErr := c.Reply(ctx, "Maaf, laporan gagal terkirim. Coba lagi nanti ya.")
		return replyErr
	}

	name := strings.TrimSpace(c.Evt.Info.PushName)
	if name == "" {
		name = "Tanpa nama"
	}
	forwarded := buildReportMessage(c, name, text)
	if _, err := c.Client.SendMessage(ctx, ownerJID, &waE2E.Message{Conversation: proto.String(forwarded)}); err != nil {
		_, replyErr := c.Reply(ctx, "Maaf, laporan gagal terkirim. Coba lagi nanti ya.")
		return replyErr
	}

	_, err = c.Reply(ctx, "Laporan error kamu sudah diteruskan ke owner. Terima kasih sudah melapor 🙏")
	return err
}

func buildReportMessage(c *command.Ctx, name, text string) string {
	message := "⚠️ *Laporan Error*\n\n" +
		"Dari: " + name + " (" + c.Sender().String() + ")\n" +
		"Chat: " + c.Chat().String() + "\n"
	return message + "\nPesan:\n" + text
}

func truncateReport(text string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	runes := []rune(text)
	return string(runes[:maxRunes]) + "…"
}
