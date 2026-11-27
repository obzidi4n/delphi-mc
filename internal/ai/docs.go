package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"delphi.dev/delphi/internal/store"
)

// DocStore is documentation persistence.
type DocStore interface {
	ReplaceDocs(ctx context.Context, hash string, chunks []store.DocChunk) (bool, error)
	SearchDocs(ctx context.Context, query string, limit int) ([]store.DocChunk, error)
}

const (
	chunkChars = 1600
	maxDocs    = 5000
)

// IndexDocs chunks a markdown tree and swaps it in when the corpus changed.
// It returns the chunk count and whether it wrote.
func IndexDocs(ctx context.Context, db DocStore, fsys fs.FS) (int, bool, error) {
	var paths []string
	err := fs.WalkDir(fsys, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return 0, false, fmt.Errorf("walk docs: %w", err)
	}
	sort.Strings(paths)

	var chunks []store.DocChunk
	for _, path := range paths {
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return 0, false, fmt.Errorf("read %s: %w", path, err)
		}
		chunks = append(chunks, chunkDoc(path, string(data))...)
		if len(chunks) >= maxDocs {
			chunks = chunks[:maxDocs]
			break
		}
	}
	hash := corpusHash(chunks)
	changed, err := db.ReplaceDocs(ctx, hash, chunks)
	if err != nil {
		return len(chunks), false, err
	}
	return len(chunks), changed, nil
}

// chunkDoc splits markdown at headings and at a size cap. The heading line
// stays in the text so the model sees where the snippet came from.
func chunkDoc(path, text string) []store.DocChunk {
	var out []store.DocChunk
	heading := ""
	var buffer strings.Builder
	ordinal := 0
	flush := func() {
		body := strings.TrimSpace(buffer.String())
		buffer.Reset()
		if body == "" {
			return
		}
		out = append(out, store.DocChunk{Path: path, Ordinal: ordinal, Heading: heading, Content: body})
		ordinal++
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "#") {
			flush()
			heading = strings.TrimSpace(strings.TrimLeft(line, "#"))
			continue
		}
		buffer.WriteString(line)
		buffer.WriteByte('\n')
		if buffer.Len() > chunkChars {
			flush()
		}
	}
	flush()
	return out
}

func corpusHash(chunks []store.DocChunk) string {
	h := sha256.New()
	for _, c := range chunks {
		fmt.Fprintf(h, "%s\x00%d\x00%s\x00%s\x00", c.Path, c.Ordinal, c.Heading, c.Content)
	}
	return hex.EncodeToString(h.Sum(nil))
}
