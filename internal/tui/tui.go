// Package tui is the terminal-native DelphiMC client.
package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/coder/websocket"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/wire"
)

type screen int

const (
	screenFleet screen = iota
	screenServer
)

type approval struct {
	ID      string
	Summary string
}

// Model is the Bubble Tea model.
type Model struct {
	client *Client
	width  int
	height int

	screen  screen
	servers []apitypes.ServerDTO
	cursor  int
	current apitypes.ServerDTO

	console []string
	stats   string
	bridge  string
	status  string

	ai           []string
	aiInput      string
	aiFocus      bool
	aiBusy       bool
	aiInDelta    bool
	aiAnswered   bool
	conversation string
	approval     *approval

	overlayTitle string
	overlay      []string

	msgs   chan tea.Msg
	cancel context.CancelFunc
}

// NewModel builds the initial model.
func NewModel(client *Client) *Model {
	return &Model{client: client, width: 80, height: 24, msgs: make(chan tea.Msg, 256)}
}

// Messages.
type serversMsg struct {
	items []apitypes.ServerDTO
	err   error
}
type serverMsg struct {
	item apitypes.ServerDTO
	err  error
}
type consoleMsg struct{ lines []string }
type statsMsg struct{ text string }
type bridgeMsg struct{ text string }
type noteMsg struct{ text string }
type aiEventMsg struct{ event map[string]any }
type tickMsg struct{}
type backupMsg struct {
	id  string
	err error
}
type pluginsMsg struct {
	lines []string
	err   error
}
type updatesAppliedMsg struct {
	n   int
	err error
}

func (m *Model) send(msg tea.Msg) {
	select {
	case m.msgs <- msg:
	case <-time.After(200 * time.Millisecond):
	}
}

func waitFor(m *Model) tea.Cmd {
	return func() tea.Msg { return <-m.msgs }
}

// Init fetches the fleet.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(fetchServers(m.client), waitFor(m), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func fetchServers(c *Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		items, err := c.Servers(ctx)
		return serversMsg{items: items, err: err}
	}
}

func fetchServer(c *Client, id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		item, err := c.Server(ctx, id)
		return serverMsg{item: item, err: err}
	}
}

