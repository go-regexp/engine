// Package charset classifies a single Unicode code point against the property
// names this engine recognises for the \p{…} / \P{…} construct. It is the one
// rune-aware piece of the otherwise byte-oriented engine: the parser uses
// Valid to reject unknown property names at compile time, and the VM uses Match
// to test a decoded rune.
//
// Two families of names are supported, matching the slice of Onigmo/Ruby this
// engine implements:
//
//   - General categories: the one-letter groups L, N, P, S, Z, C and the
//     two-letter letter/number/other subcategories Lu, Ll, Lt, Lm, Lo, Nd, Nl,
//     No, Cf. These map directly onto Go's unicode range tables.
//   - Onigmo POSIX-style aliases: Alpha, Alnum, Digit, Space, Upper, Lower and
//     Word. These follow Ruby's definitions (e.g. Space is the Unicode
//     White_Space property, which is broader than the Z general category;
//     Word is letter | mark | decimal-number | connector-punctuation), which
//     do not always coincide with a single general category.
//
// Names are matched case-sensitively, exactly as Onigmo treats them. Negation
// (\P{…} and \p{^…}) is handled by the caller, not here.
package charset

import "unicode"

// classify maps a recognised property name to its membership predicate. It
// returns nil for an unknown name.
func classify(name string) func(rune) bool {
	switch name {
	// General categories — one-letter groups.
	case "L":
		return unicode.IsLetter
	case "N":
		return unicode.IsNumber
	case "P":
		return unicode.IsPunct
	case "S":
		return unicode.IsSymbol
	case "Z":
		return func(r rune) bool { return unicode.Is(unicode.Z, r) }
	case "C":
		return func(r rune) bool { return unicode.Is(unicode.C, r) }
	case "Cf":
		return func(r rune) bool { return unicode.Is(unicode.Cf, r) }
	// General categories — letter and number subcategories.
	case "Lu":
		return func(r rune) bool { return unicode.Is(unicode.Lu, r) }
	case "Ll":
		return func(r rune) bool { return unicode.Is(unicode.Ll, r) }
	case "Lt":
		return func(r rune) bool { return unicode.Is(unicode.Lt, r) }
	case "Lm":
		return func(r rune) bool { return unicode.Is(unicode.Lm, r) }
	case "Lo":
		return func(r rune) bool { return unicode.Is(unicode.Lo, r) }
	case "Nd":
		return func(r rune) bool { return unicode.Is(unicode.Nd, r) }
	case "Nl":
		return func(r rune) bool { return unicode.Is(unicode.Nl, r) }
	case "No":
		return func(r rune) bool { return unicode.Is(unicode.No, r) }
	// Onigmo POSIX-style aliases.
	case "Alpha":
		return isAlpha
	case "Alnum":
		return func(r rune) bool { return isAlpha(r) || unicode.Is(unicode.Nd, r) }
	case "Digit":
		return func(r rune) bool { return unicode.Is(unicode.Nd, r) }
	case "Space":
		return unicode.IsSpace
	case "Upper":
		return isUpper
	case "Lower":
		return isLower
	case "Word":
		return isWord
	case "Blank":
		return isBlank
	case "Cntrl":
		return isCntrl
	case "Graph":
		return isGraph
	case "Print":
		return isPrint
	case "Punct":
		return isPunct
	default:
		return nil
	}
}

// isBlank is Onigmo's Blank alias: a horizontal space — a Unicode space
// separator (Zs) or a tab. Line/paragraph separators and other whitespace are
// not blank.
func isBlank(r rune) bool {
	return r == '\t' || unicode.Is(unicode.Zs, r)
}

// isCntrl is Onigmo's Cntrl alias. MRI (ruby 4.0) matches only the Control
// general category (Cc): format characters (Cf, e.g. U+00AD) are NOT controls,
// despite the wider wording ("Control | Format | …") in the Onigmo docs.
func isCntrl(r rune) bool {
	return unicode.Is(unicode.Cc, r)
}

// isGraph is Onigmo's Graph alias: an assigned, non-space, non-control code
// point — every graphic character (letters, marks, numbers, punctuation,
// symbols) except the space separators, plus the assigned invisible formatting
// (Cf, e.g. U+00AD) and private-use (Co) code points, which MRI treats as graph.
// Controls (Cc/Cs) and unassigned code points are excluded.
func isGraph(r rune) bool {
	return (unicode.IsGraphic(r) && !unicode.Is(unicode.Z, r)) ||
		unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Co, r)
}

