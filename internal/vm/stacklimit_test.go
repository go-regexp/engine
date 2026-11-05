package vm

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// lazyQuoted is the pathological shape of the public stack-limit test: a lazy
// loop around a capture group grows both the backtrack stack and the trail.
const lazyQuoted = `(?:'(\\.|.)*?')`

func lazyInput(n int) string {
	return "'" + strings.Repeat(`\\ \' `, n/6)
}

// heldBytes is what the machine's two backing slices occupy.
func heldBytes(m *machine) int {
	return cap(m.stack)*threadBytes + cap(m.trail)*trailBytes
}

// runLimited runs pat anchored at 0 on a machine with empty backing and the given
// limit, and returns the result plus the bytes the machine held at the end.
func runLimited(t *testing.T, pat, input string, limit int) (ok bool, err error, held int) {
	t.Helper()
	prog := build(t, pat)
	prog.StackLimit = limit
	m := acquireMachine(prog, input, math.MaxInt/2, time.Time{})
	m.stack, m.trail = nil, nil
	m.gpos = 0
	_, ok, err = m.run(0)
	held = heldBytes(m)
	releaseMachine(m)
	return ok, err, held
}

// slack is the constant of the overshoot bound: allocator size-class rounding of
// the first allocations and of the one growth that is refused one step later.
const slack = 16 << 10

// TestStackLimitOvershootBound: when ErrStackLimit fires, stack and trail together
// hold at most twice the limit plus slack bytes. The check passes only while held
// <= limit and a growth at most doubles one slice.
func TestStackLimitOvershootBound(t *testing.T) {
	in := lazyInput(256 << 10)
	for _, limit := range []int{1, 4 << 10, 64 << 10, 1 << 20} {
		ok, err, held := runLimited(t, lazyQuoted, in, limit)
		if ok || !errors.Is(err, ErrStackLimit) || !errors.Is(err, ErrBudget) {
			t.Fatalf("limit %d: ok=%v err=%v, want ErrStackLimit (wrapping ErrBudget)", limit, ok, err)
		}
		if held > 2*limit+slack {
			t.Errorf("limit %d: held %d bytes when the refusal fired, want <= %d", limit, held, 2*limit+slack)
		}
	}
}

// TestStackLimitJustUnderSucceeds: a match whose held state equals the limit
// succeeds, and the same match is refused once the limit is halfway down.
func TestStackLimitJustUnderSucceeds(t *testing.T) {
	in := lazyInput(8<<10) + "'"
	ok, err, peak := runLimited(t, lazyQuoted, in, 0)
	if !ok || err != nil {
		t.Fatalf("unlimited: ok=%v err=%v", ok, err)
	}
	if ok, err, _ := runLimited(t, lazyQuoted, in, peak); !ok || err != nil {
		t.Errorf("limit == peak held (%d): ok=%v err=%v, want a match", peak, ok, err)
	}
	if ok, err, _ := runLimited(t, lazyQuoted, in, peak/2); ok || !errors.Is(err, ErrStackLimit) {
		t.Errorf("limit == peak/2 (%d): ok=%v err=%v, want ErrStackLimit", peak/2, ok, err)
	}
}

// TestStackLimitRefusesFromEitherSlice: with a 1-byte limit the first growth of
// each slice passes (nothing held yet) and the second slice's growth is refused,
// so refusals come from both the stack and the trail sites whichever grows first.
func TestStackLimitRefusesFromEitherSlice(t *testing.T) {
	for _, pat := range []string{`(a)*b`, `(?:(a)|b)*c`, `a*(b)`, `(a)+?c`, `x*?(a)y`} {
		if ok, err, _ := runLimited(t, pat, "aaaaab", 1); ok || !errors.Is(err, ErrStackLimit) {
			t.Errorf("%s: ok=%v err=%v, want ErrStackLimit", pat, ok, err)
		}
	}
}

// TestStackLimitErrorIsNotSticky: the refusal flag is reset when a machine is
// reused, so a later unlimited match on it ends with the ordinary budget error
// rather than a stale ErrStackLimit.
func TestStackLimitErrorIsNotSticky(t *testing.T) {
	in := lazyInput(64 << 10)
	prog := build(t, lazyQuoted)
	prog.StackLimit = 1024
	m := acquireMachine(prog, in, math.MaxInt/2, time.Time{})
	if _, _, err := m.run(0); !errors.Is(err, ErrStackLimit) {
		t.Fatalf("first run: %v", err)
	}
	releaseMachine(m)
	prog2 := build(t, lazyQuoted)
	m = acquireMachine(prog2, in, 50, time.Time{})
	defer releaseMachine(m)
	if _, _, err := m.run(0); !errors.Is(err, ErrBudget) || errors.Is(err, ErrStackLimit) {
		t.Errorf("unlimited run with a 50-step budget: %v, want plain ErrBudget", err)
	}
}

// TestTrimToLimit: pooled backing larger than the limit is dropped (otherwise the
// grow-time check would never fire), smaller backing is kept, and no limit keeps
// everything.
func TestTrimToLimit(t *testing.T) {
	big := func() *machine {
		return &machine{stack: make([]thread, 0, 4096), trail: make([]trailEntry, 0, 4096)}
	}
	m := big()
	m.stackLimit = 1024
	m.trimToLimit()
	if m.stack != nil || m.trail != nil {
		t.Errorf("oversized backing kept under a 1 KiB limit: cap %d/%d", cap(m.stack), cap(m.trail))
	}
	m = big()
	m.stackLimit = heldBytes(m)
	m.trimToLimit()
	if cap(m.stack) != 4096 || cap(m.trail) != 4096 {
		t.Errorf("backing at exactly the limit was dropped")
	}
	m = big()
	m.trimToLimit()
	if cap(m.stack) != 4096 {
		t.Errorf("backing dropped without a limit")
	}
}

// BenchmarkAnchoredVMLimit compares VM-bound anchored matches with no limit and
// with a limit they never reach. "steps" is 1024 single-class steps with almost
// no backtrack state (the per-step cost of the old check is all there is);
// "lazy" is a lazy loop around a capture group that grows stack and trail. The
// two limits must not differ: the limit is checked only on growth.
func BenchmarkAnchoredVMLimit(b *testing.B) {
	for _, shape := range []struct {
		name, pat, in string
	}{
		{"steps", strings.Repeat(`[a-h]`, 1024), strings.Repeat("abcdefgh", 128)},
		{"lazy", lazyQuoted, lazyInput(32<<10) + "'"},
	} {
		for _, c := range []struct {
			name  string
			limit int
		}{{"nolimit", 0}, {"limit1GiB", 1 << 30}} {
			b.Run(shape.name+"/"+c.name, func(b *testing.B) {
				prog := build(b, shape.pat)
				prog.StackLimit = c.limit
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, ok, err := MatchAt(prog, shape.in, 0, DefaultBudget); !ok || err != nil {
						b.Fatalf("ok=%v err=%v", ok, err)
					}
				}
			})
		}
	}
}
