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
	"rbot/brain/lifecycle"
	"rbot/brain/settings"
	"rbot/brain/stats"
	"rbot/brain/store"
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
	var notifyOnce sync.Once
	client.AddEventHandler(func(rawEvt any) {
		switch evt := rawEvt.(type) {
		case *events.Message:
			if evt.Info.IsFromMe || evt.Info.Chat.Server == "broadcast" {
				return
			}
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
		case *events.LoggedOut:
			log.Printf("[rbot] sesi WhatsApp logout; hapus session/store.db untuk pairing baru")
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
		fmt.Println("==============================")
		fmt.Println(" Kode Pairing WhatsApp:", pretty)
		fmt.Println(" Buka WhatsApp di HP > Perangkat Tertaut > Tautkan dengan nomor telepon")
		fmt.Println(" PENTING: masukkan kode dalam < 2 menit, jangan sampai kadaluarsa.")
		fmt.Println("==============================")
	}

	log.Printf("[rbot] %d command terdaftar", command.Count())
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
