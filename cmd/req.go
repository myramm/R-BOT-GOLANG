package cmd

import (
	"context"
	"strings"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"rbot/brain/command"
	"rbot/brain/config"
)

// req.go: teruskan saran/permintaan fitur dari user ke owner. Port req.js.
// Tujuan diambil dari config.owners[0] — boleh berupa "nomor" atau "id@server".

func init() {
	command.Register(&command.Command{
		Name:        "req",
		Category:    "Info",
		Alias:       []string{"request"},
		Description: "Kirim request/saran fitur ke owner. Contoh: .req tambahin fitur sticker",
		Handler:     reqHandler,
	})
}

func reqHandler(ctx context.Context, c *command.Ctx) error {
	text := strings.TrimSpace(c.ArgStr())
	if text == "" {
		_, err := c.Reply(ctx, "Mau request fitur apa? Contoh: "+config.MainPrefix()+"req tambahin fitur sticker")
		return err
	}

	// Rangkai JID owner tujuan. Node: kalau sudah ada "@" pakai apa adanya,
	// selain itu anggap nomor biasa dan tempel @s.whatsapp.net.
	ownerRaw := ""
	if len(config.C.Owners) > 0 {
		ownerRaw = strings.TrimSpace(config.C.Owners[0])
	}
	if ownerRaw == "" {
		_, err := c.Reply(ctx, "Maaf, request gagal terkirim ke owner. Coba lagi nanti ya.")
		return err
	}
	if !strings.Contains(ownerRaw, "@") {
		ownerRaw += "@s.whatsapp.net"
	}
	ownerJID, err := types.ParseJID(ownerRaw)
	if err != nil {
		_, e := c.Reply(ctx, "Maaf, request gagal terkirim ke owner. Coba lagi nanti ya.")
		return e
	}

	name := strings.TrimSpace(c.Evt.Info.PushName)
	if name == "" {
		name = "Tanpa nama"
	}

	forwarded := "📨 *Request fitur baru*\n\n" +
		"Dari: " + name + " (" + c.Sender().User + ")\n"
	if c.IsGroup() {
		forwarded += "Grup: " + c.Chat().String() + "\n"
	}
	forwarded += "\nPesan:\n" + text

	// Kirim ke owner (tanpa quote — pesan pemicu ada di chat lain).
	if _, err := c.Client.SendMessage(ctx, ownerJID, &waE2E.Message{Conversation: proto.String(forwarded)}); err != nil {
		_, e := c.Reply(ctx, "Maaf, request gagal terkirim ke owner. Coba lagi nanti ya.")
		return e
	}

	_, err = c.Reply(ctx, "Request kamu sudah diteruskan ke owner. Makasih masukannya! 🙌")
	return err
}
