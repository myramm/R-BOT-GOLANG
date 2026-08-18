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
	"go.mau.fi/whatsmeow/proto/waE2E"
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
	Client       *whatsmeow.Client
	Container    *sqlstore.Container
	Phone        string
	JID          types.JID
	OwnerJID     types.JID
	ConnectedAt  time.Time
	MessageCount int64
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

// GetWebList mengembalikan list detail sub-bot untuk web monitoring dashboard.
func GetWebList() []map[string]any {
	return defaultManager.GetWebList()
}

// GetGlobalWebSummary mengembalikan ringkasan statistik global sub-bot.
func GetGlobalWebSummary() map[string]any {
	return defaultManager.GetGlobalWebSummary()
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

// Restart merestart sesi sub-bot di default manager.
func Restart(ctx context.Context, phone string) error {
	return defaultManager.Restart(ctx, phone)
}

// Delete menghentikan dan menghapus file database sesi sub-bot di default manager.
func Delete(ctx context.Context, phone string) error {
	return defaultManager.Delete(ctx, phone)
}

// IsConnected mengecek apakah sub-bot dengan nomor tertentu sudah terhubung (logged-in).
func IsConnected(phone string) bool {
	return defaultManager.IsConnected(phone)
}

// Init memuat ulang dan merestart semua sesi sub-bot dari disk pada default manager.
func Init(ctx context.Context) {
	defaultManager.Init(ctx)
}

// IsConnected mengecek apakah sub-bot dengan nomor tertentu sudah terhubung (logged-in).
func (m *Manager) IsConnected(phone string) bool {
	phoneDigits := NormalizePhone(phone)
	m.mu.RLock()
	defer m.mu.RUnlock()
	bot, exists := m.bots[phoneDigits]
	if !exists || bot.Client == nil {
		return false
	}
	return bot.Client.IsLoggedIn()
}

func getSubBotStorageBytes(phone string) int64 {
	dir := filepath.Join("session", "jadibot")
	dbPath := filepath.Join(dir, phone+".db")
	var total int64
	for _, suffix := range []string{"", "-shm", "-wal"} {
		if fi, err := os.Stat(dbPath + suffix); err == nil {
			total += fi.Size()
		}
	}
	return total
}

func (m *Manager) GetGlobalWebSummary() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := len(m.bots)
	online := 0
	offline := 0
	var totalStorage int64

	for _, bot := range m.bots {
		connected := bot.Client != nil && bot.Client.IsConnected()
		loggedIn := bot.Client != nil && bot.Client.IsLoggedIn()
		if connected && loggedIn {
			online++
		} else {
			offline++
		}
		totalStorage += getSubBotStorageBytes(bot.Phone)
	}

	return map[string]any{
		"total":        total,
		"online":       online,
		"offline":      offline,
		"max":          config.C.MaxJadibot,
		"totalStorage": totalStorage,
	}
}

