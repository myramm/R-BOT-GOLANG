package cmd

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"

	"rbot/brain/command"
	"rbot/brain/config"
)

const toimgMaxBytes = 20 * 1024 * 1024

type toimgSource struct {
	message *waE2E.Message
	sticker bool
	pdf     bool
}

func init() {
	command.Register(&command.Command{
		Name:        "toimg",
		Category:    "Converter",
		Alias:       []string{"toimage", "tomedia", "unstick"},
		Description: "Ubah stiker menjadi gambar/video atau PDF menjadi gambar",
		Handler:     toimgHandler,
	})
}

func toimgSourceMessage(c *command.Ctx) *toimgSource {
	if source := toimgInspect(c.Evt.Message); source != nil {
		return source
	}
	if ci := c.ContextInfo(); ci != nil {
		return toimgInspect(ci.GetQuotedMessage())
	}
	return nil
}

func toimgInspect(m *waE2E.Message) *toimgSource {
	m = hdUnwrap(m)
	if m == nil {
		return nil
	}
	if m.GetStickerMessage() != nil {
		return &toimgSource{message: m, sticker: true}
	}
	if doc := m.GetDocumentMessage(); doc != nil && strings.EqualFold(doc.GetMimetype(), "application/pdf") {
		return &toimgSource{message: m, pdf: true}
	}
	return nil
}

func toimgHandler(ctx context.Context, c *command.Ctx) error {
	source := toimgSourceMessage(c)
	if source == nil {
		_, err := c.Reply(ctx, "Reply stiker atau file PDF dengan *"+config.MainPrefix()+"toimg*.")
		return err
	}
	processCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	data, err := c.Client.DownloadAny(processCtx, source.message)
	if err != nil || len(data) == 0 {
		return toimgError(ctx, c, "Gagal mengunduh media: "+errorText(err, "media kosong"))
	}
	if len(data) > toimgMaxBytes {
		return toimgWarn(ctx, c, fmt.Sprintf("File terlalu besar. Maksimal %dMB.", toimgMaxBytes/(1024*1024)))
	}
	c.React(ctx, "⏳")
	if source.pdf {
		return toimgPDF(ctx, c, processCtx, data)
	}
	return toimgSticker(ctx, c, processCtx, data)
}

func toimgSticker(ctx context.Context, c *command.Ctx, processCtx context.Context, data []byte) error {
	input, err := os.CreateTemp("", "rbot-toimg-*.webp")
	if err != nil {
		return toimgError(ctx, c, err.Error())
	}
	inputName := input.Name()
	defer os.Remove(inputName)
	if _, err := input.Write(data); err != nil {
		_ = input.Close()
		return toimgError(ctx, c, err.Error())
	}
	if err := input.Close(); err != nil {
		return toimgError(ctx, c, err.Error())
	}

	// Jangan memakai ffprobe -count_frames untuk menentukan animasi. Pada
	// beberapa WebP sticker WhatsApp, timestamp/frame duration yang rusak
	// membuat ffprobe mengembalikan hasil yang tidak konsisten dan ffmpeg
	// kemudian berhenti dengan "maximum 0.666667" tanpa menulis frame.
	if isAnimatedWebP(data) {
		outputName := inputName + ".mp4"
		defer os.Remove(outputName)
		args := []string{
			"-y", "-hide_banner", "-loglevel", "error", "-max_error_rate", "1.0",
			"-fflags", "+discardcorrupt", "-err_detect", "ignore_err", "-i", inputName,
			"-vf", "fps=30,scale=trunc(iw/2)*2:trunc(ih/2)*2:flags=lanczos,format=yuv420p",
			"-an", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart", outputName,
		}
		if _, err := exec.CommandContext(processCtx, "ffmpeg", args...).CombinedOutput(); err == nil {
			if result, readErr := readNonEmptyFile(outputName); readErr == nil {
				if sendErr := c.SendMediaBytes(ctx, result, command.MediaVideo, "🎞️ Stiker → video", "sticker.mp4", "video/mp4"); sendErr != nil {
					return toimgError(ctx, c, sendErr.Error())
				}
				c.React(ctx, "✅")
				return nil
			}
		}
	}

	outputName := inputName + ".png"
	defer os.Remove(outputName)
	args := []string{
		"-y", "-hide_banner", "-loglevel", "error", "-max_error_rate", "1.0",
		"-fflags", "+discardcorrupt", "-err_detect", "ignore_err", "-i", inputName,
		"-frames:v", "1", "-vf", "format=rgba", outputName,
	}
	if out, err := exec.CommandContext(processCtx, "ffmpeg", args...).CombinedOutput(); err != nil {
		return toimgError(ctx, c, "ffmpeg gagal: "+tailText(string(out), 240))
	}
	result, err := readNonEmptyFile(outputName)
	if err != nil {
		return toimgError(ctx, c, err.Error())
	}
	if err := c.SendMediaBytes(ctx, result, command.MediaImage, "🖼️ Stiker → gambar", "sticker.png", "image/png"); err != nil {
		return toimgError(ctx, c, err.Error())
	}
	c.React(ctx, "✅")
	return nil
}

