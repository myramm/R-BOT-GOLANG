package nhentai

import (
	"strings"
	"testing"
)

const fixtureHTML = `<!DOCTYPE html><html><head><title>x</title></head><body>` +
	`<script id="__NEXT_DATA__" type="application/json">` +
	`{"props":{"pageProps":{"data":{"title":{"english":"[Author] Some Title (Group)","japanese":"[作者] タイトル"},"media_id":987560,"images":{"pages":[{"t":"https://a.kontol.online/api/imageV2/i/987560/1.jpg","w":1275,"h":1844},{"t":"https://a.kontol.online/api/imageV2/i/987560/2.png","w":1275,"h":1844},{"t":"https://a.kontol.online/api/imageV2/i/987560/3.gif","w":1275,"h":1844}]}}}}}` +
	`</script></body></html>`

func TestFetchParse(t *testing.T) {
	g, err := parse(fixtureHTML)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if g.MediaID != 987560 {
		t.Errorf("MediaID = %d, want 987560", g.MediaID)
	}
	if !strings.Contains(g.Title, "Some Title") {
		t.Errorf("Title = %q, harus memuat judul english", g.Title)
	}
	if len(g.Ext) != 3 || g.Ext[0] != "jpg" || g.Ext[1] != "png" || g.Ext[2] != "gif" {
		t.Errorf("Ext = %v, want [jpg png gif]", g.Ext)
	}
}

func TestFetchParseTanpaNEXTData(t *testing.T) {
	if _, err := parse("<html><body>kosong</body></html>"); err == nil {
		t.Fatal("parse harus gagal bila __NEXT_DATA__ tidak ada")
	}
}

func TestFetchParseMediaIDString(t *testing.T) {
	// Respons asli cin.guru mengirim media_id sebagai string.
	html := `<html><script id="__NEXT_DATA__" type="application/json">` +
		`{"props":{"pageProps":{"data":{"title":{"english":"X"},"media_id":"987560","images":{"pages":[{"t":"https://a.b/1.jpg"}]}}}}}` +
		`</script></html>`
	g, err := parse(html)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if g.MediaID != 987560 {
		t.Errorf("MediaID = %d, want 987560", g.MediaID)
	}
}

func TestImageURLs(t *testing.T) {
	g := &Gallery{MediaID: 987560, Ext: []string{"jpg", "png"}}
	urls := g.ImageURLs()
	if len(urls) != 2 {
		t.Fatalf("ImageURLs = %d url, want 2", len(urls))
	}
	for i, u := range urls {
		if !strings.HasPrefix(u, ddgProxy) {
			t.Errorf("url %d tidak lewat proxy DDG: %s", i, u)
		}
		if !strings.Contains(u, "i.nhentai.net/galleries/987560/") {
			t.Errorf("url %d salah host/galleries: %s", i, u)
		}
	}
	if !strings.HasSuffix(urls[0], "1.jpg") || !strings.HasSuffix(urls[1], "2.png") {
		t.Errorf("ext halaman salah: %v", urls)
	}
}

func TestThumbURL(t *testing.T) {
	g := &Gallery{MediaID: 987560}
	if !strings.Contains(g.ThumbURL(), "t.nhentai.net/galleries/987560/thumb.jpg") {
		t.Errorf("ThumbURL salah: %s", g.ThumbURL())
	}
}

func TestFileName(t *testing.T) {
	got := FileName(`[Author] Some: "Title"? (Group)`)
	if got != "Author_Some_Title_Group.pdf" {
		t.Errorf("FileName = %q", got)
	}
	if FileName("!!!") != "nhentai.pdf" {
		t.Error("FileName kosong harus fallback nhentai.pdf")
	}
}
