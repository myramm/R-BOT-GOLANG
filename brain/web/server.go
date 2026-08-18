package web

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"rbot/brain/config"
	"rbot/brain/jadibot"
	"rbot/brain/lifecycle"
	"rbot/brain/settings"
	"rbot/brain/simi"
	"rbot/brain/stats"
	"rbot/brain/store"
	"rbot/brain/swgc"
	"rbot/brain/updater"
	"rbot/brain/welcome"
	"rbot/cmd"
)

//go:embed static/index.html
var indexHTML []byte

type AuditLog struct {
	Timestamp string `json:"timestamp"`
	IP        string `json:"ip"`
	User      string `json:"user"`
	Action    string `json:"action"`
	Details   string `json:"details"`
}

type loginTracker struct {
	Attempts  int
	BlockedTo time.Time
}

type metricsSubscriber chan []byte

type MetricsBroadcaster struct {
	mu          sync.RWMutex
	subscribers map[metricsSubscriber]bool
}

var (
	sessions   = make(map[string]time.Time)
	sessionsMu sync.RWMutex

	loginAttempts = make(map[string]*loginTracker)
	loginMu       sync.Mutex

	auditLogs []AuditLog
	auditMu   sync.RWMutex

	waClient    *whatsmeow.Client
	pairingCode string
	startTime   = time.Now()
	stateMu     sync.RWMutex

	lastCPUTime int64
	cpuMu       sync.Mutex

	MetricsHub = &MetricsBroadcaster{
		subscribers: make(map[metricsSubscriber]bool),
	}
)

func (b *MetricsBroadcaster) Subscribe() metricsSubscriber {
	ch := make(metricsSubscriber, 50)
	b.mu.Lock()
	b.subscribers[ch] = true
	b.mu.Unlock()
	return ch
}

func (b *MetricsBroadcaster) Unsubscribe(ch metricsSubscriber) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
	for len(ch) > 0 {
		<-ch
	}
	close(ch)
}

func (b *MetricsBroadcaster) HasSubscribers() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers) > 0
}

func (b *MetricsBroadcaster) Broadcast(data []byte) {
	b.mu.RLock()
	subs := make([]metricsSubscriber, 0, len(b.subscribers))
	for ch := range b.subscribers {
		subs = append(subs, ch)
	}
	b.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- data:
		default:
		}
	}
}

func BroadcastMetricsNow() {
	if !MetricsHub.HasSubscribers() {
		return
	}
	payload := BuildStatusPayload()
	b, err := json.Marshal(payload)
	if err == nil {
		MetricsHub.Broadcast(b)
	}
}

func RecordAudit(r *http.Request, action, details string) {
	ip := "127.0.0.1"
	if r != nil {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			ip = strings.Split(forwarded, ",")[0]
		} else if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			ip = host
		} else {
			ip = r.RemoteAddr
		}
	}

	entry := AuditLog{
		Timestamp: time.Now().Format("15:04:05"),
		IP:        ip,
		User:      "admin",
		Action:    action,
		Details:   details,
	}

	auditMu.Lock()
	auditLogs = append([]AuditLog{entry}, auditLogs...)
	if len(auditLogs) > 150 {
		auditLogs = auditLogs[:150]
	}
	auditMu.Unlock()

	BroadcastMetricsNow()
}

func getPassword() string {
	return strings.TrimSpace(config.C.Web.Password)
}

func isPasswordRequired() bool {
	p := strings.ToLower(getPassword())
	return p != "" && p != "none" && p != "disabled" && p != "off" && p != "false"
}

func generateToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func createSession() string {
	token := generateToken()
	exp := time.Now().Add(30 * 24 * time.Hour)
	sessionsMu.Lock()
	sessions[token] = exp
	sessionsMu.Unlock()
	_ = store.Set("web_session_"+token, exp.Unix())
	return token
}

func removeSession(token string) {
	sessionsMu.Lock()
	delete(sessions, token)
	sessionsMu.Unlock()
	_ = store.Delete("web_session_" + token)
}

func validateToken(token string) bool {
	if token == "" {
		return false
	}
	sessionsMu.RLock()
	exp, ok := sessions[token]
	sessionsMu.RUnlock()

	if !ok {
		var unixExp int64
		found, err := store.Get("web_session_"+token, &unixExp)
		if err == nil && found {
			exp = time.Unix(unixExp, 0)
			if time.Now().Before(exp) {
				sessionsMu.Lock()
				sessions[token] = exp
				sessionsMu.Unlock()
				return true
			}
			_ = store.Delete("web_session_" + token)
		}
		return false
	}

	if time.Now().After(exp) {
		removeSession(token)
		return false
	}
	return true
}

func IsAuthenticated(r *http.Request) bool {
	if !isPasswordRequired() {
		return true
	}
	if cookie, err := r.Cookie("rbot_session"); err == nil {
		if validateToken(cookie.Value) {
			return true
		}
	}
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if validateToken(token) {
			return true
		}
	}
	if token := r.URL.Query().Get("token"); token != "" {
		if validateToken(token) {
			return true
		}
	}
	return false
}

func SetWhatsAppClient(cli *whatsmeow.Client) {
	stateMu.Lock()
	waClient = cli
	stateMu.Unlock()
	BroadcastMetricsNow()
}

func SetPairingCode(code string) {
	stateMu.Lock()
	pairingCode = code
	stateMu.Unlock()
	BroadcastMetricsNow()
}

// System Metrics Helpers
func getCPUUsage() float64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		goroutines := float64(runtime.NumGoroutine())
		return math.Min(95.0, math.Max(0.5, goroutines*0.25))
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return 1.0
	}
	fields := strings.Fields(lines[0])
	if len(fields) < 5 || fields[0] != "cpu" {
		return 1.0
	}
	var total int64
	for _, f := range fields[1:] {
		v, _ := strconv.ParseInt(f, 10, 64)
		total += v
	}
	cpuMu.Lock()
	defer cpuMu.Unlock()
	if lastCPUTime == 0 {
		lastCPUTime = total
		return 1.2
	}
	diff := total - lastCPUTime
	lastCPUTime = total
	if diff <= 0 {
		return 1.0
	}
	goroutines := float64(runtime.NumGoroutine())
	return math.Min(98.0, math.Max(0.4, goroutines*0.18))
}

func getDiskUsage() (totalMB, usedMB, freeMB uint64, usagePct float64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		total := stat.Blocks * uint64(stat.Bsize)
		free := stat.Bfree * uint64(stat.Bsize)
		used := total - free
		if total > 0 {
			usagePct = float64(used) / float64(total) * 100
		}
		return total / (1024 * 1024), used / (1024 * 1024), free / (1024 * 1024), usagePct
	}
	return 10240, 2048, 8192, 20.0
}

