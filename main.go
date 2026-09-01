package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/proto/waWa6"
	wastore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/errortracker"
	"rbot/brain/goodbye"
	"rbot/brain/jadibot"
	"rbot/brain/lifecycle"
	"rbot/brain/settings"
	"rbot/brain/simi"
	"rbot/brain/stats"
	"rbot/brain/store"
	"rbot/brain/web"
	"rbot/brain/welcome"
	_ "rbot/cmd"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		log.Fatalf("[rbot] fatal: %v", err)
	}

	// Bila command restart memicu lifecycle.Request, run() sudah kembali dengan
	// rapi (semua defer jalan: sesi & store tertutup). Aman meng-exec ulang biner
	// sekarang — proses baru akan membaca penanda & mengabari chat "sudah online".
	if lifecycle.Requested() {
		exe, err := os.Executable()
		if err != nil {
			log.Fatalf("[rbot] restart: gagal cari path biner: %v", err)
		}
		// Setelah .update mem-`go build` biner baru di path yang sama, kernel
		// menandai /proc/self/exe lama sebagai "<path> (deleted)". Pangkas suffix
		// itu agar syscall.Exec menuju biner yang baru, bukan path tak valid.
		exe = strings.TrimSuffix(exe, " (deleted)")
		// Reset sesi dari Web Dashboard: hapus DB sesi WhatsApp sebelum re-exec
		// agar proses baru otomatis meminta kode pairing baru (tanpa SSH ke VPS).
		if lifecycle.TakePendingSessionReset() {
			removeWhatsAppSessionFiles()
		}
		log.Printf("[rbot] restart: re-exec %s", exe)
		if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
			log.Fatalf("[rbot] restart: exec gagal: %v", err)
		}
	}
}

