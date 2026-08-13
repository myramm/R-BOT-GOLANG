package cmd

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/premium"
	"rbot/lib/upscaler"
)

const hdDefaultMaxDuration = 50

type hdMedia struct {
	message  *waE2E.Message
	image    bool
	mimeType string
	fileName string
}

func init() {
	command.Register(&command.Command{
		Name:        "hd",
		Category:    "Tools",
		Alias:       []string{"enhance", "upscale"},
		Description: "Tingkatkan kualitas foto/video memakai AI upscaler. Kirim atau reply media dengan .hd",
		Handler:     hdHandler,
	})
}

func hdLevel(arg string) (int, bool) {
	arg = strings.TrimSpace(strings.ToLower(arg))
	arg = strings.TrimSuffix(arg, "k")
	level, err := strconv.Atoi(arg)
	if err != nil {
		return 0, false
	}
	_, ok := upscaler.ImageLevels[level]
	return level, ok
}

func hdFindMedia(c *command.Ctx) *hdMedia {
	if m := hdMediaFromMessage(c.Evt.Message); m != nil {
		return m
	}
	if ci := c.ContextInfo(); ci != nil {
		if quoted := ci.GetQuotedMessage(); quoted != nil {
			return hdMediaFromMessage(quoted)
		}
	}
	return nil
}

func hdMediaFromMessage(m *waE2E.Message) *hdMedia {
	m = hdUnwrap(m)
	if m == nil {
		return nil
	}
	if image := m.GetImageMessage(); image != nil {
		return &hdMedia{
			message:  m,
			image:    true,
			mimeType: image.GetMimetype(),
			fileName: "image.jpg",
		}
	}
	if video := m.GetVideoMessage(); video != nil {
		return &hdMedia{
			message:  m,
			mimeType: video.GetMimetype(),
			fileName: "video.mp4",
		}
	}
	return nil
}

func hdUnwrap(m *waE2E.Message) *waE2E.Message {
	for m != nil {
		switch {
		case m.GetEphemeralMessage().GetMessage() != nil:
			m = m.GetEphemeralMessage().GetMessage()
		case m.GetViewOnceMessage().GetMessage() != nil:
			m = m.GetViewOnceMessage().GetMessage()
		case m.GetViewOnceMessageV2().GetMessage() != nil:
			m = m.GetViewOnceMessageV2().GetMessage()
		case m.GetViewOnceMessageV2Extension().GetMessage() != nil:
			m = m.GetViewOnceMessageV2Extension().GetMessage()
		default:
			return m
		}
	}
	return nil
}

func hdUsage() string {
	prefix := config.MainPrefix()
	return fmt.Sprintf(
		"📷 *Cara pakai HD / Upscale:*\n\n"+
			"1️⃣ Kirim foto/video dengan caption *%shd*\n"+
			"2️⃣ Atau reply foto/video dengan *%shd*\n\n"+
			"*Resolusi foto:* %shd 4k\n"+
			"🎬 Video otomatis di-upscale ke HD\n\n"+
			"✨ Upscale memakai AI, bukan sekadar diperbesar.\n"+
			"✨ Foto diproses dengan iLoveIMG Upscale.",
		prefix, prefix, prefix,
	)
}

func hdMaxFileSize(prem bool) int64 {
	limit := int64(config.C.Media.MaxFileSize)
	if limit <= 0 {
		limit = 30 * 1024 * 1024
	}
	if prem && limit < 100*1024*1024 {
		return 100 * 1024 * 1024
	}
	return limit
}

func hdMaxDuration(prem bool) int {
	limit := config.C.Media.MaxDuration
	if limit <= 0 {
		limit = hdDefaultMaxDuration
	}
	if prem && limit < 90 {
		return 90
	}
	return limit
}