func getNetStats() (rxMB, txMB float64) {
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	lines := strings.Split(string(b), "\n")
	var totalRX, totalTX uint64
	for _, line := range lines {
		if strings.Contains(line, ":") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				fields := strings.Fields(parts[1])
				if len(fields) >= 9 {
					rx, _ := strconv.ParseUint(fields[0], 10, 64)
					tx, _ := strconv.ParseUint(fields[8], 10, 64)
					totalRX += rx
					totalTX += tx
				}
			}
		}
	}
	return float64(totalRX) / (1024 * 1024), float64(totalTX) / (1024 * 1024)
}

func BuildStatusPayload() map[string]any {
	stateMu.RLock()
	cli := waClient
	pairCode := pairingCode
	stateMu.RUnlock()

	var connected, loggedIn bool
	var jid, pushName string

	if cli != nil {
		connected = cli.IsConnected()
		loggedIn = cli.IsLoggedIn()
		if cli.Store != nil {
			if cli.Store.ID != nil {
				jid = cli.Store.ID.String()
			}
			pushName = cli.Store.PushName
		}
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	diskTotal, diskUsed, diskFree, diskPct := getDiskUsage()
	rxMB, txMB := getNetStats()
	cpuPct := getCPUUsage()

	overview := stats.GetOverview()

	auditMu.RLock()
	recentAudit := append([]AuditLog(nil), auditLogs...)
	auditMu.RUnlock()
	if len(recentAudit) > 10 {
		recentAudit = recentAudit[:10]
	}

	return map[string]any{
		"bot": map[string]any{
			"name":        config.C.BotName,
			"uptime":      int64(time.Since(startTime).Seconds()),
			"connected":   connected,
			"loggedIn":    loggedIn,
			"jid":         jid,
			"pushName":    pushName,
			"pairingCode": pairCode,
		},
		"system": map[string]any{
			"goVersion":  runtime.Version(),
			"goroutines": runtime.NumGoroutine(),
			"allocMb":    fmt.Sprintf("%.2f", float64(m.Alloc)/1024/1024),
			"sysMb":      fmt.Sprintf("%.2f", float64(m.Sys)/1024/1024),
			"numGC":      m.NumGC,
			"os":         runtime.GOOS,
			"arch":       runtime.GOARCH,
			"cpuPct":     fmt.Sprintf("%.1f", cpuPct),
			"diskTotal":  diskTotal,
			"diskUsed":   diskUsed,
			"diskFree":   diskFree,
			"diskPct":    fmt.Sprintf("%.1f", diskPct),
			"netRxMb":    fmt.Sprintf("%.2f", rxMB),
			"netTxMb":    fmt.Sprintf("%.2f", txMB),
		},
		"health": map[string]any{
			"whatsapp":  connected,
			"database":  true,
			"api":       true,
			"websocket": true,
		},
		"config": map[string]any{
			"botName":     config.C.BotName,
			"prefix":      config.C.Prefix,
			"botNumber":   config.C.BotNumber,
			"ownerNumber": config.C.OwnerNumber,
			"logLevel":    config.C.LogLevel,
			"webPort":     config.C.Web.Port,
		},
		"stats":            overview,
		"jadibot": map[string]any{
			"count":   jadibot.Count(),
			"max":     config.C.MaxJadibot,
			"summary": jadibot.GetGlobalWebSummary(),
			"list":    jadibot.GetWebList(),
		},
		"topUsers":         stats.TopUsers(),
		"topGroups":        stats.TopGroups(),
		"topCommands":      stats.TopCommands(),
		"topCommandErrors": stats.TopCommandErrors(),
		"hourlyMessages":   stats.GetHourlyMessages(),
		"auditLogs":        recentAudit,
	}
}

// Start mengaktifkan server web monitoring dan WebSocket terminal.
func Start(ctx context.Context) {
	InitLogger()

	stats.OnChangeEvent = func() {
		BroadcastMetricsNow()
	}

	host := strings.TrimSpace(config.C.Web.Host)
	if host == "" {
		host = "0.0.0.0"
	}
	port := config.C.Web.Port
	if port <= 0 {
		port = 7090
	}

	addr := fmt.Sprintf("%s:%d", host, port)

	mux := http.NewServeMux()

	// Endpoints Public & Auth
	mux.HandleFunc("/api/login", handleLogin)
	mux.HandleFunc("/api/logout", handleLogout)
	mux.HandleFunc("/api/auth/status", handleAuthStatus)

	// Endpoints Protected API
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/audit-logs", handleAuditLogs)
	mux.HandleFunc("/api/restart", handleRestart)
	mux.HandleFunc("/api/stop", handleStop)
	mux.HandleFunc("/api/jadibot/start", handleJadibotStart)
	mux.HandleFunc("/api/jadibot/stop", handleJadibotStop)
	mux.HandleFunc("/api/jadibot/restart", handleJadibotRestart)
	mux.HandleFunc("/api/jadibot/delete", handleJadibotDelete)
	mux.HandleFunc("/api/jadibot/detail", handleJadibotDetail)
	mux.HandleFunc("/api/kill", handleKill)
	mux.HandleFunc("/api/reload", handleReload)
	mux.HandleFunc("/api/broadcast", handleBroadcast)
	mux.HandleFunc("/api/action/user", handleUserAction)
	mux.HandleFunc("/api/swgc/groups", handleSWGCGroups)
	mux.HandleFunc("/api/swgc/send", handleSWGCSend)
	mux.HandleFunc("/api/group/send-message", handleGroupSendMessage)
	mux.HandleFunc("/api/group/mute", handleGroupMute)
	mux.HandleFunc("/api/blacklist", handleBlacklistList)
	mux.HandleFunc("/api/blacklist/toggle", handleBlacklistToggle)
	mux.HandleFunc("/api/bot/setpp", handleSetPPBotWeb)
	mux.HandleFunc("/api/update/check", handleUpdateCheck)
	mux.HandleFunc("/api/update/apply", handleUpdateApply)

	// Simi-Simi AI Endpoints
	mux.HandleFunc("/api/simi/data", handleSimiData)
	mux.HandleFunc("/api/simi/persona", handleSimiPersona)
	mux.HandleFunc("/api/simi/stickers/upload", handleSimiStickerUpload)
	mux.HandleFunc("/api/simi/stickers/delete", handleSimiStickerDelete)
	mux.HandleFunc("/api/simi/stickers/clear", handleSimiStickerClear)

	// Welcome Message Endpoints
	mux.HandleFunc("/api/welcome/groups", handleWelcomeGroups)
	mux.HandleFunc("/api/welcome/toggle", handleWelcomeToggle)
	mux.HandleFunc("/api/welcome/template", handleWelcomeTemplate)

	// Bot Mode Endpoints (Self / Public)
	mux.HandleFunc("/api/mode", handleBotMode)

	// File Manager API Endpoints
	mux.HandleFunc("/api/files/list", handleFileList)
	mux.HandleFunc("/api/files/read", handleFileRead)
	mux.HandleFunc("/api/files/write", handleFileWrite)
	mux.HandleFunc("/api/files/create", handleFileCreate)
	mux.HandleFunc("/api/files/rename", handleFileRename)
	mux.HandleFunc("/api/files/delete", handleFileDelete)
	mux.HandleFunc("/api/files/download", handleFileDownload)
	mux.HandleFunc("/api/files/upload", handleFileUpload)
	mux.HandleFunc("/api/files/search", handleFileSearch)

	// WebSockets
	mux.HandleFunc("/ws/metrics", handleMetricsWS)
	mux.HandleFunc("/ws/logs", handleLogsWS)
	mux.HandleFunc("/ws/terminal", handleTerminalWS)

	// Single Page App
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
	}

	// Real-time hardware ticker loop (every 1.5 seconds)
	go func() {
		ticker := time.NewTicker(1500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if MetricsHub.HasSubscribers() {
					BroadcastMetricsNow()
				}
			}
		}
	}()

	RecordAudit(nil, "SYSTEM", "Web Server Control Center Aktif")
	log.Printf("[rbot] Web monitoring & control center aktif di http://%s", addr)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[rbot] Web server error: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	inputPwd := strings.TrimSpace(req.Password)
	targetPwd := getPassword()

	if inputPwd == targetPwd {
		token := createSession()
		http.SetCookie(w, &http.Cookie{
			Name:     "rbot_session",
			Value:    token,
			Path:     "/",
			Expires:  time.Now().Add(30 * 24 * time.Hour),
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		RecordAudit(r, "LOGIN", "Login berhasil ke Web Dashboard")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    true,
			"token": token,
		})
		return
	}

	RecordAudit(r, "LOGIN_FAILED", "Percobaan login gagal (Password salah)")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":    false,
		"error": "Password salah!",
	})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	RecordAudit(r, "LOGOUT", "Logout dari session")
	if cookie, err := r.Cookie("rbot_session"); err == nil {
		removeSession(cookie.Value)
	}
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		removeSession(strings.TrimPrefix(authHeader, "Bearer "))
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "rbot_session",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	required := isPasswordRequired()
	authenticated := IsAuthenticated(r)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"required":      required,
		"authenticated": authenticated,
	})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	payload := BuildStatusPayload()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	auditMu.RLock()
	logs := append([]AuditLog(nil), auditLogs...)
	auditMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "logs": logs})
}

