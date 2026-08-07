// Package onigmo is a pure-Go (cgo-free) regular-expression engine compatible
// with Onigmo — the regular-expression library Ruby uses — that exposes an API
// shaped like the standard library's regexp package.
//
// Unlike the standard library regexp (and RE2, which it is built on), this
// engine supports lookahead, lookbehind, backreferences, atomic groups and
// subexpression calls. Patterns that require those features — which stdlib
// regexp rejects at Compile time — compile and match here.
//
// The package name is onigmo (not regexp), so it can be imported alongside the
// standard library regexp without an alias:
//
//	import (
//		"regexp"
//		onigmo "github.com/go-regexp/engine"
//	)
//
//	re := onigmo.MustCompile(`v\d+\.\d+\.\d+(?=["<])`) // lookahead: RE2 cannot
//	re.FindAllString(`grab v1.2.3" and v4.5.6< but not v9.9.9`, -1)
//	// -> ["v1.2.3", "v4.5.6"]
//
// A *Regexp is immutable once compiled and safe for concurrent use by multiple
// goroutines. The heavy matcher state is built lazily on the first match, so a
// compiled-but-unmatched Regexp pays only the parse cost.
package onigmo

import (
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-regexp/engine/internal/compile"
	"github.com/go-regexp/engine/internal/syntax"
	"github.com/go-regexp/engine/internal/vm"
)

// machine holds the heavy, lazily-built matcher state shared by a Regexp and
// every WithTimeout copy of it. Compile validates the pattern's syntax eagerly
// (so a malformed pattern is reported at Compile time) and stores the parse
// result here, but defers the expensive lowering — building the instruction
// program and the lazy-NFA/DFA accelerator — until the first match.
//
// build is guarded by sync.Once so the one-time lowering is race-free even when
// a freshly-compiled Regexp is shared across goroutines and matched
// concurrently: every caller observes a fully-built prog/dfa or blocks until the
// single builder finishes. The result is immutable thereafter, so subsequent
// concurrent matches read it without synchronisation.
type machine struct {
	once sync.Once
	res  syntax.Result // parsed AST + capture metadata, retained for the deferred lowering
	enc  compile.Encoding
	prog *compile.Program
	// dfa is the lazy-NFA accelerator for the matchable subset (no backreference,
	// call, lookaround, atomic group, or over-large bounded loop). It finds the
	// leftmost-first whole-match span in linear time, replacing the backtracking VM
	// for the search / is-match case and for locating the bounds the VM is anchored
	// to when submatches are needed. It is nil when the program is outside the
	// subset, in which case every match runs on the backtracking VM.
	dfa *vm.DFA
}

// build performs the deferred lowering exactly once, lowering the retained parse
// result into the instruction program and building the DFA accelerator (which may
// be nil for a program outside the DFA subset). Safe under concurrent callers.
func (m *machine) build() {
	m.once.Do(func() {
		m.prog = compile.CompileEnc(m.res, m.enc)
		m.dfa = vm.BuildDFA(m.prog)
	})
}

// Regexp is a compiled regular expression, safe for concurrent use by multiple
// goroutines. A Regexp is immutable once compiled; WithTimeout returns a copy
// carrying a wall-clock match limit rather than mutating the receiver, so a
// shared Regexp stays concurrency-safe.
type Regexp struct {
	m       *machine
	source  string
	enc     compile.Encoding
	timeout time.Duration
}

// Encoding selects how the byte-oriented input-advancing atoms — the dot (`.`)
// and a byte-oriented character class — traverse the input.
//
// In UTF8 (the default) the dot and byte-oriented classes advance by a whole
// UTF-8 code point, so `.` matches a complete multi-byte character. In ASCII8BIT
// (binary mode) every atom advances one byte, and Unicode case-folding (/i) and
// \p{…} properties operate per byte. Match offsets are byte offsets in both
// modes.
type Encoding = compile.Encoding

const (
	// UTF8 is the default encoding: the dot and byte-oriented classes advance by a
	// whole UTF-8 code point.
	UTF8 = compile.UTF8
	// ASCII8BIT is the binary encoding: every atom advances one byte.
	ASCII8BIT = compile.ASCII8BIT
)

