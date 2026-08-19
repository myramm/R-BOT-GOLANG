// Package httpx adalah helper HTTP bersama untuk scraper di lib/* — setara
// fetchTimeout di bot Node lama: pasang User-Agent browser + timeout, lalu sediakan
// pembantu ambil JSON, byte mentah, dan ukuran (HEAD). Dipakai lib/ytsearch & lib/kamino.
package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// UA meniru header browser desktop supaya endpoint tidak menolak permintaan bot.
const UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"

// client dipakai bersama; timeout per-permintaan diatur lewat context, bukan di sini.
var client = &http.Client{}

// Do menjalankan permintaan dengan timeout & UA default. Header tambahan opsional.
// Pemanggil WAJIB menutup resp.Body.
func Do(ctx context.Context, method, url string, body io.Reader, timeout time.Duration, headers map[string]string) (*http.Response, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	req, err := http.NewRequestWithContext(cctx, method, url, body)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("User-Agent", UA)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	// Bungkus Body agar cancel terpanggil saat Body ditutup (hindari bocor context).
	resp.Body = &cancelBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

// GetJSON mengambil URL lalu men-decode body JSON ke out. Header opsional.
func GetJSON(ctx context.Context, url string, timeout time.Duration, headers map[string]string, out any) error {
	resp, err := Do(ctx, http.MethodGet, url, nil, timeout, headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP error %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// GetBytes mengunduh URL jadi byte, dibatasi maxBytes (0 = tanpa batas). Bila
// melebihi batas, mengembalikan error tanpa memuat sisanya.
func GetBytes(ctx context.Context, url string, timeout time.Duration, maxBytes int64) ([]byte, error) {
	resp, err := Do(ctx, http.MethodGet, url, nil, timeout, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var r io.Reader = resp.Body
	if maxBytes > 0 {
		r = io.LimitReader(resp.Body, maxBytes+1)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file melebihi batas %d byte", maxBytes)
	}
	return data, nil
}

// HeadSize mengembalikan Content-Length via HEAD, atau (-1, nil) bila tak diketahui.
func HeadSize(ctx context.Context, url string, timeout time.Duration) (int64, error) {
	resp, err := Do(ctx, http.MethodHead, url, nil, timeout, nil)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()
	cl := resp.Header.Get("Content-Length")
	if cl == "" {
		return -1, nil
	}
	n, err := strconv.ParseInt(cl, 10, 64)
	if err != nil {
		return -1, nil
	}
	return n, nil
}
