package errortracker

import (
	"testing"

	"rbot/brain/store"
)

func TestErrorTracker(t *testing.T) {
	tracker := &Tracker{
		errors: make([]ErrorEntry, 0, 100),
		max:    50,
	}

	tracker.RecordError("COMMAND", "database lock error", "at handleDbQuery()")
	if len(tracker.errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(tracker.errors))
	}

	id := tracker.errors[0].ID
	if tracker.errors[0].Count != 1 {
		t.Errorf("expected count 1, got %d", tracker.errors[0].Count)
	}

	// Record the exact same error again (should deduplicate and increment count)
	tracker.RecordError("COMMAND", "database lock error", "at handleDbQuery() second time")
	if len(tracker.errors) != 1 {
		t.Fatalf("expected 1 deduplicated error, got %d", len(tracker.errors))
	}
	if tracker.errors[0].Count != 2 {
		t.Errorf("expected count 2, got %d", tracker.errors[0].Count)
	}

	// Update AI analysis
	ok := tracker.UpdateAiAnalysis(id, "Penyebabnya adalah SQLite contention.")
	if !ok {
		t.Errorf("expected UpdateAiAnalysis to succeed")
	}

	e, found := tracker.GetErrorByID(id)
	if !found || e.AiAnalysis != "Penyebabnya adalah SQLite contention." {
		t.Errorf("expected AiAnalysis to match, got %v", e.AiAnalysis)
	}

	// Get Summary
	summary := tracker.GetSummary()
	if total, ok := summary["total"].(int); !ok || total != 1 {
		t.Errorf("expected summary total 1, got %v", summary["total"])
	}

	// Delete error
	deleted := tracker.DeleteError(id)
	if !deleted || len(tracker.errors) != 0 {
		t.Errorf("expected error to be deleted, remaining: %d", len(tracker.errors))
	}
}

func TestAiFixConfig(t *testing.T) {
	_ = store.Open(t.TempDir())
	cfg := GetAiFixConfig()
	if cfg.Provider == "" {
		t.Errorf("expected default provider, got empty")
	}

	newCfg := AiFixConfig{
		Provider:    "openai",
		ApiUrl:      "https://api.openai.com/v1/chat/completions",
		ApiKey:      "test-key-123",
		Model:       "gpt-4o-mini",
		Temperature: 0.2,
	}
	if err := SetAiFixConfig(newCfg); err != nil {
		t.Fatalf("SetAiFixConfig: %v", err)
	}

	saved := GetAiFixConfig()
	if saved.ApiKey != "test-key-123" {
		t.Errorf("expected ApiKey 'test-key-123', got %q", saved.ApiKey)
	}
}
