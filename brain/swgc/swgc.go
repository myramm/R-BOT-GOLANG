package swgc

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// BufferItem menyimpan data media/teks sementara untuk alur 2-step .swgc -> .swgc_process.
type BufferItem struct {
	Media    []byte
	Mime     string
	Caption  string
	UpdateAt time.Time
}

var (
	bufMu   sync.RWMutex
	buffers = make(map[string]*BufferItem)
)

// SetBuffer menyimpan buffer media/teks pengguna.
func SetBuffer(senderID string, media []byte, mime, caption string) {
	bufMu.Lock()
	defer bufMu.Unlock()
	buffers[senderID] = &BufferItem{
		Media:    media,
		Mime:     mime,
		Caption:  caption,
		UpdateAt: time.Now(),
	}
}

// GetBuffer mengambil buffer media/teks pengguna jika belum expired (15 menit).
func GetBuffer(senderID string) (*BufferItem, bool) {
	bufMu.RLock()
	defer bufMu.RUnlock()
	item, ok := buffers[senderID]
	if !ok {
		return nil, false
	}
	if time.Since(item.UpdateAt) > 15*time.Minute {
		return nil, false
	}
	return item, true
}

// ClearBuffer menghapus buffer pengguna.
func ClearBuffer(senderID string) {
	bufMu.Lock()
	defer bufMu.Unlock()
	delete(buffers, senderID)
}

// SendGroupStatus mengirimkan pesan Group Status (SWGC) ke JID grup target.
func SendGroupStatus(ctx context.Context, client *whatsmeow.Client, groupJID types.JID, mime string, data []byte, caption string) error {
	if client == nil || !client.IsConnected() {
		return fmt.Errorf("koneksi WhatsApp tidak aktif")
	}

	ctxInfo := &waE2E.ContextInfo{
		IsGroupStatus: proto.Bool(true),
	}

	var innerMsg *waE2E.Message
	mimeLower := strings.ToLower(mime)

	if len(data) > 0 {
		var appInfo whatsmeow.MediaType
		switch {
		case strings.HasPrefix(mimeLower, "image/"):
			appInfo = whatsmeow.MediaImage
		case strings.HasPrefix(mimeLower, "video/"):
			appInfo = whatsmeow.MediaVideo
		case strings.HasPrefix(mimeLower, "audio/"):
			appInfo = whatsmeow.MediaAudio
		default:
			appInfo = whatsmeow.MediaDocument
		}

		up, err := client.Upload(ctx, data, appInfo)
		if err != nil {
			return fmt.Errorf("gagal mengunggah media ke server WhatsApp: %w", err)
		}

		switch appInfo {
		case whatsmeow.MediaVideo:
			v := &waE2E.VideoMessage{
				URL:           proto.String(up.URL),
				DirectPath:    proto.String(up.DirectPath),
				MediaKey:      up.MediaKey,
				Mimetype:      proto.String(orDefault(mime, "video/mp4")),
				FileEncSHA256: up.FileEncSHA256,
				FileSHA256:    up.FileSHA256,
				FileLength:    proto.Uint64(up.FileLength),
				ContextInfo:   ctxInfo,
			}
			if caption != "" {
				v.Caption = proto.String(caption)
			}
			innerMsg = &waE2E.Message{VideoMessage: v}

		case whatsmeow.MediaAudio:
			a := &waE2E.AudioMessage{
				URL:           proto.String(up.URL),
				DirectPath:    proto.String(up.DirectPath),
				MediaKey:      up.MediaKey,
				Mimetype:      proto.String(orDefault(mime, "audio/mp4")),
				FileEncSHA256: up.FileEncSHA256,
				FileSHA256:    up.FileSHA256,
				FileLength:    proto.Uint64(up.FileLength),
				ContextInfo:   ctxInfo,
			}
			innerMsg = &waE2E.Message{AudioMessage: a}

		default: // MediaImage / Document
			im := &waE2E.ImageMessage{
				URL:           proto.String(up.URL),
				DirectPath:    proto.String(up.DirectPath),
				MediaKey:      up.MediaKey,
				Mimetype:      proto.String(orDefault(mime, "image/jpeg")),
				FileEncSHA256: up.FileEncSHA256,
				FileSHA256:    up.FileSHA256,
				FileLength:    proto.Uint64(up.FileLength),
				ContextInfo:   ctxInfo,
			}
			if caption != "" {
				im.Caption = proto.String(caption)
			}
			innerMsg = &waE2E.Message{ImageMessage: im}
		}
	} else if caption != "" {
		innerMsg = &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:        proto.String(caption),
				ContextInfo: ctxInfo,
			},
		}
	} else {
		return fmt.Errorf("media atau caption teks tidak boleh kosong")
	}

	statusMsg := &waE2E.Message{
		GroupStatusMessage: &waE2E.FutureProofMessage{
			Message: innerMsg,
		},
		GroupStatusMessageV2: &waE2E.FutureProofMessage{
			Message: innerMsg,
		},
	}

	// 1. Kirim GroupStatusMessage ke groupJID
	_, err := client.SendMessage(ctx, groupJID, statusMsg)
	if err != nil {
		// Fallback: Kirim innerMsg langsung dengan IsGroupStatus = true
		if _, errFallback := client.SendMessage(ctx, groupJID, innerMsg); errFallback != nil {
			return fmt.Errorf("gagal mengirim Group Status: %w", err)
		}
	}
	return nil
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
