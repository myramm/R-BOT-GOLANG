package komik

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Comic struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Slug     string    `json:"slug"`
	Source   string    `json:"source"` // "komiktap" | "komiku"
	Count    int       `json:"count"`
	Link     string    `json:"link"`
	CatID    int       `json:"cat_id,omitempty"`
	Chapters []Chapter `json:"chapters,omitempty"`
}

type Chapter struct {
	ID     string   `json:"id"`
	Num    string   `json:"num"`
	Title  string   `json:"title"`
	URL    string   `json:"url"`
	Slug   string   `json:"slug"`
	Images []string `json:"images,omitempty"`
	Source string   `json:"source"`
}

type categoryItem struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Count int    `json:"count"`
	Link  string `json:"link"`
}

type postItem struct {
	ID    int    `json:"id"`
	Slug  string `json:"slug"`
	Link  string `json:"link"`
	Title struct {
		Rendered string `json:"rendered"`
	} `json:"title"`
	Content struct {
		Rendered string `json:"rendered"`
	} `json:"content"`
	Categories []int `json:"categories"`
}

var (
	reImgTag        = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)
	reChapterNum    = regexp.MustCompile(`(?i)(?:chapter|ch\.?)\s*([0-9]+(?:\.[0-9]+)?)`)
	reChapterSuffix = regexp.MustCompile(`(?i)\s*[-:]?\s*(?:chapter|ch\.?)\s*[0-9]+(?:\.[0-9]+)?.*$`)
	httpClient      = &http.Client{Timeout: 15 * time.Second}
)

func cleanTitle(s string) string {
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "&#8211;", "-")
	s = strings.ReplaceAll(s, "&#8217;", "'")
	return strings.TrimSpace(s)
}

func ExtractSeriesTitle(rawTitle string) string {
	cleaned := cleanTitle(rawTitle)
	series := reChapterSuffix.ReplaceAllString(cleaned, "")
	series = strings.TrimSpace(series)
	if series == "" {
		return cleaned
	}
	return series
}

func extractImages(htmlContent string) []string {
	matches := reImgTag.FindAllStringSubmatch(htmlContent, -1)
	var images []string
	seen := make(map[string]bool)

	for _, m := range matches {
		if len(m) > 1 {
			u := strings.TrimSpace(m[1])
			if (strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")) && !seen[u] {
				seen[u] = true
				images = append(images, u)
			}
		}
	}
	return images
}

func filterComicImages(images []string) []string {
	var filtered []string
	for _, img := range images {
		if !strings.Contains(img, "ico.ico") &&
			!strings.Contains(img, "jp.png") &&
			!strings.Contains(img, "kr.png") &&
			!strings.Contains(img, "cn.png") &&
			!strings.Contains(img, "google.svg") &&
			!strings.Contains(img, "avatar") &&
			!strings.Contains(img, "wmkomiku") &&
			!strings.Contains(img, "asset/img") &&
			!strings.Contains(img, "komikuplus") &&
			!strings.Contains(img, "gravatar.com") {
			filtered = append(filtered, img)
		}
	}
	return filtered
}

