package cmd

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/lib/httpx"
	"rbot/lib/kamino"
)

const (
	maxUploadBytes    = 64 * 1024 * 1024 // batas kirim WhatsApp (port MAX_UPLOAD_BYTES)
	audioDocThreshold = 32 * 1024 * 1024 // default; audio > ini dikirim sebagai document
)

var (
	reHTTP          = regexp.MustCompile(`(?i)^https?://`)
	reURL           = regexp.MustCompile(`(?i)https?://[^\s<>"'\(\)]+`)
	reTrailingPunct = regexp.MustCompile(`[.,!?;:]+$`)
	reFormatOption  = regexp.MustCompile(`(?i)^(mp3|audio|sound|music|hd|sd|video)$`)
	reFileUnsafe    = regexp.MustCompile(`[\\/:*?"<>|]+`)
	reSpaces        = regexp.MustCompile(`\s+`)
)

func init() {
	command.Register(&command.Command{
		Name:        "download",
		Category:    "Downloader",
		Alias:       []string{"dl"},
		Description: "Download video/foto/lagu dari TikTok, Instagram, YouTube, Spotify, Facebook, X, Threads, Pinterest, Doodstream. Contoh: .download <link> • .download <link> mp3 • atau Reply pesan berisi link lalu ketik .dl",
		Handler:     downloadHandler,
	})
}

// audioDocThresholdBytes: ambang audio→document dari config (audioDocMB), fallback 32MB.
func audioDocThresholdBytes() int64 {
	if mb := config.C.Kamino.AudioDocMB; mb > 0 {
		return int64(mb) * 1024 * 1024
	}
	return audioDocThreshold
}

func extractURLAndArg(c *command.Ctx) (string, string) {
	var targetURL string
	var extraArg string

	// 1. Cari URL di c.Args lebih dulu
	for _, arg := range c.Args {
		if loc := reURL.FindString(arg); loc != "" {
			targetURL = loc
			break
		}
	}

	// 2. Jika tak ada di c.Args, cari di c.Text secara penuh
	if targetURL == "" && c.Text != "" {
		if loc := reURL.FindString(c.Text); loc != "" {
			targetURL = loc
		}
	}

	// 3. Jika belum ada, cari di Quoted Message (Reply link)
	if targetURL == "" {
		if ci := c.ContextInfo(); ci != nil {
			if quoted := ci.GetQuotedMessage(); quoted != nil {
				quotedText := command.ExtractText(quoted)
				if loc := reURL.FindString(quotedText); loc != "" {
					targetURL = loc
				}
			}
		}
	}

	if targetURL != "" {
		targetURL = reTrailingPunct.ReplaceAllString(targetURL, "")
	}

	// 4. Cari opsi format (seperti "mp3" atau argumen kedua) di c.Args
	for _, arg := range c.Args {
		cleanArg := strings.TrimSpace(arg)
		if cleanArg == "" || strings.Contains(targetURL, cleanArg) {
			continue
		}
		if reFormatOption.MatchString(cleanArg) {
			extraArg = cleanArg
			break
		}
		if extraArg == "" {
			extraArg = cleanArg
		}
	}

	return targetURL, extraArg
}

func downloadHandler(ctx context.Context, c *command.Ctx) error {
	url, arg := extractURLAndArg(c)
	if url == "" {
		_, err := c.Reply(ctx, "Kasih atau reply link yang mau didownload.\n\nContoh:\n• .dl https://vt.tiktok.com/xxxx\n• Reply pesan berisi link lalu ketik .dl\n• YouTube ke MP3: .dl <link youtube> mp3 (atau reply link dengan .dl mp3)")
		return err
	}
	platform := kamino.Detect(url)
	if platform == "" {
		_, err := c.Reply(ctx, "Link tidak dikenali. Didukung: TikTok, Instagram, YouTube, Spotify, Facebook, X (Twitter), Threads, Pinterest, Doodstream.")
		return err
	}

	c.React(ctx, "⏳")

	data, err := kamino.Resolve(ctx, url, platform, arg)
	if err != nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, fmt.Sprintf("Gagal memproses: %s. Coba lagi sebentar lagi.", err.Error()))
		return e
	}
	if data.Playlist {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, "Ini link playlist/album. Kirim link satu video/lagu saja ya, bukan playlist.")
		return e
	}
	if len(data.Medias) == 0 {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, "Tidak bisa ambil media. Mungkin private, dihapus, atau link salah.")
		return e
	}

	caption := "*" + data.Title + "*"
	if data.Source != "" {
		caption += "\n_via " + data.Source + "_"
	}

	sentAny := false
	for i, m := range data.Medias {
		cap := ""
		if i == 0 {
			cap = caption
		}
		if sendOneMedia(ctx, c, m, data.Title, cap) {
			sentAny = true
		}
	}

	if sentAny {
		c.React(ctx, "✅")
	} else {
		c.React(ctx, "❌")
	}
	return nil
}

