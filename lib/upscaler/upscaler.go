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
	"net/textproto"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"rbot/lib/httpx"
)

const (
	VideoMaxBytes = 10 * 1024 * 1024
	imageBase     = "https://imgupscaler.com"
	videoAPI      = "https://api.unwatermark.ai/api"
	cdnBase       = "https://cdn.unblurimage.ai"
	userAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

var ImageLevels = map[int]string{2: "2K", 4: "4K", 8: "8K", 16: "16K"}

type apiError struct {
	Code    any    `json:"code"`
	Message any    `json:"message"`
	Result  result `json:"result"`
}

type result struct {
	URL            string   `json:"url"`
	ObjectName     string   `json:"object_name"`
	OutputImageURL string   `json:"output_image_url"`
	OutputVideoURL string   `json:"output_video_url"`
	VideoURL       string   `json:"video_url"`
	OutputURLs     []string `json:"output_urls"`
	JobID          string   `json:"job_id"`
	TaskID         string   `json:"task_id"`
	OutputURL      string   `json:"output_url"`
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
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, fileField, fileName))
		if contentType != "" {
			header.Set("Content-Type", contentType)
		}
		part, err := writer.CreatePart(header)
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

// UpscaleImage menaikkan resolusi foto memakai ImgLarger. Level yang didukung:
// 2, 4, 8, 16. Level tinggi dikerjakan bertahap agar setiap job tetap maksimal 4x.
// Tidak ada perpindahan engine: error dari ImgLarger dikembalikan langsung agar
// hasil HD konsisten dan penyebab kegagalan mudah dilacak.
func UpscaleImage(ctx context.Context, data []byte, level int, mimeType string) ([]byte, error) {
	if _, ok := ImageLevels[level]; !ok {
		return nil, fmt.Errorf("resolusi %dK tidak didukung", level)
	}
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	current := data
	for _, scale := range imageChain(level) {
		var err error
		current, err = upscaleImageImgLarger(ctx, current, scale, mimeType)
		if err != nil {
			return nil, err
		}
	}
	return current, nil
}

func upscaleImageImgLarger(ctx context.Context, data []byte, scale int, mimeType string) ([]byte, error) {
	data, mimeType, err := normalizeImgLargerImage(ctx, data, mimeType)
	if err != nil {
		return nil, err
	}
	ext := "jpg"
	if mimeType == "image/png" {
		ext = "png"
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
	var uploaded json.RawMessage
	if err := decodeResponse(resp, &uploaded); err != nil {
		return nil, err
	}
	taskID, message, err := parseImgLargerTaskResponse(uploaded)
	if err != nil {
		return nil, err
	}
	if taskID == "" {
		if message != "" {
			return nil, fmt.Errorf("imglarger gagal: %s", message)
		}
		return nil, fmt.Errorf("imglarger tidak mengembalikan taskId (respons API berubah atau ditolak)")
	}

	for i := 0; i < 60; i++ {
		if err := sleepContext(ctx, 3*time.Second); err != nil {
			return nil, err
		}
		payload := fmt.Sprintf(`{"tool":"upscale","taskId":%q,"scaleRadio":%d}`, taskID, scale)
		poll, err := request(ctx, http.MethodPost, imageBase+"/api/legacy/status", strings.NewReader(payload), 20*time.Second, map[string]string{
			"Content-Type": "application/json", "Origin": imageBase, "Referer": imageBase + "/",
		})
		if err != nil {
			continue
		}
		var statusRaw json.RawMessage
		if err := decodeResponse(poll, &statusRaw); err != nil {
			continue
		}
		state, urls, statusMessage := parseImgLargerStatusResponse(statusRaw)
		for _, url := range urls {
			if url != "" && !strings.HasSuffix(url, "/results/") {
				return download(ctx, url)
			}
		}
		if state == "failed" || state == "error" {
			if statusMessage != "" {
				return nil, fmt.Errorf("imglarger job gagal: %s", statusMessage)
			}
			return nil, fmt.Errorf("imglarger job gagal")
		}
	}
	return nil, fmt.Errorf("imglarger timeout")
}

func parseImgLargerTaskResponse(raw []byte) (taskID, message string, err error) {
	var root any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return "", "", fmt.Errorf("decode respons imglarger: %w", err)
	}
	message = findJSONTextPriority(root, "message", "error", "msg")
	// Prioritaskan field task-specific. Field id generik hanya dipakai sebagai
	// fallback terakhir agar ID objek lain tidak salah dianggap task ID.
	if taskID := findJSONTextPriority(root, "taskId", "task_id", "taskID", "pid", "jobId", "job_id"); taskID != "" {
		return taskID, message, nil
	}
	// Respons legacy tertentu hanya menaruh token di raw.data.code.
	if taskID := findJSONTextNonNumeric(root, "code"); taskID != "" {
		return taskID, message, nil
	}
	if taskID := findJSONTextPriority(root, "id"); taskID != "" {
		return taskID, message, nil
	}
	return "", message, nil
}

