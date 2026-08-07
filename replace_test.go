package onigmo

import (
	"reflect"
	"testing"
)

func TestReplaceAllString(t *testing.T) {
	tests := []struct {
		name, pat, src, repl, want string
	}{
		{"simple", `\d+`, "a1b22c333", "N", "aNbNcN"},
		{"strip-empty", `-\d+$`, "openssl-3", "", "openssl"},
		{"group-ref", `(\d+)\.(\d+)`, "v1.2", "$2.$1", "v2.1"},
		{"brace-ref", `(\d+)`, "x9y", "[${1}]", "x[9]y"},
		{"dollar-literal", `\d`, "a1", "$$", "a$"},
		{"malformed-dollar-end", `\d`, "a1", "x$", "ax$"},
		{"malformed-dollar-space", `\d`, "a1", "$ z", "a$ z"},
		{"missing-group", `(a)|(b)`, "a", "<$2>", "<>"},
		{"out-of-range-group", `(a)`, "a", "$9", ""},
		{"no-dollar-tail", `\d`, "a1b", "Q", "aQb"},
		{"empty-pat-anchored", `x*`, "abc", "-", "-a-b-c-"},
		{"lookahead-strip", `\d+(?=px)`, "10px 20", "", "px 20"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := MustCompile(tt.pat)
			if got := re.ReplaceAllString(tt.src, tt.repl); got != tt.want {
				t.Fatalf("ReplaceAllString(%q,%q)=%q want %q", tt.src, tt.repl, got, tt.want)
			}
		})
	}
}

func TestReplaceAllStringNamedGroup(t *testing.T) {
	re := MustCompile(`(?<yr>\d{4})-(?<mo>\d{2})`)
	// named reference that resolves, one that does not, and $$ literal
	got := re.ReplaceAllString("2026-08", "${mo}/${yr}$$${absent}")
	if got != "08/2026$" {
		t.Fatalf("named expand=%q want %q", got, "08/2026$")
	}
}

func TestReplaceAllLiteralString(t *testing.T) {
	re := MustCompile(`\d+`)
	// $1 must NOT be expanded in literal mode
	if got := re.ReplaceAllLiteralString("a1b2", "$1"); got != "a$1b$1" {
		t.Fatalf("literal=%q", got)
	}
}

func TestReplaceAllStringFunc(t *testing.T) {
	re := MustCompile(`[a-z]+`)
	got := re.ReplaceAllStringFunc("ab CD ef", func(s string) string {
		return "<" + s + ">"
	})
	if got != "<ab> CD <ef>" {
		t.Fatalf("func=%q", got)
	}
}

func TestReplaceByteWrappers(t *testing.T) {
	re := MustCompile(`(\d)(\d)`)
	if got := re.ReplaceAll([]byte("x12y"), []byte("$2$1")); !reflect.DeepEqual(got, []byte("x21y")) {
		t.Fatalf("ReplaceAll=%q", got)
	}
	if got := re.ReplaceAllLiteral([]byte("x12y"), []byte("$1")); !reflect.DeepEqual(got, []byte("x$1y")) {
		t.Fatalf("ReplaceAllLiteral=%q", got)
	}
	upper := re.ReplaceAllFunc([]byte("x12y34z"), func(b []byte) []byte {
		return []byte("#")
	})
	if !reflect.DeepEqual(upper, []byte("x#y#z")) {
		t.Fatalf("ReplaceAllFunc=%q", upper)
	}
}

func TestExtractEdgeCases(t *testing.T) {
	tests := []struct {
		tmpl     string
		wantName string
		wantNum  int
		wantRest string
		wantOK   bool
	}{
		{"", "", 0, "", false},
		{"1", "1", 1, "", true},
		{"{1}rest", "1", 1, "rest", true},
		{"{name}z", "name", -1, "z", true},
		{"{bad", "", 0, "", false}, // missing closing brace
		{" x", "", 0, "", false},   // first char not a name char
		{"01", "01", -1, "", true}, // leading zero -> not a number
		{"1000000000!", "1000000000", -1, "!", true}, // >= 1e8 -> num -1
	}
	for _, tt := range tests {
		name, num, rest, ok := extract(tt.tmpl)
		if ok != tt.wantOK {
			t.Fatalf("extract(%q) ok=%v want %v", tt.tmpl, ok, tt.wantOK)
		}
		// name/num/rest are only meaningful for a valid reference.
		if ok && (name != tt.wantName || num != tt.wantNum || rest != tt.wantRest) {
			t.Fatalf("extract(%q)=(%q,%d,%q) want (%q,%d,%q)",
				tt.tmpl, name, num, rest, tt.wantName, tt.wantNum, tt.wantRest)
		}
	}
}
