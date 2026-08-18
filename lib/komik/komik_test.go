package komik

import (
	"context"
	"testing"
	"time"
)

func TestMultipleComics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	testQueries := []string{
		"solo leveling",
		"taming",
		"photo",
		"sugar daddy",
		"reincarnated",
	}

	for _, q := range testQueries {
		t.Run(q, func(t *testing.T) {
			results, err := SearchComics(ctx, q)
			if err != nil {
				t.Fatalf("SearchComics error for '%s': %v", q, err)
			}
			if len(results) == 0 {
				t.Fatalf("No results found for '%s'", q)
			}

			t.Logf("Query '%s' found %d comic series:", q, len(results))
			for i, r := range results {
				if i >= 3 {
					break
				}
				t.Logf("  [%d] %s (%s)", i+1, r.Title, r.Source)
			}

			// Test GetChapters & GetChapterImages for first result
			first := results[0]
			chapters, err := GetChapters(ctx, first)
			if err != nil {
				t.Fatalf("GetChapters error for '%s' (%s): %v", first.Title, first.Source, err)
			}

			if len(chapters) == 0 {
				t.Fatalf("No chapters found for '%s'", first.Title)
			}

			t.Logf("  Found %d chapters for '%s' (First: Ch %s, Last: Ch %s)", len(chapters), first.Title, chapters[0].Num, chapters[len(chapters)-1].Num)

			// Pick first chapter and test image extraction
			ch := chapters[0]
			imgs, err := GetChapterImages(ctx, ch)
			if err != nil {
				t.Fatalf("GetChapterImages error for '%s' Ch %s: %v", first.Title, ch.Num, err)
			}

			if len(imgs) == 0 {
				t.Fatalf("0 images found for '%s' Ch %s", first.Title, ch.Num)
			}

			t.Logf("  Successfully extracted %d images for '%s' Ch %s (Sample: %s)", len(imgs), first.Title, ch.Num, imgs[0])
		})
	}
}
