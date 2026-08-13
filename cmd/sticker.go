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
// mengubah gambar. WebP dengan metadata wajib memakai VP8X sebagai chunk
// pertama; EXIF diletakkan setelah bitstream/animasi dan sebelum XMP.
func stickerAddExif(webp []byte, packname, author string) ([]byte, error) {
	chunks, err := parseWebPChunks(webp)
	if err != nil {
		return nil, err
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
	// Header 22-byte ini adalah format `exifAttr` de facto yang dipakai
	// Baileys dan banyak bot WhatsApp. Nilai 0x16 (22) menunjuk tepat ke
	// byte pertama JSON; jangan diganti menjadi TIFF EXIF kamera standar.
	exif := []byte{
		0x49, 0x49, 0x2a, 0x00, 0x08, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x41, 0x57, 0x07, 0x00, 0x00, 0x00,
		0x16, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	exif = append(exif, metadata...)

	// Hapus EXIF lama agar file tidak memiliki metadata ganda.
	cleanChunks := make([]webpChunk, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.kind != "EXIF" {
			cleanChunks = append(cleanChunks, chunk)
		}
	}
	chunks = cleanChunks
	vp8xIndex := -1
	for i := range chunks {
		if chunks[i].kind == "VP8X" {
			vp8xIndex = i
			break
		}
	}

	if vp8xIndex >= 0 {
		if len(chunks[vp8xIndex].payload) != 10 {
			return nil, errors.New("chunk VP8X tidak valid")
		}
		vp8x := append([]byte(nil), chunks[vp8xIndex].payload...)
		vp8x[0] |= 0x08 // EXIF metadata flag.
		chunks[vp8xIndex].payload = vp8x
	} else {
		width, height, err := webpDimensions(chunks)
		if err != nil {
			return nil, err
		}
		flags := byte(0x08) // EXIF metadata.
		for _, chunk := range chunks {
			if chunk.kind == "ALPH" {
				flags |= 0x10
			}
			if chunk.kind == "ANIM" || chunk.kind == "ANMF" {
				flags |= 0x02
			}
		}
		vp8x := make([]byte, 10)
		vp8x[0] = flags
		putWebP24(vp8x[4:7], width-1)
		putWebP24(vp8x[7:10], height-1)
		chunks = append([]webpChunk{{kind: "VP8X", payload: vp8x}}, chunks...)
		vp8xIndex = 0
	}

	// VP8X must be the first chunk. EXIF ditempatkan setelah bitstream/animasi
	// (dan sebelum XMP) mengikuti urutan chunk WebP RIFF.
	vp8x := chunks[vp8xIndex]
	withoutVP8X := append([]webpChunk(nil), chunks[:vp8xIndex]...)
	withoutVP8X = append(withoutVP8X, chunks[vp8xIndex+1:]...)
	chunks = append([]webpChunk{vp8x}, withoutVP8X...)
	insertAt := len(chunks)
	for i, chunk := range chunks {
		if chunk.kind == "XMP " {
			insertAt = i
			break
		}
	}
	chunks = append(chunks, webpChunk{})
	copy(chunks[insertAt+1:], chunks[insertAt:])
	chunks[insertAt] = webpChunk{kind: "EXIF", payload: exif}

	var out bytes.Buffer
	out.Grow(len(webp) + 8 + len(exif) + 1)
	out.WriteString("RIFF")
	out.Write(make([]byte, 4))
	out.WriteString("WEBP")
	for _, chunk := range chunks {
		writeWebPChunk(&out, chunk.kind, chunk.payload)
	}
	result := out.Bytes()
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)-8))
	return result, nil
}

type webpChunk struct {
	kind    string
	payload []byte
}

func parseWebPChunks(webp []byte) ([]webpChunk, error) {
	if len(webp) < 12 || string(webp[:4]) != "RIFF" || string(webp[8:12]) != "WEBP" {
		return nil, errors.New("hasil encoder bukan WebP RIFF yang valid")
	}
	if binary.LittleEndian.Uint32(webp[4:8]) != uint32(len(webp)-8) {
		return nil, errors.New("ukuran RIFF WebP tidak valid")
	}
	chunks := make([]webpChunk, 0, 3)
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
		if chunkSize%2 != 0 && webp[end] != 0 {
			return nil, errors.New("padding chunk WebP harus nol")
		}
		chunks = append(chunks, webpChunk{
			kind:    string(webp[offset : offset+4]),
			payload: append([]byte(nil), webp[offset+8:end]...),
		})
		offset = paddedEnd
	}
	return chunks, nil
}

func webpDimensions(chunks []webpChunk) (uint32, uint32, error) {
	for _, chunk := range chunks {
		switch chunk.kind {
		case "VP8 ":
			if len(chunk.payload) < 10 || !bytes.Equal(chunk.payload[3:6], []byte{0x9d, 0x01, 0x2a}) {
				return 0, 0, errors.New("bitstream VP8 tidak valid")
			}
			width := uint32(binary.LittleEndian.Uint16(chunk.payload[6:8]) & 0x3fff)
			height := uint32(binary.LittleEndian.Uint16(chunk.payload[8:10]) & 0x3fff)
			if width == 0 || height == 0 {
				return 0, 0, errors.New("dimensi VP8 tidak valid")
			}
			return width, height, nil
		case "VP8L":
			if len(chunk.payload) < 5 || chunk.payload[0] != 0x2f {
				return 0, 0, errors.New("bitstream VP8L tidak valid")
			}
			bits := binary.LittleEndian.Uint32(chunk.payload[1:5])
			width := 1 + (bits & 0x3fff)
			height := 1 + ((bits >> 14) & 0x3fff)
			if width == 0 || height == 0 {
				return 0, 0, errors.New("dimensi VP8L tidak valid")
			}
			return width, height, nil
		}
	}
	return 0, 0, errors.New("dimensi WebP tidak ditemukan")
}

func putWebP24(dst []byte, value uint32) {
	dst[0] = byte(value)
	dst[1] = byte(value >> 8)
	dst[2] = byte(value >> 16)
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
