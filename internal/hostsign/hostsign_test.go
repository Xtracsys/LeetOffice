package hostsign

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnsureNoopOnNonMachO(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "leetd")
	if err := os.WriteFile(p, []byte("not-a-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(p); err != nil {
		t.Fatalf("Ensure(non-Mach-O): %v", err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "not-a-binary" {
		t.Fatal("Ensure mutated a non-Mach-O file")
	}
}

func TestEnsureEmptyPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		if err := Ensure(""); err != nil {
			t.Fatalf("non-darwin empty: %v", err)
		}
		return
	}
	if err := Ensure(""); err == nil {
		t.Fatal("darwin empty path should fail")
	}
}

func TestEnsureSignsCopiedDarwinBinary(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("codesign is a Darwin tool")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain required to build a throwaway Mach-O")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	built := filepath.Join(dir, "built")
	cmd := exec.Command("go", "build", "-o", built, src)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	// Copying is what invalidates the linker's a.out signature.
	dest := filepath.Join(dir, "leetd")
	data, err := os.ReadFile(built)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dest); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if err := exec.Command("codesign", "--verify", dest).Run(); err != nil {
		t.Fatalf("codesign --verify: %v", err)
	}
	info, err := exec.Command("codesign", "-dv", "--verbose=2", dest).CombinedOutput()
	if err != nil {
		t.Fatalf("codesign -dv: %v\n%s", err, info)
	}
	s := string(info)
	if !strings.Contains(s, "Identifier="+Identifier) {
		t.Fatalf("identifier missing:\n%s", s)
	}
	if strings.Contains(s, "linker-signed") {
		t.Fatalf("still linker-signed:\n%s", s)
	}
	if err := Ensure(dest); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
}