// isAnimatedWebP membaca flag animasi dari header WebP tanpa mendekode semua
// frame. VP8X menyimpan animation bit pada byte flags, sedangkan ANIM adalah
// fallback untuk file WebP yang tidak memiliki chunk VP8X standar.
func isAnimatedWebP(data []byte) bool {
	if len(data) < 16 || !bytes.Equal(data[:4], []byte("RIFF")) || !bytes.Equal(data[8:12], []byte("WEBP")) {
		return false
	}
	if bytes.Equal(data[12:16], []byte("VP8X")) && len(data) > 20 {
		return data[20]&0x02 != 0
	}
	return hasWebPChunk(data[12:], "ANIM")
}

func hasWebPChunk(data []byte, wanted string) bool {
	if len(wanted) != 4 {
		return false
	}
	for offset := 0; offset+8 <= len(data); {
		if string(data[offset:offset+4]) == wanted {
			return true
		}
		size := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		next := offset + 8 + size
		if size&1 != 0 {
			next++
		}
		if next <= offset || next > len(data) {
			return false
		}
		offset = next
	}
	return false
}

func readNonEmptyFile(name string) ([]byte, error) {
	result, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("hasil ffmpeg kosong")
	}
	return result, nil
}

func toimgPDF(ctx context.Context, c *command.Ctx, processCtx context.Context, data []byte) error {
	key := strings.TrimSpace(config.C.ILovePDF.KeyLove)
	if key == "" {
		key = strings.TrimSpace(config.C.ILovePDF.PublicKey)
	}
	if key != "" {
		if pages, err := ilovePDFToJPG(processCtx, key, data); err == nil {
			for i, page := range pages {
				caption := fmt.Sprintf("📄 Halaman %d/%d", i+1, len(pages))
				if sendErr := c.SendMediaBytes(ctx, page, command.MediaImage, caption, fmt.Sprintf("page-%d.jpg", i+1), "image/jpeg"); sendErr != nil {
					return toimgError(ctx, c, sendErr.Error())
				}
			}
			c.React(ctx, "✅")
			return nil
		} else {
			log.Printf("[rbot] iLovePDF PDF->JPG gagal, fallback ke pdftoppm")
		}
	}

	dir, err := os.MkdirTemp("", "rbot-toimg-pdf-")
	if err != nil {
		return toimgError(ctx, c, err.Error())
	}
	defer os.RemoveAll(dir)
	inputName := filepath.Join(dir, "input.pdf")
	if err := os.WriteFile(inputName, data, 0o600); err != nil {
		return toimgError(ctx, c, err.Error())
	}
	prefix := filepath.Join(dir, "page")
	cmd := exec.CommandContext(processCtx, "pdftoppm", "-f", "1", "-l", "5", "-jpeg", "-r", "120", inputName, prefix)
	if out, err := cmd.CombinedOutput(); err != nil {
		return toimgError(ctx, c, "pdftoppm gagal: "+tailText(string(out), 240))
	}
	pages, err := filepath.Glob(prefix + "-*.jpg")
	if err != nil || len(pages) == 0 {
		return toimgError(ctx, c, "PDF tidak menghasilkan gambar halaman")
	}
	for i, page := range pages {
		result, readErr := os.ReadFile(page)
		if readErr != nil {
			continue
		}
		caption := fmt.Sprintf("📄 Halaman %d/%d", i+1, len(pages))
		if sendErr := c.SendMediaBytes(ctx, result, command.MediaImage, caption, fmt.Sprintf("page-%d.jpg", i+1), "image/jpeg"); sendErr != nil {
			return toimgError(ctx, c, sendErr.Error())
		}
	}
	c.React(ctx, "✅")
	return nil
}

const (
	ilovePDFAPIBase       = "https://api.ilovepdf.com/v1"
	ilovePDFRegion        = "eu"
	ilovePDFMaxPages      = 5
	ilovePDFMaxImageBytes = 5 * 1024 * 1024
	ilovePDFMaxZIPBytes   = 30 * 1024 * 1024
)

type ilovePDFTask struct {
	Server string `json:"server"`
	Task   string `json:"task"`
}

func ilovePDFToJPG(ctx context.Context, publicKey string, data []byte) ([][]byte, error) {
	token, err := ilovePDFAuth(ctx, publicKey)
	if err != nil {
		return nil, err
	}
	task, err := ilovePDFStart(ctx, token)
	if err != nil {
		return nil, err
	}
	serverFilename, err := ilovePDFUpload(ctx, token, task, data)
	if err != nil {
		return nil, err
	}
	if err := ilovePDFProcess(ctx, token, task, serverFilename); err != nil {
		return nil, err
	}
	archive, err := ilovePDFDownload(ctx, token, task)
	if err != nil {
		return nil, err
	}
	return ilovePDFExtractImages(archive)
}

