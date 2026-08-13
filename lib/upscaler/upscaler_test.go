package upscaler

import (
	"bytes"
	"context"
	"mime/multipart"
	"strings"
	"testing"
	"time"
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

func TestVideoIdentity(t *testing.T) {
	identity := newVideoIdentity()
	for _, key := range []string{"Accept", "Origin", "Referer", "User-Agent", "Product-Serial", "Device-Id"} {
		if identity[key] == "" {
			t.Fatalf("identity missing %q", key)
		}
	}
}

func TestVideoEnhancerConstants(t *testing.T) {
	if videoAPI != "https://api.unblurimage.ai/api/upscaler" {
		t.Fatalf("videoAPI = %q", videoAPI)
	}
	if videoPollInterval != 5*time.Second || videoPollMax != 90 {
		t.Fatalf("poll settings = %v/%d", videoPollInterval, videoPollMax)
	}
}

func TestVideoOutputSignature(t *testing.T) {
	if !looksLikeVideo([]byte("....ftypisom")) {
		t.Fatal("MP4 signature rejected")
	}
	for _, data := range [][]byte{[]byte("<html>error</html>"), []byte("plain text")} {
		if looksLikeVideo(data) {
			t.Fatalf("non-video signature accepted: %q", data)
		}
	}
}

func TestUpscaleImageRejectsUnsupportedScaleWithoutNetwork(t *testing.T) {
	_, err := UpscaleImage(context.Background(), []byte("image"), 2, "image/jpeg")
	if err == nil || !strings.Contains(err.Error(), "gunakan 4K") {
		t.Fatalf("error = %v", err)
	}
}
