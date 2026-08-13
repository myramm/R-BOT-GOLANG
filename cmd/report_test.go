package cmd

import (
	"strings"
	"testing"

	"rbot/brain/command"
)

func TestReportCommandRegistered(t *testing.T) {
	cmd := command.Resolve("report")
	if cmd == nil {
		t.Fatal("command report tidak terdaftar")
	}
	if alias := command.Resolve("lapor"); alias == nil || alias.Name != "report" {
		t.Fatal("alias lapor tidak mengarah ke report")
	}
}

func TestTruncateReport(t *testing.T) {
	if got := truncateReport("halo", 10); got != "halo" {
		t.Fatalf("short report = %q", got)
	}
	got := truncateReport(strings.Repeat("😀", maxReportRunes+10), maxReportRunes)
	if len([]rune(got)) != maxReportRunes+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated report rune length = %d", len([]rune(got)))
	}
}