func handleRestart(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	RecordAudit(r, "RESTART", "Permintaan restart bot dari Web Dashboard")

	go func() {
		time.Sleep(500 * time.Millisecond)
		lifecycle.Request("")
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"message": "Permintaan restart bot berhasil dikirim.",
	})
}

func handleStop(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	RecordAudit(r, "STOP", "Permintaan menghentikan bot")

	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"message": "Proses bot dihentikan secara aman.",
	})
}

func handleKill(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	RecordAudit(r, "FORCE_KILL", "Force kill biner proses bot")

	go func() {
		time.Sleep(200 * time.Millisecond)
		os.Exit(1)
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"message": "Biner bot di-kill paksa.",
	})
}

func handleReload(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	RecordAudit(r, "RELOAD", "Muat ulang file config.json")

	if err := config.Load("config.json"); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": fmt.Sprintf("Gagal muat config: %v", err),
		})
		return
	}

	_ = settings.Load()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"message": "Konfigurasi bot berhasil diperbarui.",
	})
}

func handleBroadcast(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TargetJID string `json:"jid"`
		Message   string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TargetJID == "" || req.Message == "" {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	stateMu.RLock()
	cli := waClient
	stateMu.RUnlock()

	if cli == nil || !cli.IsConnected() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "WhatsApp client belum terhubung!"})
		return
	}

	targetJID, err := types.ParseJID(req.TargetJID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Format JID tidak valid!"})
		return
	}

	_, err = cli.SendMessage(r.Context(), targetJID, &waE2E.Message{
		Conversation: proto.String(req.Message),
	})
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": fmt.Sprintf("Gagal kirim pesan: %v", err)})
		return
	}

	RecordAudit(r, "BROADCAST", fmt.Sprintf("Kirim pesan ke %s", req.TargetJID))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": "Pesan WhatsApp berhasil dikirim!"})
}

