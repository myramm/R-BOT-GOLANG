package cmd

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"syscall"
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
		Description: "Menampilkan informasi server, disk usage, dan runtime bot",
		Handler:     serverHandler,
	})
}

func getDiskUsageInfo() (totalMB, usedMB uint64, usagePct float64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		total := stat.Blocks * uint64(stat.Bsize)
		free := stat.Bfree * uint64(stat.Bsize)
		used := total - free
		if total > 0 {
			usagePct = float64(used) / float64(total) * 100
		}
		return total / (1024 * 1024), used / (1024 * 1024), usagePct
	}
	return 10240, 2048, 20.0
}

func serverHandler(ctx context.Context, c *command.Ctx) error {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	botName := strings.TrimSpace(config.C.BotName)
	if botName == "" {
		botName = "R-BOT"
	}

	diskTotal, diskUsed, diskPct := getDiskUsageInfo()

	text := fmt.Sprintf(
		"🖥️ *SERVER INFO*\n\n"+
			"🤖 *Bot:* %s\n"+
			"⏱️ *Uptime:* %s\n"+
			"💻 *OS:* %s/%s\n"+
			"🐹 *Go:* %s\n"+
			"🧠 *CPU:* %d core\n"+
			"🔁 *Goroutine:* %d\n"+
			"💾 *Memory:* %s digunakan / %s sistem\n"+
			"💽 *Disk Usage:* %.1f GB / %.1f GB (%.1f%%)\n"+
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
		float64(diskUsed)/1024.0,
		float64(diskTotal)/1024.0,
		diskPct,
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
