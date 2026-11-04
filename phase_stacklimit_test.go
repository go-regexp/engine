package onigmo

import (
	"errors"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

// TestWithStackLimitRefusesInsteadOfExhaustingMemory: 1 MiB of `'(\\.|.)*?'`
// needs ~475 MB of backtrack state unlimited; with a 16 MiB limit the match is
// abandoned with ErrStackLimit (Onigmo's ONIGERR_MATCH_STACK_LIMIT_OVER), the
// allocation stays within a small multiple of the limit, and the refusal is not
// a failed match: ok is false but err is set, whereas a pattern that genuinely
// does not match reports err == nil. The default (no limit) is unchanged.
func TestWithStackLimitRefusesInsteadOfExhaustingMemory(t *testing.T) {
	defer debug.SetMemoryLimit(debug.SetMemoryLimit(512 << 20))
	const limit = 16 << 20
	src := "'" + strings.Repeat(`\\ \' `, (1<<20)/6)

	re := MustCompile(`\G(?:'(\\.|.)*?')`).WithStackLimit(limit)
	var a, b runtime.MemStats
	runtime.ReadMemStats(&a)
	_, _, ok, err := re.MatchBoundsAtErr(src, 0)
	runtime.ReadMemStats(&b)
	if ok || !errors.Is(err, ErrStackLimit) || !errors.Is(err, ErrBudget) {
		t.Fatalf("ok=%v err=%v, want a refusal that is ErrStackLimit and ErrBudget", ok, err)
	}
	if alloc := b.TotalAlloc - a.TotalAlloc; alloc > 8*limit {
		t.Errorf("a refused match allocated %d bytes, want at most %d (8x the limit)", alloc, 8*limit)
	}
	if re.Timeout() != 0 {
		t.Errorf("WithStackLimit changed the timeout to %v", re.Timeout())
	}

	// Not a failed match: a pattern that does not match says so with err == nil.
	_, _, ok, err = re.MatchBoundsAtErr("abc", 0)
	if ok || err != nil {
		t.Errorf("a genuine non-match gave ok=%v err=%v", ok, err)
	}
	// A match within the limit is unaffected.
	if bb, e, ok, err := re.MatchBoundsAtErr(`'ab'`, 0); !ok || err != nil || bb != 0 || e != 4 {
		t.Errorf("small match = (%d,%d,%v,%v)", bb, e, ok, err)
	}
	// The receiver is untouched, and n <= 0 is unlimited (Onigmo: 0 == unlimited).
	base := MustCompile(`\G(?:'(\\.|.)*?')`)
	small := "'" + strings.Repeat(`\\ \' `, (64<<10)/6)
	for _, r := range []*Regexp{base, base.WithStackLimit(0), base.WithStackLimit(-1)} {
		if _, _, ok, err := r.MatchBoundsAtErr(small, 0); !ok || err != nil {
			t.Errorf("unlimited match = ok %v err %v", ok, err)
		}
	}
}
