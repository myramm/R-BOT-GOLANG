package cmd

import (
	"context"
	"strings"

	"rbot/brain/command"
	cmdfunc "rbot/cmd/func"
)

// setname.go: ganti nama (subject) grup. Port setname.js.

func init() {
	command.Register(&command.Command{
		Name:        "setname",
		Category:    "Grup",
		Alias:       []string{"setsubject", "gcname"},
		Description: "Ganti nama grup (admin). Contoh: .setname Kelas XII IPA 2",
		Handler:     setnameHandler,
	})
}

func setnameHandler(ctx context.Context, c *command.Ctx) error {
	gate := cmdfunc.EnsureAdminContext(ctx, c)
	if gate.Err != "" {
		_, err := c.Reply(ctx, gate.Err)
		return err
	}
	name := strings.TrimSpace(c.ArgStr())
	if name == "" {
		_, err := c.Reply(ctx, "Mau ganti jadi apa? Contoh: .setname Grup Keluarga")
		return err
	}
	if len([]rune(name)) > 25 {
		_, err := c.Reply(ctx, "Nama grup maksimal 25 karakter ya.")
		return err
	}
	if err := c.Client.SetGroupName(ctx, c.Chat(), name); err != nil {
		_, e := c.Reply(ctx, "Gagal ganti nama: "+err.Error())
		return e
	}
	_, err := c.Reply(ctx, "✅ Nama grup diganti jadi: *"+name+"*")
	return err
}
