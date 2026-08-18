package swgc

import (
	"testing"
	"time"
)

func TestBufferStore(t *testing.T) {
	sender := "628123456789"
	media := []byte("fake_image_bytes")
	mime := "image/jpeg"
	caption := "Test status caption"

	// Test Set and Get
	SetBuffer(sender, media, mime, caption)
	item, ok := GetBuffer(sender)
	if !ok {
		t.Fatalf("expected buffer item to exist for %s", sender)
	}
	if string(item.Media) != string(media) {
		t.Errorf("expected media %s, got %s", media, item.Media)
	}
	if item.Mime != mime {
		t.Errorf("expected mime %s, got %s", mime, item.Mime)
	}
	if item.Caption != caption {
		t.Errorf("expected caption %s, got %s", caption, item.Caption)
	}

	// Test Clear
	ClearBuffer(sender)
	_, ok = GetBuffer(sender)
	if ok {
		t.Fatalf("expected buffer item to be deleted for %s", sender)
	}
}

func TestBufferExpiry(t *testing.T) {
	sender := "628999999999"
	SetBuffer(sender, []byte("data"), "text", "test expiry")

	bufMu.Lock()
	if item, ok := buffers[sender]; ok {
		item.UpdateAt = time.Now().Add(-20 * time.Minute)
	}
	bufMu.Unlock()

	_, ok := GetBuffer(sender)
	if ok {
		t.Fatalf("expected expired buffer item to return false")
	}
}
