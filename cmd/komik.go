package cmd

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/command"
	"rbot/lib/komik"
	"rbot/lib/nhentai"
)

type komikSessionStep int

const (
	stepSelectComic komikSessionStep = iota + 1
	stepSelectChapter
)

type komikSession struct {
	Step          komikSessionStep
	SearchResults []komik.Comic
	SelectedComic *komik.Comic
	Chapters      []komik.Chapter
	CreatedAt     time.Time
}

var (
	komikSessMu   sync.Mutex
	komikSessions = make(map[string]*komikSession)
)

func init() {
	command.Register(&command.Command{
		Name:        "komik",
		Category:    "Downloader",
		Alias:       []string{"manga", "comic", "nhentai", "doujin", "manhwa", "manhua"},
		Description: "Cari & download komik jadi PDF (KomikTap & Komiku), atau kode nhentai jadi PDF doujin. Contoh: .komik solo leveling | .komik solo leveling 5 | .komik 177013",
		Handler:     komikHandler,
	})

	// Hook interaktif untuk menangkap respon nomor dari user
	prevHook := command.ResumeHook
	command.ResumeHook = func(ctx context.Context, client *whatsmeow.Client, evt *events.Message, text string) {
		if handleKomikSessionReply(ctx, client, evt, text) {
			return
		}
		if prevHook != nil {
			prevHook(ctx, client, evt, text)
		}
	}
}

func getKomikSessionKey(evt *events.Message) string {
	return evt.Info.Chat.String() + ":" + evt.Info.Sender.String()
}

func saveKomikSession(key string, sess *komikSession) {
	komikSessMu.Lock()
	defer komikSessMu.Unlock()
	sess.CreatedAt = time.Now()
	komikSessions[key] = sess
}

func getKomikSession(key string) *komikSession {
	komikSessMu.Lock()
	defer komikSessMu.Unlock()
	sess, ok := komikSessions[key]
	if !ok {
		return nil
	}
	if time.Since(sess.CreatedAt) > 5*time.Minute {
		delete(komikSessions, key)
		return nil
	}
	return sess
}

func clearKomikSession(key string) {
	komikSessMu.Lock()
	defer komikSessMu.Unlock()
	delete(komikSessions, key)
}

func safeKomikFileName(title, chapterNum string) string {
	clean := regexp.MustCompile(`[^\w\s\.-]`).ReplaceAllString(title, "")
	clean = regexp.MustCompile(`\s+`).ReplaceAllString(clean, "_")
	if clean == "" {
		clean = "komik"
	}
	return fmt.Sprintf("%s_Ch_%s.pdf", clean, chapterNum)
}

func komikHandler(ctx context.Context, c *command.Ctx) error {
	argStr := strings.TrimSpace(c.ArgStr())
	if argStr == "" {
		_, err := c.Reply(ctx, "Mau cari komik apa?\n\n*Contoh:*\n• `.komik solo leveling` (Cari & pilih chapter)\n• `.komik solo leveling 5` (Langsung download Chapter 5)")
		return err
	}

	key := getKomikSessionKey(c.Evt)
	args := strings.Fields(argStr)

	// Kasus: kode nhentai (angka murni) -> PDF doujin (contoh: .komik 177013)
	if isNhentaiCode(argStr) {
		go processNhentaiPDF(ctx, c, argStr)
		return nil
	}

	// Kasus: Judul + Nomor Chapter (contoh: .komik solo leveling 5)
	if len(args) >= 2 {
		lastArg := args[len(args)-1]
		if _, err := strconv.ParseFloat(lastArg, 64); err == nil {
			query := strings.Join(args[:len(args)-1], " ")
			c.React(ctx, "🔎")

			results, err := komik.SearchComics(ctx, query)
			if err != nil || len(results) == 0 {
				c.React(ctx, "❌")
				if err != nil {
					c.ReportError(ctx, err)
				}
				_, e := c.Reply(ctx, fmt.Sprintf("Komik '%s' tidak ditemukan.", query))
				return e
			}

			topComic := results[0]
			c.React(ctx, "⏳")

			chapters, err := komik.GetChapters(ctx, topComic)
			if err != nil || len(chapters) == 0 {
				c.React(ctx, "❌")
				if err != nil {
					c.ReportError(ctx, err)
				}
				_, e := c.Reply(ctx, fmt.Sprintf("Gagal mengambil chapter untuk '%s'.", topComic.Title))
				return e
			}

			var targetCh *komik.Chapter
			for _, ch := range chapters {
				if ch.Num == lastArg || strings.EqualFold(ch.Num, lastArg) {
					targetCh = &ch
					break
				}
			}
			if targetCh == nil {
				targetCh = &chapters[len(chapters)-1]
			}

			go processAndSendPDF(ctx, c, topComic, *targetCh)
			return nil
		}
	}

	// Pencarian Standar (.komik solo leveling)
	c.React(ctx, "🔎")
	results, err := komik.SearchComics(ctx, argStr)
	if err != nil {
		c.React(ctx, "❌")
		c.ReportError(ctx, err)
		_, e := c.Reply(ctx, fmt.Sprintf("Gagal mencari komik: %s", err.Error()))
		return e
	}
	if len(results) == 0 {
		c.React(ctx, "🤷")
		_, e := c.Reply(ctx, fmt.Sprintf("Tidak ditemukan komik untuk kata kunci '%s'.", argStr))
		return e
	}

	c.React(ctx, "📚")
	maxShow := 10
	if len(results) < maxShow {
		maxShow = len(results)
	}
	slicedResults := results[:maxShow]

	saveKomikSession(key, &komikSession{
		Step:          stepSelectComic,
		SearchResults: slicedResults,
	})

	var b strings.Builder
	fmt.Fprintf(&b, "📚 *Hasil Pencarian Komik: '%s'*\n\n", argStr)
	for i, r := range slicedResults {
		srcInfo := "KomikTap"
		if r.Source == "komiku" {
			srcInfo = "Komiku"
		}
		fmt.Fprintf(&b, "%d. *%s* [%s]\n", i+1, r.Title, srcInfo)
	}
	fmt.Fprintf(&b, "\n👉 *Balas dengan nomor komik (1 - %d) yang ingin kamu baca.*\n_Ketik \"batal\" untuk membatalkan._", maxShow)

	_, err = c.Reply(ctx, b.String())
	return err
}

