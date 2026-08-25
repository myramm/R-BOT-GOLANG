package simi

import (
	"testing"

	"rbot/brain/config"
	"rbot/brain/store"
)

// Regresi: `.simi off` harus tetap mati walau bentuk JID chat berubah
// (migrasi LID WhatsApp: @g.us <-> @lid, @s.whatsapp.net <-> @lid).

func TestOffTetapMatiLewatDomainGrup(t *testing.T) {
	setupTestStore(t)
	config.C.Simi.EnabledByDefault = true

	if err := SetEnabledIn("120363021824594@g.us", true, "", false); err != nil {
		t.Fatalf("SetEnabledIn: %v", err)
	}
	if IsEnabledIn("120363021824594@g.us", true, "") {
		t.Fatal("grup g.us harus mati")
	}
	if IsEnabledIn("120363021824594@lid", true, "") {
		t.Fatal("grup domain lain (numeric sama) harus mati via key kanonik")
	}
}

func TestOffTetapMatiLewatBentukJIDDM(t *testing.T) {
	setupTestStore(t)
	config.C.Simi.EnabledByDefault = true

	// .simi off saat chat masih berbentuk PN
	if err := SetEnabledIn("6281234567890@s.whatsapp.net", false, "", false); err != nil {
		t.Fatalf("SetEnabledIn: %v", err)
	}
	// Pesan quote berikutnya datang sebagai @lid; sender ter-resolve ke PN
	if IsEnabledIn("987654321098765@lid", false, "6281234567890") {
		t.Fatal("DM @lid harus mati via key kanonik sender")
	}
	if IsEnabledIn("6281234567890@s.whatsapp.net", false, "6281234567890") {
		t.Fatal("DM bentuk PN harus tetap mati")
	}
}

func TestOnTetapNyalaLintasDomainGrup(t *testing.T) {
	setupTestStore(t)
	config.C.Simi.EnabledByDefault = false

	if err := SetEnabledIn("120363021824594@g.us", true, "", true); err != nil {
		t.Fatalf("SetEnabledIn: %v", err)
	}
	if !IsEnabledIn("120363021824594@lid", true, "") {
		t.Fatal("grup domain lain harus tetap nyala (explicit on)")
	}
}

func TestTanpaKeyPakaiDefault(t *testing.T) {
	setupTestStore(t)

	config.C.Simi.EnabledByDefault = true
	if !IsEnabledIn("999@g.us", true, "") {
		t.Fatal("chat tanpa setting harus pakai default (true)")
	}
	config.C.Simi.EnabledByDefault = false
	if IsEnabledIn("999@g.us", true, "") {
		t.Fatal("chat tanpa setting harus pakai default (false)")
	}
}

func TestKeyLamaTetapDibaca(t *testing.T) {
	setupTestStore(t)
	config.C.Simi.EnabledByDefault = true

	// Key ditulis oleh versi lama (hanya key eksak)
	if err := store.Set(chatSettingKeyPrefix+"120363021824594@g.us", false); err != nil {
		t.Fatalf("store.Set: %v", err)
	}
	if IsEnabledIn("120363021824594@g.us", true, "") {
		t.Fatal("key eksak versi lama harus tetap dibaca")
	}
}
