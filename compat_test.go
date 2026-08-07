package onigmo_test

import onigmo "github.com/go-regexp/engine"

// This file adapts the ported engine feature tests, which were written against a
// MatchData-returning Match API, onto the go-regexp/engine index-based public API. reWrap
// embeds *onigmo.Regexp (so every real method — MatchString, MatchBounds,
// MatchBoundsAt, Encoding, WithTimeout, … — promotes through unchanged) and adds
// Match / MatchAt returning a *matchData whose Str/Begin/End/StrName accessors
// mirror the shape the tests expect. compile / compileEnc mint a reWrap so that
// call sites such as `re := mustCompile(t, p)` and `re, err := compile(p)`
// produce a value that carries the Match helpers.

type reWrap struct {
	*onigmo.Regexp
}

func compile(pattern string) (reWrap, error) {
	re, err := onigmo.Compile(pattern)
	return reWrap{re}, err
}

func compileEnc(pattern string, enc onigmo.Encoding) (reWrap, error) {
	re, err := onigmo.CompileEnc(pattern, enc)
	return reWrap{re}, err
}

// Match returns the leftmost match's capture spans as a *matchData, or nil for no
// match — the shape the ported tests were written against.
func (r reWrap) Match(s string) *matchData {
	idx := r.FindStringSubmatchIndex(s)
	if idx == nil {
		return nil
	}
	return &matchData{re: r, s: s, idx: idx}
}

// MatchAt is Match anchored exactly at byte offset pos (no forward scan).
func (r reWrap) MatchAt(s string, pos int) *matchData {
	idx := r.FindStringSubmatchIndexAt(s, pos)
	if idx == nil {
		return nil
	}
	return &matchData{re: r, s: s, idx: idx}
}

type matchData struct {
	re  reWrap
	s   string
	idx []int
}

func (m *matchData) Begin(i int) int {
	if i < 0 || 2*i+1 >= len(m.idx) {
		return -1
	}
	return m.idx[2*i]
}

func (m *matchData) End(i int) int {
	if i < 0 || 2*i+1 >= len(m.idx) {
		return -1
	}
	return m.idx[2*i+1]
}

func (m *matchData) Str(i int) string {
	b, e := m.Begin(i), m.End(i)
	if b < 0 || e < 0 {
		return ""
	}
	return m.s[b:e]
}

func (m *matchData) IndexOfName(name string) int { return m.re.SubexpIndex(name) }

func (m *matchData) StrName(name string) string { return m.Str(m.re.SubexpIndex(name)) }
