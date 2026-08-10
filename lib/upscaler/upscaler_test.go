package upscaler

import (
	"bytes"
	"context"
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

func TestVideoCreateJobFields(t *testing.T) {
	body, contentType, err := multipartBody(map[string]string{
		"original_video_url": "https://cdn.example/video.mp4",
		"resolution":         "2k",
		"is_preview":         "false",
	}, "", "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if contentType == "" || !strings.HasPrefix(contentType, "multipart/form-data;") {
		t.Fatalf("content type = %q, want multipart form", contentType)
	}
	text := body.String()
	for _, want := range []string{"name=\"original_video_url\"", "https://cdn.example/video.mp4", "name=\"resolution\"", "\r\n2k\r\n", "name=\"is_preview\"", "\r\nfalse\r\n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("body tidak memuat %q: %s", want, text)
		}
	}
}

func TestVideoResultURLs(t *testing.T) {
	for name, result := range map[string]result{
		"output":      {OutputURL: "https://cdn.example/output.mp4"},
		"video":       {VideoURL: "https://cdn.example/video.mp4"},
		"outputVideo": {OutputVideoURL: "https://cdn.example/output-video.mp4"},
	} {
		got := result.OutputURL
		if got == "" {
			got = result.OutputVideoURL
		}
		if got == "" {
			got = result.VideoURL
		}
		if got == "" {
			t.Fatalf("%s result URL kosong", name)
		}
	}
}

func TestVideoMaxBytes(t *testing.T) {
	if VideoMaxBytes != 10*1024*1024 {
		t.Fatalf("VideoMaxBytes = %d, want %d", VideoMaxBytes, 10*1024*1024)
	}
}

func TestParseImgLargerTaskResponse(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"root camel", `{"taskId":"abc"}`, "abc"},
		{"root snake", `{"task_id":"def"}`, "def"},
		{"nested data", `{"code":200,"data":{"taskId":"ghi"}}`, "ghi"},
		{"nested result pid", `{"result":{"pid":"jkl"}}`, "jkl"},
		{"deep snake numeric", `{"raw":{"data":{"result":{"task_id":9876543210123456789}}}}`, "9876543210123456789"},
		{"legacy raw code", `{"taskId":"","raw":{"code":200,"data":{"code":"raw-code-123"}}}`, "raw-code-123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := parseImgLargerTaskResponse([]byte(tt.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("task ID = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseImgLargerTaskResponseMessage(t *testing.T) {
	got, message, err := parseImgLargerTaskResponse([]byte(`{"code":403,"message":"image too large"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got != "" || message != "image too large" {
		t.Fatalf("got task=%q message=%q", got, message)
	}
}

func TestParseImgLargerStatusResponse(t *testing.T) {
	state, urls, message := parseImgLargerStatusResponse([]byte(`{"raw":{"data":{"status":"success","downloadUrls":["https://cdn.test/result.jpg"],"result":{"output_url":"https://cdn.test/result.jpg"}}}}`))
	if state != "success" || message != "" {
		t.Fatalf("state=%q message=%q", state, message)
	}
	if len(urls) != 1 || urls[0] != "https://cdn.test/result.jpg" {
		t.Fatalf("urls=%v", urls)
	}
}
