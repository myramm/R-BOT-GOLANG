package cmd

import (
	"context"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/sponsor"
)

// sponsor.go: tampilkan media/teks promosi sponsor bot. Port sponsor.js.
// Bila sponsor bertipe image/video dan medianya tersedia, kirim sebagai media
// berkapsi; selain itu kirim teksnya saja.

func init() {
	command.Register(&command.Command{
		Name:        "sponsor",
		Category:    "Info",
		Alias:       []string{"promo"},
		Description: "Lihat media promosi/sponsor bot",
		Handler:     sponsorHandler,
	})
}

func sponsorHandler(ctx context.Context, c *command.Ctx) error {
	s := sponsor.Get()
	if s == nil {
		_, err := c.Reply(ctx, "📣 Belum ada sponsor saat ini.\n\n_Owner bisa mengatur lewat_ *"+config.MainPrefix()+"setsponsor*")
		return err
	}

	caption := sponsor.Text()

	if s.Type == "image" || s.Type == "video" {
		if media := sponsor.Media(); media != nil {
			kind := command.MediaImage
			if s.Type == "video" {
				kind = command.MediaVideo
			}
			return c.SendMediaBytes(ctx, media, kind, caption, "", "")
		}
	}

	if caption == "" {
		caption = "📣 Sponsor"
	}
	_, err := c.Reply(ctx, caption)
	return err
}
