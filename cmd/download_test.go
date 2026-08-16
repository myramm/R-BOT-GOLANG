package cmd

import (
	"context"
	"testing"
	"time"

	"rbot/brain/command"
)

func TestDownloadVideoMetadataFileSize(t *testing.T) {
	data := []byte("not a real video")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	metadata := downloadVideoMetadata(ctx, data)
	if metadata == nil {
		t.Fatal("downloadVideoMetadata mengembalikan nil")
	}
	if metadata.FileSize != int64(len(data)) {
		t.Errorf("FileSize = %d, mau %d", metadata.FileSize, len(data))
	}
}

func TestExtractURLAndArg(t *testing.T) {
	tests := []struct {
		name        string
		ctx         *command.Ctx
		wantURL     string
		wantExtra   string
	}{
		{
			name: "Direct URL",
			ctx: &command.Ctx{
				Args: []string{"https://vt.tiktok.com/abc123"},
				Text: ".dl https://vt.tiktok.com/abc123",
			},
			wantURL:   "https://vt.tiktok.com/abc123",
			wantExtra: "",
		},
		{
			name: "Direct URL with MP3 option",
			ctx: &command.Ctx{
				Args: []string{"https://youtube.com/watch?v=abc123", "mp3"},
				Text: ".dl https://youtube.com/watch?v=abc123 mp3",
			},
			wantURL:   "https://youtube.com/watch?v=abc123",
			wantExtra: "mp3",
		},
		{
			name: "Embedded URL in text",
			ctx: &command.Ctx{
				Args: []string{"download", "ini", "https://instagram.com/p/abc123"},
				Text: ".dl download ini https://instagram.com/p/abc123",
			},
			wantURL:   "https://instagram.com/p/abc123",
			wantExtra: "download",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotArg := extractURLAndArg(tt.ctx)
			if gotURL != tt.wantURL {
				t.Errorf("extractURLAndArg() gotURL = %v, want %v", gotURL, tt.wantURL)
			}
			if gotArg != tt.wantExtra {
				t.Errorf("extractURLAndArg() gotArg = %v, want %v", gotArg, tt.wantExtra)
			}
		})
	}
}
