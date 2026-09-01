// Package minioppai adalah scraper minioppai.org: search series, metadata,
// daftar episode, opsi stream (mirror), dan bagian "Download" halaman episode.
// Satu-satunya provider untuk perintah .hentai; tipe hasil didefinisikan di
// sini (self-contained) sehingga tidak bergantung pada 
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
)

const (
	BaseURL   = "https://minioppai.org"
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

// Provider adalah penanda sumber hasil minioppai.
const Provider = "MINIOPPAI"

// SearchResult adalah satu hasil pencarian series/episode.
type SearchResult struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Thumbnail string `json:"thumbnail,omitempty"`
	Source    string `json:"source,omitempty"` // penanda provider (kosong = MINIOPPAI)
}

// SeriesInfo adalah metadata halaman series: judul, sinopsis, dan genre.
type SeriesInfo struct {
	Title     string   `json:"title"`
	URL       string   `json:"url"`
	Thumbnail string   `json:"thumbnail,omitempty"`
	Synopsis  string   `json:"synopsis,omitempty"`
	Genres    []string `json:"genres,omitempty"`
}

// Episode adalah metadata lengkap satu halaman episode.
type Episode struct {
	Source      string `json:"source"`
	Title       string `json:"title"`
	Slug        string `json:"slug"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	Thumbnail   string `json:"thumbnail,omitempty"`
	VideoURL    string `json:"video_url,omitempty"`
}

// EpisodeLink adalah tautan satu episode pada halaman series.
type EpisodeLink struct {
	Number string `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

// DownloadOption adalah satu pilihan kualitas stream pada halaman episode.
type DownloadOption struct {
	Quality string `json:"quality"`
	URL     string `json:"url"`
}

var (
	reEpisodeNum    = regexp.MustCompile(`(?i)(?:episode|ep)[-_\s]*(\d+)`)
	reMirrorOpt     = regexp.MustCompile(`(?is)<option value="(.*?)">(.*?)</option>`)
	reIframeSrc     = regexp.MustCompile(`(?i)<iframe[^>]*src="([^"]+)"`)
	reQuality       = regexp.MustCompile(`(?i)(\d{3,4}p|\bmp4\b)`)
	reDirectMP4     = regexp.MustCompile(`(?i)https?://[^"'\s<>]+\.mp4`)
	reSkipLinkText = regexp.MustCompile(`(?i)(watch\s*now|hentai\s*episodes|bukan\s*episode|next|prev|previous|back|home|menu|^\s*(id|eng|sub|indo|english)\s*$|\s*(id|eng|sub)\s*$)`)
)

// GetSeriesInfo mengambil metadata series (judul, genre, sinopsis, thumbnail)
// dari halaman /anime/<slug>/.
func GetSeriesInfo(ctx context.Context, seriesURL string) (*SeriesInfo, error) {
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
	return &SeriesInfo{
		Title:     title,
		URL:       seriesURL,
		Thumbnail: thumbnail,
		Synopsis:  synopsis,
		Genres:    genres,
	}, nil
}

// GetEpisodeList mengambil daftar episode dari halaman series /anime/<slug>/.
func GetEpisodeList(ctx context.Context, seriesURL string) ([]EpisodeLink, error) {
	body, err := fetchHTML(ctx, seriesURL)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	var eps []EpisodeLink
	seen := make(map[string]bool)
	doc.Find(`a[href]`).Each(func(i int, a *goquery.Selection) {
		href := a.AttrOr("href", "")
		linkText := strings.TrimSpace(a.Text())
		if reSkipLinkText.MatchString(linkText) {
			return
		}
		if len(linkText) < 3 {
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
		eps = append(eps, EpisodeLink{Number: number, Title: title, URL: href})
	})

	sort.SliceStable(eps, func(i, j int) bool {
		return chapterLess(eps[i].Number, eps[j].Number)
	})
	return eps, nil
}

// Search mencari series di minioppai.org melalui halaman hasil pencarian HTML.
func Search(ctx context.Context, query string) ([]SearchResult, error) {
	searchURL := fmt.Sprintf("%s/?s=%s", BaseURL, url.QueryEscape(query))
	body, err := fetchHTML(ctx, searchURL)
	if err != nil {
		return nil, err
	}
	return searchFromHTML(body)
}

// searchFromHTML mengurai hasil pencarian seri dari HTML halaman ?s=<query>.
func searchFromHTML(body string) ([]SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	var results []SearchResult
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
		results = append(results, SearchResult{
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
func GetEpisode(ctx context.Context, episodeURL string) (*Episode, error) {
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

	return &Episode{
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
func GetStreamOptions(ctx context.Context, episodeURL string) []DownloadOption {
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
func parseStreamOptions(body string) []DownloadOption {
	var opts []DownloadOption
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
		opts = append(opts, DownloadOption{Quality: quality, URL: src})
	}
	// Jika tidak ada dropdown mirror, fallback ke iframe utama.
	if len(opts) == 0 {
		if sm := reIframeSrc.FindStringSubmatch(body); len(sm) > 1 {
			src := strings.TrimSpace(sm[1])
			if strings.HasPrefix(src, "//") {
				src = "https:" + src
			}
			opts = append(opts, DownloadOption{Quality: "Stream", URL: src})
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

// GetEpisodeDownloads mengambil link download per kualitas dari halaman episode
// minioppai. Bagian "Download" memuat baris kualitas (.soraurlx): label kualitas
// (<strong>) diikuti link provider (MediaFire, KrakenFiles, PixelDrain).
func GetEpisodeDownloads(ctx context.Context, epURL string) (*EpisodeDownload, error) {
	body, err := fetchHTML(ctx, epURL)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	title, groups := parseDownloadSection(doc, body)
	if len(groups) == 0 {
		return nil, fmt.Errorf("tidak ada pilihan download tersedia untuk episode ini")
	}
	isBatch := strings.Contains(epURL, "/batch/") || strings.Contains(strings.ToLower(title), "batch")

	return &EpisodeDownload{
		Title:     title,
		URL:       epURL,
		Qualities: groups,
		IsBatch:   isBatch,
	}, nil
}

// qualityRank memetakan kualitas ke urutan tetap: 1080p -> 720p -> 480p -> 360p.
// Kualitas di luar daftar dikenal diberi prioritas terendah dan tetap diurutkan
// sesuai urutan dokumen di antara mereka.
func qualityRank(q string) int {
	switch strings.ToLower(strings.TrimSpace(q)) {
	case "1080p":
		return 0
	case "720p":
		return 1
	case "480p":
		return 2
	case "360p":
		return 3
	}
	return 99
}

// parseDownloadSection mengurai bagian "Download" pada halaman episode. Struktur
// (dikonfirmasi dari minioppai.org/hajimete-no-hitozuma-episode-6/):
//
//	<div class="soraddlx soradlg">
//	  <div class="sorattlx"><h3>{judul episode}</h3></div>
//	  <div class="soraurlx"><strong>1080p</strong>
//	    <a href="...">MediaFire</a><a href="...">KrakenFiles</a><a>PixelDrain</a></div>
//	  <div class="soraurlx"><strong>720p</strong>...</div>
//	  ...
//	</div>
//
// Parser menggunakan selector semantik/atribut dengan fallback bila struktur
// sedikit berubah, dan tidak pernah memproduksi URL palsu: hanya <a href>
// yang valid yang dimasukkan, sibling non-link diabaikan.
func parseDownloadSection(doc *goquery.Document, body string) (string, []QualityGroup) {
	// 1) Judul episode: prioritas pada heading baris download, lalu h1, lalu <title>.
	title := cleanText(doc.Find(".sorattlx h3, .sorattlx").First().Text())
	if title == "" {
		title = cleanText(doc.Find("h1").First().Text())
	}
	if title == "" {
		title = cleanText(doc.Find("title").First().Text())
	}

	// 2) Kumpulkan baris kualitas. Prioritas utama: .soraurlx dalam konten utama.
	var rows []*goquery.Selection
	doc.Find(".soraurlx").Each(func(i int, s *goquery.Selection) {
		rows = append(rows, s)
	})

	// 3) Fallback: bila tidak ada .soraurlx, cari di dalam blok "bixbox" yang
	//    diawali heading berisi kata "Download" — urai <strong>/<a> berurutan.
	if len(rows) == 0 {
		rows = fallbackDownloadRows(doc)
	}

	var groups []QualityGroup
	seen := make(map[string]bool)
	for _, row := range rows {
		quality := strings.TrimSpace(row.Find("strong").First().Text())
		if quality == "" {
			quality = strings.TrimSpace(row.Text())
		}
		quality = normalizeDownloadQuality(quality)
		if quality == "" || seen[quality] {
			continue
		}
		seen[quality] = true

		var links []DownloadLink
		row.Find("a[href]").Each(func(j int, a *goquery.Selection) {
			href := strings.TrimSpace(a.AttrOr("href", ""))
			server := strings.TrimSpace(a.Text())
			if server == "" || !isValidDownloadURL(href) {
				return
			}
			links = append(links, DownloadLink{Server: providerName(server), URL: href})
		})

		if len(links) > 0 {
			groups = append(groups, QualityGroup{Quality: quality, Format: "MP4", Links: links})
		}
	}

	// 4) Pertahankan urutan kualitas: 1080p -> 720p -> 480p -> 360p.
	sort.SliceStable(groups, func(i, j int) bool {
		return qualityRank(groups[i].Quality) < qualityRank(groups[j].Quality)
	})

	return title, groups
}

// fallbackDownloadRows menemukan baris kualitas bila struktur .soraurlx tidak
// ada: hanya blok "bixbox" yang berjudul "Download" yang diproses, lalu setiap
// <strong> yang diikuti satu atau lebih <a href> dianggap satu baris kualitas.
func fallbackDownloadRows(doc *goquery.Document) []*goquery.Selection {
	var rows []*goquery.Selection
	doc.Find(".bixbox").Each(func(i int, box *goquery.Selection) {
		heading := strings.ToLower(box.Find("h1, h2, h3, h4, .releases, .releases h3").First().Text())
		if !strings.Contains(heading, "download") {
			return
		}
		box.Find("strong").Each(func(j int, s *goquery.Selection) {
			upper := strings.ToUpper(strings.TrimSpace(s.Text()))
			if strings.Contains(upper, "P") && reQuality.MatchString(upper) {
				rows = append(rows, s.Parent())
			}
		})
	})
	return rows
}

// normalizeDownloadQuality menormalkan label kualitas (mis. "1080p"). Mengembalikan
// string kosong bila label bukan kualitas yang dikenal.
func normalizeDownloadQuality(label string) string {
	lower := strings.ToLower(strings.TrimSpace(label))
	if m := reQuality.FindString(lower); m != "" && regexp.MustCompile(`(?i)^\d{3,4}p$`).MatchString(m) {
		return m
	}
	return ""
}

// isValidDownloadURL memastikan URL berasal dari <a href> yang benar-benar
// valid (http/https) sehingga tidak ada URL palsu/placeholder yang bocor.
func isValidDownloadURL(raw string) bool {
	if raw == "" || raw == "#" || strings.HasPrefix(raw, "javascript:") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

// knownProviders memetakan teks anchor ke nama server yang konsisten.
var knownProviders = map[string]string{
	"mediafire":   "MediaFire",
	"krakenfiles": "KrakenFiles",
	"pixeldrain":  "PixelDrain",
}

// providerName menormalkan nama provider dari teks anchor, dengan fallback ke
// teks asli bila bukan provider yang dikenal.
func providerName(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	for k, v := range knownProviders {
		if strings.Contains(lower, k) {
			return v
		}
	}
	return strings.TrimSpace(text)
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
