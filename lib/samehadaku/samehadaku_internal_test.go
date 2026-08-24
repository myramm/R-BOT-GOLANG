package samehadaku

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestAcefileID(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "format f dengan slug",
			url:  "https://acefile.co/f/33722679/360p-mkv-tonikawa-1-12end-batch-samehadaku-vip-rar",
			want: "33722679",
		},
		{
			name: "format f slug pendek",
			url:  "https://acefile.co/f/112136784/alqanime_synrlara_08_360p-mp4",
			want: "112136784",
		},
		{
			name: "format d",
			url:  "https://acefile.co/d/123456",
			want: "123456",
		},
		{
			name: "format file",
			url:  "https://acefile.co/file/99887766/some-file-rar",
			want: "99887766",
		},
		{
			name: "bukan acefile",
			url:  "https://pixeldrain.com/u/abc123",
			want: "",
		},
		{
			name: "acefile tanpa id numerik",
			url:  "https://acefile.co/home",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := acefileID(tc.url)
			if got != tc.want {
				t.Errorf("acefileID(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

func TestAcefileMirrorCodeDecode(t *testing.T) {
	// code dari get_mirrors = base64 standar berisi ID GDrive.
	cases := []struct {
		name   string
		code   string
		wantID string
	}{
		{
			name:   "code file direct-hosted (tonikawa s2 ep1)",
			code:   "MVpLS0lIdkl0QnF6UHN4cUY2MlRteGNTTUxodURFczVx",
			wantID: "1ZKKIHvItBqzPsxqF62TmxcSMLhuDEs5q",
		},
		{
			name:   "code mirror kedua",
			code:   "MUphYktGZENXTGJUbktPWXJRa2w3MXVqWC0yM1ZFMGw2",
			wantID: "1JabKFdCWLbTnKOYrQkl71ujX-23VE0l6",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, err := base64.StdEncoding.DecodeString(tc.code)
			if err != nil {
				t.Fatalf("decode gagal: %v", err)
			}
			gid := strings.TrimSpace(string(dec))
			if gid != tc.wantID {
				t.Errorf("decoded ID = %q, want %q", gid, tc.wantID)
			}
			if !reGDriveID.MatchString(gid) {
				t.Errorf("ID %q tidak valid sebagai GDrive ID", gid)
			}
		})
	}
}

func TestAcefileGDriveURL(t *testing.T) {
	got := acefileGDriveURL("1ZKKIHvItBqzPsxqF62TmxcSMLhuDEs5q")
	want := "https://drive.usercontent.google.com/download?id=1ZKKIHvItBqzPsxqF62TmxcSMLhuDEs5q&export=download&confirm=t"
	if got != want {
		t.Errorf("acefileGDriveURL() = %q, want %q", got, want)
	}
}
