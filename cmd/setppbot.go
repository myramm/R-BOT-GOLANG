package cmd

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"

	"rbot/brain/command"
)

// setppbot.go: Ubah foto profil bot dari gambar, stiker (WebP), atau URL. Port setppbot.js.

func init() {
	command.Register(&command.Command{
		Name:        "setppbot",
		Category:    "Owner",
		Alias:       []string{"setpp", "setbotpp"},
		Description: "Ubah foto profil bot dari gambar, stiker, atau URL (owner). Contoh: .setppbot (reply gambar/stiker) atau .setppbot https://link.com/gambar.jpg",
		OwnerOnly:   true,
		Handler:     setppbotHandler,
	})
}

func setppbotHandler(ctx context.Context, c *command.Ctx) error {
	var rawData []byte
	var err error

	// 1. Cek media terpaut (pesan langsung atau quoted message)
	msg := getMediaMsg(c)
	if msg != nil {
		processCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		rawData, err = c.Client.DownloadAny(processCtx, msg)
		if err != nil || len(rawData) == 0 {
			_, e := c.Reply(ctx, "❌ Gagal mengunduh gambar/stiker: "+errorText(err, "media kosong"))
			return e
		}
	} else {
		// 2. Cek URL di argumen jika tidak ada media terpaut
		argUrl := strings.TrimSpace(c.ArgStr())
		if argUrl != "" {
			if !strings.HasPrefix(argUrl, "http://") && !strings.HasPrefix(argUrl, "https://") {
				_, e := c.Reply(ctx, "❌ Masukkan URL gambar yang valid (http:// atau https://) atau reply gambar/stiker!")
				return e
			}
			rawData, err = fetchImageFromURL(ctx, argUrl)
			if err != nil {
				_, e := c.Reply(ctx, "❌ Gagal mengunduh gambar dari URL: "+err.Error())
				return e
			}
		}
	}

	if len(rawData) == 0 {
		_, e := c.Reply(ctx, "❌ Balas gambar/stiker atau sertakan URL gambar yang valid!\nContoh: .setppbot (reply gambar/stiker) atau .setppbot https://link.com/gambar.jpg")
		return e
	}

	c.React(ctx, "⏳")

	// 3. Proses Crop 1:1 & Resize ke 720x720 JPEG
	imgJpeg, err := processProfilePicture(ctx, rawData)
	if err != nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, "❌ Gagal memproses gambar foto profil: "+err.Error())
		return e
	}

	// 4. Update Foto Profil Bot di WhatsApp
	botJID := c.Client.Store.ID.ToNonAD()
	if _, err := c.Client.SetGroupPhoto(ctx, botJID, imgJpeg); err != nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, "❌ Gagal memperbarui foto profil bot: "+err.Error())
		return e
	}

	c.React(ctx, "✅")
	_, err = c.Reply(ctx, "✅ Sukses memperbarui foto profil bot!")
	return err
}

func getMediaMsg(c *command.Ctx) *waE2E.Message {
	if c.Evt == nil || c.Evt.Message == nil {
		return nil
	}
	m := c.Evt.Message
	if m.GetImageMessage() != nil || m.GetStickerMessage() != nil || m.GetDocumentMessage() != nil {
		return m
	}
	if ci := c.ContextInfo(); ci != nil {
		qm := ci.GetQuotedMessage()
		if qm != nil && (qm.GetImageMessage() != nil || qm.GetStickerMessage() != nil || qm.GetDocumentMessage() != nil) {
			return qm
		}
	}
	return nil
}

func fetchImageFromURL(ctx context.Context, urlStr string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status %d: %s", resp.StatusCode, resp.Status)
	}

	return io.ReadAll(io.LimitReader(resp.Body, 15*1024*1024))
}

func processProfilePicture(ctx context.Context, data []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		// Jika decode bawaan (image/png, image/jpeg, image/webp) gagal, coba konversi stiker via ffmpeg
		pngData, ffErr := convertWebPToPNG(ctx, data)
		if ffErr != nil {
			return nil, fmt.Errorf("gagal decode gambar atau stiker: %w", err)
		}
		src, _, err = image.Decode(bytes.NewReader(pngData))
		if err != nil {
			return nil, fmt.Errorf("gagal decode hasil konversi stiker: %w", err)
		}
	}

	bounds := src.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("dimensi gambar tidak valid (%dx%d)", w, h)
	}

	size := w
	if h < size {
		size = h
	}
	startX := bounds.Min.X + (w-size)/2
	startY := bounds.Min.Y + (h-size)/2
	cropRect := image.Rect(startX, startY, startX+size, startY+size)

	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}

	var cropped image.Image
	if si, ok := src.(subImager); ok {
		cropped = si.SubImage(cropRect)
	} else {
		rgba := image.NewRGBA(image.Rect(0, 0, size, size))
		draw.Draw(rgba, rgba.Bounds(), src, cropRect.Min, draw.Src)
		cropped = rgba
	}

	targetDim := 720
	dst := image.NewRGBA(image.Rect(0, 0, targetDim, targetDim))
	draw.BiLinear.Scale(dst, dst.Bounds(), cropped, cropped.Bounds(), draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("gagal encode JPEG foto profil: %w", err)
	}

	return buf.Bytes(), nil
}

func convertWebPToPNG(ctx context.Context, webpData []byte) ([]byte, error) {
	input, err := os.CreateTemp("", "rbot-setpp-*.webp")
	if err != nil {
		return nil, err
	}
	inputName := input.Name()
	defer os.Remove(inputName)

	if _, err := input.Write(webpData); err != nil {
		_ = input.Close()
		return nil, err
	}
	_ = input.Close()

	outputName := inputName + ".png"
	defer os.Remove(outputName)

	args := []string{
		"-y", "-hide_banner", "-loglevel", "error", "-max_error_rate", "1.0",
		"-fflags", "+discardcorrupt", "-err_detect", "ignore_err", "-i", inputName,
		"-frames:v", "1", "-vf", "format=rgba", outputName,
	}

	if out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg error: %s", string(out))
	}

	return os.ReadFile(outputName)
}
