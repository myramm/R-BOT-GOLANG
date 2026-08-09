package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"rbot/brain/command"
	"rbot/lib/httpx"
)

const pixivBase = "https://www.pixiv.net"

var pixivHeaders = map[string]string{
	"Referer":         pixivBase + "/",
	"Accept-Language": "en-US,en;q=0.9",
}

type pixivItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type pixivDetail struct {
	ID            string
	Title         string
	UserName      string
	UserAccount   string
	UserID        string
	Tags          []string
	PageCount     int
	BookmarkCount int
	RegularURL    string
	ProxyURL      string
}

func init() {
	command.Register(&command.Command{
		Name:        "pixiv",
		Category:    "Downloader",
		Alias:       []string{"px", "pixivsearch"},
		Description: "Cari & download artwork dari Pixiv. Contoh: .pixiv hatsune miku atau .pixiv 147721150",
		Handler:     pixivHandler,
	})
}

func pixivGet(ctx context.Context, endpoint string, out any) error {
	resp, err := httpx.Do(ctx, http.MethodGet, endpoint, nil, 30*time.Second, mergeHeaders(pixivHeaders, map[string]string{"User-Agent": httpx.UA}))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Pixiv HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

func mergeHeaders(a, b map[string]string) map[string]string {
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a { out[k] = v }
	for k, v := range b { out[k] = v }
	return out
}

func pixivSearch(ctx context.Context, query string) ([]pixivItem, error) {
	q := url.QueryEscape(query)
	endpoint := fmt.Sprintf("%s/ajax/search/artworks/%s?word=%s&order=date_d&mode=all&p=1&s_mode=s_tag&type=all&lang=en", pixivBase, q, q)
	var raw struct {
		Error bool `json:"error"`
		Message string `json:"message"`
		Body struct { IllustManga struct { Data []pixivItem `json:"data"` } `json:"illustManga"` } `json:"body"`
	}
	if err := pixivGet(ctx, endpoint, &raw); err != nil { return nil, err }
	if raw.Error { return nil, fmt.Errorf("%s", raw.Message) }
	return raw.Body.IllustManga.Data, nil
}

func pixivDetailGet(ctx context.Context, id string) (pixivDetail, error) {
	var raw struct {
		Error bool `json:"error"`
		Message string `json:"message"`
		Body struct {
			ID string `json:"id"`
			Title string `json:"title"`
			UserName string `json:"userName"`
			UserAccount string `json:"userAccount"`
			UserID string `json:"userId"`
			PageCount int `json:"pageCount"`
			BookmarkCount int `json:"bookmarkCount"`
			Tags struct { Tags []struct { Tag string `json:"tag"` } `json:"tags"` } `json:"tags"`
			URLs struct { Regular string `json:"regular"` } `json:"urls"`
		} `json:"body"`
	}
	if err := pixivGet(ctx, pixivBase+"/ajax/illust/"+url.PathEscape(id), &raw); err != nil { return pixivDetail{}, err }
	if raw.Error || raw.Body.ID == "" { return pixivDetail{}, fmt.Errorf("artwork tidak ditemukan") }
	tags := make([]string, 0, len(raw.Body.Tags.Tags))
	for _, tag := range raw.Body.Tags.Tags { tags = append(tags, tag.Tag) }
	return pixivDetail{ID: raw.Body.ID, Title: raw.Body.Title, UserName: raw.Body.UserName, UserAccount: raw.Body.UserAccount, UserID: raw.Body.UserID, Tags: tags, PageCount: raw.Body.PageCount, BookmarkCount: raw.Body.BookmarkCount, RegularURL: raw.Body.URLs.Regular, ProxyURL: "https://pixiv.re/"+raw.Body.ID+".png"}, nil
}

func pixivHandler(ctx context.Context, c *command.Ctx) error {
	query := strings.TrimSpace(c.ArgStr())
	if query == "" {
		_, err := c.Reply(ctx, "Mau cari artwork apa di Pixiv? Contoh:\n.pixiv hatsune miku\n.pixiv 147721150")
		return err
	}
	c.React(ctx, "🎨")
	id := query
	if _, err := strconv.ParseInt(query, 10, 64); err != nil {
		results, err := pixivSearch(ctx, query)
		if err != nil { return pixivError(ctx, c, "Gagal mencari di Pixiv: "+err.Error()) }
		if len(results) == 0 { return pixivError(ctx, c, "Tidak menemukan artwork untuk kata kunci itu.") }
		limit := len(results); if limit > 15 { limit = 15 }
		rand.Seed(time.Now().UnixNano()); id = results[rand.Intn(limit)].ID
	}
	detail, err := pixivDetailGet(ctx, id)
	if err != nil { return pixivError(ctx, c, "Gagal mengambil detail artwork: "+err.Error()) }
	imageURL := detail.RegularURL
	if imageURL == "" {
		imageURL = detail.ProxyURL
	}
	tags := make([]string, 0, minInt(len(detail.Tags), 7))
	for _, tag := range detail.Tags[:minInt(len(detail.Tags), 7)] { tags = append(tags, "#"+strings.ReplaceAll(tag, " ", "_")) }
	caption := fmt.Sprintf("🎨 *%s*\n👤 *Artist:* %s (@%s)\n🆔 *Illust ID:* %s\n", detail.Title, detail.UserName, firstNonEmpty(detail.UserAccount, detail.UserID), detail.ID)
	if detail.PageCount > 1 { caption += fmt.Sprintf("📄 *Pages:* %d\n", detail.PageCount) }
	if detail.BookmarkCount > 0 { caption += fmt.Sprintf("❤️ *Bookmarks:* %d\n", detail.BookmarkCount) }
	if len(tags) > 0 { caption += "🏷️ *Tags:* "+strings.Join(tags, " ")+"\n" }
	caption += "\n🔗 https://www.pixiv.net/en/artworks/"+detail.ID
	imageResp, err := httpx.Do(ctx, http.MethodGet, imageURL, nil, 2*time.Minute, mergeHeaders(pixivHeaders, map[string]string{"User-Agent": httpx.UA}))
	if err != nil {
		return pixivError(ctx, c, "Gagal mengunduh artwork: "+err.Error())
	}
	defer imageResp.Body.Close()
	if imageResp.StatusCode < 200 || imageResp.StatusCode >= 300 {
		return pixivError(ctx, c, fmt.Sprintf("Gagal mengunduh artwork: HTTP %d", imageResp.StatusCode))
	}
	imageData, err := io.ReadAll(io.LimitReader(imageResp.Body, 32<<20))
	if err != nil {
		return pixivError(ctx, c, "Gagal membaca artwork: "+err.Error())
	}
	if err := c.SendMediaBytes(ctx, imageData, command.MediaImage, caption, "", "image/jpeg"); err != nil {
		return pixivError(ctx, c, "Gagal mengirim artwork: "+err.Error())
	}
	c.React(ctx, "✅")
	return nil
}

func pixivError(ctx context.Context, c *command.Ctx, message string) error { c.React(ctx, "❌"); _, err := c.Reply(ctx, "❌ "+message); return err }
func firstNonEmpty(a, b string) string { if a != "" { return a }; return b }
