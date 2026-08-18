package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"

	"rbot/brain/command"
)

func init() {
	command.Register(&command.Command{
		Name:        ">",
		Category:    "Owner",
		Alias:       []string{"c", "eval", "e", "go"},
		Description: "Evaluasi & eksekusi kode Golang di server (owner). Contoh: > 1 + 1 | > fmt.Println(\"Halo\") | > package main...",
		OwnerOnly:   true,
		Handler:     evalHandler,
	})
}

func evalHandler(ctx context.Context, c *command.Ctx) error {
	code := strings.TrimSpace(c.ArgStr())
	if code == "" {
		_, err := c.Reply(ctx, "Masukkan kode Golang yang ingin dievaluasi.\n\n*Contoh:*\n• `> 1 + 1`\n• `> fmt.Println(\"Halo Golang\")`\n• `.eval package main; import \"fmt\"; func main() { fmt.Println(\"Halo!\") }`")
		return err
	}

	c.React(ctx, "⚙️")

	output, err := runGoEvalCode(ctx, c, code)
	if err != nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, fmt.Sprintf("❌ *Golang Eval Error:*\n```%s```", err.Error()))
		return e
	}

	if output == "" {
		output = "(tanpa output)"
	}

	c.React(ctx, "✅")
	output = truncRunes(output, 4000)
	_, e := c.Reply(ctx, fmt.Sprintf("```%s```", output))
	return e
}

func runGoEvalCode(ctx context.Context, c *command.Ctx, code string) (string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "(kosong)", nil
	}

	// 1. Jika kode berisi "package main" atau "func main", kompilasi & jalankan via `go run`
	if strings.Contains(code, "package main") || strings.Contains(code, "func main()") {
		tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("eval_%d.go", time.Now().UnixNano()))
		if !strings.Contains(code, "package main") {
			code = "package main\n" + code
		}
		if err := os.WriteFile(tmpFile, []byte(code), 0644); err != nil {
			return "", err
		}
		defer os.Remove(tmpFile)

		runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()

		cmd := exec.CommandContext(runCtx, "go", "run", tmpFile)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("Go Compile Error: %s", string(out))
		}
		return strings.TrimSpace(string(out)), nil
	}

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
	bodyCode := strings.TrimSpace(strings.Join(codeLines, "\n"))

	// 2. Jika kode berisi statement output (fmt.Print / log.Print), jalankan via `go run`
	if strings.Contains(bodyCode, "fmt.Print") || strings.Contains(bodyCode, "log.Print") {
		imports := "import \"fmt\"\n"
		if len(importLines) > 0 {
			imports = strings.Join(importLines, "\n") + "\n"
		}
		fullGo := fmt.Sprintf("package main\n%s\nfunc main() {\n\t%s\n}", imports, bodyCode)
		tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("eval_%d.go", time.Now().UnixNano()))
		if e := os.WriteFile(tmpFile, []byte(fullGo), 0644); e == nil {
			defer os.Remove(tmpFile)
			runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(runCtx, "go", "run", tmpFile)
			out, e2 := cmd.CombinedOutput()
			if e2 == nil {
				return strings.TrimSpace(string(out)), nil
			}
			return "", fmt.Errorf("Go Compile Error: %s", string(out))
		}
	}

	// 3. Evaluasi ekspresi Golang dinamis via Yaegi Interpreter
	i := interp.New(interp.Options{})
	_ = i.Use(stdlib.Symbols)

	// Pre-import paket umum Golang standar
	_, _ = i.Eval(`import "fmt"`)
	_, _ = i.Eval(`import "strings"`)
	_, _ = i.Eval(`import "time"`)
	_, _ = i.Eval(`import "math"`)

	for _, imp := range importLines {
		_, _ = i.Eval(imp)
	}

	if bodyCode == "" {
		return "(imported)", nil
	}

	res, err := i.Eval(bodyCode)
	if err != nil {
		// Fallback: Bungkus ke dalam package main dan jalankan dengan `go run`
		imports := "import \"fmt\"\n"
		if len(importLines) > 0 {
			imports = strings.Join(importLines, "\n") + "\n"
		}
		fullGo := fmt.Sprintf("package main\n%s\nfunc main() {\n\tfmt.Println(%s)\n}", imports, bodyCode)

		tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("eval_%d.go", time.Now().UnixNano()))
		if e := os.WriteFile(tmpFile, []byte(fullGo), 0644); e == nil {
			defer os.Remove(tmpFile)
			runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(runCtx, "go", "run", tmpFile)
			out, e2 := cmd.CombinedOutput()
			if e2 == nil {
				return strings.TrimSpace(string(out)), nil
			}
		}
		return "", fmt.Errorf("Golang Eval Error: %v", err)
	}

	if !res.IsValid() {
		return "(nil)", nil
	}

	return fmt.Sprintf("%v", res.Interface()), nil
}
