// Package watchhentai adalah scraper watchhentai.net: search, metadata episode,
// dan ekstraksi direct MP4 (hstorage.xyz). Port dari scrp/watchhentai.js.
package watchhentai

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	BaseURL   = "https://watchhentai.net"
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

// SearchResult adalah satu hasil pencarian series/episode.
type SearchResult struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Thumbnail string `json:"thumbnail,omitempty"`
	Source    string `json:"source,omitempty"` // penanda provider (kosong = WATCHHENTAI)
}

// ProviderName mengembalikan label provider untuk ditampilkan ke pengguna.
func (s SearchResult) ProviderName() string {
	if s.Source == "" {
		return "WatchHentai"
	}
	return s.Source
}

// Episode adalah metadata lengkap satu halaman episode beserta direct MP4.
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

// DownloadOption adalah satu pilihan kualitas pada halaman /download/.
type DownloadOption struct {
	Quality string `json:"quality"`
	URL     string `json:"url"`
}

// SeriesInfo adalah metadata halaman series: judul, sinopsis, dan genre.
type SeriesInfo struct {
	Title     string   `json:"title"`
	URL       string   `json:"url"`
	Thumbnail string   `json:"thumbnail,omitempty"`
	Synopsis  string   `json:"synopsis,omitempty"`
	Genres    []string `json:"genres,omitempty"`
}

var (
	reJWPlayerSrc = regexp.MustCompile(`(?i)(?:data-litespeed-src|src)=['"]([^'"]*jwplayer[^'"]*)['"]`)
	reSourceParam = regexp.MustCompile(`(?i)source=([^&]+)`)
	reDirectMP4   = regexp.MustCompile(`https?://[^"'\s<>]+\.mp4`)
	reEpNumber    = regexp.MustCompile(`-episode-(\d+)`)
	reTitleNum    = regexp.MustCompile(`(?i)episode\s*(\d+)`)
	reDlButton    = regexp.MustCompile(`(?is)<button[^>]*onclick="window\.location\.href\s*=\s*'([^']+)'">(.+?)</button>`)
	reHTMLTag     = regexp.MustCompile(`<[^>]*>`)
	reIDSuffix    = regexp.MustCompile(`(?i)-id-\d+$`)
)

func fetchHTML(ctx context.Context, targetURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", BaseURL)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("watchhentai status HTTP %d", resp.StatusCode)
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
	s = strings.ReplaceAll(s, "Watch Hentai", "")
	return strings.Trim(strings.TrimSpace(s), "-– ")
}

// Search mencari series / episode di watchhentai.net (?s=query).
func Search(ctx context.Context, query string) ([]SearchResult, error) {
	searchURL := fmt.Sprintf("%s/?s=%s", BaseURL, url.QueryEscape(query))
	body, err := fetchHTML(ctx, searchURL)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	var results []SearchResult
	seen := make(map[string]bool)
	doc.Find("article").Each(func(i int, art *goquery.Selection) {
		link := ""
		art.Find("a[href]").Each(func(j int, a *goquery.Selection) {
			if link != "" {
				return
			}
			href := a.AttrOr("href", "")
			if strings.HasPrefix(href, BaseURL+"/") && !strings.Contains(href, "/genre/") {
				link = href
			}
		})
		if link == "" || seen[link] {
			return
		}

		title := art.Find("a[title]").First().AttrOr("title", "")
		if title == "" {
			title = strings.TrimSpace(art.Find("h3").First().Text())
		}
		title = cleanText(title)
		if title == "" {
			title = slugToTitle(slugFromURL(link))
		}

		img := art.Find("img").AttrOr("data-src", "")
		if img == "" {
			img = art.Find("img").AttrOr("src", "")
		}

		seen[link] = true
		results = append(results, SearchResult{Title: title, URL: link, Thumbnail: img})
	})

	return results, nil
}

// GetEpisode mengambil metadata & direct MP4 dari URL episode atau slug.
// Contoh: "furachi-episode-1-id-01" -> https://watchhentai.net/videos/furachi-episode-1-id-01/
func GetEpisode(ctx context.Context, urlOrSlug string) (*Episode, error) {
	targetURL := strings.TrimSpace(urlOrSlug)
	if !strings.HasPrefix(targetURL, "http") {
		targetURL = fmt.Sprintf("%s/videos/%s/", BaseURL, strings.Trim(targetURL, "/"))
	}

	body, err := fetchHTML(ctx, targetURL)
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
	if title == "" {
		title = slugToTitle(slugFromURL(targetURL))
	}

	description := strings.TrimSpace(doc.Find(".wp-content").First().Text())

	videoURL := extractVideoURL(body)

	thumbnail := doc.Find(`meta[property="og:image"]`).First().AttrOr("content", "")
	if thumbnail == "" {
		thumbnail = doc.Find(`img[data-src*="uploads"]`).First().AttrOr("data-src", "")
	}

	return &Episode{
		Source:      "WATCHHENTAI_NET",
		Title:       title,
		Slug:        slugFromURL(targetURL),
		URL:         targetURL,
		Description: description,
		Thumbnail:   thumbnail,
		VideoURL:    videoURL,
	}, nil
}

// GetEpisodeList mengambil daftar episode (/videos/) dari sebuah halaman series.
func GetEpisodeList(ctx context.Context, seriesURL string) ([]EpisodeLink, error) {
	body, err := fetchHTML(ctx, seriesURL)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	var episodes []EpisodeLink
	seen := make(map[string]bool)
	doc.Find("a[href]").Each(func(i int, a *goquery.Selection) {
		href := a.AttrOr("href", "")
		if !strings.HasPrefix(href, BaseURL+"/videos/") || seen[href] {
			return
		}

		title := strings.TrimSpace(a.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(a.Text())
		}
		title = cleanText(title)

		number := ""
		slug := slugFromURL(href)
		if m := reEpNumber.FindStringSubmatch(slug); len(m) > 1 {
			number = m[1]
		} else if m := reTitleNum.FindStringSubmatch(title); len(m) > 1 {
			number = m[1]
		}
		if title == "" {
			title = "Episode " + number
		}

		seen[href] = true
		episodes = append(episodes, EpisodeLink{Number: number, Title: title, URL: href})
	})

	return episodes, nil
}

