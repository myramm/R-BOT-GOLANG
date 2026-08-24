package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
	"rbot/brain/premium"
	"rbot/lib/samehadaku"
)

type animeSessionStep int

const (
	stepSelectAnime animeSessionStep = iota + 1
	stepSelectEpisode
	stepSelectQuality
)

type animeSession struct {
	Step          animeSessionStep
	SearchResults []samehadaku.AnimeSearchResult
	SelectedAnime *samehadaku.AnimeDetail
	SelectedEp    *samehadaku.EpisodeDownload
	CreatedAt     time.Time
}

var (
	sessMu        sync.Mutex
	animeSessions = make(map[string]*animeSession)
	reEpURL       = regexp.MustCompile(`(?i)https?://[^\s]+/.*-episode-\d+/?`)
	reBatchURL    = regexp.MustCompile(`(?i)https?://[^\s]+/batch/.*-batch/?`)
)

const maxAnimeDocBytes = 2 * 1024 * 1024 * 1024 // 2GB batas dokumen WhatsApp

func init() {
	command.Register(&command.Command{
		Name:        "anime",
		Category:    "Downloader",
		Alias:       []string{"samehadaku", "animedl"},
		Description: "Cari dan download anime per episode / BATCH dari Samehadaku. Contoh: .anime bleach | .anime bleach 4 | .anime bleach batch | .anime <link episode/batch>",
		Handler:     animeHandler,
	})

	// Register interaktif ResumeHook untuk menangkap nomor pilihan (1, 2, dst)
	prevHook := command.ResumeHook
	command.ResumeHook = func(ctx context.Context, client *whatsmeow.Client, evt *events.Message, text string) {
		if handleAnimeSessionReply(ctx, client, evt, text) {
			return
		}
		if prevHook != nil {
			prevHook(ctx, client, evt, text)
		}
	}
}

func getSessionKey(evt *events.Message) string {
	return evt.Info.Chat.String() + ":" + evt.Info.Sender.String()
}

func saveSession(key string, sess *animeSession) {
	sessMu.Lock()
	defer sessMu.Unlock()
	sess.CreatedAt = time.Now()
	animeSessions[key] = sess
}

func getSession(key string) *animeSession {
	sessMu.Lock()
	defer sessMu.Unlock()
	sess, ok := animeSessions[key]
	if !ok {
		return nil
	}
	if time.Since(sess.CreatedAt) > 5*time.Minute {
		delete(animeSessions, key)
		return nil
	}
	return sess
}

func clearSession(key string) {
	sessMu.Lock()
	defer sessMu.Unlock()
	delete(animeSessions, key)
}

func isPremiumQuality(quality string) bool {
	q := strings.ToLower(quality)
	return strings.Contains(q, "1080") || strings.Contains(q, "fullhd") || strings.Contains(q, "x265") || strings.Contains(q, "2k") || strings.Contains(q, "4k")
}

func isQualityBroken(q samehadaku.QualityGroup) bool {
	if len(q.Links) == 0 {
		return true
	}
	validCount := 0
	for _, l := range q.Links {
		u := strings.TrimSpace(l.URL)
		if u != "" && u != "#" && !strings.HasPrefix(u, "javascript:") {
			validCount++
		}
	}
	return validCount == 0
}

func isBatchInput(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "batch" || s == "b" || s == "0"
}

