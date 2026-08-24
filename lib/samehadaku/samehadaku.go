package samehadaku

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	BaseURL   = "https://v2.samehadaku.how"
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

type AnimeSearchResult struct {
	Title string `json:"title"`
	Link  string `json:"link"`
	Image string `json:"image"`
	Score string `json:"score"`
	Type  string `json:"type"`
}

type EpisodeInfo struct {
	Number string `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

type AnimeDetail struct {
	Title       string        `json:"title"`
	URL         string        `json:"url"`
	Image       string        `json:"image"`
	Synopsis    string        `json:"synopsis"`
	Rating      string        `json:"rating"`
	Status      string        `json:"status"`
	Genres      []string      `json:"genres"`
	Episodes    []EpisodeInfo `json:"episodes"`
	TotalEp     int           `json:"total_ep"`
	BatchURL    string        `json:"batch_url,omitempty"`
	HasBatch    bool          `json:"has_batch"`
}

type DownloadLink struct {
	Server    string `json:"server"`
	URL       string `json:"url"`
	DirectURL string `json:"direct_url,omitempty"`
}

type QualityGroup struct {
	Quality string         `json:"quality"`
	Format  string         `json:"format"`
	Links   []DownloadLink `json:"links"`
}

type EpisodeDownload struct {
	Title     string         `json:"title"`
	URL       string         `json:"url"`
	Qualities []QualityGroup `json:"qualities"`
	IsBatch   bool           `json:"is_batch"`
}

func fetchDoc(ctx context.Context, targetURL string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("samehadaku status HTTP %d", resp.StatusCode)
	}

	return goquery.NewDocumentFromReader(resp.Body)
}

// Search mencari anime berdasarkan kata kunci di Samehadaku.
func Search(ctx context.Context, query string) ([]AnimeSearchResult, error) {
	searchURL := fmt.Sprintf("%s/?s=%s", BaseURL, url.QueryEscape(query))
	doc, err := fetchDoc(ctx, searchURL)
	if err != nil {
		return nil, err
	}

	var results []AnimeSearchResult
	doc.Find("article, .animpost").Each(func(i int, s *goquery.Selection) {
		a := s.Find("h2 a, .title a, a").First()
		link := a.AttrOr("href", "")
		title := strings.TrimSpace(a.Text())
		img := s.Find("img").AttrOr("src", "")
		if img == "" {
			img = s.Find("img").AttrOr("data-src", "")
		}
		score := strings.TrimSpace(s.Find(".score, .rating").Text())
		typeStr := strings.TrimSpace(s.Find(".type, .typez").Text())

		if link != "" && title != "" {
			results = append(results, AnimeSearchResult{
				Title: cleanText(title),
				Link:  link,
				Image: img,
				Score: score,
				Type:  typeStr,
			})
		}
	})

	if len(results) == 0 {
		docText, _ := doc.Html()
		reArt := regexp.MustCompile(`href="(https://v2\.samehadaku\.how/anime/[^"/]+/?)"[^>]*>(.*?)</a>`)
		matches := reArt.FindAllStringSubmatch(docText, -1)
		seen := make(map[string]bool)
		for _, m := range matches {
			if !seen[m[1]] && !strings.Contains(m[2], "<img") {
				seen[m[1]] = true
				t := cleanText(m[2])
				if t != "" {
					results = append(results, AnimeSearchResult{
						Title: t,
						Link:  m[1],
					})
				}
			}
		}
	}

	return results, nil
}

