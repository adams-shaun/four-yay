package rules

// The engine-level end to end for Infernal Tutor (task infernaltutor1), using
// the REAL compiled corpus card: effReveal used to take pool[:1] for a hand
// reveal with no choice, so "Reveal a card from your hand" showed the FIRST
// hand card and the chained search looked for THAT name. Here the pick is
// answered with the second hand card and the library card sharing the CHOSEN
// name is the one that moves to hand.
//
// The effects-package leaf (effects/infernal_tutor_test.go) pins the ask and
// the same-name search OFFER; this file drives the whole cast through the
// engine's own frame stack, which is what actually applies the search's move.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// tutorIndexForObj returns the pending decision's option index whose Obj is
// id, or -1.
func tutorIndexForObj(d *decision.Decision, id state.ObjID) int {
	for _, o := range d.Options {
		if o.Obj == id {
			return o.Index
		}
	}
	return -1
}

// tutorPassIndex returns the pending priority decision's pass option index.
func tutorPassIndex(t *testing.T, d *decision.Decision) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == "pass" {
			return o.Index
		}
	}
	t.Fatalf("no pass option: %+v", d)
	return -1
}

// TestInfernalTutorEngineSearchUsesTheChosenCard casts the real Infernal
// Tutor with a two-card hand (Lightning Bolt, then Forest) and a library
// holding both names, answers the reveal pick with the Forest, and asserts
// the chained search moves the FOREST -- never the Lightning Bolt. The
// pre-fix build revealed the front card (Lightning Bolt) and searched for
// Lightning Bolt.
func TestInfernalTutorEngineSearchUsesTheChosenCard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	tutor := mustCorpusCard(t, reg, "Infernal Tutor")
	bolt := mustCorpusCard(t, reg, "Lightning Bolt")
	forest := mustCorpusCard(t, reg, "Forest")

	e := layerEngine(t)

	// Seat 0's hand: the tutor plus the two reveal candidates, Bolt first.
	spell := e.G.AddObject(tutor, 0)
	spell.Zone = state.ZHand
	handBolt := e.G.AddObject(bolt, 0)
	handBolt.Zone = state.ZHand
	handForest := e.G.AddObject(forest, 0)
	handForest.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{spell.ID, handBolt.ID, handForest.ID})

	// Seat 0's library: both names, Bolt first, so a bug that searched the
	// front hand card would find the Bolt.
	libBolt := e.G.AddObject(bolt, 0)
	libBolt.Zone = state.ZLibrary
	libForest := e.G.AddObject(forest, 0)
	libForest.Zone = state.ZLibrary
	e.G.SetZone(state.ZLibrary, 0, []state.ObjID{libBolt.ID, libForest.ID})

	// Preconditions: the setup is what the assertions depend on.
	if got := e.G.Obj(handForest.ID).Zone; got != state.ZHand {
		t.Fatalf("precondition: hand Forest zone = %s, want hand", got)
	}
	if got := e.G.Obj(libForest.ID).Zone; got != state.ZLibrary {
		t.Fatalf("precondition: library Forest zone = %s, want library", got)
	}

	addMana := func(sym rune) {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(sym), Amount: 1})
	}
	addMana('B') // {1}{B}: the black mana also pays the generic pip
	addMana('B')
	e.pending = nil
	e.beginCast(0, decision.Option{Kind: "cast", Obj: spell.ID})
	e.Advance()

	picked, searched := false, false
	for i := 0; i < 200; i++ {
		if picked && searched {
			break // the reveal and the chained search are both answered
		}
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			submitChoices(t, e, tutorPassIndex(t, d))
			continue
		}
		switch d.ResumeKind {
		case "reveal_pick":
			if idx := tutorIndexForObj(d, handForest.ID); idx >= 0 {
				submitChoices(t, e, idx)
				picked = true
			} else {
				t.Fatalf("reveal pick did not offer the hand Forest: %+v", d)
			}
		case "search":
			if idx := tutorIndexForObj(d, libForest.ID); idx >= 0 {
				submitChoices(t, e, idx)
				searched = true
			} else {
				t.Fatalf("search did not offer the chosen name's library card: %+v", d)
			}
		default:
			t.Fatalf("unexpected mid-resolution ask: kind=%s resume=%q %+v", d.Kind, d.ResumeKind, d)
		}
	}
	if !picked {
		t.Fatal("the reveal pick was never posed")
	}
	if !searched {
		t.Fatal("the chained same-name search was never posed")
	}
	// The chosen card's name drove the search: the library Forest moved to
	// hand, the library Lightning Bolt stayed.
	if got := e.G.Obj(libForest.ID).Zone; got != state.ZHand {
		t.Fatalf("library Forest zone = %s, want hand (search read the chosen name)", got)
	}
	if got := e.G.Obj(libBolt.ID).Zone; got != state.ZLibrary {
		t.Fatalf("library Lightning Bolt zone = %s, want library (search read the unchosen name)", got)
	}
	if got := e.G.Obj(spell.ID).Zone; got == state.ZHand {
		t.Fatalf("Infernal Tutor still in hand: the cast neve resolved")
	}
}
