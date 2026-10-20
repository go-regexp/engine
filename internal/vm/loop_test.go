package vm

import (
	"strings"
	"testing"

	"github.com/go-regexp/engine/internal/compile"
)

// TestFusedLoopAtomKinds exercises every atom kind a fused OpLoop can repeat
// (OpChar, OpClass, OpAny, OpUniProp, OpFoldChar), so loopAtomStep's full
// dispatch is covered and each accepts exactly what its unfused atom would.
func TestFusedLoopAtomKinds(t *testing.T) {
	for _, tc := range []struct {
		pat, in    string
		begin, end int
		ok         bool
	}{
		{`a+`, "aaab", 0, 3, true},       // OpChar
		{`[a-c]+`, "abcd", 0, 3, true},   // OpClass
		{`.+`, "xyz", 0, 3, true},        // OpAny (greedy dot)
		{`\p{L}+`, "héllo!", 0, 6, true}, // OpUniProp (é is two bytes)
		{`(?i)a+`, "AaAaB", 0, 4, true},  // OpFoldChar (case-insensitive)
		{`\p{N}+`, "12ab", 0, 2, true},   // OpUniProp digits
		{`[^x]+`, "abxc", 0, 2, true},    // negated class loop
	} {
		b, e, ok := matchSpan(t, tc.pat, tc.in)
		if ok != tc.ok || (ok && (b != tc.begin || e != tc.end)) {
			t.Errorf("/%s/ on %q = (%d,%d,%v) want (%d,%d,%v)",
				tc.pat, tc.in, b, e, ok, tc.begin, tc.end, tc.ok)
		}
	}
}

// TestFusedLoopLazyGiveBack drives a lazy fused loop through every take-more
// step, including the exhaustion at the longest run (loopResume's lazy branch).
// a+? is lazy: it matches the fewest a's that let the rest succeed; b after a
// run of a's forces it to take all of them, and a non-matching tail forces it to
// exhaust the loop.
func TestFusedLoopLazyGiveBack(t *testing.T) {
	for _, tc := range []struct {
		pat, in    string
		begin, end int
		ok         bool
	}{
		{`a+?b`, "aaab", 0, 4, true},        // lazy must grow to consume all a's then b
		{`a+?c`, "aaab", -1, -1, false},     // no c: lazy take-more exhausts, no match
		{`a*?b`, "b", 0, 1, true},           // lazy star takes zero
		{`a{2,5}?b`, "aaaab", 0, 5, true},   // bounded lazy grows from 2 to 4
		{`a{2,5}?x`, "aaaa", -1, -1, false}, // bounded lazy exhausts at max
	} {
		b, e, ok := matchSpan(t, tc.pat, tc.in)
		if ok != tc.ok || (ok && (b != tc.begin || e != tc.end)) {
			t.Errorf("/%s/ on %q = (%d,%d,%v) want (%d,%d,%v)",
				tc.pat, tc.in, b, e, ok, tc.begin, tc.end, tc.ok)
		}
	}
}

// TestFusedLoopGreedyGiveBack drives a greedy fused loop through every give-back
// step down to its floor (loopResume's greedy branch and its Min cutoff).
func TestFusedLoopGreedyGiveBack(t *testing.T) {
	for _, tc := range []struct {
		pat, in    string
		begin, end int
		ok         bool
	}{
		{`a+a`, "aaaa", 0, 4, true},      // greedy gives back one a for the trailing a
		{`a{2,4}a`, "aaaaa", 0, 5, true}, // bounded greedy gives back to fit
		{`a{2,4}a`, "aa", -1, -1, false}, // cannot satisfy floor+1, exhausts to Min
	} {
		b, e, ok := matchSpan(t, tc.pat, tc.in)
		if ok != tc.ok || (ok && (b != tc.begin || e != tc.end)) {
			t.Errorf("/%s/ on %q = (%d,%d,%v) want (%d,%d,%v)",
				tc.pat, tc.in, b, e, ok, tc.begin, tc.end, tc.ok)
		}
	}
}

// TestFusedLoopBudget verifies a fused loop charges the deterministic step budget
// per atom scanned, so a long run cannot evade the ReDoS/time bound: a tiny
// budget against a long run trips ErrBudget rather than silently completing.
func TestFusedLoopBudget(t *testing.T) {
	long := strings.Repeat("a", 1000)
	if _, _, err := Match(build(t, `a+`), long, 8); err == nil {
		t.Fatal("a fused loop over a long run must exhaust a tiny budget")
	}
	// An ample budget completes.
	if _, ok, err := Match(build(t, `a+`), long, DefaultBudget); err != nil || !ok {
		t.Fatalf("a+ on a long run with ample budget: ok=%v err=%v", ok, err)
	}
}

// TestMemoGenSparseBacking exercises the sparse-map backing of the (pc, sp) memo,
// taken when the dense window would grow past the cap or reach a position before
// its base. It drives test/set/clearAll on the sparse path directly, and checks
// that the marks already in the dense window move with the switch.
func TestMemoGenSparseBacking(t *testing.T) {
	var b memoGen
	b.init(1024, true)
	b.reset(10)
	b.set(4, 10) // dense, row 0
	if b.sparseOn {
		t.Fatal("a mark at base must stay in the dense window")
	}
	far := 10 + maxDenseMemoCells/1024 // first row past the cap
	b.set(3, far)
	if !b.sparseOn {
		t.Fatal("a mark past the dense cap must switch to the sparse map")
	}
	if !b.test(4, 10) || !b.test(3, far) {
		t.Fatal("marks must survive the switch to the sparse map")
	}
	if b.test(3, 7) {
		t.Fatal("unset (3,7) must not be marked")
	}
	b.clearAll()
	if b.test(3, far) || b.test(4, 10) {
		t.Fatal("clearAll must forget the marks")
	}
	// A position before the base also takes the sparse path.
	b.reset(10)
	if b.sparseOn {
		t.Fatal("reset must return to the dense window")
	}
	b.set(2, 5)
	if !b.sparseOn || !b.test(2, 5) || b.test(2, 6) {
		t.Fatal("a mark before base must be kept in the sparse map")
	}
}

