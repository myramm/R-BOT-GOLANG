package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDailyLimitSentinel(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{`"00"`, 0},
		{`"0"`, 0},
		{`0`, 0},
		{`20`, 20},
		{`-5`, 0},
		{`"15"`, 15},
		{``, 20},
	}
	for _, tc := range cases {
		e := EnergyConfig{}
		if tc.raw != "" {
			e.RawDailyLimit = json.RawMessage(tc.raw)
		}
		if got := e.DailyLimit(); got != tc.want {
			t.Errorf("DailyLimit(%q) = %d, mau %d", tc.raw, got, tc.want)
		}
	}

	if !(EnergyConfig{RawDailyLimit: json.RawMessage(`"00"`)}).IsUnlimited() {
		t.Error(`IsUnlimited("00") harus true`)
	}
	if (EnergyConfig{RawDailyLimit: json.RawMessage(`20`)}).IsUnlimited() {
		t.Error("IsUnlimited(20) harus false")
	}
}

func TestEnergyAccessorDefaults(t *testing.T) {
	// Semua field absen → default Node.
	e := EnergyConfig{}
	if got := e.PremiumDaily(); got != 1000 {
		t.Errorf("PremiumDaily default = %d, mau 1000", got)
	}
	if got := e.PajakExpired(); got != 0.2 {
		t.Errorf("PajakExpired default = %v, mau 0.2", got)
	}
	if got := e.DiskonGrup(); got != 0.03 {
		t.Errorf("DiskonGrup default = %v, mau 0.03", got)
	}
	// EnergyCost: command tak terdaftar = 1.
	if got := e.EnergyCost("tidakada"); got != 1 {
		t.Errorf("EnergyCost(absen) = %d, mau 1", got)
	}
}

func TestEnergyAccessorNilaiEksplisit(t *testing.T) {
	e := EnergyConfig{
		RawPremiumDaily: json.RawMessage(`500`),
		RawPajakExpired: json.RawMessage(`0.5`),
		RawDiskonGrup:   json.RawMessage(`"0.1"`), // string juga sah (Number())
		Cost:            map[string]int{"hd": 3, "gratis": 0, "aneh": -4},
	}
	if got := e.PremiumDaily(); got != 500 {
		t.Errorf("PremiumDaily = %d, mau 500", got)
	}
	if got := e.PajakExpired(); got != 0.5 {
		t.Errorf("PajakExpired = %v, mau 0.5", got)
	}
	if got := e.DiskonGrup(); got != 0.1 {
		t.Errorf("DiskonGrup = %v, mau 0.1", got)
	}
	// Cost terdaftar dipakai apa adanya; 0 tetap 0 (beda dari absen=1); negatif→0.
	if got := e.EnergyCost("hd"); got != 3 {
		t.Errorf("EnergyCost(hd) = %d, mau 3", got)
	}
	if got := e.EnergyCost("gratis"); got != 0 {
		t.Errorf("EnergyCost(gratis) = %d, mau 0", got)
	}
	if got := e.EnergyCost("aneh"); got != 0 {
		t.Errorf("EnergyCost(negatif) = %d, mau 0", got)
	}
}

func TestPajakDiskonDijepit(t *testing.T) {
	// Nilai di luar [0,1] dijepit (mirror Math.min/max di Node).
	if got := (EnergyConfig{RawPajakExpired: json.RawMessage(`5`)}).PajakExpired(); got != 1 {
		t.Errorf("PajakExpired(5) = %v, mau 1", got)
	}
	if got := (EnergyConfig{RawDiskonGrup: json.RawMessage(`-1`)}).DiskonGrup(); got != 0 {
		t.Errorf("DiskonGrup(-1) = %v, mau 0", got)
	}
}

func TestPrimaryOwnerAddress(t *testing.T) {
	C = Config{OwnerNumber: " 628123456789 ", Owners: []string{"123@lid"}}
	if got := PrimaryOwnerAddress(); got != "628123456789" {
		t.Errorf("PrimaryOwnerAddress nomor = %q", got)
	}
	C = Config{Owners: []string{" 123@lid "}}
	if got := PrimaryOwnerAddress(); got != "123@lid" {
		t.Errorf("PrimaryOwnerAddress owners = %q", got)
	}
	C = Config{}
	if got := PrimaryOwnerAddress(); got != "" {
		t.Errorf("PrimaryOwnerAddress kosong = %q", got)
	}
}

func TestBareNumberDanDigits(t *testing.T) {
	if got := BareNumber("6283891155427@s.whatsapp.net"); got != "6283891155427" {
		t.Errorf("BareNumber @ = %q", got)
	}
	if got := BareNumber("50324836462733:12@lid"); got != "50324836462733" {
		t.Errorf("BareNumber : = %q", got)
	}
	if got := Digits("+62 838-9115 5427"); got != "6283891155427" {
		t.Errorf("Digits = %q", got)
	}
}

func TestLoadDanMatchPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{"prefix":[".","/","!!"],"botName":"R-BOT","owners":["50324836462733@lid"],"energy":{"dailyLimit":"00"}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if MainPrefix() != "." {
		t.Errorf("MainPrefix = %q, mau .", MainPrefix())
	}
	// "!!" (2 char) harus dicoba sebelum "." saat teks diawali "!!".
	if p := MatchPrefix("!!ping"); p != "!!" {
		t.Errorf("MatchPrefix(!!ping) = %q, mau !!", p)
	}
	if p := MatchPrefix(".menu"); p != "." {
		t.Errorf("MatchPrefix(.menu) = %q, mau .", p)
	}
	if p := MatchPrefix("halo"); p != "" {
		t.Errorf("MatchPrefix(halo) = %q, mau kosong", p)
	}
	if !C.Energy.IsUnlimited() {
		t.Error("energy dailyLimit 00 harus unlimited")
	}
}

func TestLoadILovePDFKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"ilovepdf":{"key_love":"project_public_test"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if C.ILovePDF.KeyLove != "project_public_test" {
		t.Fatalf("key_love = %q", C.ILovePDF.KeyLove)
	}
}

func TestLoadDefaultPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"botName":"X"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if MainPrefix() != "." {
		t.Errorf("default MainPrefix = %q, mau .", MainPrefix())
	}
}
