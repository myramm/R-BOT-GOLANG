package cmd

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/hentailimit"
	"rbot/brain/identity"
	"rbot/brain/premium"
	"rbot/lib/minioppai"
	"rbot/lib/samehadaku"
)

type hentaiSessionStep int

const (
	stepSelectHentaiSeries hentaiSessionStep = iota + 1
	stepSelectHentaiEpisode
	stepSelectHentaiQuality
)

type hentaiSession struct {
	Step      hentaiSessionStep
	Results   []minioppai.SearchResult
	Series    *minioppai.SearchResult
	Info      *minioppai.SeriesInfo
	Episodes  []minioppai.EpisodeLink
	Title     string
	Options   []minioppai.DownloadOption // opsi stream (mirror) bila ada
	Download  *minioppai.EpisodeDownload // section download per kualitas/provider
	CreatedAt time.Time
}

var (
	hentaiMu       sync.Mutex
	hentaiSessions = make(map[string]*hentaiSession)
)

const maxHentaiDocBytes = 2 * 1024 * 1024 * 1024 // 2GB batas dokumen WhatsApp

func init() {
	command.Register(&command.Command{
		Name:        "hentai",
		Category:    "Downloader",
		Description: "Cari & download video per episode & kualitas dari MiniOppai. Contoh: .hentai kotowarenai | .hentai <link episode minioppai>",
		Handler:     hentaiHandler,
	})

	// Register interaktif ResumeHook untuk menangkap nomor pilihan (1, 2, dst)
	prevHook := command.ResumeHook
	command.ResumeHook = func(ctx context.Context, client *whatsmeow.Client, evt *events.Message, text string) {
		if handleHentaiSessionReply(ctx, client, evt, text) {
			return
		}
		if prevHook != nil {
			prevHook(ctx, client, evt, text)
		}
	}
}

func saveHentaiSession(key string, sess *hentaiSession) {
	hentaiMu.Lock()
	defer hentaiMu.Unlock()
	sess.CreatedAt = time.Now()
	hentaiSessions[key] = sess
}

func getHentaiSession(key string) *hentaiSession {
	hentaiMu.Lock()
	defer hentaiMu.Unlock()
	sess, ok := hentaiSessions[key]
	if !ok {
		return nil
	}
	if time.Since(sess.CreatedAt) > 5*time.Minute {
		delete(hentaiSessions, key)
		return nil
	}
	return sess
}

func clearHentaiSession(key string) {
	hentaiMu.Lock()
	defer hentaiMu.Unlock()
	delete(hentaiSessions, key)
}

// isGrupOfficialChat true bila pesan berasal dari grup official (dicocokkan
// dengan grupOfficial.jid pada bagian nomor saja, tanpa peduli server).
func isGrupOfficialChat(evt *events.Message) bool {
	if evt == nil || !evt.Info.IsGroup || evt.Info.Chat.IsEmpty() {
		return false
	}
	official := strings.TrimSpace(config.C.GrupOfficial.JID)
	if official == "" {
		return false
	}
	return config.BareNumber(official) == evt.Info.Chat.User
}

// hentaiIzin memeriksa izin download .hentai: premium/owner bebas semua,
// user free hanya kualitas <= 480p dengan kuota harian.
// Return (boleh, pesanPenolakan); pesan kosong bila boleh.
func hentaiIzin(userKey, quality string, dalamGrup, isPrem bool) (bool, string) {
	if isPrem {
		return true, ""
	}
	return hentailimit.Check(userKey, quality, dalamGrup)
}

// hentaiIzinFallback memeriksa kuota free untuk fallback direct-MP4 (tanpa
// pilihan kualitas) lalu mencatat pemakaian bila diizinkan.
// Return pesan penolakan; kosong bila boleh.
func hentaiIzinFallback(evt *events.Message) string {
	userKey := identity.SenderPhone(evt)
	dalamGrup := isGrupOfficialChat(evt)
	isPrem := premium.IsPremium(evt)
	ok, tolak := hentaiIzin(userKey, "MP4", dalamGrup, isPrem)
	if !ok {
		return tolak
	}
	if !isPrem {
		hentailimit.Record(userKey, "MP4")
	}
	return ""
}

