package ai

import (
	"testing"

	"delphi.dev/delphi/internal/store"
)

func TestChunkDocSplitsAtHeadings(t *testing.T) {
	chunks := chunkDoc("docs/a.md", "# Title\nintro\n## One\nalpha\n## Two\nbeta gamma\n")
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Heading != "Title" || chunks[1].Heading != "One" || chunks[2].Heading != "Two" {
		t.Fatalf("headings = %q %q %q", chunks[0].Heading, chunks[1].Heading, chunks[2].Heading)
	}
	if chunks[2].Content != "beta gamma" {
		t.Fatalf("content = %q", chunks[2].Content)
	}
}

func TestCorpusHashChangesWithContent(t *testing.T) {
	a := []store.DocChunk{{Path: "a", Ordinal: 0, Heading: "h", Content: "one"}}
	if corpusHash(a) == corpusHash([]store.DocChunk{{Path: "a", Ordinal: 0, Heading: "h", Content: "two"}}) {
		t.Fatal("hash ignored content")
	}
}
