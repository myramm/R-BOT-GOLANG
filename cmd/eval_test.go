package cmd

import (
	"context"
	"testing"
)

func TestGoEvalEngine(t *testing.T) {
	ctx := context.Background()

	// Test 1: Go math expression
	out1, err := runGoEvalCode(ctx, nil, "10 + 20")
	if err != nil {
		t.Fatalf("Test 1 error: %v", err)
	}
	t.Logf("Go Eval '10 + 20' => %s", out1)
	if out1 != "30" {
		t.Errorf("Expected '30', got %q", out1)
	}

	// Test 2: fmt.Println statement
	out2, err := runGoEvalCode(ctx, nil, `fmt.Println("Hello Go World!")`)
	if err != nil {
		t.Fatalf("Test 2 error: %v", err)
	}
	t.Logf("Go Eval fmt.Println => %s", out2)
	if out2 != "Hello Go World!" {
		t.Errorf("Expected 'Hello Go World!', got %q", out2)
	}

	// Test 3: Inline package main
	out3, err := runGoEvalCode(ctx, nil, `package main; import "fmt"; func main() { fmt.Println("Hello Inline!") }`)
	if err != nil {
		t.Fatalf("Test 3 error: %v", err)
	}
	t.Logf("Go Eval inline program => %s", out3)
	if out3 != "Hello Inline!" {
		t.Errorf("Expected 'Hello Inline!', got %q", out3)
	}
}
