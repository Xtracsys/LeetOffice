package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// RR-030: hand-uploaded releases have carried the tag verbatim in asset
// names ("leetd-v0.1.9-darwin-arm64"); Latest must accept both that and the
// normalized name, and Apply must verify + install from them.
func TestLatestAcceptsTagPrefixedAssetNames(t *testing.T) {
	payload := []byte("fake-leetd-binary-for-tests")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])
	goos, goarch := runtime.GOOS, runtime.GOARCH
	tag := "v0.1.9"
	binName := "leetd-" + tag + "-" + goos + "-" + goarch // NOT AssetName: v kept
	sumName := "checksums-" + tag + ".txt"                // v kept

	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/Xtracsys/LeetOffice/releases/latest":
			fmt.Fprintf(w, `{
				"tag_name": %q,
				"html_url": "https://github.com/Xtracsys/LeetOffice/releases/tag/%s",
				"assets": [
					{"name": %q, "browser_download_url": %q},
					{"name": %q, "browser_download_url": %q}
				]
			}`, tag, tag, binName, srv.URL+"/dl/"+binName, sumName, srv.URL+"/dl/"+sumName)
		case "/dl/" + binName:
			_, _ = w.Write(payload)
		case "/dl/" + sumName:
			fmt.Fprintf(w, "%s  %s\n", hexSum, binName)
		default:
			http.NotFound(w, r)
		}
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{API: srv.URL, Repo: DefaultRepo, GOOS: goos, GOARCH: goarch}
	res, err := c.Check(context.Background(), "v0.1.8")
	if err != nil {
		t.Fatalf("check against v-prefixed assets: %v", err)
	}
	if !res.Newer || res.Release.AssetName != binName {
		t.Fatalf("check: %+v", res)
	}
	dest := filepath.Join(t.TempDir(), "leetd")
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(context.Background(), dest, res.Release); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("replaced: %q %v", got, err)
	}
}
