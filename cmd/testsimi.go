package cmd

import (
	"context"
	"strings"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/simi"
)

func init() {
	command.Register(&command.Command{
		Name:        "testsimi",
		Category:    "AI",
		Alias:       []string{"simitest"},
		Description: "Uji respon Simi-Simi AI secara langsung tanpa perlu reply quote",
		Handler:     testSimiHandler,
	})
}

func testSimiHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()
	input := strings.TrimSpace(c.ArgStr())

	if input == "" {
		_, err := c.Reply(ctx, "Masukkan pesan yang ingin dites ke Simi-Simi.\n\n*Contoh:* `"+mp+"testsimi halo bot ganteng`")
		return err
	}

	c.React(ctx, "⏳")
	reply, err := simi.AskSimi(ctx, input)
	if err != nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, "Gagal memanggil Simi: "+err.Error())
		return e
	}

	c.React(ctx, "✅")
	_, err = c.Reply(ctx, reply)
	return err
}
