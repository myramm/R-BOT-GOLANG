// media.go: helper kirim media ke WhatsApp untuk command downloader/konverter.
// Mengunduh byte dari URL (via lib/httpx), mengunggahnya ke server WhatsApp
// (Client.Upload), lalu membangun pesan image/video/audio/document. Port dari
// mediaPayload/audioDocPayload + pemakaian sock.sendMessage di download.js.
package command

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	"rbot/lib/httpx"
)

// MediaKind menentukan jenis pesan media yang dibangun.
type MediaKind int

const (
	MediaImage MediaKind = iota
	MediaVideo
	MediaAudio
	MediaDocument
)

const mediaDownloadTimeout = 5 * time.Minute

// VideoMetadata adalah metadata opsional untuk pesan video mandiri.
// Nilai nol berarti field tersebut tidak disertakan.
type VideoMetadata struct {
	Seconds       uint32
	Width         uint32
	Height        uint32
	JPEGThumbnail []byte
	FileSize      int64
}

// SendMedia mengunduh media dari url, mengunggah ke WhatsApp, lalu mengirimnya
// sebagai balasan (quote pesan pemicu). caption dipakai untuk image/video/document.
// fileName & mimetype hanya dipakai document/audio; kosongkan untuk default.
func (c *Ctx) SendMedia(ctx context.Context, url string, kind MediaKind, caption, fileName, mimetype string, maxBytes int64) error {
	data, err := httpx.GetBytes(ctx, url, mediaDownloadTimeout, maxBytes)
	if err != nil {
		return err
	}
	return c.SendMediaBytes(ctx, data, kind, caption, fileName, mimetype)
}

// SendMediaBytes seperti SendMedia tapi memakai byte yang sudah ada di memori
// (hasil konversi/unduh lokal), bukan mengunduh dari URL. Dipakai command
// konverter (rvo, sticker, toimg, get, dll). Pesan dikirim dengan quote.
func (c *Ctx) SendMediaBytes(ctx context.Context, data []byte, kind MediaKind, caption, fileName, mimetype string) error {
	return c.sendMediaBytes(ctx, data, kind, caption, fileName, mimetype, true, nil)
}

// SendMediaBytesWithMetadata seperti SendMediaBytes, tetapi dapat menyertakan
// metadata video (thumbnail, durasi, dan dimensi) pada pesan yang dikutip.
func (c *Ctx) SendMediaBytesWithMetadata(ctx context.Context, data []byte, kind MediaKind, caption, fileName, mimetype string, video *VideoMetadata) error {
	return c.sendMediaBytes(ctx, data, kind, caption, fileName, mimetype, true, video)
}

// SendMediaBytesStandalone mengirim media tanpa mengutip pesan pemicu. Ini
// dipakai downloader agar hasil media tampil sebagai pesan baru di chat.
// Untuk video, metadata opsional dapat mengisi thumbnail, durasi, dan dimensi.
func (c *Ctx) SendMediaBytesStandalone(ctx context.Context, data []byte, kind MediaKind, caption, fileName, mimetype string, video *VideoMetadata) error {
	return c.sendMediaBytes(ctx, data, kind, caption, fileName, mimetype, false, video)
}

