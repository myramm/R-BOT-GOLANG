// Package upscaler menyediakan klien HTTP untuk meningkatkan kualitas foto/video.
// Endpoint dan alur job dipertahankan dari lib/upscaler.js pada bot Node lama.
package upscaler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rbot/lib/httpx"
)

const (
	VideoMaxBytes                = 10 * 1024 * 1024
	iloveIMGPage                 = "https://www.iloveimg.com/upscale-image"
	iloveIMGAPI                  = "https://api29g.iloveimg.com/v1"
	freeConvertAPI               = "https://api.freeconvert.com/v1"
	freeConvertVideoQuality      = 60
	freeConvertTokenMaxBodyBytes = 64 * 1024
	userAgent                    = "Gienetic/1.2.0 Mobile"
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

func findJSONTextPriority(value any, keys ...string) string {
	if object, ok := value.(map[string]any); ok {
		for _, key := range keys {
			if text, ok := object[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
		for _, nestedKey := range []string{"data", "result", "raw", "response", "payload"} {
			if nested, ok := object[nestedKey]; ok {
				if text := findJSONTextPriority(nested, keys...); text != "" {
					return text
				}
			}
		}
	}
	if list, ok := value.([]any); ok {
		for _, nested := range list {
			if text := findJSONTextPriority(nested, keys...); text != "" {
				return text
			}
		}
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func jsonText(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
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

type freeConvertJob struct {
	ID     string          `json:"id"`
	Status string          `json:"status"`
	Tasks  json.RawMessage `json:"tasks"`
}

func freeConvertJobBody(fileName string) map[string]any {
	if fileName == "" {
		fileName = "video.mp4"
	}
	return map[string]any{
		"tasks": map[string]any{
			"import-1": map[string]string{
				"operation": "import/upload",
			},
			"compress-1": map[string]any{
				"operation":     "compress",
				"input":         "import-1",
				"input_format":  "mp4",
				"output_format": "mp4",
				"options": map[string]any{
					"video_codec_compress":              "libx264",
					"compress_video":                    "by_percentage",
					"video_compress_quality_percentage": freeConvertVideoQuality,
				},
			},
			"export-1": map[string]string{
				"operation": "export/url",
				"input":     "compress-1",
				"filename":  path.Base(fileName),
			},
		},
	}
}

func freeConvertToken(ctx context.Context) (string, error) {
	resp, err := request(ctx, http.MethodGet, freeConvertAPI+"/account/guest", nil, time.Minute, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err != nil {
		return "", fmt.Errorf("gagal membaca respons guest FreeConvert")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("FreeConvert guest HTTP %d", resp.StatusCode)
	}
	if len(body) > freeConvertTokenMaxBodyBytes {
		return "", fmt.Errorf("respons guest FreeConvert terlalu besar")
	}
	return parseFreeConvertTokenResponse(body)
}

func parseFreeConvertTokenResponse(body []byte) (string, error) {
	if len(body) > freeConvertTokenMaxBodyBytes {
		return "", fmt.Errorf("respons guest FreeConvert terlalu besar")
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "", fmt.Errorf("FreeConvert tidak mengembalikan guest token")
	}

	var token string
	var value any
	if json.Unmarshal([]byte(text), &value) == nil {
		token = findJSONTextPriority(value, "token", "access_token", "accessToken")
		if token == "" {
			token = jsonText(value)
		}
	}
	if token == "" {
		// Endpoint guest pada beberapa deployment mengembalikan JWT mentah
		// (`eyJ...`) dengan content-type text/plain, bukan JSON.
		token = text
	}
	if validFreeConvertToken(token) {
		return token, nil
	}
	return "", fmt.Errorf("FreeConvert mengembalikan respons guest yang tidak valid")
}

func validFreeConvertToken(token string) bool {
	if !strings.HasPrefix(token, "eyJ") || strings.ContainsAny(token, "<>\r\n \t") {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
	}
	return true
}

func freeConvertJSON(ctx context.Context, method, endpoint, token string, payload any, timeout time.Duration) (json.RawMessage, error) {
	var bodyReader io.Reader
	headers := map[string]string{
		"Authorization": "Bearer " + token,
		"Accept":        "application/json",
	}
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(body)
		headers["Content-Type"] = "application/json"
	}
	resp, err := request(ctx, method, freeConvertAPI+endpoint, bodyReader, timeout, headers)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := decodeResponse(resp, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func freeConvertUpload(ctx context.Context, token string, job freeConvertJob, data []byte, fileName string) error {
	formURL, signature := findUploadForm(job.Tasks)
	if formURL == "" || signature == "" {
		return fmt.Errorf("FreeConvert tidak mengembalikan form upload")
	}
	body, contentType, err := multipartBody(map[string]string{"signature": signature}, "file", path.Base(fileName), data, "video/mp4")
	if err != nil {
		return err
	}
	resp, err := request(ctx, http.MethodPost, formURL, body, 5*time.Minute, map[string]string{
		"Authorization": "Bearer " + token,
		"Content-Type":  contentType,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("upload video ke FreeConvert HTTP %d", resp.StatusCode)
	}
	return nil
}

func findUploadForm(value json.RawMessage) (url, signature string) {
	var root any
	if json.Unmarshal(value, &root) != nil {
		return "", ""
	}
	var visit func(any)
	visit = func(current any) {
		if url != "" && signature != "" {
			return
		}
		if object, ok := current.(map[string]any); ok {
			if operation, _ := object["operation"].(string); operation == "import/upload" {
				if result, ok := object["result"].(map[string]any); ok {
					if form, ok := result["form"].(map[string]any); ok {
						url, _ = form["url"].(string)
						if parameters, ok := form["parameters"].(map[string]any); ok {
							signature, _ = parameters["signature"].(string)
						}
					}
				}
			}
			for _, nested := range object {
				visit(nested)
			}
		} else if list, ok := current.([]any); ok {
			for _, nested := range list {
				visit(nested)
			}
		}
	}
	visit(root)
	return url, signature
}

func freeConvertExportURL(tasks json.RawMessage) string {
	var root any
	if json.Unmarshal(tasks, &root) != nil {
		return ""
	}
	var output string
	var visit func(any)
	visit = func(current any) {
		if output != "" {
			return
		}
		if object, ok := current.(map[string]any); ok {
			if operation, _ := object["operation"].(string); operation == "export/url" {
				if result, ok := object["result"].(map[string]any); ok {
					output, _ = result["url"].(string)
				}
			}
			for _, nested := range object {
				visit(nested)
			}
		} else if list, ok := current.([]any); ok {
			for _, nested := range list {
				visit(nested)
			}
		}
	}
	visit(root)
	return output
}

func isPermanentFreeConvertPollError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "HTTP 401") || strings.Contains(message, "HTTP 403") || strings.Contains(message, "HTTP 404")
}

func (j freeConvertJob) Error() string {
	if j.Status != "" {
		return "FreeConvert job " + strings.ToLower(j.Status)
	}
	return "FreeConvert gagal membuat job"
}

// UpscaleVideo mengompres video memakai FreeConvert guest API dan menunggu hasilnya.
// API ini dipakai sebagai pengganti endpoint enhancer lama yang sering membalas
// "Parameter error". Batas input tetap 10 MB sesuai batas bot.
func UpscaleVideo(ctx context.Context, data []byte, fileName string) ([]byte, error) {
	if len(data) > VideoMaxBytes {
		return nil, fmt.Errorf("video terlalu besar: maksimal %d MB", VideoMaxBytes/(1024*1024))
	}
	if fileName == "" {
		fileName = "video.mp4"
	}
	token, err := freeConvertToken(ctx)
	if err != nil {
		return nil, err
	}
	createdRaw, err := freeConvertJSON(ctx, http.MethodPost, "/process/jobs", token, freeConvertJobBody(fileName), 2*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("gagal membuat job FreeConvert: %w", err)
	}
	var created freeConvertJob
	if err := json.Unmarshal(createdRaw, &created); err != nil || created.ID == "" {
		return nil, fmt.Errorf("FreeConvert tidak mengembalikan job id")
	}
	if err := freeConvertUpload(ctx, token, created, data, fileName); err != nil {
		return nil, err
	}

	var lastPollErr error
	for i := 0; i < 120; i++ {
		if err := sleepContext(ctx, 5*time.Second); err != nil {
			return nil, err
		}
		statusRaw, err := freeConvertJSON(ctx, http.MethodGet, "/process/jobs/"+created.ID, token, nil, time.Minute)
		if err != nil {
			if isPermanentFreeConvertPollError(err) {
				return nil, fmt.Errorf("gagal polling job FreeConvert: %w", err)
			}
			lastPollErr = err
			continue
		}
		var status freeConvertJob
		if err := json.Unmarshal(statusRaw, &status); err != nil {
			return nil, fmt.Errorf("decode status FreeConvert: %w", err)
		}
		switch strings.ToLower(status.Status) {
		case "failed", "error":
			return nil, status
		case "completed":
			if outputURL := freeConvertExportURL(status.Tasks); outputURL != "" {
				return download(ctx, outputURL)
			}
			return nil, fmt.Errorf("FreeConvert selesai tanpa URL hasil")
		}
	}
	if lastPollErr != nil {
		return nil, fmt.Errorf("gagal polling job FreeConvert: %w", lastPollErr)
	}
	return nil, fmt.Errorf("FreeConvert timeout menunggu hasil video")
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