func animeHandler(ctx context.Context, c *command.Ctx) error {
	argStr := strings.TrimSpace(c.ArgStr())
	if argStr == "" {
		_, err := c.Reply(ctx, "Masukkan judul anime atau link episode Samehadaku.\n\n*Contoh:*\n• `.anime bleach` (Cari & pilih episode)\n• `.anime bleach 4` (Langsung ke episode 4)\n• `.anime bleach batch` (Download Batch 1 Season)\n• `.anime <link episode/batch>`")
		return err
	}

	key := getSessionKey(c.Evt)

	// Case 1: Direct link episode / batch Samehadaku
	if reEpURL.MatchString(argStr) || reBatchURL.MatchString(argStr) {
		c.React(ctx, "⏳")
		epData, err := samehadaku.GetEpisodeDownloads(ctx, argStr)
		if err != nil {
			c.React(ctx, "❌")
			_, e := c.Reply(ctx, fmt.Sprintf("Gagal mengambil link download: %s", err.Error()))
			return e
		}
		c.React(ctx, "✅")
		saveSession(key, &animeSession{
			Step:       stepSelectQuality,
			SelectedEp: epData,
		})
		_, err = c.Reply(ctx, formatQualityChoices(epData, c.Evt))
		return err
	}

	// Case 2: Judul + "batch" (contoh: .anime bleach batch)
	args := strings.Fields(argStr)
	if len(args) >= 2 && isBatchInput(args[len(args)-1]) {
		query := strings.Join(args[:len(args)-1], " ")
		c.React(ctx, "⏳")

		results, err := samehadaku.Search(ctx, query)
		if err != nil || len(results) == 0 {
			c.React(ctx, "❌")
			_, e := c.Reply(ctx, fmt.Sprintf("Anime '%s' tidak ditemukan.", query))
			return e
		}

		detail, err := samehadaku.GetDetail(ctx, results[0].Link)
		if err != nil {
			c.React(ctx, "❌")
			_, e := c.Reply(ctx, "Gagal mengambil detail anime.")
			return e
		}

		if !detail.HasBatch || detail.BatchURL == "" {
			c.React(ctx, "❌")
			_, e := c.Reply(ctx, fmt.Sprintf("Maaf, Batch Download belum tersedia untuk '%s'. Silakan download per episode.", detail.Title))
			return e
		}

		epData, err := samehadaku.GetEpisodeDownloads(ctx, detail.BatchURL)
		if err != nil {
			c.React(ctx, "❌")
			_, e := c.Reply(ctx, fmt.Sprintf("Gagal memuat Batch download: %s", err.Error()))
			return e
		}

		c.React(ctx, "✅")
		saveSession(key, &animeSession{
			Step:          stepSelectQuality,
			SelectedAnime: detail,
			SelectedEp:    epData,
		})
		_, err = c.Reply(ctx, formatQualityChoices(epData, c.Evt))
		return err
	}

	// Case 3: Judul + Nomor Episode (contoh: .anime bleach 4)
	if len(args) >= 2 {
		lastArg := args[len(args)-1]
		if epNum, err := strconv.Atoi(lastArg); err == nil && epNum > 0 {
			query := strings.Join(args[:len(args)-1], " ")
			c.React(ctx, "⏳")

			results, err := samehadaku.Search(ctx, query)
			if err != nil || len(results) == 0 {
				c.React(ctx, "❌")
				_, e := c.Reply(ctx, fmt.Sprintf("Anime '%s' tidak ditemukan.", query))
				return e
			}

			// Detail top anime
			detail, err := samehadaku.GetDetail(ctx, results[0].Link)
			if err != nil || len(detail.Episodes) == 0 {
				c.React(ctx, "❌")
				_, e := c.Reply(ctx, "Gagal mengambil daftar episode.")
				return e
			}

			// Cari episode yang sesuai
			var targetEp *samehadaku.EpisodeInfo
			epNumStr := fmt.Sprintf("%d", epNum)
			for _, ep := range detail.Episodes {
				if ep.Number == epNumStr || strings.Contains(ep.Title, fmt.Sprintf("Episode %d", epNum)) {
					targetEp = &ep
					break
				}
			}
			if targetEp == nil && epNum <= len(detail.Episodes) {
				targetEp = &detail.Episodes[epNum-1]
			}

			if targetEp == nil {
				c.React(ctx, "❌")
				_, e := c.Reply(ctx, fmt.Sprintf("Episode %d tidak tersedia. Total episode: %d", epNum, len(detail.Episodes)))
				return e
			}

			epData, err := samehadaku.GetEpisodeDownloads(ctx, targetEp.URL)
			if err != nil {
				c.React(ctx, "❌")
				_, e := c.Reply(ctx, fmt.Sprintf("Gagal memuat link download episode: %s", err.Error()))
				return e
			}

			c.React(ctx, "✅")
			saveSession(key, &animeSession{
				Step:          stepSelectQuality,
				SelectedAnime: detail,
				SelectedEp:    epData,
			})
			_, err = c.Reply(ctx, formatQualityChoices(epData, c.Evt))
			return err
		}
	}

	// Case 4: Standard Search (.anime bleach)
	c.React(ctx, "⏳")
	results, err := samehadaku.Search(ctx, argStr)
	if err != nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, fmt.Sprintf("Gagal mencari anime: %s", err.Error()))
		return e
	}
	if len(results) == 0 {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, fmt.Sprintf("Anime '%s' tidak ditemukan di Samehadaku.", argStr))
		return e
	}

	c.React(ctx, "✅")
	saveSession(key, &animeSession{
		Step:          stepSelectAnime,
		SearchResults: results,
	})

	_, err = c.Reply(ctx, formatSearchChoice(argStr, results))
	return err
}

