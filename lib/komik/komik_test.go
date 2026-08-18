package komik

import (
	"context"
	"testing"
	"time"
)

func TestSearchComicsGrouped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	results, err := SearchComics(ctx, "solo leveling")
	if err != nil {
		t.Fatalf("SearchComics error: %v", err)
	}

	t.Logf("SearchComics for 'solo leveling' found %d series:", len(results))
	for i, r := range results {
		t.Logf("  [%d] %s (%s)", i+1, r.Title, r.Source)
	}

	// Test GetChapters for "Solo Leveling"
	var targetComic *Comic
	for _, r := range results {
		if r.Title == "Solo Leveling" {
			targetComic = &r
			break
		}
	}

	if targetComic == nil && len(results) > 0 {
		targetComic = &results[0]
	}

	if targetComic != nil {
		chapters, err := GetChapters(ctx, *targetComic)
		if err != nil {
			t.Fatalf("GetChapters error for %s: %v", targetComic.Title, err)
		}
		t.Logf("GetChapters for '%s' returned %d chapters!", targetComic.Title, len(chapters))
		if len(chapters) > 0 {
			t.Logf("  First Chapter: Num=%s, Title=%s", chapters[0].Num, chapters[0].Title)
			t.Logf("  Last Chapter: Num=%s, Title=%s", chapters[len(chapters)-1].Num, chapters[len(chapters)-1].Title)
		}
	}
}
