package samehadaku_test

import (
	"context"
	"testing"
	"time"

	"rbot/lib/samehadaku"
)

func TestSearch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	results, err := samehadaku.Search(ctx, "bleach")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatalf("Expected search results for 'bleach', got 0")
	}

	t.Logf("Search returned %d results", len(results))
	for i, r := range results {
		if i >= 3 {
			break
		}
		t.Logf("[%d] %s -> %s", i+1, r.Title, r.Link)
	}
}

func TestGetDetailAndDownloads(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 1. Get Detail
	detailURL := "https://v2.samehadaku.how/anime/bleach-sennen-kessen-hen-kashin-tan-cour-4/"
	detail, err := samehadaku.GetDetail(ctx, detailURL)
	if err != nil {
		t.Fatalf("GetDetail failed: %v", err)
	}

	t.Logf("Anime Title: %s (Total Ep: %d)", detail.Title, detail.TotalEp)
	if detail.Title == "" {
		t.Errorf("Expected title, got empty")
	}
	if len(detail.Episodes) == 0 {
		t.Fatalf("Expected episodes in detail, got 0")
	}

	// 2. Get Downloads for episode 1 or latest
	sampleEpURL := detail.Episodes[len(detail.Episodes)-1].URL
	downloads, err := samehadaku.GetEpisodeDownloads(ctx, sampleEpURL)
	if err != nil {
		t.Fatalf("GetEpisodeDownloads failed: %v", err)
	}

	t.Logf("Episode Title: %s (Qualities: %d)", downloads.Title, len(downloads.Qualities))
	if len(downloads.Qualities) == 0 {
		t.Errorf("Expected quality groups in downloads, got 0")
	}
}