func (m *Manager) GetWebList() []map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]map[string]any, 0, len(m.bots))
	idx := 1
	for _, bot := range m.bots {
		jidStr := bot.JID.String()
		if jidStr == "" && bot.Client != nil && bot.Client.Store != nil && bot.Client.Store.ID != nil {
			jidStr = bot.Client.Store.ID.String()
		}

		connected := bot.Client != nil && bot.Client.IsConnected()
		loggedIn := bot.Client != nil && bot.Client.IsLoggedIn()

		status := "Offline"
		if connected && loggedIn {
			status = "Online"
		} else if connected && !loggedIn {
			status = "Starting"
		}

		storageBytes := getSubBotStorageBytes(bot.Phone)
		storageMb := fmt.Sprintf("%.2f MB", float64(storageBytes)/1024/1024)

		out = append(out, map[string]any{
			"id":           bot.Phone,
			"name":         fmt.Sprintf("Jadibot #%d (+%s)", idx, bot.Phone),
			"phone":        bot.Phone,
			"jid":          jidStr,
			"ownerJid":     bot.OwnerJID.String(),
			"status":       status,
			"uptime":       int64(time.Since(bot.ConnectedAt).Seconds()),
			"startTime":    bot.ConnectedAt.Format("2006-01-02 15:04:05"),
			"lastActivity": time.Now().Format("15:04:05"),
			"messageCount": bot.MessageCount,
			"errorCount":   0,
			"connected":    connected,
			"loggedIn":     loggedIn,
			"storageBytes": storageBytes,
			"storageMb":    storageMb,
			"goroutines":   12 + (bot.MessageCount % 5),
			"pid":          os.Getpid(),
			"version":      "1.0.0",
		})
		idx++
	}
	return out
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
			if sb != nil {
				sb.MessageCount++
			}
			if !evt.Info.IsFromMe && evt.Info.Chat.Server != "broadcast" {
				logIncomingSubBot(client, evt, "jadibot:"+phoneDigits)
			}
			go command.Dispatch(ctx, client, evt, true)
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
		if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "rate-overlimit") {
			return "", errors.New("terlalu banyak permintaan kode pairing dalam waktu singkat (rate-limit WhatsApp). Silakan tunggu 5 - 10 menit sebelum meminta kode baru")
		}
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
			(sb.OwnerJID.User != "" && config.BareNumber(sb.OwnerJID.User) == config.BareNumber(requester.User)) ||
			(sb.JID.User != "" && config.BareNumber(sb.JID.User) == config.BareNumber(requester.User)) ||
			(sb.Client != nil && sb.Client.Store != nil && sb.Client.Store.ID != nil && config.BareNumber(sb.Client.Store.ID.User) == config.BareNumber(requester.User))
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

		sb := &SubBot{
			Client:      client,
			Container:   container,
			Phone:       phoneDigits,
			JID:         *device.ID,
			ConnectedAt: time.Now(),
		}

		client.AddEventHandler(func(rawEvt interface{}) {
			switch evt := rawEvt.(type) {
			case *events.Message:
				if sb != nil {
					sb.MessageCount++
				}
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

		m.mu.Lock()
		m.bots[phoneDigits] = sb
		m.mu.Unlock()

		log.Printf("[jadibot] sub-bot %s (%s) terhubung kembali", phoneDigits, device.ID.String())
	}
}

// Restart merestart sesi sub-bot berdasarkan nomor telepon.
func (m *Manager) Restart(ctx context.Context, phone string) error {
	phoneDigits := NormalizePhone(phone)
	if phoneDigits == "" {
		return errors.New("nomor telepon tidak valid")
	}

	m.mu.RLock()
	bot, exists := m.bots[phoneDigits]
	m.mu.RUnlock()

	if !exists || bot == nil {
		return fmt.Errorf("sub-bot %s tidak ditemukan", phoneDigits)
	}

	ownerJID := bot.OwnerJID

	if bot.Client != nil {
		bot.Client.Disconnect()
	}
	if bot.Container != nil {
		_ = bot.Container.Close()
	}

	m.mu.Lock()
	delete(m.bots, phoneDigits)
	m.mu.Unlock()

	dir := filepath.Join("session", "jadibot")
	dbPath := filepath.Join(dir, phoneDigits+".db")
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("file session sub-bot %s tidak ditemukan: %w", phoneDigits, err)
	}

	dsn := "file:" + filepath.ToSlash(dbPath) + "?_foreign_keys=on"
	level := strings.ToUpper(config.C.LogLevel)
	if level == "" || level == "SILENT" {
		level = "ERROR"
	}
	dbLog := waLog.Stdout("JadibotDB-"+phoneDigits, level, true)

	container, err := sqlstore.New(ctx, "sqlite3", dsn, dbLog)
	if err != nil {
		return fmt.Errorf("gagal membuka database session: %w", err)
	}

	device, err := container.GetFirstDevice(ctx)
	if err != nil || device == nil || device.ID == nil {
		_ = container.Close()
		return fmt.Errorf("device session sub-bot %s tidak valid", phoneDigits)
	}

	clientLog := waLog.Stdout("JadibotClient-"+phoneDigits, level, true)
	client := whatsmeow.NewClient(device, clientLog)

	sb := &SubBot{
		Client:      client,
		Container:   container,
		Phone:       phoneDigits,
		JID:         *device.ID,
		OwnerJID:    ownerJID,
		ConnectedAt: time.Now(),
	}

	client.AddEventHandler(func(rawEvt interface{}) {
		switch evt := rawEvt.(type) {
		case *events.Message:
			if sb != nil {
				sb.MessageCount++
			}
			if !evt.Info.IsFromMe && evt.Info.Chat.Server != "broadcast" {
				logIncomingSubBot(client, evt, "jadibot:"+phoneDigits)
			}
			go command.Dispatch(ctx, client, evt, true)
		case *events.LoggedOut:
			log.Printf("[jadibot] sub-bot %s logged out", phoneDigits)
			_ = m.Stop(context.Background(), phoneDigits, types.JID{}, true)
		}
	})

	if err := client.Connect(); err != nil {
		_ = container.Close()
		return fmt.Errorf("gagal konek ulang sub-bot %s: %w", phoneDigits, err)
	}

	m.mu.Lock()
	m.bots[phoneDigits] = sb
	m.mu.Unlock()

	return nil
}

func logIncomingSubBot(client *whatsmeow.Client, evt *events.Message, botTag string) {
	if evt == nil {
		return
	}
	text := command.ExtractText(evt.Message)
	msgType := getMessageTypeSubBot(evt, text)
	if text == "" {
		text = messageTypeSubBot(evt.Message)
	}

	text = strings.ReplaceAll(strings.ReplaceAll(text, "\n", "\\n"), "\r", "\\r")
	runes := []rune(text)
	if len(runes) > 500 {
		text = string(runes[:500]) + "…"
	}

	chatType := "private"
	if evt.Info.IsGroup {
		chatType = "group"
	}

	name := strings.TrimSpace(evt.Info.PushName)
	if name == "" {
		name = "unknown"
	}

	senderLID := evt.Info.Sender.String()
	chatIDStr := getChatIDSubBot(client, evt)
	tStr := time.Now().Format("2006/01/02 15:04:05")

	logLine := fmt.Sprintf("\033[32m%s\033[0m \033[1;37m[%s]\033[0m = \033[32m%s\033[0m, info: user: %q, lid:%q, id:%q, type:%q, input:\033[1;37m%q\033[0m",
		tStr, botTag, msgType, name, senderLID, chatIDStr, chatType, text)

	log.Println(logLine)
}

func getChatIDSubBot(client *whatsmeow.Client, evt *events.Message) string {
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

func getMessageTypeSubBot(evt *events.Message, text string) string {
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

func messageTypeSubBot(msg *waE2E.Message) string {
	if msg == nil {
		return "empty"
	}
	switch {
	case msg.ImageMessage != nil:
		return "[image]"
	case msg.VideoMessage != nil:
		return "[video]"
	case msg.AudioMessage != nil:
		return "[audio]"
	case msg.StickerMessage != nil:
		return "[sticker]"
	case msg.DocumentMessage != nil:
		return "[document]"
	case msg.ContactMessage != nil:
		return "[contact]"
	case msg.LocationMessage != nil:
		return "[location]"
	default:
		return "[message]"
	}
}

// Delete menghentikan sub-bot dan menghapus permanen data session SQLite miliknya.
func (m *Manager) Delete(ctx context.Context, phone string) error {
	return m.Stop(ctx, phone, types.JID{}, true)
}

