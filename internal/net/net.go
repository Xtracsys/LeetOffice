// Package net implements the LeetOffice node network (BUILD_SPEC §6, D8):
// a local CA issuing mTLS node identities, enrollment gated by a one-time
// team secret, mDNS discovery of `_leetoffice._tcp` peers, and the
// mutually-authenticated `leet://` git sync transport. Everything is
// LAN-local: membership is the CA-signed certificate, so a rogue node
// (no cert, or a cert from a foreign CA) is rejected during the TLS
// handshake and its traffic stays unreadable.
package net

// Scheme is the git remote URL scheme served by GitServer and installed
// client-side by InstallTransport (§6.4): leet://host:port/repo.git.
const Scheme = "leet"

// DefaultPort is the port assumed for leet:// URLs without an explicit one.
const DefaultPort = 7418
