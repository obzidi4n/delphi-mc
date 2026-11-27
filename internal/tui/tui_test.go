package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"delphi.dev/delphi/internal/apitypes"
)

func TestHandleAIDeltaAndApproval(t *testing.T) {
	m := NewModel(nil)
	m.ai = []string{"> restart"}

	m.handleAI(map[string]any{"type": "delta", "text": "working"})
	if len(m.ai) == 0 || !strings.Contains(m.ai[len(m.ai)-1], "working") {
		t.Fatalf("delta not appended: %v", m.ai)
	}
	m.handleAI(map[string]any{"type": "done", "conversation_id": "c_1", "approval": map[string]any{
		"id":    "ap_1",
		"calls": []any{map[string]any{"summary": "restart Plugins-Demo"}},
	}})
	if m.conversation != "c_1" || m.approval == nil || m.approval.ID != "ap_1" {
		t.Fatalf("approval state = %+v conversation=%q", m.approval, m.conversation)
	}
	if !strings.Contains(strings.Join(m.ai, "\n"), "APPROVAL: restart Plugins-Demo") {
		t.Fatalf("approval not shown: %v", m.ai)
	}
}

func TestFleetViewAndSelection(t *testing.T) {
	m := NewModel(nil)
	m.width, m.height = 100, 30
	m.servers = []apitypes.ServerDTO{
		{Name: "SMP", ObservedState: "running"},
		{Name: "Lobby", ObservedState: "stopped"},
	}
	view := m.View()
	if !strings.Contains(view, "SMP") || !strings.Contains(view, "Lobby") {
		t.Fatalf("view = %q", view)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("cursor = %d", m.cursor)
	}
}

func TestZeroWindowSizeKeepsDefaults(t *testing.T) {
	m := NewModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 0, Height: 0})
	if m.width != 80 || m.height != 24 {
		t.Fatalf("size = %dx%d", m.width, m.height)
	}
}
