package paymirror

import (
	"bytes"
	"fmt"
	"runtime/debug"
	"strings"
	"testing"
)

//go:noinline
func horizonPanicFixtureBoom() { panic(fmt.Sprintf("boom at seq %d", 1234)) }

// capturePanic runs f and returns what resolveHorizon's drive records for
// a panic: the value and the trimmed stack.
func capturePanic(f func()) (out string) {
	defer func() {
		if r := recover(); r != nil {
			out = fmt.Sprintf("%v\n%s", r, trimPanicStack(debug.Stack()))
		}
	}()
	f()
	return ""
}

// TestHorizonPanicIsSurfaced pins that a resolve-horizon panic keeps its
// value and a trimmed stack (the route's Resolved class alone is
// "skipped:panic", which dropped both), is grouped by a digit-folded
// signature naming the innermost frame, survives compact, and is counted
// with an example line in the summary without changing the route verdict.
func TestHorizonPanicIsSurfaced(t *testing.T) {
	a := capturePanic(horizonPanicFixtureBoom)
	lines := strings.Split(a, "\n")
	if lines[0] != "boom at seq 1234" {
		t.Fatalf("panic value line = %q", lines[0])
	}
	if len(lines) < 2 || !strings.Contains(lines[1], "horizonPanicFixtureBoom") ||
		!strings.Contains(lines[1], "internal/paymirror/horizon_panic_test.go:") {
		t.Fatalf("innermost frame not the panicking function:\n%s", a)
	}
	for _, l := range lines[1:] {
		if !strings.HasPrefix(l, "  at ") || strings.Contains(l, "runtime/debug") || strings.Contains(l, "+0x") {
			t.Fatalf("untrimmed stack line %q in\n%s", l, a)
		}
	}
	if len(lines)-1 > maxPanicFrames {
		t.Fatalf("%d frames kept, bound %d", len(lines)-1, maxPanicFrames)
	}

	// The mirror panicked the same way at a different sequence number: one
	// "both" entry, and the signature folds the numbers.
	b := strings.Replace(a, "1234", "1240", 1)
	hp := horizonPanic([2]string{a, b})
	if !strings.HasPrefix(hp, "both: boom at seq 1234\n") {
		t.Fatalf("horizonPanic(same class) = %q", hp)
	}
	if got := horizonPanic([2]string{"", b}); !strings.HasPrefix(got, "mirror: ") {
		t.Fatalf("mirror-only panic = %q", got)
	}
	if got := horizonPanic([2]string{a, "other\n  at x y:1"}); !strings.HasPrefix(got, "A: ") || !strings.Contains(got, "\nmirror: other") {
		t.Fatalf("differing panics = %q", got)
	}
	sig := HorizonPanicSignature(hp)
	if want := "both: boom at seq N @ internal/paymirror.horizonPanicFixtureBoom"; sig != want {
		t.Fatalf("signature = %q, want %q", sig, want)
	}

	rep := &Report{Seq: 7, Card: "Fixture Card", Routes: []RouteResult{
		{Route: RouteFloat, Status: Equivalent, Resolved: "skipped:panic:boom", HorizonPanic: hp},
		{Route: RouteBase, Status: Equivalent, Resolved: "equivalent"},
	}}
	if st, _ := rep.Verdict(); st != Equivalent {
		t.Fatalf("a horizon panic changed the route verdict to %s", st)
	}
	if c := compact(rep); c.Routes[0].HorizonPanic != hp {
		t.Fatal("compact dropped the horizon panic")
	}
	s := NewSummary()
	s.Add(GameResult{Spec: GameSpec{Seed: 42, Decks: []string{"x", "y"}, Policy: "bot"}, Reports: []*Report{rep}})
	if s.HorizonPanicCount != 1 || s.Equivalent != 1 {
		t.Fatalf("horizon panics %d, equivalent %d; want 1, 1", s.HorizonPanicCount, s.Equivalent)
	}
	var out bytes.Buffer
	s.Write(&out)
	text := out.String()
	for _, want := range []string{
		"resolve-horizon panics: 1",
		"1  float_then_cast|" + sig,
		`e.g. seed=42 decks=x,y commander=false policy=bot seq=7 card="Fixture Card"`,
		"float_then_cast|resolved:skipped:panic",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary lacks %q:\n%s", want, text)
		}
	}
}
