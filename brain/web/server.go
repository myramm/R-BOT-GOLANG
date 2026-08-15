package web

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"go.mau.fi/whatsmeow"

	"rbot/brain/config"
	"rbot/brain/lifecycle"
	"rbot/brain/settings"
	"rbot/brain/stats"
)

//go:embed static/index.html
var indexHTML []byte

var (
	sessions   = make(map[string]time.Time)
	sessionsMu sync.RWMutex

	waClient    *whatsmeow.Client
	pairingCode string
	startTime   = time.Now()
	stateMu     sync.RWMutex
)

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
	sessions[token] = time.Now().Add(7 * 24 * time.Hour)
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
}

func SetPairingCode(code string) {
	stateMu.Lock()
	pairingCode = code
	stateMu.Unlock()
}

// Start mengaktifkan server web monitoring dan WebSocket terminal.
func Start(ctx context.Context) {
	InitLogger()

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
	mux.HandleFunc("/api/restart", handleRestart)
	mux.HandleFunc("/api/reload", handleReload)

	// WebSockets
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

	log.Printf("[rbot] Web monitoring & terminal aktif di http://%s (Password: %s)", addr, getPassword())

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

	if req.Password == getPassword() {
		token := createSession()
		http.SetCookie(w, &http.Cookie{
			Name:     "rbot_session",
			Value:    token,
			Path:     "/",
			Expires:  time.Now().Add(7 * 24 * time.Hour),
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    true,
			"token": token,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":    false,
		"error": "Password salah!",
	})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
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

	overview := stats.GetOverview()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
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
		},
		"config": map[string]any{
			"botName":     config.C.BotName,
			"prefix":      config.C.Prefix,
			"botNumber":   config.C.BotNumber,
			"ownerNumber": config.C.OwnerNumber,
			"logLevel":    config.C.LogLevel,
			"webPort":     config.C.Web.Port,
		},
		"stats":     overview,
		"topUsers":  stats.TopUsers(),
		"topGroups": stats.TopGroups(),
	})
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

func handleReload(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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
