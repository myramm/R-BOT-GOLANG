// Package ytsearch mencari video YouTube lewat instance Piped (port dari
// lib/ytsearch.js). Berbeda dari Node: fallback ke paket npm `yt-search` tidak
// diikutkan (tak ada padanan Go) — di sini Piped-only. Bila semua instance Piped
// down, Search mengembalikan (nil, nil) dan pemanggil menampilkan pesan "tidak ada hasil".
package ytsearch

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"rbot/brain/config"
	"rbot/lib/httpx"
)

const searchTimeout = 8 * time.Second

// Result adalah satu hasil pencarian (subset field yang dipakai command play).
type Result struct {
	Title     string
	URL       string
	Seconds   int
	Timestamp string // "m:ss"
	Author    string
	Source    string
}

// pipedItem memetakan bentuk item pencarian Piped (/search?filter=videos).
type pipedItem struct {
	URL          string `json:"url"`
	Title        string `json:"title"`
	Duration     int    `json:"duration"`
	UploaderName string `json:"uploaderName"`
}

type pipedResp struct {
	Items []pipedItem `json:"items"`
}

// normalizeSeconds membentuk timestamp "m:ss" dari detik (port normalizeSeconds).
func normalizeSeconds(sec int) (int, string) {
	if sec < 0 {
		sec = 0
	}
	return sec, fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

// Search mengembalikan hasil pertama yang valid dari daftar instance Piped di
// config, mencoba berurutan sampai ada yang berhasil. (nil, nil) bila tak ada.
func Search(ctx context.Context, query string) (*Result, error) {
	q := url.QueryEscape(query)
	for _, base := range config.C.PipedInstances {
		base = strings.TrimRight(strings.TrimSpace(base), "/")
		if base == "" {
			continue
		}
		var data pipedResp
		if err := httpx.GetJSON(ctx, base+"/search?q="+q+"&filter=videos", searchTimeout, nil, &data); err != nil {
			continue
		}
		for _, it := range data.Items {
			if !strings.Contains(it.URL, "/watch") {
				continue
			}
			sec, ts := normalizeSeconds(it.Duration)
			title := it.Title
			if title == "" {
				title = query
			}
			return &Result{
				Title:     title,
				URL:       "https://www.youtube.com" + it.URL,
				Seconds:   sec,
				Timestamp: ts,
				Author:    it.UploaderName,
				Source:    "piped",
			}, nil
		}
	}
	return nil, nil
}
