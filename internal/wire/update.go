package wire

const TypeAgentUpdate = "agent.update"

// AgentUpdateFrame tells a node to install a signed agent binary.
type AgentUpdateFrame struct {
	Type      string `json:"type"`
	SHA256    string `json:"sha256"`
	Version   string `json:"version"`
	Signature string `json:"signature"`
}
