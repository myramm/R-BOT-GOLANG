package cmd

import (
	"testing"

	"rbot/brain/command"
)

func TestModeCommandRegistered(t *testing.T) {
	cmd := command.Resolve("mode")
	if cmd == nil {
		t.Fatalf("command 'mode' harus terdaftar")
	}
	if !cmd.OwnerOnly {
		t.Errorf("command 'mode' harus OwnerOnly")
	}

	selfCmd := command.Resolve("self")
	if selfCmd == nil {
		t.Fatalf("alias 'self' harus terdaftar")
	}

	publicCmd := command.Resolve("public")
	if publicCmd == nil {
		t.Fatalf("alias 'public' harus terdaftar")
	}
}
