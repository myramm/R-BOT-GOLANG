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

func TestILoveIMGConstantsAndScale(t *testing.T) {
	if iloveIMGPage != "https://www.iloveimg.com/upscale-image" {
		t.Fatalf("page = %q", iloveIMGPage)
	}
	if iloveIMGAPI != "https://api29g.iloveimg.com/v1" {
		t.Fatalf("api = %q", iloveIMGAPI)
	}
	if len(ImageLevels) != 1 || ImageLevels[4] != "4K" {
		t.Fatalf("ImageLevels = %#v, want only 4K", ImageLevels)
	}
}

func TestILoveIMGHTMLCredentials(t *testing.T) {
	html := []byte(`<script>var x = "token":"eyJabc123"; ilovepdfConfig.taskId = 'task-456';</script>`)
	tokenMatch := iloveIMGTokenRE.FindSubmatch(html)
	taskMatch := iloveIMGTaskIDRE.FindSubmatch(html)
	if len(tokenMatch) != 2 || string(tokenMatch[1]) != "eyJabc123" {
		t.Fatalf("token match = %q", tokenMatch)
	}
	if len(taskMatch) != 2 || string(taskMatch[1]) != "task-456" {
		t.Fatalf("task match = %q", taskMatch)
	}
}

func TestILoveIMGUploadFields(t *testing.T) {
	body, contentType, err := multipartBody(map[string]string{
		"name": "image.jpg", "chunk": "0", "chunks": "1", "task": "task-1", "preview": "1", "v": "web.0",
	}, "file", "image.jpg", []byte("image-bytes"), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	text := body.String()
	for _, want := range []string{"name=\"name\"", "\r\nimage.jpg\r\n", "name=\"chunk\"", "\r\n0\r\n", "name=\"chunks\"", "name=\"task\"", "\r\ntask-1\r\n", "name=\"preview\"", "name=\"v\"", "name=\"file\""} {
		if !strings.Contains(text, want) {
			t.Fatalf("multipart tidak memuat %q: %s", want, text)
		}
	}
	if !strings.HasPrefix(contentType, "multipart/form-data;") {
		t.Fatalf("content type = %q", contentType)
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
		`"operation":"import/upload"`, `"operation":"compress"`, `"input":"import-1"`,
		`"input_format":"mp4"`, `"output_format":"mp4"`, `"video_codec_compress":"libx264"`,
		`"compress_video":"by_percentage"`, `"video_compress_quality_percentage":60`,
		`"operation":"export/url"`, `"filename":"my-video.mp4"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("job body tidak memuat %q: %s", want, text)
		}
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

func TestUpscaleImageRejectsUnsupportedScaleWithoutNetwork(t *testing.T) {
	_, err := UpscaleImage(context.Background(), []byte("image"), 2, "image/jpeg")
	if err == nil || !strings.Contains(err.Error(), "gunakan 4K") {
		t.Fatalf("error = %v", err)
	}
}
