package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/robertkrimen/otto"

	"rbot/brain/command"
	"rbot/brain/config"
)

func init() {
	command.Register(&command.Command{
		Name:        ">",
		Category:    "Owner",
		Alias:       []string{"c", "eval", "e"},
		Description: "Evaluasi kode JavaScript di server (owner). Contoh: > 1 + 1 | .eval JSON.stringify(config)",
		OwnerOnly:   true,
		Handler:     evalHandler,
	})
}

func evalHandler(ctx context.Context, c *command.Ctx) error {
	code := strings.TrimSpace(c.ArgStr())
	if code == "" {
		_, err := c.Reply(ctx, "Masukkan kode JavaScript yang ingin dievaluasi.\n\n*Contoh:*\n• `> 1 + 1`\n• `.eval JSON.stringify(config)`")
		return err
	}

	vm := otto.New()

	// Interrupter timeout 10s untuk mencegah infinite loop
	vm.Interrupt = make(chan func(), 1)
	timer := time.AfterFunc(10*time.Second, func() {
		vm.Interrupt <- func() {
			panic("eval timeout (10 detik)")
		}
	})
	defer timer.Stop()

	// Inject context & helper ke VM Otto
	_ = vm.Set("c", c)
	_ = vm.Set("sender", c.Sender().String())
	_ = vm.Set("chat", c.Chat().String())
	_ = vm.Set("isOwner", command.IsOwner(c.Evt))
	_ = vm.Set("config", map[string]interface{}{
		"botName":     config.C.BotName,
		"ownerNumber": config.C.OwnerNumber,
		"mainPrefix":  config.MainPrefix(),
		"goVersion":   runtime.Version(),
	})

	val, err := vm.Run(code)
	if err != nil {
		_, e := c.Reply(ctx, fmt.Sprintf("❌ *Eval Error:*\n```%s```", err.Error()))
		return e
	}

	output := val.String()
	if val.IsObject() {
		export, _ := val.Export()
		if b, err := json.MarshalIndent(export, "", "  "); err == nil {
			output = string(b)
		}
	}

	if output == "" {
		output = "(undefined)"
	}

	output = truncRunes(output, 4000)
	_, e := c.Reply(ctx, fmt.Sprintf("```%s```", output))
	return e
}
