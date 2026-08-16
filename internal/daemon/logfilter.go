// Log hygiene for the node's console: the hashicorp/mdns library logs an
// error every time it cannot bind IPv6 multicast (ff02::fb) — machines
// without IPv6 multicast do this on every announce/discovery refresh while
// IPv4 mDNS keeps working perfectly. Filter those two known-benign lines and
// say so once, so an operator's console shows real signal.
package daemon

import (
	"bytes"
	"log"
	"os"
	"sync"
)

var (
	quietOnce sync.Once
	quietMu   sync.Mutex
)

type noiseFilter struct{ w *os.File }

var benignSnippets = [][]byte{
	[]byte("Failed to bind to udp6 port"),
	[]byte("Failed to listen to both unicast and multicast on IPv6"),
}

func (n noiseFilter) Write(p []byte) (int, error) {
	for _, snip := range benignSnippets {
		if bytes.Contains(p, snip) {
			quietOnce.Do(func() {
				log.Print("net: IPv6 multicast unavailable on this machine — mDNS continues over IPv4 (harmless)")
			})
			return len(p), nil // swallow the repeat
		}
	}
	return n.w.Write(p)
}

// quietKnownNoise routes the standard logger through the filter. Call once
// from the daemon entrypoint before any networking starts.
func quietKnownNoise() {
	quietMu.Lock()
	defer quietMu.Unlock()
	log.SetOutput(noiseFilter{w: os.Stderr})
}
