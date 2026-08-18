package cmd

import (
	"testing"

	"rbot/brain/command"
)

func TestSimiCommandRegistered(t *testing.T) {
	cmd := command.Resolve("simi")
	if cmd == nil {
		t.Fatalf("command 'simi' harus terdaftar")
	}
	if cmd.Category != "AI" {
		t.Errorf("kategori command 'simi' harus 'AI', dapat %s", cmd.Category)
	}
}
