// Package hostsign ad-hoc signs the leetd binary on macOS.
//
// A copied Go binary keeps the linker's identifier "a.out". launchd then
// SIGKILLs it with "Code Signature Invalid" / Invalid Page, KeepAlive
// restarts it until the job is marked inefficient, and the UI looks
// crashed until someone starts leetd by hand. Re-signing with our
// identifier on install, update, and first listen stops that.
//
// No-op on Linux/Windows. Ad-hoc (--sign -) talks to no network
// (--timestamp=none), so this stays inside P1.
package hostsign

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Identifier is the codesign id launchd / AMFI see. Must match the
// launchd Label prefix (dev.leetoffice.leetd).
const Identifier = "dev.leetoffice.leetd"

// Ensure ad-hoc signs path on Darwin when it is an unsigned or
// linker-signed Mach-O. The running process keeps its old inode if
// path is replaced. No-op on other OSes and on non-Mach-O files
// (tests feed ReplaceBinary fake payloads).
func Ensure(path string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	if path == "" {
		return fmt.Errorf("empty path")
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if !isMachO(path) {
		return nil
	}
	stripXattrs(path)
	if alreadySigned(path) {
		return nil
	}
	return replaceSigned(path)
}

func alreadySigned(path string) bool {
	if err := exec.Command("codesign", "--verify", path).Run(); err != nil {
		return false
	}
	info, err := exec.Command("codesign", "-dv", "--verbose=2", path).CombinedOutput()
	if err != nil {
		return false
	}
	s := string(info)
	if strings.Contains(s, "linker-signed") {
		return false
	}
	return strings.Contains(s, "Identifier="+Identifier)
}

func replaceSigned(path string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".leetd-sign-*")
	if err != nil {
		return signInPlace(path)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	src, err := os.Open(path)
	if err != nil {
		tmp.Close()
		return err
	}
	_, copyErr := io.Copy(tmp, src)
	src.Close()
	if copyErr != nil {
		tmp.Close()
		return copyErr
	}
	mode := os.FileMode(0o755)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode()
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := signInPlace(tmpName); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return signInPlace(path)
	}
	cleanup = false
	stripXattrs(path)
	return nil
}

func signInPlace(path string) error {
	cmd := exec.Command("codesign", "--force", "--sign", "-",
		"--identifier", Identifier, "--timestamp=none", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesign %s: %w (%s)", path, err, strings.TrimSpace(string(out)))
	}
	stripXattrs(path)
	return nil
}

func stripXattrs(path string) {
	_ = exec.Command("xattr", "-d", "com.apple.quarantine", path).Run()
	_ = exec.Command("xattr", "-d", "com.apple.provenance", path).Run()
}

func isMachO(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var mag [4]byte
	if _, err := io.ReadFull(f, mag[:]); err != nil {
		return false
	}
	n := binary.LittleEndian.Uint32(mag[:])
	switch n {
	case 0xfeedfacf, // MH_MAGIC_64
		0xcffaedfe, // MH_CIGAM_64
		0xfeedface, // MH_MAGIC
		0xcefaedfe, // MH_CIGAM
		0xcafebabe, // FAT_MAGIC
		0xbebafeca: // FAT_CIGAM
		return true
	}
	return false
}
