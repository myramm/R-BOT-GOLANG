package errortracker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"rbot/brain/store"
)

// AiFixConfig menyimpan konfigurasi API AI untuk fitur auto-diagnostik error.
type AiFixConfig struct {
	Provider    string  `json:"provider"`    // "openai" (OpenAI / OpenRouter / Groq / Ollama / Custom) | "gemini"
	ApiUrl      string  `json:"apiUrl"`      // e.g. "https://api.openai.com/v1/chat/completions" or custom
	ApiKey      string  `json:"apiKey"`      // API Key
	Model       string  `json:"model"`       // e.g. "gpt-4o-mini", "gemini-1.5-flash", "deepseek-chat"
	Temperature float64 `json:"temperature"` // default 0.3
}

var defaultAiFixConfig = AiFixConfig{
	Provider:    "openai",
	ApiUrl:      "https://api.openai.com/v1/chat/completions",
	ApiKey:      "",
	Model:       "gpt-4o-mini",
	Temperature: 0.3,
}

// GetAiFixConfig mengambil konfigurasi AI Fixer dari store.
func GetAiFixConfig() AiFixConfig {
	var cfg AiFixConfig
	found, err := store.Get("aifix_config", &cfg)
	if err == nil && found {
		if cfg.Provider == "" {
			cfg.Provider = "openai"
		}
		if cfg.ApiUrl == "" {
			cfg.ApiUrl = "https://api.openai.com/v1/chat/completions"
		}
		if cfg.Model == "" {
			cfg.Model = "gpt-4o-mini"
		}
		if cfg.Temperature <= 0 {
			cfg.Temperature = 0.3
		}
		return cfg
	}
	return defaultAiFixConfig
}

// SetAiFixConfig menyimpan konfigurasi AI Fixer ke store.
func SetAiFixConfig(cfg AiFixConfig) error {
	if cfg.Provider == "" {
		cfg.Provider = "openai"
	}
	if cfg.ApiUrl == "" {
		cfg.ApiUrl = "https://api.openai.com/v1/chat/completions"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	if cfg.Temperature <= 0 {
		cfg.Temperature = 0.3
	}
	return store.Set("aifix_config", cfg)
}

// AnalyzeErrorWithAi mengirimkan data error ke API AI yang telah dikonfigurasi untuk mendapatkan diagnosa & solusi perbaikan.
func AnalyzeErrorWithAi(ctx context.Context, entry ErrorEntry) (string, error) {
	cfg := GetAiFixConfig()
	if strings.TrimSpace(cfg.ApiKey) == "" {
		return "", errors.New("API Key AI belum dikonfigurasi. Silakan atur API Key di Settings / Error Center")
	}

	systemPrompt := `Anda adalah AI Senior Software Engineer & Debugger Ahli untuk bot WhatsApp berbasis Golang (WhatsMeow).
Tugas Anda adalah menganalisis log error sistem yang diberikan, menemukan akar penyebabnya (Root Cause), dan memberikan langkah-langkah solusi perbaikan yang konkret serta contoh potongan kode patch jika relevan.

Formatkan jawaban Anda dalam Markdown terstruktur dengan bagian:
1. 🔍 **Analisis Masalah & Root Cause**: Penjelasan ringkas apa yang menyebabkan error terjadi.
2. 💡 **Langkah Solusi / Rekomendasi**: Panduan langkah perbaikan.
3. 📝 **Rekomendasi Kode / Patch (Jika relevan)**: Contoh kode Golang / config yang benar dalam code block.
4. ⚡ **Tindakan Instan**: Rekomendasi apakah perlu restart, cek koneksi, cek token, atau update file.`

	userPrompt := fmt.Sprintf(`Berikut adalah data error sistem bot yang perlu dianalisis:
- **Sumber**: %s
- **Waktu Pertama Ditemukan**: %s
- **Terakhir Muncul**: %s
- **Frekuensi Terjadi**: %d kali
- **Pesan Error**: %s
- **Konteks / Log Detail**:
%s

Mohon berikan analisis diagnostik dan solusi perbaikannya.`,
		entry.Source,
		entry.TimeStr,
		entry.LastSeenStr,
		entry.Count,
		entry.Message,
		entry.Context,
	)

	client := &http.Client{Timeout: 45 * time.Second}

	if cfg.Provider == "gemini" || strings.Contains(cfg.ApiUrl, "generativelanguage.googleapis.com") {
		return callGeminiAi(ctx, client, cfg, systemPrompt, userPrompt)
	}

	return callOpenAiCompatible(ctx, client, cfg, systemPrompt, userPrompt)
}

func callOpenAiCompatible(ctx context.Context, client *http.Client, cfg AiFixConfig, systemPrompt, userPrompt string) (string, error) {
	reqBody := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": cfg.Temperature,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	apiUrl := cfg.ApiUrl
	if apiUrl == "" {
		apiUrl = "https://api.openai.com/v1/chat/completions"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.ApiKey))

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gagal request ke API AI (%s): %w", apiUrl, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("baca response AI: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("API AI error (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return "", fmt.Errorf("parse JSON response AI: %w", err)
	}

	if res.Error != nil && res.Error.Message != "" {
		return "", fmt.Errorf("AI Error: %s", res.Error.Message)
	}

	if len(res.Choices) == 0 || strings.TrimSpace(res.Choices[0].Message.Content) == "" {
		return "", errors.New("tidak ada respon balasan dari API AI")
	}

	return strings.TrimSpace(res.Choices[0].Message.Content), nil
}

func callGeminiAi(ctx context.Context, client *http.Client, cfg AiFixConfig, systemPrompt, userPrompt string) (string, error) {
	apiUrl := cfg.ApiUrl
	if apiUrl == "" || strings.Contains(apiUrl, "chat/completions") {
		model := cfg.Model
		if model == "" {
			model = "gemini-1.5-flash"
		}
		apiUrl = fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, strings.TrimSpace(cfg.ApiKey))
	} else if !strings.Contains(apiUrl, "key=") {
		if strings.Contains(apiUrl, "?") {
			apiUrl += "&key=" + strings.TrimSpace(cfg.ApiKey)
		} else {
			apiUrl += "?key=" + strings.TrimSpace(cfg.ApiKey)
		}
	}

	reqBody := map[string]any{
		"systemInstruction": map[string]any{
			"parts": []map[string]string{
				{"text": systemPrompt},
			},
		},
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]string{
					{"text": userPrompt},
				},
			},
		},
		"generationConfig": map[string]any{
			"temperature": cfg.Temperature,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gagal request ke Gemini API: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("baca response Gemini: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Gemini API error (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return "", fmt.Errorf("parse JSON response Gemini: %w", err)
	}

	if res.Error != nil && res.Error.Message != "" {
		return "", fmt.Errorf("Gemini Error: %s", res.Error.Message)
	}

	if len(res.Candidates) == 0 || len(res.Candidates[0].Content.Parts) == 0 {
		return "", errors.New("tidak ada respon balasan dari Gemini")
	}

	return strings.TrimSpace(res.Candidates[0].Content.Parts[0].Text), nil
}