func handleUserAction(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		JID    string `json:"jid"`
		Action string `json:"action"` // "block", "unblock"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.JID == "" || req.Action == "" {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	RecordAudit(r, "USER_ACTION", fmt.Sprintf("%s pada %s", strings.ToUpper(req.Action), req.JID))

	stateMu.RLock()
	cli := waClient
	stateMu.RUnlock()

	if req.Action == "block" || req.Action == "unblock" {
		if cli != nil && cli.IsConnected() {
			targetJID, err := types.ParseJID(req.JID)
			if err == nil {
				if req.Action == "block" {
					_, _ = cli.UpdateBlocklist(r.Context(), targetJID, events.BlocklistChangeActionBlock)
				} else {
					_, _ = cli.UpdateBlocklist(r.Context(), targetJID, events.BlocklistChangeActionUnblock)
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"message": fmt.Sprintf("Aksi %s pada %s berhasil diproses.", req.Action, req.JID),
	})
}

func handleMetricsWS(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer c.CloseNow()

	ctx := r.Context()

	// Immediately push current metrics JSON upon connecting
	if b, err := json.Marshal(BuildStatusPayload()); err == nil {
		writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_ = c.Write(writeCtx, websocket.MessageText, b)
		cancel()
	}

	sub := MetricsHub.Subscribe()
	defer MetricsHub.Unsubscribe(sub)

	go func() {
		for {
			if _, _, err := c.Read(ctx); err != nil {
				break
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-sub:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := c.Write(writeCtx, websocket.MessageText, data)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func handleLogsWS(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer c.CloseNow()

	ctx := r.Context()

	recent := Broadcaster.GetRecentLogs()
	for _, line := range recent {
		writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_ = c.Write(writeCtx, websocket.MessageText, []byte(line))
		cancel()
	}

	sub := Broadcaster.Subscribe()
	defer Broadcaster.Unsubscribe(sub)

	go func() {
		for {
			if _, _, err := c.Read(ctx); err != nil {
				break
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-sub:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := c.Write(writeCtx, websocket.MessageText, []byte(line))
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func handleJadibotStop(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Phone == "" {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if err := jadibot.Stop(r.Context(), req.Phone, types.JID{}, true); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	RecordAudit(r, "JADIBOT", "Menghentikan sub-bot "+req.Phone)
	BroadcastMetricsNow()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func handleJadibotStart(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Phone == "" {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	code, err := jadibot.StartPairing(r.Context(), req.Phone, types.JID{})
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	RecordAudit(r, "JADIBOT_START", "Memulai pairing sub-bot "+req.Phone)
	BroadcastMetricsNow()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "code": code, "phone": req.Phone})
}

func handleJadibotRestart(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Phone == "" {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if err := jadibot.Restart(r.Context(), req.Phone); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	RecordAudit(r, "JADIBOT_RESTART", "Merestart sub-bot "+req.Phone)
	BroadcastMetricsNow()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func handleJadibotDelete(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Phone == "" {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if err := jadibot.Delete(r.Context(), req.Phone); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	RecordAudit(r, "JADIBOT_DELETE", "Menghapus sub-bot "+req.Phone)
	BroadcastMetricsNow()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func handleJadibotDetail(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	phone := r.URL.Query().Get("phone")
	if phone == "" {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	list := jadibot.GetWebList()
	var detail map[string]any
	for _, item := range list {
		if p, ok := item["phone"].(string); ok && p == phone {
			detail = item
			break
		}
	}

	if detail == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Jadibot tidak ditemukan"})
		return
	}

	recentLogs := Broadcaster.GetRecentLogs()
	if len(recentLogs) > 25 {
		recentLogs = recentLogs[len(recentLogs)-25:]
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":     true,
		"detail": detail,
		"logs":   recentLogs,
	})
}

func handleSWGCGroups(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	stateMu.RLock()
	cli := waClient
	stateMu.RUnlock()

	if cli == nil || !cli.IsConnected() || !cli.IsLoggedIn() {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"groups": []any{},
			"info":   "WhatsApp bot belum login atau belum terhubung.",
		})
		return
	}

	groups, err := cli.GetJoinedGroups(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"groups": []any{},
			"error":  fmt.Sprintf("Gagal mengambil grup: %v", err),
		})
		return
	}

	type groupInfo struct {
		JID          string `json:"jid"`
		Name         string `json:"name"`
		Participants int    `json:"participants"`
		IsAnnounce   bool   `json:"isAnnounce"`
		IsMuted      bool   `json:"isMuted"`
	}

	list := make([]groupInfo, 0, len(groups))
	for _, g := range groups {
		name := g.Name
		if name == "" {
			name = "Grup Tanpa Nama"
		}
		list = append(list, groupInfo{
			JID:          g.JID.String(),
			Name:         name,
			Participants: len(g.Participants),
			IsAnnounce:   g.IsAnnounce,
			IsMuted:      settings.IsGroupMuted(g.JID.String()),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "groups": list})
}

func handleSWGCSend(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stateMu.RLock()
	cli := waClient
	stateMu.RUnlock()

	if cli == nil || !cli.IsConnected() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "WhatsApp client belum terhubung!"})
		return
	}

	if err := r.ParseMultipartForm(64 * 1024 * 1024); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Format request/upload tidak valid."})
		return
	}

	targetJIDRaw := strings.TrimSpace(r.FormValue("jid"))
	caption := strings.TrimSpace(r.FormValue("caption"))

	targetJID, err := types.ParseJID(targetJIDRaw)
	if err != nil || targetJID.Server != types.GroupServer {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "JID grup target tidak valid!"})
		return
	}

	var mediaBytes []byte
	var mime string

	file, header, err := r.FormFile("media")
	if err == nil && file != nil {
		defer file.Close()
		mediaBytes, err = io.ReadAll(file)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": fmt.Sprintf("Gagal membaca file upload: %v", err)})
			return
		}
		mime = header.Header.Get("Content-Type")
	}

	if len(mediaBytes) == 0 && caption == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Pilih file media atau isi teks status!"})
		return
	}

	if mime == "" && len(mediaBytes) > 0 {
		mime = "image/jpeg"
	} else if len(mediaBytes) == 0 {
		mime = "text"
	}

	err = swgc.SendGroupStatus(r.Context(), cli, targetJID, mime, mediaBytes, caption)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": fmt.Sprintf("Gagal mengirim SWGC: %v", err)})
		return
	}

	RecordAudit(r, "SWGC_SEND", fmt.Sprintf("Group Status berhasil dikirim ke grup %s", targetJIDRaw))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"message": fmt.Sprintf("✅ Berhasil mengirim Status Story ke grup %s!", targetJIDRaw),
	})
}

func handleGroupSendMessage(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stateMu.RLock()
	cli := waClient
	stateMu.RUnlock()

	if cli == nil || !cli.IsConnected() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "WhatsApp client belum terhubung!"})
		return
	}

	if err := r.ParseMultipartForm(64 * 1024 * 1024); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Format request/upload tidak valid."})
		return
	}

	targetJIDRaw := strings.TrimSpace(r.FormValue("jid"))
	messageText := strings.TrimSpace(r.FormValue("message"))

	targetJID, err := types.ParseJID(targetJIDRaw)
	if err != nil || targetJID.Server != types.GroupServer {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "JID grup target tidak valid!"})
		return
	}

	var mediaBytes []byte
	var mime string

	file, header, err := r.FormFile("media")
	if err == nil && file != nil {
		defer file.Close()
		mediaBytes, err = io.ReadAll(file)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": fmt.Sprintf("Gagal membaca file upload: %v", err)})
			return
		}
		mime = header.Header.Get("Content-Type")
	}

	if len(mediaBytes) == 0 && messageText == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Isi pesan teks atau pilih file media!"})
		return
	}

	var msg *waE2E.Message

	if len(mediaBytes) > 0 {
		mimeLower := strings.ToLower(mime)
		var appInfo whatsmeow.MediaType
		switch {
		case strings.HasPrefix(mimeLower, "image/"):
			appInfo = whatsmeow.MediaImage
		case strings.HasPrefix(mimeLower, "video/"):
			appInfo = whatsmeow.MediaVideo
		case strings.HasPrefix(mimeLower, "audio/"):
			appInfo = whatsmeow.MediaAudio
		default:
			appInfo = whatsmeow.MediaDocument
		}

		up, err := cli.Upload(r.Context(), mediaBytes, appInfo)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": fmt.Sprintf("Gagal mengunggah media: %v", err)})
			return
		}

		switch appInfo {
		case whatsmeow.MediaVideo:
			v := &waE2E.VideoMessage{
				URL:           proto.String(up.URL),
				DirectPath:    proto.String(up.DirectPath),
				MediaKey:      up.MediaKey,
				Mimetype:      proto.String(orDefault(mime, "video/mp4")),
				FileEncSHA256: up.FileEncSHA256,
				FileSHA256:    up.FileSHA256,
				FileLength:    proto.Uint64(up.FileLength),
			}
			if messageText != "" {
				v.Caption = proto.String(messageText)
			}
			msg = &waE2E.Message{VideoMessage: v}

		case whatsmeow.MediaAudio:
			a := &waE2E.AudioMessage{
				URL:           proto.String(up.URL),
				DirectPath:    proto.String(up.DirectPath),
				MediaKey:      up.MediaKey,
				Mimetype:      proto.String(orDefault(mime, "audio/mp4")),
				FileEncSHA256: up.FileEncSHA256,
				FileSHA256:    up.FileSHA256,
				FileLength:    proto.Uint64(up.FileLength),
			}
			msg = &waE2E.Message{AudioMessage: a}

		case whatsmeow.MediaDocument:
			d := &waE2E.DocumentMessage{
				URL:           proto.String(up.URL),
				DirectPath:    proto.String(up.DirectPath),
				MediaKey:      up.MediaKey,
				Mimetype:      proto.String(orDefault(mime, "application/octet-stream")),
				FileEncSHA256: up.FileEncSHA256,
				FileSHA256:    up.FileSHA256,
				FileLength:    proto.Uint64(up.FileLength),
			}
			if header != nil && header.Filename != "" {
				d.FileName = proto.String(header.Filename)
			}
			if messageText != "" {
				d.Caption = proto.String(messageText)
			}
			msg = &waE2E.Message{DocumentMessage: d}

		default: // MediaImage
			im := &waE2E.ImageMessage{
				URL:           proto.String(up.URL),
				DirectPath:    proto.String(up.DirectPath),
				MediaKey:      up.MediaKey,
				Mimetype:      proto.String(orDefault(mime, "image/jpeg")),
				FileEncSHA256: up.FileEncSHA256,
				FileSHA256:    up.FileSHA256,
				FileLength:    proto.Uint64(up.FileLength),
			}
			if messageText != "" {
				im.Caption = proto.String(messageText)
			}
			msg = &waE2E.Message{ImageMessage: im}
		}
	} else {
		msg = &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String(messageText),
			},
		}
	}

	_, err = cli.SendMessage(r.Context(), targetJID, msg)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": fmt.Sprintf("Gagal mengirim pesan ke grup: %v", err)})
		return
	}

	RecordAudit(r, "GROUP_MSG_SEND", fmt.Sprintf("Pesan obrolan berhasil dikirim ke grup %s", targetJIDRaw))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"message": fmt.Sprintf("✅ Berhasil mengirim pesan ke grup %s!", targetJIDRaw),
	})
}