func handleAnimeSessionReply(ctx context.Context, client *whatsmeow.Client, evt *events.Message, text string) bool {
	key := evt.Info.Chat.String() + ":" + evt.Info.Sender.String()
	sess := getSession(key)
	if sess == nil {
		return false
	}

	c := &command.Ctx{Client: client, Evt: evt, Text: text}
	cleanInput := strings.TrimSpace(text)

	switch sess.Step {
	case stepSelectAnime:
		choiceNum, err := strconv.Atoi(cleanInput)
		if err != nil || choiceNum <= 0 || choiceNum > len(sess.SearchResults) {
			return false
		}

		selected := sess.SearchResults[choiceNum-1]
		c.React(ctx, "⏳")

		detail, err := samehadaku.GetDetail(ctx, selected.Link)
		if err != nil {
			c.React(ctx, "❌")
			_, _ = c.Reply(ctx, fmt.Sprintf("Gagal memuat detail anime: %s", err.Error()))
			clearSession(key)
			return true
		}
		c.React(ctx, "✅")

		sess.Step = stepSelectEpisode
		sess.SelectedAnime = detail
		saveSession(key, sess)

		_, _ = c.Reply(ctx, formatAnimeDetailChoice(detail))
		return true

	case stepSelectEpisode:
		if sess.SelectedAnime == nil {
			clearSession(key)
			return false
		}
		detail := sess.SelectedAnime

		// Cek jika user mengetik "batch" / "b" / "0"
		if isBatchInput(cleanInput) {
			if !detail.HasBatch || detail.BatchURL == "" {
				_, _ = c.Reply(ctx, fmt.Sprintf("Maaf, Batch Download belum tersedia untuk '%s'. Silakan pilih nomor episode.", detail.Title))
				return true
			}

			c.React(ctx, "⏳")
			batchData, err := samehadaku.GetEpisodeDownloads(ctx, detail.BatchURL)
			if err != nil {
				c.React(ctx, "❌")
				_, _ = c.Reply(ctx, fmt.Sprintf("Gagal memuat Batch download: %s", err.Error()))
				clearSession(key)
				return true
			}
			c.React(ctx, "✅")

			sess.Step = stepSelectQuality
			sess.SelectedEp = batchData
			saveSession(key, sess)

			_, _ = c.Reply(ctx, formatQualityChoices(batchData, evt))
			return true
		}

		choiceNum, err := strconv.Atoi(cleanInput)
		if err != nil || choiceNum <= 0 {
			return false
		}

		if len(detail.Episodes) == 0 {
			clearSession(key)
			return false
		}

		var targetEp *samehadaku.EpisodeInfo
		epNumStr := fmt.Sprintf("%d", choiceNum)
		for _, ep := range detail.Episodes {
			if ep.Number == epNumStr {
				targetEp = &ep
				break
			}
		}
		if targetEp == nil && choiceNum <= len(detail.Episodes) {
			targetEp = &detail.Episodes[choiceNum-1]
		}

		if targetEp == nil {
			_, _ = c.Reply(ctx, fmt.Sprintf("Episode %d tidak ditemukan. Pilih episode 1 - %d atau ketik 'batch'.", choiceNum, len(detail.Episodes)))
			return true
		}

		c.React(ctx, "⏳")
		epData, err := samehadaku.GetEpisodeDownloads(ctx, targetEp.URL)
		if err != nil {
			c.React(ctx, "❌")
			_, _ = c.Reply(ctx, fmt.Sprintf("Gagal memuat download episode: %s", err.Error()))
			clearSession(key)
			return true
		}
		c.React(ctx, "✅")

		sess.Step = stepSelectQuality
		sess.SelectedEp = epData
		saveSession(key, sess)

		_, _ = c.Reply(ctx, formatQualityChoices(epData, evt))
		return true

	case stepSelectQuality:
		choiceNum, err := strconv.Atoi(cleanInput)
		if err != nil || choiceNum <= 0 {
			return false
		}

		if sess.SelectedEp == nil || len(sess.SelectedEp.Qualities) == 0 {
			clearSession(key)
			return false
		}
		epData := sess.SelectedEp
		if choiceNum > len(epData.Qualities) {
			_, _ = c.Reply(ctx, fmt.Sprintf("Nomor kualitas tidak valid. Pilih 1 - %d.", len(epData.Qualities)))
			return true
		}

		selectedQual := epData.Qualities[choiceNum-1]

		// Cek jika kualitas ini rusak / tidak memiliki link aktif
		if isQualityBroken(selectedQual) {
			c.React(ctx, "❌")
			_, _ = c.Reply(ctx, fmt.Sprintf("⚠️ *Kualitas %s Tidak Tersedia (Link Rusak)!*\n\nSeluruh provider download untuk kualitas ini sedang tidak aktif atau rusak di Samehadaku. Silakan pilih nomor kualitas yang lain.", selectedQual.Quality))
			return true
		}

		// Batasan Premium: 1080p, FULLHD, x265 hanya untuk Premium Only
		if isPremiumQuality(selectedQual.Quality) && !premium.IsPremium(evt) {
			mp := config.MainPrefix()
			c.React(ctx, "💎")
			_, _ = c.Reply(ctx, fmt.Sprintf("💎 *Kualitas %s Khusus User Premium!*\n\nUser Free hanya dapat mengunduh kualitas *360p, 480p, dan 720p*.\n\nUpgrade ke Premium untuk membuka semua kualitas (1080p, FULLHD, x265):\nKetik *%spremium*", selectedQual.Quality, mp))
			return true
		}

		c.React(ctx, "⏳")
		clearSession(key)

		_, _ = c.Reply(ctx, fmt.Sprintf("⏳ *Memproses %s (%s)...*\nProses download berjalan di background agar bot tetap responsif menerima pesan lain.", epData.Title, selectedQual.Quality))

		// Jalankan pengunduhan & pengiriman secara asynchronous (goroutine)
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()

			resolvedLinks := make([]samehadaku.DownloadLink, len(selectedQual.Links))
			var fileSent bool

			for i, l := range selectedQual.Links {
				directURL := samehadaku.ResolveDirectLink(bgCtx, l.Server, l.URL)
				resolvedLinks[i] = samehadaku.DownloadLink{
					Server:    l.Server,
					URL:       l.URL,
					DirectURL: directURL,
				}

				// Coba kirim file langsung ke WhatsApp jika ukurannya <= 2GB via disk stream
				if !fileSent && directURL != l.URL {
					tmpPath, downloadErr := downloadToTempFile(bgCtx, directURL, 10*time.Minute, maxAnimeDocBytes)
					if downloadErr == nil && tmpPath != "" {
						defer os.Remove(tmpPath)
						fileData, readErr := os.ReadFile(tmpPath)
						if readErr == nil && len(fileData) > 0 {
						fileName := safeAnimeFileName(epData.Title, selectedQual.Quality, selectedQual.Format)
						caption := fmt.Sprintf("🎥 *%s*\nKualitas: %s (%s)", epData.Title, selectedQual.Quality, selectedQual.Format)
						mimeType := "video/x-matroska"

						// Deteksi arsip RAR batch (dari nama server atau slug URL provider)
						linkHint := strings.ToLower(l.Server + " " + l.URL + " " + directURL)
						isRar := strings.Contains(linkHint, ".rar") || strings.Contains(linkHint, "-rar")

						if strings.ToUpper(selectedQual.Format) == "MP4" {
							mimeType = "video/mp4"
						}
						if isRar {
							mimeType = "application/x-rar-compressed"
							fileName = regexp.MustCompile(`\.(mkv|mp4)$`).ReplaceAllString(fileName, ".rar")
							caption = fmt.Sprintf("📦 *%s*\nKualitas: %s (%s) [Batch RAR]", epData.Title, selectedQual.Quality, selectedQual.Format)
						}

							sendErr := c.SendMediaBytesWithMetadata(bgCtx, fileData, command.MediaDocument, caption, fileName, mimeType, nil)
							if sendErr == nil {
								fileSent = true
								c.React(bgCtx, "✅")
							}
						}
					}
				}
			}

			if !fileSent {
				c.React(bgCtx, "✅")
				updatedQual := selectedQual
				updatedQual.Links = resolvedLinks
				_, _ = c.Reply(bgCtx, formatFinalDownloads(epData.Title, updatedQual))
			}
		}()

		return true
	}

	return false
}

