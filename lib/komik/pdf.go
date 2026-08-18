package komik

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"time"

	"github.com/signintech/gopdf"
	_ "golang.org/x/image/webp"
)

const defaultUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"

// FetchImage mengunduh data gambar dari URL dengan retry.
func FetchImage(ctx context.Context, imgURL, referer string) ([]byte, error) {
	if referer == "" {
		referer = "https://komiktap.info/"
	}

	client := &http.Client{
		Timeout: 25 * time.Second,
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, imgURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", defaultUA)
		req.Header.Set("Referer", referer)

		resp, err := client.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				data, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err == nil && len(data) > 0 {
					return data, nil
				}
				lastErr = fmt.Errorf("baca data kosong / error: %v", err)
			} else {
				resp.Body.Close()
				lastErr = fmt.Errorf("HTTP status %d", resp.StatusCode)
			}
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}

	return nil, lastErr
}

// ImagesToPDF mengubah daftar URL gambar menjadi dokumen PDF.
// Mengembalikan (pdfBytes, validPages, totalPages, error).
func ImagesToPDF(ctx context.Context, imageURLs []string, referer string) ([]byte, int, int, error) {
	if len(imageURLs) == 0 {
		return nil, 0, 0, fmt.Errorf("tidak ada URL gambar untuk diproses")
	}

	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{})

	addedPages := 0
	total := len(imageURLs)

	for _, u := range imageURLs {
		select {
		case <-ctx.Done():
			return nil, addedPages, total, ctx.Err()
		default:
		}

		raw, err := FetchImage(ctx, u, referer)
		if err != nil {
			continue
		}

		// Decode image untuk memperoleh dimensi & format
		img, _, err := image.Decode(bytes.NewReader(raw))
		if err != nil {
			continue
		}

		bounds := img.Bounds()
		w := float64(bounds.Dx())
		h := float64(bounds.Dy())
		if w <= 0 || h <= 0 {
			continue
		}

		// Convert gambar ke JPG buffer agar gopdf dapat membacanya secara konsisten
		var jpegBuf bytes.Buffer
		if err := jpeg.Encode(&jpegBuf, img, &jpeg.Options{Quality: 85}); err != nil {
			continue
		}

		imgHolder, err := gopdf.ImageHolderByBytes(jpegBuf.Bytes())
		if err != nil {
			continue
		}

		pdf.AddPageWithOption(gopdf.PageOption{
			PageSize: &gopdf.Rect{W: w, H: h},
		})

		if err := pdf.ImageByHolder(imgHolder, 0, 0, &gopdf.Rect{W: w, H: h}); err != nil {
			continue
		}

		addedPages++
	}

	if addedPages == 0 {
		return nil, 0, total, fmt.Errorf("semua %d gambar gagal diunduh atau diproses", total)
	}

	var out bytes.Buffer
	if _, err := pdf.WriteTo(&out); err != nil {
		return nil, addedPages, total, fmt.Errorf("gagal merakit PDF: %w", err)
	}

	return out.Bytes(), addedPages, total, nil
}