// GetDetail mengambil info detail dan daftar episode anime dari URL anime.
func GetDetail(ctx context.Context, animeURL string) (*AnimeDetail, error) {
	doc, err := fetchDoc(ctx, animeURL)
	if err != nil {
		return nil, err
	}

	title := cleanText(doc.Find("h1.entry-title, .animetitle").Text())
	img := doc.Find(".thumb img, .poster img").AttrOr("src", "")
	if img == "" {
		img = doc.Find(".thumb img, .poster img").AttrOr("data-src", "")
	}
	synopsis := cleanText(doc.Find(".entry-content, .desc, .synopsis").Text())
	rating := cleanText(doc.Find(".rating strong, .score").Text())
	status := cleanText(doc.Find(".spe span:contains('Status')").Text())

	var genres []string
	doc.Find(".genre-info a, .genres a").Each(func(i int, s *goquery.Selection) {
		g := strings.TrimSpace(s.Text())
		if g != "" {
			genres = append(genres, g)
		}
	})

	var episodes []EpisodeInfo
	seenEps := make(map[string]bool)
	reEpNum := regexp.MustCompile(`(?i)episode\s*(\d+)`)

	doc.Find("a[href*='-episode-'], .lss ul li a, .eps-list li a, .episode-list li a").Each(func(i int, a *goquery.Selection) {
		epURL := a.AttrOr("href", "")
		epTitle := cleanText(a.Text())
		if epURL != "" && !seenEps[epURL] {
			seenEps[epURL] = true
			epNum := ""
			if m := reEpNum.FindStringSubmatch(epTitle); len(m) > 1 {
				epNum = m[1]
			} else if epTitle != "" && regexp.MustCompile(`^\d+$`).MatchString(epTitle) {
				epNum = epTitle
			} else {
				if m := reEpNum.FindStringSubmatch(epURL); len(m) > 1 {
					epNum = m[1]
				} else {
					epNum = fmt.Sprintf("%d", len(episodes)+1)
				}
			}
			if epTitle == "" || len(epTitle) < 3 {
				epTitle = fmt.Sprintf("Episode %s", epNum)
			}
			episodes = append(episodes, EpisodeInfo{
				Number: epNum,
				Title:  epTitle,
				URL:    epURL,
			})
		}
	})

	// Reverse episode list to be Episode 1..N order
	for i, j := 0, len(episodes)-1; i < j; i, j = i+1, j-1 {
		episodes[i], episodes[j] = episodes[j], episodes[i]
	}

	// Extract batch URL if available
	batchURL := doc.Find("a[href*='/batch/']").AttrOr("href", "")
	hasBatch := batchURL != ""

	return &AnimeDetail{
		Title:    title,
		URL:      animeURL,
		Image:    img,
		Synopsis: truncate(synopsis, 300),
		Rating:   rating,
		Status:   status,
		Genres:   genres,
		Episodes: episodes,
		TotalEp:  len(episodes),
		BatchURL: batchURL,
		HasBatch: hasBatch,
	}, nil
}

