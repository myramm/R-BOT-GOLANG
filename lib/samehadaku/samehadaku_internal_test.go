package samehadaku

import "testing"

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
