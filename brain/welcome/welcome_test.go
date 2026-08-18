package welcome

import (
	"os"
	"testing"

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

func TestWelcomeToggle(t *testing.T) {
	setupTestStore(t)

	groupJID := "120363012345678901@g.us"
	if IsEnabled(groupJID) {
		t.Errorf("default welcome harus disabled")
	}

	if err := SetEnabled(groupJID, true); err != nil {
		t.Fatalf("SetEnabled gagal: %v", err)
	}

	if !IsEnabled(groupJID) {
		t.Errorf("welcome harus enabled setelah SetEnabled(true)")
	}

	if err := SetEnabled(groupJID, false); err != nil {
		t.Fatalf("SetEnabled gagal: %v", err)
	}

	if IsEnabled(groupJID) {
		t.Errorf("welcome harus disabled setelah SetEnabled(false)")
	}
}

func TestWelcomeTemplate(t *testing.T) {
	setupTestStore(t)

	groupJID := "120363012345678901@g.us"
	if GetTemplate(groupJID) != DefaultTemplate() {
		t.Errorf("template awal harus defaultTemplate")
	}
	if HasCustomTemplate(groupJID) {
		t.Errorf("HasCustomTemplate awal harus false")
	}

	custom := "Welcome @user to {group}!"
	if err := SetTemplate(groupJID, custom); err != nil {
		t.Fatalf("SetTemplate gagal: %v", err)
	}

	if !HasCustomTemplate(groupJID) {
		t.Errorf("HasCustomTemplate harus true setelah SetTemplate")
	}
	if GetTemplate(groupJID) != custom {
		t.Errorf("GetTemplate = %q, want %q", GetTemplate(groupJID), custom)
	}

	if err := ResetTemplate(groupJID); err != nil {
		t.Fatalf("ResetTemplate gagal: %v", err)
	}

	if HasCustomTemplate(groupJID) {
		t.Errorf("HasCustomTemplate harus false setelah ResetTemplate")
	}
	if GetTemplate(groupJID) != DefaultTemplate() {
		t.Errorf("GetTemplate setelah reset harus defaultTemplate")
	}
}
