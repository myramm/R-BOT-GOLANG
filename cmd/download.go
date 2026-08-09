package cmd

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path"
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

var reHTTP = regexp.MustCompile(`(?i)^https?://`)
var reFileUnsafe = regexp.MustCompile(`[\\/:*?"<>|]+`)
var reSpaces = regexp.MustCompile(`\s+`)

func init() {
	command.Register(&command.Command{
		Name:        "download",
		Category:    "Downloader",
		Alias:       []string{"dl"},
		Description: "Download video/foto/lagu dari TikTok, Instagram, YouTube, Spotify, Facebook, X, Threads, Pinterest. Contoh: .download <link>  •  YouTube jadi mp3: .download <link> mp3",
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

func downloadHandler(ctx context.Context, c *command.Ctx) error {
	url := ""
	if len(c.Args) > 0 {
		url = strings.TrimSpace(c.Args[0])
	}
	if url == "" || !reHTTP.MatchString(url) {
		_, err := c.Reply(ctx, "Kasih link-nya. Contoh:\n.download https://vt.tiktok.com/xxxx\n.download <link youtube> mp3")
		return err
	}
	platform := kamino.Detect(url)
	if platform == "" {
		_, err := c.Reply(ctx, "Link tidak dikenali. Didukung: TikTok, Instagram, YouTube, Spotify, Facebook, X (Twitter), Threads, Pinterest.")
		return err
	}

	c.React(ctx, "⏳")

	arg := ""
	if len(c.Args) > 1 {
		arg = c.Args[1]
	}
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
			err = c.SendMediaBytesStandalone(ctx, data, command.MediaVideo, caption, "", "video/mp4", metadata)
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
// atau ffmpeg tidak tersedia, nil dikembalikan dan video tetap dikirim tanpa
// thumbnail/durasi/dimensi tambahan.
func downloadVideoMetadata(ctx context.Context, data []byte) *command.VideoMetadata {
	processCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	file, err := os.CreateTemp("", "rbot-download-*.mp4")
	if err != nil {
		return nil
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return nil
	}
	if err := file.Close(); err != nil {
		return nil
	}

	metadata := &command.VideoMetadata{}
	if out, err := exec.CommandContext(processCtx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", name).Output(); err == nil {
		if duration, parseErr := strconv.ParseFloat(strings.TrimSpace(string(out)), 64); parseErr == nil && duration > 0 && !math.IsNaN(duration) && !math.IsInf(duration, 0) {
			seconds := math.Ceil(duration)
			maxSeconds := float64(^uint32(0))
			if seconds > maxSeconds {
				seconds = maxSeconds
			}
			metadata.Seconds = uint32(seconds)
		}
	}
	if out, err := exec.CommandContext(processCtx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "csv=p=0:s=x", name).Output(); err == nil {
		parts := strings.Split(strings.TrimSpace(string(out)), "x")
		if len(parts) == 2 {
			metadata.Width, _ = parsePositiveUint32(parts[0])
			metadata.Height, _ = parsePositiveUint32(parts[1])
		}
	}
	if thumb := downloadVideoThumbnail(processCtx, name); len(thumb) > 0 {
		metadata.JPEGThumbnail = thumb
	}
	if metadata.Seconds == 0 && metadata.Width == 0 && metadata.Height == 0 && len(metadata.JPEGThumbnail) == 0 {
		return nil
	}
	return metadata
}

func downloadVideoThumbnail(ctx context.Context, name string) []byte {
	const maxThumbnailBytes = 256 * 1024
	for _, quality := range []string{"8", "15"} {
		thumb, err := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-i", name, "-frames:v", "1", "-vf", "scale=480:480:force_original_aspect_ratio=decrease", "-f", "image2pipe", "-c:v", "mjpeg", "-q:v", quality, "pipe:1").Output()
		if err == nil && len(thumb) > 0 && len(thumb) <= maxThumbnailBytes {
			return thumb
		}
	}
	return nil
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
