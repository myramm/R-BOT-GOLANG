package cmd

import (
	"context"
	"strings"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/settings"
)

func init() {
	command.Register(&command.Command{
		Name:        "set",
		Category:    "Owner",
		Description: "Ubah pengaturan bot (owner). .set autoread on/off",
		OwnerOnly:   true,
		Handler:     setHandler,
	})
}

// parseOnOff mengubah token jadi bool: on/nyala/aktif/1/true → true; off/... → false.
func parseOnOff(s string) (val, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "nyala", "aktif", "1", "true", "enable":
		return true, true
	case "off", "mati", "nonaktif", "0", "false", "disable":
		return false, true
	}
	return false, false
}

func setHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()
	if len(c.Args) == 0 {
		_, err := c.Reply(ctx, "Pengaturan tersedia:\n• *"+mp+"set autoread on/off* — bot otomatis tandai pesan masuk sebagai dibaca")
		return err
	}

	switch strings.ToLower(c.Args[0]) {
	case "autoread":
		if len(c.Args) < 2 {
			status := "off"
			if settings.AutoRead() {
				status = "on"
			}
			_, err := c.Reply(ctx, "Autoread sekarang: *"+status+"*.\nUbah: *"+mp+"set autoread on/off*")
			return err
		}
		on, ok := parseOnOff(c.Args[1])
		if !ok {
			_, err := c.Reply(ctx, "Pakai *on* atau *off*. Contoh: *"+mp+"set autoread on*")
			return err
		}
		if err := settings.SetAutoRead(on); err != nil {
			_, e := c.Reply(ctx, "Gagal menyimpan pengaturan: "+err.Error())
			return e
		}
		word := "dimatikan"
		if on {
			word = "diaktifkan"
		}
		_, err := c.Reply(ctx, "✅ Autoread "+word+". Bot "+ternary(on, "akan", "tidak lagi")+" otomatis menandai pesan masuk sebagai dibaca.")
		return err
	default:
		_, err := c.Reply(ctx, "Pengaturan tidak dikenal: *"+c.Args[0]+"*.\nCoba: *"+mp+"set autoread on/off*")
		return err
	}
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
