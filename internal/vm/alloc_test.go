package vm

import (
	"strings"
	"testing"
	"time"
)

// TestMatchAtAllocsIndependentOfInputLength is the regression test for
// go-regexp/engine#10: a cursor-anchored match with a lookaround inside a long
// buffer used to allocate a fresh (pc × whole-input) memo on every lookaround
// evaluation. Now a warm call allocates only the returned capture slice, however
// long the input.
func TestMatchAtAllocsIndependentOfInputLength(t *testing.T) {
	prog := build(t, `\G(?:(?<![a-z])(?=[a-z]+\()[a-z]+|\s+)`)
	for _, n := range []int{100, 100_000} {
		input := strings.Repeat("foo(bar) ", n)
		pos := len(input) / 2
		pos -= pos % 9 // at a "foo"
		if _, ok, err := MatchAt(prog, input, pos, DefaultBudget); !ok || err != nil {
			t.Fatalf("n=%d: no match at %d (%v)", n, pos, err)
		}
		if raceEnabled {
			continue // sync.Pool drops Puts at random under -race
		}
		allocs := testing.AllocsPerRun(50, func() {
			_, _, _ = MatchAt(prog, input, pos, DefaultBudget)
		})
		if allocs > 1 {
			t.Errorf("n=%d: %v allocations per MatchAt, want at most 1 (the result)", n, allocs)
		}
	}
}

// TestNestedLookFramesRetained covers the release path that drops lookaround
// frames past maxRetainedLookFrames, and checks that a deeply nested lookaround
// still matches correctly from a machine that kept only some frames.
func TestNestedLookFramesRetained(t *testing.T) {
	pat := "a"
	for range maxRetainedLookFrames + 2 {
		pat = "(?=" + pat + ")"
	}
	prog := build(t, pat+"a")
	for range 3 {
		res, ok, err := MatchAt(prog, "ba", 1, DefaultBudget)
		if err != nil || !ok || res[0] != 1 || res[1] != 2 {
			t.Fatalf("nested lookahead: %v %v %v", res, ok, err)
		}
	}
	m := acquireMachine(prog, "ba", DefaultBudget, time.Time{})
	if _, ok, _ := m.run(1); !ok {
		t.Fatal("nested lookahead did not match")
	}
	if len(m.looks) <= maxRetainedLookFrames {
		t.Fatalf("expected more than %d frames, got %d", maxRetainedLookFrames, len(m.looks))
	}
	releaseMachine(m)
	if len(m.looks) != maxRetainedLookFrames {
		t.Fatalf("release kept %d frames, want %d", len(m.looks), maxRetainedLookFrames)
	}
}

// FuzzMemoGenModel drives the memo with random operations — init with a new
// program size, reset at a new base, clearAll, set and test at positions before,
// inside and far past the dense window — and checks every answer against a plain
// map model of the same set.
func FuzzMemoGenModel(f *testing.F) {
	f.Add([]byte{0, 3, 1, 4, 2, 9, 3, 200, 1, 4})
	f.Add([]byte{2, 50, 3, 255, 3, 0, 1, 7, 0, 2})
	f.Fuzz(func(t *testing.T, ops []byte) {
		var b memoGen
		nPC := 7
		b.init(nPC, true)
		model := map[[2]int]bool{}
		for i := 0; i+1 < len(ops); i += 2 {
			arg := int(ops[i+1])
			switch ops[i] % 5 {
			case 0: // new program, same backing
				nPC = arg%300 + 1
				b.init(nPC, true)
				clear(model)
			case 1: // new attempt at a base
				b.reset(arg * 3)
				clear(model)
			case 2:
				b.clearAll()
				clear(model)
			case 3, 4: // set, then the far position that crosses the cap
				pc, sp := arg%nPC, arg*arg*7
				if ops[i]%5 == 4 {
					sp = b.base + maxDenseMemoCells/nPC + arg
				}
				b.set(pc, sp)
				model[[2]int{pc, sp}] = true
			}
			for k := range model {
				if !b.test(k[0], k[1]) {
					t.Fatalf("op %d: (%d,%d) set but not marked", i, k[0], k[1])
				}
			}
			for sp := range 64 {
				for pc := range min(nPC, 8) {
					if b.test(pc, sp) != model[[2]int{pc, sp}] {
						t.Fatalf("op %d: (%d,%d) marked=%v, model %v", i, pc, sp, b.test(pc, sp), model[[2]int{pc, sp}])
					}
				}
			}
		}
	})
}

// BenchmarkMatchAtLookaroundLongInput is the tokenizer shape of #10: an anchored
// rule with lookarounds tried at a cursor inside a 90 KB buffer.
func BenchmarkMatchAtLookaroundLongInput(b *testing.B) {
	prog := build(b,`\G(?:(?<![a-z])(?=[a-z]+\()[a-z]+|\s+)`)
	input := strings.Repeat("foo(bar) ", 10_000)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		_, _, _ = MatchAt(prog, input, (i%9_999)*9, DefaultBudget)
	}
}
