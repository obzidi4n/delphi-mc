// Package nbt reads Minecraft's binary NBT format. It is deliberately
// read-only and bounded: the file browser must never let a malformed or
// decompression-bomb file stall the panel or agent.
package nbt

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
)

const (
	maxDepth        = 64
	maxNodes        = 200000
	maxStringBytes  = 1 << 20
	maxDecompressed = 32 << 20
	maxDisplayRunes = 4096
)

// Document is a decoded NBT file.
type Document struct {
	Compression      string `json:"compression"`
	Size             int64  `json:"size"`
	DecompressedSize int64  `json:"decompressed_size"`
	Root             Node   `json:"root"`
}

// Node is a read-only NBT tag. Arrays are summarized rather than expanded.
type Node struct {
	Name      string `json:"name,omitempty"`
	Type      string `json:"type"`
	Value     string `json:"value,omitempty"`
	Length    int    `json:"length,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Children  []Node `json:"children,omitempty"`
}

var typeNames = map[byte]string{
	0:  "End",
	1:  "Byte",
	2:  "Short",
	3:  "Int",
	4:  "Long",
	5:  "Float",
	6:  "Double",
	7:  "ByteArray",
	8:  "String",
	9:  "List",
	10: "Compound",
	11: "IntArray",
	12: "LongArray",
}

// Parse decodes raw, gzip, or zlib-compressed NBT.
func Parse(raw []byte) (Document, error) {
	if len(raw) == 0 {
		return Document{}, errors.New("empty NBT file")
	}
	compression := "none"
	data := raw
	switch {
	case len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b:
		compression = "gzip"
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return Document{}, fmt.Errorf("gzip: %w", err)
		}
		data, err = io.ReadAll(io.LimitReader(zr, maxDecompressed+1))
		if err != nil {
			_ = zr.Close()
			return Document{}, fmt.Errorf("gzip: %w", err)
		}
		if err := zr.Close(); err != nil {
			return Document{}, fmt.Errorf("gzip: %w", err)
		}
		if len(data) > maxDecompressed {
			return Document{}, errors.New("decompressed NBT is too large")
		}
	case isZlib(raw):
		compression = "zlib"
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return Document{}, fmt.Errorf("zlib: %w", err)
		}
		data, err = io.ReadAll(io.LimitReader(zr, maxDecompressed+1))
		if err != nil {
			_ = zr.Close()
			return Document{}, fmt.Errorf("zlib: %w", err)
		}
		if err := zr.Close(); err != nil {
			return Document{}, fmt.Errorf("zlib: %w", err)
		}
		if len(data) > maxDecompressed {
			return Document{}, errors.New("decompressed NBT is too large")
		}
	}
	root, err := parseRoot(data)
	if err != nil {
		return Document{}, err
	}
	return Document{
		Compression: compression, Size: int64(len(raw)),
		DecompressedSize: int64(len(data)), Root: root,
	}, nil
}

func isZlib(data []byte) bool {
	// RFC 1950: CMF/FLG is a valid zlib header.
	return len(data) >= 2 && data[0]&0x0f == 8 && (uint16(data[0])<<8|uint16(data[1]))%31 == 0
}

func parseRoot(data []byte) (Node, error) {
	r := bufio.NewReader(bytes.NewReader(data))
	d := decoder{r: r}
	id, err := d.readByte()
	if err != nil {
		return Node{}, fmt.Errorf("root tag: %w", err)
	}
	if id == 0 {
		return Node{}, errors.New("root tag is End")
	}
	name, err := d.readString()
	if err != nil {
		return Node{}, fmt.Errorf("root name: %w", err)
	}
	node, err := d.readPayload(id, name, 0)
	if err != nil {
		return Node{}, err
	}
	if _, err := r.Peek(1); err != io.EOF {
		if err == nil {
			return Node{}, errors.New("trailing bytes after NBT root")
		}
		return Node{}, err
	}
	return node, nil
}

type decoder struct {
	r     *bufio.Reader
	nodes int
}

func (d *decoder) readByte() (byte, error) {
	b, err := d.r.ReadByte()
	return b, err
}

func (d *decoder) readBytes(n int) ([]byte, error) {
	if n < 0 {
		return nil, errors.New("negative length")
	}
	b := make([]byte, n)
	_, err := io.ReadFull(d.r, b)
	return b, err
}

func (d *decoder) readUint16() (uint16, error) {
	b, err := d.readBytes(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b), nil
}

func (d *decoder) readInt32() (int32, error) {
	b, err := d.readBytes(4)
	if err != nil {
		return 0, err
	}
	return int32(binary.BigEndian.Uint32(b)), nil
}

func (d *decoder) readInt64() (int64, error) {
	b, err := d.readBytes(8)
	if err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(b)), nil
}

func (d *decoder) readString() (string, error) {
	n, err := d.readUint16()
	if err != nil {
		return "", err
	}
	if int(n) > maxStringBytes {
		return "", errors.New("string is too long")
	}
	b, err := d.readBytes(int(n))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (d *decoder) addNode() error {
	d.nodes++
	if d.nodes > maxNodes {
		return errors.New("NBT contains too many tags")
	}
	return nil
}

func (d *decoder) readPayload(id byte, name string, depth int) (Node, error) {
	if depth > maxDepth {
		return Node{}, errors.New("NBT nesting is too deep")
	}
	if err := d.addNode(); err != nil {
		return Node{}, err
	}
	node := Node{Name: name, Type: typeNames[id]}
	if node.Type == "" {
		return Node{}, fmt.Errorf("unknown NBT tag type %d", id)
	}
	switch id {
	case 1:
		b, err := d.readByte()
		if err != nil {
			return Node{}, err
		}
		node.Value = strconv.FormatInt(int64(int8(b)), 10)
	case 2:
		b, err := d.readBytes(2)
		if err != nil {
			return Node{}, err
		}
		node.Value = strconv.FormatInt(int64(int16(binary.BigEndian.Uint16(b))), 10)
	case 3:
		b, err := d.readBytes(4)
		if err != nil {
			return Node{}, err
		}
		node.Value = strconv.FormatInt(int64(int32(binary.BigEndian.Uint32(b))), 10)
	case 4:
		b, err := d.readBytes(8)
		if err != nil {
			return Node{}, err
		}
		node.Value = strconv.FormatInt(int64(binary.BigEndian.Uint64(b)), 10)
	case 5:
		b, err := d.readBytes(4)
		if err != nil {
			return Node{}, err
		}
		node.Value = strconv.FormatFloat(float64(math.Float32frombits(binary.BigEndian.Uint32(b))), 'g', -1, 32)
	case 6:
		b, err := d.readBytes(8)
		if err != nil {
			return Node{}, err
		}
		node.Value = strconv.FormatFloat(math.Float64frombits(binary.BigEndian.Uint64(b)), 'g', -1, 64)
	case 7, 11, 12:
		n, err := d.readInt32()
		if err != nil {
			return Node{}, err
		}
		if n < 0 {
			return Node{}, errors.New("negative array length")
		}
		width := int64(1)
		if id == 11 {
			width = 4
		} else if id == 12 {
			width = 8
		}
		if int64(n)*width > maxDecompressed {
			return Node{}, errors.New("NBT array is too large")
		}
		if _, err := io.CopyN(io.Discard, d.r, int64(n)*width); err != nil {
			return Node{}, err
		}
		node.Length = int(n)
		node.Value = fmt.Sprintf("%d bytes", int64(n)*width)
		if id == 11 {
			node.Value = fmt.Sprintf("%d ints", n)
		} else if id == 12 {
			node.Value = fmt.Sprintf("%d longs", n)
		}
	case 8:
		s, err := d.readString()
		if err != nil {
			return Node{}, err
		}
		node.Length = len(s)
		runes := []rune(s)
		if len(runes) > maxDisplayRunes {
			node.Value = string(runes[:maxDisplayRunes])
			node.Truncated = true
		} else {
			node.Value = s
		}
	case 9:
		elem, err := d.readByte()
		if err != nil {
			return Node{}, err
		}
		n, err := d.readInt32()
		if err != nil {
			return Node{}, err
		}
		if n < 0 {
			return Node{}, errors.New("negative list length")
		}
		if int(n) > maxNodes {
			return Node{}, errors.New("NBT list is too large")
		}
		if elem == 0 && n != 0 {
			return Node{}, errors.New("list of End tags has a nonzero length")
		}
		node.Value = typeNames[elem]
		node.Length = int(n)
		node.Children = make([]Node, 0, min(int(n), 1024))
		for i := 0; i < int(n); i++ {
			child, err := d.readPayload(elem, "", depth+1)
			if err != nil {
				return Node{}, err
			}
			node.Children = append(node.Children, child)
		}
	case 10:
		for {
			childID, err := d.readByte()
			if err != nil {
				return Node{}, err
			}
			if childID == 0 {
				break
			}
			childName, err := d.readString()
			if err != nil {
				return Node{}, err
			}
			child, err := d.readPayload(childID, childName, depth+1)
			if err != nil {
				return Node{}, err
			}
			node.Children = append(node.Children, child)
			node.Length = len(node.Children)
		}
	default:
		return Node{}, fmt.Errorf("unknown NBT tag type %d", id)
	}
	return node, nil
}