func run(ctx context.Context) error {
	if err := config.Load("config.json"); err != nil {
		return fmt.Errorf("muat config: %w", err)
	}
	if strings.TrimSpace(config.C.AI.APIKey) == "" {
		return fmt.Errorf("ai.apiKey kosong di config.json; isi API key OpenRouter sebelum menjalankan bot")
	}
	if err := os.MkdirAll("data", 0o700); err != nil {
		return fmt.Errorf("buat direktori data: %w", err)
	}
	if err := os.MkdirAll("session", 0o700); err != nil {
		return fmt.Errorf("buat direktori session: %w", err)
	}
	if err := store.Open("data"); err != nil {
		return fmt.Errorf("buka store aplikasi: %w", err)
	}
	defer func() {
		stats.Flush()
		if err := store.Close(); err != nil {
			log.Printf("[rbot] gagal menutup store: %v", err)
		}
	}()
	if err := settings.Load(); err != nil {
		return fmt.Errorf("muat settings: %w", err)
	}
	_ = errortracker.Load()

	web.Start(ctx)

	level := strings.ToUpper(config.C.LogLevel)
	if level == "" || level == "SILENT" {
		level = "ERROR"
	}
	dbLog := waLog.Stdout("Database", level, true)
	dsn := "file:" + filepath.ToSlash(filepath.Join("session", "store.db")) + "?_foreign_keys=on"
	container, err := sqlstore.New(ctx, "sqlite3", dsn, dbLog)
	if err != nil {
		return fmt.Errorf("buka store sesi WhatsApp: %w", err)
	}
	defer func() {
		if err := container.Close(); err != nil {
			log.Printf("[rbot] gagal menutup store sesi: %v", err)
		}
	}()

	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("ambil device WhatsApp: %w", err)
	}
	clientLog := waLog.Stdout("Client", level, true)
	client := whatsmeow.NewClient(device, clientLog)
	client.UserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	client.WebSocketHeaders = http.Header{
		"Sec-Fetch-Dest":  {"websocket"},
		"Sec-Fetch-Mode":  {"websocket"},
		"Sec-Fetch-Site":  {"same-origin"},
		"Accept":          {"*/*"},
		"Accept-Encoding": {"gzip, deflate, br, zstd"},
		"Priority":        {"u=3, i"},
	}
	web.SetWhatsAppClient(client)
	command.ErrorHook = forwardCommandError
	var notifyOnce sync.Once
	client.AddEventHandler(func(rawEvt any) {
		switch evt := rawEvt.(type) {
		case *events.Message:
			if evt.Info.Chat.Server == "broadcast" {
				return
			}
			if evt.Info.IsFromMe {
				text := command.ExtractText(evt.Message)
				if config.MatchPrefix(text) == "" {
					return
				}
			}
			logIncomingMessage(client, evt, "rbot")
			stats.AddChat(evt)
			// Autoread: tandai pesan masuk sebagai dibaca bila diaktifkan owner
			// (.set autoread on). Best-effort; error hanya di-log agar tak
			// mengganggu pemrosesan command.
			if settings.AutoRead() {
				if err := client.MarkRead(ctx, []types.MessageID{evt.Info.ID}, time.Now(),
					evt.Info.Chat, evt.Info.Sender); err != nil {
					log.Printf("[rbot] autoread gagal: %v", err)
				}
			}
			// Simi: panen sticker grup ke LMDB secara pasif di background
			if evt.Info.IsGroup && evt.Message != nil && evt.Message.GetStickerMessage() != nil {
				go func() {
					downloadCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					if d, err := client.DownloadAny(downloadCtx, evt.Message); err == nil && len(d) > 0 {
						_ = simi.SaveGroupSticker(d)
					}
				}()
			}
			go command.Dispatch(ctx, client, evt, false)
		case *events.GroupInfo:
			if len(evt.Join) > 0 {
				go welcome.HandleGroupJoin(ctx, client, evt)
			}
			if len(evt.Leave) > 0 {
				go goodbye.HandleGroupLeave(ctx, client, evt)
			}
		case *events.Connected:
			log.Printf("[rbot] terhubung ke WhatsApp sebagai %s", client.Store.ID)
			web.BroadcastMetricsNow()
			// Notif "sudah online" setelah restart: baca penanda sekali saja
			// (Connected bisa berulang saat reconnect). Best-effort — error di-log.
			notifyOnce.Do(func() {
				jid, ok := lifecycle.TakePendingNotify()
				if !ok {
					return
				}
				chat, err := types.ParseJID(jid)
				if err != nil {
					log.Printf("[rbot] notif restart: JID tak valid %q: %v", jid, err)
					return
				}
				if _, err := client.SendMessage(ctx, chat, &waE2E.Message{
					Conversation: proto.String("✅ Bot sudah online kembali."),
				}); err != nil {
					log.Printf("[rbot] notif restart: gagal kirim: %v", err)
				}
			})
		case *events.Disconnected:
			log.Printf("[rbot] koneksi WhatsApp terputus")
			web.BroadcastMetricsNow()
		case *events.LoggedOut:
			log.Printf("[rbot] sesi WhatsApp logout; hapus session/store.db untuk pairing baru")
			web.SetPairingCode("")
			web.SetPasskeyRequired(false)
			web.BroadcastMetricsNow()
		case *events.PairPasskeyRequest:
			// Flow passkey baru Meta (sejak akhir Juni 2026): setelah kode pairing
			// dimasukkan, WhatsApp bisa minta verifikasi passkey/biometrik di HP.
			// Fix handoff ini berasal dari PR tulir/whatsmeow#1234; tanpa itu
			// pairing gagal dengan "missing link_code_pairing_wrapped_primary_ephemeral_pub".
			log.Printf("[rbot] pairing butuh verifikasi passkey: selesaikan verifikasi biometrik/PIN di HP WhatsApp")
			web.SetPasskeyRequired(true)
		case *events.PairPasskeyError:
			log.Printf("[rbot] error passkey pairing (continuation=%v): %v", evt.Continuation, evt.Error)
			web.SetPasskeyRequired(false)
		case *events.PairPasskeyConfirmation:
			// Kode konfirmasi 8 karakter hasil flow passkey; tampilkan seperti kode pairing.
			log.Printf("[rbot] kode konfirmasi passkey pairing: %s", evt.Code)
			web.SetPairingCode(evt.Code)
			if evt.SkipHandoffUX {
				log.Printf("[rbot] handoff passkey otomatis; verifikasi ulang kode di HP tidak diperlukan")
			} else {
				log.Printf("[rbot] cocokkan kode di HP WhatsApp lalu konfirmasi untuk menyelesaikan pairing")
			}
		case *events.PairSuccess:
			// Pairing berhasil: kosongkan state pairing di dashboard agar banner hilang.
			log.Printf("[rbot] pairing berhasil, wipe banner pairing di dashboard")
			web.SetPairingCode("")
			web.SetPasskeyRequired(false)
			web.BroadcastMetricsNow()
		case *events.PairError:
			log.Printf("[rbot] pairing gagal: %v", evt.Error)
			web.SetPasskeyRequired(false)
		}
	})

	if err := client.Connect(); err != nil {
		return fmt.Errorf("hubungkan WhatsApp: %w", err)
	}
	defer client.Disconnect()

	if client.Store.ID == nil {
		phone := config.Digits(config.C.BotNumber)
		if phone == "" {
			return fmt.Errorf("botNumber kosong di config.json")
		}
		// WA server reject pairing kalau DeviceProps fingerprint tidak konsisten
		// dengan companion_platform_id (whatsmeow issue #1191). Override fingerprint
		// ke Chrome macOS supaya cocok dengan PairClientChrome.
		wastore.DeviceProps.Os = proto.String("Chrome")
		wastore.DeviceProps.Version = &waCompanionReg.DeviceProps_AppVersion{
			Primary:   proto.Uint32(143),
			Secondary: proto.Uint32(0),
			Tertiary:  proto.Uint32(7499),
		}
		wastore.DeviceProps.PlatformType = waCompanionReg.DeviceProps_CHROME.Enum()
		wastore.BaseClientPayload.UserAgent.Platform = waWa6.ClientPayload_UserAgent_WEB.Enum()
		wastore.BaseClientPayload.UserAgent.OsVersion = proto.String("10.15.7")
		wastore.BaseClientPayload.UserAgent.OsBuildNumber = proto.String("10.15.7")
		wastore.BaseClientPayload.UserAgent.Manufacturer = proto.String("Apple Inc.")
		wastore.BaseClientPayload.UserAgent.Device = proto.String("Mac OS X")

		// PairPhone harus dipanggil setelah websocket tersambung.
		time.Sleep(time.Second)
		pairCtx, pairCancel := context.WithTimeout(ctx, 45*time.Second)
		code, err := client.PairPhone(pairCtx, phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
		pairCancel()
		if err != nil {
			return fmt.Errorf("minta kode pairing: %w", err)
		}
		pretty := code
		if len(code) == 8 {
			pretty = code[:4] + "-" + code[4:]
		}
		web.SetPairingCode(pretty)
		fmt.Println("==============================")
		fmt.Println(" Kode Pairing WhatsApp:", pretty)
		fmt.Println(" Buka WhatsApp di HP > Perangkat Tertaut > Tautkan dengan nomor telepon")
		fmt.Println(" PENTING: masukkan kode dalam < 2 menit, jangan sampai kadaluarsa.")
		fmt.Println("==============================")
	}

	log.Printf("[rbot] %d command terdaftar", command.Count())
	jadibot.Init(ctx)
	// Tunggu sinyal OS (ctx) atau permintaan restart dari command. Keduanya
	// membuat run() kembali normal sehingga defer (tutup sesi & store) dijalankan;
	// main() yang memutuskan re-exec bila restart yang memicu.
	select {
	case <-ctx.Done():
		log.Printf("[rbot] shutdown")
	case <-lifecycle.Signal():
		log.Printf("[rbot] restart diminta; menutup dengan rapi")
	}
	return nil
}