// sendOneMedia mengirim satu media; fallback ke link teks bila gagal/terlalu besar.
// Mengembalikan true bila ada sesuatu yang terkirim.
func sendOneMedia(ctx context.Context, c *command.Ctx, m kamino.Media, title, caption string) bool {
	kind := command.MediaVideo
	switch m.Type {
	case "audio":
		kind = command.MediaAudio
	case "image":
		kind = command.MediaImage
	}

	// Audio besar (atau ukuran tak diketahui) dikirim sebagai document (port asDocument).
	fileName, mimetype := "", ""
	if m.Type == "audio" {
		kind = command.MediaAudio
		if size, sizeErr := httpx.HeadSize(ctx, m.URL, 15*time.Second); sizeErr != nil || size < 0 || size > audioDocThresholdBytes() {
			kind = command.MediaDocument
			fileName = safeFileName(title, m.Ext)
			mimetype = "audio/mpeg"
		}
	}

	var err error
	if m.Type == "video" {
		data, downloadErr := httpx.GetBytes(ctx, m.URL, 5*time.Minute, maxUploadBytes)
		if downloadErr == nil {
			metadata := downloadVideoMetadata(ctx, data)
			err = c.SendMediaBytesWithMetadata(ctx, data, command.MediaVideo, caption, "", "video/mp4", metadata)
		} else {
			err = downloadErr
		}
	} else {
		err = c.SendMedia(ctx, m.URL, kind, caption, fileName, mimetype, maxUploadBytes)
	}
	if err != nil {
		// Fallback: kirim link download langsung (port cabang catch/terlalu besar).
		prefix := ""
		if caption != "" {
			prefix = caption + "\n\n"
		}
		_, e := c.Reply(ctx, fmt.Sprintf("%sGagal kirim %s ke WhatsApp (mungkin terlalu besar). Link download langsung:\n%s", prefix, m.Type, m.URL))
		return e == nil
	}
	return true
}

// downloadVideoMetadata mengambil metadata video secara best-effort. Bila ffprobe
// atau ffmpeg tidak tersedia, ukuran file tetap diisi dan video tetap dikirim
// tanpa metadata tambahan yang gagal dibaca.
func downloadVideoMetadata(ctx context.Context, data []byte) *command.VideoMetadata {
	metadata := &command.VideoMetadata{FileSize: int64(len(data))}
	processCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "rbot-video-*")
	if err != nil {
		return metadata
	}
	defer os.RemoveAll(tmpDir)

	videoPath := filepath.Join(tmpDir, "video.mp4")
	thumbPath := filepath.Join(tmpDir, "thumb.jpg")
	if err := os.WriteFile(videoPath, data, 0o600); err != nil {
		return metadata
	}

	// Satu probe mengisi resolusi dan durasi stream video.
	if out, err := exec.CommandContext(processCtx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height,duration", "-of", "csv=p=0", videoPath).Output(); err == nil {
		fields := strings.Split(strings.TrimSpace(string(out)), ",")
		if len(fields) >= 2 {
			metadata.Width, _ = parsePositiveUint32(fields[0])
			metadata.Height, _ = parsePositiveUint32(fields[1])
		}
		if len(fields) >= 3 {
			metadata.Seconds = parseVideoDuration(fields[2])
		}
	}
	// Beberapa container hanya menyediakan durasi pada format, bukan stream.
	if metadata.Seconds == 0 {
		if out, err := exec.CommandContext(processCtx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", videoPath).Output(); err == nil {
			metadata.Seconds = parseVideoDuration(string(out))
		}
	}

	// Ambil frame pertama sebagai JPEG thumbnail 72px sesuai WhatsMeow issue #204.
	if err := exec.CommandContext(processCtx, "ffmpeg", "-y", "-v", "error", "-i", videoPath, "-frames:v", "1", "-vf", "scale=72:72:force_original_aspect_ratio=decrease", "-q:v", "8", thumbPath).Run(); err == nil {
		if thumb, readErr := os.ReadFile(thumbPath); readErr == nil && len(thumb) <= 256*1024 {
			metadata.JPEGThumbnail = thumb
		}
	}
	return metadata
}

func parseVideoDuration(raw string) uint32 {
	duration, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return 0
	}
	seconds := math.Round(duration)
	if seconds < 1 {
		seconds = 1
	}
	maxSeconds := float64(^uint32(0))
	if seconds > maxSeconds {
		seconds = maxSeconds
	}
	return uint32(seconds)
}

func parsePositiveUint32(s string) (uint32, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 32)
	if err != nil || n == 0 {
		if err == nil {
			err = fmt.Errorf("nilai harus positif")
		}
		return 0, err
	}
	return uint32(n), nil
}

// safeFileName membersihkan judul jadi nama file aman (port safeFileName).
func safeFileName(title, ext string) string {
	base := reSpaces.ReplaceAllString(reFileUnsafe.ReplaceAllString(title, " "), " ")
	base = strings.TrimSpace(base)
	if len(base) > 80 {
		base = strings.TrimSpace(base[:80])
	}
	if base == "" {
		base = "audio"
	}
	if ext == "" {
		ext = "mp3"
	}
	return base + "." + strings.TrimPrefix(path.Ext("."+ext), ".")
}
