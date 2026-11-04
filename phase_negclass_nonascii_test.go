package onigmo

import "testing"

// TestNegatedClassEscapeInsideClassOnNonASCII pins \D \W \S \H used INSIDE a
// bracket class on a UTF-8 string. Each row is MRI's own answer (ruby 2.6.10,
// `Regexp.new(pat) =~ s`; nil is -1 here): in Ruby \d \w \s \h are ASCII-only,
// so their complements hold every non-ASCII code point.
//
// The parser used to complement the set over the bytes 0x00-0xFF only, so a
// code point above U+00FF (π, 語) was in neither the escape nor its complement:
// [^\S\n] matched "π" as if it were whitespace, and [\S] refused it.
func TestNegatedClassEscapeInsideClassOnNonASCII(t *testing.T) {
	for _, c := range []struct {
		pat, s string
		at     int
	}{
		{`[^\S\n]`, "π", -1}, {`[^\S\n]`, " ", 0}, {`[^\S\n]`, "\n", -1},
		{`[^\S\n]`, "é", -1}, {`[^\S\n]`, "a", -1},
		{`[\S]`, "π", 0}, {`[\S]`, " ", -1}, {`[\S]`, "é", 0},
		{`[^\S]`, "π", -1}, {`[^\S]`, " ", 0},
		{`[\D]`, "π", 0}, {`[^\D]`, "π", -1}, {`[^\D]`, "5", 0},
		{`[\W]`, "π", 0}, {`[^\W]`, "π", -1}, {`[\W]`, "a", -1},
		{`[\H]`, "π", 0}, {`[^\H]`, "π", -1}, {`[^\H]`, "f", 0},
		{`[a\S]`, "語", 0}, {`[^a\S]`, "語", -1}, {`[^\s\S]`, "π", -1},
	} {
		re, err := Compile(c.pat)
		if err != nil {
			t.Fatalf("%s: %v", c.pat, err)
		}
		at := -1
		if b, _, ok := re.MatchBounds(c.s); ok {
			at = b
		}
		if at != c.at {
			t.Errorf("%s on %q: match at %d, MRI says %d", c.pat, c.s, at, c.at)
		}
	}
}

// FuzzNegatedClassEscapeEqualsLookahead states the identity the bug broke:
// [^\S\n] is whitespace except newline, so it must agree with the spelling
// without a double negation, on any input.
func FuzzNegatedClassEscapeEqualsLookahead(f *testing.F) {
	for _, s := range []string{"", " ", "π", " π", "2 * π", "語 \n x", "\xff", "a\tb", " ", "é "} {
		f.Add(s)
	}
	// Anchored, like a StringScanner#skip: the unanchored search takes another
	// path through the engine and did not show the defect.
	a := MustCompile(`\G[^\S\n]+`)
	b := MustCompile(`\G(?:(?!\n)\s)+`)
	f.Fuzz(func(t *testing.T, s string) {
		ab, ae, aok := a.MatchBoundsAt(s, 0)
		bb, be, bok := b.MatchBoundsAt(s, 0)
		if aok != bok || (aok && (ab != bb || ae != be)) {
			t.Fatalf("%q: [^\\S\\n]+ gave (%d,%d,%v), (?:(?!\\n)\\s)+ gave (%d,%d,%v)", s, ab, ae, aok, bb, be, bok)
		}
	})
}