func handleKomikSessionReply(ctx context.Context, client *whatsmeow.Client, evt *events.Message, text string) bool {
	clean := strings.TrimSpace(text)
	if strings.EqualFold(clean, "batal") || strings.EqualFold(clean, "cancel") {
		key := getKomikSessionKey(evt)
		if getKomikSession(key) != nil {
			clearKomikSession(key)
			c := &command.Ctx{Client: client, Evt: evt, Text: text}
			_, _ = c.Reply(ctx, "Oke, pencarian komik dibatalkan.")
			return true
		}
		return false
	}

	key := getKomikSessionKey(evt)
	sess := getKomikSession(key)
	if sess == nil {
		return false
	}

	c := &command.Ctx{Client: client, Evt: evt, Text: text}

	switch sess.Step {
	case stepSelectComic:
		num, err := strconv.Atoi(clean)
		if err != nil || num <= 0 || num > len(sess.SearchResults) {
			return false
		}

		selected := sess.SearchResults[num-1]
		c.React(ctx, "⏳")

		chapters, err := komik.GetChapters(ctx, selected)
		if err != nil || len(chapters) == 0 {
			c.React(ctx, "❌")
			if err != nil {
				c.ReportError(ctx, err)
			}
			_, _ = c.Reply(ctx, fmt.Sprintf("Gagal mengambil daftar chapter untuk '%s'.", selected.Title))
			clearKomikSession(key)
			return true
		}
		c.React(ctx, "📖")

		sess.Step = stepSelectChapter
		sess.SelectedComic = &selected
		sess.Chapters = chapters
		saveKomikSession(key, sess)

		firstNum := chapters[0].Num
		lastNum := chapters[len(chapters)-1].Num

		var b strings.Builder
		fmt.Fprintf(&b, "📚 *%s*\n", selected.Title)
		fmt.Fprintf(&b, "Tersedia Chapter %s – %s (%d total chapter).\n\n", firstNum, lastNum, len(chapters))
		fmt.Fprintf(&b, "Mau chapter berapa? Balas angka chapternya (misal: *%s*).\n_Ketik \"batal\" untuk membatalkan._", firstNum)

		_, _ = c.Reply(ctx, b.String())
		return true

	case stepSelectChapter:
		if sess.SelectedComic == nil || len(sess.Chapters) == 0 {
			clearKomikSession(key)
			return false
		}

		var targetCh *komik.Chapter
		for _, ch := range sess.Chapters {
			if ch.Num == clean || strings.EqualFold(ch.Num, clean) {
				targetCh = &ch
				break
			}
		}

		if targetCh == nil {
			numVal, err := strconv.Atoi(clean)
			if err == nil && numVal > 0 && numVal <= len(sess.Chapters) {
				targetCh = &sess.Chapters[numVal-1]
			}
		}

		if targetCh == nil {
			_, _ = c.Reply(ctx, fmt.Sprintf("Chapter '%s' tidak ditemukan. Silakan masukkan nomor chapter yang valid.", clean))
			return true
		}

		comicInfo := *sess.SelectedComic
		clearKomikSession(key)

		go processAndSendPDF(ctx, c, comicInfo, *targetCh)
		return true
	}

	return false
}

