package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// The literal "Number$<int>" SVar body (task poison: the arm the Vraska,
// Betrayal's Sting end-to-end pin rides on). Before the arm every Number$
// body evaluated to (0, not-evaluated): Num$ Difference read 0, so Vraska's
// "poison equal to the difference" ultimatum placed nothing. The leaves pin
// the literal, the SVar-named /Op operand through the SAME resolver the
// Count$ branch uses, and the fail-closed non-integer body.
func TestPoisonCounterNumberLiteralSVarBody(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	g.Players[1].AddCounter("POISON", 3)
	c := &Ctx{Source: ids["myBear"], Controller: 0,
		Targets: []state.Target{{Player: 1, IsPlayer: true}},
		SVars: map[string]string{
			"X":          "TargetedPlayer$Counters.Poison",
			"Difference": "Number$9/Minus.X",
			"Plain":      "Number$9",
			"Z":          "Number$2/Plus.1",
		}}
	for _, tc := range []struct {
		expr     string
		want     int32
		wantOK   bool
		premise  string
		premiseF func(*Ctx) *Ctx
	}{
		// The bare literal is evaluated.
		{"Number$9", 9, true, "", nil},
		// The Vraska shape: a literal minus an SVar-named operand, where the
		// operand itself is a TargetedPlayer$ count head.
		{"Number$9/Minus.X", 6, true, "", nil},
		// A numeric suffix rides the same arm.
		{"Number$2/Plus.1", 3, true, "", nil},
		// A non-integer body stays fail-closed (never a fake zero verdict).
		{"Number$abc", 0, false, "", nil},
		// The empty literal (SVar:RepeatCheck:Number$) stays fail-closed.
		{"Number$", 0, false, "", nil},
		// Mathemagics' Pow suffix uses the same named-operand resolution.
		// Here X is the target's three poison counters, so 2^3 is 8.
		{"Number$2/Pow.X", 8, true, "", nil},
	} {
		got, ok := EvalCountOK(h, c, tc.expr)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("EvalCountOK(%q) = (%d, %v), want (%d, %v)", tc.expr, got, ok, tc.want, tc.wantOK)
		}
	}
	// The value the Vraska differential reads must change with the target's
	// poison, so the end-to-end leaf cannot pass on a coincidental constant.
	g.Players[1].Counters = nil
	g.Players[1].AddCounter("POISON", 7)
	if got, _ := EvalCountOK(h, c, "Number$9/Minus.X"); got != 2 {
		t.Errorf("Number$9/Minus.X at 7 poison = %d, want 2 (the read is live)", got)
	}
}
