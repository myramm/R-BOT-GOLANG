// Package errortracker mencatat, mengagregasi, dan menganalisis error sistem serta menyediakan fitur Fix with AI.
package errortracker

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"rbot/brain/store"
)

// ErrorEntry merepresentasikan satu entri error terstruktur pada sistem bot.
type ErrorEntry struct {
	ID         string    `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	TimeStr    string    `json:"timeStr"`
	Source     string    `json:"source"` // "COMMAND" | "WHATSAPP" | "JADIBOT" | "SYSTEM" | "WEB_API" | "DATABASE" | "SIMI"
	Message    string    `json:"message"`
	Context    string    `json:"context,omitempty"`
	Count      int64     `json:"count"`
	LastSeen   time.Time `json:"lastSeen"`
	LastSeenStr string   `json:"lastSeenStr"`
	Status     string    `json:"status"` // "unresolved" | "resolved"
	AiAnalysis string    `json:"aiAnalysis,omitempty"`
}

type Tracker struct {
	mu     sync.Mutex
	errors []ErrorEntry
	max    int
	loaded bool
}

var DefaultTracker = &Tracker{
	errors: make([]ErrorEntry, 0, 100),
	max:    150,
}

// Load membaca riwayat error dari DB store ke memori.
func Load() error {
	return DefaultTracker.Load()
}

func (t *Tracker) Load() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	var saved []ErrorEntry
	found, err := store.Get("system_errors_registry", &saved)
	if err != nil {
		return err
	}
	if found && saved != nil {
		t.errors = saved
	}
	t.loaded = true
	return nil
}

func (t *Tracker) ensureLoadedLocked() {
	if t.loaded {
		return
	}
	var saved []ErrorEntry
	found, err := store.Get("system_errors_registry", &saved)
	if err == nil && found && saved != nil {
		t.errors = saved
		t.loaded = true
	}
}

func generateID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (t *Tracker) save() {
	_ = store.Set("system_errors_registry", t.errors)
}

// RecordError mencatat error baru atau menambahkan hit count jika error serupa sudah ada.
func RecordError(source, message, errContext string) {
	DefaultTracker.RecordError(source, message, errContext)
}

func (t *Tracker) RecordError(source, message, errContext string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	if source == "" {
		source = "SYSTEM"
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.ensureLoadedLocked()

	now := time.Now()
	nowStr := now.Format("2006-01-02 15:04:05")

	// Cek apakah error serupa sudah tercatat baru-baru ini untuk deduplikasi
	for i := range t.errors {
		if t.errors[i].Source == source && t.errors[i].Message == message && t.errors[i].Status == "unresolved" {
			t.errors[i].Count++
			t.errors[i].LastSeen = now
			t.errors[i].LastSeenStr = nowStr
			if errContext != "" {
				t.errors[i].Context = errContext
			}
			t.save()
			return
		}
	}

	entry := ErrorEntry{
		ID:          generateID(),
		Timestamp:   now,
		TimeStr:     nowStr,
		Source:      source,
		Message:     message,
		Context:     errContext,
		Count:       1,
		LastSeen:    now,
		LastSeenStr: nowStr,
		Status:      "unresolved",
	}

	t.errors = append([]ErrorEntry{entry}, t.errors...)
	if len(t.errors) > t.max {
		t.errors = t.errors[:t.max]
	}
	t.save()
}

// GetErrors mengembalikan daftar error terdaftar.
func GetErrors() []ErrorEntry {
	return DefaultTracker.GetErrors()
}

func (t *Tracker) GetErrors() []ErrorEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ensureLoadedLocked()
	out := make([]ErrorEntry, len(t.errors))
	copy(out, t.errors)
	return out
}

// GetErrorByID mencari error berdasarkan ID.
func GetErrorByID(id string) (ErrorEntry, bool) {
	return DefaultTracker.GetErrorByID(id)
}

func (t *Tracker) GetErrorByID(id string) (ErrorEntry, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ensureLoadedLocked()
	for _, e := range t.errors {
		if e.ID == id {
			return e, true
		}
	}
	return ErrorEntry{}, false
}

// DeleteError menghapus error tertentu berdasarkan ID.
func DeleteError(id string) bool {
	return DefaultTracker.DeleteError(id)
}

func (t *Tracker) DeleteError(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ensureLoadedLocked()
	found := false
	filtered := make([]ErrorEntry, 0, len(t.errors))
	for _, e := range t.errors {
		if e.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, e)
	}
	if found {
		t.errors = filtered
		t.save()
	}
	return found
}

// ClearErrors membersihkan seluruh riwayat error.
func ClearErrors() {
	DefaultTracker.ClearErrors()
}

func (t *Tracker) ClearErrors() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ensureLoadedLocked()
	t.errors = make([]ErrorEntry, 0, 100)
	t.save()
}

// UpdateAiAnalysis menyimpan hasil diagnosa AI pada entri error.
func UpdateAiAnalysis(id string, analysis string) bool {
	return DefaultTracker.UpdateAiAnalysis(id, analysis)
}

func (t *Tracker) UpdateAiAnalysis(id string, analysis string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ensureLoadedLocked()
	for i := range t.errors {
		if t.errors[i].ID == id {
			t.errors[i].AiAnalysis = analysis
			t.save()
			return true
		}
	}
	return false
}

// GetSummary mengembalikan statistik ringkas error untuk metrics web.
func GetSummary() map[string]any {
	return DefaultTracker.GetSummary()
}

func (t *Tracker) GetSummary() map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ensureLoadedLocked()

	total := len(t.errors)
	unresolved := 0
	sources := make(map[string]int64)

	for _, e := range t.errors {
		if e.Status == "unresolved" {
			unresolved++
		}
		sources[e.Source] += e.Count
	}

	return map[string]any{
		"total":      total,
		"unresolved": unresolved,
		"sources":    sources,
	}
}

// ParseLogLine memeriksa apakah baris log adalah pesan log berlevel ERROR, FATAL, atau PANIC.
// Hanya log yang benar-benar berlevel error (berwarna merah / log level ERROR) yang dicatat ke Error Tracker,
// bukan sembarang baris teks yang kebetulan memuat kata "error" atau "failed".
func ParseLogLine(line string) {
	lower := strings.ToLower(line)

	// Filter keluar log info, debug, warn, tracker, atau metrik normal
	if strings.Contains(lower, "[info]") || strings.Contains(lower, "level=info") ||
		strings.Contains(lower, "[debug]") || strings.Contains(lower, "level=debug") ||
		strings.Contains(lower, "[warn]") || strings.Contains(lower, "level=warn") ||
		strings.Contains(lower, "errorcount") || strings.Contains(lower, "errortracker") ||
		strings.Contains(lower, "top command error") || strings.Contains(lower, "no error") ||
		strings.Contains(lower, "0 error") || strings.Contains(lower, "errors: 0") ||
		strings.Contains(lower, "metrics") || strings.Contains(lower, "[audit]") {
		return
	}

	// Hanya proses jika memiliki penanda log level ERROR / FATAL / PANIC eksplisit
	isRealError := strings.Contains(lower, "[error]") ||
		strings.Contains(lower, "level=error") ||
		strings.Contains(lower, "level=\"error\"") ||
		strings.Contains(lower, "[fatal]") ||
		strings.Contains(lower, "level=fatal") ||
		strings.Contains(lower, "level=\"fatal\"") ||
		strings.Contains(lower, "panic:") ||
		strings.Contains(lower, "fatal error:") ||
		strings.Contains(lower, "[panic]")

	if !isRealError {
		return
	}

	source := "SYSTEM"
	if strings.Contains(lower, "[command]") || strings.Contains(lower, "command") {
		source = "COMMAND"
	} else if strings.Contains(lower, "[jadibot]") || strings.Contains(lower, "jadibot") {
		source = "JADIBOT"
	} else if strings.Contains(lower, "[simi]") {
		source = "SIMI"
	} else if strings.Contains(lower, "[web]") || strings.Contains(lower, "http:") {
		source = "WEB_API"
	} else if strings.Contains(lower, "whatsmeow") || strings.Contains(lower, "whatsapp") {
		source = "WHATSAPP"
	}

	cleanMsg := strings.TrimSpace(line)
	if len(cleanMsg) > 300 {
		cleanMsg = cleanMsg[:300] + "…"
	}

	RecordError(source, cleanMsg, line)
}