// GetEpisodeDownloads mengambil link download per kualitas/format dari URL episode / batch.
func GetEpisodeDownloads(ctx context.Context, epURL string) (*EpisodeDownload, error) {
	doc, err := fetchDoc(ctx, epURL)
	if err != nil {
		return nil, err
	}

	title := cleanText(doc.Find("h1.entry-title").Text())
	isBatch := strings.Contains(epURL, "/batch/") || strings.Contains(strings.ToLower(title), "batch")
	var qualities []QualityGroup

	doc.Find(".download-eps, .list-download, .download-batch").Each(func(i int, s *goquery.Selection) {
		sectionFormat := ""

		prev := s.Prev()
		prevText := strings.TrimSpace(prev.Text())
		innerTitle := strings.TrimSpace(s.Find(".title, h4, h3, p, b").First().Text())
		combinedText := strings.ToUpper(innerTitle + " " + prevText)

		if strings.Contains(combinedText, "X265") || strings.Contains(combinedText, "265") {
			sectionFormat = "x265"
		} else if strings.Contains(combinedText, "MP4") {
			sectionFormat = "MP4"
		} else if strings.Contains(combinedText, "MKV") {
			sectionFormat = "MKV"
		}

		if sectionFormat == "" {
			s.PrevAllFiltered("p, h4, h3, div.title, b, strong").Each(func(k int, h *goquery.Selection) {
				if sectionFormat == "" {
					txt := strings.ToUpper(h.Text())
					if strings.Contains(txt, "X265") || strings.Contains(txt, "265") {
						sectionFormat = "x265"
					} else if strings.Contains(txt, "MP4") {
						sectionFormat = "MP4"
					} else if strings.Contains(txt, "MKV") {
						sectionFormat = "MKV"
					}
				}
			})
		}

		if sectionFormat == "" {
			sectionFormat = "MKV"
		}

		s.Find("ul li, li").Each(func(j int, li *goquery.Selection) {
			resRaw := strings.TrimSpace(li.Find("strong").Text())
			if resRaw == "" {
				return
			}
			var links []DownloadLink

			li.Find("span a").Each(func(k int, a *goquery.Selection) {
				serverName := strings.TrimSpace(a.Text())
				linkURL := a.AttrOr("href", "")
				if linkURL != "" && serverName != "" {
					links = append(links, DownloadLink{
						Server: serverName,
						URL:    linkURL,
					})
				}
			})

			if len(links) > 0 {
				itemUpper := strings.ToUpper(resRaw)
				format := sectionFormat
				if strings.Contains(itemUpper, "X265") || strings.Contains(itemUpper, "265") {
					format = "x265"
				} else if strings.Contains(itemUpper, "MP4") || strings.Contains(itemUpper, "FULLHD") || strings.Contains(itemUpper, "MP4HD") {
					format = "MP4"
				}

				qualities = append(qualities, QualityGroup{
					Quality: resRaw,
					Format:  format,
					Links:   links,
				})
			}
		})
	})

	// Fallback if no container matched
	if len(qualities) == 0 {
		doc.Find("ul li, li").Each(func(i int, s *goquery.Selection) {
			resRaw := strings.TrimSpace(s.Find("strong").Text())
			var links []DownloadLink
			s.Find("span a").Each(func(j int, a *goquery.Selection) {
				serverName := strings.TrimSpace(a.Text())
				linkURL := a.AttrOr("href", "")
				if linkURL != "" && serverName != "" {
					links = append(links, DownloadLink{
						Server: serverName,
						URL:    linkURL,
					})
				}
			})

			if len(links) > 0 {
				format := "MKV"
				itemUpper := strings.ToUpper(resRaw)
				if strings.Contains(itemUpper, "X265") || strings.Contains(itemUpper, "265") {
					format = "x265"
				} else if strings.Contains(itemUpper, "MP4") || strings.Contains(itemUpper, "FULLHD") || strings.Contains(itemUpper, "MP4HD") {
					format = "MP4"
				}

				qualities = append(qualities, QualityGroup{
					Quality: resRaw,
					Format:  format,
					Links:   links,
				})
			}
		})
	}

	return &EpisodeDownload{
		Title:     title,
		URL:       epURL,
		Qualities: qualities,
		IsBatch:   isBatch,
	}, nil
}

var reAcefileID = regexp.MustCompile(`acefile\.co/(?:f|d|file|download|player)/(\d+)`)

// acefileID mengekstrak ID numerik file dari URL acefile.co.
func acefileID(pageURL string) string {
	if m := reAcefileID.FindStringSubmatch(pageURL); len(m) > 1 {
		return m[1]
	}
	return ""
}

