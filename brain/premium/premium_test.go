package premium

import (
	"testing"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/config"
	"rbot/brain/store"
)

func evtFrom(user string) *events.Message {
	e := &events.Message{}
	e.Info.Sender = types.NewJID(user, types.DefaultUserServer)
	return e
}

func setup(t *testing.T, owners ...string) {
	t.Helper()
	if err := store.Open(t.TempDir()); err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	config.C = config.Config{Owners: owners}
}

func TestOwnerSelaluPremium(t *testing.T) {
	setup(t, "999")
	if !IsPremium(evtFrom("999")) {
		t.Error("owner harus selalu premium")
	}
	if IsPremium(evtFrom("123")) {
		t.Error("non-owner tanpa entri tidak boleh premium")
	}
}

func TestAddListRemove(t *testing.T) {
	setup(t)
	exp, ok := Add("123", 7)
	if !ok || exp <= nowMs() {
		t.Fatalf("Add 7 hari → (%d, %v), harus expiry di masa depan", exp, ok)
	}
	if !IsPremium(evtFrom("123")) {
		t.Error("123 harus premium setelah Add")
	}

	list := List()
	if len(list) != 1 || list[0].ID != "123" {
		t.Fatalf("List = %+v, mau 1 entri id 123", list)
	}

	// Perpanjang: expiry harus bertambah dari yang lama.
	exp2, _ := Add("123", 3)
	if exp2 <= exp {
		t.Errorf("perpanjang → %d, harus > %d", exp2, exp)
	}

	if !Remove("123") {
		t.Error("Remove(123) harus true")
	}
	if Remove("123") {
		t.Error("Remove kedua harus false")
	}
	if IsPremium(evtFrom("123")) {
		t.Error("123 tidak boleh premium setelah Remove")
	}
}

func TestPruneKadaluarsa(t *testing.T) {
	setup(t)
	// Tulis entri sudah lewat langsung ke store.
	_ = store.Set(storeKey, map[string]int64{"123": nowMs() - 1000, "456": nowMs() + 3600_000})
	list := List() // memicu prune
	if len(list) != 1 || list[0].ID != "456" {
		t.Fatalf("List setelah prune = %+v, mau hanya 456", list)
	}
	// Entri kadaluarsa sudah terhapus dari store.
	data := readAll()
	if _, ada := data["123"]; ada {
		t.Error("123 (kadaluarsa) harus terhapus dari store")
	}
}

func TestRemaining(t *testing.T) {
	setup(t)
	if got := Remaining(evtFrom("123")); got != 0 {
		t.Errorf("Remaining tanpa premium = %d, mau 0", got)
	}
	Add("123", 1)
	if got := Remaining(evtFrom("123")); got <= 0 {
		t.Errorf("Remaining setelah Add = %d, harus > 0", got)
	}
}

func TestAddIdKosong(t *testing.T) {
	setup(t)
	if _, ok := Add("", 5); ok {
		t.Error("Add id kosong harus (_, false)")
	}
}
