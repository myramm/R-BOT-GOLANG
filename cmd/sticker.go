package cmd

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"

	"rbot/brain/command"
	"rbot/brain/config"
)

const (
	stickerMaxBytes    = 15 * 1024 * 1024
	stickerMaxDuration = 8
	stickerMaxOutput   = 1024 * 1024
)

type stickerSource struct {
	message *waE2E.Message
	video   bool
}

func init() {
	command.Register(&command.Command{
		Name:        "sticker",
		Category:    "Converter",
		Alias:       []string{"s", "stiker"},
		Description: "Ubah gambar/video menjadi sticker WebP. Kirim atau reply media dengan .sticker",
		Handler:     stickerHandler,
	})
}

func stickerHandler(ctx context.Context, c *command.Ctx) error {
	media := stickerSourceMessage(c)
	if media == nil {
		_, err := c.Reply(ctx, "Kirim gambar/video dengan caption *"+config.MainPrefix()+"sticker*, atau reply medianya.")
		return err
	}
	if _, err := c.Reply(ctx, "⏳ Membuat sticker..."); err != nil {
		return err
	}
	c.React(ctx, "⏳")
	processCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	data, err := c.Client.DownloadAny(processCtx, media.message)
	if err != nil || len(data) == 0 {
		return stickerError(ctx, c, "Gagal mengunduh media: "+errorText(err, "media kosong"))
	}
	if len(data) > stickerMaxBytes {
		return stickerError(ctx, c, fmt.Sprintf("Media terlalu besar. Maksimal %dMB.", stickerMaxBytes/(1024*1024)))
	}
	if media.video {
		duration, ok := hdVideoDuration(processCtx, data)
		if !ok {
			return stickerError(ctx, c, "Tidak bisa membaca durasi video. Pastikan ffprobe terpasang di server.")
		}
		if duration > stickerMaxDuration {
			return stickerError(ctx, c, fmt.Sprintf("Video terlalu panjang. Maksimal %d detik.", stickerMaxDuration))
		}
	}
	webp, err := stickerEncode(processCtx, data, media.video)
	if err != nil {
		return stickerError(ctx, c, "Gagal membuat WebP: "+err.Error())
	}
	webp, err = stickerAddExif(webp, config.C.Sticker.Packname, config.C.Sticker.Author)
	if err != nil {
		return stickerError(ctx, c, "Gagal memasang metadata sticker: "+err.Error())
	}
	if len(webp) > stickerMaxOutput {
		return stickerError(ctx, c, "Sticker lebih besar dari batas 1MB. Coba media yang lebih sederhana.")
	}
	if err := c.SendStickerBytes(ctx, webp); err != nil {
		return stickerError(ctx, c, "Gagal mengirim sticker: "+err.Error())
	}
	c.React(ctx, "✅")
	return nil
}

func stickerSourceMessage(c *command.Ctx) *stickerSource {
	if m := stickerVideoOrImage(c.Evt.Message); m != nil {
		return m
	}
	if ci := c.ContextInfo(); ci != nil {
		return stickerVideoOrImage(ci.GetQuotedMessage())
	}
	return nil
}

func stickerVideoOrImage(m *waE2E.Message) *stickerSource {
	m = hdUnwrap(m)
	if m == nil {
		return nil
	}
	if m.GetVideoMessage() != nil {
		return &stickerSource{message: m, video: true}
	}
	if m.GetImageMessage() != nil {
		return &stickerSource{message: m}
	}
	return nil
}

const stickerPackID = "com.rbot.sticker"

