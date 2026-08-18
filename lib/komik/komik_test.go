package komik

import (
	"context"
	"testing"
	"time"
)

func TestSearchComicsGrouped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	results, err := SearchComics(ctx, "solo leveling")
	if err != nil {
		t.Fatalf("SearchComics error: %v", err)
	}

	t.Logf("SearchComics for 'solo leveling' found %d series:", len(results))
	for i, r := range results {
		t.Logf("  [%d] %s (%s) - %d chapters", i+1, r.Title, r.Source, r.Count)
	}
}
