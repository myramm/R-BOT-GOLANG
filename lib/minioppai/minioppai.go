// Package minioppai adalah scraper minioppai.org: search series, metadata,
// daftar episode, dan opsi stream (mirror). Menggunakan kembali tipe dari
// watchhentai (SearchResult, EpisodeLink, SeriesInfo, DownloadOption) agar
// perintah .hentai dapat menggabungkan dua provider dengan satu struktur.
package minioppai

import (
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"rbot/lib/httpx"
	"rbot/lib/watchhentai"
)

const (
	BaseURL   = "https://minioppai.org"
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

// Provider adalah penanda sumber hasil minioppai.
const Provider = "MINIOPPAI"

var (
	reEpisodeNum    = regexp.MustCompile(`(?i)(?:episode|ep)[-_\s]*(\d+)`)
	reMirrorOpt     = regexp.MustCompile(`(?is)<option value="(.*?)">(.*?)</option>`)
	reIframeSrc     = regexp.MustCompile(`(?i)<iframe[^>]*src="([^"]+)"`)
	reQuality       = regexp.MustCompile(`(?i)(\d{3,4}p|\bmp4\b)`)
	reDirectMP4     = regexp.MustCompile(`(?i)https?://[^"'\s<>]+\.mp4`)
	reSkipLinkText  = regexp.MustCompile(`(?i)(watch\s*now|hentai\s*episodes|bukan\s*episode|next|prev|previous|back|home|menu)`)
)

// GetSeriesInfo mengambil metadata series (judul, genre, sinopsis, thumbnail)
// dari halaman /anime/<slug>/.
func GetSeriesInfo(ctx context.Context, seriesURL string) (*watchhentai.SeriesInfo, error) {
	body, err := fetchHTML(ctx, seriesURL)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	title := cleanText(doc.Find("h1").First().Text())
	thumbnail := doc.Find(`meta[property="og:image"]`).First().AttrOr("content", "")
	if thumbnail == "" {
		thumbnail = doc.Find(`img[src*="uploads"]`).First().AttrOr("src", "")
	}

	var genres []string
	genreSeen := make(map[string]bool)
	doc.Find(`a[href*="/genres/"]`).Each(func(i int, a *goquery.Selection) {
		g := cleanText(a.Text())
		if g != "" && !genreSeen[g] && len(genres) < 12 {
			genreSeen[g] = true
			genres = append(genres, g)
		}
	})

	synopsis := extractSynopsis(body, doc)

	if title == "" {
		return nil, fmt.Errorf("tidak dapat mem-parse halaman series %s", seriesURL)
	}
	return &watchhentai.SeriesInfo{
		Title:     title,
		URL:       seriesURL,
		Thumbnail: thumbnail,
		Synopsis:  synopsis,
		Genres:    genres,
	}, nil
}

// GetEpisodeList mengambil daftar episode dari halaman series /anime/<slug>/.
func GetEpisodeList(ctx context.Context, seriesURL string) ([]watchhentai.EpisodeLink, error) {
	body, err := fetchHTML(ctx, seriesURL)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	var eps []watchhentai.EpisodeLink
	seen := make(map[string]bool)
	doc.Find(`a[href]`).Each(func(i int, a *goquery.Selection) {
		href := a.AttrOr("href", "")
		linkText := strings.TrimSpace(a.Text())
		if reSkipLinkText.MatchString(linkText) {
			return
		}
		if !strings.HasPrefix(href, BaseURL+"/") || strings.Contains(href, "/anime/") || seen[href] {
			return
		}
		if !strings.Contains(href, "-episode-") && !strings.Contains(href, "-ep-") {
			return
		}
		seen[href] = true
		number := extractEpisodeNumber(slugFromURL(href))
		if number == "" {
			number = extractEpisodeNumber(linkText)
		}
		if number == "" {
			return
		}
		title := "Episode " + number
		eps = append(eps, watchhentai.EpisodeLink{Number: number, Title: title, URL: href})
	})

	sort.SliceStable(eps, func(i, j int) bool {
		return chapterLess(eps[i].Number, eps[j].Number)
	})
	return eps, nil
}

// Search mencari series di minioppai.org melalui halaman hasil pencarian HTML.
func Search(ctx context.Context, query string) ([]watchhentai.SearchResult, error) {
	searchURL := fmt.Sprintf("%s/?s=%s", BaseURL, url.QueryEscape(query))
	body, err := fetchHTML(ctx, searchURL)
	if err != nil {
		return nil, err
	}
	return searchFromHTML(body)
}

// searchFromHTML mengurai hasil pencarian seri dari HTML halaman ?s=<query>.
func searchFromHTML(body string) ([]watchhentai.SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	var results []watchhentai.SearchResult
	seen := make(map[string]bool)
	doc.Find(`a[href]`).Each(func(i int, a *goquery.Selection) {
		href := a.AttrOr("href", "")
		if !strings.HasPrefix(href, BaseURL+"/anime/") || seen[href] {
			return
		}
		seen[href] = true
		title := strings.TrimSpace(a.AttrOr("title", ""))
		thumb := ""
		if title == "" {
			img := a.Find("img").First()
			alt := img.AttrOr("alt", "")
			if alt != "" {
				title = cleanText(alt)
			}
		}
		if title == "" {
			return
		}
		img := a.Find("img").First()
		thumb = img.AttrOr("data-src", "")
		if thumb == "" {
			thumb = img.AttrOr("src", "")
		}
		results = append(results, watchhentai.SearchResult{
			Title:     cleanText(title),
			URL:       href,
			Thumbnail: thumb,
			Source:    Provider,
		})
	})

	// Dedup judul yang sama dari link berbeda (mis. thumbnail + judul).
	seenTitle := make(map[string]bool)
	dedup := results[:0]
	for _, r := range results {
		k := strings.ToLower(r.Title)
		if k != "" && seenTitle[k] {
			continue
		}
		seenTitle[k] = true
		dedup = append(dedup, r)
	}
	return dedup, nil
}

// GetEpisode mengambil metadata satu episode dari halaman episode, termasuk
// opsi stream (mirror) bila tersedia.
func GetEpisode(ctx context.Context, episodeURL string) (*watchhentai.Episode, error) {
	body, err := fetchHTML(ctx, episodeURL)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	title := cleanText(doc.Find("h1").First().Text())
	if title == "" {
		title = cleanText(doc.Find("title").First().Text())
	}
	thumbnail := doc.Find(`meta[property="og:image"]`).First().AttrOr("content", "")

	return &watchhentai.Episode{
		Source:    Provider,
		Title:     title,
		Slug:      slugFromURL(episodeURL),
		URL:       episodeURL,
		Thumbnail: thumbnail,
		VideoURL:  firstStreamURL(body),
	}, nil
}

// GetStreamOptions membaca dropdown "Pilih Server Video" (mirror) pada halaman
// episode. Setiap opsi memuat iframe stream, ditandai kualitas bila ada.
func GetStreamOptions(ctx context.Context, episodeURL string) []watchhentai.DownloadOption {
	body, err := fetchHTML(ctx, episodeURL)
	if err != nil {
		return nil
	}
	return parseStreamOptions(body)
}

// streamSRC mengekstrak URL stream (src iframe) dari nilai option mirror.
// Nilai bisa berupa HTML iframe langsung, base64 dari iframe, atau URL telanjang.
func streamSRC(val, pageBody string) string {
	v := strings.TrimSpace(val)

	// 1) Coba base64-decode (opsi mirror minioppai umumnya base64 iframe).
	if dec, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v)); err == nil {
		if sm := reIframeSrc.FindStringSubmatch(string(dec)); len(sm) > 1 {
			return normalizeStreamURL(sm[1])
		}
	}

	// 2) Iframe literal.
	if sm := reIframeSrc.FindStringSubmatch(v); len(sm) > 1 {
		return normalizeStreamURL(sm[1])
	}

	// 3) URL telanjang.
	if strings.HasPrefix(v, "http") {
		return v
	}

	// 4) Jika halaman hanya punya satu iframe utama, gunakan itu sebagai fallback.
	_ = pageBody
	return ""
}

