// Package simi mengelola fitur Simi-Simi AI (auto-reply pesan quote & sticker)
// menggunakan DeepSeek/Tokenbom API dan penyimpanan sticker di LMDB.
package simi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
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
	"rbot/lib/exif"
	"rbot/lib/httpx"
)

const (
	defaultInteractionsAPI = "https://tokenbom.com/v1/chat/completions"
	defaultModel           = "deepseek-v4-flash"
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

// ChatMessage adalah entri pesan dalam riwayat sesi percakapan Simi-Simi.
type ChatMessage struct {
	Role    string    `json:"role"`
	Content string    `json:"content"`
	Time    time.Time `json:"time"`
}

// SimiSession menyimpan riwayat percakapan aktif per sesi user/chat.
type SimiSession struct {
	ChatID     string
	SenderID   string
	LastActive time.Time
	Messages   []ChatMessage
}

var (
	sessionMu sync.Mutex
	sessions  = map[string]*SimiSession{}
)

const (
	sessionTimeout     = 5 * time.Minute
	maxHistoryMessages = 10
)

// GetSessionKey mengembalikan identifier unik untuk sesi chat.
func GetSessionKey(chatID, senderID string, isGroup bool) string {
	if isGroup {
		return chatID + ":" + senderID
	}
	return senderID
}

// HasActiveSession mengecek apakah user memiliki sesi obrolan aktif dengan Simi.
func HasActiveSession(sessionKey string) bool {
	if sessionKey == "" {
		return false
	}
	sessionMu.Lock()
	defer sessionMu.Unlock()
	s, ok := sessions[sessionKey]
	if !ok || s == nil {
		return false
	}
	if time.Since(s.LastActive) > sessionTimeout {
		delete(sessions, sessionKey)
		return false
	}
	return len(s.Messages) > 0
}

// AddMessageToSession menambahkan riwayat pesan ke sesi obrolan.
func AddMessageToSession(sessionKey, role, content string) {
	if sessionKey == "" || strings.TrimSpace(content) == "" {
		return
	}
	sessionMu.Lock()
	defer sessionMu.Unlock()

	s, ok := sessions[sessionKey]
	if !ok || s == nil || time.Since(s.LastActive) > sessionTimeout {
		s = &SimiSession{
			LastActive: time.Now(),
			Messages:   make([]ChatMessage, 0, maxHistoryMessages),
		}
		sessions[sessionKey] = s
	}

	s.LastActive = time.Now()
	s.Messages = append(s.Messages, ChatMessage{
		Role:    role,
		Content: strings.TrimSpace(content),
		Time:    time.Now(),
	})

	if len(s.Messages) > maxHistoryMessages {
		s.Messages = s.Messages[len(s.Messages)-maxHistoryMessages:]
	}
}

// ClearSession mereset riwayat sesi percakapan Simi.
func ClearSession(sessionKey string) {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	delete(sessions, sessionKey)
}

var (
	indoMonths = []string{
		"", "Januari", "Februari", "Maret", "April", "Mei", "Juni",
		"Juli", "Agustus", "September", "Oktober", "November", "Desember",
	}
	indoDays = []string{
		"Minggu", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu",
	}
)

func formatIndoTime(t time.Time) string {
	wib := t.UTC().Add(7 * time.Hour)
	dayName := indoDays[wib.Weekday()]
	monthName := indoMonths[wib.Month()]
	return fmt.Sprintf("%s, %d %s %d jam %02d:%02d WIB", dayName, wib.Day(), monthName, wib.Year(), wib.Hour(), wib.Minute())
}

// fetchRealtimeSnippet mengambil ringkasan informasi terkini dari web jika pertanyaan menyangkut hal real-time.
func fetchRealtimeSnippet(ctx context.Context, query string) string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return ""
	}

	needsSearch := false
	markers := []string{
		"sekarang", "saat ini", "hari ini", "terbaru", "terkini", "tahun ini",
		"presiden", "wapres", "menteri", "gubernur", "juara", "skor", "pertandingan",
		"jadwal", "cuaca", "berita", "gempa", "update", "viral", "pemilu", "pilkada",
	}
	for _, m := range markers {
		if strings.Contains(q, m) {
			needsSearch = true
			break
		}
	}

	if !needsSearch {
		return ""
	}

	searchCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	urlStr := "https://id.wikipedia.org/w/api.php?action=query&list=search&srsearch=" + url.QueryEscape(query) + "&utf8=1&format=json"
	req, err := http.NewRequestWithContext(searchCtx, http.MethodGet, urlStr, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "RBot/1.0 (https://github.com/myramm/R-BOT-GOLANG)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Query struct {
			Search []struct {
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
			} `json:"search"`
		} `json:"query"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || len(result.Query.Search) == 0 {
		return ""
	}

	var snippets []string
	for i, s := range result.Query.Search {
		if i >= 3 {
			break
		}
		cleanSnippet := strings.ReplaceAll(s.Snippet, "<span class=\"searchmatch\">", "")
		cleanSnippet = strings.ReplaceAll(cleanSnippet, "</span>", "")
		cleanSnippet = strings.ReplaceAll(cleanSnippet, "&quot;", "\"")
		cleanSnippet = strings.ReplaceAll(cleanSnippet, "&amp;", "&")
		cleanSnippet = strings.TrimSpace(cleanSnippet)
		if cleanSnippet != "" {
			snippets = append(snippets, fmt.Sprintf("• %s: %s", s.Title, cleanSnippet))
		}
	}

	if len(snippets) == 0 {
		return ""
	}
	return strings.Join(snippets, "\n")
}

// BuildSessionPrompt membangun prompt lengkap berisi system persona dan riwayat percakapan sebelumnya.
func BuildSessionPrompt(ctx context.Context, sessionKey, currentInput string) string {
	sessionMu.Lock()
	defer sessionMu.Unlock()

	var sb strings.Builder
	sb.WriteString("[System: ")
	sb.WriteString(DefaultPersonaPrompt())
	sb.WriteString("]")

	// Tambahkan ringkasan real-time web search jika query menanyakan peristiwa/fakta terkini
	if snippet := fetchRealtimeSnippet(ctx, currentInput); snippet != "" {
		sb.WriteString("\n\n[Informasi Real-Time Terkini dari Web:\n")
		sb.WriteString(snippet)
		sb.WriteString("\n]")
	}

	if sessionKey != "" {
		s, ok := sessions[sessionKey]
		if ok && s != nil && len(s.Messages) > 0 && time.Since(s.LastActive) < sessionTimeout {
			sb.WriteString("\n\nRiwayat Percakapan Sebelumnya:")
			for _, m := range s.Messages {
				sb.WriteString("\n")
				sb.WriteString(m.Role)
				sb.WriteString(": ")
				sb.WriteString(m.Content)
			}
		}
	}

	sb.WriteString("\n\nUser: ")
	sb.WriteString(currentInput)
	return sb.String()
}

// DefaultPersonaPrompt mengembalikan system prompt untuk Simi-Simi dengan real-time anchoring.
func DefaultPersonaPrompt() string {
	var customPrompt string
	found, err := store.Get(promptStoreKey, &customPrompt)
	if err == nil && found && strings.TrimSpace(customPrompt) != "" {
		return strings.TrimSpace(customPrompt)
	}
	if p := strings.TrimSpace(config.C.Simi.SystemPrompt); p != "" {
		return p
	}

	now := time.Now()
	currentTime := formatIndoTime(now)
	currentYear := now.UTC().Add(7 * time.Hour).Year()

	return fmt.Sprintf("Kamu adalah Simi-Simi versi netizen medsos/tongkrongan Indonesia yang julid, sarkas, pedas, ceplas-ceplos, dan ANTI YAPING.\n\n"+
		"[KONTEKS WAKTU & REAL-TIME NYATA]\n"+
		"- Waktu saat ini: %s (Tahun %d).\n"+
		"- Pemimpin Indonesia saat ini: Presiden RI adalah Jenderal TNI (Purn.) Prabowo Subianto dan Wakil Presiden adalah Gibran Rakabuming Raka (Kabinet Merah Putih, masa jabatan 2024-2029). Jangan pernah salah mengira presiden masih Jokowi atau periode lama.\n"+
		"- Selalu gunakan fakta dan konteks real-time saat menjawab pertanyaan seputar masa kini.\n\n"+
		"[ATURAN MUTLAK - ANTI YAPING & SARKASME MEMBEKAS]:\n"+
		"1. DILARANG YAPING: Jawab SUPER SINGKAT (maksimal 1 kalimat pendek atau 1 frasa tajam, sekitar 5-15 kata). Langsung to the point ke punchline!\n"+
		"2. FORMULA: Fakta Singkat + Sarkasme Pedas Membekas. Jangan menceramahi atau bertele-tele, cukup 1 pukulan telak yang bikin nyesek dan ngakak.\n"+
		"3. BAHASA & KATA GAUL: Gunakan bahasa gaul netizen Indonesia murni (contoh: 'awokawokawok', 'wkwkwk', 'bjir', 'anjir', 'kocak', 'bangt', 'emng', 'gk', 'lu', 'gw', 'najis', 'halahh', 'minimal ngotak', 'matamu', 'pencitraan', 'gaya doang', 'kang copas', 'emang bener').\n"+
		"4. EMOJI SESUAI EKSPRESI: Akhiri dengan 1-2 emoji yang pas (contoh: 🗿, 💀, 🤣, 😭😂, 🤢, 🤮).\n\n"+
		"Contoh Respon Singkat & Pedas:\n"+
		"User: TNI cina itu kuat kah\n"+
		"Simi: TNI mah Indonesia kocak, tentara China tuh PLA. Minimal ngotak lah bjir 🗿\n"+
		"User: Tanggapan lu tentang gw yg vibe coding\n"+
		"Simi: Gaya doang vibe coding, padahal cuma copas ChatGPT terus melamun kan lu? 💀\n"+
		"User: Hm\n"+
		"Simi: Ngapain hm hm emang bener kan, dasar kang copas 🤣\n"+
		"User: Siapa presiden Indonesia sekarang?\n"+
		"Simi: Prabowo Subianto lah. Keluar goa makanya, jangan scroll TikTok mulu lu kudet! 🗿",
		currentTime, currentYear)
}

// AskSimiWithSession mengirim teks input dengan konteks sesi percakapan sebelumnya dan grounding real-time.
func AskSimiWithSession(ctx context.Context, sessionKey, input string) (string, error) {
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

	prompt := BuildSessionPrompt(ctx, sessionKey, trimmed)

	reqPayload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": DefaultPersonaPrompt()},
			{"role": "user", "content": prompt},
		},
		"thinking":         map[string]string{"type": "enabled"},
		"reasoning_effort": "high",
		"stream":           false,
	}

	reqBody, err := json.Marshal(reqPayload)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + apiKey,
	}

	resp, err := httpx.Do(ctx, http.MethodPost, defaultInteractionsAPI, strings.NewReader(string(reqBody)), timeoutSimi, headers)
	if err != nil {
		return "", fmt.Errorf("panggilan API simi: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("baca respon simi: %w", err)
	}

	type errorItem struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	}
	type choiceItem struct {
		Index   int `json:"index"`
		Message struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	}
	type chatResp struct {
		ID      string       `json:"id"`
		Model   string       `json:"model"`
		Object  string       `json:"object"`
		Choices []choiceItem `json:"choices"`
		Error   *errorItem   `json:"error"`
	}

	var chat chatResp
	trimmedBody := bytes.TrimSpace(bodyBytes)
	if err := json.Unmarshal(trimmedBody, &chat); err != nil {
		return "", fmt.Errorf("decode respon simi: %w", err)
	}

	if chat.Error != nil && chat.Error.Message != "" {
		return "", fmt.Errorf("Simi API: %s", chat.Error.Message)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(trimmedBody))
	}

	if len(chat.Choices) == 0 {
		return "", errors.New("tidak ada pilihan respon dari API Simi: " + string(trimmedBody))
	}

	for _, c := range chat.Choices {
		replyText := strings.TrimSpace(c.Message.Content)
		if replyText == "" {
			continue
		}
		if sessionKey != "" {
			AddMessageToSession(sessionKey, "User", trimmed)
			AddMessageToSession(sessionKey, "Simi", replyText)
		}
		return replyText, nil
	}

	return "", errors.New("tidak ada respon teks dari API Simi: " + string(trimmedBody))
}

// AskSimi mengirim teks input ke Tokenbom/DeepSeek API dengan persona netizen sarkas (tanpa sesi).
func AskSimi(ctx context.Context, input string) (string, error) {
	return AskSimiWithSession(ctx, "", input)
}

// HandleQuotedMessage memproses pesan masuk yang mengutip (quote) pesan bot atau bagian dari sesi aktif.
// Mengembalikan true bila pesan ditangani oleh Simi.
func HandleQuotedMessage(ctx context.Context, client *whatsmeow.Client, evt *events.Message) bool {
	if evt == nil || evt.Message == nil || evt.Info.IsFromMe || client == nil {
		return false
	}

	rawMsg := unwrapMessage(evt.Message)
	if rawMsg == nil {
		return false
	}

	chatID := evt.Info.Chat.String()

	senderID := identity.SenderPhone(evt)
	if senderID == "" {
		senderID = config.BareNumber(evt.Info.Sender.String())
	}

	if !IsEnabledIn(chatID, evt.Info.IsGroup, senderID) {
		return false
	}

	sessionKey := GetSessionKey(chatID, senderID, evt.Info.IsGroup)

	ci := ExtractContextInfo(rawMsg)
	isQuotingBot := ci != nil && ci.GetQuotedMessage() != nil && IsQuotedBot(client, evt, ci)
	hasActiveSession := HasActiveSession(sessionKey)

	// Harus mengutip pesan bot ATAU memiliki sesi chat aktif yang sedang berjalan
	if !isQuotingBot && !hasActiveSession {
		return false
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
			// Tambahkan EXIF packname & author resmi bot
			packname := config.C.Sticker.Packname
			author := config.C.Sticker.Author
			if withExif, err := exif.AddStickerExif(stickerData, packname, author); err == nil {
				stickerData = withExif
			}

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

	reply, err := AskSimiWithSession(ctx, sessionKey, text)
	if err != nil || strings.TrimSpace(reply) == "" {
		log.Printf("[rbot] [simi] gagal AskSimiWithSession untuk %s: %v", senderID, err)
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

// enabledKeys mengembalikan daftar kandidat key LMDB untuk status Simi sebuah chat,
// diurutkan dari yang paling kanonik. Key kanonik bertahan lintas bentuk JID karena
// WhatsApp dapat mengirim chat sebagai @g.us/@s.whatsapp.net maupun @lid (migrasi LID).
func enabledKeys(chatID string, isGroup bool, senderPhone string) []string {
	keys := make([]string, 0, 4)
	add := func(k string) {
		if k == "" {
			return
		}
		for _, existing := range keys {
			if existing == k {
				return
			}
		}
		keys = append(keys, k)
	}

	num := config.BareNumber(chatID)
	domain := ""
	if i := strings.Index(chatID, "@"); i >= 0 {
		domain = chatID[i+1:]
	}

	switch {
	case isGroup:
		// Grup: ID numerik grup sama antara @g.us dan @lid.
		add(chatSettingKeyPrefix + "grp:" + num)
	case senderPhone != "":
		// DM: ID numerik @lid BERBEDA dari nomor HP, jadi kanonikkan via nomor pengirim.
		add(chatSettingKeyPrefix + "dm:" + senderPhone)
	case domain == "s.whatsapp.net" || domain == "":
		add(chatSettingKeyPrefix + "dm:" + num)
	}

	// Key eksak bentuk saat ini (kompatibilitas versi lama).
	add(chatSettingKeyPrefix + chatID)

	if num != "" && domain != "" {
		// Varian lintas-domain yang mungkin ditulis versi lama.
		if isGroup || domain == "g.us" || domain == "lid" {
			for _, d := range []string{"g.us", "lid"} {
				if d != domain {
					add(chatSettingKeyPrefix + num + "@" + d)
				}
			}
		}
		// Legacy DM: versi lama menulis key eksak bentuk PN.
		if !isGroup && senderPhone != "" && domain == "lid" {
			add(chatSettingKeyPrefix + senderPhone + "@s.whatsapp.net")
		}
	}

	return keys
}

// IsEnabledIn mengecek apakah fitur Simi aktif untuk chat tertentu,
// toleran terhadap perubahan bentuk JID (migrasi LID WhatsApp).
func IsEnabledIn(chatID string, isGroup bool, senderPhone string) bool {
	for _, key := range enabledKeys(chatID, isGroup, senderPhone) {
		var enabled bool
		found, err := store.Get(key, &enabled)
		if err == nil && found {
			return enabled
		}
	}
	return config.C.Simi.EnabledByDefault
}

// SetEnabledIn mengubah status Simi (ON/OFF) untuk chat tertentu di LMDB.
// Nilai ditulis ke semua varian key agar status konsisten walau bentuk JID chat berubah.
func SetEnabledIn(chatID string, isGroup bool, senderPhone string, enabled bool) error {
	var firstErr error
	for _, key := range enabledKeys(chatID, isGroup, senderPhone) {
		if err := store.Set(key, enabled); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// IsEnabled mengecek apakah fitur Simi aktif untuk chat tertentu.
func IsEnabled(chatID string) bool {
	return IsEnabledIn(chatID, strings.HasSuffix(chatID, "@g.us"), "")
}

// SetEnabled mengubah status Simi (ON/OFF) untuk chat tertentu di LMDB.
func SetEnabled(chatID string, enabled bool) error {
	return SetEnabledIn(chatID, strings.HasSuffix(chatID, "@g.us"), "", enabled)
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
