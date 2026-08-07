package onigmo

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// --- Compile / MustCompile -------------------------------------------------

func TestCompileError(t *testing.T) {
	if _, err := Compile(`(`); err == nil {
		t.Fatal("Compile of an unbalanced group should error")
	}
	if _, err := CompileEnc(`[`, ASCII8BIT); err == nil {
		t.Fatal("CompileEnc of an unterminated class should error")
	}
}

func TestMustCompile(t *testing.T) {
	re := MustCompile(`a+`)
	if re.String() != `a+` {
		t.Fatalf("String() = %q, want %q", re.String(), `a+`)
	}
	if !MustCompileEnc("\xff+", ASCII8BIT).MatchString("a\xff\xffb") {
		t.Fatal("MustCompileEnc(ASCII8BIT) should match the raw byte run")
	}
}

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s did not panic on a bad pattern", name)
		}
	}()
	f()
}

func TestMustCompilePanics(t *testing.T) {
	mustPanic(t, "MustCompile", func() { MustCompile(`(`) })
	mustPanic(t, "MustCompileEnc", func() { MustCompileEnc(`(`, UTF8) })
}

// --- Introspection ---------------------------------------------------------

func TestEncoding(t *testing.T) {
	if MustCompile(`.`).Encoding() != UTF8 {
		t.Fatal("default encoding should be UTF8")
	}
	if MustCompileEnc(`.`, ASCII8BIT).Encoding() != ASCII8BIT {
		t.Fatal("ASCII8BIT encoding should round-trip")
	}
}

func TestSubexp(t *testing.T) {
	re := MustCompile(`(?<year>\d{4})-(\d{2})`)
	if re.NumSubexp() != 2 {
		t.Fatalf("NumSubexp = %d, want 2", re.NumSubexp())
	}
	names := re.SubexpNames()
	want := []string{"", "year", ""}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("SubexpNames = %v, want %v", names, want)
	}
	if got := re.SubexpIndex("year"); got != 1 {
		t.Fatalf("SubexpIndex(year) = %d, want 1", got)
	}
	if got := re.SubexpIndex("nope"); got != -1 {
		t.Fatalf("SubexpIndex(nope) = %d, want -1", got)
	}
	if got := re.SubexpIndex(""); got != -1 {
		t.Fatalf("SubexpIndex(\"\") = %d, want -1", got)
	}
}

// --- MatchString / Match ---------------------------------------------------

func TestMatchString(t *testing.T) {
	re := MustCompile(`\d+`)
	if !re.MatchString("abc123") {
		t.Fatal("should match a digit run")
	}
	if re.MatchString("abc") {
		t.Fatal("should not match a digit-free string")
	}
	if !re.Match([]byte("x9")) {
		t.Fatal("Match([]byte) should match a digit run")
	}
	if re.Match([]byte("xyz")) {
		t.Fatal("Match([]byte) should not match a digit-free slice")
	}
}

// A lookaround pattern is outside the DFA subset, so it exercises the
// backtracking-VM path of every entry point below.
func TestMatchStringLookaround(t *testing.T) {
	re := MustCompile(`foo(?=bar)`)
	if !re.MatchString("foobar") {
		t.Fatal("lookahead should match foobar")
	}
	if re.MatchString("foobaz") {
		t.Fatal("lookahead should not match foobaz")
	}
}

// A \B (non-word-boundary) assertion in a lookaround-free pattern is DFA-eligible,
// so this drives the DFA closure walk over the non-word-boundary assertion node.
func TestNonWordBoundaryDFA(t *testing.T) {
	// A class-led pattern carries no required literal, so BuildDFA keeps the DFA
	// (it bows out only when a literal prefix/interior anchor is present). \B\w then
	// forces the DFA closure over the non-word-boundary assertion node.
	re := MustCompile(`\B\w`)
	// In "ab" the position between 'a' and 'b' is not a word boundary, so \B\w
	// matches "b" at [1,2] (position 0 is a boundary and is skipped).
	if loc := re.FindStringIndex("ab"); !reflect.DeepEqual(loc, []int{1, 2}) {
		t.Fatalf("\\B\\w on ab = %v, want [1 2]", loc)
	}
	// "a b": every word char sits at a boundary, so \B\w never matches.
	if re.MatchString("a b") {
		t.Fatal("\\B\\w should not match when every word char is at a boundary")
	}
}

// --- Bounds ----------------------------------------------------------------

