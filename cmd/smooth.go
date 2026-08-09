package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/premium"
)

const (
	smoothDefaultFPS       = 60
	smoothMinFPS           = 30
	smoothPremiumMaxFPS    = 120
	smoothDefaultMaxLength = 50
	smoothDefaultMaxBytes  = 30 * 1024 * 1024
	smoothPremiumMaxBytes  = 60 * 1024 * 1024
	smoothProcessTimeout   = 10 * time.Minute
)

func init() {
	command.Register(&command.Command{
		Name:        "smooth",
		Category:    "Tools",
		Alias:       []string{"pelancar", "interpolate"},
		Description: "Memperlancar video dengan motion interpolation 30–60 FPS; premium sampai 120 FPS",
		Handler:     smoothHandler,
	})
}

func smoothUsage() string {
	prefix := config.MainPrefix()
	return fmt.Sprintf(
		"🎬 *Panduan Pelancar Video*\n\n"+
			"*Cara pakai:*\n"+
			"1️⃣ Kirim video dengan caption *%ssmooth*\n"+
			"2️⃣ Atau reply video dengan *%ssmooth*\n\n"+
			"*Custom FPS:*\n"+
			"• *%ssmooth* → default 60fps\n"+
			"• *%ssmooth 48* → 48fps\n"+
			"• *%ssmooth 120* → 120fps (Premium)\n\n"+
			"✨ Motion interpolation membuat gerakan video lebih mulus.\n"+
			"📏 Range: 30–60 FPS gratis, sampai 120 FPS Premium.",
		prefix, prefix, prefix, prefix, prefix,
	)
}

func smoothVideoMessage(c *command.Ctx) *waE2E.Message {
	if m := smoothDirectVideo(c.Evt.Message); m != nil {
		return m
	}
	if ci := c.ContextInfo(); ci != nil {
		return smoothDirectVideo(ci.GetQuotedMessage())
	}
	return nil
}

func smoothDirectVideo(m *waE2E.Message) *waE2E.Message {
	m = hdUnwrap(m)
	if m == nil || m.GetVideoMessage() == nil {
		return nil
	}
	return m
}

func smoothMaxBytes(premiumUser bool) int64 {
	limit := int64(config.C.Media.MaxFileSize)
	if limit <= 0 {
		limit = smoothDefaultMaxBytes
	}
	if premiumUser && limit < smoothPremiumMaxBytes {
		return smoothPremiumMaxBytes
	}
	return limit
}

func smoothMaxDuration(premiumUser bool) int {
	limit := config.C.Media.MaxDuration
	if limit <= 0 {
		limit = smoothDefaultMaxLength
	}
	if premiumUser && limit < 90 {
		return 90
	}
	return limit
}

func smoothFPS(args []string) (int, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return smoothDefaultFPS, nil
	}
	fps, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil {
		return 0, fmt.Errorf("FPS harus berupa angka")
	}
	return fps, nil
}

