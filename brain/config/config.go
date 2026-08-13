// Package config memuat dan menyediakan konfigurasi bot (port dari config.js).
package config

import (
	"encoding/json"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// EnergyConfig meniru blok energy di config.js. DailyLimit bisa berupa angka
// atau string "00" (sentinel energi tak terbatas), jadi ditipe json.RawMessage
// lalu di-decode manual.
type EnergyConfig struct {
	RawDailyLimit   json.RawMessage `json:"dailyLimit"`
	RawPremiumDaily json.RawMessage `json:"premiumDaily"`
	RawPajakExpired json.RawMessage `json:"pajakExpired"`
	RawDiskonGrup   json.RawMessage `json:"diskonGrup"`
	Cost            map[string]int  `json:"cost"`
}

// Config adalah keseluruhan config.json.
type Config struct {
	Prefix      []string `json:"prefix"`
	BotName     string   `json:"botName"`
	Footer      string   `json:"footer"`
	LogoURL     string   `json:"logoUrl"`
	BotNumber   string   `json:"botNumber"`
	OwnerNumber string   `json:"ownerNumber"`
	Owners      []string `json:"owners"`

	Sticker struct {
		Packname string `json:"packname"`
		Author   string `json:"author"`
	} `json:"sticker"`

	Energy EnergyConfig `json:"energy"`

	GrupOfficial struct {
		Invite string `json:"invite"`
		JID    string `json:"jid"`
	} `json:"grupOfficial"`

	Media struct {
		MaxDuration   int `json:"maxDuration"`
		MaxFileSize   int `json:"maxFileSize"`
		MaxResolution int `json:"maxResolution"`
	} `json:"media"`

	Web struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Password string `json:"password"`
	} `json:"web"`

	LogLevel   string `json:"logLevel"`
	MaxJadibot int    `json:"maxJadibot"`

	ILovePDF struct {
		PublicKey string `json:"publicKey"`
		KeyLove   string `json:"key_love"`
	} `json:"ilovepdf"`

	AI struct {
		APIKey       string   `json:"apiKey"`
		Models       []string `json:"models"`
		SystemPrompt string   `json:"systemPrompt"`
	} `json:"ai"`

	Kamino struct {
		APIURL           string `json:"apiUrl"`
		ResolveTimeoutMs int    `json:"resolveTimeoutMs"`
		AudioDocMB       int    `json:"audioDocMB"`
	} `json:"kamino"`

	PipedInstances []string `json:"pipedInstances"`

	// prefixes disortir panjang-desc untuk MatchPrefix; diisi saat Load.
	prefixes []string
}

// C adalah instance global config, terisi setelah Load.
var C Config

var nonDigit = regexp.MustCompile(`[^0-9]`)

// Load membaca config.json dari path lalu menyiapkan turunan (prefix tersortir).
func Load(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &C); err != nil {
		return err
	}
	if len(C.Prefix) == 0 {
		C.Prefix = []string{"."}
	}
	C.prefixes = append([]string(nil), C.Prefix...)
	sort.SliceStable(C.prefixes, func(i, j int) bool {
		return len(C.prefixes[i]) > len(C.prefixes[j])
	})
	return nil
}

// MainPrefix mengembalikan prefix utama (yang pertama di config).
func MainPrefix() string {
	if len(C.Prefix) > 0 {
		return C.Prefix[0]
	}
	return "."
}

// MatchPrefix mengembalikan prefix yang cocok dengan awal text, atau "".
func MatchPrefix(text string) string {
	for _, p := range C.prefixes {
		if p != "" && strings.HasPrefix(text, p) {
			return p
		}
	}
	return ""
}

// BareNumber mengambil bagian angka sebelum '@' atau ':' dari sebuah JID/nomor.
func BareNumber(jid string) string {
	s := jid
	if i := strings.IndexAny(s, "@:"); i >= 0 {
		s = s[:i]
	}
	return s
}

// Digits menyaring hanya karakter angka dari string.
func Digits(s string) string {
	return nonDigit.ReplaceAllString(s, "")
}

// PrimaryOwnerAddress mengembalikan alamat owner utama untuk notifikasi bot.
// OwnerNumber diprioritaskan karena merupakan nomor yang ditampilkan ke user;
// bila kosong, gunakan owners[0] yang juga dapat berupa JID LID.
func PrimaryOwnerAddress() string {
	owner := strings.TrimSpace(C.OwnerNumber)
	if owner == "" && len(C.Owners) > 0 {
		owner = strings.TrimSpace(C.Owners[0])
	}
	if owner == "" || strings.Contains(owner, "@") {
		return owner
	}
	return Digits(owner)
}

// jsNumber meniru Number(x) di JS untuk RawMessage berisi angka atau string.
// Mengembalikan (nilai, true) bila terdefinisi & terbaca sebagai angka; (0,
// false) bila absen atau bukan angka (setara NaN → pemanggil pakai default).
func jsNumber(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		s = strings.TrimSpace(s)
		if s == "" {
			return 0, true // Number('') === 0
		}
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return v, true
		}
	}
	return 0, false
}

// DailyLimit meng-decode energy.dailyLimit (angka atau "00") jadi int.
// 0 berarti energi tak terbatas untuk semua user. Default 20 bila absen.
func (e EnergyConfig) DailyLimit() int {
	n, ok := jsNumber(e.RawDailyLimit)
	if !ok {
		return 20
	}
	if n < 0 {
		return 0
	}
	return int(math.Trunc(n))
}

// PremiumDaily adalah jatah energi harian premium (ditumpuk). Default 1000.
func (e EnergyConfig) PremiumDaily() int {
	n, ok := jsNumber(e.RawPremiumDaily)
	if !ok {
		return 1000
	}
	if n < 0 {
		return 0
	}
	return int(math.Trunc(n))
}

// PajakExpired adalah potongan sisa energi saat premium habis (0.2 = 20%),
// dijepit ke [0,1]. Default 0.2.
func (e EnergyConfig) PajakExpired() float64 {
	n, ok := jsNumber(e.RawPajakExpired)
	if !ok {
		return 0.2
	}
	return math.Min(1, math.Max(0, n))
}

// DiskonGrup adalah diskon energi member grup official (0.03 = 3%), dijepit ke
// [0,1]. Default 0.03.
func (e EnergyConfig) DiskonGrup() float64 {
	n, ok := jsNumber(e.RawDiskonGrup)
	if !ok {
		return 0.03
	}
	return math.Min(1, math.Max(0, n))
}

// EnergyCost mengembalikan biaya dasar sebuah command. Command tak terdaftar =
// 1 (setara Node). Nilai negatif dinaikkan ke 0.
func (e EnergyConfig) EnergyCost(cmd string) int {
	c, ok := e.Cost[cmd]
	if !ok {
		return 1
	}
	if c < 0 {
		return 0
	}
	return c
}

// IsUnlimited true bila energi tak terbatas (dailyLimit == 0/"00").
func (e EnergyConfig) IsUnlimited() bool { return e.DailyLimit() == 0 }
