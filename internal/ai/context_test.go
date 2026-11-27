package ai

import "testing"

func TestAssemblerKeepsNewestWithinBudget(t *testing.T) {
	a := Assembler{MaxChars: 200, MaxToolChars: 50}
	history := []Message{
		{Role: "user", Content: "old question " + string(make([]byte, 80))},
		{Role: "assistant", Content: "old answer " + string(make([]byte, 80))},
		{Role: "user", Content: "newest"},
	}
	out := a.Assemble("system", history)
	if out[0].Role != "system" {
		t.Fatal("system missing")
	}
	if out[len(out)-1].Content != "newest" {
		t.Fatalf("last = %q", out[len(out)-1].Content)
	}
	if len(out) >= 4 {
		t.Fatalf("budget ignored: %d messages", len(out))
	}
}

func TestAssemblerTruncatesToolOutput(t *testing.T) {
	a := Assembler{MaxChars: 1000, MaxToolChars: 10}
	out := a.Assemble("s", []Message{{Role: "tool", Content: "0123456789abcdef"}})
	if out[1].Content != "0123456789\n…[truncated]" {
		t.Fatalf("tool content = %q", out[1].Content)
	}
}
