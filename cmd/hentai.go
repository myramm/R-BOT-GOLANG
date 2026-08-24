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
	"rbot/lib/watchhentai"
)

type hentaiSessionStep int

const (
	stepSelectHentaiSeries hentaiSessionStep = iota + 1
	stepSelectHentaiEpisode
)

type hentaiSession struct {
	Step      hentaiSessionStep
	Results   []watchhentai.SearchResult
	Series    *watchhentai.SearchResult
	Episodes  []watchhentai.EpisodeLink
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
		Description: "Cari & download video dari WatchHentai.net. Contoh: .hentai furachi | .hentai <link episode>",
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

// isHentaiSlug true bila input berbentuk slug episode (contoh: furachi-episode-1-id-01)
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

func hentaiHandler(ctx context.Context, c *command.Ctx) error {
	argStr := strings.TrimSpace(c.ArgStr())
	if argStr == "" {
		mp := config.MainPrefix()
		_, err := c.Reply(ctx, fmt.Sprintf("Masukkan judul atau link episode WatchHentai.net.\n\n*Contoh:*\n• `%shentai furachi` (Cari & pilih episode)\n• `%shentai <link episode watchhentai.net>`\n• `%shentai furachi-episode-1-id-01` (slug)\n\n⚠️ _Konten 18+. Disarankan di chat pribadi._", mp, mp, mp))
		return err
	}

	key := getSessionKey(c.Evt)

	// Case 1: Link / slug langsung -> langsung proses download
	if isWatchHentaiInput(argStr) {
		c.React(ctx, "⏳")
		ep, err := watchhentai.GetEpisode(ctx, argStr)
		if err != nil {
			c.React(ctx, "❌")
			_, e := c.Reply(ctx, fmt.Sprintf("Gagal mengambil detail episode: %s", err.Error()))
			return e
		}
		c.React(ctx, "✅")
		go sendHentaiVideo(c, ep)
		return nil
	}

	// Case 2: Pencarian standar (.hentai furachi)
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

	switch sess.Step {
	case stepSelectHentaiSeries:
		choiceNum, err := strconv.Atoi(strings.TrimSpace(text))
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
			// Halaman bukan series (kemungkinan langsung halaman video)
			clearHentaiSession(key)
			ep, err := watchhentai.GetEpisode(ctx, selected.URL)
			if err != nil {
				c.React(ctx, "❌")
				_, _ = c.Reply(ctx, fmt.Sprintf("Gagal memuat detail video: %s", err.Error()))
				return true
			}
			c.React(ctx, "✅")
			go sendHentaiVideo(c, ep)
			return true
		}

		sess.Step = stepSelectHentaiEpisode
		sess.Series = &selected
		sess.Episodes = episodes
		saveHentaiSession(key, sess)

		_, _ = c.Reply(ctx, formatHentaiEpisodeList(selected.Title, episodes))
		return true

	case stepSelectHentaiEpisode:
		if sess.Series == nil || len(sess.Episodes) == 0 {
			clearHentaiSession(key)
			return false
		}
		choiceNum, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil || choiceNum <= 0 {
			return false
		}
		if choiceNum > len(sess.Episodes) {
			_, _ = c.Reply(ctx, fmt.Sprintf("Nomor episode tidak valid. Pilih 1 - %d.", len(sess.Episodes)))
			return true
		}

		targetEp := sess.Episodes[choiceNum-1]
		clearHentaiSession(key)
		c.React(ctx, "⏳")

		ep, err := watchhentai.GetEpisode(ctx, targetEp.URL)
		if err != nil {
			c.React(ctx, "❌")
			_, _ = c.Reply(ctx, fmt.Sprintf("Gagal memuat detail episode: %s", err.Error()))
			return true
		}
		c.React(ctx, "✅")

		go sendHentaiVideo(c, ep)
		return true
	}

	return false
}

// sendHentaiVideo mengunduh MP4 ke file sementara lalu mengirimkannya sebagai
// video WhatsApp secara asynchronous. Jika gagal, kirim fallback link langsung.
func sendHentaiVideo(c *command.Ctx, ep *watchhentai.Episode) {
	bgCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	if ep.VideoURL == "" {
		_, _ = c.Reply(bgCtx, fmt.Sprintf("⚠️ Direct video tidak ditemukan untuk *%s*.\n\n🔗 Buka manual: %s", ep.Title, ep.URL))
		return
	}

	_, _ = c.Reply(bgCtx, fmt.Sprintf("⏳ *Memproses %s...*\nProses download berjalan di background agar bot tetap responsif menerima pesan lain.", ep.Title))

	tmpPath, err := downloadToTempFile(bgCtx, ep.VideoURL, 10*time.Minute, maxHentaiDocBytes)
	if err != nil {
		_, _ = c.Reply(bgCtx, fmt.Sprintf("❌ Gagal download video (%s).\n\n🚀 *Link langsung:*\n%s", err.Error(), ep.VideoURL))
		return
	}
	defer os.Remove(tmpPath)

	fileData, readErr := os.ReadFile(tmpPath)
	if readErr != nil || len(fileData) == 0 {
		_, _ = c.Reply(bgCtx, fmt.Sprintf("❌ Gagal membaca file video.\n\n🚀 *Link langsung:*\n%s", ep.VideoURL))
		return
	}

	fileName := safeHentaiFileName(ep.Title)
	caption := fmt.Sprintf("🔞 *%s*\nSumber: watchhentai.net", ep.Title)
	sendErr := c.SendMediaBytesWithMetadata(bgCtx, fileData, command.MediaVideo, caption, fileName, "video/mp4", nil)
	if sendErr != nil {
		_, _ = c.Reply(bgCtx, fmt.Sprintf("⚠️ Gagal mengirim file (%s).\n\n🚀 *Link langsung:*\n%s", sendErr.Error(), ep.VideoURL))
		return
	}
	c.React(bgCtx, "✅")
}

func safeHentaiFileName(title string) string {
	cleanTitle := regexp.MustCompile(`[^\w\s\.-]`).ReplaceAllString(title, "")
	cleanTitle = regexp.MustCompile(`\s+`).ReplaceAllString(cleanTitle, "_")
	return cleanTitle + ".mp4"
}

func formatHentaiChoices(query string, results []watchhentai.SearchResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🔎 *Hasil Pencarian WatchHentai: '%s'*\n\n", query)

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

func formatHentaiEpisodeList(seriesTitle string, episodes []watchhentai.EpisodeLink) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📌 *%s*\n", seriesTitle)
	fmt.Fprintf(&b, "📜 *Daftar Episode (%d):*\n\n", len(episodes))

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

	fmt.Fprintf(&b, "\n⚠️ _Konten 18+_\n👉 *Ketik nomor episode (1 - %d) untuk download.*", len(episodes))
	return b.String()
}