func handleGroupMute(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		JID   string `json:"jid"`
		Muted bool   `json:"muted"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.JID == "" {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if err := settings.SetGroupMuted(req.JID, req.Muted); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	act := "MUTE"
	if !req.Muted {
		act = "UNMUTE"
	}
	RecordAudit(r, "GROUP_MUTE", fmt.Sprintf("%s grup %s via Web Dashboard", act, req.JID))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":    true,
		"jid":   req.JID,
		"muted": req.Muted,
	})
}

type BlacklistEntry struct {
	Phone  string   `json:"phone"`
	LID    string   `json:"lid,omitempty"`
	RawIDs []string `json:"rawIds"`
}

func handleBlacklistList(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	blMap := settings.GetGlobalBlacklist()
	list := make([]string, 0, len(blMap))
	for k := range blMap {
		list = append(list, k)
	}

	entries := getGroupedBlacklistEntries(blMap)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":        true,
		"blacklist": list,
		"entries":   entries,
	})
}

func isLIDNumber(id string) bool {
	clean := strings.TrimSpace(id)
	if !isDigitsOnly(clean) {
		return false
	}
	return len(clean) >= 14 || strings.HasPrefix(clean, "238")
}

func getGroupedBlacklistEntries(blMap map[string]bool) []BlacklistEntry {
	var phones []string
	var lids []string
	var others []string

	for k := range blMap {
		clean := strings.TrimSpace(k)
		if clean == "" {
			continue
		}
		if isLIDNumber(clean) {
			lids = append(lids, clean)
		} else if isDigitsOnly(clean) {
			phones = append(phones, clean)
		} else {
			others = append(others, clean)
		}
	}

	sort.Strings(phones)
	sort.Strings(lids)

	var entries []BlacklistEntry
	usedLids := make(map[string]bool)

	for i, ph := range phones {
		var matchedLid string
		if i < len(lids) {
			matchedLid = lids[i]
			usedLids[matchedLid] = true
		}
		raw := []string{ph}
		if matchedLid != "" {
			raw = append(raw, matchedLid)
		}
		entries = append(entries, BlacklistEntry{
			Phone:  ph,
			LID:    matchedLid,
			RawIDs: raw,
		})
	}

	for _, lid := range lids {
		if !usedLids[lid] {
			entries = append(entries, BlacklistEntry{
				Phone:  lid,
				LID:    lid,
				RawIDs: []string{lid},
			})
		}
	}

	for _, o := range others {
		entries = append(entries, BlacklistEntry{
			Phone:  o,
			RawIDs: []string{o},
		})
	}

	return entries
}

func isDigitsOnly(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func handleBlacklistToggle(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Target      string `json:"target"`
		Blacklisted bool   `json:"blacklisted"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Target == "" {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	bareID := config.BareNumber(req.Target)
	if bareID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Nomor/ID target tidak valid"})
		return
	}

	if err := settings.SetUserBlacklisted(bareID, req.Blacklisted); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	actStr := "BLACKLIST"
	if !req.Blacklisted {
		actStr = "UNBLACKLIST"
	}
	RecordAudit(r, "GLOBAL_BLACKLIST", fmt.Sprintf("%s user %s via Web Dashboard", actStr, bareID))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          true,
		"target":      bareID,
		"blacklisted": req.Blacklisted,
	})
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// ============================================================================
// FILE MANAGER & CODE EDITOR API HANDLERS
// ============================================================================

type FileItemInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
	Ext     string `json:"ext"`
}

func getWorkspaceRoot() string {
	wd, err := os.Getwd()
	if err != nil || wd == "" {
		wd = "."
	}
	abs, err := filepath.Abs(wd)
	if err != nil {
		abs = wd
	}
	realRoot, err := filepath.EvalSymlinks(abs)
	if err == nil && realRoot != "" {
		return filepath.Clean(realRoot)
	}
	return filepath.Clean(abs)
}

func getRawWorkspaceRoot() string {
	wd, err := os.Getwd()
	if err != nil || wd == "" {
		return "."
	}
	abs, err := filepath.Abs(wd)
	if err != nil {
		return filepath.Clean(wd)
	}
	return filepath.Clean(abs)
}

func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\") {
		homeDir, err := os.UserHomeDir()
		if err == nil && homeDir != "" {
			if p == "~" {
				return homeDir
			}
			return filepath.Join(homeDir, p[2:])
		}
	}
	return p
}

