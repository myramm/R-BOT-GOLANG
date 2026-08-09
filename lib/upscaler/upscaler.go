// Package upscaler menyediakan klien HTTP untuk meningkatkan kualitas foto/video.
// Endpoint dan alur job dipertahankan dari lib/upscaler.js pada bot Node lama.
package upscaler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"rbot/lib/httpx"
)

const (
	VideoMaxBytes = 5 * 1024 * 1024
	imageBase     = "https://imgupscaler.com"
	videoAPI      = "https://api.unblurimage.ai/api/upscaler"
	imageAPI      = "https://api.unwatermark.ai/api"
	cdnBase       = "https://cdn.unwatermark.ai"
	userAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

var ImageLevels = map[int]string{2: "2K", 4: "4K", 8: "8K", 16: "16K"}

type apiError struct {
	Code    any    `json:"code"`
	Message any    `json:"message"`
	Result  result `json:"result"`
}

type result struct {
	URL             string   `json:"url"`
	ObjectName      string   `json:"object_name"`
	OutputImageURL  string   `json:"output_image_url"`
	OutputURLs      []string `json:"output_urls"`
	JobID           string   `json:"job_id"`
	OutputURL       string   `json:"output_url"`
}

func (e apiError) Error() string {
	if s, ok := e.Message.(string); ok && s != "" {
		return s
	}
	if m, ok := e.Message.(map[string]any); ok {
		for _, key := range []string{"en", "id"} {
			if s, ok := m[key].(string); ok && s != "" {
				return s
			}
		}
	}
	return fmt.Sprintf("server AI mengembalikan code %v", e.Code)
}

func request(ctx context.Context, method, url string, body io.Reader, timeout time.Duration, headers map[string]string) (*http.Response, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	req, err := http.NewRequestWithContext(cctx, method, url, body)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = &cancelBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

func decodeResponse(resp *http.Response, out any) error {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server AI HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode respons server AI: %w", err)
	}
	return nil
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(b[:])
}

func identityHeaders() map[string]string {
	return map[string]string{
		"Accept":          "*/*",
		"Accept-Language": "en-US,en;q=0.9",
		"Origin":          "https://unblurimage.ai",
		"Referer":         "https://unblurimage.ai/",
		"product-serial":  randomID(),
		"device-id":       randomID(),
	}
}

func multipartBody(fields map[string]string, fileField, fileName string, data []byte, contentType string) (*bytes.Buffer, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, "", err
		}
	}
	if fileField != "" {
		part, err := writer.CreateFormFile(fileField, fileName)
		if err != nil {
			return nil, "", err
		}
		if len(data) > 0 {
			if _, err := part.Write(data); err != nil {
				return nil, "", err
			}
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &body, writer.FormDataContentType(), nil
}

func uploadSlot(ctx context.Context, endpoint, field, fileName string) (string, error) {
	// API Node mengirim nama file sebagai nilai field multipart biasa (bukan
	// upload file kosong); byte sebenarnya dikirim kemudian lewat PUT ke CDN.
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField(field, fileName); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	headers := identityHeaders()
	headers["Content-Type"] = writer.FormDataContentType()
	resp, err := request(ctx, http.MethodPost, endpoint, &body, 2*time.Minute, headers)
	if err != nil {
		return "", err
	}
	var out apiError
	if err := decodeResponse(resp, &out); err != nil {
		return "", err
	}
	if out.Result.URL == "" || out.Result.ObjectName == "" {
		return "", out
	}
	return out.Result.URL + "\x00" + out.Result.ObjectName, nil
}

func putUpload(ctx context.Context, slot, contentType string, data []byte) (string, error) {
	parts := strings.SplitN(slot, "\x00", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("slot upload tidak valid")
	}
	resp, err := request(ctx, http.MethodPut, parts[0], bytes.NewReader(data), 2*time.Minute, map[string]string{
		"Content-Type": contentType,
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("upload ke CDN HTTP %d", resp.StatusCode)
	}
	return cdnBase + "/" + parts[1], nil
}

func download(ctx context.Context, url string) ([]byte, error) {
	return httpx.GetBytes(ctx, url, 2*time.Minute, 0)
}

func imageChain(level int) []int {
	switch level {
	case 2:
		return []int{2}
	case 4:
		return []int{4}
	case 8:
		return []int{4, 2}
	case 16:
		return []int{4, 4}
	default:
		return nil
	}
}

// UpscaleImage menaikkan resolusi foto. Level yang didukung: 2, 4, 8, 16.
// Level tinggi dikerjakan bertahap agar setiap job tetap maksimal 4x.
func UpscaleImage(ctx context.Context, data []byte, level int, mimeType string) ([]byte, error) {
	if _, ok := ImageLevels[level]; !ok {
		return nil, fmt.Errorf("resolusi %dK tidak didukung", level)
	}
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	current := data
	var lastErr error
	for _, scale := range imageChain(level) {
		current, lastErr = upscaleImageImgLarger(ctx, current, scale, mimeType)
		if lastErr != nil {
			break
		}
	}
	if lastErr == nil {
		return current, nil
	}

	// Fallback mempertahankan perilaku Node lama bila engine utama sedang gagal.
	if fallback, err := upscaleImageUnwatermark(ctx, data, level, mimeType); err == nil {
		return fallback, nil
	}
	if fallback, err := upscaleImageWidipe(ctx, data); err == nil {
		return fallback, nil
	}
	return nil, lastErr
}

func upscaleImageImgLarger(ctx context.Context, data []byte, scale int, mimeType string) ([]byte, error) {
	ext := "jpg"
	if i := strings.LastIndex(mimeType, "/"); i >= 0 && i+1 < len(mimeType) {
		ext = strings.ReplaceAll(mimeType[i+1:], "jpeg", "jpg")
	}
	body, contentType, err := multipartBody(map[string]string{
		"tool": "upscale", "mode": "batch", "scaleRadio": strconv.Itoa(scale),
	}, "file", "image."+ext, data, mimeType)
	if err != nil {
		return nil, err
	}
	resp, err := request(ctx, http.MethodPost, imageBase+"/api/legacy/upload", body, time.Minute, map[string]string{
		"Content-Type": contentType,
		"Origin":       imageBase,
		"Referer":      imageBase + "/",
	})
	if err != nil {
		return nil, err
	}
	var uploaded struct {
		TaskID string `json:"taskId"`
	}
	if err := decodeResponse(resp, &uploaded); err != nil {
		return nil, err
	}
	if uploaded.TaskID == "" {
		return nil, fmt.Errorf("imglarger tidak mengembalikan taskId")
	}

	for i := 0; i < 60; i++ {
		if err := sleepContext(ctx, 3*time.Second); err != nil {
			return nil, err
		}
		payload := fmt.Sprintf(`{"tool":"upscale","taskId":%q,"scaleRadio":%d}`, uploaded.TaskID, scale)
		poll, err := request(ctx, http.MethodPost, imageBase+"/api/legacy/status", strings.NewReader(payload), 20*time.Second, map[string]string{
			"Content-Type": "application/json", "Origin": imageBase, "Referer": imageBase + "/",
		})
		if err != nil {
			continue
		}
		var status struct {
			Status      string   `json:"status"`
			DownloadURL []string `json:"downloadUrls"`
			Raw         struct {
				Data struct {
					Status      string   `json:"status"`
					DownloadURL []string `json:"downloadUrls"`
				} `json:"data"`
			} `json:"raw"`
		}
		if err := decodeResponse(poll, &status); err != nil {
			continue
		}
		urls := append(status.DownloadURL, status.Raw.Data.DownloadURL...)
		for _, url := range urls {
			if url != "" && !strings.HasSuffix(url, "/results/") {
				return download(ctx, url)
			}
		}
		state := status.Status
		if state == "" {
			state = status.Raw.Data.Status
		}
		if state == "failed" || state == "error" {
			return nil, fmt.Errorf("imglarger job gagal")
		}
	}
	return nil, fmt.Errorf("imglarger timeout")
}

func upscaleImageUnwatermark(ctx context.Context, data []byte, level int, mimeType string) ([]byte, error) {
	slot, err := uploadSlot(ctx, imageAPI+"/web/common/upload/image", "image_file_name", "image.jpg")
	if err != nil {
		return nil, err
	}
	source, err := putUpload(ctx, slot, mimeType, data)
	if err != nil {
		return nil, err
	}
	body, contentType, err := multipartBody(map[string]string{
		"original_image_url": source, "upscale_type": strconv.Itoa(level),
	}, "", "", nil, "")
	if err != nil {
		return nil, err
	}
	resp, err := request(ctx, http.MethodPost, imageAPI+"/web/unblurimage/v1/image-upscaler-v2/create-job", body, 3*time.Minute, map[string]string{
		"Content-Type": contentType,
	})
	if err != nil {
		return nil, err
	}
	var out apiError
	if err := decodeResponse(resp, &out); err != nil {
		return nil, err
	}
	url := out.Result.OutputImageURL
	if url == "" && len(out.Result.OutputURLs) > 0 {
		url = out.Result.OutputURLs[0]
	}
	if url == "" {
		return nil, out
	}
	return download(ctx, url)
}

func upscaleImageWidipe(ctx context.Context, data []byte) ([]byte, error) {
	body, contentType, err := multipartBody(nil, "image", "photo.jpg", data, "image/jpeg")
	if err != nil {
		return nil, err
	}
	resp, err := request(ctx, http.MethodPost, "https://widipe.com/hd", body, time.Minute, map[string]string{
		"Content-Type": contentType,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("widipe HTTP %d", resp.StatusCode)
	}
	contentType = resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "image") {
		return io.ReadAll(resp.Body)
	}
	var out struct {
		URL    string `json:"url"`
		Result string `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	url := out.URL
	if url == "" {
		url = out.Result
	}
	if url == "" {
		return nil, fmt.Errorf("hasil widipe tidak valid")
	}
	return download(ctx, url)
}

// UpscaleVideo mengirim video ke server job enhancer dan menunggu hasilnya.
func UpscaleVideo(ctx context.Context, data []byte, fileName string) ([]byte, error) {
	if len(data) > VideoMaxBytes {
		return nil, fmt.Errorf("video terlalu besar: maksimal %d MB", VideoMaxBytes/(1024*1024))
	}
	if fileName == "" {
		fileName = "video.mp4"
	}
	slot, err := uploadSlot(ctx, videoAPI+"/v1/ai-video-enhancer/upload-video", "video_file_name", fileName)
	if err != nil {
		return nil, err
	}
	source, err := putUpload(ctx, slot, "video/mp4", data)
	if err != nil {
		return nil, err
	}
	body, contentType, err := multipartBody(map[string]string{"original_video_file": source}, "", "", nil, "")
	if err != nil {
		return nil, err
	}
	resp, err := request(ctx, http.MethodPost, videoAPI+"/v2/ai-video-enhancer/create-job", body, 3*time.Minute, map[string]string{
		"Content-Type": contentType,
	})
	if err != nil {
		return nil, err
	}
	var created apiError
	if err := decodeResponse(resp, &created); err != nil {
		return nil, err
	}
	if created.Result.JobID == "" {
		return nil, created
	}

	for i := 0; i < 90; i++ {
		if err := sleepContext(ctx, 5*time.Second); err != nil {
			return nil, err
		}
		poll, err := request(ctx, http.MethodGet, videoAPI+"/v2/ai-video-enhancer/get-job/"+created.Result.JobID, nil, 3*time.Minute, identityHeaders())
		if err != nil {
			continue
		}
		var status apiError
		if err := decodeResponse(poll, &status); err != nil {
			continue
		}
		if status.Result.OutputURL != "" {
			return download(ctx, status.Result.OutputURL)
		}
		if code := fmt.Sprint(status.Code); code == "300015" || code == "300019" {
			return nil, status
		}
	}
	return nil, fmt.Errorf("server AI kelamaan memproses video")
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
