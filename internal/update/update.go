// Package update installs a newer leetd from GitHub Releases.
//
// P1 (100% local): this package is the documented egress exception.
// Nothing here runs unless the operator clicks Check / Apply in Settings
// or runs `leetd update`. There is no timer, no startup ping, and no
// background check.
package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"leetoffice/internal/buildinfo"
)

const (
	// DefaultRepo is the public release source. install.sh and the
	// Homebrew formula use the same org/repo.
	DefaultRepo = "Xtracsys/LeetOffice"
	defaultAPI  = "https://api.github.com"

	maxAssetBytes = 80 << 20 // 80 MiB — releases are ~13 MB
	httpTimeout   = 45 * time.Second
	applyTimeout  = 2 * time.Minute
)

// Client talks to a GitHub-compatible releases API. Tests point API at
// httptest; production leaves it empty (api.github.com).
type Client struct {
	HTTP   *http.Client
	API    string
	Repo   string
	GOOS   string
	GOARCH string
}

// Default is the production client (Xtracsys/LeetOffice, this GOOS/GOARCH).
func Default() *Client {
	return &Client{
		API:    defaultAPI,
		Repo:   DefaultRepo,
		GOOS:   runtime.GOOS,
		GOARCH: runtime.GOARCH,
	}
}

func (c *Client) api() string {
	if c != nil && c.API != "" {
		return strings.TrimRight(c.API, "/")
	}
	return defaultAPI
}

func (c *Client) repo() string {
	if c != nil && c.Repo != "" {
		return c.Repo
	}
	return DefaultRepo
}

func (c *Client) goos() string {
	if c != nil && c.GOOS != "" {
		return c.GOOS
	}
	return runtime.GOOS
}

func (c *Client) goarch() string {
	if c != nil && c.GOARCH != "" {
		return c.GOARCH
	}
	return runtime.GOARCH
}

// Release is one GitHub release plus the asset that matches this platform.
type Release struct {
	Tag          string
	HTMLURL      string
	AssetName    string
	AssetURL     string
	AssetDigest  string // sha256 hex from the API, if GitHub sent one
	ChecksumName string
	ChecksumURL  string
}

// CheckResult is what Settings and `leetd update` render.
type CheckResult struct {
	Current string
	Latest  string
	Newer   bool
	Release *Release
}

// ApplyResult is a successful on-disk replace.
type ApplyResult struct {
	Path    string
	Version string
	Bytes   int64
}

// Latest fetches /releases/latest. It does not download the binary.
func (c *Client) Latest(ctx context.Context) (*Release, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), httpTimeout)
		defer cancel()
	}
	raw, err := c.get(ctx, c.api()+"/repos/"+c.repo()+"/releases/latest", true)
	if err != nil {
		return nil, err
	}
	var gh ghRelease
	if err := json.Unmarshal(raw, &gh); err != nil {
		return nil, fmt.Errorf("parse GitHub release: %w", err)
	}
	if gh.TagName == "" {
		return nil, fmt.Errorf("GitHub release has no tag")
	}
	wantBin := AssetName(gh.TagName, c.goos(), c.goarch())
	wantSum := ChecksumName(gh.TagName)
	rel := &Release{Tag: gh.TagName, HTMLURL: gh.HTMLURL}
	for _, a := range gh.Assets {
		switch a.Name {
		case wantBin:
			rel.AssetName = a.Name
			rel.AssetURL = a.BrowserURL
			rel.AssetDigest = digestHex(a.Digest)
		case wantSum, "checksums-" + gh.TagName + ".txt":
			rel.ChecksumName = a.Name
			rel.ChecksumURL = a.BrowserURL
		}
		if rel.ChecksumURL == "" && strings.HasPrefix(a.Name, "checksums-") && strings.HasSuffix(a.Name, ".txt") {
			rel.ChecksumName = a.Name
			rel.ChecksumURL = a.BrowserURL
		}
	}
	if rel.AssetURL == "" {
		return nil, fmt.Errorf("no %s in %s — this platform is not in the release", wantBin, gh.TagName)
	}
	if rel.ChecksumURL == "" {
		return nil, fmt.Errorf("no checksums file in %s — refusing to download an unverified binary", gh.TagName)
	}
	return rel, nil
}

