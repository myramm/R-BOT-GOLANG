package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/lib/httpx"
)

const pinterestBase = "https://www.pinterest.com"

var pinCountRE = regexp.MustCompile(`(^|[[:space:]])--([0-9]{1,2})([[:space:]]|$)`)
var pinIDRE = regexp.MustCompile(`/pin/(\d+)`)

type pinterestResource struct {
	ResourceResponse struct {
		Data json.RawMessage `json:"data"`
	} `json:"resource_response"`
}

type pinImageVariant struct {
	URL string `json:"url"`
}

type pinRaw struct {
	ID          string                     `json:"id"`
	Title       any                        `json:"title"` // Pinterest kadang kirim object {content, urls}
	GridTitle   string                     `json:"grid_title"`
	Description string                     `json:"description"`
	Images      map[string]pinImageVariant `json:"images"`
	Pinner      struct {
		Username string `json:"username"`
	} `json:"pinner"`
	RepinCount int `json:"repin_count"`
}

type pinResult struct {
	ID          string
	Title       string
	Image       string
	PinURL      string
	Uploader    string
	Saves       int
	Description string
}

func init() {
	command.Register(&command.Command{
		Name:        "pin",
		Category:    "Downloader",
		Alias:       []string{"pinterest", "pint"},
		Description: "Cari & kirim foto dari Pinterest. Contoh: .pin kucing atau .pin --5 pemandangan",
		Handler:     pinHandler,
	})
}

func pinterestHeaders(sourceURL, handler string) map[string]string {
	return map[string]string{
		"Accept":                  "application/json, text/javascript, */*, q=0.01",
		"Referer":                 pinterestBase + "/",
		"User-Agent":              httpx.UA,
		"x-app-version":           "a9522f",
		"x-pinterest-appstate":    "active",
		"x-pinterest-pws-handler": handler,
		"x-pinterest-source-url":  sourceURL,
		"x-requested-with":        "XMLHttpRequest",
	}
}

func pinterestResourceData(ctx context.Context, endpoint, source, handler string, options map[string]any) (json.RawMessage, error) {
	payload, err := json.Marshal(map[string]any{"options": options, "context": map[string]any{}})
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("source_url", source)
	q.Set("data", string(payload))
	resp, err := httpx.Do(ctx, http.MethodGet, pinterestBase+"/resource/"+endpoint+"/get/?"+q.Encode(), nil, 30*time.Second, pinterestHeaders(source, handler))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Pinterest HTTP %d", resp.StatusCode)
	}
	var out pinterestResource
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out); err != nil {
		return nil, err
	}
	return out.ResourceResponse.Data, nil
}

func pinImage(images map[string]pinImageVariant) string {
	for _, key := range []string{"736x", "orig", "474x", "236x"} {
		if image, ok := images[key]; ok && image.URL != "" {
			return image.URL
		}
	}
	return ""
}

func normalizePin(raw pinRaw) *pinResult {
	image := pinImage(raw.Images)
	if image == "" || raw.ID == "" {
		return nil
	}
	title := extractPinTitle(raw.Title)
	if title == "" {
		title = raw.GridTitle
	}
	return &pinResult{ID: raw.ID, Title: title, Description: raw.Description, Image: image, PinURL: pinterestBase + "/pin/" + raw.ID + "/", Uploader: raw.Pinner.Username, Saves: raw.RepinCount}
}

// extractPinTitle menormalkan field title Pinterest yang bisa berupa
// string polos atau object {content, urls}. Tipe diturunkan dari any.
func extractPinTitle(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		if s, ok := t["content"].(string); ok {
			return s
		}
		if s, ok := t["title"].(string); ok {
			return s
		}
	case []any:
		for _, item := range t {
			if s := extractPinTitle(item); s != "" {
				return s
			}
		}
	}
	return ""
}

