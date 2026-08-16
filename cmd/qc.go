package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/lib/httpx"
)

const defaultAvatarURL = "https://i.ibb.co/3Fh9V6p/avatar.png"

func init() {
	command.Register(&command.Command{
		Name:        "qc",
		Category:    "Converter",
		Alias:       []string{"quotly", "fakeqc"},
		Description: "Buat stiker quote chat WhatsApp dari teks atau dengan membalas (reply) pesan. Contoh: .qc halo dunia | atau Reply pesan lalu kirim .qc",
		Handler:     qcHandler,
	})
}

type quotlyFrom struct {
	ID        int         `json:"id"`
	Name      string      `json:"name"`
	FirstName string      `json:"first_name,omitempty"`
	LastName  string      `json:"last_name,omitempty"`
	Username  string      `json:"username,omitempty"`
	Photo     quotlyPhoto `json:"photo"`
}

type quotlyPhoto struct {
	URL string `json:"url"`
}

type quotlyMedia struct {
	URL string `json:"url,omitempty"`
}

type quotlyMessage struct {
	Entities     []any          `json:"entities"`
	Avatar       bool           `json:"avatar"`
	From         quotlyFrom     `json:"from"`
	Text         string         `json:"text"`
	Media        *quotlyMedia   `json:"media,omitempty"`
	ReplyMessage map[string]any `json:"replyMessage"`
}

type quotlyRequest struct {
	Type            string          `json:"type"`
	Format          string          `json:"format"`
	BackgroundColor string          `json:"backgroundColor"`
	Width           int             `json:"width"`
	Height          int             `json:"height"`
	Scale           int             `json:"scale"`
	Messages        []quotlyMessage `json:"messages"`
}

type quotlyResponse struct {
	Result struct {
		Image string `json:"image"`
		URL   string `json:"url"`
	} `json:"result"`
	Image string `json:"image"`
}

func qcHandler(ctx context.Context, c *command.Ctx) error {
	ci := c.ContextInfo()
	var qm *waE2E.Message
	if ci != nil {
		qm = ci.GetQuotedMessage()
	}

	rawText := strings.TrimSpace(c.ArgStr())
	cleanText := rawText
	for _, p := range []string{".qc", "qc", "!qc", "/qc"} {
		if strings.HasPrefix(strings.ToLower(cleanText), p) {
			cleanText = strings.TrimSpace(cleanText[len(p):])
		}
	}

	var targetJID types.JID
	var targetName string
	var quoteText string

	if qm != nil {
		// Dari pesan yang di-reply
		if p := ci.GetParticipant(); p != "" {
			if parsed, err := types.ParseJID(p); err == nil {
				targetJID = parsed
				targetName = parsed.User
			}
		}
		if cleanText != "" {
			quoteText = cleanText
		} else {
			quoteText = command.ExtractText(qm)
		}
	} else {
		// Tanpa reply: gunakan pengirim sendiri
		targetJID = c.Sender()
		targetName = c.Evt.Info.PushName
		quoteText = cleanText
	}

	if quoteText == "" {
		_, err := c.Reply(ctx, "❌ Masukkan teks quote atau reply pesan yang ingin dibuatkan stiker quote.\nContoh:\n• *"+config.MainPrefix()+"qc halo dunia*\n• Reply pesan lalu kirim *"+config.MainPrefix()+"qc*")
		return err
	}

	if targetName == "" {
		targetName = c.SenderPhone()
	}
	if targetName == "" {
		targetName = "WhatsApp User"
	}

	c.React(ctx, "⏳")
	processCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	avatarURL := defaultAvatarURL
	if !targetJID.IsEmpty() {
		ppi, err := c.Client.GetProfilePictureInfo(processCtx, targetJID, &whatsmeow.GetProfilePictureParams{})
		if err == nil && ppi != nil && ppi.URL != "" {
			avatarURL = ppi.URL
		}
	}

	var mediaURL string
	if qm != nil {
		if data, err := c.Client.DownloadAny(processCtx, qm); err == nil && len(data) > 0 {
			mediaURL = mediaToDataURL(processCtx, data)
		}
	}

	pngBytes, err := fetchQuotlyPNG(processCtx, quoteText, targetName, avatarURL, mediaURL)
	if err != nil {
		log.Printf("[rbot] QC error (fetchQuotlyPNG): %v", err)
		c.React(ctx, "❌")
		c.Reply(ctx, "❌ Gagal membuat quote sticker: "+err.Error())
		return err
	}

	webp, err := stickerEncode(processCtx, pngBytes, false)
	if err != nil {
		log.Printf("[rbot] QC error (stickerEncode): %v", err)
		c.React(ctx, "❌")
		c.Reply(ctx, "❌ Gagal membuat stiker: "+err.Error())
		return err
	}

	thumbnail, thumbnailErr := stickerThumbnail(processCtx, webp)
	if thumbnailErr != nil {
		log.Printf("[rbot] QC error (stickerThumbnail): %v", thumbnailErr)
		c.React(ctx, "❌")
		c.Reply(ctx, "❌ Gagal membuat thumbnail stiker: "+thumbnailErr.Error())
		return thumbnailErr
	}

	webp, err = stickerAddExifContext(processCtx, webp, config.C.Sticker.Packname, config.C.Sticker.Author)
	if err != nil {
		log.Printf("[rbot] QC error (stickerAddExifContext): %v", err)
		c.React(ctx, "❌")
		c.Reply(ctx, "❌ Gagal memasang metadata stiker: "+err.Error())
		return err
	}

	if err := c.SendStickerBytesWithThumbnail(ctx, webp, thumbnail); err != nil {
		log.Printf("[rbot] QC error (SendStickerBytesWithThumbnail): %v", err)
		c.React(ctx, "❌")
		return err
	}

	c.React(ctx, "✅")
	return nil
}

