package jadibot

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"rbot/brain/config"
)

func TestManagerEmpty(t *testing.T) {
	if count := Count(); count != 0 {
		t.Fatalf("expected count 0, got %d", count)
	}
	list := List()
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d", len(list))
	}
}

func TestStopNonExistent(t *testing.T) {
	ctx := context.Background()
	requester := types.NewJID("628123456789", "s.whatsapp.net")

	err := Stop(ctx, "62899999999", requester, false)
	if err == nil {
		t.Fatal("expected error stopping non-existent sub-bot, got nil")
	}
	if !strings.Contains(err.Error(), "tidak ditemukan") {
		t.Fatalf("expected error containing 'tidak ditemukan', got: %v", err)
	}
}

func TestStopPermissions(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager()

	ownerJID := types.NewJID("62811111111", "s.whatsapp.net")
	otherJID := types.NewJID("62822222222", "s.whatsapp.net")
	mainOwnerJID := types.NewJID("62833333333", "s.whatsapp.net")

	botPhone := "6281234567890"
	mgr.bots[botPhone] = &SubBot{
		Phone:       botPhone,
		JID:         types.NewJID(botPhone, "s.whatsapp.net"),
		OwnerJID:    ownerJID,
		ConnectedAt: time.Now(),
	}

	if mgr.Count() != 1 {
		t.Fatalf("expected count 1, got %d", mgr.Count())
	}

	// 1. Non-owner and not creator -> expect error "Anda bukan pembuat sub-bot ini"
	err := mgr.Stop(ctx, botPhone, otherJID, false)
	if err == nil {
		t.Fatal("expected error for non-creator stop, got nil")
	}
	if err.Error() != "Anda bukan pembuat sub-bot ini" {
		t.Fatalf("expected error 'Anda bukan pembuat sub-bot ini', got: %v", err)
	}

	// Verify bot still exists
	if mgr.Count() != 1 {
		t.Fatalf("expected count still 1, got %d", mgr.Count())
	}

	// 2. Creator stopping sub-bot -> should succeed
	err = mgr.Stop(ctx, botPhone, ownerJID, false)
	if err != nil {
		t.Fatalf("expected creator to be able to stop sub-bot, got: %v", err)
	}
	if mgr.Count() != 0 {
		t.Fatalf("expected count 0 after stop, got %d", mgr.Count())
	}

	// Re-insert mock sub-bot for main owner test
	mgr.bots[botPhone] = &SubBot{
		Phone:       botPhone,
		JID:         types.NewJID(botPhone, "s.whatsapp.net"),
		OwnerJID:    ownerJID,
		ConnectedAt: time.Now(),
	}

	// 3. Main bot owner (isOwner=true) stopping sub-bot -> should succeed
	err = mgr.Stop(ctx, botPhone, mainOwnerJID, true)
	if err != nil {
		t.Fatalf("expected main owner to be able to stop sub-bot, got: %v", err)
	}
	if mgr.Count() != 0 {
		t.Fatalf("expected count 0 after owner stop, got %d", mgr.Count())
	}
}

func TestMaxJadibotLimit(t *testing.T) {
	ctx := context.Background()
	config.C.MaxJadibot = 1

	mgr := NewManager()
	mgr.bots["6281111111111"] = &SubBot{
		Phone: "6281111111111",
	}

	_, err := mgr.StartPairing(ctx, "+62 822-2222-2222", types.NewJID("6282222222222", "s.whatsapp.net"))
	if err == nil {
		t.Fatal("expected error on max jadibot limit reached, got nil")
	}
	if !strings.Contains(err.Error(), "kuota jadibot sudah penuh") {
		t.Fatalf("expected error containing 'kuota jadibot sudah penuh', got: %v", err)
	}
}

func TestStartPairingInvalidPhone(t *testing.T) {
	ctx := context.Background()
	_, err := StartPairing(ctx, "abc", types.NewJID("6281111111111", "s.whatsapp.net"))
	if err == nil {
		t.Fatal("expected error for invalid phone, got nil")
	}
	if !strings.Contains(err.Error(), "nomor telepon tidak valid") {
		t.Fatalf("expected error containing 'nomor telepon tidak valid', got: %v", err)
	}
}

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"081234567890", "6281234567890"},
		{"+62 812-3456-7890", "6281234567890"},
		{"6281234567890", "6281234567890"},
	}

	for _, tt := range tests {
		got := NormalizePhone(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizePhone(%q) = %q; expected %q", tt.input, got, tt.expected)
		}
	}
}
