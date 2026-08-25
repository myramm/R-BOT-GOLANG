package store

import "testing"

// Regresi: Set(false) lalu Get harus found=true, enabled=false.
func TestSetFalseRoundTrip(t *testing.T) {
	if err := Open(t.TempDir()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = Close() })
	if err := Set("simi:chat:x@y", false); err != nil {
		t.Fatalf("Set false: %v", err)
	}
	var enabled bool
	found, err := Get("simi:chat:x@y", &enabled)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("key harus ditemukan setelah Set(false)")
	}
	if enabled {
		t.Fatal("nilai harus false, dapat true")
	}
}
