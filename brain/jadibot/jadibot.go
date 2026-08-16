// Package jadibot mengelola sub-bot multi-sesi menggunakan WhatsMeow.
package jadibot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"rbot/brain/command"
	"rbot/brain/config"
)

// SubBotInfo mendeskripsikan informasi dasar sub-bot aktif.
type SubBotInfo struct {
	Phone       string
	JID         types.JID
	OwnerJID    types.JID
	ConnectedAt time.Time
}

// SubBot merepresentasikan satu sesi sub-bot yang berjalan.
type SubBot struct {
	Client      *whatsmeow.Client
	Container   *sqlstore.Container
	Phone       string
	JID         types.JID
	OwnerJID    types.JID
	ConnectedAt time.Time
}

// Manager mengelola map sub-bot aktif.
type Manager struct {
	mu   sync.RWMutex
	bots map[string]*SubBot
}

var defaultManager = NewManager()

// NewManager membuat instance Manager baru.
func NewManager() *Manager {
	return &Manager{
		bots: make(map[string]*SubBot),
	}
}

// Count mengembalikan jumlah sub-bot aktif di default manager.
func Count() int {
	return defaultManager.Count()
}

// List mengembalikan daftar informasi sub-bot aktif di default manager.
func List() []SubBotInfo {
	return defaultManager.List()
}

// StartPairing memulai proses pairing sub-bot baru di default manager.
func StartPairing(ctx context.Context, phone string, senderJID types.JID) (string, error) {
	return defaultManager.StartPairing(ctx, phone, senderJID)
}

// Stop menghentikan sub-bot di default manager.
func Stop(ctx context.Context, target string, requester types.JID, isOwner bool) error {
	return defaultManager.Stop(ctx, target, requester, isOwner)
}

// Init memuat ulang dan merestart semua sesi sub-bot dari disk pada default manager.
func Init(ctx context.Context) {
	defaultManager.Init(ctx)
}

// Count mengembalikan jumlah sub-bot aktif.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.bots)
}

// List mengembalikan slice informasi sub-bot aktif.
func (m *Manager) List() []SubBotInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]SubBotInfo, 0, len(m.bots))
	for _, bot := range m.bots {
		jid := bot.JID
		if jid.IsEmpty() && bot.Client != nil && bot.Client.Store != nil && bot.Client.Store.ID != nil {
			jid = *bot.Client.Store.ID
		}
		out = append(out, SubBotInfo{
			Phone:       bot.Phone,
			JID:         jid,
			OwnerJID:    bot.OwnerJID,
			ConnectedAt: bot.ConnectedAt,
		})
	}
	return out
}

// NormalizePhone membersihkan karakter non-angka dan mengubah awalan '0' menjadi '62' (format internasional).
func NormalizePhone(phone string) string {
	digits := config.Digits(phone)
	if strings.HasPrefix(digits, "0") {
		return "62" + digits[1:]
	}
	return digits
}

// StartPairing membersihkan nomor, mengecek kuota max jadibot, membuat container SQLite,
// menghubungkan client whatsmeow, dan meminta kode pairing.
func (m *Manager) StartPairing(ctx context.Context, phone string, senderJID types.JID) (string, error) {
	phoneDigits := NormalizePhone(phone)
	if phoneDigits == "" {
		return "", errors.New("nomor telepon tidak valid")
	}

	maxJadibot := config.C.MaxJadibot
	if maxJadibot > 0 && m.Count() >= maxJadibot {
		return "", fmt.Errorf("kuota jadibot sudah penuh (%d/%d)", m.Count(), maxJadibot)
	}

	m.mu.Lock()
	if bot, exists := m.bots[phoneDigits]; exists {
		if bot.Client != nil && bot.Client.IsLoggedIn() {
			m.mu.Unlock()
			return "", fmt.Errorf("sub-bot dengan nomor %s sudah aktif", phoneDigits)
		}
		m.mu.Unlock()
		_ = m.Stop(ctx, phoneDigits, types.JID{}, true)
	} else {
		m.mu.Unlock()
	}

	dir := filepath.Join("session", "jadibot")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("gagal membuat direktori session: %w", err)
	}

	dbPath := filepath.Join(dir, phoneDigits+".db")
	// Hapus file DB lama bila ada bekas percobaan pairing yang belum logged-in
	_ = os.Remove(dbPath)
	_ = os.Remove(dbPath + "-shm")
	_ = os.Remove(dbPath + "-wal")

	dsn := "file:" + filepath.ToSlash(dbPath) + "?_foreign_keys=on"

	level := strings.ToUpper(config.C.LogLevel)
	if level == "" || level == "SILENT" {
		level = "ERROR"
	}
	dbLog := waLog.Stdout("JadibotDB-"+phoneDigits, level, true)

	container, err := sqlstore.New(ctx, "sqlite3", dsn, dbLog)
	if err != nil {
		return "", fmt.Errorf("gagal membuka database session: %w", err)
	}

	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		_ = container.Close()
		return "", fmt.Errorf("gagal mendapatkan device session: %w", err)
	}
	if device == nil {
		device = container.NewDevice()
	}

	clientLog := waLog.Stdout("JadibotClient-"+phoneDigits, level, true)
	client := whatsmeow.NewClient(device, clientLog)

	sb := &SubBot{
		Client:      client,
		Container:   container,
		Phone:       phoneDigits,
		OwnerJID:    senderJID,
		ConnectedAt: time.Now(),
	}

	qrReady := make(chan struct{}, 1)
	client.AddEventHandler(func(rawEvt interface{}) {
		switch evt := rawEvt.(type) {
		case *events.QR:
			select {
			case qrReady <- struct{}{}:
			default:
			}
		case *events.Message:
			command.Dispatch(ctx, client, evt, true)
		case *events.Connected:
			if client.Store != nil && client.Store.ID != nil {
				sb.JID = *client.Store.ID
			}
			log.Printf("[jadibot] sub-bot %s terhubung sebagai %s", phoneDigits, client.Store.ID)
		case *events.LoggedOut:
			log.Printf("[jadibot] sub-bot %s logged out", phoneDigits)
			_ = m.Stop(context.Background(), phoneDigits, types.JID{}, true)
		}
	})

	if err := client.Connect(); err != nil {
		_ = container.Close()
		return "", fmt.Errorf("gagal menghubungkan client: %w", err)
	}

	// Tunggu hingga event QR/koneksi websocket siap dari server WhatsApp sebelum meminta kode pairing
	select {
	case <-qrReady:
	case <-time.After(3 * time.Second):
	}

	code, err := client.PairPhone(ctx, phoneDigits, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		client.Disconnect()
		_ = container.Close()
		_ = os.Remove(dbPath)
		return "", fmt.Errorf("gagal mendapatkan kode pairing: %w", err)
	}

	if device.ID != nil {
		sb.JID = *device.ID
	}

	m.mu.Lock()
	m.bots[phoneDigits] = sb
	m.mu.Unlock()

	return code, nil
}