// Update handles keys and async events.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if m.screen == screenServer && m.current.ID != "" {
			return m, tea.Batch(tickCmd(), fetchServer(m.client, m.current.ID))
		}
		return m, tickCmd()
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}
		if msg.Height > 0 {
			m.height = msg.Height
		}
		return m, nil
	case serversMsg:
		if msg.err != nil {
			m.status = "fleet: " + msg.err.Error()
		} else {
			m.servers = msg.items
			if m.cursor >= len(m.servers) {
				m.cursor = max(0, len(m.servers)-1)
			}
		}
		return m, waitFor(m)
	case serverMsg:
		if msg.err != nil {
			m.status = "server: " + msg.err.Error()
		} else {
			m.current = msg.item
		}
		return m, waitFor(m)
	case consoleMsg:
		m.console = append(m.console, msg.lines...)
		if len(m.console) > 3000 {
			m.console = m.console[len(m.console)-3000:]
		}
		return m, waitFor(m)
	case statsMsg:
		m.stats = msg.text
		return m, waitFor(m)
	case bridgeMsg:
		m.bridge = msg.text
		return m, waitFor(m)
	case noteMsg:
		m.status = msg.text
		return m, waitFor(m)
	case backupMsg:
		if msg.err != nil {
			m.status = "backup: " + msg.err.Error()
		} else {
			m.status = "backup " + msg.id + " ready"
		}
		return m, waitFor(m)
	case pluginsMsg:
		if msg.err != nil {
			m.status = "plugins: " + msg.err.Error()
		} else {
			m.overlayTitle = "Plugins and updates"
			m.overlay = msg.lines
		}
		return m, waitFor(m)
	case updatesAppliedMsg:
		if msg.err != nil {
			m.status = "updates: " + msg.err.Error()
		} else {
			m.status = fmt.Sprintf("applied %d update(s); boot check runs in the panel", msg.n)
		}
		return m, waitFor(m)
	case aiEventMsg:
		m.handleAI(msg.event)
		return m, waitFor(m)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" {
		m.close()
		return m, tea.Quit
	}
	if m.aiFocus {
		switch key.String() {
		case "esc":
			m.aiFocus = false
		case "enter":
			prompt := strings.TrimSpace(m.aiInput)
			m.aiInput = ""
			m.aiFocus = false
			if prompt != "" && !m.aiBusy {
				m.ai = append(m.ai, "", "> "+prompt)
				m.aiBusy = true
				m.aiAnswered = false
				path := "/api/v1/ai/chat"
				body := map[string]any{"message": prompt}
				if m.conversation != "" {
					body["conversation_id"] = m.conversation
				}
				go m.streamAI(path, http.MethodPost, body)
			}
		case "backspace":
			if len(m.aiInput) > 0 {
				m.aiInput = m.aiInput[:len(m.aiInput)-1]
			}
		default:
			if len(key.Runes) > 0 {
				m.aiInput += string(key.Runes)
			}
		}
		return m, nil
	}
	if m.overlay != nil {
		if key.String() == "esc" || key.String() == "p" || key.String() == "q" {
			m.overlay = nil
			return m, nil
		}
		return m, nil
	}
	if m.screen == screenFleet {
		switch key.String() {
		case "q":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.servers)-1 {
				m.cursor++
			}
		case "r":
			return m, fetchServers(m.client)
		case "enter":
			if len(m.servers) == 0 {
				return m, nil
			}
			item := m.servers[m.cursor]
			m.screen = screenServer
			m.current = item
			m.console = nil
			m.ai = nil
			m.aiInput = ""
			m.overlay = nil
			m.status = ""
			m.startConsole(item.ID)
			return m, fetchServer(m.client, item.ID)
		}
		return m, nil
	}

	// Server screen.
	switch key.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		m.close()
		m.screen = screenFleet
		return m, fetchServers(m.client)
	case "a":
		m.aiFocus = true
	case "b":
		return m, m.backupCmd()
	case "p":
		return m, m.pluginsCmd()
	case "u":
		return m, m.applyUpdatesCmd()
	case "s":
		return m, m.powerCmd("start")
	case "x":
		return m, m.powerCmd("stop")
	case "r":
		return m, m.powerCmd("restart")
	case "k":
		return m, m.powerCmd("kill")
	case "y":
		if m.approval != nil {
			id := m.approval.ID
			m.approval = nil
			m.ai = append(m.ai, "[approve] "+id)
			m.aiBusy = true
			m.aiAnswered = false
			go m.streamAI("/api/v1/ai/approvals/"+url.PathEscape(id)+"/approve", http.MethodPost, nil)
		}
	case "n":
		if m.approval != nil {
			id := m.approval.ID
			m.approval = nil
			m.ai = append(m.ai, "[reject] "+id)
			m.aiBusy = true
			m.aiAnswered = false
			go m.streamAI("/api/v1/ai/approvals/"+url.PathEscape(id)+"/reject", http.MethodPost, nil)
		}
	}
	return m, nil
}

func (m *Model) close() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func (m *Model) powerCmd(action string) tea.Cmd {
	id := m.current.ID
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		item, err := c.Power(ctx, id, action)
		if err != nil {
			return noteMsg{text: "power: " + err.Error()}
		}
		return serverMsg{item: item}
	}
}

func (m *Model) backupCmd() tea.Cmd {
	id := m.current.ID
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		row, err := c.Backup(ctx, id)
		return backupMsg{id: row.ID, err: err}
	}
}

func (m *Model) pluginsCmd() tea.Cmd {
	id := m.current.ID
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		plugins, updates, err := c.Plugins(ctx, id)
		if err != nil {
			return pluginsMsg{err: err}
		}
		lines := []string{"Installed:"}
		for _, p := range plugins.Items {
			lines = append(lines, fmt.Sprintf("  %s %s (%s, %s)", p.Project, p.Version, p.Provider, p.Policy))
		}
		if len(updates.Items) == 0 {
			lines = append(lines, "No updates available.")
		} else {
			lines = append(lines, "Updates:")
			for _, u := range updates.Items {
				latest := ""
				if u.Latest != nil {
					latest = *u.Latest
				}
				lines = append(lines, fmt.Sprintf("  %s %s -> %s", u.Project, u.Current, latest))
			}
		}
		return pluginsMsg{lines: lines}
	}
}

