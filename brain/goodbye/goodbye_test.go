package goodbye

import (
	"testing"

	"rbot/brain/store"
)

func TestGoodbyeTemplateAndStatus(t *testing.T) {
	_ = store.Open(t.TempDir())

	groupID := "1234567890@g.us"

	// Default state
	if IsEnabled(groupID) {
		t.Errorf("expected default IsEnabled to be false")
	}
	if GetTemplate(groupID) != DefaultTemplate() {
		t.Errorf("expected default template, got %q", GetTemplate(groupID))
	}
	if HasCustomTemplate(groupID) {
		t.Errorf("expected HasCustomTemplate to be false")
	}

	// Enable goodbye
	if err := SetEnabled(groupID, true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if !IsEnabled(groupID) {
		t.Errorf("expected IsEnabled to be true")
	}

	// Set custom template
	customTpl := "Bye bye @user dari grup {group}!"
	if err := SetTemplate(groupID, customTpl); err != nil {
		t.Fatalf("SetTemplate: %v", err)
	}
	if GetTemplate(groupID) != customTpl {
		t.Errorf("expected custom template %q, got %q", customTpl, GetTemplate(groupID))
	}
	if !HasCustomTemplate(groupID) {
		t.Errorf("expected HasCustomTemplate to be true")
	}

	// Reset template
	if err := ResetTemplate(groupID); err != nil {
		t.Fatalf("ResetTemplate: %v", err)
	}
	if HasCustomTemplate(groupID) {
		t.Errorf("expected HasCustomTemplate to be false after reset")
	}
	if GetTemplate(groupID) != DefaultTemplate() {
		t.Errorf("expected default template after reset")
	}
}
