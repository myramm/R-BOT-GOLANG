package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
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
	ID    int         `json:"id"`
	Name  string      `json:"name"`
	Photo quotlyPhoto `json:"photo"`
}

type quotlyPhoto struct {
	URL string `json:"url"`
}

type quotlyMessage struct {
	Entities     []any       `json:"entities"`
	Avatar       bool        `json:"avatar"`
	From         quotlyFrom  `json:"from"`
	Text         string      `json:"text"`
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
	} `json:"result"`
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
		c.React(ctx, "❌")
		_, replyErr := c.Reply(ctx, "❌ Gagal membuat quote sticker: "+err.Error())
		return replyErr
	}

	webp, err := stickerEncode(processCtx, pngBytes, false)
	if err != nil {
		c.React(ctx, "❌")
		_, replyErr := c.Reply(ctx, "❌ Gagal membuat stiker: "+err.Error())
		return replyErr
	}

	thumbnail, thumbnailErr := stickerThumbnail(processCtx, webp)
	if thumbnailErr != nil {
		c.React(ctx, "❌")
		_, replyErr := c.Reply(ctx, "❌ Gagal membuat thumbnail stiker: "+thumbnailErr.Error())
		return replyErr
	}

	webp, err = stickerAddExifContext(processCtx, webp, config.C.Sticker.Packname, config.C.Sticker.Author)
	if err != nil {
		c.React(ctx, "❌")
		_, replyErr := c.Reply(ctx, "❌ Gagal memasang metadata stiker: "+err.Error())
		return replyErr
	}

	if err := c.SendStickerBytesWithThumbnail(ctx, webp, thumbnail); err != nil {
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
	endpoints := []string{
		"https://bot.lyrical.tokyo/api/quote",
		"https://qc.botcazx.my.id/generate",
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
					ID:    1,
					Name:  name,
					Photo: quotlyPhoto{URL: avatarURL},
				},
				Text:         text,
				ReplyMessage: map[string]any{},
			},
		},
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, err
	}

	for _, ep := range endpoints {
		resp, err := httpx.Do(ctx, http.MethodPost, ep, bytes.NewReader(bodyBytes), 20*time.Second, map[string]string{
			"Content-Type": "application/json",
		})
		if err != nil {
			continue
		}

		var qResp quotlyResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&qResp)
		_ = resp.Body.Close()

		if decodeErr != nil || qResp.Result.Image == "" {
			continue
		}

		rawImage := qResp.Result.Image
		if idx := strings.Index(rawImage, ","); idx >= 0 {
			rawImage = rawImage[idx+1:]
		}

		pngBytes, b64Err := base64.StdEncoding.DecodeString(rawImage)
		if b64Err == nil && len(pngBytes) > 0 {
			return pngBytes, nil
		}
	}

	return nil, errors.New("seluruh Quotly API server tidak merespon/gagal menghasilkan gambar")
}