func TestMatchBounds(t *testing.T) {
	re := MustCompile(`\d+`)
	b, e, ok := re.MatchBounds("ab123cd")
	if !ok || b != 2 || e != 5 {
		t.Fatalf("MatchBounds = %d,%d,%v want 2,5,true", b, e, ok)
	}
	if _, _, ok := re.MatchBounds("nope"); ok {
		t.Fatal("MatchBounds should report no match")
	}
	// Backref pattern: DFA is nil, so this drives the VM bounds path.
	br := MustCompile(`(\w)\1`)
	if _, _, ok := br.MatchBounds("xy"); ok {
		t.Fatal("backref bounds: no doubled char, expected no match")
	}
	if b, e, ok := br.MatchBounds("axxb"); !ok || b != 1 || e != 3 {
		t.Fatalf("backref bounds = %d,%d,%v want 1,3,true", b, e, ok)
	}
}

func TestMatchBoundsAt(t *testing.T) {
	re := MustCompile(`\d+`)
	// Anchored at pos 2 (start of "123"): matches.
	if b, e, ok := re.MatchBoundsAt("ab123", 2); !ok || b != 2 || e != 5 {
		t.Fatalf("MatchBoundsAt(2) = %d,%d,%v want 2,5,true", b, e, ok)
	}
	// Anchored at pos 0: "a" is not a digit -> no match, does not scan forward.
	if _, _, ok := re.MatchBoundsAt("ab123", 0); ok {
		t.Fatal("MatchBoundsAt(0) should not scan forward")
	}
	if _, _, ok := re.MatchBoundsAt("ab", -1); ok {
		t.Fatal("negative pos should be out of range")
	}
	if _, _, ok := re.MatchBoundsAt("ab", 3); ok {
		t.Fatal("pos past len should be out of range")
	}
	// VM path (backref) anchored.
	br := MustCompile(`(\w)\1`)
	if b, e, ok := br.MatchBoundsAt("xxy", 0); !ok || b != 0 || e != 2 {
		t.Fatalf("backref MatchBoundsAt = %d,%d,%v want 0,2,true", b, e, ok)
	}
	if _, _, ok := br.MatchBoundsAt("xy", 0); ok {
		t.Fatal("backref MatchBoundsAt should not match xy")
	}
}

// --- Find (leftmost) -------------------------------------------------------

func TestFindString(t *testing.T) {
	re := MustCompile(`\d+`)
	if got := re.FindString("ab123cd456"); got != "123" {
		t.Fatalf("FindString = %q want 123", got)
	}
	if got := re.FindString("none"); got != "" {
		t.Fatalf("FindString(no match) = %q want empty", got)
	}
	if loc := re.FindStringIndex("ab123"); !reflect.DeepEqual(loc, []int{2, 5}) {
		t.Fatalf("FindStringIndex = %v want [2 5]", loc)
	}
	if loc := re.FindStringIndex("none"); loc != nil {
		t.Fatalf("FindStringIndex(no match) = %v want nil", loc)
	}
}

func TestFindStringSubmatch(t *testing.T) {
	re := MustCompile(`(\d{4})-(\d{2})`)
	idx := re.FindStringSubmatchIndex("date 2026-08 end")
	if !reflect.DeepEqual(idx, []int{5, 12, 5, 9, 10, 12}) {
		t.Fatalf("FindStringSubmatchIndex = %v", idx)
	}
	if re.FindStringSubmatchIndex("no date") != nil {
		t.Fatal("FindStringSubmatchIndex(no match) should be nil")
	}
	sub := re.FindStringSubmatch("2026-08")
	if !reflect.DeepEqual(sub, []string{"2026-08", "2026", "08"}) {
		t.Fatalf("FindStringSubmatch = %v", sub)
	}
	if re.FindStringSubmatch("nope") != nil {
		t.Fatal("FindStringSubmatch(no match) should be nil")
	}
	// Non-participating group: the second alternative leaves group 1 unset.
	alt := MustCompile(`(a)|(b)`)
	got := alt.FindStringSubmatch("b")
	if !reflect.DeepEqual(got, []string{"b", "", "b"}) {
		t.Fatalf("alt FindStringSubmatch = %v want [b \"\" b]", got)
	}
}

func TestFindStringSubmatchIndexAt(t *testing.T) {
	re := MustCompile(`(\d{2}):(\d{2})`)
	idx := re.FindStringSubmatchIndexAt("t=09:30", 2)
	if !reflect.DeepEqual(idx, []int{2, 7, 2, 4, 5, 7}) {
		t.Fatalf("FindStringSubmatchIndexAt(2) = %v", idx)
	}
	if re.FindStringSubmatchIndexAt("t=09:30", 0) != nil {
		t.Fatal("anchored at 0 should not match (does not scan forward)")
	}
	if re.FindStringSubmatchIndexAt("t=09:30", -1) != nil {
		t.Fatal("negative pos should yield nil")
	}
	if re.FindStringSubmatchIndexAt("t=09:30", 99) != nil {
		t.Fatal("pos past len should yield nil")
	}
}

