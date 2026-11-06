package config

import (
	"path/filepath"
	"strings"
	"testing"

	"leetoffice/internal/testenv"
)

// FR-012 canary: with the hermetic home active, every default path must
// resolve inside it — never the operator's real home. A missing TestMain in
// this package (or any consumer of these defaults) fails here loudly.
func TestDefaultsStayInsideHermeticHome(t *testing.T) {
	h := testenv.Home()
	if h == "" {
		t.Fatal("hermetic home not active — TestMain is missing from this package")
	}
	paths := map[string]string{
		"DefaultPath":     DefaultPath(),
		"DefaultStoreDir": DefaultStoreDir(),
		"IdentityDir":     IdentityDirFor(DefaultStoreDir()),
	}
	for name, p := range paths {
		if !strings.HasPrefix(p, h+string(filepath.Separator)) && p != h {
			t.Fatalf("%s = %q escapes the hermetic home %q", name, p, h)
		}
	}
}