func smoothHandler(ctx context.Context, c *command.Ctx) error {
	if len(c.Args) > 0 && (strings.EqualFold(c.Args[0], "help") || strings.EqualFold(c.Args[0], "--help")) {
		_, err := c.Reply(ctx, smoothUsage()+fmt.Sprintf(
			"\n\n⏱️ Limit: maksimal %d detik dan %dMB gratis. Premium: %d detik dan %dMB.",
			smoothMaxDuration(false), smoothMaxBytes(false)/(1024*1024),
			smoothMaxDuration(true), smoothMaxBytes(true)/(1024*1024)))
		return err
	}

	video := smoothVideoMessage(c)
	if video == nil {
		_, err := c.Reply(ctx, smoothUsage()+"\n\n❓ Ketik *"+config.MainPrefix()+"smooth --help* untuk panduan lengkap.")
		return err
	}

	premiumUser := premium.IsPremium(c.Evt)
	fps, err := smoothFPS(c.Args)
	if err != nil {
		_, replyErr := c.Reply(ctx, "❌ "+err.Error()+". Contoh: *"+config.MainPrefix()+"smooth 60*")
		return replyErr
	}
	maxFPS := smoothPremiumMaxFPS
	if !premiumUser {
		maxFPS = smoothDefaultFPS
	}
	if fps < smoothMinFPS || fps > maxFPS {
		if premiumUser {
			_, err = c.Reply(ctx, "❌ FPS harus antara 30–120.")
		} else {
			err = func() error {
				_, e := c.Reply(ctx, "❌ FPS harus antara 30–60.\n💎 Mau sampai 120fps? Upgrade *"+config.MainPrefix()+"premium*.")
				return e
			}()
		}
		return err
	}

	if _, err := c.Reply(ctx, fmt.Sprintf("⏳ Memuluskan video ke %dfps...", fps)); err != nil {
		return err
	}
	c.React(ctx, "⏳")

	processCtx, cancel := context.WithTimeout(ctx, smoothProcessTimeout)
	defer cancel()
	data, err := c.Client.DownloadAny(processCtx, video)
	if err != nil || len(data) == 0 {
		c.React(ctx, "❌")
		if err == nil {
			err = fmt.Errorf("media kosong")
		}
		c.ReportError(ctx, err)
		_, replyErr := c.Reply(ctx, "❌ Gagal mengunduh video: "+err.Error())
		return replyErr
	}

	maxBytes := smoothMaxBytes(premiumUser)
	if int64(len(data)) > maxBytes {
		c.ReportErrorMessage(ctx, fmt.Sprintf("File terlalu besar. Maksimal %dMB.", maxBytes/(1024*1024)))
		c.React(ctx, "❌")
		_, replyErr := c.Reply(ctx, fmt.Sprintf("❌ File terlalu besar. Maksimal %dMB.", maxBytes/(1024*1024)))
		return replyErr
	}
	duration, ok := hdVideoDuration(processCtx, data)
	if !ok {
		c.ReportErrorMessage(ctx, "Tidak bisa membaca durasi video. Pastikan ffprobe terpasang di server.")
		c.React(ctx, "❌")
		_, replyErr := c.Reply(ctx, "❌ Tidak bisa membaca durasi video. Pastikan ffprobe terpasang di server.")
		return replyErr
	}
	if duration > float64(smoothMaxDuration(premiumUser)) {
		c.ReportErrorMessage(ctx, fmt.Sprintf("Video terlalu panjang. Maksimal %d detik.", smoothMaxDuration(premiumUser)))
		c.React(ctx, "❌")
		_, replyErr := c.Reply(ctx, fmt.Sprintf("❌ Video terlalu panjang. Maksimal %d detik.", smoothMaxDuration(premiumUser)))
		return replyErr
	}

	result, err := smoothTranscode(processCtx, data, fps)
	if err != nil {
		c.ReportError(ctx, err)
		c.React(ctx, "❌")
		_, replyErr := c.Reply(ctx, "❌ Gagal memproses video: "+err.Error())
		return replyErr
	}
	if err := c.SendMediaBytes(ctx, result, command.MediaVideo, fmt.Sprintf("✨ Video %dfps selesai", fps), "", "video/mp4"); err != nil {
		c.ReportError(ctx, err)
		c.React(ctx, "❌")
		_, replyErr := c.Reply(ctx, "❌ Gagal mengirim hasil: "+err.Error())
		return replyErr
	}
	c.React(ctx, "✅")
	return nil
}

func smoothTranscode(ctx context.Context, data []byte, fps int) ([]byte, error) {
	input, err := os.CreateTemp("", "rbot-smooth-*.in")
	if err != nil {
		return nil, fmt.Errorf("buat file sementara: %w", err)
	}
	inputName := input.Name()
	defer os.Remove(inputName)
	if _, err := input.Write(data); err != nil {
		_ = input.Close()
		return nil, fmt.Errorf("tulis video sementara: %w", err)
	}
	if err := input.Close(); err != nil {
		return nil, fmt.Errorf("tutup video sementara: %w", err)
	}

	outputName := inputName + ".mp4"
	defer os.Remove(outputName)
	args := []string{
		"-y", "-i", inputName,
		"-vf", fmt.Sprintf("minterpolate=fps=%d:mi_mode=mci:mc_mode=aobmc:me_mode=bidir:vsbmc=1", fps),
		"-c:v", "libx264", "-preset", "medium", "-crf", "20",
		"-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", outputName,
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if len(message) > 300 {
			message = message[len(message)-300:]
		}
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("timeout memproses video")
		}
		if message == "" {
			return nil, fmt.Errorf("ffmpeg gagal: %w", err)
		}
		return nil, fmt.Errorf("ffmpeg gagal: %s", message)
	}
	result, err := os.ReadFile(outputName)
	if err != nil {
		return nil, fmt.Errorf("baca hasil ffmpeg: %w", err)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("hasil ffmpeg kosong")
	}
	return result, nil
}
