package simi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
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
	"rbot/lib/exif"
	"rbot/lib/httpx"
)

const postMethod = http.MethodPost

// actionableCommand mendeskripsikan command yang bisa dipanggil Simi via intent detection.
// Kita whitelist manual + validasi permission saat runtime.
type actionableCommand struct {
	Name        string   // nama command di registry
	Aliases     []string // alias resmi yang dikenali LLM
	Description string   // deskripsi singkat untuk prompt LLM
	ArgsHint    string   // contoh argumen
	OwnerOnly   bool     // hanya owner bot
	AdminOnly   bool     // hanya admin grup (di grup) / owner
}

// Commands yang bisa di-trigger Simi. TIDAK termasuk command OwnerOnly & admin-only berbahaya.
// Dibatasi sesuai prinsip least-privilege.
var simiActionableCommands = []actionableCommand{
	{
		Name:        "play",
		Aliases:     []string{"putar", "putar-lagu", "song", "lagu", "music"},
		Description: "Cari & kirim audio (lagu) dari YouTube berdasarkan judul/kata kunci.",
		ArgsHint:    `{"query": "judul lagu atau artis"}`,
	},
	{
		Name:        "sticker",
		Aliases:     []string{"s", "stiker", "stikerin"},
		Description: "Convert gambar/video/sticker (yang di-reply atau di-quote) menjadi sticker WebP. Juga bisa dari URL gambar.",
		ArgsHint:    `{"url": "opsional URL gambar, kalau tidak ada pakai gambar yang di-reply"}`,
	},
	{
		Name:        "toimg",
		Aliases:     []string{"toimage", "img"},
		Description: "Convert sticker WebP yang di-reply menjadi gambar (PNG/JPEG).",
		ArgsHint:    `{} (pakai sticker yang di-reply)`,
	},
	{
		Name:        "hd",
		Aliases:     []string{"upscale", "hdr"},
		Description: "Upscale gambar yang di-reply jadi HD (resolusi lebih tinggi).",
		ArgsHint:    `{} (pakai gambar yang di-reply)`,
	},
	{
		Name:        "smooth",
		Aliases:     []string{"smoothen", "smoothvideo"},
		Description: "Process video yang di-reply dengan frame interpolation (smooth).",
		ArgsHint:    `{} (pakai video yang di-reply)`,
	},
	{
		Name:        "download",
		Aliases:     []string{"dl"},
		Description: "Download video dari URL TikTok/Instagram/YouTube/Facebook/X/Threads/Pinterest/Spotify. Mendukung deteksi otomatis platform.",
		ArgsHint:    `{"url": "https://..."}`,
	},
	{
		Name:        "tiktok",
		Aliases:     []string{"tt", "tiktokdl"},
		Description: "Download video TikTok (no watermark) dari URL TikTok.",
		ArgsHint:    `{"url": "https://www.tiktok.com/..."}`,
	},
	{
		Name:        "ytmp3",
		Aliases:     []string{"mp3", "yta"},
		Description: "Download audio MP3 dari URL YouTube.",
		ArgsHint:    `{"url": "https://youtu.be/..."}`,
	},
	{
		Name:        "ytmp4",
		Aliases:     []string{"mp4", "ytv"},
		Description: "Download video MP4 dari URL YouTube.",
		ArgsHint:    `{"url": "https://youtu.be/..."}`,
	},
	{
		Name:        "qc",
		Aliases:     []string{"quotly", "fakequote"},
		Description: "Buat sticker/quote dari teks yang diberikan.",
		ArgsHint:    `{"text": "isi pesan untuk di-quote"}`,
	},
	{
		Name:        "meme",
		Aliases:     []string{"memes", "memegen"},
		Description: "Buat meme dari gambar yang di-reply dengan teks atas & bawah.",
		ArgsHint:    `{"top": "teks atas", "bottom": "teks bawah"}`,
	},
	{
		Name:        "pixiv",
		Aliases:     []string{"pix"},
		Description: "Cari & kirim ilustrasi dari Pixiv berdasarkan kata kunci atau URL.",
		ArgsHint:    `{"query": "kata kunci atau URL pixiv"}`,
	},
}

// commandAliasMap memetakan alias → command name untuk lookup cepat.
var commandAliasMap = map[string]string{}

func init() {
	for _, ac := range simiActionableCommands {
		commandAliasMap[strings.ToLower(ac.Name)] = ac.Name
		for _, a := range ac.Aliases {
			commandAliasMap[strings.ToLower(a)] = ac.Name
		}
	}
}

// actionPlan adalah JSON yang dikembalikan LLM untuk mendeskripsikan intent user.
type actionPlan struct {
	Action   string                 `json:"action"`           // nama command atau "none"
	Args     map[string]interface{} `json:"args,omitempty"`   // argumen untuk command
	Reply    string                 `json:"reply,omitempty"`  // pesan singkat untuk user (sarkas netizen)
	Reason   string                 `json:"reason,omitempty"` // internal reasoning
	Confidence float64              `json:"confidence,omitempty"`
}

