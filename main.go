package main

import (
	"context"
	"fmt"
	"log"
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
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/brain/jadibot"
	"rbot/brain/lifecycle"
	"rbot/brain/settings"
	"rbot/brain/stats"
	"rbot/brain/store"
	"rbot/brain/web"
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
	web.SetWhatsAppClient(client)
	command.ErrorHook = forwardCommandError
	var notifyOnce sync.Once
	client.AddEventHandler(func(rawEvt any) {
		switch evt := rawEvt.(type) {
		case *events.Message:
			if evt.Info.IsFromMe || evt.Info.Chat.Server == "broadcast" {
				return
			}
			logIncomingMessage(evt)
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
			command.Dispatch(ctx, client, evt, false)
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
			web.BroadcastMetricsNow()
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
		// PairPhone harus dipanggil setelah websocket tersambung.
		time.Sleep(time.Second)
		code, err := client.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
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

func logIncomingMessage(evt *events.Message) {
	if evt == nil {
		return
	}
	text := command.ExtractText(evt.Message)
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
	log.Printf("[rbot] incoming user=%q sender=%s chat=%s type=%s msg=%q", name, evt.Info.Sender, evt.Info.Chat, chatType, text)
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