// TestMemoGenDenseReuse exercises the dense-backing reuse path: the window grows
// on demand, and a later init/reset reuses the backing rather than reallocating,
// whatever the program size and the base.
func TestMemoGenDenseReuse(t *testing.T) {
	var b memoGen
	b.init(16, true)
	if b.gen != nil || b.rows != 0 {
		t.Fatal("init must allocate nothing")
	}
	b.set(5, 3)
	if b.gen == nil || !b.test(5, 3) {
		t.Fatal("set must grow the dense window and mark the cell")
	}
	first := &b.gen[:1][0]
	b.init(8, true)
	b.reset(1000)
	if b.test(5, 3) || b.test(5, 1003) {
		t.Fatal("re-init must forget earlier marks")
	}
	b.set(7, 1000)
	if &b.gen[:1][0] != first || !b.test(7, 1000) {
		t.Fatal("a fitting re-init must reuse the dense slice")
	}
	// Growing keeps the marks already set.
	b.set(1, 1000+10*minDenseMemoRows)
	if !b.test(7, 1000) || !b.test(1, 1000+10*minDenseMemoRows) {
		t.Fatal("growth must keep the live marks")
	}
	// An inactive (no-split) program leaves the memo untouched and allocates nothing.
	var c memoGen
	c.init(10, false)
	if c.active {
		t.Fatal("a split-free program must leave the memo inactive")
	}
	c.clearAll() // no-op on an inactive memo
	c.reset(3)   // no-op too
}

// TestMemoGenStaleCellsPastRows checks that cells beyond the window reused from
// an earlier, larger attempt never read as marked: they hold old stamps.
func TestMemoGenStaleCellsPastRows(t *testing.T) {
	var b memoGen
	b.init(4, true)
	b.reset(0)
	for sp := range 100 {
		b.set(sp%4, sp)
	}
	b.init(3, true) // different program: same backing, new layout
	b.reset(0)
	for sp := range 100 {
		for pc := range 3 {
			if b.test(pc, sp) {
				t.Fatalf("stale cell (%d,%d) read as marked", pc, sp)
			}
		}
	}
}

// TestMemoGenBumpWraparound forces the generation counter to wrap to zero, on both
// the dense and sparse backings, so the reset-and-restart-at-1 path is covered. A
// stale stamp must never collide with the live generation after a wrap.
func TestMemoGenBumpWraparound(t *testing.T) {
	// Dense: mark a cell at a generation, then wrap. After wrap the old stamp must
	// no longer read as marked.
	var d memoGen
	d.init(4, true)
	d.set(1, 1)
	d.set(1, 200) // grow, so cells past the first window exist in the backing
	// Drive the live stamp up to the wrap boundary, then bump: cur++ overflows to 0,
	// which triggers the reset (clear the slice) and restart at generation 1. The
	// stale mark written above (at the old high generation) must not survive the
	// reset, so it must read as unmarked under the fresh generation.
	d.cur = ^uint32(0)
	d.bump()
	if d.cur != 1 {
		t.Fatalf("dense wrap must restart at generation 1, got %d", d.cur)
	}
	if d.test(1, 1) || d.test(1, 200) {
		t.Fatal("dense wrap must clear the stale mark")
	}
	// The whole backing was zeroed, up to its capacity: no cell holds stamp 1.
	for i, s := range d.gen[:cap(d.gen)] {
		if s != 0 {
			t.Fatalf("dense wrap left cell %d = %d", i, s)
		}
	}

	// Sparse: same, on the map backing.
	var s memoGen
	s.init(1024, true)
	s.set(2, maxDenseMemoCells) // past the dense cap: sparse
	s.set(2, 2)
	s.cur = ^uint32(0)
	s.bump()
	if s.cur != 1 {
		t.Fatalf("sparse wrap must restart at generation 1, got %d", s.cur)
	}
	if s.test(2, 2) {
		t.Fatal("sparse wrap must clear the stale mark")
	}
}

// TestLoopAtomStepUnknownSub covers loopAtomStep's defensive default arm: a
// loop whose Sub is not one of the five consuming-atom opcodes the compiler ever
// emits (only reachable via a malformed instruction) matches nothing. This can
// never arise from the compiler, so it is exercised directly.
func TestLoopAtomStepUnknownSub(t *testing.T) {
	m := &machine{input: "aaa"}
	// OpMatch is not a consuming atom, so the default arm returns (false, 0).
	if ok, w := m.loopAtomStep(compile.Inst{Sub: compile.OpMatch}, 0); ok || w != 0 {
		t.Fatalf("unknown Sub must match nothing, got (%v,%d)", ok, w)
	}
}

// TestFusedLoopInLookbehind covers a fused loop inside a lookbehind body (the
// execLook OpLoop arm), including the success and failure outcomes.
func TestFusedLoopInLookbehind(t *testing.T) {
	if !matchString(t, `(?<=a{3})b`, "aaab") {
		t.Fatal(`(?<=a{3})b should match "aaab"`)
	}
	if matchString(t, `(?<=a{3})b`, "aab") {
		t.Fatal(`(?<=a{3})b should not match "aab" (only two a's)`)
	}
}