// buildActionPrompt menyusun prompt untuk minta LLM mendeteksi intent & menentukan action.
// Output diharapkan JSON terstruktur.
func buildActionPrompt(userText, currentAction string) string {
	var sb strings.Builder
	sb.WriteString("[TUGAS: Deteksi Intent User untuk Simi Action System]\n\n")
	sb.WriteString("Kamu adalah router intent untuk bot WhatsApp. ")
	sb.WriteString("Diberikan pesan user dalam bahasa Indonesia/gaul/netizen, ")
	sb.WriteString("tentukan apakah user ingin memicu AKSI dari daftar command di bawah, ")
	sb.WriteString("atau hanya ngobrol biasa / minta Simi-Simi menjawab pakai persona sarkasnya.\n\n")

	sb.WriteString("ATURAN KERAS:\n")
	sb.WriteString("1. Jika pesan adalah pertanyaan/curhat/ngobrol biasa, balas dengan action=\"none\" dan isi field reply dengan jawaban sarkas singkat ala netizen.\n")
	sb.WriteString("2. Jika user EKSPLISIT minta sesuatu yang COCOK dengan salah satu command di bawah (misal: 'putar lagu X', 'stikerin', 'download tiktok ini'), ")
	sb.WriteString("maka set action=nama_command, isi args sesuai ArgsHint, dan reply boleh kosong atau singkat.\n")
	sb.WriteString("3. Field reply SELALU dalam bahasa gaul netizen Indonesia yang sarkas & pedas (1 kalimat pendek).\n")
	sb.WriteString("4. Output HARUS JSON valid, tanpa markdown, tanpa teks lain. Hanya JSON.\n")
	sb.WriteString("5. Field confidence: 0.0–1.0. Set rendah (<0.6) kalau ragu, set tinggi (>0.85) kalau sangat yakin.\n\n")

	sb.WriteString("DAFTAR ACTION YANG TERSEDIA:\n")
	for _, ac := range simiActionableCommands {
		fmt.Fprintf(&sb, "- %s: %s\n  ArgsHint: %s\n", ac.Name, ac.Description, ac.ArgsHint)
	}
	sb.WriteString("\n")

	if currentAction != "" {
		fmt.Fprintf(&sb, "[INFO: Action terakhir user = %s — boleh lanjutkan dengan action sama jika masih relevan.]\n\n", currentAction)
	}

	sb.WriteString("USER MESSAGE:\n")
	sb.WriteString(userText)
	sb.WriteString("\n\n")
	sb.WriteString("OUTPUT JSON (tanpa markdown, tanpa teks lain):\n")
	return sb.String()
}

// jsonExtract mencari blok JSON dalam string (kalau LLM bungkus dengan ```json ... ```).
var jsonBlockRe = regexp.MustCompile(`(?s)\{.*\}`)

func parseActionPlan(raw string) (*actionPlan, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("respon LLM kosong")
	}
	match := jsonBlockRe.FindString(raw)
	if match == "" {
		return nil, fmt.Errorf("tidak ada JSON dalam respon LLM")
	}
	var p actionPlan
	if err := json.Unmarshal([]byte(match), &p); err != nil {
		return nil, fmt.Errorf("JSON parse gagal: %w", err)
	}
	p.Action = strings.ToLower(strings.TrimSpace(p.Action))
	return &p, nil
}

// detectIntent memanggil LLM untuk menentukan intent & action plan.
func detectIntent(ctx context.Context, userText, lastAction string) (*actionPlan, error) {
	apiKey := strings.TrimSpace(config.C.Simi.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("simi.apiKey kosong")
	}
	model := strings.TrimSpace(config.C.Simi.Model)
	if model == "" {
		model = defaultModel
	}

	prompt := buildActionPrompt(userText, lastAction)

	reqPayload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "Kamu adalah router intent JSON. Selalu balas dengan JSON valid saja, tanpa markdown, tanpa penjelasan tambahan.",
			},
			{"role": "user", "content": prompt},
		},
		"thinking":         map[string]string{"type": "enabled"},
		"reasoning_effort": "high",
		"stream":           false,
	}

	reqBody, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + apiKey,
	}

	resp, err := httpx.Do(ctx, postMethod, defaultInteractionsAPI, strings.NewReader(string(reqBody)), timeoutSimi, headers)
	if err != nil {
		return nil, fmt.Errorf("HTTP: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var chat struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&chat); err != nil {
		return nil, fmt.Errorf("decode respon: %w", err)
	}
	if chat.Error != nil {
		return nil, fmt.Errorf("API: %s", chat.Error.Message)
	}
	if len(chat.Choices) == 0 {
		return nil, fmt.Errorf("tidak ada pilihan dalam respon")
	}

	return parseActionPlan(chat.Choices[0].Message.Content)
}