// Check compares current (buildinfo.Version, or the string you pass) to Latest.
// A development / non-semver build is treated as older than any release.
func (c *Client) Check(ctx context.Context, current string) (*CheckResult, error) {
	if current == "" {
		current = buildinfo.Version
	}
	rel, err := c.Latest(ctx)
	if err != nil {
		return nil, err
	}
	out := &CheckResult{Current: current, Latest: rel.Tag, Release: rel}
	if IsRelease(current) {
		out.Newer = Compare(current, rel.Tag) < 0
	} else {
		out.Newer = true
	}
	return out, nil
}

// Apply downloads the release binary, verifies SHA-256 against the
// checksums file (and the GitHub asset digest when present), and
// replaces dest. dest should be os.Executable() in production.
func (c *Client) Apply(ctx context.Context, dest string, rel *Release) (*ApplyResult, error) {
	if dest == "" {
		return nil, fmt.Errorf("no destination binary")
	}
	if rel == nil {
		var err error
		rel, err = c.Latest(ctx)
		if err != nil {
			return nil, err
		}
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), applyTimeout)
		defer cancel()
	}
	sumBody, err := c.get(ctx, rel.ChecksumURL, false)
	if err != nil {
		return nil, fmt.Errorf("download checksums: %w", err)
	}
	want, err := ParseChecksums(sumBody, rel.AssetName)
	if err != nil {
		return nil, err
	}
	bin, err := c.get(ctx, rel.AssetURL, false)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", rel.AssetName, err)
	}
	got := sha256Hex(bin)
	if got != want {
		return nil, fmt.Errorf("checksum mismatch for %s: got %s want %s — not installing", rel.AssetName, got, want)
	}
	if rel.AssetDigest != "" && rel.AssetDigest != got {
		return nil, fmt.Errorf("GitHub asset digest mismatch for %s — not installing", rel.AssetName)
	}
	if err := ReplaceBinary(dest, bin); err != nil {
		return nil, err
	}
	return &ApplyResult{Path: dest, Version: rel.Tag, Bytes: int64(len(bin))}, nil
}