func mediaToDataURL(ctx context.Context, data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if bytes.HasPrefix(data, []byte("\x89PNG")) {
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	}
	if bytes.HasPrefix(data, []byte("\xFF\xD8\xFF")) {
		return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data)
	}
	pngBytes, err := convertMediaToPNG(ctx, data)
	if err == nil && len(pngBytes) > 0 {
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
	}
	return ""
}

func convertMediaToPNG(ctx context.Context, data []byte) ([]byte, error) {
	input, err := os.CreateTemp("", "rbot-qc-media-*")
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

	outputName := inputName + ".png"
	defer os.Remove(outputName)

	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", inputName,
		"-frames:v", "1", "-vf", "scale=512:512:force_original_aspect_ratio=decrease:flags=lanczos,format=rgba", outputName,
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg convert error: %w: %s", err, tailText(string(out), 240))
	}
	return os.ReadFile(outputName)
}

func fetchQuotlyPNG(ctx context.Context, text, name, avatarURL, mediaURL string) ([]byte, error) {
	endpoints := []string{
		"https://quote.yuri.ly/generate.png",
		"https://quote.yuri.ly/generate",
	}

	var m *quotlyMedia
	if mediaURL != "" {
		m = &quotlyMedia{URL: mediaURL}
	}

	reqPayload := quotlyRequest{
		Type:            "quote",
		Format:          "png",
		BackgroundColor: "#FFFFFF",
		Width:           512,
		Height:          768,
		Scale:           2,
		Messages: []quotlyMessage{
			{
				Entities: []any{},
				Avatar:   true,
				From: quotlyFrom{
					ID:        1,
					Name:      name,
					FirstName: name,
					Photo:     quotlyPhoto{URL: avatarURL},
				},
				Text:         text,
				Media:        m,
				ReplyMessage: map[string]any{},
			},
		},
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		log.Printf("[rbot] qc payload marshal error: %v", err)
		return nil, err
	}

	for _, ep := range endpoints {
		resp, err := httpx.Do(ctx, http.MethodPost, ep, bytes.NewReader(bodyBytes), 15*time.Second, map[string]string{
			"Content-Type": "application/json",
		})
		if err != nil {
			log.Printf("[rbot] qc endpoint %s request error: %v", ep, err)
			continue
		}

		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		respBytes, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if readErr != nil || len(respBytes) == 0 {
			log.Printf("[rbot] qc endpoint %s read error (readErr: %v, len: %d)", ep, readErr, len(respBytes))
			continue
		}

		if resp.StatusCode != http.StatusOK {
			log.Printf("[rbot] qc endpoint %s status %d: %s", ep, resp.StatusCode, string(respBytes))
			continue
		}

		if strings.HasPrefix(contentType, "image/") || bytes.HasPrefix(respBytes, []byte("\x89PNG")) || bytes.HasPrefix(respBytes, []byte("RIFF")) {
			return respBytes, nil
		}

		var qResp quotlyResponse
		if err := json.Unmarshal(respBytes, &qResp); err == nil {
			rawImage := qResp.Result.Image
			if rawImage == "" {
				rawImage = qResp.Image
			}

			if rawImage != "" {
				if idx := strings.Index(rawImage, ","); idx >= 0 {
					rawImage = rawImage[idx+1:]
				}

				pngBytes, b64Err := base64.StdEncoding.DecodeString(rawImage)
				if b64Err == nil && len(pngBytes) > 0 {
					return pngBytes, nil
				} else {
					log.Printf("[rbot] qc endpoint %s base64 decode error: %v", ep, b64Err)
				}
			}

			if qResp.Result.URL != "" {
				if imgData, getErr := httpx.GetBytes(ctx, qResp.Result.URL, 15*time.Second, 5*1024*1024); getErr == nil && len(imgData) > 0 {
					return imgData, nil
				} else {
					log.Printf("[rbot] qc endpoint %s image URL fetch error: %v", ep, getErr)
				}
			}
		} else {
			log.Printf("[rbot] qc endpoint %s json unmarshal error: %v", ep, err)
		}
	}

	return nil, errors.New("seluruh LyoSU Quotly API server tidak merespon/gagal menghasilkan gambar")
}

func ExportFetchQuotlyPNG(ctx context.Context, text, name, avatarURL string) ([]byte, error) {
	return fetchQuotlyPNG(ctx, text, name, avatarURL, "")
}
