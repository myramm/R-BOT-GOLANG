// Package simi mengelola fitur Simi-Simi AI (auto-reply pesan quote & sticker)
// menggunakan Google Gemini Interactions API dan penyimpanan sticker di LMDB.
package simi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/identity"
	"rbot/brain/store"
	"rbot/lib/httpx"
)

const (
	defaultInteractionsAPI = "https://generativelanguage.googleapis.com/v1/interactions"
	defaultModel           = "gemini-3.5-flash"
	timeoutSimi            = 25 * time.Second
	cooldownDuration       = 3 * time.Second
	maxStoredStickers      = 100
	stickersStoreKey       = "simi:stickers"
	chatSettingKeyPrefix   = "simi:chat:"
)

var (
	cooldownMu   sync.Mutex
	lastUserCall = map[string]time.Time{}
)

func init() {
	command.SimiHook = HandleQuotedMessage
}

// DefaultPersonaPrompt mengembalikan system prompt untuk Simi-Simi.
func DefaultPersonaPrompt() string {
	if p := strings.TrimSpace(config.C.Simi.SystemPrompt); p != "" {
		return p
	}
	return "Kamu adalah Simi-Simi versi netizen Indonesia yang full sarkas, suka ngegoreng, nyolot, dan kocak ala netizen medsos TikTok/FB. " +
		"Gunakan bahasa gaul santai (wkwk, bjir, lu, gw, kocak lu, anjir, dll). " +
		"DILARANG menjawab secara formal, panjang, atau kaku seperti robot asisten. " +
		"Jika user menanyakan hal serius, tugas sekolah, coding, atau pencarian informasi ilmiah, tolak dan roasting mereka (suruh cari di Google sendiri). " +
		"Jawab selalu singkat, to-the-point, dan menghibur."
}

// AskSimi mengirim teks input ke Gemini Interactions API dengan persona netizen sarkas.
func AskSimi(ctx context.Context, input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", errors.New("input pesan kosong")
	}

	apiKey := strings.TrimSpace(config.C.Simi.APIKey)
	if apiKey == "" {
		return "", errors.New("simi.apiKey belum diisi di config.json")
	}

	model := strings.TrimSpace(config.C.Simi.Model)
	if model == "" {
		model = defaultModel
	}

	prompt := fmt.Sprintf("[System: %s]\n\nUser: %s", DefaultPersonaPrompt(), trimmed)
	reqBody, err := json.Marshal(map[string]any{
		"model": model,
		"input": prompt,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	headers := map[string]string{
		"Content-Type":   "application/json",
		"x-goog-api-key": apiKey,
	}

	resp, err := httpx.Do(ctx, http.MethodPost, defaultInteractionsAPI, strings.NewReader(string(reqBody)), timeoutSimi, headers)
	if err != nil {
		return "", fmt.Errorf("panggilan API simi: %w", err)
	}
	defer resp.Body.Close()

	var data struct {
		Status string `json:"status"`
		Steps  []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"steps"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("decode respon simi: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if data.Error.Message != "" {
			return "", fmt.Errorf("%s", data.Error.Message)
		}
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	for _, step := range data.Steps {
		if step.Type == "model_output" {
			for _, item := range step.Content {
				if item.Text != "" {
					return strings.TrimSpace(item.Text), nil
				}
			}
		}
	}

	return "", errors.New("tidak ada respon teks dari API Simi")
}

// HandleQuotedMessage memproses pesan masuk yang mengutip (quote) pesan bot.
// Mengembalikan true bila pesan ditangani oleh Simi.
func HandleQuotedMessage(ctx context.Context, client *whatsmeow.Client, evt *events.Message) bool {
	if evt == nil || evt.Message == nil || evt.Info.IsFromMe || client == nil {
		return false
	}

	ci := ExtractContextInfo(evt.Message)
	if ci == nil || ci.GetQuotedMessage() == nil {
		return false
	}

	if !IsQuotedBot(client, evt, ci) {
		return false
	}

	chatID := evt.Info.Chat.String()
	if !IsEnabled(chatID) {
		return false
	}

	senderID := identity.SenderPhone(evt)
	if senderID == "" {
		senderID = config.BareNumber(evt.Info.Sender.String())
	}
	if !CheckCooldown(senderID) {
		return false
	}

	// 1. Tangani bila user membalas dengan Sticker
	if evt.Message.GetStickerMessage() != nil {
		stickerData, ok := GetRandomSticker()
		if !ok {
			return false
		}
		up, err := client.Upload(ctx, stickerData, whatsmeow.MediaImage)
		if err != nil {
			return false
		}
		now := time.Now()
		stickerMsg := &waE2E.Message{
			StickerMessage: &waE2E.StickerMessage{
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
				ContextInfo: &waE2E.ContextInfo{
					StanzaID:      proto.String(evt.Info.ID),
					Participant:   proto.String(evt.Info.Sender.String()),
					QuotedMessage: evt.Message,
				},
			},
		}
		_, _ = client.SendMessage(ctx, evt.Info.Chat, stickerMsg)
		return true
	}

	// 2. Tangani bila user membalas dengan Pesan Teks
	text := command.ExtractText(evt.Message)
	if text == "" {
		return false
	}

	reply, err := AskSimi(ctx, text)
	if err != nil || strings.TrimSpace(reply) == "" {
		return false
	}

	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(reply),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:      proto.String(evt.Info.ID),
				Participant:   proto.String(evt.Info.Sender.String()),
				QuotedMessage: evt.Message,
			},
		},
	}
	_, _ = client.SendMessage(ctx, evt.Info.Chat, msg)
	return true
}

