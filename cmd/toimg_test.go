package cmd

import (
	"encoding/binary"
	"os"
	"testing"
)

func webPHeader(chunk string, flags byte) []byte {
	data := make([]byte, 21)
	copy(data[0:4], "RIFF")
	copy(data[8:12], "WEBP")
	copy(data[12:16], chunk)
	data[20] = flags
	return data
}

func TestIsAnimatedWebP(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{name: "animated VP8X", data: webPHeader("VP8X", 0x02), want: true},
		{name: "static VP8X", data: webPHeader("VP8X", 0), want: false},
		{name: "ANIM fallback", data: webPChunkHeader("ANIM", 0), want: true},
		{name: "not WebP", data: []byte("not a webp"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAnimatedWebP(tt.data); got != tt.want {
				t.Fatalf("isAnimatedWebP() = %v, want %v", got, tt.want)
			}
		})
	}
}

func webPChunkHeader(chunk string, size uint32) []byte {
	data := make([]byte, 20)
	copy(data[0:4], "RIFF")
	copy(data[8:12], "WEBP")
	copy(data[12:16], chunk)
	binary.LittleEndian.PutUint32(data[16:20], size)
	return data
}

func TestHasWebPChunk(t *testing.T) {
	if !hasWebPChunk(webPChunkHeader("ANIM", 0)[12:], "ANIM") {
		t.Fatal("ANIM chunk tidak terdeteksi")
	}
	if hasWebPChunk(webPChunkHeader("VP8 ", 0)[12:], "ANIM") {
		t.Fatal("chunk yang berbeda salah terdeteksi sebagai ANIM")
	}
}

func TestReadNonEmptyFile(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		name := t.TempDir() + "/empty"
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readNonEmptyFile(name); err == nil {
			t.Fatal("readNonEmptyFile() harus menolak file kosong")
		}
	})

	t.Run("non-empty", func(t *testing.T) {
		name := t.TempDir() + "/result"
		want := []byte("result")
		if err := os.WriteFile(name, want, 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := readNonEmptyFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("readNonEmptyFile() = %q, want %q", got, want)
		}
	})
}
