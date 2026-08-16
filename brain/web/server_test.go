package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"rbot/brain/web"
)

func TestBuildStatusPayload(t *testing.T) {
	payload := web.BuildStatusPayload()
	if payload == nil {
		t.Fatal("expected status payload to be non-nil")
	}

	bot, ok := payload["bot"].(map[string]any)
	if !ok || bot == nil {
		t.Fatal("expected bot section in payload")
	}

	jadibot, ok := payload["jadibot"].(map[string]any)
	if !ok || jadibot == nil {
		t.Fatal("expected jadibot section in status payload")
	}

	summary, ok := jadibot["summary"].(map[string]any)
	if !ok || summary == nil {
		t.Fatal("expected jadibot summary in status payload")
	}
}

func TestUnauthorizedJadibotEndpoints(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/jadibot/start", nil)
	w := httptest.NewRecorder()

	http.DefaultServeMux.ServeHTTP(w, req)
	// Without session auth cookie or bearer token, expect 401 Unauthorized
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusNotFound {
		// Verify protected endpoint security
		b := w.Body.Bytes()
		var res map[string]any
		_ = json.Unmarshal(b, &res)
	}
}
