package simi

import (
	"context"
	"os"
	"strings"
	"testing"

	"rbot/brain/config"
	"rbot/brain/store"
)

func setupTestStore(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := store.Open(dir); err != nil {
		t.Fatalf("buka store test: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
		_ = os.RemoveAll(dir)
	})
}

func TestCooldown(t *testing.T) {
	user := "user-test-1"
	if !CheckCooldown(user) {
		t.Errorf("cooldown pertama harus true (diizinkan)")
	}
	if CheckCooldown(user) {
		t.Errorf("cooldown kedua langsung harus false (terkena rate limit)")
	}
}

func TestChatToggle(t *testing.T) {
	setupTestStore(t)
	config.C.Simi.EnabledByDefault = true

	chat1 := "group-123@g.us"
	if !IsEnabled(chat1) {
		t.Errorf("default harus enabled")
	}

	if err := SetEnabled(chat1, false); err != nil {
		t.Fatalf("SetEnabled gagal: %v", err)
	}

	if IsEnabled(chat1) {
		t.Errorf("status harus disabled setelah SetEnabled(false)")
	}

	if err := SetEnabled(chat1, true); err != nil {
		t.Fatalf("SetEnabled gagal: %v", err)
	}

	if !IsEnabled(chat1) {
		t.Errorf("status harus enabled setelah SetEnabled(true)")
	}
}

func TestStickerSaveAndGet(t *testing.T) {
	setupTestStore(t)

	_, ok := GetRandomSticker()
	if ok {
		t.Errorf("database sticker kosong tidak boleh mengembalikan ok")
	}

	dummySticker := []byte("RIFF1234WEBPVP8Xdummydata")
	if err := SaveGroupSticker(dummySticker); err != nil {
		t.Fatalf("SaveGroupSticker gagal: %v", err)
	}

	got, ok := GetRandomSticker()
	if !ok {
		t.Fatalf("GetRandomSticker harus sukses setelah ada sticker disimpan")
	}
	if string(got) != string(dummySticker) {
		t.Errorf("data sticker tidak cocok: got %s, want %s", string(got), string(dummySticker))
	}
}

func TestPersonaManagement(t *testing.T) {
	setupTestStore(t)

	custom := "Kamu adalah AI cuek dan sarkas."
	if err := SetCustomPersona(custom); err != nil {
		t.Fatalf("SetCustomPersona gagal: %v", err)
	}

	if !HasCustomPersona() {
		t.Errorf("HasCustomPersona harus true setelah SetCustomPersona")
	}

	if DefaultPersonaPrompt() != custom {
		t.Errorf("DefaultPersonaPrompt = %q, want %q", DefaultPersonaPrompt(), custom)
	}

	if err := ResetCustomPersona(); err != nil {
		t.Fatalf("ResetCustomPersona gagal: %v", err)
	}

	if HasCustomPersona() {
		t.Errorf("HasCustomPersona harus false setelah ResetCustomPersona")
	}
}

func TestStickerDeleteAndClear(t *testing.T) {
	setupTestStore(t)

	_ = SaveGroupSticker([]byte("sticker-1"))
	_ = SaveGroupSticker([]byte("sticker-2"))
	_ = SaveGroupSticker([]byte("sticker-3"))

	stickers := GetAllStickers()
	if len(stickers) != 3 {
		t.Fatalf("total stickers = %d, want 3", len(stickers))
	}

	if err := DeleteSticker(1); err != nil {
		t.Fatalf("DeleteSticker(1) gagal: %v", err)
	}

	stickers = GetAllStickers()
	if len(stickers) != 2 {
		t.Fatalf("total stickers setelah delete = %d, want 2", len(stickers))
	}

	data := GetSimiData()
	if data["total_stickers"] != 2 {
		t.Errorf("total_stickers di GetSimiData = %v, want 2", data["total_stickers"])
	}

	if err := ClearAllStickers(); err != nil {
		t.Fatalf("ClearAllStickers gagal: %v", err)
	}

	if len(GetAllStickers()) != 0 {
		t.Errorf("total stickers setelah clear = %d, want 0", len(GetAllStickers()))
	}
}

func TestSimiSession(t *testing.T) {
	setupTestStore(t)
	key := "test-chat:test-user"
	ClearSession(key)

	if HasActiveSession(key) {
		t.Errorf("HasActiveSession harus false untuk sesi baru")
	}

	AddMessageToSession(key, "User", "Tanggapan lu tentang gw yg vibe coding")
	AddMessageToSession(key, "Simi", "Vibe coding matamu, palingan cuma copas prompt ChatGPT 💀")

	if !HasActiveSession(key) {
		t.Errorf("HasActiveSession harus true setelah ada pesan")
	}

	prompt := BuildSessionPrompt(context.Background(), key, "Hm")
	if !strings.Contains(prompt, "Riwayat Percakapan Sebelumnya:") {
		t.Errorf("prompt harus mengandung riwayat percakapan")
	}
	if !strings.Contains(prompt, "Vibe coding matamu") {
		t.Errorf("prompt harus mengandung pesan sebelumnya")
	}
	if !strings.Contains(prompt, "User: Hm") {
		t.Errorf("prompt harus mengandung pesan input baru")
	}

	ClearSession(key)
	if HasActiveSession(key) {
		t.Errorf("HasActiveSession harus false setelah ClearSession")
	}
}