func parseImgLargerStatusResponse(raw []byte) (state string, urls []string, message string) {
	var root any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(&root) != nil {
		return "", nil, ""
	}
	state = strings.ToLower(findJSONTextPriority(root, "status", "state"))
	message = findJSONTextPriority(root, "message", "error", "msg")
	for _, key := range []string{"downloadUrls", "download_urls", "downloadUrl", "download_url", "urls", "outputUrls", "output_urls", "outputUrl", "output_url", "url"} {
		findJSONURLs(root, key, &urls)
	}
	return state, uniqueStrings(urls), message
}

func findJSONTextPriority(value any, keys ...string) string {
	if object, ok := value.(map[string]any); ok {
		for _, key := range keys {
			if text := jsonText(object[key]); text != "" {
				return text
			}
		}
		for _, container := range []string{"data", "result", "raw", "response", "payload"} {
			if nested, ok := object[container]; ok {
				if text := findJSONTextPriority(nested, keys...); text != "" {
					return text
				}
			}
		}
	}
	if values, ok := value.([]any); ok {
		for _, nested := range values {
			if text := findJSONTextPriority(nested, keys...); text != "" {
				return text
			}
		}
	}
	return ""
}

func findJSONURLs(value any, key string, urls *[]string) {
	switch current := value.(type) {
	case map[string]any:
		for currentKey, nested := range current {
			if currentKey == key {
				collectJSONURLs(nested, urls)
			}
			findJSONURLs(nested, key, urls)
		}
	case []any:
		for _, nested := range current {
			findJSONURLs(nested, key, urls)
		}
	}
}

