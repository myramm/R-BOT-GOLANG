package cmd

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

func TestStickerAddExif(t *testing.T) {
	webp := make([]byte, 12+8+4)
	copy(webp[:4], "RIFF")
	copy(webp[8:12], "WEBP")
	copy(webp[12:16], "VP8 ")
	binary.LittleEndian.PutUint32(webp[16:20], 4)
	copy(webp[20:24], "test")
	binary.LittleEndian.PutUint32(webp[4:8], uint32(len(webp)-8))

	got, err := stickerAddExif(webp, `Pack "R-BOT"`, `Author \\ Rama`)
	if err != nil {
		t.Fatal(err)
	}
	if string(got[:4]) != "RIFF" || string(got[8:12]) != "WEBP" {
		t.Fatal("hasil bukan WebP RIFF")
	}
	exifAt := -1
	for offset := 12; offset+8 <= len(got); {
		chunkSize := int(binary.LittleEndian.Uint32(got[offset+4 : offset+8]))
		end := offset + 8 + chunkSize
		if end > len(got) {
			t.Fatal("chunk WebP melewati akhir data")
		}
		if string(got[offset:offset+4]) == "EXIF" {
			exifAt = offset
			break
		}
		offset = end
		if chunkSize%2 != 0 {
			offset++
		}
	}
	if exifAt < 0 {
		t.Fatal("chunk EXIF tidak ada")
	}
	chunkSize := int(binary.LittleEndian.Uint32(got[exifAt+4 : exifAt+8]))
	payload := got[exifAt+8 : exifAt+8+chunkSize]
	if len(payload) < 24 || !bytes.Equal(payload[:8], []byte{0x49, 0x49, 0x2a, 0x00, 0x08, 0x00, 0x00, 0x00}) {
		t.Fatal("header EXIF WhatsApp tidak valid")
	}
	var fields map[string]any
	if err := json.Unmarshal(payload[24:], &fields); err != nil {
		t.Fatalf("payload EXIF bukan JSON valid: %v", err)
	}
	if fields["sticker-pack-name"] != `Pack "R-BOT"` {
		t.Fatalf("packname = %v", fields["sticker-pack-name"])
	}
	if fields["sticker-pack-publisher"] != `Author \\ Rama` {
		t.Fatalf("author = %v", fields["sticker-pack-publisher"])
	}
	if want := uint32(len(got) - 8); binary.LittleEndian.Uint32(got[4:8]) != want {
		t.Fatalf("ukuran RIFF = %d, want %d", binary.LittleEndian.Uint32(got[4:8]), want)
	}
}

func TestStickerAddExifRejectsInvalidWebP(t *testing.T) {
	for _, data := range [][]byte{
		[]byte("not-webp"),
		func() []byte {
			data := make([]byte, 20)
			copy(data[:4], "RIFF")
			copy(data[8:12], "WEBP")
			binary.LittleEndian.PutUint32(data[4:8], 4)
			return data
		}(),
	} {
		if _, err := stickerAddExif(data, "pack", "author"); err == nil {
			t.Fatal("WebP invalid harus ditolak")
		}
	}
}

func TestStickerAddExifReplacesExistingExif(t *testing.T) {
	payload := []byte("old")
	webp := make([]byte, 12+8+len(payload)+1)
	copy(webp[:4], "RIFF")
	copy(webp[8:12], "WEBP")
	copy(webp[12:16], "EXIF")
	binary.LittleEndian.PutUint32(webp[16:20], uint32(len(payload)))
	copy(webp[20:], payload)
	webp[len(webp)-1] = 0
	binary.LittleEndian.PutUint32(webp[4:8], uint32(len(webp)-8))

	got, err := stickerAddExif(webp, "pack", "author")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(got), "EXIF") != 1 {
		t.Fatalf("jumlah chunk EXIF = %d, want 1", strings.Count(string(got), "EXIF"))
	}
}
