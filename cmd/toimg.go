package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"

	"rbot/brain/command"
	"rbot/brain/config"
)

const toimgMaxBytes = 20 * 1024 * 1024

type toimgSource struct {
	message *waE2E.Message
	sticker bool
	pdf     bool
}

func init() {
	command.Register(&command.Command{
		Name:        "toimg",
		Category:    "Converter",
		Alias:       []string{"toimage", "tomedia", "unstick"},
		Description: "Ubah stiker menjadi gambar/video atau PDF menjadi gambar",
		Handler:     toimgHandler,
	})
}

func toimgSourceMessage(c *command.Ctx) *toimgSource {
	if source := toimgInspect(c.Evt.Message); source != nil {
		return source
	}
	if ci := c.ContextInfo(); ci != nil {
		return toimgInspect(ci.GetQuotedMessage())
	}
	return nil
}

func toimgInspect(m *waE2E.Message) *toimgSource {
	m = hdUnwrap(m)
	if m == nil {
		return nil
	}
	if m.GetStickerMessage() != nil {
		return &toimgSource{message: m, sticker: true}
	}
	if doc := m.GetDocumentMessage(); doc != nil && strings.EqualFold(doc.GetMimetype(), "application/pdf") {
		return &toimgSource{message: m, pdf: true}
	}
	return nil
}

func toimgHandler(ctx context.Context, c *command.Ctx) error {
	source := toimgSourceMessage(c)
	if source == nil {
		_, err := c.Reply(ctx, "Reply stiker atau file PDF dengan *"+config.MainPrefix()+"toimg*.")
		return err
	}
	processCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	data, err := c.Client.DownloadAny(processCtx, source.message)
	if err != nil || len(data) == 0 {
		return toimgError(ctx, c, "Gagal mengunduh media: "+errorText(err, "media kosong"))
	}
	if len(data) > toimgMaxBytes {
		return toimgError(ctx, c, fmt.Sprintf("File terlalu besar. Maksimal %dMB.", toimgMaxBytes/(1024*1024)))
	}
	c.React(ctx, "⏳")
	if source.pdf {
		return toimgPDF(ctx, c, processCtx, data)
	}
	return toimgSticker(ctx, c, processCtx, data)
}

func toimgSticker(ctx context.Context, c *command.Ctx, processCtx context.Context, data []byte) error {
	input, err := os.CreateTemp("", "rbot-toimg-*.webp")
	if err != nil {
		return toimgError(ctx, c, err.Error())
	}
	inputName := input.Name()
	defer os.Remove(inputName)
	if _, err := input.Write(data); err != nil {
		_ = input.Close()
		return toimgError(ctx, c, err.Error())
	}
	if err := input.Close(); err != nil {
		return toimgError(ctx, c, err.Error())
	}

	// Coba baca jumlah frame. Bila gagal, tetap fallback ke PNG.
	probe := exec.CommandContext(processCtx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-count_frames", "-show_entries", "stream=nb_read_frames", "-of", "default=noprint_wrappers=1:nokey=1", inputName)
	probeOut, _ := probe.Output()
	frames := strings.TrimSpace(string(probeOut))
	animated := frames != "" && frames != "1" && frames != "N/A"
	if animated {
		outputName := inputName + ".mp4"
		defer os.Remove(outputName)
		cmd := exec.CommandContext(processCtx, "ffmpeg", "-y", "-i", inputName, "-movflags", "+faststart", "-pix_fmt", "yuv420p", "-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2:flags=lanczos", outputName)
		if out, err := cmd.CombinedOutput(); err != nil {
			return toimgError(ctx, c, "ffmpeg gagal: "+tailText(string(out), 240))
		}
		result, err := os.ReadFile(outputName)
		if err != nil {
			return toimgError(ctx, c, err.Error())
		}
		if err := c.SendMediaBytes(ctx, result, command.MediaVideo, "🎞️ Stiker → video", "sticker.mp4", "video/mp4"); err != nil {
			return toimgError(ctx, c, err.Error())
		}
		c.React(ctx, "✅")
		return nil
	}
	outputName := inputName + ".png"
	defer os.Remove(outputName)
	cmd := exec.CommandContext(processCtx, "ffmpeg", "-y", "-i", inputName, "-frames:v", "1", "-vf", "format=rgba", outputName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return toimgError(ctx, c, "ffmpeg gagal: "+tailText(string(out), 240))
	}
	result, err := os.ReadFile(outputName)
	if err != nil {
		return toimgError(ctx, c, err.Error())
	}
	if err := c.SendMediaBytes(ctx, result, command.MediaImage, "🖼️ Stiker → gambar", "sticker.png", "image/png"); err != nil {
		return toimgError(ctx, c, err.Error())
	}
	c.React(ctx, "✅")
	return nil
}

func toimgPDF(ctx context.Context, c *command.Ctx, processCtx context.Context, data []byte) error {
	dir, err := os.MkdirTemp("", "rbot-toimg-pdf-")
	if err != nil {
		return toimgError(ctx, c, err.Error())
	}
	defer os.RemoveAll(dir)
	inputName := filepath.Join(dir, "input.pdf")
	if err := os.WriteFile(inputName, data, 0o600); err != nil {
		return toimgError(ctx, c, err.Error())
	}
	prefix := filepath.Join(dir, "page")
	cmd := exec.CommandContext(processCtx, "pdftoppm", "-f", "1", "-l", "5", "-jpeg", "-r", "120", inputName, prefix)
	if out, err := cmd.CombinedOutput(); err != nil {
		return toimgError(ctx, c, "pdftoppm gagal: "+tailText(string(out), 240))
	}
	pages, err := filepath.Glob(prefix + "-*.jpg")
	if err != nil || len(pages) == 0 {
		return toimgError(ctx, c, "PDF tidak menghasilkan gambar halaman")
	}
	for i, page := range pages {
		result, readErr := os.ReadFile(page)
		if readErr != nil {
			continue
		}
		caption := fmt.Sprintf("📄 Halaman %d/%d", i+1, len(pages))
		if sendErr := c.SendMediaBytes(ctx, result, command.MediaImage, caption, fmt.Sprintf("page-%d.jpg", i+1), "image/jpeg"); sendErr != nil {
			return toimgError(ctx, c, sendErr.Error())
		}
	}
	c.React(ctx, "✅")
	return nil
}

func toimgError(ctx context.Context, c *command.Ctx, message string) error {
	c.ReportErrorMessage(ctx, message)
	c.React(ctx, "❌")
	_, err := c.Reply(ctx, "❌ "+message)
	return err
}