// isPrint is Onigmo's Print alias: Graph plus the Unicode space separators (Zs),
// so an ordinary space and a no-break space print but a tab or newline does not.
func isPrint(r rune) bool {
	return isGraph(r) || unicode.Is(unicode.Zs, r)
}

// isPunct is Onigmo's Punct alias: every Unicode punctuation category (P) plus
// the nine ASCII characters $ + < = > ^ ` | ~ that Onigmo folds into punct (they
// are Symbol, not Punctuation, in Unicode). MRI (ruby 4.0) does not treat other
// symbols (e.g. × or the euro sign) as punct.
func isPunct(r rune) bool {
	switch r {
	case '$', '+', '<', '=', '>', '^', '`', '|', '~':
		return true
	}
	return unicode.IsPunct(r)
}

// isAlpha is Onigmo's Alpha alias: the Unicode Alphabetic derived property,
// which is letters plus letter-numbers (Nl) plus the Other_Alphabetic
// characters (e.g. some combining marks), but not decimal digits or symbols.
func isAlpha(r rune) bool {
	return unicode.IsLetter(r) || unicode.Is(unicode.Nl, r) || unicode.Is(unicode.Other_Alphabetic, r)
}

// isWord is Onigmo's Word alias: an alphabetic character, a mark, a decimal
// number, or a connector punctuation, matching Ruby's \p{Word}. Using the
// Alphabetic property (isAlpha) rather than bare Letter means letter-numbers
// such as the Roman numeral Ⅷ (Nl) count as word characters, as MRI treats them.
func isWord(r rune) bool {
	return isAlpha(r) || unicode.IsMark(r) || unicode.Is(unicode.Nd, r) || unicode.Is(unicode.Pc, r)
}

// isUpper is Onigmo's Upper alias: the Unicode Uppercase derived property, which
// is uppercase letters plus the Other_Uppercase code points (e.g. the uppercase
// Roman numerals), so it is broader than unicode.IsUpper's Lu-only test.
func isUpper(r rune) bool {
	return unicode.IsUpper(r) || unicode.Is(unicode.Other_Uppercase, r)
}

// isLower is Onigmo's Lower alias: the Unicode Lowercase derived property —
// lowercase letters plus the Other_Lowercase code points (e.g. the lowercase
// Roman numerals and modifier letters).
func isLower(r rune) bool {
	return unicode.IsLower(r) || unicode.Is(unicode.Other_Lowercase, r)
}

// Valid reports whether name is a property this engine recognises.
func Valid(name string) bool {
	return classify(name) != nil
}

// Match reports whether rune r is a member of the named property. negate flips
// the result (for \P{…} and \p{^…}). It returns false for an unknown name; the
// parser is expected to have rejected such names via Valid before compilation,
// so this is only a defensive fallback.
func Match(name string, negate bool, r rune) bool {
	pred := classify(name)
	if pred == nil {
		return false
	}
	return pred(r) != negate
}

// FoldEqual reports whether runes a and b are equal under simple (1:1) Unicode
// case folding — that is, whether they belong to the same simple-case-folding
// orbit. The orbit is the cycle Go's unicode.SimpleFold walks (e.g. k → K →
// Kelvin-sign → k, or Σ → ς → σ → Σ), so FoldEqual('k', 0x212A) and
// FoldEqual('É', 'é') are both true. This is the engine's rune-level /i model;
// full/special case folding (multi-character expansions such as ß→"ss" and
// locale-specific rules such as Turkish dotless-i) is deliberately out of scope.
func FoldEqual(a, b rune) bool {
	if a == b {
		return true
	}
	// Walk a's orbit; if b appears, they fold-match. SimpleFold cycles back to
	// the starting rune, so the loop is finite.
	for f := unicode.SimpleFold(a); f != a; f = unicode.SimpleFold(f) {
		if f == b {
			return true
		}
	}
	return false
}

// foldOrbit returns r together with every other rune in its simple-case-folding
// orbit, used to test a code point against a folded character-class range.
func foldOrbit(r rune) []rune {
	orbit := []rune{r}
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		orbit = append(orbit, f)
	}
	return orbit
}

// FoldRangeContains reports whether code point r is in the inclusive range
// [lo,hi] under simple case folding: r matches if r, or any rune in r's
// simple-case-folding orbit, lies within the range. So FoldRangeContains tests
// "A" against the range "a".."z" (folding to lowercase) and the Kelvin sign
// against the same range (its orbit includes ASCII "k"). The class machinery uses
// this so (?i)[a-z] and (?i)[α-ω] match their opposite-case members.
func FoldRangeContains(r, lo, hi rune) bool {
	for _, f := range foldOrbit(r) {
		if f >= lo && f <= hi {
			return true
		}
	}
	return false
}
