# go-regexp/engine

[![ci](https://github.com/go-regexp/engine/actions/workflows/ci.yml/badge.svg)](https://github.com/go-regexp/engine/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-regexp/engine.svg)](https://pkg.go.dev/github.com/go-regexp/engine)
[![Go Report Card](https://goreportcard.com/badge/github.com/go-regexp/engine)](https://goreportcard.com/report/github.com/go-regexp/engine)
[![License: BSD-3-Clause](https://img.shields.io/badge/license-BSD--3--Clause-blue.svg)](LICENSE)

A pure-Go (cgo-free) regular-expression engine compatible with
[Onigmo](https://github.com/k-takata/Onigmo) — the regular-expression library
Ruby uses — with a public API shaped like the standard library's `regexp`
package.

## Why

The standard library `regexp` is built on RE2, which guarantees linear-time
matching by **forbidding** features that require backtracking. `go-regexp/engine`
supports the constructs RE2 rejects at compile time:

- **Lookahead** `(?=…)` / `(?!…)` and **lookbehind** `(?<=…)` / `(?<!…)`
- **Backreferences** `\1`, `\k<name>`
- **Atomic groups** `(?>…)` and possessive quantifiers
- **Subexpression calls** `\g<name>`, `\g<0>` (recursive patterns)
- Ruby/Onigmo character properties, `\h`/`\H`, `\R`, named groups, and more

It matches on the backtracking VM for those patterns and transparently falls back
to a linear-time lazy-NFA/DFA accelerator for the RE2-compatible subset, so the
common case stays fast while the extra features remain available.

The package is named `onigmo` (not `regexp`), so it can be imported alongside the
standard library `regexp` without an alias.

## Install

```sh
go get github.com/go-regexp/engine
```

## Usage

```go
package main

import (
	"fmt"

	onigmo "github.com/go-regexp/engine"
)

func main() {
	// A lookahead — (?=["<]) — that RE2/stdlib regexp cannot compile.
	re := onigmo.MustCompile(`v\d+\.\d+\.\d+(?=["<])`)

	fmt.Println(re.FindAllString(`grab v1.2.3" and v4.5.6< but not v9.9.9`, -1))
	// [v1.2.3 v4.5.6]

	fmt.Println(re.MatchString(`v2.0.0"`)) // true
	fmt.Println(re.FindStringIndex(`x v3.4.5<`)) // [2 8]
}
```

## API

The `*Regexp` API mirrors the standard library `regexp` where it overlaps:

| Method | Meaning |
| --- | --- |
| `Compile`, `MustCompile` | compile a pattern (panicking variant) |
| `String` | the source pattern |
| `MatchString(s)`, `Match(b)` | does the input contain a match? |
| `FindString`, `FindStringIndex` | leftmost match text / `[begin,end)` |
| `FindAllString`, `FindAllStringIndex` | all non-overlapping matches (`n < 0` = all) |
| `FindStringSubmatch`, `FindStringSubmatchIndex` | leftmost match with capture groups |
| `NumSubexp`, `SubexpNames`, `SubexpIndex` | capturing-group introspection |

Extensions beyond the standard library:

| Method | Meaning |
| --- | --- |
| `CompileEnc`, `MustCompileEnc`, `Encoding` | UTF-8 vs binary (`ASCII8BIT`) matching |
| `MatchBounds`, `MatchBoundsAt` | allocation-free whole-match `[begin,end)`; `…At` anchors at a byte offset without scanning forward |
| `FindStringSubmatchIndexAt` | anchored submatch (cursor-style lexing) |
| `WithTimeout`, `Timeout` | per-match wall-clock limit for pathological patterns |

`FindAll*` follow the standard library's non-overlapping, left-to-right semantics,
including empty-match handling (an empty match advances by one rune and an empty
match adjacent to a previous match is skipped).

A `*Regexp` is immutable once compiled and safe for concurrent use by multiple
goroutines. The heavy matcher state is built lazily on the first match.

## Status

Engine and public API are complete with **100% test coverage** and CI on the six
supported 64-bit architectures (amd64, arm64, riscv64, loong64, ppc64le, s390x).
A full org landing page, logo and hosted documentation are a follow-up.

## License

BSD-3-Clause. See [LICENSE](LICENSE).
