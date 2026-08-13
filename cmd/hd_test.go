package cmd

import (
	"context"
	"errors"
	"testing"
)

func TestHDVideoQualityRegressionRejectsUnreadableMetadata(t *testing.T) {
	input := []byte("not-a-video")
	if worse, reason := hdVideoQualityRegression(context.Background(), input, input); !worse || reason == "" {
		t.Fatalf("unreadable metadata should be rejected: worse=%v reason=%q", worse, reason)
	}
}

func TestHDChooseVideoFallbackUsesOriginalWhenPrimaryFails(t *testing.T) {
	primaryErr := errors.New("AI quota exhausted")
	original := []byte("original-video")
	called := false

	got, usedFallback, gotErr := hdChooseVideoFallback(
		func() ([]byte, error) {
			called = true
			return nil, primaryErr
		},
		original,
		func([]byte) error { return nil },
	)
	if !called {
		t.Fatal("primary enhancer was not called")
	}
	if !usedFallback {
		t.Fatal("expected original video fallback")
	}
	if string(got) != string(original) {
		t.Fatalf("fallback output = %q, want original video", got)
	}
	if !errors.Is(gotErr, primaryErr) {
		t.Fatalf("fallback error = %v, want primary error", gotErr)
	}
}

func TestHDChooseVideoFallbackRejectsLowerQualityPrimary(t *testing.T) {
	original := []byte("original-video")
	called := false

	got, usedFallback, gotErr := hdChooseVideoFallback(
		func() ([]byte, error) {
			called = true
			return []byte("lower-quality-video"), nil
		},
		original,
		func([]byte) error { return errors.New("quality regression") },
	)
	if !called {
		t.Fatal("primary enhancer was not called")
	}
	if !usedFallback || string(got) != string(original) {
		t.Fatalf("expected original fallback, got=%q usedFallback=%v", got, usedFallback)
	}
	if gotErr == nil || gotErr.Error() != "quality regression" {
		t.Fatalf("fallback error = %v, want quality regression", gotErr)
	}
}

func TestHDChooseVideoFallbackKeepsValidPrimary(t *testing.T) {
	primary := []byte("enhanced-video")
	got, usedFallback, gotErr := hdChooseVideoFallback(
		func() ([]byte, error) { return primary, nil },
		[]byte("original-video"),
		func([]byte) error { return nil },
	)
	if usedFallback || gotErr != nil || string(got) != string(primary) {
		t.Fatalf("primary result changed: got=%q fallback=%v err=%v", got, usedFallback, gotErr)
	}
}
