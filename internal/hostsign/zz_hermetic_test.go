package hostsign

import (
	"testing"

	"leetoffice/internal/testenv"
)

// TestMain hermetically isolates every home/config lookup for this test
// binary (FR-012): no test in this package can touch the operator's real
// files. See internal/testenv.
func TestMain(m *testing.M) { testenv.Isolate(m) }
