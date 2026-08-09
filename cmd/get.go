package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"rbot/brain/command"
	"rbot/brain/config"
	"rbot/lib/httpx"
)

// get.go: ambil konten dari URL. Port get.js.
//   - HTML  → dikirim sebagai file .html mentah + preview teks.
//   - JSON  → di-pretty-print.
//   - lain  → teks inline bila pendek, selain itu file .txt.
// Guard SSRF: tolak host localhost/IP privat (meniru blokir di get.js).

const (
	getMaxTextLen = 4000             // batas aman teks WhatsApp
	getTimeout    = 20 * time.Second // sama seperti TIMEOUT_MS di get.js
	getMaxBytes   = 25 << 20         // batas unduh biar tak kehabisan memori
)

// reLocalHost meniru blokir URL lokal/privat di get.js (dicek pada hostname).
var reLocalHost = regexp.MustCompile(`^(localhost|127\.|192\.168\.|10\.|172\.(1[6-9]|2[0-9]|3[0-1])\.)`)

func init() {
	command.Register(&command.Command{
		Name:        "get",
		Category:    "Info",
		Alias:       []string{"fetch", "geturl"},
		Description: "Ambil konten dari URL. HTML dikirim sebagai file .html, konten lain jadi teks atau file .txt bila panjang. Contoh: .get https://example.com",
		Handler:     getHandler,
	})
}

func getHandler(ctx context.Context, c *command.Ctx) error {
	raw := ""
	if len(c.Args) > 0 {
		raw = strings.TrimSpace(c.Args[0])
	}
	if raw == "" {
		mp := config.MainPrefix()
		_, err := c.Reply(ctx, "🌐 *Perintah "+mp+"get*\n\n"+
			"Ambil konten dari URL. Halaman HTML dikirim sebagai file .html mentah, konten lain dikirim sebagai teks (atau file .txt kalau terlalu panjang)\n\n"+
			"Contoh:\n"+mp+"get https://example.com\n"+mp+"get https://api.github.com/users/myramm")
		return err
	}

	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		_, e := c.Reply(ctx, "❌ URL tidak valid. Pastikan dimulai dengan https:// atau http://")
		return e
	}
	if reLocalHost.MatchString(strings.ToLower(u.Hostname())) {
		_, e := c.Reply(ctx, "❌ Tidak bisa mengakses URL lokal/private.")
		return e
	}

	c.React(ctx, "⏳")

	resp, err := httpx.Do(ctx, http.MethodGet, raw, nil, getTimeout, map[string]string{
		"Accept": "text/html,application/json,text/plain,*/*",
	})
	if err != nil {
		c.React(ctx, "❌")
		reason := err.Error()
		if errors.Is(err, context.DeadlineExceeded) {
			reason = "timeout (20 detik)"
		}
		_, e := c.Reply(ctx, "❌ Gagal mengambil konten: "+reason)
		return e
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, fmt.Sprintf("❌ Gagal fetch URL: HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode)))
		return e
	}

	contentType := resp.Header.Get("Content-Type")
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, getMaxBytes))
	if err != nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, "❌ Gagal mengambil konten: "+err.Error())
		return e
	}
	body := string(bodyBytes)
	domain := strings.ReplaceAll(u.Hostname(), ".", "_")

	// HTML selalu dikirim sebagai file .html mentah.
	if strings.Contains(strings.ToLower(contentType), "html") {
		if strings.TrimSpace(body) == "" {
			c.React(ctx, "❌")
			_, e := c.Reply(ctx, "❌ Konten kosong atau tidak bisa dibaca.")
			return e
		}
		caption := fmt.Sprintf("🌐 *GET* %s\n📄 text/html · %d karakter\n%s\n\n", raw, len(body), strings.Repeat("─", 20))
		if preview := truncRunes(htmlToText(body), 300); preview != "" {
			caption += "_Preview:_\n" + preview + "…"
		}
		if err := c.SendMediaBytes(ctx, bodyBytes, command.MediaDocument, caption, domain+".html", "text/html"); err != nil {
			c.React(ctx, "❌")
			_, e := c.Reply(ctx, "❌ Gagal membuat file: "+err.Error())
			return e
		}
		c.React(ctx, "✅")
		return nil
	}

	result := body
	if strings.Contains(strings.ToLower(contentType), "json") {
		var v any
		if json.Unmarshal(bodyBytes, &v) == nil {
			if pretty, e := json.MarshalIndent(v, "", "  "); e == nil {
				result = string(pretty)
			}
		}
	}
	if strings.TrimSpace(result) == "" {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, "❌ Konten kosong atau tidak bisa dibaca.")
		return e
	}

	ctLabel := strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	if ctLabel == "" {
		ctLabel = "text"
	}
	header := fmt.Sprintf("🌐 *GET* %s\n📄 %s · %d karakter\n%s\n\n", raw, ctLabel, len(result), strings.Repeat("─", 20))

	// Cukup pendek → kirim teks langsung.
	if len(header)+len(result) <= getMaxTextLen {
		c.React(ctx, "✅")
		_, e := c.Reply(ctx, header+result)
		return e
	}

	// Terlalu panjang → kirim sebagai file .txt.
	fileContent := "GET " + raw + "\n" + contentType + "\n" + strings.Repeat("=", 40) + "\n\n" + result
	if err := c.SendMediaBytes(ctx, []byte(fileContent), command.MediaDocument, header+"_Preview:_\n"+truncRunes(result, 300)+"…", domain+".txt", "text/plain"); err != nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, "❌ Gagal membuat file: "+err.Error())
		return e
	}
	c.React(ctx, "✅")
	return nil
}

// htmlToText menyederhanakan HTML jadi teks polos untuk preview (port htmlToText).
var (
	reScript   = regexp.MustCompile(`(?is)<script.*?</script>`)
	reStyle    = regexp.MustCompile(`(?is)<style.*?</style>`)
	reBr       = regexp.MustCompile(`(?i)<br\s*/?>`)
	reCloseP   = regexp.MustCompile(`(?i)</p>`)
	reCloseDiv = regexp.MustCompile(`(?i)</div>`)
	reCloseLi  = regexp.MustCompile(`(?i)</li>`)
	reOpenLi   = regexp.MustCompile(`(?i)<li[^>]*>`)
	reCloseH   = regexp.MustCompile(`(?i)</h[1-6]>`)
	reTag      = regexp.MustCompile(`<[^>]+>`)
	reSpaceTab = regexp.MustCompile(`[ \t]+`)
	reNL3      = regexp.MustCompile(`\n{3,}`)

	htmlEntities = strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", "\"", "&#39;", "'", "&nbsp;", " ",
	)
)

func htmlToText(html string) string {
	s := reScript.ReplaceAllString(html, "")
	s = reStyle.ReplaceAllString(s, "")
	s = reBr.ReplaceAllString(s, "\n")
	s = reCloseP.ReplaceAllString(s, "\n\n")
	s = reCloseDiv.ReplaceAllString(s, "\n")
	s = reCloseLi.ReplaceAllString(s, "\n")
	s = reOpenLi.ReplaceAllString(s, "• ")
	s = reCloseH.ReplaceAllString(s, "\n\n")
	s = reTag.ReplaceAllString(s, "")
	s = htmlEntities.Replace(s)
	s = reSpaceTab.ReplaceAllString(s, " ")
	s = reNL3.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// truncRunes memotong string ke maksimal n rune (aman untuk UTF-8, tak memotong
// di tengah karakter). Dipakai bersama oleh get & exec (paket cmd).
func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
