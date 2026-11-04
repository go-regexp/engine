package onigmo

import (
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

// TestBacktrackTrailAllocationIsBounded pins the memory a long capture-group
// loop costs. `'(\\.|.)*?'` over 256 KiB with no dotall makes the engine record
// two capture writes and one backtrack alternative per character; the undo
// trail used to spend 64 bytes on every capture write (it embedded a call
// frame), so the FIRST match on a cold machine allocated ~320 MB for 256 KiB
// (~1.2 KB per input byte; go-rouge CI saw 250 MB for the same input) and ~900 MB
// for 1 MiB, reachable from any highlighted README. The trail entry is 16 bytes
// now (the frame lives in a side slice), which measures ~110 MB (~420 B/byte).
// That is still linear in the input -- a backtracking engine keeps an
// alternative and the capture undo-log per iteration -- so this pins the
// constant, not an absolute bound. The limit sits between the two, and a soft
// memory limit is set so a regression degrades the test instead of the machine.
func TestBacktrackTrailAllocationIsBounded(t *testing.T) {
	defer debug.SetMemoryLimit(debug.SetMemoryLimit(1 << 30))
	re := MustCompile(`\G(?:'(\\.|.)*?')`)
	src := "'" + strings.Repeat(`\\ \' `, (256<<10)/6)
	// The FIRST match is the one measured: a pooled machine keeps the buffers
	// it grew, so a second match reuses them and allocates almost nothing (32
	// bytes), which would hide the growth this test is about. (Run after other
	// tests that already grew a pooled machine this can only pass, never fail.)
	var a, b runtime.MemStats
	runtime.ReadMemStats(&a)
	_, e, ok := re.MatchBoundsAt(src, 0)
	runtime.ReadMemStats(&b)
	if !ok || e != len(src)-1 {
		t.Fatalf("match = (%d, %v), want the whole string", e, ok)
	}
	best := b.TotalAlloc - a.TotalAlloc
	t.Logf("%d bytes allocated for %d input bytes (%.0f per byte)", best, len(src), float64(best)/float64(len(src)))
	if limit := uint64(200 << 20); best > limit {
		t.Errorf("matching %d bytes allocated %d bytes, limit %d", len(src), best, limit)
	}
}
