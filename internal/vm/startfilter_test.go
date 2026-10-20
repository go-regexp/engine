package vm

import "testing"

// TestStartFilter checks each necessary condition of the anchored-start filter,
// and that it never rejects a position the VM matches from.
func TestStartFilter(t *testing.T) {
	cases := []struct {
		pat, input string
		pos        int
		want       bool
	}{
		{`\Aab`, "abab", 0, true},
		{`\Aab`, "abab", 2, false}, // \A past offset 0
		{`ab`, "xab", 1, true},     // literal prefix present
		{`ab`, "xab", 0, false},    // literal prefix absent
		{`ab`, "xa", 1, false},     // prefix cut by end of input
		{`[a-c]x|dx`, "dx", 0, true},
		{`[a-c]x|dx`, "ex", 0, false}, // first byte outside the set
		{`[a-c]+`, "ab", 2, false},    // end of input: no first byte
		{`(?<=a)b`, "ab", 1, true},    // lookbehind: nothing provable
		{`(?<=a)b`, "ab", 0, true},
	}
	for _, c := range cases {
		prog := build(t, c.pat)
		f := NewStartFilter(prog)
		if got := f.CanStartAt(c.input, c.pos); got != c.want {
			t.Errorf("%q at %d of %q: CanStartAt = %v, want %v", c.pat, c.pos, c.input, got, c.want)
		}
		if _, ok, _ := MatchAt(prog, c.input, c.pos, DefaultBudget); ok && !f.CanStartAt(c.input, c.pos) {
			t.Errorf("%q at %d of %q: filter rejects a position that matches", c.pat, c.pos, c.input)
		}
	}
}