func normalizeStreamURL(src string) string {
	src = strings.TrimSpace(src)
	if strings.HasPrefix(src, "//") {
		return "https:" + src
	}
	return src
}

// parseStreamOptions mengurai opsi stream dari HTML halaman episode minioppai.
func parseStreamOptions(body string) []watchhentai.DownloadOption {
	var opts []watchhentai.DownloadOption
	seen := make(map[string]bool)
	for _, m := range reMirrorOpt.FindAllStringSubmatch(body, -1) {
		val := strings.TrimSpace(m[1])
		label := strings.TrimSpace(m[2])
		if val == "" || label == "" || label == "Pilih Server Video" {
			continue
		}
		if seen[val] {
			continue
		}
		seen[val] = true
		// Opsi mirror berupa iframe (kadang di-encode base64); ambil src stream-nya.
		src := streamSRC(val, body)
		quality := normalizeQuality(label)
		if quality == "" && src != "" {
			quality = "Stream"
		}
		if src == "" {
			continue
		}
		opts = append(opts, watchhentai.DownloadOption{Quality: quality, URL: src})
	}
	// Jika tidak ada dropdown mirror, fallback ke iframe utama.
	if len(opts) == 0 {
		if sm := reIframeSrc.FindStringSubmatch(body); len(sm) > 1 {
			src := strings.TrimSpace(sm[1])
			if strings.HasPrefix(src, "//") {
				src = "https:" + src
			}
			opts = append(opts, watchhentai.DownloadOption{Quality: "Stream", URL: src})
		}
	}
	return opts
}

