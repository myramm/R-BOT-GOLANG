package cmd

import (
	"testing"

	"rbot/brain/command"
)

func TestIdchCommandRegistration(t *testing.T) {
	cmd := command.Resolve("idch")
	if cmd == nil {
		t.Fatal("expected 'idch' command to be registered")
	}

	for _, alias := range []string{"getidch", "idchannel", "channelinfo", "saluran", "cekch"} {
		c := command.Resolve(alias)
		if c == nil || c.Name != "idch" {
			t.Errorf("expected alias '%s' to resolve to 'idch'", alias)
		}
	}
}

func TestExtractNewsletterCode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://whatsapp.com/channel/0029Va9xyz123", "0029Va9xyz123"},
		{"https://www.whatsapp.com/channel/0029Va9xyz123?test=1", "0029Va9xyz123"},
		{"cek https://whatsapp.com/channel/0029VaABCD ya", "0029VaABCD"},
		{"0029Va9xyz123", "0029Va9xyz123"},
		{"120363043891829012@newsletter", "120363043891829012@newsletter"},
	}

	for _, tt := range tests {
		got := extractNewsletterCode(tt.input)
		if got != tt.expected {
			t.Errorf("extractNewsletterCode(%q) = %q, expected %q", tt.input, got, tt.expected)
		}
	}
}

func TestFormatNumber(t *testing.T) {
	if got := formatNumber(1250); got != "1.250" {
		t.Errorf("expected 1.250, got %s", got)
	}
	if got := formatNumber(1000000); got != "1.000.000" {
		t.Errorf("expected 1.000.000, got %s", got)
	}
	if got := formatNumber(50); got != "50" {
		t.Errorf("expected 50, got %s", got)
	}
}
