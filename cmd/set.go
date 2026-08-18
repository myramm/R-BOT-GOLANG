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
	case "mode":
		if len(c.Args) < 2 {
			curr := "public"
			if settings.IsSelfMode() {
				curr = "self"
			}
			_, err := c.Reply(ctx, "Mode bot saat ini: *"+curr+"*.\nUbah: *"+mp+"set mode self* atau *"+mp+"set mode public*")
			return err
		}
		val := strings.ToLower(c.Args[1])
		switch val {
		case "self", "private", "owner", "1", "on":
			if err := settings.SetSelfMode(true); err != nil {
				_, e := c.Reply(ctx, "Gagal menyimpan mode: "+err.Error())
				return e
			}
			_, err := c.Reply(ctx, "🔒 *Mode Bot diubah ke SELF*\nSekarang hanya Owner yang dapat menggunakan bot.")
			return err
		case "public", "publik", "all", "0", "off":
			if err := settings.SetSelfMode(false); err != nil {
				_, e := c.Reply(ctx, "Gagal menyimpan mode: "+err.Error())
				return e
			}
			_, err := c.Reply(ctx, "🌐 *Mode Bot diubah ke PUBLIC*\nSekarang semua orang dapat menggunakan bot.")
			return err
		default:
			_, err := c.Reply(ctx, "Pilihan tidak valid. Gunakan *"+mp+"set mode self* atau *"+mp+"set mode public*")
			return err
		}
	default:
		_, err := c.Reply(ctx, "Pengaturan tidak dikenal: *"+c.Args[0]+"*.\nPilihan: *autoread*, *mode*")
		return err
	}
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
