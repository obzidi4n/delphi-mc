package wire

const (
	TypeDevClone       = "dev.clone"
	TypeDevCloneResult = "dev.clone.result"
)

// WhitelistEntry is one owner entry written into a dev server's whitelist.json.
type WhitelistEntry struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
}

// DevCloneRequest asks a node to clone one server directory into a new dev
// server directory and rewrite its port/whitelist settings.
type DevCloneRequest struct {
	Type           string           `json:"type"`
	RequestID      string           `json:"request_id"`
	SourceServerID string           `json:"source_server_id"`
	TargetServerID string           `json:"target_server_id"`
	Port           int              `json:"port"`
	Whitelist      []WhitelistEntry `json:"whitelist,omitempty"`
	FreshWorld     bool             `json:"fresh_world,omitempty"`
}

// DevCloneResult reports the clone outcome.
type DevCloneResult struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	OK        bool   `json:"ok"`
	Method    string `json:"method,omitempty"`
	Bytes     int64  `json:"bytes,omitempty"`
	Error     string `json:"error,omitempty"`
}
