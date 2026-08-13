package cmd

import (
	"context"
	"testing"
)

func TestHDVideoQualityRegressionRejectsUnreadableMetadata(t *testing.T) {
	input := []byte("not-a-video")
	if worse, reason := hdVideoQualityRegression(context.Background(), input, input); !worse || reason == "" {
		t.Fatalf("unreadable metadata should be rejected: worse=%v reason=%q", worse, reason)
	}
}
