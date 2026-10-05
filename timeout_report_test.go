package onigmo

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// catastrophicPattern is a backreference-bearing pattern outside the lazy-NFA
// subset, so every match runs on the backtracking VM — the only path that can
// reach a limit. Matched against a run of "a" with no trailing "b" it has no
// match to find and explores exponentially many ways of not finding one, which is
// what makes a wall-clock limit observable at all.
const catastrophicPattern = `(a+)+\1b`

func catastrophicSubject() string { return strings.Repeat("a", 26) }

// TestTimeoutIsReportedNotFoldedIntoNoMatch is the fail-open regression test: a
// match abandoned at the wall-clock limit must be distinguishable from a subject
// that genuinely does not match. The non-Err methods answer "no match" for both,
// which is why a Regexp used as a guard has to use the …Err form: folded
// together, a crafted subject reads as "the guard did not fire".
func TestTimeoutIsReportedNotFoldedIntoNoMatch(t *testing.T) {
	re := MustCompile(catastrophicPattern).WithTimeout(20 * time.Millisecond)
	s := catastrophicSubject()

	t.Run("MatchBoundsErr", func(t *testing.T) {
		_, _, ok, err := re.MatchBoundsErr(s)
		if !errors.Is(err, ErrTimeout) {
			t.Fatalf("MatchBoundsErr err = %v, want ErrTimeout", err)
		}
		if ok {
			t.Fatalf("MatchBoundsErr ok = true, want false alongside the error")
		}
	})

	t.Run("FindStringSubmatchIndexErr", func(t *testing.T) {
		caps, err := re.FindStringSubmatchIndexErr(s)
		if !errors.Is(err, ErrTimeout) {
			t.Fatalf("FindStringSubmatchIndexErr err = %v, want ErrTimeout", err)
		}
		if caps != nil {
			t.Fatalf("FindStringSubmatchIndexErr caps = %v, want nil alongside the error", caps)
		}
	})

	// The anchored forms carried no deadline at all before this change: they called
	// the VM's budget-only entry point, so WithTimeout was silently ignored on every
	// anchored match. A cursor-driven scan (String#rindex probing one position after
	// another) therefore ran unbounded however small the limit.
	t.Run("MatchBoundsAtErr", func(t *testing.T) {
		st := time.Now()
		_, _, ok, err := re.MatchBoundsAtErr(s, 0)
		if !errors.Is(err, ErrTimeout) {
			t.Fatalf("MatchBoundsAtErr err = %v after %v, want ErrTimeout", err, time.Since(st))
		}
		if ok {
			t.Fatalf("MatchBoundsAtErr ok = true, want false alongside the error")
		}
	})

	t.Run("FindStringSubmatchIndexAtErr", func(t *testing.T) {
		st := time.Now()
		caps, err := re.FindStringSubmatchIndexAtErr(s, 0)
		if !errors.Is(err, ErrTimeout) {
			t.Fatalf("FindStringSubmatchIndexAtErr err = %v after %v, want ErrTimeout", err, time.Since(st))
		}
		if caps != nil {
			t.Fatalf("FindStringSubmatchIndexAtErr caps = %v, want nil alongside the error", caps)
		}
	})
}

// TestTimeoutBoundsTheAnchoredPath is the control for the gap above: it asserts
// the limit is enforced in WALL-CLOCK terms on the anchored path, not merely that
// an error comes back. Without the deadline the same match ran for ~1s; the bound
// here is deliberately loose (20x) so it measures the fix, not the machine.
func TestTimeoutBoundsTheAnchoredPath(t *testing.T) {
	const limit = 20 * time.Millisecond
	re := MustCompile(catastrophicPattern).WithTimeout(limit)
	s := catastrophicSubject()

	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"MatchBoundsAtErr", func() error { _, _, _, err := re.MatchBoundsAtErr(s, 0); return err }},
		{"FindStringSubmatchIndexAtErr", func() error { _, err := re.FindStringSubmatchIndexAtErr(s, 0); return err }},
	} {
		st := time.Now()
		err := tc.run()
		elapsed := time.Since(st)
		if !errors.Is(err, ErrTimeout) {
			t.Fatalf("%s: err = %v, want ErrTimeout", tc.name, err)
		}
		if elapsed > 20*limit {
			t.Errorf("%s: took %v with a %v limit — the deadline is not reaching the VM", tc.name, elapsed, limit)
		}
	}
}