func searchPinterest(ctx context.Context, query string, limit int) ([]pinResult, error) {
	data, err := pinterestResourceData(ctx, "BaseSearchResource", "/search/pins/?q="+url.QueryEscape(query), "www/search/[scope].js", map[string]any{
		"isPrefetch": false, "query": query, "scope": "pins", "bookmarks": []string{""}, "no_fetch_context_on_resource": false, "page_size": minInt(maxInt(limit*5, 25), 50),
	})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Results []pinRaw `json:"results"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	out := make([]pinResult, 0, len(parsed.Results))
	for _, raw := range parsed.Results {
		if pin := normalizePin(raw); pin != nil {
			out = append(out, *pin)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func resolvePinterestURL(ctx context.Context, raw string) (string, error) {
	resp, err := httpx.Do(ctx, http.MethodGet, raw, nil, 30*time.Second, map[string]string{"User-Agent": httpx.UA})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL.String(), nil
	}
	return raw, nil
}

func pinterestPinID(ctx context.Context, raw string) (string, error) {
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "https://pin.it/") || strings.HasPrefix(lower, "http://pin.it/") {
		var err error
		raw, err = resolvePinterestURL(ctx, raw)
		if err != nil {
			return "", err
		}
	}
	match := pinIDRE.FindStringSubmatch(raw)
	if len(match) != 2 {
		return "", nil
	}
	return match[1], nil
}

func getPinterestPin(ctx context.Context, id string) (*pinResult, error) {
	data, err := pinterestResourceData(ctx, "PinResource", "/pin/"+id+"/", "www/pin/[id].js", map[string]any{"field_set_key": "detailed", "id": id})
	if err != nil {
		return nil, err
	}
	var raw pinRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return normalizePin(raw), nil
}

func pinHandler(ctx context.Context, c *command.Ctx) error {
	raw := strings.TrimSpace(c.ArgStr())
	count := 1
	if match := pinCountRE.FindStringSubmatch(raw); len(match) == 4 {
		count, _ = strconv.Atoi(match[2])
		count = minInt(maxInt(count, 1), 20)
		raw = strings.TrimSpace(pinCountRE.ReplaceAllString(raw, "$1$3"))
	}
	if raw == "" {
		_, err := c.Reply(ctx, fmt.Sprintf("Mau cari foto apa di Pinterest?\n\nContoh:\n%s pin kucing anggora\n%s pin --5 pemandangan gunung", config.MainPrefix(), config.MainPrefix()))
		return err
	}
	c.React(ctx, "⏳")
	var pins []pinResult
	if strings.Contains(strings.ToLower(raw), "pinterest.") || strings.Contains(strings.ToLower(raw), "pin.it/") {
		id, err := pinterestPinID(ctx, raw)
		if err != nil || id == "" {
			return pinError(ctx, c, "Link Pinterest tidak valid.")
		}
		pin, err := getPinterestPin(ctx, id)
		if err != nil || pin == nil {
			return pinError(ctx, c, "Pin tidak ditemukan atau sudah dihapus.")
		}
		pins = []pinResult{*pin}
	} else {
		var err error
		pins, err = searchPinterest(ctx, raw, count)
		if err != nil {
			return pinError(ctx, c, "Gagal mencari di Pinterest: "+err.Error())
		}
	}
	if len(pins) == 0 {
		return pinError(ctx, c, "Tidak menemukan foto yang cocok.")
	}
	sent := 0
	for i, pin := range pins {
		caption := fmt.Sprintf("📌 *%s*", raw)
		if pin.Title != "" {
			caption = "📌 *" + pin.Title + "*"
		}
		if len(pins) > 1 {
			caption += fmt.Sprintf(" (%d/%d)", i+1, len(pins))
		}
		if pin.Uploader != "" {
			caption += "\n_by " + pin.Uploader + "_"
		}
		caption += "\n" + pin.PinURL
		if err := c.SendMedia(ctx, pin.Image, command.MediaImage, caption, "", "image/jpeg", 32*1024*1024); err != nil {
			continue
		}
		sent++
	}
	if sent == 0 {
		return pinError(ctx, c, "Foto ditemukan tetapi gagal dikirim ke WhatsApp.")
	}
	c.React(ctx, "✅")
	return nil
}

func pinError(ctx context.Context, c *command.Ctx, message string) error {
	c.ReportErrorMessage(ctx, message)
	c.React(ctx, "❌")
	_, err := c.Reply(ctx, "❌ "+message)
	return err
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