// GetDownloadOptions mengambil pilihan kualitas dari halaman /download/<slug>/.
// Input boleh URL videos/, URL download/, atau slug episode.
func GetDownloadOptions(ctx context.Context, urlOrSlug string) ([]DownloadOption, error) {
	pageURL := DownloadPageURL(urlOrSlug)
	body, err := fetchHTML(ctx, pageURL)
	if err != nil {
		return nil, err
	}
	opts := extractDownloadOptions(body)
	if len(opts) == 0 {
		return nil, fmt.Errorf("tidak ada pilihan kualitas tersedia")
	}
	return opts, nil
}

// DownloadPageURL menormalkan URL videos/, URL download/, atau slug menjadi
// URL halaman download. Contoh: https://watchhentai.net/videos/x/ -> .../download/x/
func DownloadPageURL(urlOrSlug string) string {
	s := strings.TrimSpace(urlOrSlug)
	if !strings.HasPrefix(s, "http") {
		return fmt.Sprintf("%s/download/%s/", BaseURL, strings.Trim(s, "/"))
	}
	if i := strings.Index(s, "/videos/"); i >= 0 {
		return fmt.Sprintf("%s/download/%s", strings.TrimRight(s[:i], "/"), s[i+len("/videos/"):])
	}
	return s
}

// extractDownloadOptions mem-parse tombol kualitas pada halaman download.
// Tombol WATCH ONLINE (link internal) diabaikan.
func extractDownloadOptions(pageHTML string) []DownloadOption {
	var opts []DownloadOption
	seen := make(map[string]bool)
	for _, m := range reDlButton.FindAllStringSubmatch(pageHTML, -1) {
		target := strings.TrimSpace(m[1])
		label := cleanText(reHTMLTag.ReplaceAllString(m[2], ""))
		if target == "" || label == "" || seen[target] {
			continue
		}
		if !strings.HasPrefix(target, "http") || strings.HasPrefix(target, BaseURL) {
			continue
		}
		if strings.Contains(strings.ToLower(label), "watch") {
			continue
		}
		seen[target] = true
		opts = append(opts, DownloadOption{Quality: label, URL: target})
	}
	return opts
}

// TitleFromEpisodeURL menghasilkan judul kasar dari URL/slug episode,
// tanpa sufks -id-01. Contoh: ".../kotowarenai-haha-episode-1-id-01/" ->
// "Kotowarenai Haha Episode 1"
func TitleFromEpisodeURL(urlOrSlug string) string {
	slug := strings.TrimSpace(urlOrSlug)
	if strings.HasPrefix(slug, "http") {
		slug = slugFromURL(slug)
	}
	slug = reIDSuffix.ReplaceAllString(strings.Trim(slug, "/"), "")
	return slugToTitle(slug)
}

// GetSeriesInfo mengambil metadata series (judul, sinopsis, genre, thumbnail).
func GetSeriesInfo(ctx context.Context, seriesURL string) (*SeriesInfo, error) {
	body, err := fetchHTML(ctx, seriesURL)
	if err != nil {
		return nil, err
	}
	info := parseSeriesInfo(body)
	if info.Title == "" {
		return nil, fmt.Errorf("tidak dapat mem-parse halaman series %s", seriesURL)
	}
	return info, nil
}

// parseSeriesInfo mem-parse HTML halaman series menjadi SeriesInfo.
func parseSeriesInfo(pageHTML string) *SeriesInfo {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(pageHTML))
	if err != nil {
		return &SeriesInfo{}
	}

	title := cleanText(doc.Find("h1").First().Text())
	thumbnail := doc.Find(`meta[property="og:image"]`).First().AttrOr("content", "")

	content := doc.Find(".wp-content").First()
	content.Find("h2").Remove() // buang heading "Synopsis..." agar sisa paragraf saja
	synopsis := strings.Join(strings.Fields(content.Text()), " ")

	var genres []string
	genreSeen := make(map[string]bool)
	doc.Find(`a[href*="/genre/"]`).Each(func(i int, a *goquery.Selection) {
		g := cleanText(a.Text())
		if g != "" && !genreSeen[g] && len(genres) < 8 {
			genreSeen[g] = true
			genres = append(genres, g)
		}
	})

	return &SeriesInfo{
		Title:     title,
		URL:       "",
		Thumbnail: thumbnail,
		Synopsis:  synopsis,
		Genres:    genres,
	}
}

// extractVideoURL mengambil direct MP4 dari player jwplayer (param source=)
// atau fallback ke regex .mp4 langsung di HTML.
func extractVideoURL(pageHTML string) string {
	if m := reJWPlayerSrc.FindStringSubmatch(pageHTML); len(m) > 1 {
		if sm := reSourceParam.FindStringSubmatch(m[1]); len(sm) > 1 {
			if dec, err := url.PathUnescape(sm[1]); err == nil && dec != "" {
				return dec
			}
			return sm[1]
		}
	}
	return reDirectMP4.FindString(pageHTML)
}

func slugFromURL(targetURL string) string {
	parts := strings.Split(strings.Trim(targetURL, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func slugToTitle(slug string) string {
	words := strings.Fields(strings.ReplaceAll(strings.TrimSpace(slug), "-", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