// Compile parses a pattern and returns a compiled Regexp in the default UTF-8
// encoding, or an error if the pattern is malformed. Unlike the standard library
// regexp.Compile, a pattern using lookaround, backreferences, atomic groups or
// subexpression calls is accepted.
func Compile(expr string) (*Regexp, error) {
	return CompileEnc(expr, UTF8)
}

// CompileEnc is Compile with an explicit input encoding (see Encoding). UTF8
// makes the dot and byte-oriented classes advance by a whole code point;
// ASCII8BIT makes every atom advance one byte.
func CompileEnc(expr string, enc Encoding) (*Regexp, error) {
	// Parse eagerly: this validates the whole pattern (backreference bounds,
	// \g<…> call resolution, balanced groups, …) so a malformed pattern is reported
	// here, at Compile time. Only the expensive machine build is deferred to first
	// use; the parse result is retained so that build can lower it.
	res, err := syntax.ParseEnc(expr, enc)
	if err != nil {
		return nil, err
	}
	return &Regexp{m: &machine{res: res, enc: enc}, source: expr, enc: enc}, nil
}

// MustCompile is like Compile but panics if the pattern cannot be compiled. It
// simplifies safe initialization of package-level compiled regular expressions.
func MustCompile(expr string) *Regexp {
	re, err := Compile(expr)
	if err != nil {
		panic("onigmo: Compile(" + strconv.Quote(expr) + "): " + err.Error())
	}
	return re
}

// MustCompileEnc is like CompileEnc but panics if the pattern cannot be compiled.
func MustCompileEnc(expr string, enc Encoding) *Regexp {
	re, err := CompileEnc(expr, enc)
	if err != nil {
		panic("onigmo: CompileEnc(" + strconv.Quote(expr) + "): " + err.Error())
	}
	return re
}

// String returns the source text of the pattern the Regexp was compiled from.
func (re *Regexp) String() string { return re.source }

// Encoding returns the input encoding the Regexp matches under: UTF8 by default,
// ASCII8BIT for a binary pattern. It does not trigger the deferred machine build.
func (re *Regexp) Encoding() Encoding { return re.enc }

// Timeout returns the wall-clock limit applied to a single match, or zero if no
// limit is set.
func (re *Regexp) Timeout() time.Duration { return re.timeout }

// WithTimeout returns a copy of the Regexp that aborts any single match taking
// longer than d of wall-clock time, reporting no match. A non-positive d clears
// the limit. The copy shares the compiled program with the receiver, which is
// left unchanged, so a Regexp can be shared across goroutines and given per-use
// timeouts without data races.
func (re *Regexp) WithTimeout(d time.Duration) *Regexp {
	cp := *re
	if d <= 0 {
		cp.timeout = 0
	} else {
		cp.timeout = d
	}
	return &cp
}

// deadline turns the configured timeout into an absolute instant for this match,
// or the zero time when there is no limit.
func (re *Regexp) deadline() time.Time {
	if re.timeout <= 0 {
		return time.Time{}
	}
	return time.Now().Add(re.timeout)
}

// NumSubexp returns the number of parenthesized capturing subexpressions in the
// pattern, not counting the whole match (group 0). It matches the semantics of
// the standard library regexp.Regexp.NumSubexp.
func (re *Regexp) NumSubexp() int {
	re.m.build()
	return re.m.prog.NumCapture
}

// SubexpNames returns the names of the parenthesized capturing subexpressions.
// The name of the first sub-expression is names[1], so that the index into the
// slice matches the group number used by FindStringSubmatchIndex. Because the
// whole match has no name, names[0] is always "". A subexpression without a name
// has an empty string entry. The result is freshly allocated on each call and the
// caller may modify it. This mirrors regexp.Regexp.SubexpNames.
func (re *Regexp) SubexpNames() []string {
	re.m.build()
	names := make([]string, re.m.prog.NumCapture+1)
	for name, idx := range re.m.prog.Names {
		if idx >= 0 && idx < len(names) {
			names[idx] = name
		}
	}
	return names
}

// SubexpIndex returns the index of the first subexpression with the given name,
// or -1 if there is no subexpression with that name. Group 0 (the whole match)
// has no name. This mirrors regexp.Regexp.SubexpIndex.
func (re *Regexp) SubexpIndex(name string) int {
	if name == "" {
		return -1
	}
	re.m.build()
	if idx, ok := re.m.prog.Names[name]; ok {
		return idx
	}
	return -1
}

