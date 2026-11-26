// Package web embeds the static assets served by the panel. It becomes the
// build output of the React SPA at M3a; at M0 it is a single hello page.
package web

import (
	"embed"
	"fmt"
)

//go:embed static
var assets embed.FS

// Index returns the HTML entry point.
func Index() ([]byte, error) {
	b, err := assets.ReadFile("static/index.html")
	if err != nil {
		return nil, fmt.Errorf("read embedded index: %w", err)
	}
	return b, nil
}