// AssetName matches install.sh / dist.sh: leetd-0.1.3-darwin-arm64.
func AssetName(tag, goos, goarch string) string {
	name := "leetd-" + Normalize(tag) + "-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// ChecksumName matches the release asset checksums-0.1.3.txt.
func ChecksumName(tag string) string {
	return "checksums-" + Normalize(tag) + ".txt"
}

// Normalize strips a leading v/V.
func Normalize(v string) string {
	v = strings.TrimSpace(v)
	return strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
}

// IsRelease reports a dotted numeric version (0.1.3 or 0.1).
func IsRelease(v string) bool {
	v = Normalize(v)
	parts := strings.Split(v, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return true
}

// Compare is -1 if a<b, 0 if equal, 1 if a>b. Non-releases compare as 0.0.0.
func Compare(a, b string) int {
	ap, bp := parseVer(a), parseVer(b)
	for i := 0; i < 3; i++ {
		if ap[i] < bp[i] {
			return -1
		}
		if ap[i] > bp[i] {
			return 1
		}
	}
	return 0
}

func parseVer(v string) [3]int {
	var out [3]int
	if !IsRelease(v) {
		return out
	}
	for i, p := range strings.Split(Normalize(v), ".") {
		n, _ := strconv.Atoi(p)
		out[i] = n
	}
	return out
}

// ParseChecksums finds the SHA-256 hex for asset in a shasum -a 256 file.
func ParseChecksums(raw []byte, asset string) (string, error) {
	base := filepath.Base(asset)
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if filepath.Base(fields[len(fields)-1]) != base {
			continue
		}
		sum := strings.ToLower(fields[0])
		if _, err := hex.DecodeString(sum); err != nil || len(sum) != 64 {
			return "", fmt.Errorf("bad checksum for %s", base)
		}
		return sum, nil
	}
	return "", fmt.Errorf("checksum for %s not in checksums file — not installing", base)
}

// ReplaceBinary writes data over dest. On Unix the running process keeps
// the old inode; the next start (or launchd KeepAlive restart) runs dest.
// Windows cannot replace a running .exe — the file is left as dest.new.
func ReplaceBinary(dest string, data []byte) error {
	if dest == "" {
		return fmt.Errorf("empty destination")
	}
	if resolved, err := filepath.EvalSymlinks(dest); err == nil {
		dest = resolved
	}
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o755)
	if fi, err := os.Stat(dest); err == nil {
		mode = fi.Mode()
	}

	tmp, err := os.CreateTemp(dir, ".leetd-update-*")
	if err != nil {
		return fmt.Errorf("temp file next to %s: %w (is the install directory writable?)", dest, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		newPath := dest + ".new"
		if err := os.Rename(tmpName, newPath); err != nil {
			return err
		}
		cleanup = false
		old := dest + ".old"
		_ = os.Remove(old)
		if err := os.Rename(dest, old); err != nil {
			return fmt.Errorf("downloaded to %s — quit LeetOffice and replace %s with that file", newPath, dest)
		}
		if err := os.Rename(newPath, dest); err != nil {
			_ = os.Rename(old, dest)
			return fmt.Errorf("downloaded to %s — quit LeetOffice and replace %s with that file", newPath, dest)
		}
		_ = os.Remove(old)
		return nil
	}

	old := dest + ".old"
	_ = os.Remove(old)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, old); err != nil {
			if rmErr := os.Remove(dest); rmErr != nil {
				return fmt.Errorf("replace %s: %w", dest, err)
			}
		}
	}
	if err := os.Rename(tmpName, dest); err != nil {
		_ = os.Rename(old, dest)
		return fmt.Errorf("install %s: %w", dest, err)
	}
	cleanup = false
	_ = os.Remove(old)
	return nil
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	HTMLURL string    `json:"html_url"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name       string `json:"name"`
	BrowserURL string `json:"browser_download_url"`
	Digest     string `json:"digest"`
}

func digestHex(d string) string {
	d = strings.TrimSpace(strings.ToLower(d))
	d = strings.TrimPrefix(d, "sha256:")
	if _, err := hex.DecodeString(d); err != nil || len(d) != 64 {
		return ""
	}
	return d
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (c *Client) http() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{
		Timeout: httpTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return fmt.Errorf("too many redirects")
			}
			return c.guardURL(req.URL)
		},
	}
}

func (c *Client) get(ctx context.Context, rawURL string, jsonAPI bool) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if err := c.guardURL(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "leetoffice/"+buildinfo.Version+" (user-initiated update)")
	if jsonAPI {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	} else {
		req.Header.Set("Accept", "application/octet-stream")
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxAssetBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxAssetBytes {
		return nil, fmt.Errorf("download too large (>%d bytes) — not installing", maxAssetBytes)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub %s: %s", res.Status, strings.TrimSpace(string(body)))
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty response from %s", u.Host)
	}
	return body, nil
}

func (c *Client) guardURL(u *url.URL) error {
	if u == nil {
		return fmt.Errorf("empty URL")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("refusing non-http URL")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("empty host")
	}
	// Tests point API at httptest (127.0.0.1 / ::1). Production only
	// talks to GitHub and its download CDN.
	if api, err := url.Parse(c.api()); err == nil {
		if h := strings.ToLower(api.Hostname()); h != "" && host == h {
			return nil
		}
	}
	switch host {
	case "api.github.com", "github.com", "objects.githubusercontent.com",
		"release-assets.githubusercontent.com":
		return nil
	}
	if strings.HasSuffix(host, ".githubusercontent.com") {
		return nil
	}
	// loopback is only valid when the API itself is loopback (tests)
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if api, err := url.Parse(c.api()); err == nil {
			if aip := net.ParseIP(api.Hostname()); aip != nil && aip.IsLoopback() {
				return nil
			}
		}
	}
	return fmt.Errorf("refusing host %s (updates only talk to GitHub)", host)
}
