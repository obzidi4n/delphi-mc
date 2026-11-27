package wire

import "time"

// Bridge frame types.
const (
	TypeBridgeState   = "bridge.state"
	TypeBridgeEvent   = "bridge.event"
	TypeBridgeCommand = "bridge.command"
)

// BridgePlayer is one online player.
type BridgePlayer struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
}

// BridgeEvent is one structured event from the plugin.
type BridgeEvent struct {
	Seq     int64  `json:"seq"`
	Kind    string `json:"kind"` // chat | join | quit | moderation | ready
	Player  string `json:"player,omitempty"`
	Message string `json:"message,omitempty"`
	Action  string `json:"action,omitempty"`
	OK      bool   `json:"ok,omitempty"`
	Error   string `json:"error,omitempty"`
	ID      string `json:"id,omitempty"`
}

// BridgeStateFrame is the plugin's periodic snapshot.
type BridgeStateFrame struct {
	Type     string         `json:"type"`
	ServerID string         `json:"server_id"`
	TPS      float64        `json:"tps"`
	Players  []BridgePlayer `json:"players"`
	Commands []string       `json:"commands"`
	Updated  time.Time      `json:"updated"`
}

// BridgeEventFrame carries one event to the panel.
type BridgeEventFrame struct {
	Type     string      `json:"type"`
	ServerID string      `json:"server_id"`
	Event    BridgeEvent `json:"event"`
}

// BridgeCommandFrame asks the agent's plugin to act.
type BridgeCommandFrame struct {
	Type     string `json:"type"`
	ServerID string `json:"server_id"`
	ID       string `json:"id"`
	Action   string `json:"action"` // kick | ban | command
	Player   string `json:"player,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Command  string `json:"command,omitempty"`
}
