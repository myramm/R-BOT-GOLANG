package energy

import (
	"encoding/json"
	"testing"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/config"
	"rbot/brain/store"
)

// evtFrom membuat pesan sintetis dari satu nomor (server s.whatsapp.net).
func evtFrom(user string) *events.Message {
	e := &events.Message{}
	e.Info.Sender = types.NewJID(user, types.DefaultUserServer)
	return e
}

// setup menyiapkan store temp + config energi (limit, owner) untuk sub-test.
func setup(t *testing.T, limit string, owners ...string) {
	t.Helper()
	if err := store.Open(t.TempDir()); err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	config.C = config.Config{Owners: owners}
	config.C.Energy = config.EnergyConfig{
		RawDailyLimit: json.RawMessage(limit),
		Cost:          map[string]int{"hd": 3, "sticker": 1},
	}
}

func TestBiayaEfektifDiskon(t *testing.T) {
	config.C = config.Config{}
	config.C.Energy = config.EnergyConfig{
		Cost:          map[string]int{"hd": 3, "sticker": 1},
		RawDiskonGrup: json.RawMessage(`0.03`),
	}
	// Bukan member: biaya penuh.
	if got := BiayaEfektif("hd", false); got != 3 {
		t.Errorf("BiayaEfektif(hd, non-member) = %d, mau 3", got)
	}
	// Member: ceil(3 * 0.97) = ceil(2.91) = 3.
	if got := BiayaEfektif("hd", true); got != 3 {
		t.Errorf("BiayaEfektif(hd, member) = %d, mau 3", got)
	}
	// sticker member: max(1, ceil(1*0.97)) = 1 (tidak pernah gratis).
	if got := BiayaEfektif("sticker", true); got != 1 {
		t.Errorf("BiayaEfektif(sticker, member) = %d, mau 1", got)
	}
}

func TestOwnerDanUnlimited(t *testing.T) {
	// Owner selalu unlimited walau limit 20.
	setup(t, `20`, "999")
	if !HasEnergy(evtFrom("999"), 9999) {
		t.Error("owner harus selalu punya energi")
	}
	if got := Get(evtFrom("999")); !got.Unlimited {
		t.Error("owner harus Unlimited")
	}
	// Unlimited global: limit 0.
	setup(t, `0`)
	if !HasEnergy(evtFrom("123"), 9999) {
		t.Error("limit 0 = unlimited untuk semua")
	}
}

func TestConsumeDanHabis(t *testing.T) {
	setup(t, `20`)
	u := func() *events.Message { return evtFrom("123") }

	if got := Get(u()).Remaining; got != 20 {
		t.Fatalf("awal Remaining = %d, mau 20", got)
	}
	Consume(u(), 5)
	if got := Get(u()).Remaining; got != 15 {
		t.Errorf("setelah consume 5, Remaining = %d, mau 15", got)
	}
	// Tidak bisa minus.
	Consume(u(), 100)
	if got := Get(u()).Remaining; got != 0 {
		t.Errorf("consume berlebih, Remaining = %d, mau 0", got)
	}
	if HasEnergy(u(), 1) {
		t.Error("energi 0 tidak boleh cukup untuk cost 1")
	}
	if !HasEnergy(u(), 0) {
		t.Error("cost 0 selalu cukup")
	}
}

func TestRestoreTidakLebihDariPlafon(t *testing.T) {
	setup(t, `20`)
	u := func() *events.Message { return evtFrom("123") }

	Consume(u(), 12) // sisa 8
	r := Restore(u(), 5)
	if !r.OK || r.Bank != 13 {
		t.Fatalf("Restore 5 dari 8 → Bank %d (ok=%v), mau 13", r.Bank, r.OK)
	}
	// Isi sampai penuh: plafon = limit(20)+bonus(0). Sisa 13, tambah 20 → mentok 20.
	r = Restore(u(), 20)
	if r.Bank != 20 || r.Terbuang != 13 {
		t.Errorf("Restore mentok → Bank %d terbuang %d, mau Bank 20 terbuang 13", r.Bank, r.Terbuang)
	}
	// Sudah penuh.
	r = Restore(u(), 5)
	if !r.Penuh || r.Tambah != 0 {
		t.Errorf("Restore saat penuh → Penuh=%v Tambah=%d, mau true/0", r.Penuh, r.Tambah)
	}
}

func TestAddBonusMenaikkanPlafon(t *testing.T) {
	setup(t, `20`)
	u := func() *events.Message { return evtFrom("123") }
	_ = Get(u()) // materialisasi record (bank=20)

	// Bonus +10: bank 20→30, bonus 10, plafon jadi 30.
	if bonus, ok := AddBonus("123", 10); !ok || bonus != 10 {
		t.Fatalf("AddBonus +10 → (%d, %v), mau (10, true)", bonus, ok)
	}
	if got := Get(u()).Remaining; got != 30 {
		t.Errorf("setelah bonus, Remaining = %d, mau 30", got)
	}
	// Bonus tidak bisa jadi negatif.
	if bonus, _ := AddBonus("123", -100); bonus != 0 {
		t.Errorf("AddBonus besar-negatif → bonus %d, mau 0 (dijepit)", bonus)
	}
}

func TestResetDanResetAll(t *testing.T) {
	setup(t, `20`)
	Consume(evtFrom("123"), 10)
	Consume(evtFrom("456"), 10)

	if !Reset("123") {
		t.Fatal("Reset(123) harus true")
	}
	if got := Get(evtFrom("123")).Remaining; got != 20 {
		t.Errorf("setelah Reset, Remaining = %d, mau 20", got)
	}
	// 123 (di-reset) + 456 masih ada → 2 record.
	if n := ResetAll(); n != 2 {
		t.Errorf("ResetAll = %d, mau 2", n)
	}
	if Reset("") {
		t.Error("Reset(id kosong) harus false")
	}
}

func TestKlaimHarian(t *testing.T) {
	setup(t, `20`)
	u := evtFrom("123")

	// Habiskan lalu paksa tanggal kemarin supaya sync mengisi ulang.
	Consume(u, 20)
	data := readAll()
	if data["123"] == nil {
		t.Fatal("record 123 harus ada")
	}
	data["123"].Date = "2000-01-01"
	data["123"].Bank = 0
	writeAll(data)

	// Get menyinkronkan: isi ulang ke limit+bonus = 20, catat event isi-harian.
	info := Get(evtFrom("123"))
	if info.Remaining != 20 {
		t.Errorf("klaim harian → Remaining %d, mau 20", info.Remaining)
	}
	found := false
	for _, e := range info.Events {
		if e.Kind == EventIsiHarian {
			found = true
		}
	}
	if !found {
		t.Errorf("harus ada event isi-harian, dapat %+v", info.Events)
	}
}

func TestMigrasiFormatLama(t *testing.T) {
	setup(t, `20`)
	// Tulis record format lama { used, date, bonus } langsung ke store.
	used := 5
	old := map[string]*record{
		"123": {Date: today(), Bonus: 2, Used: &used},
	}
	writeAll(old)

	// Get memigrasi: bank = max(0, limit(20)+bonus(2)-used(5)) = 17.
	if got := Get(evtFrom("123")).Bank; got != 17 {
		t.Errorf("migrasi bank = %d, mau 17", got)
	}
	// Field used sudah dibersihkan di store.
	data := readAll()
	if data["123"].Used != nil {
		t.Error("field used harus nil setelah migrasi")
	}
}
