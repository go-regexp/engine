package onigmo

import (
	"errors"
	"strings"
	"testing"
)

// A limited copy must share the lowered program's instruction stream, the DFA and
// the start filter with the original: rebuilding them per copy cost a scanning
// lexer about 10% (go-rouge #26), and the limit lives in a Program header copy.
func TestWithStackLimitSharesBuiltProgram(t *testing.T) {
	re := MustCompile(`(a|b)*c`)
	lim := re.WithStackLimit(1 << 20)
	if lim.m == re.m {
		t.Fatal("limited copy must have its own machine")
	}
	if &lim.m.prog.Insts[0] != &re.m.prog.Insts[0] {
		t.Error("instruction stream was rebuilt, not shared")
	}
	if lim.m.dfa != re.m.dfa || lim.m.start != re.m.start {
		t.Error("DFA or start filter was rebuilt, not shared")
	}
	if re.m.prog.StackLimit != 0 || lim.m.prog.StackLimit != 1<<20 {
		t.Errorf("limit leaked into the original: %d / %d", re.m.prog.StackLimit, lim.m.prog.StackLimit)
	}
	// Negative means unlimited.
	if MustCompile(`a`).WithStackLimit(-5).m.prog.StackLimit != 0 {
		t.Error("negative limit must mean unlimited")
	}
	// The limit still bites, and only on the copy.
	in := strings.Repeat("ab", 200000)
	if _, err := lim.FindStringSubmatchIndexAtErr(in, 0); !errors.Is(err, ErrStackLimit) {
		t.Errorf("limited copy: err = %v, want ErrStackLimit", err)
	}
	if _, err := re.FindStringSubmatchIndexAtErr(in, 0); err != nil {
		t.Errorf("original refused: %v", err)
	}
}

func BenchmarkLimitedVsUnlimited(b *testing.B) {
	in := "def foo(bar, baz):\n    return bar + baz\n"
	for _, c := range []struct {
		name string
		re   *Regexp
	}{
		{"unlimited", MustCompile(`\G(?:def|class)\b`)},
		{"limited", MustCompile(`\G(?:def|class)\b`).WithStackLimit(16 << 20)},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				c.re.FindStringSubmatchIndexAtErr(in, 0)
			}
		})
	}
}
