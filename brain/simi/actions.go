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

type actionableCommand struct {
	Name        string
	Aliases     []string
	Description string
	ArgsHint    string
	OwnerOnly   bool
	AdminOnly   bool
}

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
	{
		Name:        "anime",
		Aliases:     []string{"samehadaku"},
		Description: "Cari episode anime berdasarkan judul (mis. 'Naruto eps 12'). Download link video.",
		ArgsHint:    `{"query": "judul anime + episode opsional"}`,
	},
	{
		Name:        "hentai",
		Aliases:     []string{"minioppai"},
		Description: "Cari & download video hentai dari MiniOppai berdasarkan judul atau kode episode.",
		ArgsHint:    `{"query": "judul atau kode episode hentai"}`,
	},
}

var commandAliasMap = map[string]string{}

func init() {
	for _, ac := range simiActionableCommands {
		commandAliasMap[strings.ToLower(ac.Name)] = ac.Name
		for _, a := range ac.Aliases {
			commandAliasMap[strings.ToLower(a)] = ac.Name
		}
	}
}

type actionPlan struct {
	Action     string                 `json:"action"`
	Args       map[string]interface{} `json:"args,omitempty"`
	Reply      string                 `json:"reply,omitempty"`
	Reason     string                 `json:"reason,omitempty"`
	Confidence float64                `json:"confidence,omitempty"`
}

func buildActionPrompt(userText, currentAction string) string {
	var sb strings.Builder
	sb.WriteString("[TUGAS: Deteksi Intent User untuk Simi Action System]\n\n")
	sb.WriteString("Kamu router intent untuk bot WhatsApp. Diberikan pesan user dalam bahasa Indonesia/gaul/netizen, ")
	sb.WriteString("tentukan apakah user ingin memicu AKSI dari daftar command di bawah, ")
	sb.WriteString("atau hanya ngobrol biasa.\n\n")

	sb.WriteString("ATURAN KERAS:\n")
	sb.WriteString("1. Jika pesan pertanyaan/curhat/ngobrol biasa → action=\"none\", reply = jawaban sarkas singkat ala netizen.\n")
	sb.WriteString("2. Jika user EKSPLISIT minta sesuatu yang COCOK dengan salah satu command → action=nama_command, isi args.\n")
	sb.WriteString("3. PENTING: field query/url/text HARUS berisi KATA KUNCI/URL/TEXT INTI, BUKAN kata perintah seperti 'Carikan', 'Cari', 'Putar', 'Stikerin', 'Bikin', 'Buatkan'.\n")
	sb.WriteString("   Contoh BENAR: user='Carikan foto megumi' → args.query='megumi' (BUKAN 'Carikan').\n")
	sb.WriteString("   Contoh BENAR: user='Putar in musik bye' → args.query='bye'.\n")
	sb.WriteString("   Contoh BENAR: user='Stikerin ini' (reply gambar) → action=sticker, args={} (kosong, pakai yang di-reply).\n")
	sb.WriteString("4. Untuk sticker/toimg/hd/smooth/meme: jika user reply pesan berisi media, args={} (kosong). Jika user kasih URL, args.url=URL.\n")
	sb.WriteString("5. Untuk play/pixiv/anime/hentai: SELALU isi args.query dengan KATA KUNCI INTI saja.\n")
	sb.WriteString("6. Untuk download/tiktok/ytmp3/ytmp4/pin: SELALU isi args.url dengan URL lengkap.\n")
	sb.WriteString("7. Field reply: bahasa gaul netizen Indonesia sarkas (1 kalimat pendek). Boleh kosong untuk action nyata.\n")
	sb.WriteString("8. Output HARUS JSON valid, tanpa markdown.\n\n")

	sb.WriteString("DAFTAR ACTION:\n")
	for _, ac := range simiActionableCommands {
		fmt.Fprintf(&sb, "- %s: %s\n  ArgsHint: %s\n", ac.Name, ac.Description, ac.ArgsHint)
	}
	sb.WriteString("\n")

	if currentAction != "" {
		fmt.Fprintf(&sb, "[INFO: Action terakhir = %s]\n\n", currentAction)
	}

	sb.WriteString("USER MESSAGE:\n")
	sb.WriteString(userText)
	sb.WriteString("\n\nOUTPUT JSON:\n")
	return sb.String()
}

var jsonBlockRe = regexp.MustCompile(`(?s)\{.*\}`)

func parseActionPlan(raw string) (*actionPlan, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("respon kosong")
	}
	match := jsonBlockRe.FindString(raw)
	if match == "" {
		return nil, fmt.Errorf("tidak ada JSON")
	}
	var p actionPlan
	if err := json.Unmarshal([]byte(match), &p); err != nil {
		return nil, fmt.Errorf("parse gagal: %w", err)
	}
	p.Action = strings.ToLower(strings.TrimSpace(p.Action))
	return &p, nil
}