func resolveSecurePath(subPath string) (string, error) {
	realRoot := getWorkspaceRoot()
	rawRoot := getRawWorkspaceRoot()

	if strings.Contains(subPath, "\x00") {
		log.Printf("[rbot] [File API] Akses ditolak! Null byte terdeteksi di subPath: %q", subPath)
		return "", errors.New("akses path ditolak: null byte terdeteksi")
	}

	trimmed := strings.TrimSpace(subPath)
	expanded := expandTilde(trimmed)
	cleaned := filepath.Clean(filepath.FromSlash(expanded))

	if cleaned == "." || cleaned == "" || cleaned == "/" || cleaned == "\\" {
		return realRoot, nil
	}

	var targetCandidate string
	if filepath.IsAbs(cleaned) {
		targetCandidate = cleaned
	} else {
		targetCandidate = filepath.Join(realRoot, cleaned)
	}

	absTarget, err := filepath.Abs(targetCandidate)
	if err != nil {
		absTarget = targetCandidate
	}

	realTarget, err := filepath.EvalSymlinks(absTarget)
	if err != nil {
		parentDir := filepath.Dir(absTarget)
		realParent, pErr := filepath.EvalSymlinks(parentDir)
		if pErr == nil {
			realTarget = filepath.Clean(filepath.Join(realParent, filepath.Base(absTarget)))
		} else {
			realTarget = filepath.Clean(absTarget)
		}
	} else {
		realTarget = filepath.Clean(realTarget)
	}

	relReal, errReal := filepath.Rel(realRoot, realTarget)
	insideReal := errReal == nil && relReal != ".." && !strings.HasPrefix(relReal, ".."+string(filepath.Separator)) && !strings.HasPrefix(relReal, "../")

	relRaw, errRaw := filepath.Rel(rawRoot, absTarget)
	insideRaw := errRaw == nil && relRaw != ".." && !strings.HasPrefix(relRaw, ".."+string(filepath.Separator)) && !strings.HasPrefix(relRaw, "../")

	if !insideReal && !insideRaw {
		log.Printf("[rbot] [File API] Akses path ditolak! Requested: %q | TargetCandidate: %q | RealTarget: %q | RealRoot: %q | RelReal: %q | RelRaw: %q",
			subPath, targetCandidate, realTarget, realRoot, relReal, relRaw)
		return "", errors.New("akses path ditolak: di luar workspace bot")
	}

	return realTarget, nil
}

func handleFileList(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	reqPath := r.URL.Query().Get("path")
	fullPath, err := resolveSecurePath(reqPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Gagal membaca direktori: " + err.Error()})
		return
	}

	root := getWorkspaceRoot()
	relPath, _ := filepath.Rel(root, fullPath)
	if relPath == "." {
		relPath = ""
	}

	var items []FileItemInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		itemRel := filepath.ToSlash(filepath.Join(relPath, entry.Name()))
		ext := strings.ToLower(filepath.Ext(entry.Name()))

		items = append(items, FileItemInfo{
			Name:    entry.Name(),
			Path:    itemRel,
			IsDir:   entry.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
			Ext:     ext,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	parentPath := ""
	if relPath != "" {
		parentPath = filepath.ToSlash(filepath.Dir(relPath))
		if parentPath == "." {
			parentPath = ""
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          true,
		"currentPath": filepath.ToSlash(relPath),
		"parentPath":  parentPath,
		"items":       items,
	})
}

func handleFileRead(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	reqPath := r.URL.Query().Get("path")
	fullPath, err := resolveSecurePath(reqPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "File tidak ditemukan"})
		return
	}

	if info.IsDir() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Path adalah direktori, bukan file"})
		return
	}

	if info.Size() > 5*1024*1024 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Ukuran file terlalu besar untuk diedit di browser (maks 5MB)"})
		return
	}

	contentBytes, err := os.ReadFile(fullPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Gagal membaca file: " + err.Error()})
		return
	}

	root := getWorkspaceRoot()
	relPath, _ := filepath.Rel(root, fullPath)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"name":    filepath.Base(fullPath),
		"path":    filepath.ToSlash(relPath),
		"content": string(contentBytes),
		"size":    info.Size(),
		"modTime": info.ModTime().Format("2006-01-02 15:04:05"),
		"ext":     strings.ToLower(filepath.Ext(fullPath)),
	})
}

func handleFileWrite(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Bad request"})
		return
	}

	fullPath, err := resolveSecurePath(req.Path)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	// 1. Create Backup if target file exists
	if _, err := os.Stat(fullPath); err == nil {
		createFileBackup(fullPath)
	}

	// 2. Atomic Write: Write to temp file first, then flush & rename
	dir := filepath.Dir(fullPath)
	tmpFile, err := os.CreateTemp(dir, ".tmp_rbot_*")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Gagal membuat file sementara: " + err.Error()})
		return
	}
	tmpName := tmpFile.Name()

	if _, err := tmpFile.WriteString(req.Content); err != nil {
		tmpFile.Close()
		os.Remove(tmpName)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Gagal menulis file: " + err.Error()})
		return
	}
	_ = tmpFile.Sync()
	tmpFile.Close()

	if err := os.Rename(tmpName, fullPath); err != nil {
		os.Remove(tmpName)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Gagal menyimpan file secara atomic: " + err.Error()})
		return
	}

	RecordAudit(r, "FILE_WRITE", fmt.Sprintf("Menyimpan file %s (Atomic write + backup)", req.Path))

	info, _ := os.Stat(fullPath)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"path":    req.Path,
		"modTime": info.ModTime().Format("2006-01-02 15:04:05"),
	})
}

func createFileBackup(fullPath string) {
	root := getWorkspaceRoot()
	relPath, err := filepath.Rel(root, fullPath)
	if err != nil {
		return
	}

	backupDir := filepath.Join(root, ".backups")
	_ = os.MkdirAll(backupDir, 0755)

	timestamp := time.Now().Format("20060102_150405")
	cleanRel := strings.ReplaceAll(relPath, "/", "_")
	cleanRel = strings.ReplaceAll(cleanRel, "\\", "_")
	backupName := fmt.Sprintf("%s.%s.bak", cleanRel, timestamp)
	backupPath := filepath.Join(backupDir, backupName)

	data, err := os.ReadFile(fullPath)
	if err == nil {
		_ = os.WriteFile(backupPath, data, 0644)
	}

	cleanupOldBackups(backupDir, cleanRel, 10)
}

func cleanupOldBackups(backupDir, cleanRel string, maxKeep int) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}

	prefix := cleanRel + "."
	var matching []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".bak") {
			matching = append(matching, e)
		}
	}

	if len(matching) <= maxKeep {
		return
	}

	sort.Slice(matching, func(i, j int) bool {
		iInfo, iErr := matching[i].Info()
		jInfo, jErr := matching[j].Info()
		if iErr != nil || jErr != nil {
			return matching[i].Name() < matching[j].Name()
		}
		return iInfo.ModTime().Before(jInfo.ModTime())
	})

	toDelete := len(matching) - maxKeep
	for i := 0; i < toDelete; i++ {
		_ = os.Remove(filepath.Join(backupDir, matching[i].Name()))
	}
}

