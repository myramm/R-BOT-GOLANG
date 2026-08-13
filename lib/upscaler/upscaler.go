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
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rbot/lib/httpx"
)

const (
	VideoMaxBytes       = 10 * 1024 * 1024
	iloveIMGPage        = "https://www.iloveimg.com/upscale-image"
	iloveIMGAPI         = "https://api29g.iloveimg.com/v1"
	videoAPI            = "https://api.unblurimage.ai/api/upscaler"
	videoCDN            = "https://cdn.unblurimage.ai"
	videoPollInterval   = 5 * time.Second
	videoPollMax        = 90
	videoUploadTimeout  = 2 * time.Minute
	videoJobTimeout     = 3 * time.Minute
	videoOutputMaxBytes = 64 * 1024 * 1024
	userAgent           = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

var (
	ImageLevels      = map[int]string{4: "4K"}
	iloveIMGTokenRE  = regexp.MustCompile(`"token"\s*:\s*"(eyJ[^"]+)"`)
	iloveIMGTaskIDRE = regexp.MustCompile(`ilovepdfConfig\.taskId\s*=\s*'([^']+)'`)
)

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

func download(ctx context.Context, url string) ([]byte, error) {
	return httpx.GetBytes(ctx, url, 2*time.Minute, 0)
}

func iloveIMGSession(ctx context.Context) (token, task string, err error) {
	resp, err := request(ctx, http.MethodGet, iloveIMGPage, nil, 30*time.Second, map[string]string{
		"Accept": "text/html,application/xhtml+xml",
	})
	if err != nil {
		return "", "", fmt.Errorf("gagal membuka iLoveIMG: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return "", "", fmt.Errorf("gagal membaca halaman iLoveIMG: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("iLoveIMG HTTP %d", resp.StatusCode)
	}
	tokenMatch := iloveIMGTokenRE.FindSubmatch(body)
	taskMatch := iloveIMGTaskIDRE.FindSubmatch(body)
	if len(tokenMatch) < 2 || len(taskMatch) < 2 {
		return "", "", fmt.Errorf("iLoveIMG tidak mengembalikan token/task")
	}
	return string(tokenMatch[1]), string(taskMatch[1]), nil
}

func iloveIMGUpload(ctx context.Context, data []byte, mimeType, token, task string) (string, error) {
	if mimeType == "" || !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		mimeType = "image/jpeg"
	}
	body, contentType, err := multipartBody(map[string]string{
		"name": "image.jpg", "chunk": "0", "chunks": "1", "task": task, "preview": "1", "v": "web.0",
	}, "file", "image.jpg", data, mimeType)
	if err != nil {
		return "", err
	}
	resp, err := request(ctx, http.MethodPost, iloveIMGAPI+"/upload", body, 2*time.Minute, map[string]string{
		"Authorization": "Bearer " + token,
		"Content-Type":  contentType,
		"Origin":        "https://www.iloveimg.com",
		"Referer":       iloveIMGPage,
	})
	if err != nil {
		return "", fmt.Errorf("gagal upload gambar ke iLoveIMG: %w", err)
	}
	responseBody, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return "", fmt.Errorf("gagal membaca respons upload iLoveIMG: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("upload iLoveIMG HTTP %d: %s", resp.StatusCode, responseSnippet(responseBody))
	}
	var result struct {
		ServerFilename string `json:"server_filename"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return "", fmt.Errorf("respons upload iLoveIMG tidak valid: %w", err)
	}
	if result.ServerFilename == "" {
		return "", fmt.Errorf("iLoveIMG tidak mengembalikan server_filename")
	}
	return result.ServerFilename, nil
}

func iloveIMGUpscale(ctx context.Context, serverFilename, token, task string, level int) ([]byte, error) {
	body, contentType, err := multipartBody(map[string]string{
		"task": task, "server_filename": serverFilename, "scale": strconv.Itoa(level),
	}, "", "", nil, "")
	if err != nil {
		return nil, err
	}
	resp, err := request(ctx, http.MethodPost, iloveIMGAPI+"/upscale", body, 5*time.Minute, map[string]string{
		"Authorization": "Bearer " + token,
		"Content-Type":  contentType,
		"Origin":        "https://www.iloveimg.com",
		"Referer":       iloveIMGPage,
	})
	if err != nil {
		return nil, fmt.Errorf("gagal memproses upscale iLoveIMG: %w", err)
	}
	result, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("gagal membaca hasil upscale iLoveIMG: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upscale iLoveIMG HTTP %d: %s", resp.StatusCode, responseSnippet(result))
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("iLoveIMG mengembalikan hasil gambar kosong")
	}
	detected := http.DetectContentType(result)
	if detected != "image/jpeg" && detected != "image/png" && detected != "image/webp" {
		return nil, fmt.Errorf("iLoveIMG tidak mengembalikan gambar: %s", responseSnippet(result))
	}
	return result, nil
}

func responseSnippet(data []byte) string {
	text := strings.TrimSpace(string(data))
	if len(text) > 160 {
		return text[:160] + "..."
	}
	return text
}

// UpscaleImage menaikkan foto memakai flow upscale-image milik iLoveIMG.
// Flow pada Pastebin hanya menyediakan scale 4, sehingga level yang diterima
// dibatasi ke 4K.
func UpscaleImage(ctx context.Context, data []byte, level int, mimeType string) ([]byte, error) {
	if _, ok := ImageLevels[level]; !ok {
		return nil, fmt.Errorf("resolusi %dK tidak didukung; gunakan 4K", level)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("gambar kosong")
	}
	token, task, err := iloveIMGSession(ctx)
	if err != nil {
		return nil, err
	}
	serverFilename, err := iloveIMGUpload(ctx, data, mimeType, token, task)
	if err != nil {
		return nil, err
	}
	return iloveIMGUpscale(ctx, serverFilename, token, task, level)
}

// UpscaleVideo memakai AI video enhancer dari bot lama. FreeConvert sengaja
// tidak dipakai lagi karena operasi compress membuat video lebih buram.
func UpscaleVideo(ctx context.Context, data []byte, fileName string) ([]byte, error) {
	if len(data) > VideoMaxBytes {
		return nil, fmt.Errorf("video terlalu besar: maksimal %d MB", VideoMaxBytes/(1024*1024))
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("video kosong")
	}
	if fileName == "" {
		fileName = "video.mp4"
	}
	identity := newVideoIdentity()
	sourceURL, err := uploadVideoSource(ctx, data, fileName, identity)
	if err != nil {
		return nil, err
	}
	jobID, err := createVideoJob(ctx, sourceURL, identity)
	if err != nil {
		return nil, err
	}
	outputURL, err := pollVideoJob(ctx, jobID, identity)
	if err != nil {
		return nil, err
	}
	return downloadVideoOutput(ctx, outputURL)
}

func newVideoIdentity() map[string]string {
	return map[string]string{
		"Accept":          "*/*",
		"Accept-Language": "en-US,en;q=0.9",
		"Origin":          "https://unblurimage.ai",
		"Referer":         "https://unblurimage.ai/",
		"User-Agent":      userAgent,
		"Product-Serial":  randomVideoID(),
		"Device-Id":       randomVideoID(),
	}
}

func randomVideoID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "rbot-video"
	}
	return hex.EncodeToString(raw[:])
}

func videoRequest(ctx context.Context, method, endpoint string, body io.Reader, timeout time.Duration, headers map[string]string) (*http.Response, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	req, err := http.NewRequestWithContext(cctx, method, endpoint, body)
	if err != nil {
		cancel()
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = &videoCancelBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

type videoCancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *videoCancelBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

func uploadVideoSource(ctx context.Context, data []byte, fileName string, identity map[string]string) (string, error) {
	body, contentType, err := multipartBody(map[string]string{"video_file_name": path.Base(fileName)}, "", "", nil, "")
	if err != nil {
		return "", err
	}
	resp, err := videoRequest(ctx, http.MethodPost, videoAPI+"/v1/ai-video-enhancer/upload-video", body, videoUploadTimeout, mergeVideoHeaders(identity, map[string]string{"Content-Type": contentType}))
	if err != nil {
		return "", fmt.Errorf("gagal meminta slot upload video: %w", err)
	}
	var ticket struct {
		Result struct {
			URL        string `json:"url"`
			ObjectName string `json:"object_name"`
		} `json:"result"`
	}
	if err := decodeVideoJSON(resp, &ticket); err != nil {
		return "", fmt.Errorf("slot upload video tidak valid: %w", err)
	}
	if ticket.Result.URL == "" || ticket.Result.ObjectName == "" {
		return "", fmt.Errorf("server AI tidak mengembalikan slot upload video")
	}
	putResp, err := videoRequest(ctx, http.MethodPut, ticket.Result.URL, bytes.NewReader(data), videoUploadTimeout, map[string]string{"Content-Type": "video/mp4", "Content-Length": strconv.Itoa(len(data))})
	if err != nil {
		return "", fmt.Errorf("gagal upload video ke server AI: %w", err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode < 200 || putResp.StatusCode >= 300 {
		return "", fmt.Errorf("upload video ke server AI HTTP %d", putResp.StatusCode)
	}
	return videoCDN + "/" + strings.TrimLeft(ticket.Result.ObjectName, "/"), nil
}

func createVideoJob(ctx context.Context, sourceURL string, identity map[string]string) (string, error) {
	body, contentType, err := multipartBody(map[string]string{
		"original_video_file": sourceURL,
		"resolution":          "4k",
		"is_preview":          "false",
	}, "", "", nil, "")
	if err != nil {
		return "", err
	}
	resp, err := videoRequest(ctx, http.MethodPost, videoAPI+"/v2/ai-video-enhancer/create-job", body, videoJobTimeout, mergeVideoHeaders(identity, map[string]string{"Content-Type": contentType}))
	if err != nil {
		return "", fmt.Errorf("gagal membuat job enhancer video: %w", err)
	}
	var created struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Result  struct {
			JobID string `json:"job_id"`
		} `json:"result"`
	}
	if err := decodeVideoJSON(resp, &created); err != nil {
		return "", fmt.Errorf("respons create-job video tidak valid: %w", err)
	}
	if created.Code != 100000 {
		if created.Message != "" {
			return "", fmt.Errorf("server AI menolak job video (code %d): %s", created.Code, created.Message)
		}
		return "", fmt.Errorf("server AI menolak job video (code %d)", created.Code)
	}
	if created.Result.JobID == "" {
		return "", fmt.Errorf("server AI tidak mengembalikan job video")
	}
	return created.Result.JobID, nil
}

func pollVideoJob(ctx context.Context, jobID string, identity map[string]string) (string, error) {
	endpoint := videoAPI + "/v2/ai-video-enhancer/get-job/" + url.PathEscape(jobID)
	for attempt := 0; attempt < videoPollMax; attempt++ {
		if err := sleepContext(ctx, videoPollInterval); err != nil {
			return "", err
		}
		resp, err := videoRequest(ctx, http.MethodGet, endpoint, nil, videoJobTimeout, identity)
		if err != nil {
			if isPermanentVideoError(err) {
				return "", err
			}
			continue
		}
		var status struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Result  struct {
				OutputURL string `json:"output_url"`
			} `json:"result"`
		}
		decodeErr := decodeVideoJSON(resp, &status)
		if decodeErr != nil {
			if isPermanentVideoError(decodeErr) {
				return "", decodeErr
			}
			continue
		}
		if status.Code == 100000 && status.Result.OutputURL != "" {
			return status.Result.OutputURL, nil
		}
		if status.Code == 300010 {
			// Job masih diproses.
			continue
		}
		if status.Message != "" {
			return "", fmt.Errorf("server AI gagal memproses video (code %d): %s", status.Code, status.Message)
		}
		return "", fmt.Errorf("server AI gagal memproses video (code %d)", status.Code)
	}
	return "", fmt.Errorf("server AI kelamaan memproses video")
}

func downloadVideoOutput(ctx context.Context, outputURL string) ([]byte, error) {
	parsed, err := url.Parse(outputURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return nil, fmt.Errorf("URL hasil video AI tidak valid")
	}
	data, err := httpx.GetBytes(ctx, outputURL, videoUploadTimeout, videoOutputMaxBytes)
	if err != nil {
		return nil, fmt.Errorf("gagal mengunduh hasil video AI: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("hasil video AI kosong")
	}
	if !looksLikeVideo(data) {
		return nil, fmt.Errorf("server AI mengembalikan file hasil yang bukan video")
	}
	return data, nil
}

func looksLikeVideo(data []byte) bool {
	// Jalur HD meminta MP4. Jangan menerima HTML, WebP, atau file acak yang
	// kebetulan diawali byte tertentu dari endpoint hasil AI.
	return len(data) >= 12 && string(data[4:8]) == "ftyp"
}

func isPermanentVideoError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "HTTP 400") || strings.Contains(message, "HTTP 401") || strings.Contains(message, "HTTP 403") || strings.Contains(message, "HTTP 404")
}

func mergeVideoHeaders(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func decodeVideoJSON(resp *http.Response, out any) error {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server AI HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return err
	}
	return nil
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
