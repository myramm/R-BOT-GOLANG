// Package ai: obrolan AI WhatsApp. ai.go memegang katalog model, preset role,
// sesi per-user (in-memory, hidup selama proses), dan dispatcher AskAI: model
// "claude"/"overchat" → engine Overchat (overchat.go), sisanya → OpenRouter
// dengan fallback berputar ke model lain bila satu gagal. Port lib/commands/ai.js
// bagian non-handler. Sesi dijaga mutex karena event WhatsApp bisa paralel.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"rbot/brain/config"
	"rbot/lib/httpx"
)

const (
	openrouterAPI = "https://openrouter.ai/api/v1/chat/completions"
	timeoutAI     = 60 * time.Second

	maxTurns          = 10
	maxUsers          = 100
	idleDur           = 30 * time.Minute
	MaxReasoningChars = 1500
)

// Model mendeskripsikan satu pilihan model AI.
type Model struct {
	ID       string
	Name     string
	Provider string // "openrouter" | "overchat"
}

// Models adalah katalog model (port AVAILABLE_MODELS). Urutannya = nomor pilihan.
var Models = []Model{
	{ID: "openrouter/free", Name: "Auto", Provider: "openrouter"},
	{ID: "claude", Name: "Claude AI (Overchat Engine)", Provider: "overchat"},
	{ID: "google/gemma-4-26b-a4b-it:free", Name: "Google Gemma 4 26B", Provider: "openrouter"},
	{ID: "nvidia/nemotron-3-super-120b-a12b:free", Name: "Nvidia Nemotron Super 120B", Provider: "openrouter"},
	{ID: "inclusionai/ling-3.0-flash:free", Name: "Ling 3.0 Flash", Provider: "openrouter"},
	{ID: "nvidia/nemotron-3-nano-30b-a3b:free", Name: "Nvidia Nemotron Nano 30B", Provider: "openrouter"},
	{ID: "deepseek/deepseek-r1:free", Name: "DeepSeek R1 (Free)", Provider: "openrouter"},
	{ID: "meta-llama/llama-3.3-70b-instruct:free", Name: "Meta Llama 3.3 70B", Provider: "openrouter"},
}

// DefaultModel adalah model awal tiap sesi baru.
const DefaultModel = "openrouter/free"

// PresetRoles: system-prompt siap-pakai (port PRESET_ROLES).
var PresetRoles = map[string]string{
	"programmer": "Kamu adalah Software Engineer senior yang ahli dalam coding, debugging, dan arsitektur perangkat lunak. Jawab dengan kode yang efisien dan penjelasan teknis yang jelas.",
	"translator": "Kamu adalah penerjemah bahasa profesional. Terjemahkan dan jelaskan tatabahasa serta padanan kata secara akurat dan natural.",
	"anime":      "Kamu adalah karakter anime yang ramah, sopan, ekspresif, dan ceria. Gunakan gaya bicara ala anime yang imut dan menyenangkan!",
	"formal":     "Kamu adalah asisten eksekutif formal dan profesional. Jawab selalu dengan bahasa baku, sopan, terstruktur, dan analisis mendalam.",
	"santai":     "Kamu adalah teman obrolan yang santai, gaul, humoris, dan asyik diajak berdiskusi tentang apa saja.",
}

// RoleOrder menjaga urutan tampil daftar role (map Go tak berurut).
var RoleOrder = []string{"programmer", "translator", "anime", "formal", "santai"}

// DefaultSystemPrompt mengambil systemPrompt dari config, fallback ke bawaan.
func DefaultSystemPrompt() string {
	if p := strings.TrimSpace(config.C.AI.SystemPrompt); p != "" {
		return p
	}
	return "Kamu adalah R-BOT, asisten AI WhatsApp yang cerdas, ramah, dan helpful. " +
		"Jawab selalu dalam bahasa Indonesia kecuali pengguna minta bahasa lain. " +
		"Jawab langsung, jelas, dan to-the-point. Jangan pura-pura jadi AI lain."
}

// FindModel mencari model berdasar id persis. nil bila tak ada.
func FindModel(id string) *Model {
	for i := range Models {
		if Models[i].ID == id {
			return &Models[i]
		}
	}
	return nil
}

// ModelName mengembalikan nama tampilan model, fallback ke id lalu "AI".
func ModelName(id string) string {
	if m := FindModel(id); m != nil {
		return m.Name
	}
	if id != "" {
		return id
	}
	return "AI"
}