func fetchHTML(ctx context.Context, targetURL string) (string, error) {
	resp, err := httpx.Do(ctx, http.MethodGet, targetURL, nil, 20*time.Second, map[string]string{
		"Referer": BaseURL,
		"Accept":  "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("minioppai status HTTP %d", resp.StatusCode)
	}
	var b strings.Builder
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			b.Write(buf[:n])
		}
		if readErr != nil {
			break
		}
	}
	return b.String(), nil
}

func cleanText(s string) string {
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "MiniOppai", "")
	return strings.Trim(strings.TrimSpace(s), "-– ")
}

func slugFromURL(u string) string {
	parts := strings.Split(strings.Trim(u, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func extractEpisodeNumber(s string) string {
	if m := reEpisodeNum.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}

func extractSynopsis(body string, doc *goquery.Document) string {
	idx := strings.Index(strings.ToLower(body), "sinopsis")
	if idx < 0 {
		return ""
	}
	chunk := body[idx:]
	end := len(chunk)
	for _, marker := range []string{"Karakter & Pengisi Suara", "Karakter & Pengisi", "Karakter"} {
		if e := strings.Index(chunk, marker); e >= 0 && e < end {
			end = e
		}
	}
	chunk = chunk[:end]
	chunk = strings.ReplaceAll(chunk, "Sinopsis", " ")
	chunk = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(chunk, " ")
	chunk = html.UnescapeString(chunk)
	chunk = strings.Join(strings.Fields(chunk), " ")
	chunk = strings.TrimPrefix(chunk, strings.TrimSpace(doc.Find("h1").Text()))
	return strings.TrimSpace(chunk)
}

func normalizeQuality(label string) string {
	lower := strings.ToLower(strings.TrimSpace(label))
	if lower == "" || lower == "pilih server video" || lower == "server" || lower == "utama" || lower == "utam" {
		return ""
	}
	if m := reQuality.FindString(lower); m != "" {
		return strings.ToUpper(m)
	}
	return cleanText(label)
}

func firstStreamURL(body string) string {
	if sm := reIframeSrc.FindStringSubmatch(body); len(sm) > 1 {
		src := strings.TrimSpace(sm[1])
		if strings.HasPrefix(src, "//") {
			return "https:" + src
		}
		return src
	}
	return ""
}

// DownloadLink adalah tautan download dari satu provider/server.
type DownloadLink struct {
	Server    string `json:"server"`
	URL       string `json:"url"`
	DirectURL string `json:"direct_url,omitempty"`
}

// QualityGroup mengelompokkan link download berdasarkan kualitas dan format.
type QualityGroup struct {
	Quality string         `json:"quality"`
	Format  string         `json:"format"`
	Links   []DownloadLink `json:"links"`
}

// EpisodeDownload adalah hasil download per episode dengan daftar kualitas.
type EpisodeDownload struct {
	Title     string         `json:"title"`
	URL       string         `json:"url"`
	Qualities []QualityGroup `json:"qualities"`
	IsBatch   bool           `json:"is_batch"`
}

// GetEpisodeDownloads mengambil link download per kualitas/format dari halaman episode minioppai.
func GetEpisodeDownloads(ctx context.Context, epURL string) (*EpisodeDownload, error) {
	body, err := fetchHTML(ctx, epURL)
	if err != nil {
		return nil, err
	}
	title := cleanText(parseTitle(body))
	isBatch := strings.Contains(epURL, "/batch/") || strings.Contains(strings.ToLower(title), "batch")

	opts := parseStreamOptions(body)
	if len(opts) == 0 {
		return nil, fmt.Errorf("tidak ada pilihan kualitas tersedia untuk episode ini")
	}

	// Kelompokkan berdasarkan kualitas dan format
	groupMap := make(map[string]*QualityGroup)
	var order []string
	for _, o := range opts {
		quality := normalizeQuality(o.Quality)
		if quality == "" {
			quality = "Stream"
		}
		format := detectFormat(o.URL, quality)

		key := quality + "|" + format
		if _, ok := groupMap[key]; !ok {
			groupMap[key] = &QualityGroup{
				Quality: quality,
				Format:  format,
				Links:   []DownloadLink{},
			}
			order = append(order, key)
		}
		groupMap[key].Links = append(groupMap[key].Links, DownloadLink{
			Server:    detectServer(o.URL),
			URL:       o.URL,
			DirectURL: ResolveDirectLink(ctx, detectServer(o.URL), o.URL),
		})
	}

	var qualities []QualityGroup
	for _, key := range order {
		qualities = append(qualities, *groupMap[key])
	}

	return &EpisodeDownload{
		Title:     title,
		URL:       epURL,
		Qualities: qualities,
		IsBatch:   isBatch,
	}, nil
}

// ResolveDirectLink mencoba mengekstrak link unduhan langsung dari provider streaming.
func ResolveDirectLink(ctx context.Context, server, pageURL string) string {
	srvLower := strings.ToLower(server)

	// 1) Streampai: coba ambil direct MP4 dari halaman stream
	if strings.Contains(srvLower, "streampai") {
		return resolveStreampai(ctx, pageURL)
	}

	// 2) Jika bukan halaman stream, kembalikan URL langsung
	if strings.HasPrefix(pageURL, "http") && (strings.HasSuffix(pageURL, ".mp4") || strings.HasSuffix(pageURL, ".m3u8")) {
		return pageURL
	}

	return pageURL
}

// resolveStreampai mencoba mengekstrak URL video langsung dari halaman streampai.
func resolveStreampai(ctx context.Context, streamURL string) string {
	body, err := fetchHTML(ctx, streamURL)
	if err != nil {
		return streamURL
	}

	// Cari source MP4 langsung di halaman
	if m := regexp.MustCompile(`(?i)source\s*=\s*["']([^"']+\.mp4[^"']*)["']`).FindStringSubmatch(body); len(m) > 1 {
		return m[1]
	}
	if m := reDirectMP4.FindStringSubmatch(body); len(m) > 1 {
		return m[1]
	}

	// Cari di JavaScript variabel video source
	if m := regexp.MustCompile(`(?i)(?:file|source|video|url)\s*[:=]\s*["']([^"']+(?:\.mp4|\.m3u8)[^"']*)["']`).FindStringSubmatch(body); len(m) > 1 {
		return m[1]
	}

	return streamURL
}

func parseTitle(body string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return ""
	}
	return cleanText(doc.Find("h1").First().Text())
}

func detectFormat(url, quality string) string {
	if strings.Contains(strings.ToLower(url), ".mp4") || strings.HasSuffix(strings.ToLower(quality), "mp4") {
		return "MP4"
	}
	if strings.Contains(strings.ToLower(url), ".m3u8") || strings.Contains(strings.ToLower(quality), "x265") || strings.Contains(strings.ToLower(quality), "265") {
		return "x265"
	}
	return "MP4"
}

func detectServer(url string) string {
	if strings.Contains(url, "streampai") {
		return "Streampai"
	}
	if strings.Contains(url, "vidstream") {
		return "Vidstream"
	}
	if strings.Contains(url, "streamsb") {
		return "StreamSB"
	}
	if strings.Contains(url, "mp4upload") {
		return "MP4Upload"
	}
	return "Stream"
}

func chapterLess(a, b string) bool {
	ai, aerr := strconv.ParseFloat(a, 64)
	bi, berr := strconv.ParseFloat(b, 64)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	return a < b
}
