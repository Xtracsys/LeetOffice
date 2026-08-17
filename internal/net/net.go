// Package net implements the LeetOffice node network (BUILD_SPEC §6, D8):
// a local CA issuing mTLS node identities, enrollment gated by a one-time
// team secret, mDNS discovery of `_leetoffice._tcp` peers, and the
// mutually-authenticated `leet://` git sync transport. Everything is
// LAN-local: membership is the CA-signed certificate, so a rogue node
// (no cert, or a cert from a foreign CA) is rejected during the TLS
// handshake and its traffic stays unreadable.
package net

import "strings"

// Scheme is the git remote URL scheme served by GitServer and installed
// client-side by InstallTransport (§6.4): leet://host:port/repo.git.
const Scheme = "leet"

// DefaultPort is the port assumed for leet:// URLs without an explicit one.
const DefaultPort = 7418

// DefaultRepoName is the bare-repo directory the coordinator creates and
// the path joiners request (leet://host:port/main.git). v0.1.0's wizard
// created LegacyRepoName instead; the git server still serves that name
// when /main.git is requested so already-enrolled teams keep working.
const (
	DefaultRepoName = "main.git"
	LegacyRepoName  = "main-share.git"
	DefaultRepoPath = "/" + DefaultRepoName
)

// RepoPath turns a repo directory name into the leet:// path ("main.git" → "/main.git").
func RepoPath(name string) string {
	name = strings.Trim(name, "/")
	if name == "" {
		name = DefaultRepoName
	}
	return "/" + name
}

// ShareRemote builds the joiner's main_share URL: leet://gitAddr/main.git.
// Empty gitPath falls back to DefaultRepoPath so older coordinators that
// only advertised a host:port still produce a usable remote.
func ShareRemote(gitAddr, gitPath string) string {
	if gitPath == "" {
		gitPath = DefaultRepoPath
	} else if !strings.HasPrefix(gitPath, "/") {
		gitPath = "/" + gitPath
	}
	return Scheme + "://" + gitAddr + gitPath
}