// ResolveDirectLink mencoba mengekstrak link unduhan langsung dari provider (Pixeldrain, Mediafire, Krakenfiles, Filedon, Acefile, dll).
func ResolveDirectLink(ctx context.Context, server, pageURL string) string {
	srvLower := strings.ToLower(server)

	// 1. Pixeldrain: https://pixeldrain.com/u/ID -> https://pixeldrain.com/api/file/ID
	if strings.Contains(srvLower, "pixeldrain") || strings.Contains(pageURL, "pixeldrain.com/u/") {
		rePD := regexp.MustCompile(`pixeldrain\.com/u/([a-zA-Z0-9]+)`)
		if m := rePD.FindStringSubmatch(pageURL); len(m) > 1 {
			return fmt.Sprintf("https://pixeldrain.com/api/file/%s", m[1])
		}
	}

	// 2. Mediafire: Scrape #downloadButton
	if strings.Contains(srvLower, "mediafire") || strings.Contains(pageURL, "mediafire.com") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
		if err == nil {
			req.Header.Set("User-Agent", UserAgent)
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				doc, err := goquery.NewDocumentFromReader(resp.Body)
				if err == nil {
					dlBtn := doc.Find("#downloadButton, a.input.popsok, a[aria-label='Download file']").AttrOr("href", "")
					if dlBtn != "" {
						return dlBtn
					}
				}
			}
		}
	}

	// 3. Krakenfiles: Extract token from form and POST to /download/{id}
	if strings.Contains(srvLower, "kraken") || strings.Contains(pageURL, "krakenfiles.com") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
		if err == nil {
			req.Header.Set("User-Agent", UserAgent)
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				doc, err := goquery.NewDocumentFromReader(resp.Body)
				if err == nil {
					form := doc.Find("form#dl-form, form[action*='/download/']").First()
					actionPath := form.AttrOr("action", "")
					token := form.Find("input[name='token']").AttrOr("value", "")

					if actionPath != "" && token != "" {
						if !strings.HasPrefix(actionPath, "http") {
							actionPath = "https://krakenfiles.com" + actionPath
						}
						postData := url.Values{}
						postData.Set("token", token)

						postReq, pErr := http.NewRequestWithContext(ctx, http.MethodPost, actionPath, strings.NewReader(postData.Encode()))
						if pErr == nil {
							postReq.Header.Set("User-Agent", UserAgent)
							postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
							postReq.Header.Set("X-Requested-With", "XMLHttpRequest")
							postReq.Header.Set("Referer", pageURL)

							postResp, pDoErr := client.Do(postReq)
							if pDoErr == nil {
								defer postResp.Body.Close()
								var kRes struct {
									Status string `json:"status"`
									URL    string `json:"url"`
								}
								if json.NewDecoder(postResp.Body).Decode(&kRes) == nil && kRes.URL != "" {
									return kRes.URL
								}
							}
						}
					}
				}
			}
		}
	}

	// 4. Filedon: Extract Inertia version + XSRF token, then POST /download/{slug}
	if strings.Contains(srvLower, "filedon") || strings.Contains(pageURL, "filedon.co") || strings.Contains(pageURL, "filedon.com") {
		reFiledon := regexp.MustCompile(`filedon\.(?:co|com)/view/([a-zA-Z0-9]+)`)
		if m := reFiledon.FindStringSubmatch(pageURL); len(m) > 1 {
			slug := m[1]
			viewURL := fmt.Sprintf("https://filedon.co/view/%s", slug)

			jar, _ := cookiejar.New(nil)
			client := &http.Client{Timeout: 12 * time.Second, Jar: jar}

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, viewURL, nil)
			if err == nil {
				req.Header.Set("User-Agent", UserAgent)
				req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

				resp, err := client.Do(req)
				if err == nil {
					defer resp.Body.Close()
					doc, err := goquery.NewDocumentFromReader(resp.Body)
					if err == nil {
						dataPage := doc.Find("[data-page]").AttrOr("data-page", "")
						var version string
						if dataPage != "" {
							var inertiaData struct {
								Version string `json:"version"`
							}
							_ = json.Unmarshal([]byte(dataPage), &inertiaData)
							version = inertiaData.Version
						}

						var xsrfToken string
						u, _ := url.Parse("https://filedon.co")
						for _, c := range jar.Cookies(u) {
							if c.Name == "XSRF-TOKEN" {
								xsrfToken, _ = url.QueryUnescape(c.Value)
								break
							}
						}

						dlPostURL := fmt.Sprintf("https://filedon.co/download/%s", slug)
						postReq, pErr := http.NewRequestWithContext(ctx, http.MethodPost, dlPostURL, strings.NewReader("{}"))
						if pErr == nil {
							postReq.Header.Set("User-Agent", UserAgent)
							postReq.Header.Set("X-Inertia", "true")
							if version != "" {
								postReq.Header.Set("X-Inertia-Version", version)
							}
							if xsrfToken != "" {
								postReq.Header.Set("X-XSRF-TOKEN", xsrfToken)
							}
							postReq.Header.Set("X-Requested-With", "XMLHttpRequest")
							postReq.Header.Set("Referer", viewURL)
							postReq.Header.Set("Content-Type", "application/json")
							postReq.Header.Set("Accept", "text/html, application/xhtml+xml, application/json")

							postResp, pDoErr := client.Do(postReq)
							if pDoErr == nil {
								defer postResp.Body.Close()
								var filedonRes struct {
									Props struct {
										Flash struct {
											DownloadURL string `json:"download_url"`
										} `json:"flash"`
									} `json:"props"`
								}
								if json.NewDecoder(postResp.Body).Decode(&filedonRes) == nil {
									if filedonRes.Props.Flash.DownloadURL != "" {
										return filedonRes.Props.Flash.DownloadURL
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// 5. Acefile: file di acefile.co adalah mirror Google Drive.
	//    - resource_check: file dengan backup GDrive primer -> data = ID GDrive.
	//    - get_mirrors: file direct-hosted -> code = base64 dari ID GDrive mirror.
	//    File mati tetap bisa mengembalikan mirror usang; download-nya akan gagal
	//    dan bot jatuh kembali ke daftar link manual.
	if strings.Contains(srvLower, "acefile") || strings.Contains(pageURL, "acefile.co") {
		if id := acefileID(pageURL); id != "" {
			if gid := acefileResourceCheck(ctx, id); gid != "" {
				return acefileGDriveURL(gid)
			}
			if gid := acefileMirrorCode(ctx, id); gid != "" {
				return acefileGDriveURL(gid)
			}
		}
	}

	return pageURL
}

var reGDriveID = regexp.MustCompile(`^[A-Za-z0-9_-]{20,}$`)

// acefileGetJSON melakukan GET JSON ke endpoint internal acefile.co.
func acefileGetJSON(ctx context.Context, targetURL string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", "https://acefile.co/")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("acefile status HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// acefileResourceCheck memanggil service/resource_check; return ID GDrive jika file ber-backup GDrive primer.
func acefileResourceCheck(ctx context.Context, id string) string {
	var res struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	checkURL := fmt.Sprintf("https://acefile.co/service/resource_check/%s/", id)
	if err := acefileGetJSON(ctx, checkURL, &res); err != nil || res.Data == "" {
		return ""
	}
	if reGDriveID.MatchString(res.Data) {
		return res.Data
	}
	return ""
}

// acefileMirrorCode memanggil service/get_mirrors; decode base64 code menjadi ID GDrive mirror.
func acefileMirrorCode(ctx context.Context, id string) string {
	var res map[string][]struct {
		Code string `json:"code"`
	}
	mirrorURL := fmt.Sprintf("https://acefile.co/service/get_mirrors/%s", id)
	if err := acefileGetJSON(ctx, mirrorURL, &res); err != nil {
		return ""
	}
	for _, mirrors := range res {
		for _, m := range mirrors {
			if m.Code == "" {
				continue
			}
			dec, err := base64.StdEncoding.DecodeString(m.Code)
			if err != nil {
				if dec, err = base64.RawStdEncoding.DecodeString(m.Code); err != nil {
					continue
				}
			}
			gid := strings.TrimSpace(string(dec))
			if reGDriveID.MatchString(gid) {
				return gid
			}
		}
	}
	return ""
}

// acefileGDriveURL membangun URL unduhan langsung Google Drive dari ID file.
func acefileGDriveURL(gdriveID string) string {
	return fmt.Sprintf("https://drive.usercontent.google.com/download?id=%s&export=download&confirm=t", gdriveID)
}

func cleanText(s string) string {
	reSpace := regexp.MustCompile(`\s+`)
	return strings.TrimSpace(reSpace.ReplaceAllString(s, " "))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// ParseInt berusaha mengonversi string ke int, return defaultVal jika gagal.
func ParseInt(s string, defaultVal int) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return defaultVal
	}
	return v
}
