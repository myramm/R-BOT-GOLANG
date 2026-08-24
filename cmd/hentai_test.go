package cmd_test

import (
	"testing"

	"rbot/brain/command"
	_ "rbot/cmd"
)

func TestHentaiCommandRegistered(t *testing.T) {
	cmd := command.Resolve("hentai")
	if cmd == nil {
		t.Fatalf("Expected 'hentai' command to be registered, got nil")
	}

	if cmd.Name != "hentai" {
		t.Errorf("Expected command name 'hentai', got '%s'", cmd.Name)
	}

	aliasCmd := command.Resolve("watchhentai")
	if aliasCmd == nil || aliasCmd.Name != "hentai" {
		t.Errorf("Expected alias 'watchhentai' to resolve to 'hentai'")
	}
}

func TestHentaiHandlerCategory(t *testing.T) {
	cmd := command.Resolve("hentai")
	if cmd == nil {
		t.Fatal("hentai command not found")
	}

	if cmd.Category != "Downloader" {
		t.Errorf("Expected category 'Downloader', got '%s'", cmd.Category)
	}
}
