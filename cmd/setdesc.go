package cmd

import (
	"context"
	"strings"

	"rbot/brain/command"
	cmdfunc "rbot/cmd/func"
)

// setdesc.go: ganti/kosongkan deskripsi (topic) grup. Port setdesc.js.
// whatsmeow SetGroupTopic mengambil previousID/newID kosong → di-fetch/di-generate
// otomatis. Deskripsi kosong = hapus topic.

func init() {
	command.Register(&command.Command{
		Name:        "setdesc",
		Category:    "Grup",
		Alias:       []string{"setdescription", "gcdesc"},
		Description: "Ganti deskripsi grup (admin). Kosongkan teks untuk menghapus. Contoh: .setdesc Aturan grup...",
		Handler:     setdescHandler,
	})
}

func setdescHandler(ctx context.Context, c *command.Ctx) error {
	gate := cmdfunc.EnsureAdminContext(ctx, c)
	if gate.Err != "" {
		_, err := c.Reply(ctx, gate.Err)
		return err
	}
	desc := strings.TrimSpace(c.ArgStr())
	if err := c.Client.SetGroupTopic(ctx, c.Chat(), "", "", desc); err != nil {
		_, e := c.Reply(ctx, "Gagal ganti deskripsi: "+err.Error())
		return e
	}
	msg := "✅ Deskripsi grup diperbarui."
	if desc == "" {
		msg = "✅ Deskripsi grup dikosongkan."
	}
	_, err := c.Reply(ctx, msg)
	return err
}