func hentaiHandler(ctx context.Context, c *command.Ctx) error {
	argStr := strings.TrimSpace(c.ArgStr())
	if argStr == "" {
		mp := config.MainPrefix()
		_, err := c.Reply(ctx, fmt.Sprintf("Masukkan judul atau link episode MiniOppai.\n\n*Contoh:*\n• `%shentai kotowarenai` (Cari → pilih series → episode → kualitas)\n• `%shentai <link episode minioppai>`\n\n⚠️ _Konten 18+. Disarankan di chat pribadi._", mp, mp))
		return err
	}

	key := getSessionKey(c.Evt)

	// Case 0: Link episode minioppai langsung -> pilihan download/kualitas.
	if isMiniOppaiURL(argStr) {
		handleDirectMiniOppaiLink(ctx, c, key, argStr)
		return nil
	}

	// Case 1: Pencarian standar (.hentai kotowarenai) dari MiniOppai.
	c.React(ctx, "⏳")
	results, err := minioppai.Search(ctx, argStr)
	if err != nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, fmt.Sprintf("Gagal mencari: %s", err.Error()))
		return e
	}
	if len(results) == 0 {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, fmt.Sprintf("'%s' tidak ditemukan di MiniOppai.", argStr))
		return e
	}

	c.React(ctx, "✅")
	saveHentaiSession(key, &hentaiSession{
		Step:    stepSelectHentaiSeries,
		Results: results,
	})

	_, err = c.Reply(ctx, formatHentaiChoices(argStr, results))
	return err
}

// isMiniOppaiURL menandai tautan dari provider minioppai.
func isMiniOppaiURL(u string) bool {
	return strings.Contains(strings.ToLower(u), "minioppai.org")
}

// handleDirectMiniOppaiLink menangani URL episode minioppai yang diberikan
// langsung: utamakan pilihan download, lalu opsi stream sebagai fallback.
func handleDirectMiniOppaiLink(ctx context.Context, c *command.Ctx, key, url string) bool {
	c.React(ctx, "⏳")
	title := "Episode"

	download, dlErr := minioppai.GetEpisodeDownloads(ctx, url)
	if dlErr == nil && len(download.Qualities) > 0 {
		if download.Title != "" {
			title = download.Title
		}
		c.React(ctx, "✅")
		saveHentaiSession(key, &hentaiSession{
			Step:     stepSelectHentaiQuality,
			Title:    title,
			Download: download,
		})
		_, _ = c.Reply(ctx, formatHentaiDownloadChoices(title, download, c.Evt))
		return true
	}

	ep, epErr := minioppai.GetEpisode(ctx, url)
	if epErr == nil && ep.Title != "" {
		title = ep.Title
	}
	opts := minioppai.GetStreamOptions(ctx, url)
	if len(opts) > 0 {
		c.React(ctx, "✅")
		saveHentaiSession(key, &hentaiSession{
			Step:    stepSelectHentaiQuality,
			Title:   title,
			Options: opts,
		})
		_, _ = c.Reply(ctx, formatHentaiQualityChoices(title, opts, c.Evt))
		return true
	}

	if tolak := hentaiIzinFallback(c.Evt); tolak != "" {
		c.React(ctx, "🚫")
		_, _ = c.Reply(ctx, tolak)
		return true
	}
	c.React(ctx, "✅")
	_, _ = c.Reply(ctx, fmt.Sprintf("🔞 *%s*\n\n📺 *Link nonton:*\n%s", title, url))
	return true
}

// sessSelectMiniOppaiSeries menangani user memilih series minioppai dari hasil
// pencarian: memuat daftar episode & info series, lalu menampilkan episode.
func sessSelectMiniOppaiSeries(ctx context.Context, c *command.Ctx, key string, sess *hentaiSession, selected minioppai.SearchResult, evt *events.Message) bool {
	episodes, err := minioppai.GetEpisodeList(ctx, selected.URL)
	if err != nil {
		c.React(ctx, "❌")
		clearHentaiSession(key)
		_, _ = c.Reply(ctx, fmt.Sprintf("Gagal memuat daftar episode: %s", err.Error()))
		return true
	}
	c.React(ctx, "✅")

	sess.Episodes = episodes
	sess.Step = stepSelectHentaiEpisode
	sess.Series = &selected
	saveHentaiSession(key, sess)

	// Ambil sinopsis & genre series (best-effort).
	var info *minioppai.SeriesInfo
	if info, err = minioppai.GetSeriesInfo(ctx, selected.URL); err == nil {
		info.URL = selected.URL
		sess.Info = info
		saveHentaiSession(key, sess)
		if info.Thumbnail != "" {
			_ = c.SendMedia(ctx, info.Thumbnail, command.MediaImage, "", "", "", 10*1024*1024)
		}
	}

	_, _ = c.Reply(ctx, formatHentaiEpisodeList(info, episodes))
	return true
}