// resolveCommandName mengubah alias / nama command ke nama command yang valid di registry.
func resolveCommandName(nameOrAlias string) (string, bool) {
	k := strings.ToLower(strings.TrimSpace(nameOrAlias))
	if k == "" {
		return "", false
	}
	if canonical, ok := commandAliasMap[k]; ok {
		return canonical, true
	}
	return "", false
}

// executeAction memanggil command handler dengan Ctx yang dibuat sedekat mungkin dengan aslinya.
// Menggunakan event tiruan. Validasi permission dilakukan manual (owner/admin).
func executeAction(ctx context.Context, client *whatsmeow.Client, evt *events.Message, plan *actionPlan) error {
	canonical, ok := resolveCommandName(plan.Action)
	if !ok {
		return fmt.Errorf("action %q tidak dikenali atau tidak termasuk daftar Simi", plan.Action)
	}
	cmd := command.Resolve(canonical)
	if cmd == nil || cmd.Handler == nil {
		return fmt.Errorf("command %q tidak ditemukan di registry", canonical)
	}

	// Validasi permission: kalau OwnerOnly, tolak (kecuali caller adalah owner).
	ac, _ := findActionable(canonical)
	if ac != nil {
		if ac.OwnerOnly && !command.IsOwner(evt) {
			return fmt.Errorf("command %q hanya untuk owner", canonical)
		}
		if ac.AdminOnly {
			isAdmin := false
			if evt.Info.IsGroup && command.IsGroupAdminHook != nil {
				isAdmin = command.IsGroupAdminHook(ctx, client, evt)
			}
			if !isAdmin && !command.IsOwner(evt) {
				return fmt.Errorf("command %q butuh admin grup", canonical)
			}
		}
	}

	// Bangun slice Args dari plan.Args.
	// Konvensi: simpan JSON asli sebagai argumen pertama (supaya handler yang butuh bisa parse),
	// plus kalau ada field "query" / "url" / "text", tambahkan sebagai argumen tail.
	args := buildArgsFromPlan(plan.Args, evt)
	invokedText := "." + canonical + " " + strings.Join(args, " ")

	fakeEvt := cloneEventForSimi(evt)
	c := &command.Ctx{
		Client:    client,
		Evt:       fakeEvt,
		Args:      args,
		Text:      invokedText,
		InvokedAs: canonical,
		SubBot:    false,
	}

	// Jalankan handler. Error ditahan agar caller bisa reply.
	return cmd.Handler(ctx, c)
}

func findActionable(name string) (*actionableCommand, bool) {
	for i, ac := range simiActionableCommands {
		if ac.Name == name {
			return &simiActionableCommands[i], true
		}
	}
	return nil, false
}

func buildArgsFromPlan(args map[string]interface{}, evt *events.Message) []string {
	if len(args) == 0 {
		return nil
	}
	// Simpan JSON sebagai elemen pertama untuk handler yang mau parse sendiri.
	raw, _ := json.Marshal(args)

	out := []string{string(raw)}

	// Tambahkan field umum yang sering dipakai command.
	if v, ok := args["query"].(string); ok && strings.TrimSpace(v) != "" {
		out = append(out, v)
	}
	if v, ok := args["url"].(string); ok && strings.TrimSpace(v) != "" {
		out = append(out, v)
	}
	if v, ok := args["text"].(string); ok && strings.TrimSpace(v) != "" {
		out = append(out, v)
	}
	if v, ok := args["top"].(string); ok && strings.TrimSpace(v) != "" {
		out = append(out, v)
	}
	if v, ok := args["bottom"].(string); ok && strings.TrimSpace(v) != "" {
		out = append(out, v)
	}
	_ = evt
	return out
}

// cloneEventForSimi membuat salinan events.Message yang aman dipakai handler lain
// tanpa mengubah event asli. Aspek yang penting: Info & pengirim tetap asli.
func cloneEventForSimi(evt *events.Message) *events.Message {
	cp := *evt
	// Message tidak perlu dimutasi; beberapa handler memang clone sendiri kalau butuh.
	// Tapi amankan dari double-send dengan menyalin Message pointer.
	if evt.Message != nil {
		m := *evt.Message
		cp.Message = &m
	}
	return &cp
}

// extractURLFromText mengambil URL pertama yang muncul di teks (untuk fallback).
var urlRe = regexp.MustCompile(`https?://[^\s]+`)

func extractURLFromText(s string) string {
	m := urlRe.FindString(s)
	return strings.TrimRight(m, ",.;)]}>\"'")
}

