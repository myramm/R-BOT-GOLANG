package cmd

import (
	"context"
	"testing"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/command"
)

func TestJadibotCommandsRegistered(t *testing.T) {
	tests := []struct {
		name         string
		lookup       string
		expectCmd    string
		wantCategory string
	}{
		{"jadibot main", "jadibot", "jadibot", "jadibot"},
		{"jadibot alias clonebot", "clonebot", "jadibot", "jadibot"},
		{"jadibot alias subbot", "subbot", "jadibot", "jadibot"},
		{"stopjadibot main", "stopjadibot", "stopjadibot", "jadibot"},
		{"stopjadibot alias deljadibot", "deljadibot", "stopjadibot", "jadibot"},
		{"listjadibot main", "listjadibot", "listjadibot", "jadibot"},
		{"listjadibot alias jadibots", "jadibots", "listjadibot", "jadibot"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := command.Resolve(tt.lookup)
			if resolved == nil {
				t.Fatalf("command or alias %q not registered", tt.lookup)
			}
			if resolved.Name != tt.expectCmd {
				t.Errorf("command for %q = %q, want %q", tt.lookup, resolved.Name, tt.expectCmd)
			}
			if resolved.Category != tt.wantCategory {
				t.Errorf("category for %q = %q, want %q", tt.lookup, resolved.Category, tt.wantCategory)
			}
			if resolved.Description == "" {
				t.Errorf("description for %q is empty", tt.lookup)
			}
			if resolved.Handler == nil {
				t.Errorf("handler for %q is nil", tt.lookup)
			}
		})
	}
}

func TestJadibotSubBotPrevention(t *testing.T) {
	cmd := command.Resolve("jadibot")
	if cmd == nil {
		t.Fatal("jadibot command not found")
	}

	evt := &events.Message{}
	evt.Info.Sender = types.NewJID("628123456789", types.DefaultUserServer)
	evt.Info.Chat = types.NewJID("628123456789", types.DefaultUserServer)
	evt.Info.ID = "MSG123"

	c := &command.Ctx{
		SubBot: true,
		Evt:    evt,
	}

	ctx := context.Background()
	// Handler should attempt c.Reply directly (returning client is nil because Client is nil in unit test)
	err := cmd.Handler(ctx, c)
	if err == nil {
		t.Fatal("expected error due to nil client in reply, got nil")
	}
}