// ExtractContextInfo mengambil ContextInfo dari berbagai jenis pesan WhatsApp.
func ExtractContextInfo(m *waE2E.Message) *waE2E.ContextInfo {
	if m == nil {
		return nil
	}
	if ci := m.GetExtendedTextMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := m.GetStickerMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := m.GetImageMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := m.GetVideoMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := m.GetDocumentMessage().GetContextInfo(); ci != nil {
		return ci
	}
	return nil
}

// IsQuotedBot memeriksa apakah pesan yang dikutip berasal dari nomor bot.
func IsQuotedBot(client *whatsmeow.Client, evt *events.Message, ci *waE2E.ContextInfo) bool {
	if ci == nil || ci.GetQuotedMessage() == nil {
		return false
	}
	participant := ci.GetParticipant()
	if participant == "" {
		// Pada DM pribadi, participant kutipan kosong berarti berasal dari lawan bicara (bot)
		return !evt.Info.IsGroup
	}

	bareParticipant := config.BareNumber(participant)
	if client != nil && client.Store != nil && client.Store.ID != nil {
		if bareParticipant == config.BareNumber(client.Store.ID.User) ||
			bareParticipant == config.BareNumber(client.Store.ID.String()) {
			return true
		}
	}
	if botNum := config.BareNumber(config.C.BotNumber); botNum != "" && bareParticipant == botNum {
		return true
	}
	return false
}

// CheckCooldown memeriksa apakah user sedang dalam batas jeda (cooldown).
// Mengembalikan true bila diizinkan, false bila masih cooldown.
func CheckCooldown(senderID string) bool {
	cooldownMu.Lock()
	defer cooldownMu.Unlock()

	now := time.Now()
	// Prune memory map jika sudah lebih dari 500 entri
	if len(lastUserCall) > 500 {
		for id, t := range lastUserCall {
			if now.Sub(t) > 10*time.Minute {
				delete(lastUserCall, id)
			}
		}
	}

	last, exists := lastUserCall[senderID]
	if exists && now.Sub(last) < cooldownDuration {
		return false
	}

	lastUserCall[senderID] = now
	return true
}

// IsEnabled mengecek apakah fitur Simi aktif untuk chat tertentu.
func IsEnabled(chatID string) bool {
	var enabled bool
	found, err := store.Get(chatSettingKeyPrefix+chatID, &enabled)
	if err == nil && found {
		return enabled
	}
	return config.C.Simi.EnabledByDefault
}

// SetEnabled mengubah status Simi (ON/OFF) untuk chat tertentu di LMDB.
func SetEnabled(chatID string, enabled bool) error {
	return store.Set(chatSettingKeyPrefix+chatID, enabled)
}

// SaveGroupSticker menyimpan byte WebP sticker yang dibuat di grup ke database LMDB.
func SaveGroupSticker(data []byte) error {
	if len(data) == 0 {
		return errors.New("data sticker kosong")
	}

	var list []string
	_ = store.GetOr(stickersStoreKey, &list)

	b64 := base64.StdEncoding.EncodeToString(data)
	// Cek duplikasi dengan 10 sticker terakhir
	for i := len(list) - 1; i >= 0 && i >= len(list)-10; i-- {
		if list[i] == b64 {
			return nil // Sudah ada
		}
	}

	list = append(list, b64)
	if len(list) > maxStoredStickers {
		list = list[len(list)-maxStoredStickers:]
	}

	return store.Set(stickersStoreKey, list)
}

// GetRandomSticker mengambil 1 sticker WebP acak dari koleksi LMDB.
func GetRandomSticker() ([]byte, bool) {
	var list []string
	found, err := store.Get(stickersStoreKey, &list)
	if err != nil || !found || len(list) == 0 {
		return nil, false
	}

	idx := rand.Intn(len(list))
	data, err := base64.StdEncoding.DecodeString(list[idx])
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}