func downloadToTempFile(ctx context.Context, targetURL string, timeout time.Duration, maxBytes int64) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(cctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", samehadaku.UserAgent)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "rbot-anime-*")
	if err != nil {
		return "", err
	}
	tmpName := tmpFile.Name()

	var r io.Reader = resp.Body
	if maxBytes > 0 {
		r = io.LimitReader(resp.Body, maxBytes)
	}

	_, copyErr := io.Copy(tmpFile, r)
	_ = tmpFile.Close()
	if copyErr != nil {
		_ = os.Remove(tmpName)
		return "", copyErr
	}

	return tmpName, nil
}

func safeAnimeFileName(title, quality, format string) string {
	cleanTitle := regexp.MustCompile(`[^\w\s\.-]`).ReplaceAllString(title, "")
	cleanTitle = regexp.MustCompile(`\s+`).ReplaceAllString(cleanTitle, "_")
	ext := "mkv"
	if strings.ToUpper(format) == "MP4" {
		ext = "mp4"
	}
	return fmt.Sprintf("%s_%s.%s", cleanTitle, quality, ext)
}

func formatSearchChoice(query string, results []samehadaku.AnimeSearchResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🔎 *Hasil Pencarian Anime Samehadaku: '%s'*\n\n", query)

	maxShow := 5
	if len(results) < maxShow {
		maxShow = len(results)
	}

	for i := 0; i < maxShow; i++ {
		r := results[i]
		info := ""
		if r.Score != "" {
			info = fmt.Sprintf(" ⭐ %s", r.Score)
		}
		if r.Type != "" {
			info += fmt.Sprintf(" [%s]", r.Type)
		}
		fmt.Fprintf(&b, "%d. *%s*%s\n", i+1, r.Title, info)
	}

	fmt.Fprintf(&b, "\n👉 *Ketik nomor anime (1 - %d) untuk memilih.*", maxShow)
	return b.String()
}