// stickerAddExif menambahkan metadata pack/author WhatsApp ke WebP tanpa
// mengubah gambar. Metadata disimpan sebagai chunk EXIF RIFF sehingga aman
// untuk sticker statis maupun animasi dan tidak membutuhkan webpmux.
func stickerAddExif(webp []byte, packname, author string) ([]byte, error) {
	if len(webp) < 12 || string(webp[:4]) != "RIFF" || string(webp[8:12]) != "WEBP" {
		return nil, errors.New("hasil encoder bukan WebP RIFF yang valid")
	}
	if binary.LittleEndian.Uint32(webp[4:8]) != uint32(len(webp)-8) {
		return nil, errors.New("ukuran RIFF WebP tidak valid")
	}

	packname = strings.TrimSpace(packname)
	author = strings.TrimSpace(author)
	if packname == "" {
		packname = "R-BOT"
	}
	if author == "" {
		author = packname
	}
	metadata, err := json.Marshal(map[string]any{
		"sticker-pack-id":        stickerPackID,
		"sticker-pack-name":      packname,
		"sticker-pack-publisher": author,
		"emojis":                 []string{},
	})
	if err != nil {
		return nil, err
	}
	// Ini adalah header EXIF canonical yang dipakai berbagai sticker maker
	// WhatsApp (tag 0x5741), lalu JSON metadata ditempel setelah header.
	// WhatsApp mengharapkan layout ini; jangan diganti menjadi EXIF kamera umum.
	exif := []byte{
		0x49, 0x49, 0x2a, 0x00, 0x08, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x41, 0x57, 0x07, 0x00, 0x00, 0x00,
		0x16, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	exif = append(exif, metadata...)

	var out bytes.Buffer
	out.Grow(len(webp) + 8 + len(exif) + 1)
	out.Write(webp[:12])
	for offset := 12; offset < len(webp); {
		if len(webp)-offset < 8 {
			return nil, errors.New("chunk WebP tidak lengkap")
		}
		chunkSize := binary.LittleEndian.Uint32(webp[offset+4 : offset+8])
		end := offset + 8 + int(chunkSize)
		if end < offset || end > len(webp) {
			return nil, errors.New("ukuran chunk WebP tidak valid")
		}
		paddedEnd := end
		if chunkSize%2 != 0 {
			paddedEnd++
		}
		if paddedEnd > len(webp) {
			return nil, errors.New("padding chunk WebP tidak lengkap")
		}
		if string(webp[offset:offset+4]) != "EXIF" {
			out.Write(webp[offset:paddedEnd])
		}
		offset = paddedEnd
	}
	writeWebPChunk(&out, "EXIF", exif)
	result := out.Bytes()
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)-8))
	return result, nil
}

func writeWebPChunk(out *bytes.Buffer, name string, payload []byte) {
	out.WriteString(name)
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(len(payload)))
	out.Write(size[:])
	out.Write(payload)
	if len(payload)%2 != 0 {
		out.WriteByte(0)
	}
}

func stickerEncode(ctx context.Context, data []byte, video bool) ([]byte, error) {
	input, err := os.CreateTemp("", "rbot-sticker-*.input")
	if err != nil {
		return nil, err
	}
	inputName := input.Name()
	defer os.Remove(inputName)
	if _, err := input.Write(data); err != nil {
		_ = input.Close()
		return nil, err
	}
	if err := input.Close(); err != nil {
		return nil, err
	}
	outputName := inputName + ".webp"
	defer os.Remove(outputName)
	args := []string{"-y", "-i", inputName}
	if video {
		args = append(args, "-t", fmt.Sprint(stickerMaxDuration), "-vf", "fps=15,scale=512:512:force_original_aspect_ratio=decrease:flags=lanczos,pad=512:512:-1:-1:color=black@0,format=rgba", "-an", "-loop", "0", "-c:v", "libwebp", "-q:v", "60")
	} else {
		args = append(args, "-vf", "scale=512:512:force_original_aspect_ratio=decrease:flags=lanczos,pad=512:512:-1:-1:color=black@0,format=rgba", "-frames:v", "1", "-c:v", "libwebp", "-q:v", "75")
	}
	args = append(args, outputName)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %s", err, tailText(string(out), 240))
	}
	return os.ReadFile(outputName)
}

func stickerError(ctx context.Context, c *command.Ctx, message string) error {
	c.ReportErrorMessage(ctx, message)
	c.React(ctx, "❌")
	_, err := c.Reply(ctx, "❌ "+message)
	return err
}

func errorText(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}

func tailText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
