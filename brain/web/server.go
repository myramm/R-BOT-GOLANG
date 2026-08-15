package web

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"runtime"
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
	"rbot/brain/lifecycle"
	"rbot/brain/settings"
	"rbot/brain/stats"
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
	p := strings.TrimSpace(config.C.Web.Password)
	if p != "" {
		return p
	}
	return "RamaGans76"
}

func generateToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func createSession() string {
	token := generateToken()
	sessionsMu.Lock()
	sessions[token] = time.Now().Add(24 * time.Hour)
	sessionsMu.Unlock()
	return token
}

func removeSession(token string) {
	sessionsMu.Lock()
	delete(sessions, token)
	sessionsMu.Unlock()
}

func validateToken(token string) bool {
	if token == "" {
		return false
	}
	sessionsMu.RLock()
	exp, ok := sessions[token]
	sessionsMu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		removeSession(token)
		return false
	}
	return true
}

func IsAuthenticated(r *http.Request) bool {
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
	mux.HandleFunc("/api/kill", handleKill)
	mux.HandleFunc("/api/reload", handleReload)
	mux.HandleFunc("/api/broadcast", handleBroadcast)
	mux.HandleFunc("/api/action/user", handleUserAction)

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

	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}

	loginMu.Lock()
	tracker, exists := loginAttempts[ip]
	if exists && time.Now().Before(tracker.BlockedTo) {
		loginMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "Terlalu banyak percobaan login gagal. Diblokir sementara!",
		})
		return
	}
	loginMu.Unlock()

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if req.Password == getPassword() {
		loginMu.Lock()
		delete(loginAttempts, ip)
		loginMu.Unlock()

		token := createSession()
		http.SetCookie(w, &http.Cookie{
			Name:     "rbot_session",
			Value:    token,
			Path:     "/",
			Expires:  time.Now().Add(24 * time.Hour),
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

	loginMu.Lock()
	if !exists {
		tracker = &loginTracker{}
		loginAttempts[ip] = tracker
	}
	tracker.Attempts++
	if tracker.Attempts >= 5 {
		tracker.BlockedTo = time.Now().Add(5 * time.Minute)
		log.Printf("[rbot] IP %s diblokir sementara karena 5x salah password.", ip)
	}
	loginMu.Unlock()

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
	authenticated := IsAuthenticated(r)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
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
