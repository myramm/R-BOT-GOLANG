package cmd

import (
	"context"
	"testing"
)

func TestHDVideoQualityRegression(t *testing.T) {
	input := []byte("not-a-video")
	if worse, _ := hdVideoQualityRegression(context.Background(), input, input); worse {
		t.Fatal("metadata-unavailable video should not be rejected")
	}
}
