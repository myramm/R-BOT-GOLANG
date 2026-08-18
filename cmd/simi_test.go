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

	testCmd := command.Resolve("testsimi")
	if testCmd == nil {
		t.Fatalf("command 'testsimi' harus terdaftar")
	}
	if testCmd.Category != "AI" {
		t.Errorf("kategori command 'testsimi' harus 'AI', dapat %s", testCmd.Category)
	}
}
