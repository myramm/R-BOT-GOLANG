package cmd

import (
	"context"
	"testing"

	"github.com/robertkrimen/otto"

	"rbot/brain/command"
)

func TestOttoEval(t *testing.T) {
	vm := otto.New()
	_ = vm.Set("a", 10)
	_ = vm.Set("b", 20)

	val, err := vm.Run("a + b")
	if err != nil {
		t.Fatalf("Otto Run error: %v", err)
	}

	result, _ := val.ToInteger()
	if result != 30 {
		t.Errorf("Expected 30, got %d", result)
	}

	t.Logf("Otto JS eval result for '10 + 20': %d", result)
}

func TestDirectOwnerTriggers(t *testing.T) {
	cmdEval := command.Resolve(">")
	if cmdEval == nil {
		t.Fatalf("Expected '>' command to be registered")
	}

	cmdExec := command.Resolve("#")
	if cmdExec == nil {
		t.Fatalf("Expected '#' command to be registered")
	}

	ctx := context.Background()
	c := &command.Ctx{
		Args:      []string{"1", "+", "1"},
		Text:      "> 1 + 1",
		InvokedAs: ">",
	}

	if c.ArgStr() != "1 + 1" {
		t.Errorf("Expected ArgStr() '1 + 1', got %q", c.ArgStr())
	}

	_ = ctx
}