func extractChapterNum(title string) string {
	m := reChapterNum.FindStringSubmatch(title)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func isMatchSeries(s1, s2 string) bool {
	s1 = strings.ToLower(strings.TrimSpace(s1))
	s2 = strings.ToLower(strings.TrimSpace(s2))
	return s1 == s2 || strings.HasPrefix(s1, s2) || strings.HasPrefix(s2, s1)
}

func fetchHTML(ctx context.Context, targetURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", defaultUA)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func fetchPosts(ctx context.Context, apiURL string) ([]postItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultUA)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var posts []postItem
	if err := json.Unmarshal(body, &posts); err != nil {
		return nil, err
	}
	return posts, nil
}

// SearchComics mencari komik dan mengelompokkannya per SERI
func SearchComics(ctx context.Context, query string) ([]Comic, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("kata kunci pencarian kosong")
	}

	var results []Comic
	seriesMap := make(map[string]*Comic)

	// 1. Cari Kategori di KomikTap (Judul Serial Komik)
	ktCatURL := fmt.Sprintf("https://komiktap.info/wp-json/wp/v2/categories?search=%s&per_page=20", url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ktCatURL, nil)
	if err == nil {
		req.Header.Set("User-Agent", defaultUA)
		resp, err := httpClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			var cats []categoryItem
			if json.Unmarshal(body, &cats) == nil {
				for _, c := range cats {
					t := cleanTitle(c.Name)
					key := "kt:" + strings.ToLower(c.Slug)
					if _, exists := seriesMap[key]; !exists {
						comic := Comic{
							ID:     strconv.Itoa(c.ID),
							Title:  t,
							Slug:   c.Slug,
							Source: "komiktap",
							Count:  c.Count,
							Link:   c.Link,
							CatID:  c.ID,
						}
						seriesMap[key] = &comic
						results = append(results, comic)
					}
				}
			}
		}
	}

	// 2. Cari Post di KomikTap & Kelompokkan per Seri
	ktPostURL := fmt.Sprintf("https://komiktap.info/wp-json/wp/v2/posts?search=%s&per_page=50", url.QueryEscape(query))
	if posts, err := fetchPosts(ctx, ktPostURL); err == nil {
		for _, p := range posts {
			postTitle := cleanTitle(p.Title.Rendered)
			seriesTitle := ExtractSeriesTitle(postTitle)
			seriesSlug := strings.ToLower(regexp.MustCompile(`[^\w]+`).ReplaceAllString(seriesTitle, "-"))
			seriesSlug = strings.Trim(seriesSlug, "-")

			key := "kt:" + seriesSlug
			if _, exists := seriesMap[key]; !exists {
				catID := 0
				if len(p.Categories) > 0 {
					catID = p.Categories[0]
				}
				comic := Comic{
					ID:     strconv.Itoa(p.ID),
					Title:  seriesTitle,
					Slug:   seriesSlug,
					Source: "komiktap",
					Link:   p.Link,
					CatID:  catID,
				}
				seriesMap[key] = &comic
				results = append(results, comic)
			}
		}
	}

	// 3. Cari Post di Komiku & Kelompokkan per Seri
	kmURL := fmt.Sprintf("https://komiku.org/wp-json/wp/v2/posts?search=%s&per_page=50", url.QueryEscape(query))
	if posts, err := fetchPosts(ctx, kmURL); err == nil {
		for _, p := range posts {
			postTitle := cleanTitle(p.Title.Rendered)
			seriesTitle := ExtractSeriesTitle(postTitle)
			seriesSlug := strings.ToLower(regexp.MustCompile(`[^\w]+`).ReplaceAllString(seriesTitle, "-"))
			seriesSlug = strings.Trim(seriesSlug, "-")

			key := "km:" + seriesSlug
			if _, exists := seriesMap[key]; !exists {
				comic := Comic{
					ID:     strconv.Itoa(p.ID),
					Title:  seriesTitle,
					Slug:   seriesSlug,
					Source: "komiku",
					Link:   p.Link,
				}
				seriesMap[key] = &comic
				results = append(results, comic)
			}
		}
	}

	return results, nil
}

