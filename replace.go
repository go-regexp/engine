package onigmo

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// replaceAll drives a non-overlapping left-to-right scan of src and builds the
// result of a replacement: the unmatched text before each match is copied
// verbatim, and repl is invoked to append the replacement for the match. It
// reproduces the standard library's empty-match handling — a replacement is
// emitted for an empty match only when it does not immediately abut the previous
// match (so two adjacent empty matches cannot both fire), and the cursor always
// advances by at least one rune so a pattern that matches the empty string
// cannot spin in place.
func (re *Regexp) replaceAll(src string, repl func(dst []byte, match []int) []byte) []byte {
	var buf []byte
	lastMatchEnd := 0
	searchPos := 0
	end := len(src)
	for searchPos <= end {
		// A search abandoned at a limit (ErrTimeout, ErrBudget) ends the walk with the
		// replacements made so far, exactly as a genuine non-match does: this family
		// has no channel to report the difference, which is why a guard must use an
		// …Err primitive instead (see ErrTimeout).
		match, _ := re.submatch(src, searchPos)
		if match == nil {
			break
		}
		buf = append(buf, src[lastMatchEnd:match[0]]...)
		if match[1] > lastMatchEnd || match[0] == 0 {
			buf = repl(buf, match)
		}
		lastMatchEnd = match[1]
		_, width := utf8.DecodeRuneInString(src[searchPos:])
		if searchPos+width > match[1] {
			searchPos += width
		} else if searchPos+1 > match[1] {
			// Only reached at the very end of src, where DecodeRuneInString
			// returns width 0; nudge past the match to terminate the scan.
			searchPos = match[1] + 1
		} else {
			searchPos = match[1]
		}
	}
	buf = append(buf, src[lastMatchEnd:]...)
	return buf
}

// ReplaceAllString returns a copy of src, replacing every non-overlapping match
// of re with the expansion of repl. Inside repl a $ introduces a submatch
// reference: $name or ${name} is replaced by the submatch named or numbered
// name, and $$ is a literal $. A reference to a group that did not participate
// expands to the empty string. This mirrors regexp.Regexp.ReplaceAllString.
func (re *Regexp) ReplaceAllString(src, repl string) string {
	return string(re.replaceAll(src, func(dst []byte, match []int) []byte {
		return re.expand(dst, repl, src, match)
	}))
}

// ReplaceAllLiteralString returns a copy of src, replacing every non-overlapping
// match of re with repl used literally — no $ expansion is performed. This
// mirrors regexp.Regexp.ReplaceAllLiteralString.
func (re *Regexp) ReplaceAllLiteralString(src, repl string) string {
	return string(re.replaceAll(src, func(dst []byte, _ []int) []byte {
		return append(dst, repl...)
	}))
}

// ReplaceAllStringFunc returns a copy of src, replacing every non-overlapping
// match of re with the return value of repl applied to the matched substring. No
// $ expansion is performed on repl's result. This mirrors
// regexp.Regexp.ReplaceAllStringFunc.
func (re *Regexp) ReplaceAllStringFunc(src string, repl func(string) string) string {
	return string(re.replaceAll(src, func(dst []byte, match []int) []byte {
		return append(dst, repl(src[match[0]:match[1]])...)
	}))
}

// ReplaceAll is the []byte form of ReplaceAllString: it returns a copy of src
// with every non-overlapping match replaced by the $-expansion of repl.
func (re *Regexp) ReplaceAll(src, repl []byte) []byte {
	return []byte(re.ReplaceAllString(string(src), string(repl)))
}

// ReplaceAllLiteral is the []byte form of ReplaceAllLiteralString: repl is used
// literally with no $ expansion.
func (re *Regexp) ReplaceAllLiteral(src, repl []byte) []byte {
	return []byte(re.ReplaceAllLiteralString(string(src), string(repl)))
}

// ReplaceAllFunc is the []byte form of ReplaceAllStringFunc.
func (re *Regexp) ReplaceAllFunc(src []byte, repl func([]byte) []byte) []byte {
	return []byte(re.ReplaceAllStringFunc(string(src), func(s string) string {
		return string(repl([]byte(s)))
	}))
}

// expand appends to dst the expansion of template against the capture slots in
// match over source string src, following the same $name / ${name} / $$ rules as
// regexp.Regexp.Expand.
func (re *Regexp) expand(dst []byte, template, src string, match []int) []byte {
	for len(template) > 0 {
		before, after, ok := strings.Cut(template, "$")
		if !ok {
			break
		}
		dst = append(dst, before...)
		template = after
		if template != "" && template[0] == '$' {
			// $$ is a literal dollar sign.
			dst = append(dst, '$')
			template = template[1:]
			continue
		}
		name, num, rest, ok := extract(template)
		if !ok {
			// Malformed reference: emit the $ literally and continue.
			dst = append(dst, '$')
			continue
		}
		template = rest
		if num >= 0 {
			if 2*num+1 < len(match) && match[2*num] >= 0 {
				dst = append(dst, src[match[2*num]:match[2*num+1]]...)
			}
			continue
		}
		for i, namei := range re.SubexpNames() {
			if name == namei && 2*i+1 < len(match) && match[2*i] >= 0 {
				dst = append(dst, src[match[2*i]:match[2*i+1]]...)
				break
			}
		}
	}
	return append(dst, template...)
}

// extract parses a leading $-reference (already past the $) of the form name or
// {name}: it returns the textual name, its numeric value (num >= 0 when name is
// all digits, else -1), the unconsumed remainder, and ok=false when template
// does not begin with a valid reference.
func extract(template string) (name string, num int, rest string, ok bool) {
	if template == "" {
		return
	}
	brace := false
	if template[0] == '{' {
		brace = true
		template = template[1:]
	}
	i := 0
	for i < len(template) {
		r, size := utf8.DecodeRuneInString(template[i:])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}
		i += size
	}
	if i == 0 {
		// An empty name is not a valid reference.
		return
	}
	name = template[:i]
	if brace {
		if i >= len(template) || template[i] != '}' {
			// Missing closing brace.
			return
		}
		i++
	}
	// A name that is all digits (with no leading zero) is a group number.
	num = 0
	for j := 0; j < len(name); j++ {
		if name[j] < '0' || '9' < name[j] || num >= 1e8 {
			num = -1
			break
		}
		num = num*10 + int(name[j]) - '0'
	}
	if name[0] == '0' && len(name) > 1 {
		num = -1
	}
	rest = template[i:]
	ok = true
	return
}
