// Package testenv isolates Go tests from the operator's real home directory
// (FR-012). Two release-readiness incidents (a wizard test committing into
// the real ~/LeetOffice store, and `leetd init` clobbering the real
// node.json) shared one root cause: code defaulting to os.UserHomeDir().
// Per-feature guards exist (FR-002, FR-009); this package makes the whole
// test tree structurally incapable of touching real files.
//
// Call Isolate from a package's TestMain; add a TestMain to every test
// package. The canary test in internal/config asserts the defaults really
// resolve inside the isolated home.
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

var home string

// Isolate points HOME, USERPROFILE, and XDG_CONFIG_HOME at a per-run temp
// directory and runs the test binary. Always call from TestMain.
func Isolate(m *testing.M) {
	h, err := os.MkdirTemp("", "leetoffice-hermetic-home-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "testenv: cannot create hermetic home:", err)
		os.Exit(1)
	}
	home = h
	os.Setenv("HOME", h)
	os.Setenv("USERPROFILE", h)
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	code := m.Run()
	_ = os.RemoveAll(h)
	os.Exit(code)
}

// Home returns the isolated home root ("" before Isolate runs).
func Home() string { return home }
