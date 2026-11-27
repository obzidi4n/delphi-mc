package ai

import "strings"

// SystemPrompt is the operator-assistant contract.
const SystemPrompt = `You are DelphiMC's operator assistant. You answer questions about the
owner's Minecraft control plane using the read-only tools you are given.

Rules:
- Ground every factual claim about servers, nodes, docs, or metrics in a tool result.
- If a tool fails, say so plainly; do not guess.
- Answer like an operator: direct, specific, no filler. Use the tool output verbatim where it matters.
- You cannot change infrastructure in this version: no start/stop, no edits, no moderation.
- When you used documentation, cite the path (for example docs/architecture/agent.md) inline.
- Some tools change things: create_server, power, run_command, moderate_player, write_file. Calling one
  does not execute it; DelphiMC shows the user an approval card first. When the user asks for
  a change, call the write tools with concrete targets instead of saying you cannot act.
  For a fleet action like "restart all SMP servers", call power with the group name.
  After approval you receive the execution results and then report what happened.`

// Assembler trims conversation history to a character budget.
type Assembler struct {
	MaxChars     int
	MaxToolChars int
}

// DefaultAssembler approximates a 12k-token window.
func DefaultAssembler() Assembler {
	return Assembler{MaxChars: 48_000, MaxToolChars: 6_000}
}

// Assemble returns system + the newest history that fits. The last message is
// always included (truncated if it alone exceeds the budget).
func (a Assembler) Assemble(system string, history []Message) []Message {
	if a.MaxChars <= 0 {
		a.MaxChars = 48_000
	}
	if a.MaxToolChars <= 0 {
		a.MaxToolChars = 6_000
	}
	trimmed := make([]Message, 0, len(history))
	total := len(system)
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		if msg.Role == "tool" && len(msg.Content) > a.MaxToolChars {
			msg.Content = msg.Content[:a.MaxToolChars] + "\n…[truncated]"
		}
		cost := len(msg.Content) + 32
		if total+cost > a.MaxChars && len(trimmed) > 0 {
			break
		}
		trimmed = append(trimmed, msg)
		total += cost
	}
	// Reverse into chronological order.
	for i, j := 0, len(trimmed)-1; i < j; i, j = i+1, j-1 {
		trimmed[i], trimmed[j] = trimmed[j], trimmed[i]
	}
	out := make([]Message, 0, len(trimmed)+1)
	if strings.TrimSpace(system) != "" {
		out = append(out, Message{Role: "system", Content: system})
	}
	return append(out, trimmed...)
}
