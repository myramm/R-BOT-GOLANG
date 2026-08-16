package cmd_test

import (
	"context"
	"strings"
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

	pngBytes, err := cmd.ExportFetchQuotlyPNG(ctx, "jskwkwkw", "Tester", "https://i.ibb.co/3Fh9V6p/avatar.png")
	if err != nil {
		t.Fatalf("FetchQuotlyPNG returned error: %v", err)
	}
	if len(pngBytes) == 0 {
		t.Fatal("expected non-empty pngBytes")
	}
}

func TestQCPrefixStripping(t *testing.T) {
	rawInputs := []string{
		"jskwkwkw",
		".qc jskwkwkw",
		"qc jskwkwkw",
		"!qc jskwkwkw",
	}

	for _, input := range rawInputs {
		cleanText := strings.TrimSpace(input)
		for _, p := range []string{".qc", "qc", "!qc", "/qc"} {
			if strings.HasPrefix(strings.ToLower(cleanText), p) {
				cleanText = strings.TrimSpace(cleanText[len(p):])
			}
		}
		if cleanText != "jskwkwkw" {
			t.Fatalf("expected cleanText 'jskwkwkw' for input %q, got %q", input, cleanText)
		}
		if strings.Contains(cleanText, ".qc") {
			t.Fatalf("cleanText %q should NEVER contain '.qc'", cleanText)
		}
	}
}


