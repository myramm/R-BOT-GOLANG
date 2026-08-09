package cmd

import (
	"context"
	"testing"
	"time"
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
