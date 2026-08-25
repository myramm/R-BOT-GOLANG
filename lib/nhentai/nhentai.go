// Package nhentai adalah scraper PDF doujin via mirror cin.guru:
// metadata dari __NEXT_DATA__, gambar halaman lewat proxy DuckDuckGo
// (host langsung i*.nhentai.net diblokir dari server).
package nhentai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	baseURL  = "https://cin.guru/v/%s/"
	ddgProxy = "https://external-content.duckduckgo.com/iu/?u="
	imgHost  = "https://i.nhentai.net/galleries/%d/%d.%s"
	thumbURL = "https://t.nhentai.net/galleries/%d/thumb.jpg"

	defaultUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
	nextKey   = `<script id="__NEXT_DATA__" type="application/json">`
)

// Gallery adalah metadata satu doujin.
type Gallery struct {
	Title   string
	MediaID int64
	Ext     []string // ekstensi tiap halaman: jpg/png/gif
}

type nextData struct {
	Props struct {
		PageProps struct {
			Data struct {
				Title struct {
					English  string `json:"english"`
					Japanese string `json:"japanese"`
				} `json:"title"`
				MediaID json.Number `json:"media_id"`
				Images  struct {
					Pages []struct {
						T string `json:"t"`
					} `json:"pages"`
				} `json:"images"`
			} `json:"data"`
		} `json:"pageProps"`
	} `json:"props"`
}

var reExt = regexp.MustCompile(`\.([a-z0-9]+)(?:\?.*)?$`)

// parse mengekstrak metadata doujin dari HTML cin.guru.
func parse(html string) (*Gallery, error) {
	i := strings.Index(html, nextKey)
	if i < 0 {
		return nil, fmt.Errorf("__NEXT_DATA__ tidak ditemukan")
	}
	payload := html[i+len(nextKey):]
	if j := strings.Index(payload, `</script>`); j >= 0 {
		payload = payload[:j]
	}

	var nd nextData
	if err := json.Unmarshal([]byte(payload), &nd); err != nil {
		return nil, fmt.Errorf("gagal parse JSON: %w", err)
	}
	d := nd.Props.PageProps.Data
	mediaID, errConv := d.MediaID.Int64()
	if errConv != nil || mediaID == 0 || len(d.Images.Pages) == 0 {
		return nil, fmt.Errorf("data doujin kosong / tidak ditemukan")
	}

	title := d.Title.English
	if title == "" {
		title = d.Title.Japanese
	}

	g := &Gallery{Title: title, MediaID: mediaID}
	for _, p := range d.Images.Pages {
		ext := "jpg"
		if m := reExt.FindStringSubmatch(p.T); len(m) > 1 {
			ext = m[1]
		}
		g.Ext = append(g.Ext, ext)
	}
	return g, nil
}

// Fetch mengambil metadata doujin berdasarkan kode (contoh: "177013").
func Fetch(ctx context.Context, code string) (*Gallery, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(baseURL, code), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultUA)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cin.guru status HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return parse(string(body))
}

// ImageURLs mengembalikan URL gambar tiap halaman via proxy DDG.
func (g *Gallery) ImageURLs() []string {
	urls := make([]string, 0, len(g.Ext))
	for i, ext := range g.Ext {
		target := fmt.Sprintf(imgHost, g.MediaID, i+1, ext)
		urls = append(urls, ddgProxy+target)
	}
	return urls
}

// ThumbURL mengembalikan URL thumbnail via proxy DDG.
func (g *Gallery) ThumbURL() string {
	return ddgProxy + fmt.Sprintf(thumbURL, g.MediaID)
}

// FileName membersihkan judul untuk dipakai sebagai nama file PDF.
func FileName(title string) string {
	clean := regexp.MustCompile(`[^\w\s-]`).ReplaceAllString(title, "")
	clean = regexp.MustCompile(`\s+`).ReplaceAllString(strings.TrimSpace(clean), "_")
	if clean == "" {
		clean = "nhentai"
	}
	return clean + ".pdf"
}
