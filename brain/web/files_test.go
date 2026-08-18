package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSecurePath_ValidWorkspacePaths(t *testing.T) {
	root := getWorkspaceRoot()

	tests := []struct {
		name    string
		input   string
		wantEnd string
	}{
		{"Empty string", "", root},
		{"Dot", ".", root},
		{"Relative main.go", "main.go", "main.go"},
		{"Relative go.mod", "go.mod", "go.mod"},
		{"Subdirectory relative", "brain/web/server.go", "brain/web/server.go"},
		{"Absolute path in workspace", filepath.Join(root, "main.go"), "main.go"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveSecurePath(tt.input)
			if err != nil {
				t.Fatalf("resolveSecurePath(%q) gagal: %v", tt.input, err)
			}
			if !strings.HasSuffix(filepath.ToSlash(got), tt.wantEnd) && filepath.Clean(got) != filepath.Clean(tt.wantEnd) {
				t.Errorf("resolveSecurePath(%q) = %q; want end with %q", tt.input, got, tt.wantEnd)
			}
		})
	}
}

func TestResolveSecurePath_DenyOutsideWorkspace(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"Path traversal parent", "../../etc/passwd"},
		{"Path traversal root", "../../../../etc/passwd"},
		{"Absolute outside path", "/etc/passwd"},
		{"Absolute shadow path", "/var/log/syslog"},
		{"Null byte attack", "main.go\x00.png"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveSecurePath(tt.input)
			if err == nil {
				t.Errorf("resolveSecurePath(%q) harus ditolak, tapi berhasil!", tt.input)
			}
		})
	}
}

func TestResolveSecurePath_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("Home directory tidak ditemukan")
	}

	root := getWorkspaceRoot()
	rel, err := filepath.Rel(home, root)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Skip("Workspace tidak berada di bawah Home directory")
	}

	tildePath := "~/" + filepath.ToSlash(rel) + "/main.go"
	got, err := resolveSecurePath(tildePath)
	if err != nil {
		t.Fatalf("resolveSecurePath(%q) ditolak: %v", tildePath, err)
	}

	if filepath.Base(got) != "main.go" {
		t.Errorf("Ekspektasi main.go, hasil: %q", got)
	}
}
