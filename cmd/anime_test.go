package cmd_test

import (
	"testing"

	"rbot/brain/command"
	_ "rbot/cmd"
)

func TestAnimeCommandRegistered(t *testing.T) {
	cmd := command.Resolve("anime")
	if cmd == nil {
		t.Fatalf("Expected 'anime' command to be registered, got nil")
	}

	if cmd.Name != "anime" {
		t.Errorf("Expected command name 'anime', got '%s'", cmd.Name)
	}

	aliasCmd := command.Resolve("samehadaku")
	if aliasCmd == nil || aliasCmd.Name != "anime" {
		t.Errorf("Expected alias 'samehadaku' to resolve to 'anime'")
	}
}

func TestAnimeHandlerWithoutArgs(t *testing.T) {
	cmd := command.Resolve("anime")
	if cmd == nil {
		t.Fatal("anime command not found")
	}

	if cmd.Category != "Downloader" {
		t.Errorf("Expected category 'Downloader', got '%s'", cmd.Category)
	}
}