// Session adalah state obrolan seorang user (in-memory, hidup selama proses).
type Session struct {
	Messages       []Message
	LastAccess     time.Time
	ShowReasoning  bool
	SelectedModel  string
	TemporaryChat  bool
	ContextReply   bool
	CustomRole     string
	CustomRoleName string
}

var (
	mu       sync.Mutex
	sessions = map[string]*Session{}
)

// prune membuang sesi idle (>30 menit) & memangkas ke MAX_USERS (buang yang
// paling lama tak diakses). Dipanggil di bawah mu.
func prune(now time.Time) {
	for id, s := range sessions {
		if now.Sub(s.LastAccess) > idleDur {
			delete(sessions, id)
		}
	}
	for len(sessions) > maxUsers {
		var oldestID string
		var oldest time.Time
		first := true
		for id, s := range sessions {
			if first || s.LastAccess.Before(oldest) {
				oldest, oldestID, first = s.LastAccess, id, false
			}
		}
		if oldestID == "" {
			break
		}
		delete(sessions, oldestID)
	}
}

// Update mengunci sesi, prune, ambil/buat sesi milik senderID, lalu jalankan fn
// dengan sesi itu. Semua mutasi field sesi WAJIB lewat sini (fn tak boleh
// melakukan I/O jaringan agar lock tak ditahan lama). Port getSession+pruneSessions.
func Update(senderID string, now time.Time, fn func(*Session)) {
	mu.Lock()
	defer mu.Unlock()
	prune(now)
	s := sessions[senderID]
	if s == nil {
		s = &Session{
			SelectedModel: DefaultModel,
			ContextReply:  true,
		}
		sessions[senderID] = s
	}
	s.LastAccess = now
	fn(s)
}

// Clear menghapus sesi user; true bila memang ada. Port clearSession.
func Clear(senderID string) bool {
	mu.Lock()
	defer mu.Unlock()
	_, ok := sessions[senderID]
	delete(sessions, senderID)
	return ok
}

// Answer adalah hasil satu panggilan AI.
type Answer struct {
	Content   string
	Reasoning string
}

// AskAI merutekan ke engine sesuai model: "claude"/"overchat" → Overchat; selain
// itu → OpenRouter, dan bila model itu gagal, coba model lain satu per satu
// (fallback berputar). Port askAI.
func AskAI(ctx context.Context, modelID string, messages []Message) (Answer, error) {
	if modelID == "claude" || modelID == "overchat" {
		out, err := AskOverchat(ctx, messages, 35*time.Second)
		if err != nil {
			return Answer{}, err
		}
		return Answer{Content: out}, nil
	}

	ans, err := askOpenRouter(ctx, modelID, messages)
	if err == nil {
		return ans, nil
	}
	firstErr := err
	for _, m := range Models {
		if m.ID == modelID {
			continue
		}
		if m.Provider == "overchat" {
			if out, e := AskOverchat(ctx, messages, 15*time.Second); e == nil {
				return Answer{Content: out}, nil
			}
			continue
		}
		if a, e := askOpenRouter(ctx, m.ID, messages); e == nil {
			return a, nil
		}
	}
	return Answer{}, firstErr
}

// askOpenRouter memanggil satu model OpenRouter (POST Bearer API key). Mengembalikan
// content + reasoning (bila model menyertakan). Port askOpenRouterModel.
func askOpenRouter(ctx context.Context, model string, messages []Message) (Answer, error) {
	body, _ := json.Marshal(map[string]any{"model": model, "messages": messages})
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + config.C.AI.APIKey,
	}
	resp, err := httpx.Do(ctx, http.MethodPost, openrouterAPI, strings.NewReader(string(body)), timeoutAI, headers)
	if err != nil {
		return Answer{}, err
	}
	defer resp.Body.Close()

	var data struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&data)

	if resp.StatusCode != http.StatusOK {
		if data.Error.Message != "" {
			return Answer{}, fmt.Errorf("%s", data.Error.Message)
		}
		return Answer{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if len(data.Choices) == 0 || strings.TrimSpace(data.Choices[0].Message.Content) == "" {
		return Answer{}, fmt.Errorf("respon kosong dari API")
	}
	c := data.Choices[0].Message
	return Answer{Content: strings.TrimSpace(c.Content), Reasoning: strings.TrimSpace(c.Reasoning)}, nil
}
