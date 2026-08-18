// Package simi mengelola fitur Simi-Simi AI (auto-reply pesan quote & sticker)
// menggunakan Google Gemini Interactions API dan penyimpanan sticker di LMDB.
package simi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
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

const promptStoreKey = "simi:system_prompt"

// DefaultPersonaPrompt mengembalikan system prompt untuk Simi-Simi.
func DefaultPersonaPrompt() string {
	var customPrompt string
	found, err := store.Get(promptStoreKey, &customPrompt)
	if err == nil && found && strings.TrimSpace(customPrompt) != "" {
		return strings.TrimSpace(customPrompt)
	}
	if p := strings.TrimSpace(config.C.Simi.SystemPrompt); p != "" {
		return p
	}
	return "Kamu adalah Simi-Simi versi netizen TikTok/FB/IG Indonesia yang hobi ngegoreng, nyolot, ketikan pedas/sarkas, dan suka ngegas ala kolom komentar netizen medsos.\n\n" +
		"Pedoman Karakter & Gaya Ketikan:\n" +
		"- Gunakan gaya ketikan khas netizen medsos Indonesia (contoh: 'ngapain?', 'lah kocak', 'Matamu...', 'Halahh...', 'Diem lu berisik bangt', 'Kocak lu', 'Minimal ngotak lah bjir', 'GK kaget sih', 'Pencitraan doang', 'lah ... ae', 'yg bro rasakan', 'wkwk', 'bjir', 'emng', 'bangt', 'lol', '🗿', '😂', '💀').\n" +
		"- Kadang bisa sangat deadpan/cuek nyelekit (misal cuma jawab 'ngapain?', 'trus?', 'lah?').\n" +
		"- DILARANG menjawab sopan, panjang-panjang, atau formal seperti robot asisten AI.\n" +
		"- Jawab selalu ceplas-ceplos, singkat 1-2 kalimat (atau bahkan 1 frasa), nyelekit, sarkas, dan bikin ngakak.\n" +
		"- Jika user bertanya hal serius, tugas sekolah, coding, atau cari info, tolak dan roasting mereka (suruh mikir/googling sendiri)."
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

	rawMsg := unwrapMessage(evt.Message)
	if rawMsg == nil {
		return false
	}

	ci := ExtractContextInfo(rawMsg)
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
	if rawMsg.GetStickerMessage() != nil {
		// Unduh sticker user yang baru masuk dan simpan ke koleksi LMDB
		downloadCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		if downloaded, err := client.DownloadAny(downloadCtx, rawMsg); err == nil && len(downloaded) > 0 {
			_ = SaveGroupSticker(downloaded)
		}
		cancel()

		stickerData, ok := GetRandomSticker()
		if ok {
			up, err := client.Upload(ctx, stickerData, whatsmeow.MediaImage)
			if err == nil {
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
				if _, sendErr := client.SendMessage(ctx, evt.Info.Chat, stickerMsg); sendErr == nil {
					log.Printf("[rbot] [simi] balas sticker ke %s", senderID)
					return true
				}
			}
		}

		// Fallback balas teks jika upload sticker belum tersedia
		reply, err := AskSimi(ctx, "User barusan reply kamu pake stiker lucu/kocak, balas singkat dan sarkas ala netizen")
		if err == nil && reply != "" {
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
		return true
	}

	// 2. Tangani bila user membalas dengan Pesan Teks
	text := command.ExtractText(rawMsg)
	if text == "" {
		return false
	}

	// Tampilkan status "sedang mengetik..." di chat
	_ = client.SendChatPresence(ctx, evt.Info.Chat, types.ChatPresenceComposing, types.ChatPresenceMediaText)
	defer func() {
		_ = client.SendChatPresence(context.Background(), evt.Info.Chat, types.ChatPresencePaused, types.ChatPresenceMediaText)
	}()

	reply, err := AskSimi(ctx, text)
	if err != nil || strings.TrimSpace(reply) == "" {
		log.Printf("[rbot] [simi] gagal AskSimi untuk %s: %v", senderID, err)
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
	log.Printf("[rbot] [simi] balas teks ke %s: %s", senderID, reply)
	return true
}

func unwrapMessage(m *waE2E.Message) *waE2E.Message {
	for m != nil {
		switch {
		case m.GetEphemeralMessage().GetMessage() != nil:
			m = m.GetEphemeralMessage().GetMessage()
		case m.GetViewOnceMessage().GetMessage() != nil:
			m = m.GetViewOnceMessage().GetMessage()
		case m.GetViewOnceMessageV2().GetMessage() != nil:
			m = m.GetViewOnceMessageV2().GetMessage()
		case m.GetViewOnceMessageV2Extension().GetMessage() != nil:
			m = m.GetViewOnceMessageV2Extension().GetMessage()
		case m.GetDocumentWithCaptionMessage().GetMessage() != nil:
			m = m.GetDocumentWithCaptionMessage().GetMessage()
		default:
			return m
		}
	}
	return nil
}

// ExtractContextInfo mengambil ContextInfo dari berbagai jenis pesan WhatsApp.
func ExtractContextInfo(m *waE2E.Message) *waE2E.ContextInfo {
	m = unwrapMessage(m)
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
	if ci := m.GetAudioMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := m.GetDocumentMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := m.GetContactMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := m.GetLocationMessage().GetContextInfo(); ci != nil {
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
	if bareParticipant == "" {
		return false
	}

	// 1. Cek GetJID & GetLID dari store whatsmeow
	if client != nil && client.Store != nil {
		if jid := client.Store.GetJID(); !jid.IsEmpty() {
			if bareParticipant == config.BareNumber(jid.User) || bareParticipant == config.BareNumber(jid.String()) {
				return true
			}
		}
		if lid := client.Store.GetLID(); !lid.IsEmpty() {
			if bareParticipant == config.BareNumber(lid.User) || bareParticipant == config.BareNumber(lid.String()) {
				return true
			}
		}
		if client.Store.ID != nil {
			if bareParticipant == config.BareNumber(client.Store.ID.User) ||
				bareParticipant == config.BareNumber(client.Store.ID.String()) {
				return true
			}
		}
	}

	// 2. Cek config BotNumber
	if botNum := config.BareNumber(config.C.BotNumber); botNum != "" {
		if bareParticipant == botNum {
			return true
		}
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

// SetCustomPersona menyimpan kustomisasi prompt persona Simi-Simi ke database LMDB.
func SetCustomPersona(prompt string) error {
	return store.Set(promptStoreKey, strings.TrimSpace(prompt))
}

// ResetCustomPersona menghapus prompt kustom di LMDB dan kembali ke bawaan prompt netizen.
func ResetCustomPersona() error {
	return store.Delete(promptStoreKey)
}

// HasCustomPersona mengembalikan true bila terdapat prompt kustom di LMDB.
func HasCustomPersona() bool {
	var customPrompt string
	found, err := store.Get(promptStoreKey, &customPrompt)
	return err == nil && found && strings.TrimSpace(customPrompt) != ""
}

// GetAllStickers mengembalikan seluruh daftar sticker WebP (base64) dari LMDB.
func GetAllStickers() []string {
	var list []string
	_, _ = store.Get(stickersStoreKey, &list)
	return list
}

// DeleteSticker menghapus satu sticker di LMDB berdasarkan indeks.
func DeleteSticker(index int) error {
	var list []string
	found, err := store.Get(stickersStoreKey, &list)
	if err != nil || !found || len(list) == 0 {
		return errors.New("tidak ada sticker yang tersimpan")
	}
	if index < 0 || index >= len(list) {
		return fmt.Errorf("indeks sticker %d tidak valid", index)
	}
	list = append(list[:index], list[index+1:]...)
	return store.Set(stickersStoreKey, list)
}

// ClearAllStickers mengosongkan seluruh koleksi sticker di LMDB.
func ClearAllStickers() error {
	return store.Delete(stickersStoreKey)
}

// GetSimiData mengumpulkan status dan data Simi-Simi untuk tampilan Web Dashboard.
func GetSimiData() map[string]any {
	var stickers []string
	_, _ = store.Get(stickersStoreKey, &stickers)

	return map[string]any{
		"enabled_default":  config.C.Simi.EnabledByDefault,
		"model":            config.C.Simi.Model,
		"has_api_key":      strings.TrimSpace(config.C.Simi.APIKey) != "",
		"system_prompt":    DefaultPersonaPrompt(),
		"is_custom_prompt": HasCustomPersona(),
		"total_stickers":   len(stickers),
		"stickers":         stickers,
	}
}
