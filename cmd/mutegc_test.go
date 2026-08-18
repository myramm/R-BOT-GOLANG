package cmd

import (
	"testing"

	"rbot/brain/command"
	"rbot/brain/settings"
	"rbot/brain/store"
)

func TestGroupMuteSettings(t *testing.T) {
	if err := store.Open(t.TempDir()); err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	groupID := "1203630123456789@g.us"

	// Test 1: Initially not muted
	if settings.IsGroupMuted(groupID) {
		t.Errorf("Expected group to not be muted initially")
	}

	// Test 2: Set muted
	if err := settings.SetGroupMuted(groupID, true); err != nil {
		t.Fatalf("SetGroupMuted error: %v", err)
	}

	if !settings.IsGroupMuted(groupID) {
		t.Errorf("Expected group to be muted")
	}

	mutedMap := settings.GetMutedGroups()
	if !mutedMap[groupID] {
		t.Errorf("Expected groupID in GetMutedGroups map")
	}

	// Test 3: Set unmuted
	if err := settings.SetGroupMuted(groupID, false); err != nil {
		t.Fatalf("SetGroupMuted false error: %v", err)
	}

	if settings.IsGroupMuted(groupID) {
		t.Errorf("Expected group to be unmuted")
	}

	// Test 4: Check command registration
	cmdMute := command.Resolve("mute")
	if cmdMute == nil {
		t.Fatalf("Expected 'mute' command to be registered")
	}

	cmdUnmute := command.Resolve("unmute")
	if cmdUnmute == nil {
		t.Fatalf("Expected 'unmute' alias command to be resolved")
	}
}