func hdHandler(ctx context.Context, c *command.Ctx) error {
	if len(c.Args) > 0 && (strings.EqualFold(c.Args[0], "help") || strings.EqualFold(c.Args[0], "--help")) {
		_, err := c.Reply(ctx, hdUsage()+fmt.Sprintf(
			"\n\n📏 Limit video: maksimal %d detik & %dMB dari server AI.\n📦 Limit file: %dMB (Premium %dMB).",
			hdMaxDuration(false), upscaler.VideoMaxBytes/(1024*1024),
			hdMaxFileSize(false)/(1024*1024), hdMaxFileSize(true)/(1024*1024)))
		return err
	}

	media := hdFindMedia(c)
	if media == nil {
		_, err := c.Reply(ctx, hdUsage()+"\n\n❓ Ketik *"+config.MainPrefix()+"hd --help* untuk panduan lengkap.")
		return err
	}

	prem := premium.IsPremium(c.Evt)
	level := 4
	if len(c.Args) > 0 {
		parsed, ok := hdLevel(c.Args[0])
		if !ok {
			_, err := c.Reply(ctx, fmt.Sprintf("❌ Resolusi %q tidak dikenal.\n\nTersedia: 4K\nContoh: *%shd 4k*", c.Args[0], config.MainPrefix()))
			return err
		}
		if !media.image {
			_, err := c.Reply(ctx, "❌ Pilihan resolusi hanya berlaku untuk foto. Video otomatis di-upscale ke HD.")
			return err
		}
		level = parsed
	}
	caption := "⏳ Upscale foto ke " + upscaler.ImageLevels[level]
	if !media.image {
		caption = "⏳ Upscale video ke HD, bisa beberapa menit..."
	}
	// Seluruh operasi jaringan HD, termasuk status awal, berjalan di background
	// agar event loop WhatsApp segera kembali menerima command lain. Validasi
	// lokal sudah selesai sebelum goroutine dibuat; Dispatch tetap melakukan
	// charge energi secara sinkron setelah handler mengembalikan kontrol.
	go func() {
		if _, replyErr := c.Reply(ctx, caption); replyErr != nil {
			c.ReportError(ctx, replyErr)
		}
		c.React(ctx, "⏳")
		defer func() {
			if recovered := recover(); recovered != nil {
				err := fmt.Errorf("panic saat memproses HD: %v", recovered)
				hdNotifyFailure(ctx, c, err)
			}
		}()
		hdProcess(ctx, c, media, prem, level)
	}()
	return nil
}

func hdNotifyFailure(ctx context.Context, c *command.Ctx, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("[rbot] gagal mengirim notifikasi panic HD: %v", recovered)
		}
	}()
	c.React(ctx, "❌")
	c.ReportError(ctx, err)
	_, _ = c.Reply(ctx, "❌ Gagal memproses media. Coba lagi beberapa saat.")
}

func hdProcess(ctx context.Context, c *command.Ctx, media *hdMedia, prem bool, level int) {
	processCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	data, err := c.Client.DownloadAny(processCtx, media.message)
	if err != nil || len(data) == 0 {
		c.React(ctx, "❌")
		if err == nil {
			err = fmt.Errorf("media kosong")
		}
		c.ReportError(ctx, err)
		_, _ = c.Reply(ctx, "❌ Gagal mengunduh media: "+err.Error())
		return
	}

	maxSize := hdMaxFileSize(prem)
	if !media.image {
		if int64(len(data)) > upscaler.VideoMaxBytes {
			c.React(ctx, "❌")
			_, _ = c.Reply(ctx, fmt.Sprintf("❌ Video terlalu besar (%s). Server AI menerima maksimal %dMB.", formatMB(len(data)), upscaler.VideoMaxBytes/(1024*1024)))
			return
		}
		maxSize = minInt64(maxSize, upscaler.VideoMaxBytes)
	}
	if int64(len(data)) > maxSize {
		c.React(ctx, "❌")
		_, _ = c.Reply(ctx, fmt.Sprintf("❌ File terlalu besar. Maksimal %dMB.", maxSize/(1024*1024)))
		return
	}
	if !media.image {
		if duration, ok := hdVideoDuration(processCtx, data); ok && duration > float64(hdMaxDuration(prem)) {
			c.React(ctx, "❌")
			_, _ = c.Reply(ctx, fmt.Sprintf("❌ Video terlalu panjang. Maksimal %d detik.", hdMaxDuration(prem)))
			return
		}
	}

	var output []byte
	if media.image {
		output, err = upscaler.UpscaleImage(processCtx, data, level, media.mimeType)
	} else {
		output, err = upscaler.UpscaleVideo(processCtx, data, media.fileName)
	}
	if err != nil {
		c.React(ctx, "❌")
		c.ReportError(ctx, err)
		_, _ = c.Reply(ctx, "❌ Gagal memproses media. Coba lagi beberapa saat.")
		return
	}
	if !media.image {
		if worse, reason := hdVideoQualityRegression(processCtx, data, output); worse {
			c.React(ctx, "❌")
			err := fmt.Errorf("hasil AI ditolak karena kualitas turun: %s", reason)
			c.ReportError(ctx, err)
			_, _ = c.Reply(ctx, "❌ Hasil AI ditolak karena kualitas video turun. Coba video lain atau ulangi beberapa saat lagi.")
			return
		}
	}

	if media.image {
		outputMIME := http.DetectContentType(output)
		if !strings.HasPrefix(outputMIME, "image/") {
			outputMIME = media.mimeType
		}
		err = c.SendMediaBytes(ctx, output, command.MediaImage, fmt.Sprintf("✨ Foto %s selesai!", upscaler.ImageLevels[level]), "", outputMIME)
	} else {
		metadata := downloadVideoMetadata(ctx, output)
		err = c.SendMediaBytesWithMetadata(ctx, output, command.MediaVideo, "✨ Video HD selesai!", "", "video/mp4", metadata)
	}
	if err != nil {
		c.React(ctx, "❌")
		c.ReportError(ctx, err)
		_, _ = c.Reply(ctx, "❌ Gagal mengirim hasil: "+err.Error())
		return
	}
	c.React(ctx, "✅")
}

