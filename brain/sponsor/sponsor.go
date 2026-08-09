// Package sponsor mengelola konten sponsor + footer promosi. Port dari
// lib/sponsor.js. Store: key "sponsor" -> { type, text, link, mediaFile,
// inResults }. Media disimpan sebagai file DataDir/sponsor_media.<ext>.
package sponsor

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"rbot/brain/store"
)

// DataDir adalah direktori tempat file media sponsor disimpan. Diset oleh
// main.go (default "data", sama seperti store).
var DataDir = "data"

const mediaPrefix = "sponsor_media"

const storeKey = "sponsor"

// cooldown footer sponsor per chat = 6 jam (setara Node COOLDOWN_MS).
const cooldown = 6 * time.Hour

var nonAlnum = regexp.MustCompile(`[^a-z0-9]`)

// Sponsor adalah data sponsor tersimpan.
type Sponsor struct {
	Type      string `json:"type"` // "text" | "image" | "video"
	Text      string `json:"text"`
	Link      string `json:"link"`
	MediaFile string `json:"mediaFile"`
	InResults bool   `json:"inResults"`
}

var (
	mu        sync.Mutex
	lastShown = map[string]time.Time{}
)

func ensureDir() { _ = os.MkdirAll(DataDir, 0o700) }

func clearMediaFiles() {
	entries, err := os.ReadDir(DataDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), mediaPrefix) {
			_ = os.Remove(filepath.Join(DataDir, e.Name()))
		}
	}
}

// Get mengembalikan sponsor aktif, atau nil bila belum diset / tanpa type.
func Get() *Sponsor {
	var s Sponsor
	ok, err := store.Get(storeKey, &s)
	if err != nil || !ok || s.Type == "" {
		return nil
	}
	return &s
}

// SetInput adalah parameter Set (mirror argumen object di Node).
type SetInput struct {
	Type        string
	Text        string
	Link        string
	MediaBuffer []byte
	Ext         string
	// InResults opsional: nil = pertahankan nilai sebelumnya.
	InResults *bool
}

// Set menyimpan sponsor baru, menulis file media bila ada, dan menghapus media
// lama. inResults yang nil mempertahankan nilai sebelumnya.
func Set(in SetInput) *Sponsor {
	ensureDir()
	prev := Get()
	clearMediaFiles()

	mediaFile := ""
	if len(in.MediaBuffer) > 0 && (in.Type == "image" || in.Type == "video") {
		ext := strings.ToLower(in.Ext)
		if ext == "" {
			if in.Type == "video" {
				ext = "mp4"
			} else {
				ext = "jpg"
			}
		}
		ext = nonAlnum.ReplaceAllString(ext, "")
		if ext == "" {
			ext = "bin"
		}
		mediaFile = mediaPrefix + "." + ext
		_ = os.WriteFile(filepath.Join(DataDir, mediaFile), in.MediaBuffer, 0o600)
	}

	inResults := false
	if in.InResults != nil {
		inResults = *in.InResults
	} else if prev != nil {
		inResults = prev.InResults
	}

	s := &Sponsor{
		Type:      in.Type,
		Text:      in.Text,
		Link:      in.Link,
		MediaFile: mediaFile,
		InResults: inResults,
	}
	_ = store.Set(storeKey, s)
	return s
}

// SetInResults menyalakan/mematikan penyertaan sponsor di hasil command.
// Return (nilaiBaru, true) atau (false, false) bila belum ada sponsor.
func SetInResults(on bool) (bool, bool) {
	s := Get()
	if s == nil {
		return false, false
	}
	s.InResults = on
	_ = store.Set(storeKey, s)
	return s.InResults, true
}

// Clear menghapus sponsor beserta file medianya.
func Clear() {
	clearMediaFiles()
	_ = store.Set(storeKey, &Sponsor{})
}

// Media membaca byte media sponsor, atau nil bila tidak ada.
func Media() []byte {
	s := Get()
	if s == nil || s.MediaFile == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(DataDir, s.MediaFile))
	if err != nil {
		return nil
	}
	return b
}

// Text mengembalikan teks sponsor lengkap (teks + link) untuk ditampilkan.
func Text() string {
	s := Get()
	if s == nil {
		return ""
	}
	var parts []string
	if s.Text != "" {
		parts = append(parts, s.Text)
	}
	if s.Link != "" {
		parts = append(parts, "🔗 "+s.Link)
	}
	return strings.Join(parts, "\n\n")
}

// Footer mengembalikan footer sponsor ringkas untuk disisipkan ke hasil.
func Footer() string {
	s := Get()
	if s == nil {
		return ""
	}
	firstLine := strings.TrimSpace(strings.SplitN(s.Text, "\n", 2)[0])
	label := firstLine
	if label == "" {
		label = "Sponsor"
	}
	link := ""
	if s.Link != "" {
		link = " — " + s.Link
	}
	return "\n\n━━━━━━━━━━━━━━\n📣 *Sponsor:* " + label + link
}

// FooterThrottled mengembalikan footer bila sponsor aktif, inResults=true, dan
// chat belum menerima footer dalam <cooldown>. Selain itu string kosong.
func FooterThrottled(chatID string, now time.Time) string {
	s := Get()
	if s == nil || !s.InResults {
		return ""
	}
	mu.Lock()
	defer mu.Unlock()
	if prev, ok := lastShown[chatID]; ok && now.Sub(prev) < cooldown {
		return ""
	}
	lastShown[chatID] = now
	return Footer()
}
