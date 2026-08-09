package cmd

import (
	"context"
	"strings"

	"rbot/brain/command"
	cmdfunc "rbot/cmd/func"
)

// link.go: tampilkan link undangan grup; ".link reset" untuk mencabut & ganti.
// Port link.js. whatsmeow GetGroupInviteLink(reset) mengembalikan URL penuh
// (https://chat.whatsapp.com/<code>), jadi tak perlu merangkai manual.

func init() {
	command.Register(&command.Command{
		Name:        "link",
		Category:    "Grup",
		Alias:       []string{"linkgrup", "grouplink"},
		Description: "Tampilkan link undangan grup (admin). .link reset untuk ganti link.",
		Handler:     linkHandler,
	})
}

func linkHandler(ctx context.Context, c *command.Ctx) error {
	gate := cmdfunc.EnsureAdminContext(ctx, c)
	if gate.Err != "" {
		_, err := c.Reply(ctx, gate.Err)
		return err
	}
	reset := len(c.Args) > 0 && strings.EqualFold(c.Args[0], "reset")
	url, err := c.Client.GetGroupInviteLink(ctx, c.Chat(), reset)
	if err != nil {
		_, e := c.Reply(ctx, "Gagal mengambil link: "+err.Error())
		return e
	}
	msg := "🔗 Link grup *" + gate.Info.Name + "*:\n" + url
	if reset {
		msg += "\n\n_(link lama sudah dinonaktifkan)_"
	}
	_, err = c.Reply(ctx, msg)
	return err
}