func ilovePDFRequest(ctx context.Context, method, endpoint, token string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("iLovePDF HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func ilovePDFAuth(ctx context.Context, publicKey string) (string, error) {
	endpoint := ilovePDFAPIBase + "/auth?public_key=" + url.QueryEscape(publicKey)
	resp, err := ilovePDFRequest(ctx, http.MethodGet, endpoint, "", nil, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || result.Token == "" {
		return "", fmt.Errorf("iLovePDF auth tidak mengembalikan token")
	}
	return result.Token, nil
}

func ilovePDFStart(ctx context.Context, token string) (ilovePDFTask, error) {
	resp, err := ilovePDFRequest(ctx, http.MethodGet, ilovePDFAPIBase+"/start/pdfjpg/"+ilovePDFRegion, token, nil, "")
	if err != nil && strings.Contains(err.Error(), "HTTP 404") {
		resp, err = ilovePDFRequest(ctx, http.MethodGet, ilovePDFAPIBase+"/start/pdfjpg", token, nil, "")
	}
	if err != nil {
		return ilovePDFTask{}, err
	}
	defer resp.Body.Close()
	var task ilovePDFTask
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil || task.Server == "" || task.Task == "" {
		return ilovePDFTask{}, fmt.Errorf("iLovePDF start task tidak valid")
	}
	return task, nil
}

func ilovePDFUpload(ctx context.Context, token string, task ilovePDFTask, data []byte) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("task", task.Task); err != nil {
		return "", err
	}
	part, err := writer.CreateFormFile("file", "input.pdf")
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	resp, err := ilovePDFRequest(ctx, http.MethodPost, "https://"+task.Server+"/v1/upload", token, &body, writer.FormDataContentType())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		ServerFilename string `json:"server_filename"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || result.ServerFilename == "" {
		return "", fmt.Errorf("iLovePDF upload tidak mengembalikan filename")
	}
	return result.ServerFilename, nil
}

func ilovePDFProcess(ctx context.Context, token string, task ilovePDFTask, serverFilename string) error {
	payload, err := json.Marshal(map[string]any{
		"task":  task.Task,
		"tool":  "pdfjpg",
		"files": []map[string]string{{"server_filename": serverFilename, "filename": "input.pdf"}},
	})
	if err != nil {
		return err
	}
	resp, err := ilovePDFRequest(ctx, http.MethodPost, "https://"+task.Server+"/v1/process", token, bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func ilovePDFDownload(ctx context.Context, token string, task ilovePDFTask) ([]byte, error) {
	resp, err := ilovePDFRequest(ctx, http.MethodGet, "https://"+task.Server+"/v1/download/"+url.PathEscape(task.Task), token, nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, ilovePDFMaxZIPBytes+1))
}

func ilovePDFExtractImages(data []byte) ([][]byte, error) {
	if isJPEGBytes(data) {
		return [][]byte{data}, nil
	}
	if len(data) > ilovePDFMaxZIPBytes {
		return nil, fmt.Errorf("arsip hasil iLovePDF terlalu besar")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("hasil iLovePDF bukan ZIP valid")
	}
	files := make([]*zip.File, 0, len(reader.File))
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || filepath.Base(file.Name) != file.Name {
			continue
		}
		if strings.EqualFold(filepath.Ext(file.Name), ".jpg") || strings.EqualFold(filepath.Ext(file.Name), ".jpeg") {
			files = append(files, file)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	if len(files) > ilovePDFMaxPages {
		files = files[:ilovePDFMaxPages]
	}
	pages := make([][]byte, 0, len(files))
	for _, file := range files {
		if file.UncompressedSize64 > ilovePDFMaxImageBytes {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			continue
		}
		page, readErr := io.ReadAll(io.LimitReader(reader, ilovePDFMaxImageBytes+1))
		_ = reader.Close()
		if readErr != nil || len(page) == 0 || len(page) > ilovePDFMaxImageBytes || !isJPEGBytes(page) {
			continue
		}
		pages = append(pages, page)
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("iLovePDF tidak menghasilkan gambar")
	}
	return pages, nil
}

func isJPEGBytes(data []byte) bool {
	return len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff
}

func toimgWarn(ctx context.Context, c *command.Ctx, message string) error {
	c.React(ctx, "❌")
	_, _ = c.Reply(ctx, "❌ "+message)
	return nil
}

func toimgError(ctx context.Context, c *command.Ctx, message string) error {
	c.React(ctx, "❌")
	_, _ = c.Reply(ctx, "❌ "+message)
	c.ReportErrorMessage(ctx, message)
	return nil
}
