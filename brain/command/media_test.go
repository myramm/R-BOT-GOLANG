package command

import (
	"bytes"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestApplyVideoMetadata(t *testing.T) {
	msg := &waE2E.Message{VideoMessage: &waE2E.VideoMessage{}}
	thumb := []byte{0xff, 0xd8, 0xff, 0xd9}
	applyVideoMetadata(msg, &VideoMetadata{
		Seconds:       12,
		Width:         1920,
		Height:        1080,
		JPEGThumbnail: thumb,
	})

	video := msg.GetVideoMessage()
	if video.GetSeconds() != 12 || video.GetWidth() != 1920 || video.GetHeight() != 1080 {
		t.Fatalf("metadata = seconds:%d width:%d height:%d", video.GetSeconds(), video.GetWidth(), video.GetHeight())
	}
	if !bytes.Equal(video.GetJPEGThumbnail(), thumb) {
		t.Fatalf("thumbnail = %x, want %x", video.GetJPEGThumbnail(), thumb)
	}
}

func TestApplyVideoMetadataIgnoresNonVideoAndZeroValues(t *testing.T) {
	image := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}
	applyVideoMetadata(image, &VideoMetadata{Seconds: 10})
	if image.GetImageMessage().GetContextInfo() != nil {
		t.Fatal("metadata video tidak boleh mengubah image message")
	}

	video := &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
		Seconds:       proto.Uint32(7),
		Width:         proto.Uint32(640),
		Height:        proto.Uint32(360),
		JPEGThumbnail: []byte{1, 2, 3},
	}}
	applyVideoMetadata(video, &VideoMetadata{})
	got := video.GetVideoMessage()
	if got.GetSeconds() != 7 || got.GetWidth() != 640 || got.GetHeight() != 360 || !bytes.Equal(got.GetJPEGThumbnail(), []byte{1, 2, 3}) {
		t.Fatal("nilai metadata yang sudah ada tertimpa nilai nol")
	}
}

func TestBuildStickerMessage(t *testing.T) {
	up := whatsmeow.UploadResponse{
		URL:           "https://mmg.example/sticker",
		DirectPath:    "/v/t",
		MediaKey:      []byte{1, 2, 3},
		FileEncSHA256: []byte{4, 5},
		FileSHA256:    []byte{6, 7},
		FileLength:    1234,
	}
	before := time.Now()
	msg := buildStickerMessage(up, nil)
	after := time.Now()
	sticker := msg.GetStickerMessage()
	if sticker.GetURL() != up.URL || sticker.GetDirectPath() != up.DirectPath || sticker.GetMimetype() != "image/webp" {
		t.Fatal("field dasar sticker tidak sesuai upload response")
	}
	if sticker.GetWidth() != 512 || sticker.GetHeight() != 512 || sticker.GetFileLength() != up.FileLength {
		t.Fatalf("metadata sticker = width:%d height:%d length:%d", sticker.GetWidth(), sticker.GetHeight(), sticker.GetFileLength())
	}
	if sticker.GetMediaKeyTimestamp() < before.Unix() || sticker.GetMediaKeyTimestamp() > after.Unix() {
		t.Fatalf("media key timestamp berada di luar rentang test")
	}
	if sticker.GetStickerSentTS() < before.UnixMilli() || sticker.GetStickerSentTS() > after.UnixMilli() {
		t.Fatalf("sticker sent timestamp berada di luar rentang test")
	}
	if !bytes.Equal(sticker.GetMediaKey(), up.MediaKey) || !bytes.Equal(sticker.GetFileEncSHA256(), up.FileEncSHA256) || !bytes.Equal(sticker.GetFileSHA256(), up.FileSHA256) {
		t.Fatal("hash/media key sticker tidak sesuai upload response")
	}
}

func TestApplyThumbnailMetadata(t *testing.T) {
	thumb := whatsmeow.UploadResponse{
		DirectPath:    "/thumb",
		FileSHA256:    []byte{1, 2},
		FileEncSHA256: []byte{3, 4},
	}
	image := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}
	video := &waE2E.Message{VideoMessage: &waE2E.VideoMessage{}}
	inline := []byte{0xff, 0xd8, 0xff, 0xd9}
	applyInlineJPEGThumbnail(image, inline)
	applyInlineJPEGThumbnail(video, inline)
	applyUploadedThumbnail(image, thumb)
	applyUploadedThumbnail(video, thumb)
	for name, msg := range map[string]*waE2E.Message{"image": image, "video": video} {
		var gotInline []byte
		var directPath string
		var sha, encSHA []byte
		if msg.GetImageMessage() != nil {
			gotInline = msg.GetImageMessage().GetJPEGThumbnail()
			directPath = msg.GetImageMessage().GetThumbnailDirectPath()
			sha, encSHA = msg.GetImageMessage().GetThumbnailSHA256(), msg.GetImageMessage().GetThumbnailEncSHA256()
		} else {
			gotInline = msg.GetVideoMessage().GetJPEGThumbnail()
			directPath = msg.GetVideoMessage().GetThumbnailDirectPath()
			sha, encSHA = msg.GetVideoMessage().GetThumbnailSHA256(), msg.GetVideoMessage().GetThumbnailEncSHA256()
		}
		if !bytes.Equal(gotInline, inline) || directPath != thumb.DirectPath || !bytes.Equal(sha, thumb.FileSHA256) || !bytes.Equal(encSHA, thumb.FileEncSHA256) {
			t.Fatalf("%s thumbnail metadata tidak sesuai", name)
		}
	}
}

func TestBuildStickerMessageWithPNGThumbnail(t *testing.T) {
	thumbnail := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	msg := buildStickerMessage(whatsmeow.UploadResponse{}, thumbnail)
	if !bytes.Equal(msg.GetStickerMessage().GetPngThumbnail(), thumbnail) {
		t.Fatalf("png thumbnail = %x, want %x", msg.GetStickerMessage().GetPngThumbnail(), thumbnail)
	}
}

func TestBuildStickerMessageRejectsInvalidPNGThumbnail(t *testing.T) {
	msg := buildStickerMessage(whatsmeow.UploadResponse{}, []byte("not-png"))
	if len(msg.GetStickerMessage().GetPngThumbnail()) != 0 {
		t.Fatal("thumbnail bukan PNG tidak boleh dipasang")
	}
}
