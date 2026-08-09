package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"rbot/brain/command"
	"rbot/lib/httpx"
)

// proxy.go: ambil daftar proxy gratis aktif dari Geonode. Port proxy.js +
// commands/proxy.js. Node mengirim tombol interaktif; di sini output teks biasa
// (whatsmeow tak punya dukungan tombol yang setara). Filter protokol/negara
// diambil dari argumen pertama, sama seperti Node.

const (
	proxyAPI     = "https://proxylist.geonode.com/api/proxy-list"
	proxyLimit   = 10
	proxyTimeout = 15 * time.Second
)

// proxyProtocols meniru whitelist protokol di commands/proxy.js.
var proxyProtocols = map[string]bool{"http": true, "https": true, "socks4": true, "socks5": true}

func init() {
	command.Register(&command.Command{
		Name:        "proxy",
		Category:    "Info",
		Alias:       []string{"proxies", "geonode", "proxylist"},
		Description: "Ambil daftar proxy gratis aktif dari Geonode (HTTP/HTTPS/SOCKS). Contoh: .proxy • .proxy socks5 • .proxy id",
		Handler:     proxyHandler,
	})
}

// geonodeResp memetakan bentuk respons Geonode (field port berupa string).
type geonodeResp struct {
	Data []struct {
		IP             string   `json:"ip"`
		Port           string   `json:"port"`
		Protocols      []string `json:"protocols"`
		Country        string   `json:"country"`
		City           string   `json:"city"`
		AnonymityLevel string   `json:"anonymityLevel"`
		Latency        float64  `json:"latency"`
	} `json:"data"`
}

func proxyHandler(ctx context.Context, c *command.Ctx) error {
	input := ""
	if len(c.Args) > 0 {
		input = strings.ToLower(strings.TrimSpace(c.Args[0]))
	}

	// Argumen pertama: nama protokol → filter protokol; 2 huruf → filter negara.
	protocols := []string{"http", "https"}
	country := ""
	switch {
	case proxyProtocols[input]:
		protocols = []string{input}
	case len(input) == 2:
		country = strings.ToUpper(input)
	}

	q := url.Values{}
	q.Set("limit", fmt.Sprintf("%d", proxyLimit))
	q.Set("page", "1")
	q.Set("sort_by", "lastChecked")
	q.Set("sort_type", "desc")
	q.Set("protocols", strings.Join(protocols, ","))
	if country != "" {
		q.Set("country", country)
	}
	reqURL := proxyAPI + "?" + q.Encode()

	c.React(ctx, "🌐")

	resp, err := httpx.Do(ctx, http.MethodGet, reqURL, nil, proxyTimeout, map[string]string{
		"Accept": "application/json",
	})
	if err != nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, "❌ Gagal mengambil proxy dari Geonode: "+err.Error())
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, fmt.Sprintf("❌ Gagal mengambil proxy dari Geonode: HTTP %d", resp.StatusCode))
		return e
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, "❌ Gagal mengambil proxy dari Geonode: "+err.Error())
		return e
	}

	var parsed geonodeResp
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.Data) == 0 {
		c.React(ctx, "🤷")
		_, e := c.Reply(ctx, fmt.Sprintf("❌ Tidak ditemukan proxy yang cocok untuk kriteria %q.", input))
		return e
	}

	var b strings.Builder
	fmt.Fprintf(&b, "🌐 *GEONODE FREE PROXY LIST*\n\nDitemukan *%d* proxy aktif dari Geonode:\n\n", len(parsed.Data))
	for i, p := range parsed.Data {
		proto := strings.ToUpper(strings.Join(p.Protocols, "/"))
		city := ""
		if p.City != "" {
			city = " (" + p.City + ")"
		}
		latency := "Fast"
		if p.Latency > 0 {
			latency = fmt.Sprintf("%.0fms", p.Latency)
		}
		fmt.Fprintf(&b, "%d. *%s:%s*\n   • Protokol: %s\n   • Negara: %s%s\n   • Anonymity: %s\n   • Latensi: %s\n\n",
			i+1, p.IP, p.Port, proto, p.Country, city, p.AnonymityLevel, latency)
	}
	b.WriteString("_Sumber: https://geonode.com/free-proxy-list_")

	c.React(ctx, "✅")
	_, e := c.Reply(ctx, b.String())
	return e
}