func (c *Ctx) sendMediaBytes(ctx context.Context, data []byte, kind MediaKind, caption, fileName, mimetype string, quoted bool, video *VideoMetadata) error {
	appInfo := whatsmeow.MediaImage
	switch kind {
	case MediaVideo:
		appInfo = whatsmeow.MediaVideo
	case MediaAudio:
		appInfo = whatsmeow.MediaAudio
	case MediaDocument:
		appInfo = whatsmeow.MediaDocument
	}

	up, err := c.Client.Upload(ctx, data, appInfo)
	if err != nil {
		return err
	}

	msg := buildMediaMessage(kind, up, caption, fileName, mimetype)
	applyVideoMetadata(msg, video)
	if kind == MediaImage || kind == MediaVideo {
		thumbnail := videoThumbnail(video)
		if len(thumbnail) == 0 {
			thumbnail, _ = generateJPEGThumbnail(ctx, data)
		}
		if len(thumbnail) > 0 {
			applyInlineJPEGThumbnail(msg, thumbnail)
			// Issue #204: upload thumbnail separately so clients that do not have
			// the sender saved can fetch a preview from its direct path.
			if thumb, uploadErr := c.Client.Upload(ctx, thumbnail, whatsmeow.MediaImage); uploadErr == nil {
				applyUploadedThumbnail(msg, thumb)
			}
		}
	}
	if quoted {
		// Sematkan quote ke pesan pemicu lewat ContextInfo tiap tipe.
		attachQuote(msg, c.Evt)
	}
	_, err = c.Client.SendMessage(ctx, c.Evt.Info.Chat, msg)
	return err
}

func videoThumbnail(video *VideoMetadata) []byte {
	if video == nil {
		return nil
	}
	return video.JPEGThumbnail
}

func applyInlineJPEGThumbnail(msg *waE2E.Message, thumbnail []byte) {
	if msg == nil || len(thumbnail) == 0 {
		return
	}
	switch {
	case msg.ImageMessage != nil:
		msg.ImageMessage.JPEGThumbnail = thumbnail
	case msg.VideoMessage != nil:
		msg.VideoMessage.JPEGThumbnail = thumbnail
	}
}

func applyUploadedThumbnail(msg *waE2E.Message, up whatsmeow.UploadResponse) {
	if msg == nil {
		return
	}
	switch {
	case msg.ImageMessage != nil:
		msg.ImageMessage.ThumbnailDirectPath = proto.String(up.DirectPath)
		msg.ImageMessage.ThumbnailSHA256 = up.FileSHA256
		msg.ImageMessage.ThumbnailEncSHA256 = up.FileEncSHA256
	case msg.VideoMessage != nil:
		msg.VideoMessage.ThumbnailDirectPath = proto.String(up.DirectPath)
		msg.VideoMessage.ThumbnailSHA256 = up.FileSHA256
		msg.VideoMessage.ThumbnailEncSHA256 = up.FileEncSHA256
	}
}

func generateJPEGThumbnail(ctx context.Context, data []byte) ([]byte, error) {
	thumbCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(thumbCtx, "ffmpeg", "-v", "error", "-i", "pipe:0", "-frames:v", "1", "-vf", "scale=72:72:force_original_aspect_ratio=decrease", "-f", "image2pipe", "-c:v", "mjpeg", "-q:v", "8", "pipe:1")
	cmd.Stdin = bytes.NewReader(data)
	thumbnail, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	if len(thumbnail) == 0 {
		return nil, fmt.Errorf("thumbnail kosong")
	}
	if len(thumbnail) > 64*1024 {
		return nil, fmt.Errorf("thumbnail terlalu besar: %d bytes", len(thumbnail))
	}
	return thumbnail, nil
}

func applyVideoMetadata(msg *waE2E.Message, video *VideoMetadata) {
	if msg == nil || msg.VideoMessage == nil || video == nil {
		return
	}
	if video.Seconds > 0 {
		msg.VideoMessage.Seconds = proto.Uint32(video.Seconds)
	}
	if video.Width > 0 {
		msg.VideoMessage.Width = proto.Uint32(video.Width)
	}
	if video.Height > 0 {
		msg.VideoMessage.Height = proto.Uint32(video.Height)
	}
	if len(video.JPEGThumbnail) > 0 {
		msg.VideoMessage.JPEGThumbnail = video.JPEGThumbnail
	}
}

// SendStickerBytes mengunggah WebP lalu mengirimnya sebagai sticker WhatsApp.
func (c *Ctx) SendStickerBytes(ctx context.Context, data []byte) error {
	return c.SendStickerBytesWithThumbnail(ctx, data, nil)
}

