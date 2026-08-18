package cmd

import (
	"testing"

	"rbot/brain/command"
	"rbot/brain/settings"
	"rbot/brain/store"
)

func TestGlobalBlacklistSettings(t *testing.T) {
	if err := store.Open(t.TempDir()); err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	userBareID := "6281234567890"

	// 1. Initially not blacklisted
	if settings.IsUserBlacklisted([]string{userBareID}) {
		t.Errorf("Expected user to not be blacklisted initially")
	}

	// 2. Set blacklisted
	if err := settings.SetUserBlacklisted(userBareID, true); err != nil {
		t.Fatalf("SetUserBlacklisted error: %v", err)
	}

	if !settings.IsUserBlacklisted([]string{userBareID}) {
		t.Errorf("Expected user to be blacklisted")
	}

	blMap := settings.GetGlobalBlacklist()
	if !blMap[userBareID] {
		t.Errorf("Expected userBareID in GetGlobalBlacklist map")
	}

	// 3. Remove from blacklist
	if err := settings.SetUserBlacklisted(userBareID, false); err != nil {
		t.Fatalf("SetUserBlacklisted false error: %v", err)
	}

	if settings.IsUserBlacklisted([]string{userBareID}) {
		t.Errorf("Expected user to be removed from blacklist")
	}

	// 4. Command registration
	cmdBL := command.Resolve("blacklist")
	if cmdBL == nil {
		t.Fatalf("Expected 'blacklist' command to be registered")
	}

	cmdUnBL := command.Resolve("unblacklist")
	if cmdUnBL == nil {
		t.Fatalf("Expected 'unblacklist' alias command to be resolved")
	}
}

func TestGroupBanSettings(t *testing.T) {
	if err := store.Open(t.TempDir()); err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	groupID := "1203630123456789@g.us"
	userBareIDs := []string{"6281234567890", "238182377492614"}

	// 1. Initially not banned in group
	if settings.IsUserBannedInGroup(groupID, userBareIDs) {
		t.Errorf("Expected user to not be banned in group initially")
	}

	// 2. Ban user in group
	if err := settings.SetUserBannedInGroup(groupID, userBareIDs, true); err != nil {
		t.Fatalf("SetUserBannedInGroup error: %v", err)
	}

	if !settings.IsUserBannedInGroup(groupID, []string{"6281234567890"}) {
		t.Errorf("Expected user to be banned in group")
	}

	bannedList := settings.GetGroupBannedUsers(groupID)
	if len(bannedList) != 2 {
		t.Errorf("Expected 2 banned IDs, got %d", len(bannedList))
	}

	// 3. Unban user in group
	if err := settings.SetUserBannedInGroup(groupID, userBareIDs, false); err != nil {
		t.Fatalf("SetUserBannedInGroup false error: %v", err)
	}

	if settings.IsUserBannedInGroup(groupID, userBareIDs) {
		t.Errorf("Expected user to be unbanned in group")
	}

	// 4. Command registration
	cmdBan := command.Resolve("ban")
	if cmdBan == nil {
		t.Fatalf("Expected 'ban' command to be registered")
	}

	cmdUnban := command.Resolve("unban")
	if cmdUnban == nil {
		t.Fatalf("Expected 'unban' alias command to be resolved")
	}
}