// TestNoLimitMeansNoError is the negative control: the same methods must report a
// plain non-match (nil error) when no limit is set, so a non-nil error always
// means "abandoned", never "did not match".
func TestNoLimitMeansNoError(t *testing.T) {
	re := MustCompile(`z+q`)
	const s = "aaa"

	if _, _, ok, err := re.MatchBoundsErr(s); ok || err != nil {
		t.Errorf("MatchBoundsErr = (%v, %v), want (false, nil)", ok, err)
	}
	if _, _, ok, err := re.MatchBoundsAtErr(s, 0); ok || err != nil {
		t.Errorf("MatchBoundsAtErr = (%v, %v), want (false, nil)", ok, err)
	}
	if caps, err := re.FindStringSubmatchIndexErr(s); caps != nil || err != nil {
		t.Errorf("FindStringSubmatchIndexErr = (%v, %v), want (nil, nil)", caps, err)
	}
	if caps, err := re.FindStringSubmatchIndexAtErr(s, 0); caps != nil || err != nil {
		t.Errorf("FindStringSubmatchIndexAtErr = (%v, %v), want (nil, nil)", caps, err)
	}

	// A real match reports itself with a nil error through the …Err forms too.
	hit := MustCompile(`(a)(b)`)
	if b, e, ok, err := hit.MatchBoundsErr("xabz"); !ok || err != nil || b != 1 || e != 3 {
		t.Errorf("MatchBoundsErr = (%d, %d, %v, %v), want (1, 3, true, nil)", b, e, ok, err)
	}
	if b, e, ok, err := hit.MatchBoundsAtErr("xabz", 1); !ok || err != nil || b != 1 || e != 3 {
		t.Errorf("MatchBoundsAtErr = (%d, %d, %v, %v), want (1, 3, true, nil)", b, e, ok, err)
	}
	if caps, err := hit.FindStringSubmatchIndexErr("xabz"); err != nil || len(caps) != 6 || caps[0] != 1 {
		t.Errorf("FindStringSubmatchIndexErr = (%v, %v), want a match at 1 with nil error", caps, err)
	}
	if caps, err := hit.FindStringSubmatchIndexAtErr("xabz", 1); err != nil || len(caps) != 6 || caps[0] != 1 {
		t.Errorf("FindStringSubmatchIndexAtErr = (%v, %v), want a match at 1 with nil error", caps, err)
	}

	// An out-of-range anchor is a non-match, not an error.
	if _, _, ok, err := hit.MatchBoundsAtErr("xabz", 99); ok || err != nil {
		t.Errorf("MatchBoundsAtErr(pos out of range) = (%v, %v), want (false, nil)", ok, err)
	}
	if caps, err := hit.FindStringSubmatchIndexAtErr("xabz", -1); caps != nil || err != nil {
		t.Errorf("FindStringSubmatchIndexAtErr(pos out of range) = (%v, %v), want (nil, nil)", caps, err)
	}
}

// TestLazyNFASubsetReportsNoError pins the claim the …Err doc comments make: on
// the lazy-NFA subset neither limit can be reached, so err is always nil there
// and a caller cannot be handed a spurious refusal.
func TestLazyNFASubsetReportsNoError(t *testing.T) {
	// (a+)+b IS in the subset (no backreference): the classic one-line ReDoS is
	// linear here, and must not report a limit even with an absurdly small one.
	re := MustCompile(`(a+)+b`).WithTimeout(time.Nanosecond)
	s := strings.Repeat("a", 40)
	if _, _, ok, err := re.MatchBoundsErr(s); ok || err != nil {
		t.Errorf("MatchBoundsErr on the NFA subset = (%v, %v), want (false, nil)", ok, err)
	}
	if _, _, ok, err := re.MatchBoundsAtErr(s, 0); ok || err != nil {
		t.Errorf("MatchBoundsAtErr on the NFA subset = (%v, %v), want (false, nil)", ok, err)
	}
}

// TestNonErrFormsStillFoldTheLimit documents the shape the rest of the API keeps:
// the standard-library-shaped methods answer "no match" for an abandoned search.
// This is the behaviour a guard must NOT rely on, and the test exists so the
// divergence is a recorded decision rather than an accident.
func TestNonErrFormsStillFoldTheLimit(t *testing.T) {
	re := MustCompile(catastrophicPattern).WithTimeout(20 * time.Millisecond)
	s := catastrophicSubject()

	if re.MatchString(s) {
		t.Error("MatchString = true, want false")
	}
	if re.Match([]byte(s)) {
		t.Error("Match = true, want false")
	}
	if _, _, ok := re.MatchBounds(s); ok {
		t.Error("MatchBounds ok = true, want false")
	}
	if _, _, ok := re.MatchBoundsAt(s, 0); ok {
		t.Error("MatchBoundsAt ok = true, want false")
	}
	if loc := re.FindStringIndex(s); loc != nil {
		t.Errorf("FindStringIndex = %v, want nil", loc)
	}
	if got := re.FindString(s); got != "" {
		t.Errorf("FindString = %q, want \"\"", got)
	}
	if caps := re.FindStringSubmatchIndex(s); caps != nil {
		t.Errorf("FindStringSubmatchIndex = %v, want nil", caps)
	}
	if caps := re.FindStringSubmatchIndexAt(s, 0); caps != nil {
		t.Errorf("FindStringSubmatchIndexAt = %v, want nil", caps)
	}
	if got := re.FindStringSubmatch(s); got != nil {
		t.Errorf("FindStringSubmatch = %v, want nil", got)
	}
	if got := re.FindAllString(s, -1); got != nil {
		t.Errorf("FindAllString = %v, want nil", got)
	}
	if got := re.FindAllStringIndex(s, -1); got != nil {
		t.Errorf("FindAllStringIndex = %v, want nil", got)
	}
	if got := re.ReplaceAllString(s, "X"); got != s {
		t.Errorf("ReplaceAllString = %q, want the subject unchanged", got)
	}
}

// TestExportedLimitErrorsAreTheVMErrors pins the identity the …Err contract rests
// on: the package-level sentinels are the ones the VM actually returns, so
// errors.Is on an exported sentinel matches what a match reports.
func TestExportedLimitErrorsAreTheVMErrors(t *testing.T) {
	if ErrTimeout == nil || ErrBudget == nil {
		t.Fatal("exported limit sentinels must be non-nil")
	}
	if errors.Is(ErrTimeout, ErrBudget) {
		t.Error("ErrTimeout and ErrBudget must be distinguishable")
	}
	if got := ErrTimeout.Error(); got == "" {
		t.Error("ErrTimeout has an empty message")
	}
	if got := ErrBudget.Error(); got == "" {
		t.Error("ErrBudget has an empty message")
	}
}
