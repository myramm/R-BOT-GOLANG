package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"rbot/brain/command"
	"rbot/lib/httpx"
)

const laheluAPI = "https://lahelu.com/api/post/get-posts"

type laheluResponse struct {
	PostInfos []struct {
		Title   string `json:"title"`
		Content []struct {
			Value string `json:"value"`
		} `json:"content"`
	} `json:"postInfos"`
}

type memeMedia struct {
	URL   string
	Video bool
}

func init() {
	command.Register(&command.Command{
		Name:        "meme",
		Category:    "Entertainment",
		Alias:       []string{"randommeme", "lahelu"},
		Description: "Mengirim video/gambar meme random dari lahelu.com",
		Handler:     memeHandler,
	})
}

func memeHandler(ctx context.Context, c *command.Ctx) error {
	c.React(ctx, "⏳")
	page := int(time.Now().UnixNano()%5) + 1
	endpointURL := fmt.Sprintf("%s?feed=1&page=%d", laheluAPI, page)
	resp, err := httpx.Do(ctx, http.MethodGet, endpointURL, nil, 30*time.Second, map[string]string{
		"Accept": "application/json",
	})
	if err != nil {
		return memeError(ctx, c, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return memeError(ctx, c, fmt.Errorf("API Lahelu HTTP %d", resp.StatusCode))
	}
	var data laheluResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&data); err != nil {
		return memeError(ctx, c, fmt.Errorf("decode API Lahelu: %w", err))
	}

	var posts []struct {
		Title string
		Media memeMedia
	}
	for _, post := range data.PostInfos {
		for _, item := range post.Content {
			value := strings.TrimSpace(item.Value)
			if value == "" {
				continue
			}
			mediaURL, err := url.Parse(value)
			if err != nil || mediaURL.Scheme == "" || mediaURL.Host == "" {
				continue
			}
			ext := strings.ToLower(path.Ext(mediaURL.Path))
			video := ext == ".mp4" || ext == ".webm" || ext == ".mov"
			image := ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" || ext == ".gif"
			if video || image {
				posts = append(posts, struct {
					Title string
					Media memeMedia
				}{Title: post.Title, Media: memeMedia{URL: value, Video: video}})
				break
			}
		}
	}
	if len(posts) == 0 {
		c.React(ctx, "🤷")
		_, err := c.Reply(ctx, "❌ Tidak menemukan meme di halaman ini, coba lagi nanti.")
		return err
	}
	index := int(time.Now().UnixNano() % int64(len(posts)))
	if index < 0 {
		index = -index
	}
	selected := posts[index]
	title := selected.Title
	if title == "" {
		title = "Random Meme"
	}
	kind := command.MediaImage
	mimeType := "image/jpeg"
	if selected.Media.Video {
		kind = command.MediaVideo
		mimeType = "video/mp4"
	}
	caption := "*" + title + "*\n_Source: lahelu.com_"
	if err := c.SendMedia(ctx, selected.Media.URL, kind, caption, "", mimeType, 64*1024*1024); err != nil {
		return memeError(ctx, c, err)
	}
	c.React(ctx, "✅")
	return nil
}

func memeError(ctx context.Context, c *command.Ctx, err error) error {
	c.ReportError(ctx, err)
	c.React(ctx, "❌")
	_, replyErr := c.Reply(ctx, "❌ Gagal mengambil meme: "+err.Error())
	return replyErr
}