func formatAnimeDetailChoice(detail *samehadaku.AnimeDetail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📌 *%s*\n", detail.Title)
	if detail.Status != "" {
		fmt.Fprintf(&b, "Status: %s\n", detail.Status)
	}
	if detail.Rating != "" {
		fmt.Fprintf(&b, "Rating: %s\n", detail.Rating)
	}
	if len(detail.Genres) > 0 {
		fmt.Fprintf(&b, "Genre: %s\n", strings.Join(detail.Genres, ", "))
	}
	if detail.Synopsis != "" {
		fmt.Fprintf(&b, "\n📖 *Sinopsis:*\n%s\n", detail.Synopsis)
	}

	fmt.Fprintf(&b, "\n📜 *Daftar Episode (%d Episode):*\n", detail.TotalEp)
	maxShow := 10
	if detail.TotalEp < maxShow {
		maxShow = detail.TotalEp
	}
	for i := 0; i < maxShow; i++ {
		ep := detail.Episodes[i]
		fmt.Fprintf(&b, " • Ep %s: %s\n", ep.Number, ep.Title)
	}
	if detail.TotalEp > maxShow {
		fmt.Fprintf(&b, " • ... s/d Episode %s\n", detail.Episodes[detail.TotalEp-1].Number)
	}

	if detail.HasBatch {
		fmt.Fprintf(&b, "\n📦 *Batch Download (1 Season Penuh):* Tersedia!\n👉 *Ketik 'batch' (atau 'b') untuk download 1 Season sekaligus.*")
	}

	fmt.Fprintf(&b, "\n\n👉 *Ketik nomor episode (1 - %d) untuk memilih episode.*", detail.TotalEp)
	return b.String()
}