func isNhentaiCode(s string) bool {
	if s == "" || len(s) > 7 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func processNhentaiPDF(ctx context.Context, c *command.Ctx, code string) {
	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	c.React(bgCtx, "🔎")

	g, err := nhentai.Fetch(bgCtx, code)
	if err != nil {
		c.React(bgCtx, "❌")
		c.ReportError(bgCtx, err)
		_, _ = c.Reply(bgCtx, fmt.Sprintf("Doujin dengan kode *%s* tidak ditemukan.", code))
		return
	}

	urls := g.ImageURLs()
	c.React(bgCtx, "⏳")
	_, _ = c.Reply(bgCtx, fmt.Sprintf("📥 Mengunduh *%s* (%d halaman) & merakit PDF...\nHarap tunggu sebentar.", g.Title, len(urls)))

	pdfBytes, valid, total, err := komik.ImagesToPDF(bgCtx, urls, "https://nhentai.net/")
	if err != nil || len(pdfBytes) == 0 {
		c.React(bgCtx, "❌")
		errMsg := fmt.Sprintf("❌ Gagal membuat PDF: %v", err)
		c.ReportErrorMessage(bgCtx, errMsg)
		_, _ = c.Reply(bgCtx, errMsg)
		return
	}

	fileSizeMB := float64(len(pdfBytes)) / 1024 / 1024
	warn := ""
	if valid < total {
		warn = fmt.Sprintf("\n⚠️ %d/%d halaman gagal diunduh.", total-valid, total)
	}
	caption := fmt.Sprintf("📕 *%s*\n%d/%d halaman • %.1f MB%s", g.Title, valid, total, fileSizeMB, warn)

	if err := c.SendMediaBytes(bgCtx, pdfBytes, command.MediaDocument, caption, nhentai.FileName(g.Title), "application/pdf"); err != nil {
		c.React(bgCtx, "❌")
		errMsg := fmt.Sprintf("PDF berhasil dirakit (%.1f MB) tetapi gagal dikirim ke WhatsApp: %v", fileSizeMB, err)
		c.ReportErrorMessage(bgCtx, errMsg)
		_, _ = c.Reply(bgCtx, errMsg)
		return
	}

	c.React(bgCtx, "✅")
}

func processAndSendPDF(ctx context.Context, c *command.Ctx, comic komik.Comic, ch komik.Chapter) {
	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	c.React(bgCtx, "⏳")

	imageURLs, err := komik.GetChapterImages(bgCtx, ch)
	if err != nil || len(imageURLs) == 0 {
		c.React(bgCtx, "❌")
		errMsg := fmt.Sprintf("Gagal mendapatkan gambar chapter %s: %v", ch.Num, err)
		c.ReportErrorMessage(bgCtx, errMsg)
		_, _ = c.Reply(bgCtx, errMsg)
		return
	}

	_, _ = c.Reply(bgCtx, fmt.Sprintf("📥 Mengunduh *%s* Chapter %s (%d halaman) & merakit PDF...\nHarap tunggu sebentar.", comic.Title, ch.Num, len(imageURLs)))

	pdfBytes, valid, total, err := komik.ImagesToPDF(bgCtx, imageURLs, comic.Link)
	if err != nil || len(pdfBytes) == 0 {
		c.React(bgCtx, "❌")
		errMsg := fmt.Sprintf("❌ Gagal membuat PDF: %v", err)
		c.ReportErrorMessage(bgCtx, errMsg)
		_, _ = c.Reply(bgCtx, errMsg)
		return
	}

	fileName := safeKomikFileName(comic.Title, ch.Num)
	fileSizeMB := float64(len(pdfBytes)) / 1024 / 1024

	warn := ""
	if valid < total {
		warn = fmt.Sprintf("\n⚠️ %d/%d halaman gagal diunduh.", total-valid, total)
	}

	caption := fmt.Sprintf("📕 *%s*\nChapter %s • %d/%d halaman • %.1f MB%s", comic.Title, ch.Num, valid, total, fileSizeMB, warn)

	if err := c.SendMediaBytes(bgCtx, pdfBytes, command.MediaDocument, caption, fileName, "application/pdf"); err != nil {
		c.React(bgCtx, "❌")
		errMsg := fmt.Sprintf("PDF berhasil dirakit (%.1f MB) tetapi gagal dikirim ke WhatsApp: %v", fileSizeMB, err)
		c.ReportErrorMessage(bgCtx, errMsg)
		_, _ = c.Reply(bgCtx, errMsg)
		return
	}

	c.React(bgCtx, "✅")
}
