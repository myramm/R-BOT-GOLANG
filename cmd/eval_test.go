package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

func runGoEval(code string) (string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "(kosong)", nil
	}

	// 1. Jika kode berisi "package main" atau "func main", jalankan lewat `go run`
	if strings.Contains(code, "package main") || strings.Contains(code, "func main()") {
		tmpDir := os.TempDir()
		tmpFile := filepath.Join(tmpDir, fmt.Sprintf("eval_%d.go", time.Now().UnixNano()))
		if !strings.HasPrefix(code, "package main") {
			code = "package main\n" + code
		}
		if err := os.WriteFile(tmpFile, []byte(code), 0644); err != nil {
			return "", err
		}
		defer os.Remove(tmpFile)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "go", "run", tmpFile)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("%v: %s", err, string(out))
		}
		return strings.TrimSpace(string(out)), nil
	}

	// 2. Evaluasi ekspresi/kode Go via Yaegi Interpreter
	i := interp.New(interp.Options{})
	_ = i.Use(stdlib.Symbols)

	// Jika ada import, evaluasi import dulu
	lines := strings.Split(code, "\n")
	var importLines []string
	var codeLines []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "import ") {
			importLines = append(importLines, trimmed)
		} else {
			codeLines = append(codeLines, l)
		}
	}

	for _, imp := range importLines {
		_, _ = i.Eval(imp)
	}

	bodyCode := strings.TrimSpace(strings.Join(codeLines, "\n"))
	if bodyCode == "" {
		return "(imported)", nil
	}

	res, err := i.Eval(bodyCode)
	if err != nil {
		// Fallback: bungkus ke dalam package main dan jalankan dengan go run
		fullGo := fmt.Sprintf("package main\nimport \"fmt\"\nfunc main() {\n\tfmt.Println(%s)\n}", code)
		tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("eval_%d.go", time.Now().UnixNano()))
		if e := os.WriteFile(tmpFile, []byte(fullGo), 0644); e == nil {
			defer os.Remove(tmpFile)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "go", "run", tmpFile)
			if out, e2 := cmd.CombinedOutput(); e2 == nil {
				return strings.TrimSpace(string(out)), nil
			}
		}
		return "", err
	}

	if !res.IsValid() {
		return "(nil)", nil
	}
	return fmt.Sprintf("%v", res.Interface()), nil
}

func TestGoEvalEngine(t *testing.T) {
	// Test 1: Go math expression
	out1, err := runGoEval("10 + 20")
	if err != nil {
		t.Fatalf("Test 1 error: %v", err)
	}
	t.Logf("Go Eval '10 + 20' => %s", out1)
	if out1 != "30" {
		t.Errorf("Expected '30', got %q", out1)
	}

	// Test 2: Full Go program with go run fallback
	out2, err := runGoEval(`package main
import "fmt"
func main() {
    fmt.Println("Halo dari Golang!")
}`)
	if err != nil {
		t.Fatalf("Test 2 error: %v", err)
	}
	t.Logf("Go Eval program => %s", out2)
	if out2 != "Halo dari Golang!" {
		t.Errorf("Expected 'Halo dari Golang!', got %q", out2)
	}
}