func detectIntent(ctx context.Context, userText, lastAction string) (*actionPlan, error) {
	apiKey := strings.TrimSpace(config.C.Simi.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("apiKey kosong")
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
				"content": "Kamu router intent JSON. Selalu balas JSON valid saja, tanpa markdown.",
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
		return nil, fmt.Errorf("decode: %w", err)
	}
	if chat.Error != nil {
		return nil, fmt.Errorf("API: %s", chat.Error.Message)
	}
	if len(chat.Choices) == 0 {
		return nil, fmt.Errorf("tidak ada pilihan")
	}

	return parseActionPlan(chat.Choices[0].Message.Content)
}

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

func mustParseJID(jidStr string) types.JID {
	jid, err := types.ParseJID(jidStr)
	if err != nil {
		return types.JID{User: jidStr}
	}
	return jid
}

func buildSimiArgs(args map[string]interface{}) []string {
	if len(args) == 0 {
		return nil
	}
	raw, _ := json.Marshal(args)
	out := []string{string(raw)}
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
	return out
}

func executeActionCore(ctx context.Context, client *whatsmeow.Client, canonical string, args []string, chatID, senderID string, quotedMsg *waE2E.Message) error {
	cmd := command.Resolve(canonical)
	if cmd == nil || cmd.Handler == nil {
		return fmt.Errorf("command %q tidak ditemukan", canonical)
	}

	fakeEvt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:    mustParseJID(chatID),
				Sender:  mustParseJID(senderID),
				IsGroup: strings.Contains(chatID, "@g.us"),
			},
			PushName:  "",
			Timestamp: time.Now(),
		},
		Message: quotedMsg,
	}

	c := &command.Ctx{
		Client:    client,
		Evt:       fakeEvt,
		Args:      args,
		Text:      "." + canonical + " " + strings.Join(args, " "),
		InvokedAs: canonical,
		SubBot:    false,
	}

	return cmd.Handler(ctx, c)
}

var lastActionStore sync.Map

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

var urlRe = regexp.MustCompile(`https?://[^\s]+`)

func extractURLFromText(s string) string {
	m := urlRe.FindString(s)
	return strings.TrimRight(m, ",.;)]}>\"'")
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

func isOwnerOrAdminAction(evt *events.Message) bool {
	if command.IsOwner(evt) {
		return true
	}
	if evt.Info.IsGroup && command.IsGroupAdminHook != nil {
		return command.IsGroupAdminHook(context.Background(), nil, evt)
	}
	return false
}

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

	if rawMsg.GetStickerMessage() != nil {
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
		reply, err := AskSimi(ctx, "User barusan reply kamu pake stiker lucu/kocak, balas singkat dan sarkas ala netizen")
		if err == nil && reply != "" {
			sendSimiTextReply(ctx, client, evt, reply)
			return true
		}
		return true
	}

	text := command.ExtractText(rawMsg)
	if text == "" {
		return false
	}

	lastAction := lastSimiAction(sessionKey)
	plan, err := detectIntent(ctx, text, lastAction)
	if err != nil {
		log.Printf("[rbot] [simi] detectIntent gagal: %v — fallback ke persona biasa", err)
		reply, askErr := AskSimiWithSession(ctx, sessionKey, text)
		if askErr != nil || strings.TrimSpace(reply) == "" {
			return false
		}
		sendSimiTextReply(ctx, client, evt, reply)
		return true
	}

	AddMessageToSession(sessionKey, "User", text)

	if plan.Action != "" && plan.Action != "none" {
		canonical, ok := resolveCommandName(plan.Action)
		if !ok {
			if plan.Reply != "" {
				AddMessageToSession(sessionKey, "Simi", plan.Reply)
				sendSimiTextReply(ctx, client, evt, plan.Reply)
				return true
			}
			return false
		}

		ac, _ := findActionable(canonical)
		if ac != nil && ac.OwnerOnly && !command.IsOwner(evt) {
			if plan.Reply != "" {
				sendSimiTextReply(ctx, client, evt, plan.Reply)
			}
			return true
		}
		if ac != nil && ac.AdminOnly && !isOwnerOrAdminAction(evt) {
			if plan.Reply != "" {
				sendSimiTextReply(ctx, client, evt, plan.Reply)
			}
			return true
		}

		setLastSimiAction(sessionKey, canonical)
		args := buildSimiArgs(plan.Args)
		chatID := evt.Info.Chat.String()
		senderID := evt.Info.Sender.String()
		var quotedMsg *waE2E.Message = evt.Message

		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[rbot] [simi] executeAction %s panic: %v", canonical, r)
				}
			}()
			bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if execErr := executeActionCore(bgCtx, client, canonical, args, chatID, senderID, quotedMsg); execErr != nil {
				log.Printf("[rbot] [simi] executeAction %s gagal: %v", canonical, execErr)
			}
		}()
		return true
	}

	if plan.Reply != "" {
		AddMessageToSession(sessionKey, "Simi", plan.Reply)
		sendSimiTextReply(ctx, client, evt, plan.Reply)
		return true
	}
	return false
}

func findActionable(name string) (*actionableCommand, bool) {
	for i, ac := range simiActionableCommands {
		if ac.Name == name {
			return &simiActionableCommands[i], true
		}
	}
	return nil, false
}
