package nbt

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"testing"
)

func tagString(buf *bytes.Buffer, s string) {
	_ = binary.Write(buf, binary.BigEndian, uint16(len(s)))
	buf.WriteString(s)
}

func testNBT() []byte {
	var buf bytes.Buffer
	buf.WriteByte(10) // root compound
	tagString(&buf, "")
	buf.WriteByte(3) // int
	tagString(&buf, "num")
	_ = binary.Write(&buf, binary.BigEndian, int32(42))
	buf.WriteByte(8) // string
	tagString(&buf, "name")
	tagString(&buf, "hello")
	buf.WriteByte(9) // list
	tagString(&buf, "list")
	buf.WriteByte(1) // byte
	_ = binary.Write(&buf, binary.BigEndian, int32(2))
	buf.WriteByte(1)
	buf.WriteByte(2)
	buf.WriteByte(7) // byte array
	tagString(&buf, "bytes")
	_ = binary.Write(&buf, binary.BigEndian, int32(3))
	buf.Write([]byte{1, 2, 3})
	buf.WriteByte(0) // end root
	return buf.Bytes()
}

func TestParseRawNBT(t *testing.T) {
	doc, err := Parse(testNBT())
	if err != nil {
		t.Fatal(err)
	}
	if doc.Compression != "none" || doc.Root.Type != "Compound" || len(doc.Root.Children) != 4 {
		t.Fatalf("document = %+v", doc)
	}
	byName := map[string]Node{}
	for _, child := range doc.Root.Children {
		byName[child.Name] = child
	}
	if byName["num"].Value != "42" || byName["name"].Value != "hello" {
		t.Fatalf("children = %+v", byName)
	}
	if byName["list"].Length != 2 || len(byName["list"].Children) != 2 {
		t.Fatalf("list = %+v", byName["list"])
	}
	if byName["bytes"].Length != 3 || byName["bytes"].Value != "3 bytes" {
		t.Fatalf("array = %+v", byName["bytes"])
	}
}

func TestParseGzipNBT(t *testing.T) {
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(testNBT()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	doc, err := Parse(compressed.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if doc.Compression != "gzip" || doc.Root.Type != "Compound" {
		t.Fatalf("document = %+v", doc)
	}
}

func TestParseRejectsMalformed(t *testing.T) {
	cases := [][]byte{
		nil,
		{0},
		{10, 0, 0, 99},
		append(testNBT(), 0),
	}
	for _, data := range cases {
		if _, err := Parse(data); err == nil {
			t.Fatalf("malformed NBT accepted: %x", data)
		}
	}
}
