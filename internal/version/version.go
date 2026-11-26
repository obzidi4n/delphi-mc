// Package version reports the build identity of this binary.
package version

// Version is overridden at build time:
//
//	go build -ldflags "-X delphi.dev/delphi/internal/version.Version=$(git describe --always --dirty)"
var Version = "dev"

// String returns the human-readable version line.
func String() string { return "delphi " + Version }
