package web

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
)

type terminalResizeMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	if !IsAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("[web] ws terminal accept error: %v", err)
		return
	}
	defer c.CloseNow()

	ctx := r.Context()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Printf("[web] terminal pty start error: %v", err)
		_ = c.Close(websocket.StatusInternalError, "Gagal memulai PTY shell")
		return
	}

	var closeOnce sync.Once
	cleanup := func() {
		closeOnce.Do(func() {
			_ = ptmx.Close()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		})
	}
	defer cleanup()

	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80})

	// PTY output -> WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				break
			}
			if n > 0 {
				writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := c.Write(writeCtx, websocket.MessageText, buf[:n])
				cancel()
				if err != nil {
					break
				}
			}
		}
		_ = c.Close(websocket.StatusNormalClosure, "Shell exited")
	}()

	// WebSocket input -> PTY
	for {
		msgType, data, err := c.Read(ctx)
		if err != nil {
			break
		}

		if msgType == websocket.MessageText || msgType == websocket.MessageBinary {
			var resize terminalResizeMsg
			if err := json.Unmarshal(data, &resize); err == nil && resize.Type == "resize" {
				if resize.Cols > 0 && resize.Rows > 0 {
					_ = pty.Setsize(ptmx, &pty.Winsize{
						Rows: uint16(resize.Rows),
						Cols: uint16(resize.Cols),
					})
				}
				continue
			}
			_, _ = ptmx.Write(data)
		}
	}
}
