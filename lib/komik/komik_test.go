package komik

import (
	"context"
	"testing"
	"time"
)

func TestSoloLevelingChapter00_1(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	ch := Chapter{
		ID:     "113780",
		Num:    "00.1",
		Title:  "Solo Leveling Chapter 00.1",
		URL:    "https://komiku.org/solo-leveling-chapter-00-1/",
		Slug:   "solo-leveling-chapter-00-1",
		Source: "komiku",
	}

	imgs, err := GetChapterImages(ctx, ch)
	if err != nil {
		t.Fatalf("GetChapterImages error for Solo Leveling Chapter 00.1: %v", err)
	}

	t.Logf("GetChapterImages returned %d images for Chapter 00.1:", len(imgs))
	for i, img := range imgs {
		if i >= 5 {
			break
		}
		t.Logf("  Image %d: %s", i+1, img)
	}

	if len(imgs) == 0 {
		t.Errorf("Expected > 0 images for Solo Leveling Chapter 00.1")
	}
}