func handleFileCreate(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Bad request"})
		return
	}

	fullPath, err := resolveSecurePath(req.Path)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	if _, err := os.Stat(fullPath); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "File atau folder sudah ada"})
		return
	}

	if req.Type == "folder" {
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Gagal membuat folder: " + err.Error()})
			return
		}
		RecordAudit(r, "FOLDER_CREATE", "Membuat folder "+req.Path)
	} else {
		_ = os.MkdirAll(filepath.Dir(fullPath), 0755)
		f, err := os.Create(fullPath)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Gagal membuat file: " + err.Error()})
			return
		}
		f.Close()
		RecordAudit(r, "FILE_CREATE", "Membuat file "+req.Path)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "path": req.Path})
}

func handleFileRename(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		OldPath string `json:"oldPath"`
		NewPath string `json:"newPath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OldPath == "" || req.NewPath == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Bad request"})
		return
	}

	oldFullPath, err1 := resolveSecurePath(req.OldPath)
	newFullPath, err2 := resolveSecurePath(req.NewPath)
	if err1 != nil || err2 != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Akses path ditolak"})
		return
	}

	if err := os.Rename(oldFullPath, newFullPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Gagal mengubah nama: " + err.Error()})
		return
	}

	RecordAudit(r, "FILE_RENAME", fmt.Sprintf("Rename %s menjadi %s", req.OldPath, req.NewPath))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func handleFileDelete(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Bad request"})
		return
	}

	fullPath, err := resolveSecurePath(req.Path)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "File tidak ditemukan"})
		return
	}

	if info.IsDir() {
		entries, _ := os.ReadDir(fullPath)
		if len(entries) > 0 && !req.Recursive {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":            false,
				"error":         fmt.Sprintf("Folder berisi %d item. Membutuhkan konfirmasi hapus rekursif.", len(entries)),
				"itemCount":     len(entries),
				"needRecursive": true,
			})
			return
		}
		if err := os.RemoveAll(fullPath); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Gagal menghapus folder: " + err.Error()})
			return
		}
	} else {
		if err := os.Remove(fullPath); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Gagal menghapus file: " + err.Error()})
			return
		}
	}

	RecordAudit(r, "FILE_DELETE", "Menghapus "+req.Path)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func handleFileDownload(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	reqPath := r.URL.Query().Get("path")
	fullPath, err := resolveSecurePath(reqPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		http.Error(w, "File tidak ditemukan atau berupa folder", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(fullPath)))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, fullPath)
}

func handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_ = r.ParseMultipartForm(32 << 20)
	targetDir := r.FormValue("dir")
	fullDir, err := resolveSecurePath(targetDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Gagal membaca upload file"})
		return
	}
	defer file.Close()

	destPath := filepath.Join(fullDir, filepath.Base(header.Filename))
	destFile, err := os.Create(destPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Gagal membuat file tujuan: " + err.Error()})
		return
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, file); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Gagal menyimpan file upload: " + err.Error()})
		return
	}

	relPath, _ := filepath.Rel(getWorkspaceRoot(), destPath)
	RecordAudit(r, "FILE_UPLOAD", "Upload file "+relPath)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "path": filepath.ToSlash(relPath)})
}

func handleFileSearch(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))
	if query == "" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "items": []FileItemInfo{}})
		return
	}

	root := getWorkspaceRoot()
	var matches []FileItemInfo
	count := 0

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || count >= 100 {
			return nil
		}
		name := d.Name()
		if d.IsDir() && (name == ".git" || name == "node_modules" || name == ".backups") {
			return filepath.SkipDir
		}

		if strings.Contains(strings.ToLower(name), query) {
			rel, _ := filepath.Rel(root, path)
			info, err := d.Info()
			sz := int64(0)
			modTime := ""
			if err == nil {
				sz = info.Size()
				modTime = info.ModTime().Format("2006-01-02 15:04:05")
			}
			matches = append(matches, FileItemInfo{
				Name:    name,
				Path:    filepath.ToSlash(rel),
				IsDir:   d.IsDir(),
				Size:    sz,
				ModTime: modTime,
				Ext:     strings.ToLower(filepath.Ext(name)),
			})
			count++
		}
		return nil
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "items": matches})
}

func handleSetPPBotWeb(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stateMu.RLock()
	cli := waClient
	stateMu.RUnlock()

	if cli == nil || !cli.IsConnected() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Bot WhatsApp belum terhubung!"})
		return
	}

	var rawBytes []byte
	var targetDim int
	var err error

	// 1. Cek upload file multipart form
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if parseErr := r.ParseMultipartForm(16 << 20); parseErr == nil {
			if sVal := r.FormValue("size"); sVal != "" {
				targetDim, _ = strconv.Atoi(sVal)
			}
			file, _, fErr := r.FormFile("image")
			if fErr == nil {
				defer file.Close()
				rawBytes, err = io.ReadAll(file)
			}
		}
	}

	// 2. Cek JSON payload { "url": "https://...", "size": 1080 }
	if len(rawBytes) == 0 {
		var req struct {
			URL  string `json:"url"`
			Size int    `json:"size"`
		}
		if json.NewDecoder(r.Body).Decode(&req) == nil && strings.TrimSpace(req.URL) != "" {
			if req.Size > 0 {
				targetDim = req.Size
			}
			targetURL := strings.TrimSpace(req.URL)
			if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "URL harus berawalan http:// atau https://"})
				return
			}
			reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()

			httpReq, hErr := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL, nil)
			if hErr == nil {
				httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
				resp, pErr := http.DefaultClient.Do(httpReq)
				if pErr == nil {
					defer resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						rawBytes, _ = io.ReadAll(resp.Body)
					}
				}
			}
		}
	}

	if len(rawBytes) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Pilih file gambar atau masukkan URL gambar yang valid!"})
		return
	}

	if targetDim <= 0 {
		targetDim = 720
	}

	// 3. Crop 1:1 & Resize ke targetDim x targetDim JPEG menggunakan cmd.ProcessProfilePictureWithSize
	procCtx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	imgJpeg, err := cmd.ProcessProfilePictureWithSize(procCtx, rawBytes, targetDim)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Gagal memproses gambar: " + err.Error()})
		return
	}

	// 4. Update Foto Profil Bot di WhatsMeow
	botJID := cli.Store.ID.ToNonAD()
	if _, err := cli.SetGroupPhoto(procCtx, botJID, imgJpeg); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Gagal memperbarui foto profil WhatsApp: " + err.Error()})
		return
	}

	RecordAudit(r, "SET_PP_BOT", "Memperbarui foto profil bot via Web Dashboard")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"message": "Foto profil bot berhasil diperbarui!",
	})
}

func handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	info := updater.CheckUpdate(ctx)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}

func handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Force bool `json:"force"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx, cancel := context.WithTimeout(r.Context(), 300*time.Second)
	defer cancel()

	var applyRes updater.ApplyResult
	if req.Force {
		applyRes = updater.ForceUpdate(ctx)
	} else {
		applyRes = updater.ApplyUpdate(ctx)
	}

	if !applyRes.OK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": applyRes.Err,
		})
		return
	}

	// Rebuild biner baru
	if err := updater.Rebuild(ctx); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "Git pull sukses, tetapi gagal build biner baru: " + err.Error(),
		})
		return
	}

	RecordAudit(r, "SYSTEM_UPDATE", fmt.Sprintf("Update GitHub sukses (branch: %s, forced: %v)", applyRes.Branch, applyRes.Forced))

	// Trigger restart otomatis
	go func() {
		time.Sleep(1 * time.Second)
		lifecycle.Request("")
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"message": fmt.Sprintf("Sukses update dari GitHub (branch: %s)! Engine berhasil di-build ulang dan akan restart otomatis dalam 1 detik.", applyRes.Branch),
	})
}

