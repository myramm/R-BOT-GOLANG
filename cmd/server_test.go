package cmd

import (
	"strings"
	"testing"

	"rbot/brain/command"
)

func TestServerCommandRegistration(t *testing.T) {
	server := command.Resolve("server")
	if server == nil {
		t.Fatal("command server tidak terdaftar")
	}
	if alias := command.Resolve("info"); alias == nil || alias.Name != "server" {
		t.Fatal("alias info tidak mengarah ke server")
	}
}

func TestFormatServerBytes(t *testing.T) {
	tests := []struct {
		input uint64
		want  string
	}{
		{0, "0 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
	}
	for _, test := range tests {
		if got := formatServerBytes(test.input); got != test.want {
			t.Fatalf("formatServerBytes(%d) = %q, want %q", test.input, got, test.want)
		}
	}
	if !strings.Contains(formatServerBytes(1024*1024*1024), "GB") {
		t.Fatal("gigabyte format missing")
	}
}
