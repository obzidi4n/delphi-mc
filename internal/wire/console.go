package wire

import (
	"encoding/binary"
	"errors"
	"time"
)

// Console batch framing (binary, one frame per batch):
//
//	u8  kind (=1)
//	u16 server id length
//	u8[] server id
//	u64 seq of the first line (each following line is seq+1)
//	u32 payload length
//	u8[] payload: newline-delimited raw log lines, no trailing newline
//
// The server id is included because one agent channel carries a node's whole
// fleet; the docs' server-implied framing only works for a single subscription.
const ConsoleKind byte = 1

// ErrBadConsoleFrame reports a truncated or unknown console frame.
var ErrBadConsoleFrame = errors.New("bad console frame")

// EncodeConsole builds a console batch frame.
func EncodeConsole(serverID string, seq uint64, payload []byte) []byte {
	out := make([]byte, 0, 1+2+len(serverID)+8+4+len(payload))
	out = append(out, ConsoleKind, byte(len(serverID)>>8), byte(len(serverID)))
	out = append(out, serverID...)
	var num [8]byte
	binary.BigEndian.PutUint64(num[:], seq)
	out = append(out, num[:]...)
	var ln [4]byte
	binary.BigEndian.PutUint32(ln[:], uint32(len(payload)))
	out = append(out, ln[:]...)
	return append(out, payload...)
}

// DecodeConsole parses a console batch frame.
func DecodeConsole(frame []byte) (serverID string, seq uint64, payload []byte, err error) {
	if len(frame) < 1+2+8+4 || frame[0] != ConsoleKind {
		return "", 0, nil, ErrBadConsoleFrame
	}
	idLen := int(binary.BigEndian.Uint16(frame[1:3]))
	off := 3 + idLen
	if len(frame) < off+8+4 {
		return "", 0, nil, ErrBadConsoleFrame
	}
	serverID = string(frame[3:off])
	seq = binary.BigEndian.Uint64(frame[off : off+8])
	off += 8
	payLen := int(binary.BigEndian.Uint32(frame[off : off+4]))
	off += 4
	if len(frame) < off+payLen {
		return "", 0, nil, ErrBadConsoleFrame
	}
	return serverID, seq, frame[off : off+payLen], nil
}

// ConsoleLine is one line in a ring buffer, with its sequence number.
type ConsoleLine struct {
	Seq  uint64 `json:"seq"`
	Text string `json:"text"`
}

// MetricsFrame is one server's 1s sample. The panel rolls it up.
type MetricsFrame struct {
	Type        string    `json:"type"`
	ServerID    string    `json:"server_id"`
	CPUPercent  float64   `json:"cpu_percent"`
	MemoryBytes int64     `json:"memory_bytes"`
	At          time.Time `json:"at"`
}