func (m *Model) applyUpdatesCmd() tea.Cmd {
	id := m.current.ID
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		res, err := c.ApplyUpdates(ctx, id)
		return updatesAppliedMsg{n: len(res.Applied), err: err}
	}
}

// startConsole opens the server console stream; it reconnects until cancelled.
func (m *Model) startConsole(id string) {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	c := m.client
	ch := m.msgs
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			wsURL := c.base + "/ws/servers/" + url.PathEscape(id)
			if u, err := url.Parse(c.base); err == nil {
				if u.Scheme == "https" {
					u.Scheme = "wss"
				} else {
					u.Scheme = "ws"
				}
				u.Path = strings.TrimRight(u.Path, "/") + "/ws/servers/" + url.PathEscape(id)
				wsURL = u.String()
			}
			conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
				HTTPClient: c.stream,
				HTTPHeader: http.Header{"Authorization": {"Bearer " + c.token}},
			})
			if err != nil {
				select {
				case ch <- noteMsg{text: "console offline; retrying"}:
				case <-ctx.Done():
					return
				}
				time.Sleep(2 * time.Second)
				continue
			}
			conn.SetReadLimit(16 << 20)
			readConsole(ctx, conn, ch)
			conn.CloseNow()
			if ctx.Err() != nil {
				return
			}
		}
	}()
}

