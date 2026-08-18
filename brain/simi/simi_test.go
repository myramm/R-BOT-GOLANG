package simi

import (
	"os"
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
