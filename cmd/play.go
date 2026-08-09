package cmd

import (
	"context"
	"strings"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/lib/ytsearch"
)

func init() {
	command.Register(&command.Command{
		Name:        "play",
		Category:    "Downloader",
		Alias:       []string{"song", "lagu"},
		Description: "Cari lagu di YouTube lalu kirim audionya. Contoh: .play judul lagu",
		Handler:     playHandler,
	})
}

func playHandler(ctx context.Context, c *command.Ctx) error {
	query := strings.TrimSpace(c.ArgStr())
	if query == "" {
		_, err := c.Reply(ctx, "Mau cari lagu apa? Contoh: "+config.MainPrefix()+"play akhir tak bahagia")
		return err
	}

	c.React(ctx, "🔎")

	video, err := ytsearch.Search(ctx, query)
	if err != nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, "Gagal mencari: "+err.Error()+". Coba lagi sebentar lagi.")
		return e
	}
	if video == nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, "Tidak ada hasil untuk \""+query+"\", atau sumber pencarian sedang down. Coba kata kunci lain atau ulangi sebentar lagi.")
		return e
	}

	var b strings.Builder
	b.WriteString("🎵 *" + video.Title + "*\n")
	if video.Author != "" {
		b.WriteString("👤 " + video.Author + "\n")
	}
	if video.Timestamp != "" {
		b.WriteString("⏱️ " + video.Timestamp + "\n")
	}
	b.WriteString("🔗 " + video.URL + "\n\n_Mengunduh audio..._")
	if _, err := c.Reply(ctx, b.String()); err != nil {
		return err
	}

	// Delegasikan ke command download dengan paksa format mp3 (port pemanggilan
	// download.handler(sock, msg, [video.url, 'mp3']) di play.js). Salin Ctx dangkal
	// lalu ganti Args supaya handler download memroses link hasil pencarian.
	dc := *c
	dc.Args = []string{video.URL, "mp3"}
	dc.InvokedAs = "download"
	return downloadHandler(ctx, &dc)
}
