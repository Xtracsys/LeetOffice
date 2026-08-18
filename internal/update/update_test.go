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
	"strings"
	"testing"
)

func TestAssetNameAndChecksums(t *testing.T) {
	if got := AssetName("v0.1.3", "darwin", "arm64"); got != "leetd-0.1.3-darwin-arm64" {
		t.Fatalf("asset: %s", got)
	}
	if got := AssetName("0.1.3", "windows", "amd64"); got != "leetd-0.1.3-windows-amd64.exe" {
		t.Fatalf("win asset: %s", got)
	}
	if got := ChecksumName("v0.1.3"); got != "checksums-0.1.3.txt" {
		t.Fatalf("sum: %s", got)
	}
}

func TestCompareAndIsRelease(t *testing.T) {
	if !IsRelease("v0.1.3") || !IsRelease("0.1.3") || IsRelease("dev") || IsRelease("bb00e1d") {
		t.Fatal("IsRelease")
	}
	if Compare("v0.1.2", "0.1.3") >= 0 {
		t.Fatal("0.1.2 should be older")
	}
	if Compare("v0.1.3", "0.1.3") != 0 {
		t.Fatal("equal")
	}
	if Compare("v0.2.0", "0.1.9") <= 0 {
		t.Fatal("0.2.0 should be newer")
	}
}

func TestParseChecksums(t *testing.T) {
	raw := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  leetd-0.1.3-darwin-arm64\n")
	got, err := ParseChecksums(raw, "leetd-0.1.3-darwin-arm64")
	if err != nil || got != strings.Repeat("a", 64) {
		t.Fatalf("parse: %q %v", got, err)
	}
	if _, err := ParseChecksums(raw, "leetd-0.1.3-linux-amd64"); err == nil {
		t.Fatal("missing asset should fail")
	}
}

func TestCheckAndApply(t *testing.T) {
	payload := []byte("fake-leetd-binary-for-tests")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])
	goos, goarch := runtime.GOOS, runtime.GOARCH
	tag := "v9.9.9"
	binName := AssetName(tag, goos, goarch)
	sumName := ChecksumName(tag)

	var hits []string
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/repos/Xtracsys/LeetOffice/releases/latest":
			fmt.Fprintf(w, `{
				"tag_name": %q,
				"html_url": "https://github.com/Xtracsys/LeetOffice/releases/tag/%s",
				"assets": [
					{"name": %q, "browser_download_url": %q, "digest": "sha256:%s"},
					{"name": %q, "browser_download_url": %q}
				]
			}`, tag, tag, binName, srv.URL+"/dl/"+binName, hexSum, sumName, srv.URL+"/dl/"+sumName)
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

	res, err := c.Check(context.Background(), "v0.1.3")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Newer || res.Latest != tag || res.Release.AssetName != binName {
		t.Fatalf("check: %+v", res)
	}

	same, err := c.Check(context.Background(), "v9.9.9")
	if err != nil || same.Newer {
		t.Fatalf("same should not be newer: %+v %v", same, err)
	}

	dest := filepath.Join(t.TempDir(), "leetd")
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	applied, err := c.Apply(context.Background(), dest, res.Release)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("replaced: %q %v", got, err)
	}
	if applied.Version != tag || applied.Bytes != int64(len(payload)) {
		t.Fatalf("apply result: %+v", applied)
	}
	if _, err := os.Stat(dest + ".old"); !os.IsNotExist(err) {
		t.Fatal("stale .old left behind")
	}

	// GET /settings must never be implied — only Latest/Apply hit the server.
	// We just assert the recorded paths are the ones we called.
	joined := strings.Join(hits, "\n")
	if !strings.Contains(joined, "/releases/latest") || !strings.Contains(joined, "/dl/"+binName) {
		t.Fatalf("unexpected hits:\n%s", joined)
	}
}

func TestApplyRejectsBadChecksum(t *testing.T) {
	payload := []byte("tampered")
	goos, goarch := "darwin", "arm64"
	binName := AssetName("v1.0.0", goos, goarch)
	sumName := ChecksumName("v1.0.0")
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			fmt.Fprintf(w, `{"tag_name":"v1.0.0","assets":[
				{"name":%q,"browser_download_url":%q},
				{"name":%q,"browser_download_url":%q}]}`,
				binName, srv.URL+"/b", sumName, srv.URL+"/c")
		case r.URL.Path == "/b":
			_, _ = w.Write(payload)
		case r.URL.Path == "/c":
			fmt.Fprintf(w, "%s  %s\n", strings.Repeat("0", 64), binName)
		default:
			http.NotFound(w, r)
		}
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{API: srv.URL, GOOS: goos, GOARCH: goarch}
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "leetd")
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(context.Background(), dest, rel); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum mismatch, got %v", err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "old" {
		t.Fatal("dest overwritten after failed verify")
	}
}

func TestGuardURLRejectsOffHost(t *testing.T) {
	c := Default()
	for _, raw := range []string{"https://evil.example/leetd", "file:///etc/passwd"} {
		req, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.guardURL(req.URL); err == nil {
			t.Fatalf("allowed %s", raw)
		}
	}
	ok, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/Xtracsys/LeetOffice/releases/latest", nil)
	if err := c.guardURL(ok.URL); err != nil {
		t.Fatal(err)
	}
}