// MatchString reports whether the string s contains any match of the regular
// expression. When the program is in the lazy-NFA subset (no backreference,
// call, lookaround, atomic group, or over-large bounded loop) the question is
// answered by the linear-time NFA simulation rather than the backtracking VM.
func (re *Regexp) MatchString(s string) bool {
	_, _, ok := re.matchBounds(s, 0)
	return ok
}

// Match reports whether the byte slice b contains any match of the regular
// expression.
func (re *Regexp) Match(b []byte) bool {
	return re.MatchString(string(b))
}

// matchBounds scans s from byte offset `from` for the leftmost match and returns
// its whole-match [begin, end) byte span. The DFA accelerator (when built) finds
// the span for from == 0 without the backtracking VM; other cases and offsets
// run the VM, which keeps the full string visible so anchors and lookbehind see
// the real prefix.
func (re *Regexp) matchBounds(s string, from int) (begin, end int, ok bool) {
	m := re.m
	m.build()
	if m.dfa != nil && from == 0 {
		return m.dfa.Search(s, m.prog.Enc, 0)
	}
	caps, matched, err := vm.MatchTimeoutFrom(m.prog, s, from, vm.DefaultBudget, re.deadline())
	if err != nil || !matched {
		return 0, 0, false
	}
	return caps[0], caps[1], true
}

// MatchBounds scans s left to right for the leftmost match and returns its
// whole-match [begin, end) byte span, without extracting submatches. On the
// lazy-NFA subset the search runs on the linear-time NFA; otherwise it falls to
// the backtracking VM. The span is identical to FindStringIndex(s).
func (re *Regexp) MatchBounds(s string) (begin, end int, ok bool) {
	return re.matchBounds(s, 0)
}

// MatchBoundsAt reports the whole match's [begin, end) byte span for a match
// anchored exactly at byte offset pos (begin == pos on success), without
// extracting submatches. Unlike MatchBounds it does not scan forward: it matches
// at pos or reports ok == false. The whole string stays visible, so ^, \A and
// lookbehind see the real prefix s[:pos] — the primitive a cursor-anchored
// tokenizer needs. pos out of range yields ok == false.
func (re *Regexp) MatchBoundsAt(s string, pos int) (begin, end int, ok bool) {
	if pos < 0 || pos > len(s) {
		return 0, 0, false
	}
	m := re.m
	m.build()
	if m.dfa != nil {
		return m.dfa.MatchAt(s, m.prog.Enc, pos)
	}
	caps, matched, err := vm.MatchAt(m.prog, s, pos, vm.DefaultBudget)
	if err != nil || !matched {
		return 0, 0, false
	}
	return caps[0], caps[1], true
}

// FindStringIndex returns a two-element slice of integers defining the location
// of the leftmost match in s of the regular expression. The match itself is at
// s[loc[0]:loc[1]]. A return value of nil indicates no match. This mirrors
// regexp.Regexp.FindStringIndex.
func (re *Regexp) FindStringIndex(s string) []int {
	b, e, ok := re.matchBounds(s, 0)
	if !ok {
		return nil
	}
	return []int{b, e}
}

// FindString returns the text of the leftmost match in s of the regular
// expression. If there is no match, the return value is an empty string, but it
// will also be empty if the regular expression successfully matches an empty
// string. Use FindStringIndex if it is necessary to distinguish these cases.
func (re *Regexp) FindString(s string) string {
	loc := re.FindStringIndex(s)
	if loc == nil {
		return ""
	}
	return s[loc[0]:loc[1]]
}

// submatch searches s from byte offset `from` for the leftmost match and returns
// the capture slots [b0,e0,b1,e1,…] (2*(NumSubexp+1) ints) for it, or nil if
// there is no match at or after `from`. A group that did not participate has both
// of its slots set to -1.
func (re *Regexp) submatch(s string, from int) []int {
	m := re.m
	m.build()
	caps, ok, err := vm.MatchTimeoutFrom(m.prog, s, from, vm.DefaultBudget, re.deadline())
	if err != nil || !ok {
		return nil
	}
	return caps
}

