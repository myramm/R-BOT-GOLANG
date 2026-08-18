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

func extractChapterNum(title string) string {
	m := reChapterNum.FindStringSubmatch(title)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

// SearchComics mencari komik dan mengelompokkannya per SERI (bukan per chapter)
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
	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, ktPostURL, nil)
	if err == nil {
		req2.Header.Set("User-Agent", defaultUA)
		resp, err := httpClient.Do(req2)
		if err == nil && resp.StatusCode == http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			var posts []postItem
			if json.Unmarshal(body, &posts) == nil {
				for _, p := range posts {
					postTitle := cleanTitle(p.Title.Rendered)
					seriesTitle := ExtractSeriesTitle(postTitle)
					seriesSlug := strings.ToLower(regexp.MustCompile(`[^\w]+`).ReplaceAllString(seriesTitle, "-"))
					seriesSlug = strings.Trim(seriesSlug, "-")

					key := "kt:" + seriesSlug
					chNum := extractChapterNum(postTitle)
					if chNum == "" {
						chNum = "1"
					}

					ch := Chapter{
						ID:     strconv.Itoa(p.ID),
						Num:    chNum,
						Title:  postTitle,
						URL:    p.Link,
						Slug:   p.Slug,
						Images: extractImages(p.Content.Rendered),
						Source: "komiktap",
					}

					if existing, exists := seriesMap[key]; exists {
						existing.Chapters = append(existing.Chapters, ch)
						existing.Count = len(existing.Chapters)
					} else {
						catID := 0
						if len(p.Categories) > 0 {
							catID = p.Categories[0]
						}
						comic := Comic{
							ID:       strconv.Itoa(p.ID),
							Title:    seriesTitle,
							Slug:     seriesSlug,
							Source:   "komiktap",
							Link:     p.Link,
							CatID:    catID,
							Chapters: []Chapter{ch},
							Count:    1,
						}
						seriesMap[key] = &comic
						results = append(results, comic)
					}
				}
			}
		}
	}

	// 3. Cari Post di Komiku & Kelompokkan per Seri
	kmURL := fmt.Sprintf("https://komiku.org/wp-json/wp/v2/posts?search=%s&per_page=50", url.QueryEscape(query))
	req3, err := http.NewRequestWithContext(ctx, http.MethodGet, kmURL, nil)
	if err == nil {
		req3.Header.Set("User-Agent", defaultUA)
		resp, err := httpClient.Do(req3)
		if err == nil && resp.StatusCode == http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			var posts []postItem
			if json.Unmarshal(body, &posts) == nil {
				for _, p := range posts {
					postTitle := cleanTitle(p.Title.Rendered)
					seriesTitle := ExtractSeriesTitle(postTitle)
					seriesSlug := strings.ToLower(regexp.MustCompile(`[^\w]+`).ReplaceAllString(seriesTitle, "-"))
					seriesSlug = strings.Trim(seriesSlug, "-")

					key := "km:" + seriesSlug
					chNum := extractChapterNum(postTitle)
					if chNum == "" {
						chNum = "1"
					}

					ch := Chapter{
						ID:     strconv.Itoa(p.ID),
						Num:    chNum,
						Title:  postTitle,
						URL:    p.Link,
						Slug:   p.Slug,
						Images: extractImages(p.Content.Rendered),
						Source: "komiku",
					}

					if existing, exists := seriesMap[key]; exists {
						existing.Chapters = append(existing.Chapters, ch)
						existing.Count = len(existing.Chapters)
					} else {
						comic := Comic{
							ID:       strconv.Itoa(p.ID),
							Title:    seriesTitle,
							Slug:     seriesSlug,
							Source:   "komiku",
							Link:     p.Link,
							Chapters: []Chapter{ch},
							Count:    1,
						}
						seriesMap[key] = &comic
						results = append(results, comic)
					}
				}
			}
		}
	}

	// Update pointer slice results dengan isi seriesMap
	for i := range results {
		key := "kt:" + results[i].Slug
		if results[i].Source == "komiku" {
			key = "km:" + results[i].Slug
		}
		if s, ok := seriesMap[key]; ok {
			results[i] = *s
		}
	}

	return results, nil
}

// GetChapters mengambil daftar chapter dari sebuah seri komik
func GetChapters(ctx context.Context, c Comic) ([]Chapter, error) {
	// Jika sudah ada chapter yang terkumpul saat pencarian
	if len(c.Chapters) > 0 {
		sortChapters(c.Chapters)
		return c.Chapters, nil
	}

	var chapters []Chapter

	if c.Source == "komiktap" && c.CatID > 0 {
		apiURL := fmt.Sprintf("https://komiktap.info/wp-json/wp/v2/posts?categories=%d&per_page=100", c.CatID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", defaultUA)

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("gagal mengambil chapter KomikTap: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP status %d dari KomikTap", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		var posts []postItem
		if err := json.Unmarshal(body, &posts); err != nil {
			return nil, fmt.Errorf("gagal parse JSON chapter: %w", err)
		}

		for _, p := range posts {
			t := cleanTitle(p.Title.Rendered)
			num := extractChapterNum(t)
			if num == "" {
				num = t
			}
			imgs := extractImages(p.Content.Rendered)

			chapters = append(chapters, Chapter{
				ID:     strconv.Itoa(p.ID),
				Num:    num,
				Title:  t,
				URL:    p.Link,
				Slug:   p.Slug,
				Images: imgs,
				Source: "komiktap",
			})
		}
	} else {
		// Cari posts berdasarkan judul seri komik
		searchURL := ""
		if c.Source == "komiktap" {
			searchURL = fmt.Sprintf("https://komiktap.info/wp-json/wp/v2/posts?search=%s&per_page=100", url.QueryEscape(c.Title))
		} else {
			searchURL = fmt.Sprintf("https://komiku.org/wp-json/wp/v2/posts?search=%s&per_page=100", url.QueryEscape(c.Title))
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
		if err == nil {
			req.Header.Set("User-Agent", defaultUA)
			resp, err := httpClient.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()

				var posts []postItem
				if json.Unmarshal(body, &posts) == nil {
					for _, p := range posts {
						t := cleanTitle(p.Title.Rendered)
						seriesTitle := ExtractSeriesTitle(t)
						if strings.EqualFold(seriesTitle, c.Title) || strings.Contains(strings.ToLower(seriesTitle), strings.ToLower(c.Title)) {
							num := extractChapterNum(t)
							if num == "" {
								num = "1"
							}
							imgs := extractImages(p.Content.Rendered)

							chapters = append(chapters, Chapter{
								ID:     strconv.Itoa(p.ID),
								Num:    num,
								Title:  t,
								URL:    p.Link,
								Slug:   p.Slug,
								Images: imgs,
								Source: c.Source,
							})
						}
					}
				}
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

	var apiURL string
	if ch.Source == "komiku" {
		apiURL = fmt.Sprintf("https://komiku.org/wp-json/wp/v2/posts?slug=%s", url.QueryEscape(ch.Slug))
	} else {
		apiURL = fmt.Sprintf("https://komiktap.info/wp-json/wp/v2/posts?slug=%s", url.QueryEscape(ch.Slug))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultUA)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil gambar chapter: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var posts []postItem
	if err := json.Unmarshal(body, &posts); err != nil || len(posts) == 0 {
		return nil, fmt.Errorf("post chapter tidak ditemukan")
	}

	imgs := extractImages(posts[0].Content.Rendered)
	if len(imgs) == 0 {
		return nil, fmt.Errorf("tidak ada gambar terdeteksi pada chapter ini")
	}

	return imgs, nil
}
