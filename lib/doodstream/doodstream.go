package doodstream

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

var (
	reDoodDomain = regexp.MustCompile(`(?i)(doodstream|dood|dso|ds2play|myvidplay|ds2video|do0od|dood\.la|dood\.ws|dood\.so|dood\.pm|dood\.to|dood\.watch|dood\.wf|dood\.re|dood\.video|dood\.work)`)
	reFileCode   = regexp.MustCompile(`/(?:d|e)/([A-Za-z0-9]+)`)
	reTitle      = regexp.MustCompile(`(?i)<title>([^<]+)</title>`)
	rePassMD5    = regexp.MustCompile(`/(pass_md5/[^'"]+)`)
)

type Result struct {
	Title        string
	FileCode     string
	DownloadURL  string
	Thumbnail    string
	IsCloudflare bool
}

// IsDoodURL menguji apakah URL merupakan domain DoodStream yang didukung.
func IsDoodURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return reDoodDomain.MatchString(u.Hostname())
}

// ExtractFileCode mengambil filecode dari URL DoodStream.
func ExtractFileCode(rawURL string) string {
	m := reFileCode.FindStringSubmatch(rawURL)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func randomString(length int) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// Resolve mengekstrak data video dan URL MP4 langsung dari link DoodStream.
func Resolve(ctx context.Context, rawURL string) (*Result, error) {
	if !IsDoodURL(rawURL) {
		return nil, fmt.Errorf("bukan URL DoodStream yang valid")
	}

	fileCode := ExtractFileCode(rawURL)
	if fileCode == "" {
		return nil, fmt.Errorf("filecode DoodStream tidak ditemukan di URL")
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("URL tidak valid: %w", err)
	}

	origin := parsedURL.Scheme + "://" + parsedURL.Hostname()
	if parsedURL.Port() != "" {
		origin += ":" + parsedURL.Port()
	}

	embedURL := origin + "/e/" + fileCode

	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("terlalu banyak redirect")
			}
			return nil
		},
	}

	// 1. Fetch halaman embed /e/<filecode>
	reqEmbed, err := http.NewRequestWithContext(ctx, "GET", embedURL, nil)
	if err != nil {
		return nil, err
	}
	reqEmbed.Header.Set("User-Agent", userAgent)
	reqEmbed.Header.Set("Referer", origin+"/")
	reqEmbed.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	respEmbed, err := client.Do(reqEmbed)
	if err != nil {
		return nil, fmt.Errorf("gagal mengakses embed DoodStream: %w", err)
	}
	defer respEmbed.Body.Close()

	if respEmbed.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed DoodStream mengembalikan status HTTP %d", respEmbed.StatusCode)
	}

	bodyEmbedBytes, err := io.ReadAll(respEmbed.Body)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca body embed: %w", err)
	}
	bodyEmbed := string(bodyEmbedBytes)

	if !strings.Contains(bodyEmbed, "pass_md5") {
		// Proteksi Cloudflare Turnstile terdeteksi
		fallbackURL := fmt.Sprintf("https://9xbuddy.com/process?url=%s", url.QueryEscape(embedURL))
		return &Result{
			Title:        "DoodStream Video",
			FileCode:     fileCode,
			DownloadURL:  fallbackURL,
			IsCloudflare: true,
		}, nil
	}

	// Extract Title
	title := "DoodStream Video"
	if m := reTitle.FindStringSubmatch(bodyEmbed); len(m) > 1 {
		title = strings.TrimSpace(m[1])
		title = regexp.MustCompile(`(?i)\s*-\s*DoodStream.*$`).ReplaceAllString(title, "")
		title = strings.TrimSpace(title)
	}

	// Extract pass_md5 path
	passMatch := rePassMD5.FindStringSubmatch(bodyEmbed)
	if len(passMatch) < 2 {
		return nil, fmt.Errorf("pass_md5 path tidak ditemukan")
	}
	passPath := passMatch[1]

	// Extract token if present
	token := ""
	parts := strings.Split(passPath, "/")
	if len(parts) >= 3 {
		token = parts[len(parts)-1]
	}

	// Extract cookies
	cookies := respEmbed.Cookies()
	cookieHeader := ""
	for _, c := range cookies {
		cookieHeader += c.Name + "=" + c.Value + "; "
	}
	cookieHeader = strings.TrimSuffix(cookieHeader, "; ")

	// 2. Request pass_md5 endpoint
	passURL := origin + "/" + passPath
	reqPass, err := http.NewRequestWithContext(ctx, "GET", passURL, nil)
	if err != nil {
		return nil, err
	}
	reqPass.Header.Set("User-Agent", userAgent)
	reqPass.Header.Set("Referer", embedURL)
	reqPass.Header.Set("X-Requested-With", "XMLHttpRequest")
	reqPass.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	if cookieHeader != "" {
		reqPass.Header.Set("Cookie", cookieHeader)
	}

	respPass, err := client.Do(reqPass)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil pass_md5: %w", err)
	}
	defer respPass.Body.Close()

	bodyPassBytes, err := io.ReadAll(respPass.Body)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca pass_md5 body: %w", err)
	}
	mp4Base := strings.TrimSpace(string(bodyPassBytes))
	if !strings.HasPrefix(mp4Base, "http://") && !strings.HasPrefix(mp4Base, "https://") {
		return nil, fmt.Errorf("pass_md5 tidak mengembalikan URL mp4 yang valid: %s", mp4Base)
	}

	// 3. Form final download URL
	randStr := randomString(10)
	expiry := time.Now().UnixNano() / int64(time.Millisecond)
	downloadURL := fmt.Sprintf("%s%s?token=%s&expiry=%d", mp4Base, randStr, token, expiry)

	return &Result{
		Title:       title,
		FileCode:    fileCode,
		DownloadURL: downloadURL,
	}, nil
}
