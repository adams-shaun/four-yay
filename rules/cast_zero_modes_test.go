package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestUpToModalCastWithNoLegalModeAnnouncesZero is the cardfuzz batch1 line 7
// panic: Call Damage Control ("Choose up to two" -- MinCharmNum$ 0 -- over
// four graveyard-targeting modes) cast with an empty graveyard left no legal
// mode, and castModeAsk posted a Min 0 / Max 0 / 0-option KModes, which
// Engine.ask rejects by panicking. The only legal announcement is zero modes
// (CR 601.2b), so it is recorded without an ask; and a zero-mode
// announcement must resolve as nothing even when a legal target appears
// before resolution, rather than re-posing the modal ask at resolution.
func TestUpToModalCastWithNoLegalModeAnnouncesZero(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Call Damage Control"))
	spell := handIDsByFace(e)["Call Damage Control"]
	e.G.Players[0].Pool[state.MG] = 2
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, spell)) // panicked before the fix
	if d := e.Pending(); d != nil && d.Kind == decision.KModes {
		t.Fatalf("a zero-legal-mode cast posed a modes ask: %+v", d)
	}
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack {
		t.Fatalf("Call Damage Control zone = %v, want on the stack", o)
	} else if o.ChosenModes == nil || len(o.ChosenModes) != 0 {
		t.Fatalf("ChosenModes = %#v, want the non-nil empty zero-mode announcement", o.ChosenModes)
	}
	modeChosen := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.ModeChosen && ev.Obj == spell {
			modeChosen++
		}
	}
	if modeChosen != 1 {
		t.Fatalf("ModeChosen markers for the cast = %d, want 1", modeChosen)
	}
	// A creature card reaches the graveyard before resolution: the spell
	// announced no mode, so it must not now ask for one.
	bear := graveCard(e, mustCorpusCard(t, reg, "Grizzly Bears"), 0, 0)
	for i := 0; i < 20 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no pending decision while the spell is on the stack")
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision while resolving a zero-mode spell: %+v", d)
		}
		castFirst(t, e, "pass")
	}
	if z := e.G.Obj(spell).Zone; z != state.ZGraveyard {
		t.Fatalf("Call Damage Control zone = %s, want Graveyard after resolving", z)
	}
	if z := e.G.Obj(bear).Zone; z != state.ZGraveyard {
		t.Fatalf("Grizzly Bears zone = %s, want it left in the graveyard", z)
	}
}
