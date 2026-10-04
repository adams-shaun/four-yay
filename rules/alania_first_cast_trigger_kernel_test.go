package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Restored from the W3 legacy removal: Alania, Divergent Storm cast-trigger
// pins, driven on the resolution kernel (e.resolveTop is a kernel probe in
// tests).

func TestAlaniaFirstInstantTriggersAndCopies(t *testing.T) {
	t.Parallel()
	probe := card(t, "Name:Probe\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Defined$ You | NumCards$ 1\nOracle:Draw a card.\n")
	e := alaniaEngine(t, probe)
	before := opponentHand(t, e)
	kr3CastAlaniaProbe(t, e, "Probe")
	if got := opponentHand(t, e); got != before+1 {
		t.Fatalf("the target opponent drew %d cards, want 1 (the first instant of the turn must fire)", got-before)
	}
}

func TestAlaniaSecondInstantDoesNotTrigger(t *testing.T) {
	t.Parallel()
	probe := card(t, "Name:Probe\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Defined$ You | NumCards$ 1\nOracle:Draw a card.\n")
	probe2 := card(t, "Name:Probe II\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Defined$ You | NumCards$ 1\nOracle:Draw a card.\n")
	e := alaniaEngine(t, probe, probe2)
	kr3CastAlaniaProbe(t, e, "Probe") // the first instant: fires
	before := opponentHand(t, e)
	kr3CastAlaniaProbe(t, e, "Probe II") // the second instant: must not fire
	if got := opponentHand(t, e); got != before {
		t.Fatalf("the second instant drew %d cards for the opponent, want 0 (EQ1 fails at 2)", got-before)
	}
}

func TestAlaniaFirstSorceryAfterAnInstantTriggers(t *testing.T) {
	t.Parallel()
	probe := card(t, "Name:Probe\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Defined$ You | NumCards$ 1\nOracle:Draw a card.\n")
	verse := card(t, "Name:Verse\nManaCost:1 G\nTypes:Sorcery\nA:SP$ Draw | Defined$ You | NumCards$ 1\nOracle:Draw a card.\n")
	e := alaniaEngine(t, probe, verse)
	kr3CastAlaniaProbe(t, e, "Probe") // the first instant: fires
	before := opponentHand(t, e)
	kr3CastAlaniaProbe(t, e, "Verse") // the first SORCERY: the Each disjunction fires on it
	if got := opponentHand(t, e); got != before+1 {
		t.Fatalf("the first sorcery after an instant drew %d cards for the opponent, want 1 (Each semantics)", got-before)
	}
}

func TestAlaniaSecondInstantAfterASorceryDoesNotTrigger(t *testing.T) {
	t.Parallel()
	// The round-2 review edge the original pins missed: instant → sorcery →
	// second instant. The sorcery alternative's tally legitimately held EQ1
	// on the sorcery cast, but the SECOND INSTANT matches only the instant
	// alternative, whose tally is 2 — the Each loop must skip alternatives
	// the current cast does not match, or the trigger false-fires here.
	probe := card(t, "Name:Probe\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Defined$ You | NumCards$ 1\nOracle:Draw a card.\n")
	verse := card(t, "Name:Verse\nManaCost:1 G\nTypes:Sorcery\nA:SP$ Draw | Defined$ You | NumCards$ 1\nOracle:Draw a card.\n")
	probe2 := card(t, "Name:Probe II\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Defined$ You | NumCards$ 1\nOracle:Draw a card.\n")
	e := alaniaEngine(t, probe, verse, probe2)
	kr3CastAlaniaProbe(t, e, "Probe") // the first instant: fires
	kr3CastAlaniaProbe(t, e, "Verse") // the first sorcery: fires
	before := opponentHand(t, e)
	kr3CastAlaniaProbe(t, e, "Probe II") // the second instant: must not fire
	if got := opponentHand(t, e); got != before {
		t.Fatalf("the second instant after a sorcery drew %d cards for the opponent, want 0 (it matches no alternative whose first it is)", got-before)
	}
}

// kr3CastAlaniaProbe is castAlaniaProbe for the kernel: the stack is resolved
// by passing priority through Submit (so each resolution runs under the
// tape kernel and its asks are posed as real decisions), instead of calling
// resolveTop beneath a pending priority decision.
func kr3CastAlaniaProbe(t *testing.T, e *Engine, name string) {
	t.Helper()
	id := state.ObjID(0)
	for _, hid := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(hid); o != nil && o.Face() != nil && o.Face().Name == name {
			id = hid
		}
	}
	if id == 0 {
		t.Fatalf("%s not in hand", name)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		e.pending = nil
		e.priorityRound()
		d = e.Pending()
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		e.pending = nil
		e.priorityRound()
		d = e.Pending()
		for _, o := range d.Options {
			if o.Kind == "cast" && o.Obj == id {
				idx = o.Index
			}
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %s: %+v", name, d.Options)
	}
	submitChoices(t, e, idx)
	for i := 0; i < 80; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision pending while draining %s", name)
		}
		switch d.Kind {
		case decision.KPriority:
			if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 && d.Player == 0 {
				return
			}
			submitChoices(t, e, tapePassIndex(d))
		case decision.KTriggerOptional:
			submitChoices(t, e, 0)
		case decision.KTarget:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "player" && o.Player == 1 {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("seat 1 not offered as a target: %+v", d.Options)
			}
			submitChoices(t, e, idx)
		default:
			submitChoices(t, e, 0)
		}
	}
	t.Fatalf("the drain did not settle after casting %s", name)
}