type hdVideoInfo struct {
	Width   int
	Height  int
	Bitrate int64
}

func hdVideoQualityRegression(ctx context.Context, input, output []byte) (bool, string) {
	before, beforeOK := hdVideoInfoFromBytes(ctx, input)
	after, afterOK := hdVideoInfoFromBytes(ctx, output)
	if !beforeOK {
		return true, "metadata video input tidak dapat dibaca"
	}
	if !afterOK {
		return true, "metadata video hasil tidak dapat dibaca"
	}
	if after.Width > 0 && before.Width > 0 && after.Width < before.Width {
		return true, fmt.Sprintf("resolusi turun %dx%d menjadi %dx%d", before.Width, before.Height, after.Width, after.Height)
	}
	if after.Height > 0 && before.Height > 0 && after.Height < before.Height {
		return true, fmt.Sprintf("resolusi turun %dx%d menjadi %dx%d", before.Width, before.Height, after.Width, after.Height)
	}
	if after.Width < 720 && after.Height < 720 {
		return true, fmt.Sprintf("hasil belum mencapai minimal HD (%dx%d)", after.Width, after.Height)
	}
	if before.Bitrate > 0 && after.Bitrate > 0 && after.Bitrate < before.Bitrate/3 {
		return true, fmt.Sprintf("bitrate turun terlalu jauh (%d menjadi %d bps)", before.Bitrate, after.Bitrate)
	}
	return false, ""
}

func hdVideoInfoFromBytes(ctx context.Context, data []byte) (hdVideoInfo, bool) {
	file, err := os.CreateTemp("", "rbot-hd-info-*.mp4")
	if err != nil {
		return hdVideoInfo{}, false
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return hdVideoInfo{}, false
	}
	if err := file.Close(); err != nil {
		return hdVideoInfo{}, false
	}
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height,bit_rate", "-of", "csv=p=0", name)
	out, err := cmd.Output()
	if err != nil {
		return hdVideoInfo{}, false
	}
	fields := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(fields) < 2 {
		return hdVideoInfo{}, false
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(fields[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(fields[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return hdVideoInfo{}, false
	}
	info := hdVideoInfo{Width: width, Height: height}
	if len(fields) > 2 {
		info.Bitrate, _ = strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
	}
	return info, true
}

func hdVideoDuration(ctx context.Context, data []byte) (float64, bool) {
	file, err := os.CreateTemp("", "rbot-hd-*.mp4")
	if err != nil {
		return 0, false
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return 0, false
	}
	if err := file.Close(); err != nil {
		return 0, false
	}
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", name)
	out, err := cmd.Output()
	if err != nil {
		return 0, false
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return duration, err == nil
}

func formatMB(size int) string {
	return fmt.Sprintf("%.1fMB", float64(size)/(1024*1024))
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
