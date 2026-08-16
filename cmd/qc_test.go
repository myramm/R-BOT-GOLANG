package cmd_test

import (
	"context"
	"testing"
	"time"

	"rbot/brain/command"
	"rbot/cmd"
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

func TestFetchQuotlyPNG(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pngBytes, err := cmd.ExportFetchQuotlyPNG(ctx, "Halo Dunia", "Tester", "https://i.ibb.co/3Fh9V6p/avatar.png")
	if err != nil {
		t.Fatalf("FetchQuotlyPNG returned error: %v", err)
	}
	if len(pngBytes) == 0 {
		t.Fatal("expected non-empty pngBytes")
	}
}