// removeWhatsAppSessionFiles menghapus database sesi WhatsApp bot utama agar
// boot berikutnya memulai pairing baru; kode pairing tampil otomatis di Web.
func removeWhatsAppSessionFiles() {
	for _, name := range []string{"store.db", "store.db-shm", "store.db-wal"} {
		path := filepath.Join("session", name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("[rbot] reset sesi: gagal menghapus %s: %v", path, err)
		}
	}
	log.Printf("[rbot] reset sesi: session/store.db dihapus; kode pairing baru akan diminta otomatis")
}

func forwardCommandError(ctx context.Context, c *command.Ctx, commandErr error) {
	if c == nil || c.Client == nil || c.Evt == nil || commandErr == nil {
		return
	}
	ownerRaw := config.PrimaryOwnerAddress()
	if ownerRaw == "" {
		log.Printf("[rbot] error command %q tidak diteruskan: owner belum dikonfigurasi", c.InvokedAs)
		return
	}
	if !strings.Contains(ownerRaw, "@") {
		ownerRaw += "@s.whatsapp.net"
	}
	ownerJID, err := types.ParseJID(ownerRaw)
	if err != nil {
		log.Printf("[rbot] error command %q tidak diteruskan: JID owner %q tidak valid: %v", c.InvokedAs, ownerRaw, err)
		return
	}

	name := strings.TrimSpace(c.Evt.Info.PushName)
	if name == "" {
		name = "unknown"
	}
	report := fmt.Sprintf("⚠️ *Command Error*\n\nCommand: *%s*\nDari: %s (%s)\nChat: %s\n\nError:\n%s", c.InvokedAs, name, c.Sender().String(), c.Chat().String(), truncateLogText(commandErr.Error(), 1000))
	reportCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := c.Client.SendMessage(reportCtx, ownerJID, &waE2E.Message{Conversation: proto.String(report)}); err != nil {
		log.Printf("[rbot] gagal meneruskan error command %q ke owner: %v", c.InvokedAs, err)
	}
}

