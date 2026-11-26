// Package wire defines the frames exchanged over the agent channel. It is the
// Go half of docs/reference/wire-protocol.md; the JSON tags are the contract.
package wire

// ServerSpec is one server's desired state, as the panel sends it to a node.
type ServerSpec struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Revision      int64   `json:"revision"`
	DesiredState  string  `json:"desired_state"` // running | stopped
	KillRequested bool    `json:"kill_requested,omitempty"`
	AcceptEULA    bool    `json:"accept_eula"`
	Image         string  `json:"image"`
	JavaVersion   int     `json:"java_version"`
	MemoryMB      int     `json:"memory_mb"`
	CPUCores      float64 `json:"cpu_cores"`
	Port          int     `json:"port"`
	JarURL        string  `json:"jar_url"`
	JarSHA256     string  `json:"jar_sha256"`
}

// ServersFrame replaces the node's full desired set. Reconciliation is
// declarative: the agent applies the frame, it does not execute commands.
type ServersFrame struct {
	Type  string       `json:"type"`
	Items []ServerSpec `json:"items"`
}

// ServerStateFrame is the agent's observed result for one server.
type ServerStateFrame struct {
	Type     string `json:"type"`
	ServerID string `json:"server_id"`
	State    string `json:"state"` // unknown | starting | running | stopped | exited | error
	Revision int64  `json:"revision"`
	Detail   string `json:"detail,omitempty"`
}

// Frame type names.
const (
	TypeHello       = "hello"
	TypeHeartbeat   = "heartbeat"
	TypeHelloAck    = "hello.ack"
	TypeServers     = "servers"
	TypeServerState = "server.state"
)