// handleWithActions adalah entry point baru yang menggantikan HandleQuotedMessage
// ketika mode "actions" aktif. Mempertahankan perilaku Simi-Simi biasa (sarkas chat),
// TETAPI mencoba mendeteksi intent & menjalankan command yang relevan.
//
// Flow:
//  1. Panggil LLM untuk deteksi intent (action + args + reply).
//  2. Kalau action valid → kirim ack → eksekusi command handler.
//  3. Kalau action "none" / gagal → fallback ke persona sarkas biasa.
//
// Catatan: real-time snippet (Wikipedia) tidak dipakai di sini karena prompt Simi action
// sudah mandiri dan biaya token harus dijaga.
func handleWithActions(ctx context.Context, client *whatsmeow.Client, evt *events.Message) bool {
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

	if !isQuotingBot && !hasActiveSession {
		return false
	}
	if !CheckCooldown(senderID) {
		return false
	}

	// Sticker reply: pertahankan perilaku lama (balas stiker acak / fallback sarkas)
	if rawMsg.GetStickerMessage() != nil {
		// Unduh sticker user yang baru masuk dan simpan ke koleksi LMDB
		downloadCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		if downloaded, err := client.DownloadAny(downloadCtx, rawMsg); err == nil && len(downloaded) > 0 {
			_ = SaveGroupSticker(downloaded)
		}
		cancel()

		if stickerData, ok := GetRandomSticker(); ok {
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
		// Fallback teks
		reply, err := AskSimi(ctx, "User barusan reply kamu pake stiker lucu/kocak, balas singkat dan sarkas ala netizen")
		if err == nil && reply != "" {
			sendSimiTextReply(ctx, client, evt, reply)
			return true
		}
		return true
	}

	// Text reply: deteksi intent & eksekusi
	text := command.ExtractText(rawMsg)
	if text == "" {
		return false
	}

	_ = client.SendChatPresence(ctx, evt.Info.Chat, types.ChatPresenceComposing, types.ChatPresenceMediaText)
	defer func() {
		_ = client.SendChatPresence(context.Background(), evt.Info.Chat, types.ChatPresencePaused, types.ChatPresenceMediaText)
	}()

	lastAction := lastSimiAction(sessionKey)
	plan, err := detectIntent(ctx, text, lastAction)
	if err != nil {
		log.Printf("[rbot] [simi] detectIntent gagal: %v — fallback ke persona biasa", err)
		// Fallback ke persona sarkas biasa
		reply, askErr := AskSimiWithSession(ctx, sessionKey, text)
		if askErr != nil || strings.TrimSpace(reply) == "" {
			return false
		}
		sendSimiTextReply(ctx, client, evt, reply)
		return true
	}

	// Update history sesi (seperti biasa) untuk konteks percakapan
	AddMessageToSession(sessionKey, "User", text)

	// Path 1: LLM bilang ini action nyata → eksekusi command
	if plan.Action != "" && plan.Action != "none" {
		canonical, ok := resolveCommandName(plan.Action)
		if !ok {
			// Action tak dikenal → fallback ke reply sarkas dari LLM
			if plan.Reply != "" {
				AddMessageToSession(sessionKey, "Simi", plan.Reply)
				sendSimiTextReply(ctx, client, evt, plan.Reply)
				return true
			}
			return false
		}
		setLastSimiAction(sessionKey, canonical)

		// Eksekusi command handler di background supaya tidak block event loop
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if execErr := executeAction(bgCtx, client, evt, plan); execErr != nil {
				log.Printf("[rbot] [simi] executeAction %s gagal: %v", canonical, execErr)
				errReply := fmt.Sprintf("⚠️ Gagal eksekusi %s: %s", canonical, execErr.Error())
				// Pakai SendMessage langsung (evt lama tidak valid di goroutine lain)
				_, _ = client.SendMessage(context.Background(), evt.Info.Chat, &waE2E.Message{
					ExtendedTextMessage: &waE2E.ExtendedTextMessage{
						Text: proto.String(errReply),
					},
				})
			}
		}()
		return true
	}

	// Path 2: Ngobrol biasa / sarkas chat (mode Simi-Simi original)
	if plan.Reply != "" {
		AddMessageToSession(sessionKey, "Simi", plan.Reply)
		sendSimiTextReply(ctx, client, evt, plan.Reply)
		return true
	}
	return false
}

func sendSimiTextReply(ctx context.Context, client *whatsmeow.Client, evt *events.Message, reply string) {
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
}

// lastSimiAction & setLastSimiAction menyimpan action terakhir per sesi
// supaya LLM bisa melanjutkan multi-turn task (misal: pilih hasil play).
func lastSimiAction(sessionKey string) string {
	if v, ok := lastActionStore.Load(sessionKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func setLastSimiAction(sessionKey, action string) {
	lastActionStore.Store(sessionKey, action)
}

// lastActionStore adalah sync.Map untuk tracking action terakhir per sesi.
var lastActionStore sync.Map
