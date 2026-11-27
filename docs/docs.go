// Package docs embeds the documentation corpus used by the AI retrieval tool.
// The markdown files remain the source; the binary carries a snapshot.
package docs

import "embed"

//go:embed *.md */*.md
var FS embed.FS
