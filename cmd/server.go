package cmd

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"rbot/brain/command"
	"rbot/brain/config"
	cmdfunc "rbot/cmd/func"
)

func init() {
	command.Register(&command.Command{
		Name:        "server",
		Category:    "Info",
		Alias:       []string{"info"},
		Description: "Menampilkan informasi server dan runtime bot",
		Handler:     serverHandler,
	})
}

func serverHandler(ctx context.Context, c *command.Ctx) error {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	botName := strings.TrimSpace(config.C.BotName)
	if botName == "" {
		botName = "R-BOT"
	}

	text := fmt.Sprintf(
		"🖥️ *SERVER INFO*\n\n"+
			"🤖 *Bot:* %s\n"+
			"⏱️ *Uptime:* %s\n"+
			"💻 *OS:* %s/%s\n"+
			"🐹 *Go:* %s\n"+
			"🧠 *CPU:* %d core\n"+
			"🔁 *Goroutine:* %d\n"+
			"💾 *Memory:* %s digunakan / %s sistem\n"+
			"🧩 *Command:* %d",
		botName,
		cmdfunc.FormatUptime(time.Since(cmdfunc.StartTime)),
		runtime.GOOS,
		runtime.GOARCH,
		runtime.Version(),
		runtime.NumCPU(),
		runtime.NumGoroutine(),
		formatServerBytes(mem.Alloc),
		formatServerBytes(mem.Sys),
		command.Count(),
	)
	_, err := c.Reply(ctx, text)
	return err
}

func formatServerBytes(size uint64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	units := []string{"KB", "MB", "GB", "TB"}
	for _, name := range units {
		value /= unit
		if value < unit || name == units[len(units)-1] {
			return fmt.Sprintf("%.1f %s", value, name)
		}
	}
	return fmt.Sprintf("%.1f TB", value)
}
