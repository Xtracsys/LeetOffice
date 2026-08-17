package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type fakeAnnouncer struct{ shut atomic.Bool }

func (f *fakeAnnouncer) Shutdown() error {
	f.shut.Store(true)
	return nil
}

// TestKeepAnnounceUntilCancel is the client-mDNS gate: startClient used to
// Announce then immediately Shutdown, so presence never lasted. The helper
// must not shut down until the process context is cancelled.
func TestKeepAnnounceUntilCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeAnnouncer{}
	keepAnnounce(ctx, f)

	select {
	case <-time.After(30 * time.Millisecond):
	case <-ctx.Done():
	}
	if f.shut.Load() {
		t.Fatal("announce shut down before context cancel")
	}

	cancel()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if f.shut.Load() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("announce not shut down after context cancel")
}