// sessSelectMiniOppaiEpisode menangani user memilih episode minioppai: menampilkan
// pilihan download bila ada, lalu opsi stream, atau fallback ke link halaman.
func sessSelectMiniOppaiEpisode(ctx context.Context, c *command.Ctx, key string, sess *hentaiSession, targetEp minioppai.EpisodeLink, evt *events.Message) bool {
	title := targetEp.Title
	if title == "" {
		title = "Episode " + targetEp.Number
	}

	download, dlErr := minioppai.GetEpisodeDownloads(ctx, targetEp.URL)
	if dlErr == nil && len(download.Qualities) > 0 {
		if download.Title != "" {
			title = download.Title
		}
		c.React(ctx, "✅")
		sess.Step = stepSelectHentaiQuality
		sess.Title = title
		sess.Download = download
		sess.Options = nil
		saveHentaiSession(key, sess)
		_, _ = c.Reply(ctx, formatHentaiDownloadChoices(title, download, evt))
		return true
	}

	opts := minioppai.GetStreamOptions(ctx, targetEp.URL)
	if len(opts) > 0 {
		c.React(ctx, "✅")
		sess.Step = stepSelectHentaiQuality
		sess.Title = title
		sess.Options = opts
		sess.Download = nil
		saveHentaiSession(key, sess)
		_, _ = c.Reply(ctx, formatHentaiQualityChoices(title, opts, evt))
		return true
	}

	// Tak ada download maupun mirror: berikan tautan halaman episode.
	if tolak := hentaiIzinFallback(evt); tolak != "" {
		c.React(ctx, "🚫")
		_, _ = c.Reply(ctx, tolak)
		clearHentaiSession(key)
		return true
	}
	c.React(ctx, "✅")
	_, _ = c.Reply(ctx, fmt.Sprintf("🔞 *%s*\n\n📺 *Link nonton:*\n%s", title, targetEp.URL))
	clearHentaiSession(key)
	return true
}

func handleHentaiSessionReply(ctx context.Context, client *whatsmeow.Client, evt *events.Message, text string) bool {
	key := evt.Info.Chat.String() + ":" + evt.Info.Sender.String()
	sess := getHentaiSession(key)
	if sess == nil {
		return false
	}

	c := &command.Ctx{Client: client, Evt: evt, Text: text}
	cleanInput := strings.TrimSpace(text)

	switch sess.Step {
	case stepSelectHentaiSeries:
		choiceNum, err := strconv.Atoi(cleanInput)
		if err != nil || choiceNum <= 0 || choiceNum > len(sess.Results) {
			return false
		}
		selected := sess.Results[choiceNum-1]
		c.React(ctx, "⏳")
		return sessSelectMiniOppaiSeries(ctx, c, key, sess, selected, evt)

	case stepSelectHentaiEpisode:
		if len(sess.Episodes) == 0 {
			clearHentaiSession(key)
			return false
		}
		choiceNum, err := strconv.Atoi(cleanInput)
		if err != nil || choiceNum <= 0 || choiceNum > len(sess.Episodes) {
			return false
		}
		targetEp := sess.Episodes[choiceNum-1]
		c.React(ctx, "⏳")
		return sessSelectMiniOppaiEpisode(ctx, c, key, sess, targetEp, evt)

	case stepSelectHentaiQuality:
		choiceNum, err := strconv.Atoi(cleanInput)
		if err != nil || choiceNum <= 0 {
			return false
		}

		// Mode 1: section download (per kualitas + provider).
		if sess.Download != nil && len(sess.Download.Qualities) > 0 {
			if choiceNum > len(sess.Download.Qualities) {
				_, _ = c.Reply(ctx, fmt.Sprintf("Nomor kualitas tidak valid. Pilih 1 - %d.", len(sess.Download.Qualities)))
				return true
			}
			return handleHentaiDownloadQuality(ctx, c, key, sess, choiceNum-1, evt)
		}

		// Mode 2: opsi stream.
		if sess.Options == nil || len(sess.Options) == 0 {
			clearHentaiSession(key)
			return false
		}
		if choiceNum > len(sess.Options) {
			_, _ = c.Reply(ctx, fmt.Sprintf("Nomor kualitas tidak valid. Pilih 1 - %d.", len(sess.Options)))
			return true
		}
		selectedOpt := sess.Options[choiceNum-1]

		if tolak := hentaiIzinFallback(evt); tolak != "" {
			c.React(ctx, "🚫")
			_, _ = c.Reply(ctx, tolak)
			return true
		}
		c.React(ctx, "✅")
		clearHentaiSession(key)
		_, _ = c.Reply(ctx, fmt.Sprintf("🔞 *%s*\nKualitas: %s\n\n📺 *Link streaming:*\n%s", sess.Title, selectedOpt.Quality, selectedOpt.URL))
		return true
	}

	return false
}

