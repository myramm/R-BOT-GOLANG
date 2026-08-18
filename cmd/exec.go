package cmd

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"

	"rbot/brain/command"
	"rbot/brain/config"
)

// exec.go: command "#" / "$" / "exec" — jalankan perintah shell di server buat debug (owner).
// Timeout 20 detik, output tiap stream dibatasi (meniru maxBuffer),
// hasilnya dipangkas 4000 karakter lalu dibungkus code fence.

const (
	execTimeout   = 20 * time.Second
	execMaxBuffer = 1 << 20 // 1 MB per stream
	execMaxOutput = 4000
)

func init() {
	command.Register(&command.Command{
		Name:        "#",
		Category:    "Owner",
		Alias:       []string{"$", "exec"},
		Description: "Jalankan perintah shell di server buat debug (owner). Contoh: .# ls -la | .$ uptime | .exec whoami",
		OwnerOnly:   true,
		Handler:     execHandler,
	})
}

func execHandler(ctx context.Context, c *command.Ctx) error {
	cmdStr := strings.TrimSpace(c.ArgStr())
	if cmdStr == "" {
		_, err := c.Reply(ctx, "Contoh: "+config.MainPrefix()+"# ls -la")
		return err
	}

	runCtx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-c", cmdStr)
	var stdout, stderr capWriter
	stdout.cap, stderr.cap = execMaxBuffer, execMaxBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	var parts []string
	if s := stdout.String(); s != "" {
		parts = append(parts, s)
	}
	if s := stderr.String(); s != "" {
		parts = append(parts, s)
	}
	if runErr != nil && stdout.buf.Len() == 0 && stderr.buf.Len() == 0 {
		reason := runErr.Error()
		if runCtx.Err() == context.DeadlineExceeded {
			reason = "timeout (20 detik)"
		}
		parts = append(parts, "[error] "+reason)
	}

	output := strings.TrimSpace(strings.Join(parts, "\n"))
	if output == "" {
		output = "(tidak ada output)"
	}
	output = strings.ToValidUTF8(truncRunes(output, execMaxOutput), "")

	_, err := c.Reply(ctx, "```"+output+"```")
	return err
}

// capWriter menampung maksimal cap byte lalu membuang sisanya.
type capWriter struct {
	buf bytes.Buffer
	cap int
}

func (w *capWriter) Write(p []byte) (int, error) {
	if remain := w.cap - w.buf.Len(); remain > 0 {
		if len(p) > remain {
			w.buf.Write(p[:remain])
		} else {
			w.buf.Write(p)
		}
	}
	return len(p), nil
}

func (w *capWriter) String() string { return w.buf.String() }