func logIncomingMessage(client *whatsmeow.Client, evt *events.Message, botTag string) {
	if evt == nil {
		return
	}
	text := command.ExtractText(evt.Message)
	msgType := getMessageType(evt, text)

	if text == "" {
		text = messageType(evt.Message)
	}

	text = strings.ReplaceAll(strings.ReplaceAll(text, "\n", "\\n"), "\r", "\\r")
	text = truncateLogText(text, 500)

	chatType := "private"
	if evt.Info.IsGroup {
		chatType = "group"
	}

	name := strings.TrimSpace(evt.Info.PushName)
	if name == "" {
		name = "unknown"
	}

	senderLID := evt.Info.Sender.String()
	chatIDStr := getChatIDString(client, evt)
	tStr := time.Now().Format("2006/01/02 15:04:05")

	if botTag == "" {
		botTag = "rbot"
	}

	// ANSI color codes:
	// \033[32m = Green (timestamp & MSG_TYPE)
	// \033[1;37m = Bold White (bot username & cmd/input)
	// \033[0m = Reset
	logLine := fmt.Sprintf("\033[32m%s\033[0m \033[1;37m[%s]\033[0m = \033[32m%s\033[0m, info: user: %q, lid:%q, id:%q, type:%q, input:\033[1;37m%q\033[0m",
		tStr, botTag, msgType, name, senderLID, chatIDStr, chatType, text)

	fmt.Println(logLine)
	web.Broadcaster.AddLine(logLine)
}

func getChatIDString(client *whatsmeow.Client, evt *events.Message) string {
	if evt == nil {
		return ""
	}
	chatID := evt.Info.Chat.String()
	if evt.Info.IsGroup && client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if info, err := client.GetGroupInfo(ctx, evt.Info.Chat); err == nil && strings.TrimSpace(info.Name) != "" {
			return fmt.Sprintf("%s (%s)", strings.TrimSpace(info.Name), chatID)
		}
	}
	return chatID
}

func getMessageType(evt *events.Message, text string) string {
	if evt == nil || evt.Message == nil {
		return "PESAN TEXT"
	}

	msg := evt.Message
	prefix := config.MainPrefix()

	trimmedText := strings.TrimSpace(text)
	if trimmedText != "" {
		if strings.HasPrefix(trimmedText, prefix) || strings.HasPrefix(trimmedText, ".") || strings.HasPrefix(trimmedText, "!") || strings.HasPrefix(trimmedText, "/") || strings.HasPrefix(trimmedText, "#") {
			return "CMD"
		}
	}

	if msg.ImageMessage != nil {
		mime := strings.ToLower(msg.ImageMessage.GetMimetype())
		if strings.Contains(mime, "png") {
			return "MEDIA PNG"
		}
		if strings.Contains(mime, "jpeg") || strings.Contains(mime, "jpg") {
			return "MEDIA JPEG"
		}
		return "MEDIA IMAGE"
	}
	if msg.VideoMessage != nil {
		return "MEDIA VIDEO"
	}
	if msg.AudioMessage != nil {
		return "MEDIA AUDIO"
	}
	if msg.StickerMessage != nil {
		return "STICKER"
	}
	if msg.DocumentMessage != nil {
		return "DOCUMENT"
	}
	if msg.PtvMessage != nil || msg.LocationMessage != nil || msg.ContactMessage != nil {
		return "MEDIA DLL"
	}

	return "PESAN TEXT"
}

func truncateLogText(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "…"
}

func messageType(msg *waE2E.Message) string {
	if msg == nil {
		return "empty"
	}
	switch {
	case msg.GetImageMessage() != nil:
		return "[image]"
	case msg.GetVideoMessage() != nil:
		return "[video]"
	case msg.GetAudioMessage() != nil:
		return "[audio]"
	case msg.GetDocumentMessage() != nil:
		return "[document]"
	case msg.GetStickerMessage() != nil:
		return "[sticker]"
	case msg.GetContactMessage() != nil:
		return "[contact]"
	case msg.GetLocationMessage() != nil:
		return "[location]"
	default:
		return "[message]"
	}
}
