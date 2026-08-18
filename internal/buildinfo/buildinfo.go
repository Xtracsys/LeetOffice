// Package buildinfo holds version strings injected at link time
// (`-X leetoffice/internal/buildinfo.Version=…`). The CLI, UI, and
// installer all read from here so a release binary never says "dev"
// in one place and a tag in another.
package buildinfo

import (
	"fmt"
	"runtime"
)

// Set by scripts/dist.sh / the Release workflow. Leave as-is for `go build`.
var (
	Version = "dev"
	Commit  = "none"
)

// Short is the compact stamp for the menubar, e.g. "v0.1.3 · bb00e1d".
func Short() string {
	if Commit != "" && Commit != "none" {
		c := Commit
		if len(c) > 7 {
			c = c[:7]
		}
		return Version + " · " + c
	}
	return Version
}

// Line is the `leetd version` first line without OS/arch.
func Line() string {
	return fmt.Sprintf("leetoffice %s (%s)", Version, Commit)
}

// Full is the complete CLI version line.
func Full() string {
	return fmt.Sprintf("%s %s/%s", Line(), runtime.GOOS, runtime.GOARCH)
}