// GetChapters mengambil SELURUH daftar chapter dari sebuah seri komik
func GetChapters(ctx context.Context, c Comic) ([]Chapter, error) {
	var chapters []Chapter
	seen := make(map[string]bool)

	if c.Source == "komiktap" && c.CatID > 0 {
		for page := 1; page <= 5; page++ {
			apiURL := fmt.Sprintf("https://komiktap.info/wp-json/wp/v2/posts?categories=%d&per_page=100&page=%d", c.CatID, page)
			posts, err := fetchPosts(ctx, apiURL)
			if err != nil || len(posts) == 0 {
				break
			}

			for _, p := range posts {
				t := cleanTitle(p.Title.Rendered)
				num := extractChapterNum(t)
				if num == "" {
					num = t
				}
				key := p.Slug
				if !seen[key] {
					seen[key] = true
					chapters = append(chapters, Chapter{
						ID:     strconv.Itoa(p.ID),
						Num:    num,
						Title:  t,
						URL:    p.Link,
						Slug:   p.Slug,
						Images: filterComicImages(extractImages(p.Content.Rendered)),
						Source: "komiktap",
					})
				}
			}

			if len(posts) < 100 {
				break
			}
		}
	} else {
		baseURL := "https://komiku.org/wp-json/wp/v2/posts"
		if c.Source == "komiktap" {
			baseURL = "https://komiktap.info/wp-json/wp/v2/posts"
		}

		searchTerm := c.Title
		for page := 1; page <= 5; page++ {
			apiURL := fmt.Sprintf("%s?search=%s&per_page=100&page=%d", baseURL, url.QueryEscape(searchTerm), page)
			posts, err := fetchPosts(ctx, apiURL)
			if err != nil || len(posts) == 0 {
				break
			}

			for _, p := range posts {
				t := cleanTitle(p.Title.Rendered)
				seriesTitle := ExtractSeriesTitle(t)
				if isMatchSeries(seriesTitle, c.Title) {
					num := extractChapterNum(t)
					if num == "" {
						num = "1"
					}
					key := p.Slug
					if !seen[key] {
						seen[key] = true
						chapters = append(chapters, Chapter{
							ID:     strconv.Itoa(p.ID),
							Num:    num,
							Title:  t,
							URL:    p.Link,
							Slug:   p.Slug,
							Images: filterComicImages(extractImages(p.Content.Rendered)),
							Source: c.Source,
						})
					}
				}
			}

			if len(posts) < 100 {
				break
			}
		}
	}

	sortChapters(chapters)
	return chapters, nil
}

func sortChapters(chapters []Chapter) {
	sort.Slice(chapters, func(i, j int) bool {
		n1, e1 := strconv.ParseFloat(chapters[i].Num, 64)
		n2, e2 := strconv.ParseFloat(chapters[j].Num, 64)
		if e1 == nil && e2 == nil {
			return n1 < n2
		}
		return chapters[i].Num < chapters[j].Num
	})
}

// GetChapterImages mendapatkan daftar URL gambar untuk chapter tertentu
func GetChapterImages(ctx context.Context, ch Chapter) ([]string, error) {
	if len(ch.Images) > 0 {
		return ch.Images, nil
	}

	// 1. Jika Komiku atau jika data image di API kosong, fetch langsung dari halaman HTML reader
	if ch.Source == "komiku" || ch.URL != "" {
		pageURL := ch.URL
		if pageURL == "" && ch.Slug != "" {
			if ch.Source == "komiku" {
				pageURL = fmt.Sprintf("https://komiku.org/%s/", ch.Slug)
			} else {
				pageURL = fmt.Sprintf("https://komiktap.info/%s/", ch.Slug)
			}
		}

		if pageURL != "" {
			htmlContent, err := fetchHTML(ctx, pageURL)
			if err == nil {
				imgs := filterComicImages(extractImages(htmlContent))
				if len(imgs) > 0 {
					return imgs, nil
				}
			}
		}
	}

	// 2. Fallback: WP REST API post detail
	var apiURL string
	if ch.Source == "komiku" {
		apiURL = fmt.Sprintf("https://komiku.org/wp-json/wp/v2/posts?slug=%s", url.QueryEscape(ch.Slug))
	} else {
		apiURL = fmt.Sprintf("https://komiktap.info/wp-json/wp/v2/posts?slug=%s", url.QueryEscape(ch.Slug))
	}

	posts, err := fetchPosts(ctx, apiURL)
	if err == nil && len(posts) > 0 {
		imgs := filterComicImages(extractImages(posts[0].Content.Rendered))
		if len(imgs) > 0 {
			return imgs, nil
		}
	}

	return nil, fmt.Errorf("tidak ada gambar terdeteksi pada chapter ini")
}
