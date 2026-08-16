package cmd_test

import (
	"testing"

	"rbot/brain/command"
	_ "rbot/cmd"
)

func TestQCCommandRegistered(t *testing.T) {
	cmd := command.Resolve("qc")
	if cmd == nil {
		t.Fatal("command 'qc' not registered")
	}
	if cmd.Category != "Converter" {
		t.Fatalf("expected Category Converter, got %s", cmd.Category)
	}
	foundAlias := false
	for _, a := range cmd.Alias {
		if a == "quotly" {
			foundAlias = true
			break
		}
	}
	if !foundAlias {
		t.Fatal("expected alias 'quotly' in cmd.Alias")
	}
}