// handleHentaiDownloadQuality mengirim file/tautan untuk satu kualitas pada
// section download: resolve provider lalu kirim langsung bila memungkinkan.
func handleHentaiDownloadQuality(ctx context.Context, c *command.Ctx, key string, sess *hentaiSession, qualIdx int, evt *events.Message) bool {
	qual := sess.Download.Qualities[qualIdx]

	dalamGrup := isGrupOfficialChat(evt)
	isPrem := premium.IsPremium(evt)
	userKey := identity.SenderPhone(evt)

	if ok, tolak := hentaiIzin(userKey, qual.Quality, dalamGrup, isPrem); !ok {
		c.React(ctx, "🚫")
		_, _ = c.Reply(ctx, tolak)
		return true
	}
	if !isPrem {
		hentailimit.Record(userKey, qual.Quality)
	}

	title := sess.Title
	if title == "" {
		title = sess.Download.Title
	}

	c.React(ctx, "⏳")
	clearHentaiSession(key)
	_, _ = c.Reply(ctx, fmt.Sprintf("⏳ *Memproses %s (%s)...*\nProses download berjalan di background agar bot tetap responsif menerima pesan lain.", title, qual.Quality))

	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		resolved := make([]minioppai.DownloadLink, len(qual.Links))
		var fileSent bool

		for i, l := range qual.Links {
			directURL := samehadaku.ResolveDirectLink(bgCtx, l.Server, l.URL)
			resolved[i] = minioppai.DownloadLink{
				Server:    l.Server,
				URL:       l.URL,
				DirectURL: directURL,
			}
			if !fileSent && directURL != "" && directURL != l.URL {
				tmpPath, dlErr := downloadToTempFile(bgCtx, directURL, 10*time.Minute, maxHentaiDocBytes)
				if dlErr == nil && tmpPath != "" {
					fileData, readErr := os.ReadFile(tmpPath)
					os.Remove(tmpPath)
					if readErr == nil && len(fileData) > 0 {
						fileName := safeHentaiFileName(title, qual.Quality)
						caption := fmt.Sprintf("🔞 *%s*\nKualitas: %s", title, qual.Quality)
						if sendErr := c.SendMediaBytesWithMetadata(bgCtx, fileData, command.MediaVideo, caption, fileName, "video/mp4", nil); sendErr == nil {
							fileSent = true
							c.React(bgCtx, "✅")
						}
					}
				}
			}
		}

		if !fileSent {
			c.React(bgCtx, "✅")
			updated := qual
			updated.Links = resolved
			_, _ = c.Reply(bgCtx, formatHentaiFinalDownloads(title, updated))
		}
	}()
	return true
}

// safeHentaiFileName membuat nama file video dari judul & kualitas.
func safeHentaiFileName(title, quality string) string {
	cleanTitle := regexp.MustCompile(`[^\w\s\.-]`).ReplaceAllString(title, "")
	cleanTitle = regexp.MustCompile(`\s+`).ReplaceAllString(cleanTitle, "_")
	q := regexp.MustCompile(`[^\w]`).ReplaceAllString(quality, "")
	if q != "" {
		return fmt.Sprintf("%s_%s.mp4", cleanTitle, q)
	}
	return cleanTitle + ".mp4"
}

func formatHentaiChoices(query string, results []minioppai.SearchResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🔎 *Hasil Pencarian MiniOppai: '%s'*\n\n", query)

	maxShow := 8
	if len(results) < maxShow {
		maxShow = len(results)
	}
	for i := 0; i < maxShow; i++ {
		fmt.Fprintf(&b, "%d. *%s*\n", i+1, results[i].Title)
	}

	fmt.Fprintf(&b, "\n⚠️ _Konten 18+_\n👉 *Ketik nomor (1 - %d) untuk memilih.*", maxShow)
	return b.String()
}

