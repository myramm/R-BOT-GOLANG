package komik

import (
	"context"
	"testing"
	"time"
)

func TestSearchComics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	results, err := SearchComics(ctx, "taming")
	if err != nil {
		t.Fatalf("SearchComics error: %v", err)
	}

	if len(results) == 0 {
		t.Log("Warning: SearchComics returned 0 results")
		return
	}

	t.Logf("SearchComics found %d results:", len(results))
	for i, r := range results {
		if i >= 3 {
			break
		}
		t.Logf("  [%d] %s (%s) - %s (CatID: %d)", i+1, r.Title, r.Source, r.Slug, r.CatID)
	}

	// Test GetChapters for first result
	first := results[0]
	chapters, err := GetChapters(ctx, first)
	if err != nil {
		t.Fatalf("GetChapters error for %s: %v", first.Title, err)
	}

	t.Logf("GetChapters found %d chapters for %s", len(chapters), first.Title)
	if len(chapters) > 0 {
		ch := chapters[0]
		t.Logf("  First Chapter: Num=%s, Title=%s, ImagesCount=%d", ch.Num, ch.Title, len(ch.Images))

		// Test GetChapterImages
		imgs, err := GetChapterImages(ctx, ch)
		if err != nil {
			t.Fatalf("GetChapterImages error: %v", err)
		}
		t.Logf("  Found %d images for chapter %s", len(imgs), ch.Num)
		if len(imgs) > 0 {
			t.Logf("  Sample Image 1: %s", imgs[0])
		}
	}
}
