package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFormatPairingCode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"ABCD1234", "ABCD-1234"},
		{"12345678", "1234-5678"},
		{"", ""},
		{"ABC123", "ABC123"},
		{"ABCD12345", "ABCD12345"},
	}
	for _, c := range cases {
		if got := FormatPairingCode(c.in); got != c.want {
			t.Errorf("FormatPairingCode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Tanpa password di config (default test), request dianggap terautentikasi;
// method selain POST harus ditolak sebelum sentuh client WhatsApp.
func TestHandleBotPairingRejectsNonPost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/bot/pairing", nil)
	w := httptest.NewRecorder()
	handleBotPairing(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleBotPairingResetRejectsNonPost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/bot/pairing/reset", nil)
	w := httptest.NewRecorder()
	handleBotPairingReset(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}
