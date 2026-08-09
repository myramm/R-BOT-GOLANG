// media.go: helper kirim media ke WhatsApp untuk command downloader/konverter.
// Mengunduh byte dari URL (via lib/httpx), mengunggahnya ke server WhatsApp
// (Client.Upload), lalu membangun pesan image/video/audio/document. Port dari
// mediaPayload/audioDocPayload + pemakaian sock.sendMessage di download.js.
package command

import (
	"context"
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
	if video != nil && msg.VideoMessage != nil {
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
	if quoted {
		// Sematkan quote ke pesan pemicu lewat ContextInfo tiap tipe.
		attachQuote(msg, c.Evt)
	}
	_, err = c.Client.SendMessage(ctx, c.Evt.Info.Chat, msg)
	return err
}

// SendStickerBytes mengunggah WebP lalu mengirimnya sebagai sticker WhatsApp.
func (c *Ctx) SendStickerBytes(ctx context.Context, data []byte) error {
	up, err := c.Client.Upload(ctx, data, whatsmeow.MediaImage)
	if err != nil {
		return err
	}
	msg := &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
		URL:           proto.String(up.URL),
		DirectPath:    proto.String(up.DirectPath),
		MediaKey:      up.MediaKey,
		Mimetype:      proto.String("image/webp"),
		FileEncSHA256: up.FileEncSHA256,
		FileSHA256:    up.FileSHA256,
		FileLength:    proto.Uint64(up.FileLength),
	}}
	attachQuote(msg, c.Evt)
	_, err = c.Client.SendMessage(ctx, c.Evt.Info.Chat, msg)
	return err
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
