// Package ai: obrolan AI WhatsApp. overchat.go adalah engine "claude" — memanggil
// endpoint chat Overchat (dipakai landing page mereka) dengan rotasi persona, dan
// fallback ke Pollinations bila semua persona gagal/kena rate-limit. Port
// lib/overchat.js. randomUUID Node → crypto/rand; pemilihan acak → math/rand
// (aman di kode runtime, beda dari skrip Workflow).
package ai

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	mrand "math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"rbot/lib/httpx"
)

const overchatAPI = "https://api.overchat.ai/v1/chat/completions"

// landingPersonas: id persona landing-page Overchat; dirotasi agar beban tersebar.
var landingPersonas = []string{
	"claude-opus-4-6-landing",
	"claude-opus-4-7-landing",
	"free-gpt-chat-landing",
	"best-free-ai-chat-landing",
	"gpt-4-5-landing",
	"gpt-5-2-landing",
	"deepseek-v3-1-landing",
	"ai-answer-generator-landing",
}

// userAgents: UA browser yang dirotasi (endpoint menolak UA non-browser).
var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
}

// Message adalah satu pesan chat (role: system|user|assistant).
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func randItem(arr []string) string { return arr[mrand.Intn(len(arr))] }

// uuid membuat UUIDv4 acak (pengganti crypto.randomUUID Node) untuk header
// x-device-uuid. Pakai crypto/rand agar tiap perangkat "unik".
func uuid() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // versi 4
	b[8] = (b[8] & 0x3f) | 0x80 // varian 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// shuffledPersonas mengembalikan salinan personas dengan urutan acak (Fisher-Yates,
// crypto/rand untuk indeks agar tak bergantung seed math/rand global).
func shuffledPersonas() []string {
	p := append([]string(nil), landingPersonas...)
	for i := len(p) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		j := i
		if err == nil {
			j = int(n.Int64())
		}
		p[i], p[j] = p[j], p[i]
	}
	return p
}

// AskOverchat mencoba tiap persona berurutan (acak) memanggil Overchat; balasan
// datang sebagai SSE (baris "data: {json}") yang di-rakit dari delta.content.
// Bila semua persona gagal → fallback Pollinations. timeout default 15s per persona.
func AskOverchat(ctx context.Context, messages []Message, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	var lastErr error
	payloadBase := map[string]any{"messages": messages}

	for _, persona := range shuffledPersonas() {
		payloadBase["personaId"] = persona
		body, _ := json.Marshal(payloadBase)
		headers := map[string]string{
			"Content-Type":      "application/json",
			"User-Agent":        randItem(userAgents),
			"Origin":            "https://overchat.ai",
			"Referer":           "https://overchat.ai/web",
			"x-device-platform": "web",
			"x-device-version":  "1.0.0",
			"x-device-language": "en",
			"x-device-uuid":     uuid(),
		}
		resp, err := httpx.Do(ctx, http.MethodPost, overchatAPI, strings.NewReader(string(body)), timeout, headers)
		if err != nil {
			lastErr = err
			continue
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("Overchat API %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
			continue
		}

		var full strings.Builder
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			jsonStr := strings.TrimSpace(line[6:])
			if jsonStr == "[DONE]" {
				continue
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					Text string `json:"text"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(jsonStr), &chunk); err != nil || len(chunk.Choices) == 0 {
				continue
			}
			c := chunk.Choices[0]
			if c.Delta.Content != "" {
				full.WriteString(c.Delta.Content)
			} else if c.Text != "" {
				full.WriteString(c.Text)
			}
		}
		if out := strings.TrimSpace(full.String()); out != "" {
			return out, nil
		}
	}

	// Fallback: Pollinations (kena kuota/rate-limit di semua persona).
	if out, err := fallbackPollinations(ctx, messages); err == nil {
		return out, nil
	} else if lastErr == nil {
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("Overchat tidak memberikan jawaban")
	}
	return "", lastErr
}

// fallbackPollinations memakai text.pollinations.ai (GET prompt di path) sebagai
// AI cadangan; hanya pesan terakhir yang dikirim sebagai prompt.
func fallbackPollinations(ctx context.Context, messages []Message) (string, error) {
	last := "Halo"
	if n := len(messages); n > 0 && messages[n-1].Content != "" {
		last = messages[n-1].Content
	}
	u := "https://text.pollinations.ai/" + url.PathEscape(last)
	resp, err := httpx.Do(ctx, http.MethodGet, u, nil, 20*time.Second, map[string]string{"User-Agent": randItem(userAgents)})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Pollinations HTTP %d", resp.StatusCode)
	}
	if out := strings.TrimSpace(string(raw)); out != "" {
		return out, nil
	}
	return "", fmt.Errorf("Fallback AI tidak memberikan jawaban")
}
