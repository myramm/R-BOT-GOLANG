package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
)

func TestExtractText(t *testing.T) {
	cases := []struct {
		name string
		msg  *waE2E.Message
		want string
	}{
		{"nil", nil, ""},
		{"conversation", &waE2E.Message{Conversation: proto.String("  halo  ")}, "halo"},
		{
			"extendedText",
			&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(".menu")}},
			".menu",
		},
		{
			"imageCaption",
			&waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String(".sticker")}},
			".sticker",
		},
		{
			"videoCaption",
			&waE2E.Message{VideoMessage: &waE2E.VideoMessage{Caption: proto.String(".s ")}},
			".s",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractText(tc.msg); got != tc.want {
				t.Errorf("ExtractText = %q, mau %q", got, tc.want)
			}
		})
	}
}

func TestReportErrorCallsHook(t *testing.T) {
	oldHook := ErrorHook
	defer func() { ErrorHook = oldHook }()

	called := make(chan error, 1)
	ErrorHook = func(_ context.Context, _ *Ctx, err error) {
		called <- err
	}
	want := errors.New("imglarger tidak mengembalikan taskId")
	(&Ctx{InvokedAs: "hd"}).ReportError(context.Background(), want)

	select {
	case got := <-called:
		if got != want {
			t.Fatalf("error hook menerima %v, mau %v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("ErrorHook tidak terpanggil")
	}
}

func TestRegisterResolveAlias(t *testing.T) {
	// Isolasi dari registry global paket.
	mu.Lock()
	registry = map[string]*Command{}
	aliases = map[string]string{}
	mu.Unlock()

	h := func(context.Context, *Ctx) error { return nil }
	Register(&Command{Name: "sticker", Alias: []string{"s", "stiker"}, Handler: h})

	if c := Resolve("sticker"); c == nil || c.Name != "sticker" {
		t.Fatal("resolve nama gagal")
	}
	if c := Resolve("S"); c == nil || c.Name != "sticker" {
		t.Error("resolve alias case-insensitive gagal")
	}
	if c := Resolve("stiker"); c == nil || c.Name != "sticker" {
		t.Error("resolve alias kedua gagal")
	}
	if c := Resolve("tidakada"); c != nil {
		t.Error("resolve tak dikenal harus nil")
	}
	if Count() != 1 {
		t.Errorf("Count = %d, mau 1", Count())
	}
}
