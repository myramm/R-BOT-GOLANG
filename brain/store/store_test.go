package store

import (
	"testing"
)

type rec struct {
	Bank int    `json:"bank"`
	Date string `json:"date"`
}

func TestRoundTrip(t *testing.T) {
	if err := Open(t.TempDir()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = Close() })

	// Key belum ada.
	var out rec
	found, err := Get("energy", &out)
	if err != nil {
		t.Fatalf("Get kosong: %v", err)
	}
	if found {
		t.Fatal("Get key belum ada harus (false)")
	}

	in := rec{Bank: 42, Date: "2026-08-07"}
	if err := Set("energy", in); err != nil {
		t.Fatalf("Set: %v", err)
	}

	out = rec{}
	found, err = Get("energy", &out)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found || out != in {
		t.Fatalf("Get = (%v, %+v), mau (true, %+v)", found, out, in)
	}

	if err := Delete("energy"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	found, _ = Get("energy", &out)
	if found {
		t.Fatal("setelah Delete harus tidak ditemukan")
	}
}

func TestGetOrBiarkanDefault(t *testing.T) {
	if err := Open(t.TempDir()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = Close() })

	out := rec{Bank: 7} // default pemanggil
	if err := GetOr("tidakada", &out); err != nil {
		t.Fatalf("GetOr: %v", err)
	}
	if out.Bank != 7 {
		t.Errorf("GetOr menimpa default: %+v", out)
	}
}

func TestOperasiSebelumOpen(t *testing.T) {
	// Pastikan state global bersih untuk sub-test ini.
	env = nil
	if _, err := Get("x", &rec{}); err == nil {
		t.Error("Get sebelum Open harus error")
	}
	if err := Set("x", rec{}); err == nil {
		t.Error("Set sebelum Open harus error")
	}
}
