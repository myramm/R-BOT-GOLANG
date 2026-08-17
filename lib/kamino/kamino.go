// Package kamino adalah klien resolver download (port dari lib/commands/download.js
// bagian backend). Alur: minta challenge → selesaikan Proof-of-Work SHA-256 →
// tukar jadi token sesi → panggil endpoint resolve per-platform. Token disimpan
// di memori & di-refresh otomatis saat 403.
package kamino

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"rbot/brain/config"
	"rbot/lib/httpx"
)

const (
	defaultBaseURL = "https://dl.kaminoater.top"
	requestTimeout = 30 * time.Second
)

// Media adalah satu item media hasil resolve.
type Media struct {
	Type  string // "audio" | "video" | "image"
	URL   string
	Ext   string
	Label string
}

// Result adalah hasil resolve sebuah link.
type Result struct {
	Title    string
	Source   string
	Medias   []Media
	Playlist bool // true bila link berupa playlist/album (tak didukung)
}

// platform deteksi berdasarkan host link (port konstanta DETECT).
var detectPatterns = []struct {
	key string
	re  *regexp.Regexp
}{
	{"tiktok", regexp.MustCompile(`(?i)tiktok\.com|vt\.tiktok|vm\.tiktok`)},
	{"instagram", regexp.MustCompile(`(?i)instagram\.com|instagr\.am`)},
	{"facebook", regexp.MustCompile(`(?i)facebook\.com|fb\.watch|fb\.com`)},
	{"twitter", regexp.MustCompile(`(?i)twitter\.com|x\.com|t\.co`)},
	{"threads", regexp.MustCompile(`(?i)threads\.(net|com)`)},
	{"pinterest", regexp.MustCompile(`(?i)pinterest\.[a-z.]+|pin\.it`)},
	{"youtube", regexp.MustCompile(`(?i)youtube\.com|youtu\.be`)},
	{"spotify", regexp.MustCompile(`(?i)open\.spotify\.com|spotify\.link`)},
}

// Detect mengembalikan nama platform dari URL, atau "" bila tak dikenali.
func Detect(rawURL string) string {
	for _, d := range detectPatterns {
		if d.re.MatchString(rawURL) {
			return d.key
		}
	}
	return ""
}

var reDigitsOnly = regexp.MustCompile(`^\d+$`)

func baseURL() string {
	u := strings.TrimRight(config.C.Kamino.APIURL, "/")
	if u == "" {
		return defaultBaseURL
	}
	return u
}

func resolveTimeout() time.Duration {
	if ms := config.C.Kamino.ResolveTimeoutMs; ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return 120 * time.Second
}

// --- Proof of Work ---------------------------------------------------------