func collectJSONURLs(value any, urls *[]string) {
	switch value := value.(type) {
	case string:
		if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
			*urls = append(*urls, value)
		}
	case []any:
		for _, nested := range value {
			collectJSONURLs(nested, urls)
		}
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func findJSONTextNonNumeric(value any, key string) string {
	if object, ok := value.(map[string]any); ok {
		if text := jsonText(object[key]); text != "" && !isNumericJSONText(text) {
			return text
		}
		for _, container := range []string{"data", "result", "raw", "response", "payload"} {
			if nested, ok := object[container]; ok {
				if text := findJSONTextNonNumeric(nested, key); text != "" {
					return text
				}
			}
		}
	}
	if values, ok := value.([]any); ok {
		for _, nested := range values {
			if text := findJSONTextNonNumeric(nested, key); text != "" {
				return text
			}
		}
	}
	return ""
}

func isNumericJSONText(value string) bool {
	if value == "" {
		return false
	}
	_, err := strconv.ParseFloat(value, 64)
	return err == nil
}

func jsonText(value any) string {
	switch value := value.(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return value.String()
	default:
		return ""
	}
}

func normalizeImgLargerImage(ctx context.Context, data []byte, _ string) ([]byte, string, error) {
	detectedMIME := http.DetectContentType(data)
	switch detectedMIME {
	case "image/jpeg", "image/png":
		return data, detectedMIME, nil
	}
	// WhatsApp kadang memberi MIME image/jpeg untuk byte WebP/HEIC/format lain.
	// Jangan mengirim byte tersebut dengan label JPEG karena ImgUpscaler akan
	// membalas "Parameter error"; validasi berdasarkan magic bytes dan konversi
	// format yang tidak didukung menjadi JPEG.
	convertCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(convertCtx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-i", "pipe:0", "-frames:v", "1", "-f", "mjpeg", "-q:v", "2", "pipe:1")
	cmd.Stdin = bytes.NewReader(data)
	var output limitedBuffer
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil || output.Len() == 0 {
		if err == nil {
			err = fmt.Errorf("hasil konversi kosong")
		}
		return nil, "", fmt.Errorf("format gambar tidak didukung ImgUpscaler: %w", err)
	}
	return output.Bytes(), "image/jpeg", nil
}

const maxNormalizedImageBytes = 20 * 1024 * 1024

type limitedBuffer struct {
	bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := maxNormalizedImageBytes - b.Len()
	if remaining <= 0 {
		return 0, fmt.Errorf("hasil konversi gambar terlalu besar")
	}
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		return remaining, fmt.Errorf("hasil konversi gambar terlalu besar")
	}
	return b.Buffer.Write(p)
}

// UpscaleVideo mengirim video ke server job enhancer dan menunggu hasilnya.
func UpscaleVideo(ctx context.Context, data []byte, fileName string) ([]byte, error) {
	if len(data) > VideoMaxBytes {
		return nil, fmt.Errorf("video terlalu besar: maksimal %d MB", VideoMaxBytes/(1024*1024))
	}
	if fileName == "" {
		fileName = "video.mp4"
	}
	slot, err := uploadSlot(ctx, videoAPI+"/web/common/upload/video", "video_file_name", fileName)
	if err != nil {
		return nil, err
	}
	source, err := putUpload(ctx, slot, "video/mp4", data)
	if err != nil {
		return nil, err
	}
	// Endpoint aktif menerima multipart form dengan URL hasil upload dan
	// resolusi upscale. JSON akan dianggap body kosong oleh endpoint ini (422).
	body, contentType, err := multipartBody(map[string]string{
		"original_video_url": source,
		"resolution":         "2k",
		"is_preview":         "false",
	}, "", "", nil, "")
	if err != nil {
		return nil, err
	}
	resp, err := request(ctx, http.MethodPost, videoAPI+"/web/unblurimage/v1/video-enhancer/create-job", body, 3*time.Minute, map[string]string{
		"Content-Type": contentType,
	})
	if err != nil {
		return nil, err
	}
	var created apiError
	if err := decodeResponse(resp, &created); err != nil {
		return nil, err
	}
	jobID := created.Result.JobID
	if jobID == "" {
		jobID = created.Result.TaskID
	}
	if jobID == "" {
		return nil, created
	}

	for i := 0; i < 90; i++ {
		if err := sleepContext(ctx, 5*time.Second); err != nil {
			return nil, err
		}
		poll, err := request(ctx, http.MethodGet, videoAPI+"/web/unblurimage/v1/video-enhancer/get-job/"+jobID, nil, 3*time.Minute, map[string]string{
			"Origin":  "https://unblurimage.ai",
			"Referer": "https://unblurimage.ai/",
		})
		if err != nil {
			continue
		}
		var status apiError
		if err := decodeResponse(poll, &status); err != nil {
			continue
		}
		outputURL := status.Result.OutputURL
		if outputURL == "" {
			outputURL = status.Result.OutputVideoURL
		}
		if outputURL == "" {
			outputURL = status.Result.VideoURL
		}
		if outputURL != "" {
			return download(ctx, outputURL)
		}
		if code := fmt.Sprint(status.Code); code == "300015" || code == "300019" || code == "400202" || code == "400301" {
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
