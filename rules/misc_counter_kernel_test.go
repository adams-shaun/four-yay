package rules

// Restores effects/misc_counter_test.go's Mana Leak leaves on the kernel:
// the UnlessCost$ pay/decline is posed to the CONTROLLER OF THE COUNTERED
// SPELL (not the counterspell's caster), nothing is countered before the
// answer, a paid tax lets the spell resolve, and a decline counters it.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// kr2ManaLeakAsk puts seat 1's creature spell on the stack, has seat 0 cast
// the real Mana Leak at it, and returns the unless_pay ask.
func kr2ManaLeakAsk(t *testing.T) (*Engine, state.ObjID, *decision.Decision) {
	t.Helper()
	e := kr2Engine(t, 2)
	bear := kr2Put(t, e, 1, kr2Src(t, "Name:Leak Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), state.ZStack, false)
	leak := kr2Put(t, e, 0, kr2Corpus(t, "Mana Leak"), state.ZHand, false)
	kr2Fund(e, 1)
	d := kr2Cast(t, e, 0, leak)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Mana Leak cast = %+v, want its target ask", d)
	}
	d = kr2Want(t, kr2Answer(t, e, d, kr2ObjIdx(t, d, bear)), "unless_pay")
	return e, bear, d
}

func TestManaLeakAsksTheCounteredSpellsController(t *testing.T) {
	t.Parallel()
	e, bear, d := kr2ManaLeakAsk(t)
	if d.Kind != decision.KModes || d.Min != 1 || d.Max != 1 {
		t.Fatalf("decision = %+v, want a Min==Max==1 KModes", d)
	}
	if d.Player != 1 {
		t.Fatalf("payer = seat %d, want the countered spell's controller (seat 1), not the caster", d.Player)
	}
	if !strings.Contains(d.Prompt, "Pay 3") {
		t.Fatalf("prompt = %q, want it to name the {3} tax", d.Prompt)
	}
	if len(d.Options) != 2 || d.Options[0].Label != "Pay 3 — don't counter" || d.Options[1].Label != "Don't pay" {
		t.Fatalf("options = %+v, want the pay/decline pair", d.Options)
	}
	if z := e.G.Obj(bear).Zone; z != state.ZStack {
		t.Fatalf("spell left the stack before the answer: %s", z)
	}
}

func TestManaLeakPayDoesNotCounterAndDeclineCounters(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		pay  bool
		want state.Zone
	}{{"pay", true, state.ZBattlefield}, {"decline", false, state.ZGraveyard}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, bear, d := kr2ManaLeakAsk(t)
			idx := 1
			if tc.pay {
				idx = 0
			}
			d = kr2Answer(t, e, d, idx)
			for i := 0; d != nil && i < 20; i++ {
				switch d.ResumeKind {
				case unlessManaKind:
					pick := len(d.Options) - 1
					for _, o := range d.Options {
						if o.Kind == "activate" {
							pick = o.Index
							break
						}
					}
					d = kr2Answer(t, e, d, pick)
				default:
					t.Fatalf("unexpected decision %+v", d)
				}
			}
			if z := e.G.Obj(bear).Zone; z != tc.want {
				t.Fatalf("%s: Leak Bear is on %s, want %s", tc.name, z, tc.want)
			}
		})
	}
}