// FindStringSubmatchIndex returns a slice holding the index pairs identifying the
// leftmost match of the regular expression in s and the matches, if any, of its
// subexpressions, as defined by the 'Submatch' and 'Index' descriptions of
// regexp.Regexp. A return value of nil indicates no match. This mirrors
// regexp.Regexp.FindStringSubmatchIndex.
func (re *Regexp) FindStringSubmatchIndex(s string) []int {
	return re.submatch(s, 0)
}

// FindStringSubmatchIndexAt is FindStringSubmatchIndex for a match anchored
// exactly at byte offset pos: the whole match must begin at pos (it does not scan
// forward), with the full string visible so ^, \A and lookbehind see the real
// prefix s[:pos]. It returns the capture index pairs, or nil if the pattern does
// not match anchored at pos. pos out of range yields nil.
func (re *Regexp) FindStringSubmatchIndexAt(s string, pos int) []int {
	if pos < 0 || pos > len(s) {
		return nil
	}
	m := re.m
	m.build()
	caps, ok, err := vm.MatchAt(m.prog, s, pos, vm.DefaultBudget)
	if err != nil || !ok {
		return nil
	}
	return caps
}

// FindStringSubmatch returns a slice of strings holding the text of the leftmost
// match of the regular expression in s and the matches, if any, of its
// subexpressions. A return value of nil indicates no match. An entry is the empty
// string when the corresponding subexpression did not participate in the match.
// This mirrors regexp.Regexp.FindStringSubmatch.
func (re *Regexp) FindStringSubmatch(s string) []string {
	idx := re.submatch(s, 0)
	if idx == nil {
		return nil
	}
	res := make([]string, len(idx)/2)
	for i := range res {
		if b, e := idx[2*i], idx[2*i+1]; b >= 0 && e >= 0 {
			res[i] = s[b:e]
		}
	}
	return res
}

// allMatches drives a non-overlapping, left-to-right scan of s, calling deliver
// with the capture slots of each successive match until deliver has been called n
// times or the input is exhausted. It reproduces the standard library's
// empty-match handling: an empty match immediately following a previous match at
// the same position is skipped, and after any empty match the cursor advances by
// one whole rune so the scan makes progress instead of looping forever.
func (re *Regexp) allMatches(s string, n int, deliver func([]int)) {
	end := len(s)
	for pos, i, prevMatchEnd := 0, 0, -1; i < n && pos <= end; {
		match := re.submatch(s, pos)
		if match == nil {
			break
		}
		accept := true
		if match[1] == match[0] {
			// Empty match: reject one that sits exactly at the end of the previous
			// match (it would duplicate that boundary), and advance by one rune so a
			// pattern that matches the empty string cannot spin in place.
			if match[0] == prevMatchEnd {
				accept = false
			}
			_, width := utf8.DecodeRuneInString(s[pos:])
			if width == 0 || pos+width > end {
				pos = end + 1
			} else {
				pos += width
			}
		} else {
			pos = match[1]
		}
		prevMatchEnd = match[1]
		if accept {
			deliver(match)
			i++
		}
	}
}

// FindAllStringIndex returns a slice of all successive non-overlapping matches of
// the regular expression in s, expressed as index pairs (see FindStringIndex).
// The matches are found left to right. A value of n >= 0 limits the result to at
// most n matches; n < 0 returns all of them. A return value of nil indicates no
// match. This mirrors regexp.Regexp.FindAllStringIndex.
func (re *Regexp) FindAllStringIndex(s string, n int) [][]int {
	if n < 0 {
		n = len(s) + 1
	}
	var res [][]int
	re.allMatches(s, n, func(match []int) {
		res = append(res, []int{match[0], match[1]})
	})
	return res
}

// FindAllString returns a slice of all successive non-overlapping matches of the
// regular expression in s. A value of n >= 0 limits the result to at most n
// matches; n < 0 returns all of them. A return value of nil indicates no match.
// This mirrors regexp.Regexp.FindAllString.
func (re *Regexp) FindAllString(s string, n int) []string {
	if n < 0 {
		n = len(s) + 1
	}
	var res []string
	re.allMatches(s, n, func(match []int) {
		res = append(res, s[match[0]:match[1]])
	})
	return res
}
