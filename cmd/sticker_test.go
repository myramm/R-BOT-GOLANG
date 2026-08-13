package cmd

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func TestStickerAddExif(t *testing.T) {
	vp8 := []byte{0x00, 0x00, 0x00, 0x9d, 0x01, 0x2a, 0x00, 0x02, 0x00, 0x02}
	webp := makeWebP("VP8 ", vp8)

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
	if len(payload) < 22 || !bytes.Equal(payload[:8], []byte{0x49, 0x49, 0x2a, 0x00, 0x08, 0x00, 0x00, 0x00}) {
		t.Fatal("header EXIF WhatsApp tidak valid")
	}
	metadataLength := int(binary.LittleEndian.Uint32(payload[14:18]))
	if metadataLength != len(payload)-22 {
		t.Fatalf("panjang metadata = %d, want %d", metadataLength, len(payload)-22)
	}
	if want := uint32(0x16); binary.LittleEndian.Uint32(payload[18:22]) != want {
		t.Fatalf("offset header EXIF = %d, want %d", binary.LittleEndian.Uint32(payload[18:22]), want)
	}
	var fields map[string]any
	if err := json.Unmarshal(payload[22:], &fields); err != nil {
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
	chunks, err := parseWebPChunks(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 3 || chunks[0].kind != "VP8X" || chunks[1].kind != "VP8 " || chunks[2].kind != "EXIF" {
		t.Fatalf("urutan chunk = %v, want VP8X, VP8, EXIF", chunkNames(chunks))
	}
	if len(chunks[0].payload) != 10 || chunks[0].payload[0]&0x08 == 0 {
		t.Fatal("flag EXIF pada VP8X tidak aktif")
	}
}

func TestStickerAddExifWithExistingVP8X(t *testing.T) {
	vp8x := make([]byte, 10)
	vp8x[0] = 0x10
	putWebP24(vp8x[4:7], 511)
	putWebP24(vp8x[7:10], 511)
	webp := makeWebP("VP8X", vp8x, webpChunk{kind: "VP8 ", payload: []byte{0x00, 0x00, 0x00, 0x9d, 0x01, 0x2a, 0x00, 0x02, 0x00, 0x02}})
	got, err := stickerAddExif(webp, "pack", "author")
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := parseWebPChunks(got)
	if err != nil {
		t.Fatal(err)
	}
	if chunks[0].kind != "VP8X" || chunks[1].kind != "VP8 " || chunks[2].kind != "EXIF" {
		t.Fatalf("urutan chunk = %v", chunkNames(chunks))
	}
	if chunks[0].payload[0]&0x18 != 0x18 {
		t.Fatalf("flag VP8X = %#x, want alpha+EXIF", chunks[0].payload[0])
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
	vp8 := []byte{0x00, 0x00, 0x00, 0x9d, 0x01, 0x2a, 0x00, 0x02, 0x00, 0x02}
	oldExif := []byte("old")
	webp := makeWebP("EXIF", oldExif, webpChunk{kind: "VP8 ", payload: vp8})

	got, err := stickerAddExif(webp, "pack", "author")
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := parseWebPChunks(got)
	if err != nil {
		t.Fatal(err)
	}
	exifCount := 0
	for _, chunk := range chunks {
		if chunk.kind == "EXIF" {
			exifCount++
			if len(chunk.payload) < 22 {
				t.Fatal("EXIF baru terlalu pendek")
			}
			var fields map[string]any
			if err := json.Unmarshal(chunk.payload[22:], &fields); err != nil {
				t.Fatalf("EXIF baru bukan JSON valid: %v", err)
			}
			if fields["sticker-pack-name"] != "pack" || fields["sticker-pack-publisher"] != "author" {
				t.Fatalf("metadata baru salah: %#v", fields)
			}
			if bytes.Contains(chunk.payload, oldExif) {
				t.Fatal("metadata EXIF lama masih ada")
			}
		}
	}
	if exifCount != 1 {
		t.Fatalf("jumlah chunk EXIF = %d, want 1", exifCount)
	}
}

func TestStickerAddExifRejectsNonZeroPadding(t *testing.T) {
	webp := makeWebP("EXIF", []byte("old"), webpChunk{kind: "VP8 ", payload: []byte{0x00, 0x00, 0x00}})
	webp[len(webp)-1] = 0x7f
	if _, err := stickerAddExif(webp, "pack", "author"); err == nil {
		t.Fatal("padding non-zero harus ditolak")
	}
}

func makeWebP(firstKind string, firstPayload []byte, rest ...webpChunk) []byte {
	chunks := []webpChunk{{kind: firstKind, payload: firstPayload}}
	chunks = append(chunks, rest...)
	var out bytes.Buffer
	out.WriteString("RIFF")
	out.Write(make([]byte, 4))
	out.WriteString("WEBP")
	for _, chunk := range chunks {
		writeWebPChunk(&out, chunk.kind, chunk.payload)
	}
	result := out.Bytes()
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)-8))
	return result
}

func chunkNames(chunks []webpChunk) []string {
	result := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		result = append(result, chunk.kind)
	}
	return result
}