func formatQualityChoices(ep *samehadaku.EpisodeDownload, evt *events.Message) string {
	isPremUser := premium.IsPremium(evt)
	var b strings.Builder
	fmt.Fprintf(&b, "🎬 *%s*\n", ep.Title)
	fmt.Fprintf(&b, "Pilih Kualitas & Format Download:\n\n")

	currentFormat := ""
	for i, q := range ep.Qualities {
		fmtGroup := strings.ToUpper(strings.TrimSpace(q.Format))
		if fmtGroup == "" {
			fmtGroup = "MKV"
		}

		if fmtGroup != currentFormat {
			if currentFormat != "" {
				b.WriteString("\n")
			}
			currentFormat = fmtGroup
			icon := "🎞️"
			if currentFormat == "MP4" {
				icon = "📱"
			} else if strings.Contains(strings.ToLower(currentFormat), "265") {
				icon = "⚡"
			}
			fmt.Fprintf(&b, "%s *%s:*\n", icon, currentFormat)
		}

		badge := ""
		if isQualityBroken(q) {
			badge = " ⚠️ (Link Rusak)"
		} else if isPremiumQuality(q.Quality) {
			if isPremUser {
				badge = " ✨ [PREMIUM]"
			} else {
				badge = " 💎 [PREMIUM ONLY]"
			}
		}

		fmt.Fprintf(&b, "%d. *%s*%s\n", i+1, q.Quality, badge)
	}

	fmt.Fprintf(&b, "\n👉 *Ketik nomor kualitas (1 - %d) untuk memilih.*", len(ep.Qualities))
	return b.String()
}

func formatFinalDownloads(epTitle string, q samehadaku.QualityGroup) string {
	var b strings.Builder
	fmt.Fprintf(&b, "✅ *Download %s*\n", epTitle)
	fmt.Fprintf(&b, "Kualitas: *%s* (%s)\n\n", q.Quality, q.Format)
	fmt.Fprintf(&b, "🚀 *Server / Provider Download:*\n")

	for _, link := range q.Links {
		dlURL := link.DirectURL
		if dlURL == "" {
			dlURL = link.URL
		}
		fmt.Fprintf(&b, " • *%-12s* : %s\n", link.Server, dlURL)
	}

	fmt.Fprintf(&b, "\n_Catatan: Jika ukuran file di bawah 2GB, file akan otomatis dikirimkan langsung ke WhatsApp._")
	return b.String()
}