// Stop menghentikan dan menghapus sub-bot berdasarkan nomor telepon atau JID.
func (m *Manager) Stop(ctx context.Context, target string, requester types.JID, isOwner bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanTarget := config.Digits(target)
	var foundPhone string
	var sb *SubBot

	for phone, bot := range m.bots {
		if phone == target || (cleanTarget != "" && phone == cleanTarget) {
			sb = bot
			foundPhone = phone
			break
		}
		if bot.Client != nil && bot.Client.Store != nil && bot.Client.Store.ID != nil {
			jid := bot.Client.Store.ID
			if jid.String() == target || jid.User == target || jid.ToNonAD().String() == target || (cleanTarget != "" && config.BareNumber(jid.User) == cleanTarget) {
				sb = bot
				foundPhone = phone
				break
			}
		} else if !bot.JID.IsEmpty() {
			if bot.JID.String() == target || bot.JID.User == target || bot.JID.ToNonAD().String() == target || (cleanTarget != "" && config.BareNumber(bot.JID.User) == cleanTarget) {
				sb = bot
				foundPhone = phone
				break
			}
		}
	}

	if sb == nil {
		return fmt.Errorf("sub-bot %q tidak ditemukan", target)
	}

	if !isOwner {
		sameOwner := sb.OwnerJID == requester ||
			(sb.OwnerJID.ToNonAD() == requester.ToNonAD() && !sb.OwnerJID.IsEmpty()) ||
			(sb.OwnerJID.User != "" && config.BareNumber(sb.OwnerJID.User) == config.BareNumber(requester.User))
		if !sameOwner {
			return errors.New("Anda bukan pembuat sub-bot ini")
		}
	}

	delete(m.bots, foundPhone)

	if sb.Client != nil {
		if sb.Client.IsConnected() {
			_ = sb.Client.Logout(ctx)
		}
		sb.Client.Disconnect()
	}
	if sb.Container != nil {
		_ = sb.Container.Close()
	}

	dbPath := filepath.Join("session", "jadibot", foundPhone+".db")
	_ = os.Remove(dbPath)
	_ = os.Remove(dbPath + "-shm")
	_ = os.Remove(dbPath + "-wal")

	return nil
}

// Init memindai direktori session/jadibot/ dan merekonseksikan sub-bot yang terdaftar.
func (m *Manager) Init(ctx context.Context) {
	dir := filepath.Join("session", "jadibot")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	level := strings.ToUpper(config.C.LogLevel)
	if level == "" || level == "SILENT" {
		level = "ERROR"
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		phoneDigits := strings.TrimSuffix(entry.Name(), ".db")
		if phoneDigits == "" {
			continue
		}

		m.mu.Lock()
		_, exists := m.bots[phoneDigits]
		m.mu.Unlock()
		if exists {
			continue
		}

		dbPath := filepath.Join(dir, entry.Name())
		dsn := "file:" + filepath.ToSlash(dbPath) + "?_foreign_keys=on"
		dbLog := waLog.Stdout("JadibotDB-"+phoneDigits, level, true)

		container, err := sqlstore.New(ctx, "sqlite3", dsn, dbLog)
		if err != nil {
			log.Printf("[jadibot] gagal membuka container %s: %v", dbPath, err)
			continue
		}

		device, err := container.GetFirstDevice(ctx)
		if err != nil || device == nil || device.ID == nil {
			_ = container.Close()
			continue
		}

		clientLog := waLog.Stdout("JadibotClient-"+phoneDigits, level, true)
		client := whatsmeow.NewClient(device, clientLog)

		client.AddEventHandler(func(rawEvt interface{}) {
			switch evt := rawEvt.(type) {
			case *events.Message:
				command.Dispatch(ctx, client, evt, true)
			case *events.LoggedOut:
				log.Printf("[jadibot] sub-bot %s logged out", phoneDigits)
				_ = m.Stop(context.Background(), phoneDigits, types.JID{}, true)
			}
		})

		if err := client.Connect(); err != nil {
			log.Printf("[jadibot] gagal konek sub-bot %s: %v", phoneDigits, err)
			_ = container.Close()
			continue
		}

		sb := &SubBot{
			Client:      client,
			Container:   container,
			Phone:       phoneDigits,
			JID:         *device.ID,
			ConnectedAt: time.Now(),
		}

		m.mu.Lock()
		m.bots[phoneDigits] = sb
		m.mu.Unlock()

		log.Printf("[jadibot] sub-bot %s (%s) terhubung kembali", phoneDigits, device.ID.String())
	}
}
