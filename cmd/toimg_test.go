package cmd

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestILovePDFExtractImages(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, name := range []string{"page-002.jpg", "page-001.jpg", "ignore.txt", "nested/page-003.jpg"} {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(append([]byte{0xff, 0xd8, 0xff}, []byte("fake-"+name)...)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	pages, err := ilovePDFExtractImages(archive.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("pages = %d, want 2", len(pages))
	}
	if string(pages[0]) != "\xff\xd8\xfffake-page-001.jpg" || string(pages[1]) != "\xff\xd8\xfffake-page-002.jpg" {
		t.Fatalf("page ordering/content incorrect: %q, %q", pages[0], pages[1])
	}
}

func TestILovePDFExtractImagesRejectsInvalidArchive(t *testing.T) {
	if _, err := ilovePDFExtractImages([]byte("not a zip")); err == nil {
		t.Fatal("invalid archive harus error")
	}
}