// scheme mendeskripsikan parameter PoW dari server.
type scheme struct {
	D      int      `json:"d"`
	Parts  []string `json:"parts"`
	Sep    string   `json:"sep"`
	Rounds int      `json:"rounds"`
	Salt   string   `json:"salt"`
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// partValue meniru partValue Node: 'i'→index, 's'→salt, selain itu→nonce.
func partValue(p string, i int, sc *scheme, nonce string) string {
	switch p {
	case "i":
		return itoa(i)
	case "s":
		return sc.Salt
	default:
		return nonce
	}
}

// solvePow mencari nonce index terkecil sehingga hash berawalan d buah '0'.
func solvePow(nonce string, sc *scheme) string {
	prefix := strings.Repeat("0", sc.D)
	rounds := sc.Rounds
	if rounds < 1 {
		rounds = 1
	}
	for i := 0; ; i++ {
		parts := make([]string, len(sc.Parts))
		for j, p := range sc.Parts {
			parts[j] = partValue(p, i, sc, nonce)
		}
		x := strings.Join(parts, sc.Sep)
		for r := 0; r < rounds; r++ {
			x = sha256hex(x)
		}
		if strings.HasPrefix(x, prefix) {
			return itoa(i)
		}
	}
}

// parseScheme men-decode segmen base64 pertama dari challenge lalu ambil .scheme.
func parseScheme(c string) *scheme {
	seg := strings.SplitN(c, ".", 2)[0]
	raw, err := base64.StdEncoding.DecodeString(seg)
	if err != nil {
		return nil
	}
	var wrap struct {
		Scheme *scheme `json:"scheme"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil
	}
	return wrap.Scheme
}

// --- Sesi/token ------------------------------------------------------------

var (
	tokenMu  sync.Mutex
	appToken string
)

type challengeResp struct {
	OK bool    `json:"ok"`
	C  string  `json:"c"`
	N  string  `json:"n"`
	D  *int    `json:"d"`
	Er *string `json:"error"`
}

type sessionResp struct {
	OK    bool   `json:"ok"`
	Token string `json:"token"`
}

// refreshToken menjalankan alur challenge→PoW→session dan menyimpan token baru.
func refreshToken(ctx context.Context) (string, error) {
	var ch challengeResp
	if err := httpx.GetJSON(ctx, baseURL()+"/api/challenge", requestTimeout, nil, &ch); err != nil {
		return "", err
	}
	if !ch.OK {
		return "", errors.New("challenge gagal")
	}

	sc := parseScheme(ch.C)
	if sc == nil && ch.D != nil {
		sc = &scheme{D: *ch.D, Parts: []string{"n", "i"}, Sep: "", Rounds: 1}
	}
	if sc == nil {
		return "", errors.New("scheme PoW tidak dikenali")
	}
	solution := solvePow(ch.N, sc)

	body, _ := json.Marshal(map[string]string{"challenge": ch.C, "solution": solution})
	resp, err := httpx.Do(ctx, "POST", baseURL()+"/api/session", strings.NewReader(string(body)),
		requestTimeout, map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var sess sessionResp
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		return "", err
	}
	if !sess.OK || sess.Token == "" {
		return "", errors.New("session gagal")
	}

	tokenMu.Lock()
	appToken = sess.Token
	tokenMu.Unlock()
	return sess.Token, nil
}

// apiJSON memanggil endpoint terproteksi dengan token; refresh sekali bila 403.
func apiJSON(ctx context.Context, path string, timeout time.Duration, out any) error {
	tokenMu.Lock()
	tok := appToken
	tokenMu.Unlock()
	if tok == "" {
		var err error
		if tok, err = refreshToken(ctx); err != nil {
			return err
		}
	}

	resp, err := httpx.Do(ctx, "GET", baseURL()+path, nil, timeout, map[string]string{"X-App-Token": tok})
	if err != nil {
		return err
	}
	if resp.StatusCode == 403 {
		resp.Body.Close()
		if tok, err = refreshToken(ctx); err != nil {
			return err
		}
		if resp, err = httpx.Do(ctx, "GET", baseURL()+path, nil, timeout, map[string]string{"X-App-Token": tok}); err != nil {
			return err
		}
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// --- Resolve per platform --------------------------------------------------

func isYoutubePlaylist(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if !strings.Contains(strings.ToLower(u.Hostname()), "youtube.com") {
		return false
	}
	q := u.Query()
	return q.Has("list") && !q.Has("v")
}

var reSpotifyPlaylist = regexp.MustCompile(`(?i)open\.spotify\.com/(playlist|album)/`)

type ytResp struct {
	OK     bool    `json:"ok"`
	URL    string  `json:"url"`
	Label  string  `json:"label"`
	Source string  `json:"source"`
	Ext    string  `json:"ext"`
	Error  *string `json:"error"`
}

type spotifyResp struct {
	OK     bool    `json:"ok"`
	URL    string  `json:"url"`
	Title  string  `json:"title"`
	Artist string  `json:"artist"`
	Source string  `json:"source"`
	Error  *string `json:"error"`
}

type genericMedia struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Ext   string `json:"ext"`
	Label string `json:"label"`
}

type genericResp struct {
	OK            bool           `json:"ok"`
	Title         string         `json:"title"`
	Author        string         `json:"author"`
	Source        string         `json:"source"`
	PlatformLabel string         `json:"platformLabel"`
	Medias        []genericMedia `json:"medias"`
	Error         *string        `json:"error"`
}

func errStr(p *string) error {
	if p != nil && *p != "" {
		return errors.New(*p)
	}
	return errors.New("gagal")
}

// Resolve mengembalikan media untuk link. arg opsional: untuk YouTube bisa
// "mp3"/"audio" atau angka kualitas (mengikuti download.js).
func Resolve(ctx context.Context, rawURL, platform, arg string) (*Result, error) {
	switch platform {
	case "youtube":
		if isYoutubePlaylist(rawURL) {
			return &Result{Playlist: true}, nil
		}
		kind, quality := "video", "720"
		a := strings.ToLower(strings.TrimSpace(arg))
		switch {
		case a == "mp3" || a == "audio":
			kind, quality = "audio", "128"
		case reDigitsOnly.MatchString(a):
			if a == "320" || a == "128" {
				kind, quality = "audio", a
			} else {
				quality = a
			}
		}
		var d ytResp
		path := "/api/youtube?url=" + url.QueryEscape(rawURL) + "&kind=" + kind + "&quality=" + quality
		if err := apiJSON(ctx, path, resolveTimeout(), &d); err != nil {
			return nil, err
		}
		if !d.OK || d.URL == "" {
			return nil, errStr(d.Error)
		}
		mtype := "video"
		if kind == "audio" {
			mtype = "audio"
		}
		title := d.Label
		if title == "" {
			title = "YouTube"
		}
		return &Result{Title: title, Source: d.Source, Medias: []Media{{Type: mtype, URL: d.URL, Ext: d.Ext}}}, nil

	case "spotify":
		if reSpotifyPlaylist.MatchString(rawURL) {
			return &Result{Playlist: true}, nil
		}
		var d spotifyResp
		if err := apiJSON(ctx, "/api/spotify?url="+url.QueryEscape(rawURL), resolveTimeout(), &d); err != nil {
			return nil, err
		}
		if !d.OK || d.URL == "" {
			return nil, errStr(d.Error)
		}
		title := strings.TrimSpace(strings.Join(nonEmpty(d.Artist, d.Title), " - "))
		if title == "" {
			title = "Spotify"
		}
		src := d.Source
		if src == "" {
			src = "spotify"
		}
		return &Result{Title: title, Source: src, Medias: []Media{{Type: "audio", URL: d.URL, Ext: "mp3"}}}, nil

	default:
		var d genericResp
		if err := apiJSON(ctx, "/api/download?url="+url.QueryEscape(rawURL), resolveTimeout(), &d); err != nil {
			return nil, err
		}
		if !d.OK || len(d.Medias) == 0 {
			return nil, errStr(d.Error)
		}
		title := strings.TrimSpace(strings.Join(nonEmpty(d.Author, d.Title), " - "))
		if title == "" {
			if d.PlatformLabel != "" {
				title = d.PlatformLabel
			} else {
				title = platform
			}
		}
		return &Result{Title: title, Source: d.Source, Medias: pickMedias(d.Medias)}, nil
	}
}

// pickMedias meniru pickMedias Node: utamakan semua image (maks 10), lalu satu
// video, lalu satu audio, jika tidak ambil satu pertama.
func pickMedias(medias []genericMedia) []Media {
	const maxMedias = 10
	var images []Media
	for _, m := range medias {
		if m.Type == "image" {
			images = append(images, toMedia(m))
		}
	}
	if len(images) > 0 {
		if len(images) > maxMedias {
			images = images[:maxMedias]
		}
		return images
	}
	for _, m := range medias {
		if m.Type == "video" {
			return []Media{toMedia(m)}
		}
	}
	for _, m := range medias {
		if m.Type == "audio" {
			return []Media{toMedia(m)}
		}
	}
	return []Media{toMedia(medias[0])}
}

func toMedia(m genericMedia) Media {
	return Media{Type: m.Type, URL: m.URL, Ext: m.Ext, Label: m.Label}
}

// nonEmpty mengembalikan hanya string non-kosong (port [a,b].filter(Boolean)).
func nonEmpty(ss ...string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// itoa lokal supaya file tak perlu impor strconv hanya untuk ini.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

