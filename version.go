package main

import "fmt"

// Build-time metadata injected via -ldflags "-X main.version=... -X main.commit=...
// -X main.date=...". The defaults apply to a plain `go build` with no injection;
// `make build`/`make dist` and the release workflow derive them from git.
var (
	version = "0.9.0-dev"
	commit  = "none"
	date    = "unknown"
)

// versionString renders the full version line printed by --version.
func versionString() string {
	return fmt.Sprintf("aimgen %s (commit %s, built %s)", version, commit, date)
}
