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
	"rbot/lib/watchhentai"
)

type hentaiSessionStep int

const (
	stepSelectHentaiSeries hentaiSessionStep = iota + 1
	stepSelectHentaiEpisode
	stepSelectHentaiQuality
)

type hentaiSession struct {
	Step      hentaiSessionStep
	Results   []watchhentai.SearchResult
	Series    *watchhentai.SearchResult
	Info      *watchhentai.SeriesInfo
	Episodes  []watchhentai.EpisodeLink
	Title     string
	Options   []watchhentai.DownloadOption
	CreatedAt time.Time
}

var (
	hentaiMu       sync.Mutex
	hentaiSessions = make(map[string]*hentaiSession)
)

const maxHentaiDocBytes = 2 * 1024 * 1024 * 1024 // 2GB batas dokumen WhatsApp

var reHentaiSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)+$`)

func init() {
	command.Register(&command.Command{
		Name:        "hentai",
		Category:    "Downloader",
		Alias:       []string{"watchhentai"},
		Description: "Cari & download hentai dari WatchHentai.net per episode & kualitas. Contoh: .hentai kotowarenai | .hentai <link episode>",
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

// isHentaiSlug true bila input berbentuk slug episode (contoh: kotowarenai-haha-episode-1-id-01)
func isHentaiSlug(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.ContainsAny(s, " /?#") {
		return false
	}
	if !strings.Contains(s, "-episode-") && !strings.HasSuffix(s, "-id-01") {
		return false
	}
	return reHentaiSlug.MatchString(s)
}

func isWatchHentaiInput(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.Contains(s, "watchhentai.net/") || isHentaiSlug(s)
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
// kualitas >= 1080p tetap premium-only, sisanya lewat kuota harian free.
// Return (boleh, pesanPenolakan); pesan kosong bila boleh.
func hentaiIzin(userKey, quality string, dalamGrup, isPrem bool) (bool, string) {
	if isPrem {
		return true, ""
	}
	if hentailimit.Tier(quality) == hentailimit.TierHigh {
		mp := config.MainPrefix()
		return false, fmt.Sprintf("💎 *Kualitas %s Khusus User Premium!*\n\nUser Free hanya dapat mengunduh kualitas *di bawah 1080p*.\n\nUpgrade ke Premium untuk membuka semua kualitas:\nKetik *%spremium*", quality, mp)
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
		hentailimit.Record(userKey, "MP4", dalamGrup)
	}
	return ""
}

func hentaiHandler(ctx context.Context, c *command.Ctx) error {
	argStr := strings.TrimSpace(c.ArgStr())
	if argStr == "" {
		mp := config.MainPrefix()
		_, err := c.Reply(ctx, fmt.Sprintf("Masukkan judul atau link episode WatchHentai.net.\n\n*Contoh:*\n• `%shentai kotowarenai` (Cari → pilih series → episode → kualitas)\n• `%shentai <link episode watchhentai.net>`\n• `%shentai kotowarenai-haha-episode-1-id-01` (slug)\n\n⚠️ _Konten 18+. Disarankan di chat pribadi._", mp, mp, mp))
		return err
	}

	key := getSessionKey(c.Evt)

	// Case 1: Link / slug langsung -> langsung ke pilihan kualitas (mirip .anime <link>)
	if isWatchHentaiInput(argStr) {
		c.React(ctx, "⏳")
		opts, err := watchhentai.GetDownloadOptions(ctx, argStr)
		if err != nil {
			// Fallback: halaman video tanpa halaman download -> ambil direct MP4 dari player
			ep, epErr := watchhentai.GetEpisode(ctx, argStr)
			if epErr != nil || ep.VideoURL == "" {
				c.React(ctx, "❌")
				msg := fmt.Sprintf("Gagal mengambil pilihan download: %s", err.Error())
				if epErr != nil {
					msg = fmt.Sprintf("Gagal mengambil detail episode: %s", epErr.Error())
				}
				_, e := c.Reply(ctx, msg)
				return e
			}
			if tolak := hentaiIzinFallback(c.Evt); tolak != "" {
				c.React(ctx, "🚫")
				_, e := c.Reply(ctx, tolak)
				return e
			}
			c.React(ctx, "✅")
			go sendHentaiMedia(c, ep.Title, ep.VideoURL, "MP4")
			return nil
		}
		c.React(ctx, "✅")

		title := watchhentai.TitleFromEpisodeURL(argStr)
		saveHentaiSession(key, &hentaiSession{
			Step:    stepSelectHentaiQuality,
			Title:   title,
			Options: opts,
		})
		_, err = c.Reply(ctx, formatHentaiQualityChoices(title, opts, c.Evt))
		return err
	}

	// Case 2: Pencarian standar (.hentai kotowarenai)
	c.React(ctx, "⏳")
	results, err := watchhentai.Search(ctx, argStr)
	if err != nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, fmt.Sprintf("Gagal mencari di WatchHentai: %s", err.Error()))
		return e
	}
	if len(results) == 0 {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, fmt.Sprintf("'%s' tidak ditemukan di WatchHentai.net.", argStr))
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

		episodes, err := watchhentai.GetEpisodeList(ctx, selected.URL)
		if err != nil {
			c.React(ctx, "❌")
			clearHentaiSession(key)
			_, _ = c.Reply(ctx, fmt.Sprintf("Gagal memuat daftar episode: %s", err.Error()))
			return true
		}
		c.React(ctx, "✅")

		if len(episodes) == 0 {
			// Halaman bukan series (kemungkinan langsung halaman video) -> ke pilihan kualitas
			opts, optErr := watchhentai.GetDownloadOptions(ctx, selected.URL)
			if optErr != nil {
				clearHentaiSession(key)
				ep, epErr := watchhentai.GetEpisode(ctx, selected.URL)
				if epErr != nil || ep.VideoURL == "" {
					c.React(ctx, "❌")
					_, _ = c.Reply(ctx, "Tidak ada episode maupun pilihan download di halaman ini.")
					return true
				}
				if tolak := hentaiIzinFallback(evt); tolak != "" {
					c.React(ctx, "🚫")
					_, _ = c.Reply(ctx, tolak)
					return true
				}
				c.React(ctx, "✅")
				go sendHentaiMedia(c, ep.Title, ep.VideoURL, "MP4")
				return true
			}

			sess.Step = stepSelectHentaiQuality
			sess.Series = &selected
			sess.Title = watchhentai.TitleFromEpisodeURL(selected.URL)
			sess.Options = opts
			saveHentaiSession(key, sess)

			_, _ = c.Reply(ctx, formatHentaiQualityChoices(sess.Title, opts, evt))
			return true
		}

		sess.Step = stepSelectHentaiEpisode
		sess.Series = &selected
		sess.Episodes = episodes
		saveHentaiSession(key, sess)

		// Ambil sinopsis & genre series (best-effort, gagal = tampil tanpa sinopsis)
		if info, infoErr := watchhentai.GetSeriesInfo(ctx, selected.URL); infoErr == nil {
			info.URL = selected.URL
			sess.Info = info
			saveHentaiSession(key, sess)

			// Kirim thumbnail sebagai foto asli (bukan URL) sebelum daftar episode
			if info.Thumbnail != "" {
				_ = c.SendMedia(ctx, info.Thumbnail, command.MediaImage, "", "", "", 10*1024*1024)
			}
		}

		_, _ = c.Reply(ctx, formatHentaiEpisodeList(sess.Info, episodes))
		return true

	case stepSelectHentaiEpisode:
		if sess.Series == nil || len(sess.Episodes) == 0 {
			clearHentaiSession(key)
			return false
		}
		choiceNum, err := strconv.Atoi(cleanInput)
		if err != nil || choiceNum <= 0 || choiceNum > len(sess.Episodes) {
			return false
		}

		targetEp := sess.Episodes[choiceNum-1]
		c.React(ctx, "⏳")

		opts, err := watchhentai.GetDownloadOptions(ctx, targetEp.URL)
		if err != nil {
			// Fallback: langsung ambil direct MP4 dari player
			ep, epErr := watchhentai.GetEpisode(ctx, targetEp.URL)
			if epErr != nil || ep.VideoURL == "" {
				c.React(ctx, "❌")
				_, _ = c.Reply(ctx, fmt.Sprintf("Gagal memuat pilihan download: %s", err.Error()))
				clearHentaiSession(key)
				return true
			}
			if tolak := hentaiIzinFallback(evt); tolak != "" {
				c.React(ctx, "🚫")
				_, _ = c.Reply(ctx, tolak)
				clearHentaiSession(key)
				return true
			}
			c.React(ctx, "✅")
			go sendHentaiMedia(c, ep.Title, ep.VideoURL, "MP4")
			clearHentaiSession(key)
			return true
		}
		c.React(ctx, "✅")

		title := watchhentai.TitleFromEpisodeURL(targetEp.URL)
		sess.Step = stepSelectHentaiQuality
		sess.Title = title
		sess.Options = opts
		saveHentaiSession(key, sess)

		_, _ = c.Reply(ctx, formatHentaiQualityChoices(title, opts, evt))
		return true

	case stepSelectHentaiQuality:
		choiceNum, err := strconv.Atoi(cleanInput)
		if err != nil || choiceNum <= 0 {
			return false
		}
		if sess.Options == nil || len(sess.Options) == 0 {
			clearHentaiSession(key)
			return false
		}
		if choiceNum > len(sess.Options) {
			_, _ = c.Reply(ctx, fmt.Sprintf("Nomor kualitas tidak valid. Pilih 1 - %d.", len(sess.Options)))
			return true
		}

		selectedOpt := sess.Options[choiceNum-1]

		dalamGrup := isGrupOfficialChat(evt)
		isPrem := premium.IsPremium(evt)
		userKey := identity.SenderPhone(evt)

		// Izin download: premium bebas; >=1080p premium-only; free pakai kuota harian.
		if ok, tolak := hentaiIzin(userKey, selectedOpt.Quality, dalamGrup, isPrem); !ok {
			c.React(ctx, "🚫")
			_, _ = c.Reply(ctx, tolak)
			return true
		}

		c.React(ctx, "⏳")
		clearHentaiSession(key)

		if !isPrem {
			hentailimit.Record(userKey, selectedOpt.Quality, dalamGrup)
		}

		_, _ = c.Reply(ctx, fmt.Sprintf("⏳ *Memproses %s (%s)...*\nProses download berjalan di background agar bot tetap responsif menerima pesan lain.", sess.Title, selectedOpt.Quality))

		go sendHentaiMedia(c, sess.Title, selectedOpt.URL, selectedOpt.Quality)
		return true
	}

	return false
}

// sendHentaiMedia mengunduh MP4 ke file sementara lalu mengirimkannya sebagai
// video WhatsApp secara asynchronous. Jika gagal, kirim fallback link langsung.
func sendHentaiMedia(c *command.Ctx, title, videoURL, quality string) {
	bgCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	_, _ = c.Reply(bgCtx, fmt.Sprintf("⏳ *Memproses %s (%s)...*\nProses download berjalan di background.", title, quality))

	tmpPath, err := downloadToTempFile(bgCtx, videoURL, 10*time.Minute, maxHentaiDocBytes)
	if err != nil {
		_, _ = c.Reply(bgCtx, fmt.Sprintf("❌ Gagal download video (%s).\n\n🚀 *Link langsung:*\n%s", err.Error(), videoURL))
		return
	}
	defer os.Remove(tmpPath)

	fileData, readErr := os.ReadFile(tmpPath)
	if readErr != nil || len(fileData) == 0 {
		_, _ = c.Reply(bgCtx, fmt.Sprintf("❌ Gagal membaca file video.\n\n🚀 *Link langsung:*\n%s", videoURL))
		return
	}

	fileName := safeHentaiFileName(title, quality)
	caption := fmt.Sprintf("🔞 *%s*\nKualitas: %s", title, quality)
	sendErr := c.SendMediaBytesWithMetadata(bgCtx, fileData, command.MediaVideo, caption, fileName, "video/mp4", nil)
	if sendErr != nil {
		_, _ = c.Reply(bgCtx, fmt.Sprintf("⚠️ Gagal mengirim file (%s).\n\n🚀 *Link langsung:*\n%s", sendErr.Error(), videoURL))
		return
	}
	c.React(bgCtx, "✅")
}

func safeHentaiFileName(title, quality string) string {
	cleanTitle := regexp.MustCompile(`[^\w\s\.-]`).ReplaceAllString(title, "")
	cleanTitle = regexp.MustCompile(`\s+`).ReplaceAllString(cleanTitle, "_")
	q := regexp.MustCompile(`[^\w]`).ReplaceAllString(quality, "")
	if q != "" {
		return fmt.Sprintf("%s_%s.mp4", cleanTitle, q)
	}
	return cleanTitle + ".mp4"
}

func formatHentaiChoices(query string, results []watchhentai.SearchResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🔎 *Hasil Pencarian: '%s'*\n\n", query)

	maxShow := 5
	if len(results) < maxShow {
		maxShow = len(results)
	}
	for i := 0; i < maxShow; i++ {
		fmt.Fprintf(&b, "%d. *%s*\n", i+1, results[i].Title)
	}

	fmt.Fprintf(&b, "\n⚠️ _Konten 18+_\n👉 *Ketik nomor (1 - %d) untuk memilih.*", maxShow)
	return b.String()
}

func formatHentaiEpisodeList(info *watchhentai.SeriesInfo, episodes []watchhentai.EpisodeLink) string {
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

	maxShow := 10
	if len(episodes) < maxShow {
		maxShow = len(episodes)
	}
	for i := 0; i < maxShow; i++ {
		ep := episodes[i]
		label := ep.Title
		if label == "" {
			label = "Episode " + ep.Number
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, label)
	}
	if len(episodes) > maxShow {
		fmt.Fprintf(&b, " • ... s/d Episode %s\n", episodes[len(episodes)-1].Number)
	}

	fmt.Fprintf(&b, "\n⚠️ _Konten 18+_\n👉 *Ketik nomor episode (1 - %d) untuk memilih kualitas.*", len(episodes))
	return b.String()
}

func formatHentaiQualityChoices(title string, opts []watchhentai.DownloadOption, evt *events.Message) string {
	isPremUser := premium.IsPremium(evt)
	var b strings.Builder
	fmt.Fprintf(&b, "🎬 *%s*\n", title)
	fmt.Fprintf(&b, "Pilih Kualitas Download:\n\n")

	for i, o := range opts {
		badge := ""
		if isPremiumQuality(o.Quality) {
			if isPremUser {
				badge = " ✨ [PREMIUM]"
			} else {
				badge = " 💎 [PREMIUM ONLY]"
			}
		}
		fmt.Fprintf(&b, "%d. *%s*%s\n", i+1, o.Quality, badge)
	}

	fmt.Fprintf(&b, "\n⚠️ _Konten 18+_\n👉 *Ketik nomor kualitas (1 - %d) untuk download.*", len(opts))
	return b.String()
}