// formatHentaiEpisodeList menampilkan daftar episode hanya dari source
// "ID minioppai". Jumlah episode dihitung dari episode yang benar-benar
// berhasil ditemukan oleh scraper MiniOppai.
func formatHentaiEpisodeList(info *minioppai.SeriesInfo, episodes []minioppai.EpisodeLink) string {
	var b strings.Builder
	if info != nil {
		title := info.Title
		if title == "" {
			title = "Series"
		}
		fmt.Fprintf(&b, "📌 *%s*\n", title)
		if len(info.Genres) > 0 {
			fmt.Fprintf(&b, "Genre: %s\n", strings.Join(info.Genres, ", "))
		}
		if info.Synopsis != "" {
			synopsis := info.Synopsis
			if runes := []rune(synopsis); len(runes) > 400 {
				synopsis = string(runes[:400]) + "…"
			}
			fmt.Fprintf(&b, "\n📖 *Sinopsis:*\n%s\n", synopsis)
		}
	}

	fmt.Fprintf(&b, "\n📜 *Daftar Episode (%d):*\n\n", len(episodes))
	fmt.Fprintf(&b, "*ID minioppai:*\n")
	for i, ep := range episodes {
		label := ep.Title
		if label == "" {
			label = "Episode " + ep.Number
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, label)
	}

	fmt.Fprintf(&b, "\n⚠️ _Konten 18+_\n👉 *Ketik nomor episode (1 - %d) untuk memilih kualitas.*", len(episodes))
	return b.String()
}

func formatHentaiQualityChoices(title string, opts []minioppai.DownloadOption, evt *events.Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🎬 *%s*\n", title)
	fmt.Fprintf(&b, "Pilih Kualitas Stream:\n\n")

	for i, o := range opts {
		fmt.Fprintf(&b, "%d. *%s*\n", i+1, o.Quality)
	}

	fmt.Fprintf(&b, "\n⚠️ _Konten 18+_\n👉 *Ketik nomor kualitas (1 - %d).*", len(opts))
	return b.String()
}

// formatHentaiDownloadChoices menampilkan pilihan kualitas dari section download
// (1080p -> 720p -> 480p -> 360p) lengkap dengan provider tiap kualitas.
func formatHentaiDownloadChoices(title string, ep *minioppai.EpisodeDownload, evt *events.Message) string {
	isPremUser := premium.IsPremium(evt)
	var b strings.Builder
	fmt.Fprintf(&b, "🎬 *%s*\n", title)
	fmt.Fprintf(&b, "Pilih Kualitas Download:\n\n")

	for i, q := range ep.Qualities {
		badge := ""
		if !hentailimit.IsFreeQuality(q.Quality) {
			if isPremUser {
				badge = " ✨ [PREMIUM]"
			} else {
				badge = " 💎 [PREMIUM ONLY]"
			}
		}
		fmt.Fprintf(&b, "%d. *%s*%s\n", i+1, q.Quality, badge)
	}

	fmt.Fprintf(&b, "\n⚠️ _Konten 18+_\n👉 *Ketik nomor kualitas (1 - %d) untuk download.*", len(ep.Qualities))
	return b.String()
}

// formatHentaiFinalDownloads menampilkan daftar tautan provider yang sudah
// di-resolve untuk satu kualitas.
func formatHentaiFinalDownloads(title string, q minioppai.QualityGroup) string {
	var b strings.Builder
	fmt.Fprintf(&b, "✅ *Download %s*\n", title)
	fmt.Fprintf(&b, "Kualitas: *%s*\n\n", q.Quality)
	fmt.Fprintf(&b, "🚀 *Server / Provider Download:*\n")

	for _, link := range q.Links {
		dlURL := link.DirectURL
		if dlURL == "" {
			dlURL = link.URL
		}
		if dlURL == "" {
			continue
		}
		fmt.Fprintf(&b, " • *%-12s* : %s\n", link.Server, dlURL)
	}

	fmt.Fprintf(&b, "\n_Catatan: Jika ukuran file di bawah batas WhatsApp, file akan otomatis dikirimkan langsung._")
	return b.String()
}
