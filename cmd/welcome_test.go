package cmd

import (
	"testing"

	"rbot/brain/command"
)

func TestWelcomeCommandRegistered(t *testing.T) {
	cmd := command.Resolve("welcome")
	if cmd == nil {
		t.Fatalf("command 'welcome' harus terdaftar")
	}
	if cmd.Category != "Group" {
		t.Errorf("kategori command 'welcome' harus 'Group', dapat %s", cmd.Category)
	}

	aliasCmd := command.Resolve("setwelcome")
	if aliasCmd == nil {
		t.Fatalf("alias command 'setwelcome' harus terdaftar")
	}
}
