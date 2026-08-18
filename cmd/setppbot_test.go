package cmd

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"rbot/brain/command"
)

func TestSetPPBotRegistration(t *testing.T) {
	cmd := command.Resolve("setppbot")
	if cmd == nil {
		t.Fatal("Command 'setppbot' belum terdaftar")
	}
	if !cmd.OwnerOnly {
		t.Error("Command 'setppbot' harus OwnerOnly")
	}
	hasSetPP := false
	for _, alias := range cmd.Alias {
		if alias == "setpp" {
			hasSetPP = true
			break
		}
	}
	if !hasSetPP {
		t.Error("Alias 'setpp' tidak ditemukan")
	}
}

func TestProcessProfilePicture(t *testing.T) {
	// Buat gambar PNG uji coba berukuran 1000x500 (persegi panjang)
	img := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	for x := 0; x < 1000; x++ {
		for y := 0; y < 500; y++ {
			img.Set(x, y, color.RGBA{R: 255, G: 100, B: 50, A: 255})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("Gagal encode PNG uji coba: %v", err)
	}

	ctx := context.Background()
	resultJpeg, err := ProcessProfilePicture(ctx, buf.Bytes())
	if err != nil {
		t.Fatalf("processProfilePicture gagal: %v", err)
	}

	// Decode hasil JPEG dan pastikan ukurannya persis 720x720
	outImg, err := jpeg.Decode(bytes.NewReader(resultJpeg))
	if err != nil {
		t.Fatalf("Gagal decode hasil JPEG foto profil: %v", err)
	}

	bounds := outImg.Bounds()
	if bounds.Dx() != 720 || bounds.Dy() != 720 {
		t.Errorf("Ekspektasi dimensi foto profil 720x720, hasil: %dx%d", bounds.Dx(), bounds.Dy())
	}
}
