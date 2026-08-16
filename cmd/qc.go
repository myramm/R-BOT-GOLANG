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
	"net/url"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
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
		Description: "Buat stiker quote chat WhatsApp dari teks atau balasan pesan",
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

type quotlyMessage struct {
	Entities     []any          `json:"entities"`
	Avatar       bool           `json:"avatar"`
	From         quotlyFrom     `json:"from"`
	Text         string         `json:"text"`
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
	text, targetJID, targetName := extractQCTarget(c)
	if text == "" {
		_, err := c.Reply(ctx, "Kirim teks dengan *"+config.MainPrefix()+"qc <teks>*, atau reply pesan orang yang mau dijadikan stiker quote.")
		return err
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

	pngBytes, err := fetchQuotlyPNG(processCtx, text, targetName, avatarURL)
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

func extractQCTarget(c *command.Ctx) (string, types.JID, string) {
	text := strings.TrimSpace(c.ArgStr())
	targetJID := c.Sender()
	targetName := c.Evt.Info.PushName

	if ci := c.ContextInfo(); ci != nil {
		if qm := ci.GetQuotedMessage(); qm != nil {
			if text == "" {
				text = command.ExtractText(qm)
			}
			if p := ci.GetParticipant(); p != "" {
				if parsed, err := types.ParseJID(p); err == nil {
					targetJID = parsed
					targetName = parsed.User
				}
			}
		}
	}

	if targetName == "" {
		targetName = c.SenderPhone()
	}
	if targetName == "" {
		targetName = "WhatsApp User"
	}

	return text, targetJID, targetName
}

func fetchQuotlyPNG(ctx context.Context, text, name, avatarURL string) ([]byte, error) {
	// 1. Coba Popcat Quote API terlebih dahulu
	popcatURL := fmt.Sprintf("https://api.popcat.xyz/quote?text=%s&name=%s&image=%s",
		url.QueryEscape(text), url.QueryEscape(name), url.QueryEscape(avatarURL))

	imgData, err := httpx.GetBytes(ctx, popcatURL, 15*time.Second, 10*1024*1024)
	if err == nil && len(imgData) > 0 && (bytes.HasPrefix(imgData, []byte("\x89PNG")) || bytes.HasPrefix(imgData, []byte("RIFF"))) {
		return imgData, nil
	} else if err != nil {
		log.Printf("[rbot] qc endpoint %s error: %v", popcatURL, err)
	}

	// 2. Fallback ke endpoint Quotly JSON
	endpoints := []string{
		"https://quote.yuri.ly/generate",
		"https://bot.lyo.su/quote/generate",
		"https://bot.lyrical.tokyo/api/quote",
		"https://quote.btch.bz/generate",
		"https://qc.botcazx.my.id/generate",
		"https://quotly.maba.workers.dev/generate",
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

		// Bila endpoint mengembalikan biner gambar langsung (image/png, image/webp, dsb)
		if strings.HasPrefix(contentType, "image/") || bytes.HasPrefix(respBytes, []byte("\x89PNG")) || bytes.HasPrefix(respBytes, []byte("RIFF")) {
			return respBytes, nil
		}

		// Jika balasan berupa JSON
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

			// Bila JSON mengembalikan URL gambar
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

	return nil, errors.New("seluruh Quotly API server tidak merespon/gagal menghasilkan gambar")
}

// ExportFetchQuotlyPNG diekspor khusus untuk unit test.
func ExportFetchQuotlyPNG(ctx context.Context, text, name, avatarURL string) ([]byte, error) {
	return fetchQuotlyPNG(ctx, text, name, avatarURL)
}