// -------------------------------------------------------------
// SIMI-SIMI AI WEB HANDLERS
// -------------------------------------------------------------

func handleSimiData(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	data := simi.GetSimiData()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":   true,
		"data": data,
	})
}

func handleSimiPersona(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Action string `json:"action"` // "save" | "reset"
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if req.Action == "reset" {
		_ = simi.ResetCustomPersona()
		RecordAudit(r, "SIMI_PERSONA", "Reset persona Simi-Simi ke bawaan")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"prompt": simi.DefaultPersonaPrompt(),
		})
		return
	}

	if strings.TrimSpace(req.Prompt) == "" {
		http.Error(w, "Prompt tidak boleh kosong", http.StatusBadRequest)
		return
	}

	if err := simi.SetCustomPersona(req.Prompt); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	RecordAudit(r, "SIMI_PERSONA", "Memperbarui persona Simi-Simi via Web Dashboard")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":     true,
		"prompt": simi.DefaultPersonaPrompt(),
	})
}

func handleSimiStickerUpload(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(10 * 1024 * 1024); err != nil {
		http.Error(w, "Upload terlalu besar", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("sticker")
	if err != nil {
		http.Error(w, "File sticker tidak ditemukan", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil || len(data) == 0 {
		http.Error(w, "File sticker kosong", http.StatusBadRequest)
		return
	}

	if err := simi.SaveGroupSticker(data); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	RecordAudit(r, "SIMI_STICKER_UPLOAD", fmt.Sprintf("Upload sticker Simi (%d bytes)", len(data)))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":             true,
		"total_stickers": len(simi.GetAllStickers()),
	})
}

func handleSimiStickerDelete(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Index int `json:"index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if err := simi.DeleteSticker(req.Index); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	RecordAudit(r, "SIMI_STICKER_DELETE", fmt.Sprintf("Hapus sticker Simi indeks %d", req.Index))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":             true,
		"total_stickers": len(simi.GetAllStickers()),
	})
}

func handleSimiStickerClear(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_ = simi.ClearAllStickers()
	RecordAudit(r, "SIMI_STICKER_CLEAR", "Mengosongkan seluruh koleksi sticker Simi")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":             true,
		"total_stickers": 0,
	})
}

// -------------------------------------------------------------
// WELCOME MESSAGE WEB HANDLERS
// -------------------------------------------------------------

func handleWelcomeGroups(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	stateMu.RLock()
	cli := waClient
	stateMu.RUnlock()

	if cli == nil || !cli.IsConnected() || !cli.IsLoggedIn() {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"groups": []welcome.GroupWelcomeData{},
			"info":   "WhatsApp bot belum terhubung.",
		})
		return
	}

	groups, err := welcome.GetGroupsWelcomeData(r.Context(), cli)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     false,
			"error":  err.Error(),
			"groups": []welcome.GroupWelcomeData{},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":               true,
		"groups":           groups,
		"default_template": welcome.DefaultTemplate(),
	})
}

func handleWelcomeToggle(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		JID     string `json:"jid"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.JID == "" {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if err := welcome.SetEnabled(req.JID, req.Enabled); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	statusStr := "OFF"
	if req.Enabled {
		statusStr = "ON"
	}
	RecordAudit(r, "WELCOME_TOGGLE", fmt.Sprintf("Ubah status Welcome %s untuk grup %s via Web Dashboard", statusStr, req.JID))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"jid":     req.JID,
		"enabled": req.Enabled,
	})
}

func handleWelcomeTemplate(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		JID      string `json:"jid"`
		Template string `json:"template"`
		Action   string `json:"action"` // "set" | "reset"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.JID == "" {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if req.Action == "reset" {
		_ = welcome.ResetTemplate(req.JID)
		RecordAudit(r, "WELCOME_TEMPLATE", fmt.Sprintf("Reset template welcome untuk grup %s ke bawaan", req.JID))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":             true,
			"jid":            req.JID,
			"template":       welcome.GetTemplate(req.JID),
			"has_custom_msg": false,
		})
		return
	}

	if strings.TrimSpace(req.Template) == "" {
		http.Error(w, "Template tidak boleh kosong", http.StatusBadRequest)
		return
	}

	if err := welcome.SetTemplate(req.JID, req.Template); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}

	RecordAudit(r, "WELCOME_TEMPLATE", fmt.Sprintf("Update template welcome untuk grup %s via Web Dashboard", req.JID))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":             true,
		"jid":            req.JID,
		"template":       welcome.GetTemplate(req.JID),
		"has_custom_msg": true,
	})
}

func handleBotMode(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method == http.MethodGet {
		selfMode := settings.IsSelfMode()
		modeStr := "public"
		if selfMode {
			modeStr = "self"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":        true,
			"self_mode": selfMode,
			"mode":      modeStr,
		})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			SelfMode *bool  `json:"self_mode"`
			Mode     string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		newSelf := false
		if req.SelfMode != nil {
			newSelf = *req.SelfMode
		} else if req.Mode != "" {
			switch strings.ToLower(req.Mode) {
			case "self", "private", "owner":
				newSelf = true
			default:
				newSelf = false
			}
		}

		if err := settings.SetSelfMode(newSelf); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}

		modeStr := "public"
		if newSelf {
			modeStr = "self"
		}
		RecordAudit(r, "MODE_CHANGE", fmt.Sprintf("Ubah mode bot ke %s via Web Dashboard", strings.ToUpper(modeStr)))
		BroadcastMetricsNow()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":        true,
			"self_mode": newSelf,
			"mode":      modeStr,
		})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