// --- FindAll ---------------------------------------------------------------

// The lookahead case named in the task: version numbers followed by a quote or
// angle bracket, using (?=["<]) which RE2/stdlib cannot compile.
func TestFindAllLookahead(t *testing.T) {
	re := MustCompile(`v\d+\.\d+\.\d+(?=["<])`)
	in := `grab v1.2.3" and v4.5.6< but not v9.9.9 or v0.0.1"`
	got := re.FindAllString(in, -1)
	want := []string{"v1.2.3", "v4.5.6", "v0.0.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FindAllString lookahead = %v, want %v", got, want)
	}
}

func TestFindAllString(t *testing.T) {
	re := MustCompile(`\d+`)
	if got := re.FindAllString("a1b22c333", -1); !reflect.DeepEqual(got, []string{"1", "22", "333"}) {
		t.Fatalf("FindAllString(all) = %v", got)
	}
	if got := re.FindAllString("a1b22c333", 2); !reflect.DeepEqual(got, []string{"1", "22"}) {
		t.Fatalf("FindAllString(n=2) = %v", got)
	}
	if got := re.FindAllString("none", -1); got != nil {
		t.Fatalf("FindAllString(no match) = %v want nil", got)
	}
	idx := re.FindAllStringIndex("a1b22", -1)
	if !reflect.DeepEqual(idx, [][]int{{1, 2}, {3, 5}}) {
		t.Fatalf("FindAllStringIndex = %v", idx)
	}
	if re.FindAllStringIndex("none", -1) != nil {
		t.Fatal("FindAllStringIndex(no match) should be nil")
	}
	if got := re.FindAllStringIndex("a1b22", 1); !reflect.DeepEqual(got, [][]int{{1, 2}}) {
		t.Fatalf("FindAllStringIndex(n=1) = %v", got)
	}
}

// Empty-match handling: the standard-library semantics require an empty match to
// advance by one rune and to skip an empty match adjacent to a previous match.
func TestFindAllEmptyMatch(t *testing.T) {
	// `a*` matches empty strings between non-a runes; verify the by-one-rune
	// advance and adjacency skipping produce the stdlib result.
	// stdlib semantics: "" at 0, then "aa"; the empty match at the end (pos 3)
	// sits at the previous match end and is skipped, so there is no trailing "".
	re := MustCompile(`a*`)
	got := re.FindAllString("baa", -1)
	want := []string{"", "aa"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FindAllString(a*) = %v, want %v", got, want)
	}
	// A leading a-run yields the run then an empty match before the next non-a.
	if g2 := re.FindAllString("aab", -1); !reflect.DeepEqual(g2, []string{"aa", ""}) {
		t.Fatalf("FindAllString(a*, aab) = %v, want [aa \"\"]", g2)
	}
	// Multi-byte rune advance on an all-empty pattern over UTF-8 input.
	empty := MustCompile(``)
	if got := empty.FindAllStringIndex("é", -1); !reflect.DeepEqual(got, [][]int{{0, 0}, {2, 2}}) {
		t.Fatalf("empty-pattern indexes over multibyte = %v", got)
	}
}

// --- Timeout ---------------------------------------------------------------

func TestWithTimeout(t *testing.T) {
	re := MustCompile(`a+`)
	if re.Timeout() != 0 {
		t.Fatal("default timeout should be zero")
	}
	to := re.WithTimeout(50 * time.Millisecond)
	if to.Timeout() != 50*time.Millisecond {
		t.Fatalf("WithTimeout not recorded: %v", to.Timeout())
	}
	if to.WithTimeout(0).Timeout() != 0 {
		t.Fatal("non-positive timeout should clear the limit")
	}
	// A generous timeout still matches (exercises the timeout>0 deadline path on a
	// VM program).
	backref := MustCompile(`(\w+) \1`).WithTimeout(time.Second)
	if !backref.MatchString("hi hi") {
		t.Fatal("backref with generous timeout should match")
	}
}

// A pathological backreference pattern with no possible match forces the VM to
// backtrack; a tiny timeout must abort it and report no match rather than run
// unboundedly. This drives the deadline / error return path.
func TestTimeoutAborts(t *testing.T) {
	re := MustCompile(`(a+)+b\1c`).WithTimeout(time.Millisecond)
	in := strings.Repeat("a", 40)
	if re.MatchString(in) {
		t.Fatal("no b/c present: must not match")
	}
	if re.FindStringSubmatchIndex(in) != nil {
		t.Fatal("timed-out submatch should be nil")
	}
	if loc := re.FindStringIndex(in); loc != nil {
		t.Fatalf("timed-out FindStringIndex should be nil, got %v", loc)
	}
}