// SendStickerBytesWithThumbnail mengirim sticker dengan thumbnail PNG inline
// opsional. Thumbnail tidak di-upload terpisah karena StickerMessage memakai
// field PngThumbnail untuk preview kecil.
func (c *Ctx) SendStickerBytesWithThumbnail(ctx context.Context, data, thumbnail []byte) error {
	up, err := c.Client.Upload(ctx, data, whatsmeow.MediaImage)
	if err != nil {
		return err
	}
	msg := buildStickerMessage(up, thumbnail)
	attachQuote(msg, c.Evt)
	_, err = c.Client.SendMessage(ctx, c.Evt.Info.Chat, msg)
	return err
}

func buildStickerMessage(up whatsmeow.UploadResponse, thumbnail []byte) *waE2E.Message {
	now := time.Now()
	sticker := &waE2E.StickerMessage{
		URL:               proto.String(up.URL),
		DirectPath:        proto.String(up.DirectPath),
		MediaKey:          up.MediaKey,
		Mimetype:          proto.String("image/webp"),
		FileEncSHA256:     up.FileEncSHA256,
		FileSHA256:        up.FileSHA256,
		FileLength:        proto.Uint64(up.FileLength),
		Width:             proto.Uint32(512),
		Height:            proto.Uint32(512),
		MediaKeyTimestamp: proto.Int64(now.Unix()),
		StickerSentTS:     proto.Int64(now.UnixMilli()),
	}
	if len(thumbnail) > 0 && len(thumbnail) <= 64*1024 && len(thumbnail) >= 8 &&
		thumbnail[0] == 0x89 && thumbnail[1] == 'P' && thumbnail[2] == 'N' && thumbnail[3] == 'G' {
		sticker.PngThumbnail = thumbnail
	}
	return &waE2E.Message{StickerMessage: sticker}
}

func buildMediaMessage(kind MediaKind, up whatsmeow.UploadResponse, caption, fileName, mimetype string) *waE2E.Message {
	switch kind {
	case MediaVideo:
		v := &waE2E.VideoMessage{
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String(orDefault(mimetype, "video/mp4")),
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(up.FileLength),
		}
		if caption != "" {
			v.Caption = proto.String(caption)
		}
		return &waE2E.Message{VideoMessage: v}

	case MediaAudio:
		a := &waE2E.AudioMessage{
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String(orDefault(mimetype, "audio/mpeg")),
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(up.FileLength),
		}
		return &waE2E.Message{AudioMessage: a}

	case MediaDocument:
		d := &waE2E.DocumentMessage{
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String(orDefault(mimetype, "application/octet-stream")),
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(up.FileLength),
		}
		if fileName != "" {
			d.FileName = proto.String(fileName)
		}
		if caption != "" {
			d.Caption = proto.String(caption)
		}
		return &waE2E.Message{DocumentMessage: d}

	default: // MediaImage
		im := &waE2E.ImageMessage{
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String(orDefault(mimetype, "image/jpeg")),
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(up.FileLength),
		}
		if caption != "" {
			im.Caption = proto.String(caption)
		}
		return &waE2E.Message{ImageMessage: im}
	}
}

// attachQuote menyematkan ContextInfo (quote pesan pemicu) ke pesan media.
func attachQuote(msg *waE2E.Message, evt *events.Message) {
	ci := &waE2E.ContextInfo{
		StanzaID:      proto.String(evt.Info.ID),
		Participant:   proto.String(evt.Info.Sender.String()),
		QuotedMessage: evt.Message,
	}
	switch {
	case msg.ImageMessage != nil:
		msg.ImageMessage.ContextInfo = ci
	case msg.VideoMessage != nil:
		msg.VideoMessage.ContextInfo = ci
	case msg.AudioMessage != nil:
		msg.AudioMessage.ContextInfo = ci
	case msg.DocumentMessage != nil:
		msg.DocumentMessage.ContextInfo = ci
	case msg.StickerMessage != nil:
		msg.StickerMessage.ContextInfo = ci
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
