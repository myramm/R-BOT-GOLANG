package cmd

import (
	"testing"

	"github.com/robertkrimen/otto"
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
