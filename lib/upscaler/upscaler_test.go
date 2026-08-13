package upscaler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"strings"
	"testing"
)

func TestMultipartBodySetsImageContentType(t *testing.T) {
	body, contentType, err := multipartBody(map[string]string{"scaleRadio": "2"}, "file", "image.jpg", []byte("image-bytes"), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(bytes.NewReader(body.Bytes()), strings.TrimPrefix(contentType, "multipart/form-data; boundary="))
	part, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if got := part.FormName(); got != "scaleRadio" {
		t.Fatalf("first form field = %q, want scaleRadio", got)
	}
	part, err = reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if got := part.FormName(); got != "file" {
		t.Fatalf("file field = %q, want file", got)
	}
	if got := part.FileName(); got != "image.jpg" {
		t.Fatalf("file name = %q, want image.jpg", got)
	}
	if got := part.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("file content type = %q, want image/jpeg", got)
	}
}

func TestNormalizeImgLargerJPEG(t *testing.T) {
	data := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	got, mimeType, err := normalizeImgLargerImage(context.Background(), data, "image/webp")
	if err != nil {
		t.Fatal(err)
	}
	if mimeType != "image/jpeg" || !bytes.Equal(got, data) {
		t.Fatalf("mime=%q data preserved=%v", mimeType, bytes.Equal(got, data))
	}
}

func TestImageChain(t *testing.T) {
	tests := map[int][]int{
		2:  {2},
		4:  {4},
		8:  {4, 2},
		16: {4, 4},
	}
	for level, want := range tests {
		got := imageChain(level)
		if len(got) != len(want) {
			t.Fatalf("imageChain(%d) = %v, want %v", level, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("imageChain(%d) = %v, want %v", level, got, want)
			}
		}
	}
}

func TestFreeConvertConstants(t *testing.T) {
	if freeConvertAPI != "https://api.freeconvert.com/v1" {
		t.Fatalf("freeConvertAPI = %q", freeConvertAPI)
	}
	if freeConvertVideoQuality != 60 {
		t.Fatalf("quality = %d, want 60", freeConvertVideoQuality)
	}
	if VideoMaxBytes != 10*1024*1024 {
		t.Fatalf("VideoMaxBytes = %d, want %d", VideoMaxBytes, 10*1024*1024)
	}
}

func TestFreeConvertJobBody(t *testing.T) {
	body := freeConvertJobBody("/tmp/my-video.mp4")
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		`"operation":"import/upload"`,
		`"operation":"compress"`,
		`"input":"import-1"`,
		`"input_format":"mp4"`,
		`"output_format":"mp4"`,
		`"video_codec_compress":"libx264"`,
		`"compress_video":"by_percentage"`,
		`"video_compress_quality_percentage":60`,
		`"operation":"export/url"`,
		`"filename":"my-video.mp4"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("job body tidak memuat %q: %s", want, text)
		}
	}
}

func TestFindJSONTextPriorityTokenShapes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"root token", `{"token":"root-token"}`, "root-token"},
		{"nested data string", `{"data":"data-token"}`, "data-token"},
		{"nested access token", `{"result":{"access_token":"access-token"}}`, "access-token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var value any
			if err := json.Unmarshal([]byte(tt.raw), &value); err != nil {
				t.Fatal(err)
			}
			if got := findJSONTextPriority(value, "token", "access_token", "accessToken"); got != tt.want {
				t.Fatalf("token=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindUploadForm(t *testing.T) {
	raw := json.RawMessage(`{"tasks":{"import-1":{"operation":"import/upload","result":{"form":{"url":"https://upload.example","parameters":{"signature":"sig-123"}}}}}}`)
	url, signature := findUploadForm(raw)
	if url != "https://upload.example" || signature != "sig-123" {
		t.Fatalf("url=%q signature=%q", url, signature)
	}
}

func TestFreeConvertExportURL(t *testing.T) {
	raw := json.RawMessage(`{"export-1":{"operation":"export/url","result":{"url":"https://cdn.example/output.mp4"}}}`)
	if got := freeConvertExportURL(raw); got != "https://cdn.example/output.mp4" {
		t.Fatalf("export URL=%q", got)
	}
}

func TestFreeConvertJobError(t *testing.T) {
	job := freeConvertJob{Status: "failed"}
	if got := job.Error(); got != "FreeConvert job failed" {
		t.Fatalf("error=%q", got)
	}
}
