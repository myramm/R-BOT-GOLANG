package exif

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func TestAddStickerExif(t *testing.T) {
	vp8 := []byte{0x00, 0x00, 0x00, 0x9d, 0x01, 0x2a, 0x00, 0x02, 0x00, 0x02}
	var out bytes.Buffer
	out.WriteString("RIFF")
	out.Write(make([]byte, 4))
	out.WriteString("WEBP")
	writeWebPChunk(&out, "VP8 ", vp8)
	raw := out.Bytes()
	binary.LittleEndian.PutUint32(raw[4:8], uint32(len(raw)-8))

	got, err := AddStickerExif(raw, "MyPack", "MyAuthor")
	if err != nil {
		t.Fatalf("AddStickerExif failed: %v", err)
	}

	chunks, err := parseWebPChunks(got)
	if err != nil {
		t.Fatalf("parseWebPChunks failed: %v", err)
	}

	var hasExif bool
	for _, ch := range chunks {
		if ch.kind == "EXIF" {
			hasExif = true
			if len(ch.payload) < 22 {
				t.Fatal("EXIF payload too short")
			}
			var m map[string]any
			if err := json.Unmarshal(ch.payload[22:], &m); err != nil {
				t.Fatalf("JSON decode EXIF: %v", err)
			}
			if m["sticker-pack-name"] != "MyPack" {
				t.Errorf("sticker-pack-name = %v, want MyPack", m["sticker-pack-name"])
			}
			if m["sticker-pack-publisher"] != "MyAuthor" {
				t.Errorf("sticker-pack-publisher = %v, want MyAuthor", m["sticker-pack-publisher"])
			}
		}
	}
	if !hasExif {
		t.Fatal("hasil WebP tidak memiliki chunk EXIF")
	}
}