func readConsole(ctx context.Context, conn *websocket.Conn, ch chan<- tea.Msg) {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			_, _, payload, err := wire.DecodeConsole(data)
			if err != nil {
				continue
			}
			lines := strings.Split(string(payload), "\n")
			select {
			case ch <- consoleMsg{lines: lines}:
			case <-ctx.Done():
				return
			}
			continue
		}
		var frame map[string]any
		if json.Unmarshal(data, &frame) != nil {
			continue
		}
		switch frame["type"] {
		case "stats":
			cpu, _ := frame["cpu_percent"].(float64)
			mem, _ := frame["memory_bytes"].(float64)
			select {
			case ch <- statsMsg{text: fmt.Sprintf("cpu %.1f%%  mem %s", cpu, humanBytes(int64(mem)))}:
			case <-ctx.Done():
				return
			}
		case "bridge":
			tps, _ := frame["tps"].(float64)
			players, _ := frame["players"].([]any)
			select {
			case ch <- bridgeMsg{text: fmt.Sprintf("tps %.2f  players %d", tps, len(players))}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// streamAI consumes an SSE response and feeds events into the model.
func (m *Model) streamAI(path, method string, body any) {
	c := m.client
	ch := m.msgs
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		ch <- aiEventMsg{event: map[string]any{"type": "error", "message": err.Error()}}
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "text/event-stream")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.stream.Do(req)
	if err != nil {
		ch <- aiEventMsg{event: map[string]any{"type": "error", "message": err.Error()}}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		ch <- aiEventMsg{event: map[string]any{"type": "error", "message": fmt.Sprintf("%s: %s", resp.Status, strings.TrimSpace(string(data)))}}
		return
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var event map[string]any
		if json.Unmarshal([]byte(payload), &event) != nil {
			continue
		}
		ch <- aiEventMsg{event: event}
	}
	if err := scanner.Err(); err != nil {
		ch <- aiEventMsg{event: map[string]any{"type": "error", "message": err.Error()}}
	}
}

func (m *Model) handleAI(event map[string]any) {
	kind, _ := event["type"].(string)
	switch kind {
	case "delta":
		text, _ := event["text"].(string)
		if len(m.ai) == 0 || !m.aiInDelta {
			m.ai = append(m.ai, "")
			m.aiInDelta = true
		}
		m.ai[len(m.ai)-1] += text
		m.aiAnswered = true
	case "tool":
		m.aiInDelta = false
		name, _ := event["name"].(string)
		status, _ := event["status"].(string)
		summary, _ := event["summary"].(string)
		m.ai = append(m.ai, fmt.Sprintf("[tool] %s %s %s", name, status, summary))
	case "citation":
		m.aiInDelta = false
		if c, ok := event["citation"].(map[string]any); ok {
			kind, _ := c["kind"].(string)
			ref, _ := c["ref"].(string)
			m.ai = append(m.ai, fmt.Sprintf("[source] %s %s", kind, ref))
		}
	case "done":
		m.aiInDelta = false
		if text, ok := event["text"].(string); ok && !m.aiAnswered && strings.TrimSpace(text) != "" {
			m.ai = append(m.ai, text)
			m.aiAnswered = true
		}
		if id, ok := event["conversation_id"].(string); ok && id != "" {
			m.conversation = id
		}
		if raw, ok := event["approval"].(map[string]any); ok {
			approvalID, _ := raw["id"].(string)
			summary := "approval required"
			if calls, ok := raw["calls"].([]any); ok {
				var parts []string
				for _, c := range calls {
					call, _ := c.(map[string]any)
					if s, ok := call["summary"].(string); ok {
						parts = append(parts, s)
					}
				}
				if len(parts) > 0 {
					summary = strings.Join(parts, "; ")
				}
			}
			m.approval = &approval{ID: approvalID, Summary: summary}
			m.ai = append(m.ai, "", "APPROVAL: "+summary)
		}
		m.aiBusy = false
	case "error":
		m.aiInDelta = false
		message, _ := event["message"].(string)
		m.ai = append(m.ai, "error: "+message)
		m.aiBusy = false
	}
}

// View renders the current screen.
func (m *Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	header := lipgloss.NewStyle().Bold(true).Render("DelphiMC")
	if m.screen == screenServer {
		header += "  " + m.current.Name + "  " + stateStyle(m.current.ObservedState) + "  rev " +
			fmt.Sprintf("%d/%d", m.current.AppliedRevision, m.current.DesiredRevision)
		if m.stats != "" {
			header += "  " + m.stats
		}
		if m.bridge != "" {
			header += "  " + m.bridge
		}
	}
	if m.overlay != nil {
		lines := append([]string{m.overlayTitle, ""}, m.overlay...)
		lines = append(lines, "", "esc close")
		return box(header, strings.Join(lines, "\n"), m.width, m.height)
	}
	if m.screen == screenFleet {
		var b strings.Builder
		if len(m.servers) == 0 {
			b.WriteString("No servers. Create one with the API or CLI.\n")
		}
		for i, s := range m.servers {
			marker := "  "
			if i == m.cursor {
				marker = "> "
			}
			b.WriteString(fmt.Sprintf("%s%-20s %-9s port %-5d node %s\n", marker, s.Name, s.ObservedState, s.Port, s.NodeID))
		}
		body := b.String() + "\n" + m.status
		footer := "↑/↓ select  enter open  r refresh  q quit"
		return box(header, body, m.width, m.height) + "\n" + footer
	}

	consoleHeight := max(4, (m.height-12)*2/3)
	lines := m.console
	if len(lines) > consoleHeight {
		lines = lines[len(lines)-consoleHeight:]
	}
	consoleBody := strings.Join(lines, "\n")
	if consoleBody == "" {
		consoleBody = "waiting for console…"
	}
	aiHeight := max(3, m.height-12-consoleHeight)
	aiLines := m.ai
	if len(aiLines) > aiHeight {
		aiLines = aiLines[len(aiLines)-aiHeight:]
	}
	aiBody := strings.Join(aiLines, "\n")
	if aiBody == "" {
		aiBody = "press a to ask the operator"
	}
	if m.aiFocus {
		aiBody += "\n> " + m.aiInput + "█"
	}
	if m.approval != nil {
		aiBody += "\n[y] approve  [n] reject: " + m.approval.Summary
	}
	if m.aiBusy {
		aiBody += "\n…thinking"
	}
	body := lipgloss.NewStyle().Bold(true).Render("console") + "\n" + consoleBody + "\n\n" +
		lipgloss.NewStyle().Bold(true).Render("operator") + "\n" + aiBody
	footer := "esc back  s/x/r/k power  b backup  p plugins  u updates  a AI  q quit"
	if m.status != "" {
		footer = m.status + "\n" + footer
	}
	return box(header, body, m.width, m.height) + "\n" + footer
}

func box(header, body string, width, height int) string {
	if width < 20 {
		width = 20
	}
	return lipgloss.NewStyle().
		Width(width - 2).Height(max(3, height-3)).
		Border(lipgloss.RoundedBorder()).
		Render(header + "\n\n" + body)
}

func stateStyle(state string) string {
	switch state {
	case "running":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(state)
	case "error", "exited":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(state)
	case "stopped":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(state)
	}
	return state
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
