package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// resolveChooseSource resolves the SP$ ChooseSource head of a spell object
// controlled by its owner, answers the posed KChoose with the option whose
// object id is want, and asserts the answer landed on the spell's own Chosen
// list (the binding the registered replacement's ValidSource$ gate reads).
func resolveChooseSource(t *testing.T, e *Engine, spell state.ObjID, want state.ObjID) {
	t.Helper()
	o := e.G.Obj(spell)
	sa := o.Face().SpellAbility()
	effects.Resolve(e, &effects.Ctx{Source: spell, Controller: o.Controller, SVars: o.Face().SVars}, sa)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("ChooseSource did not pose a KChoose, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Obj == want {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("object %d not among ChooseSource options %+v", want, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit ChooseSource answer: %v", err)
	}
	if len(o.Chosen) != 1 || o.Chosen[0].Obj != want {
		t.Fatalf("chosen = %+v, want [%d]", o.Chosen, want)
	}
}

// TestDeflectingPalmPreventsChosenSourceAndReflects is the end-to-end pin for
// api:ChooseSource on the real corpus card: the spell's SP$ ChooseSource
// poses a real KChoose over the damage sources on the board, the answered
// source rides into the spell's Chosen list, the chained DB$ Effect registers
// the RPrevent replacement whose ValidSource$ Card.ChosenCardStrict gate
// matches exactly that source, and the prevented amount is dealt to that
// source's controller by the Defined$ ChosenCardController body.
func TestDeflectingPalmPreventsChosenSourceAndReflects(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	e.pending = nil
	// The chosen damage source lives on seat 1's battlefield; a second,
	// unchosen source pins that only the chosen one is prevented.
	chosen := onBoard(t, e, 1, "Name:Aggressor\nManaCost:R\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n")
	other := onBoard(t, e, 1, "Name:Bystander\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nOracle:x\n")
	palm := e.G.AddObject(mustCorpusCard(t, reg, "Deflecting Palm"), 0)
	palm.Zone = state.ZStack

	resolveChooseSource(t, e, palm.ID, chosen)

	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life

	// The chosen source's damage to seat 0 is prevented and reflected.
	e.damaging = chosen
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 3})
	e.damaging = 0
	if got := e.G.Players[0].Life; got != life0 {
		t.Fatalf("seat 0 life = %d, want %d: the chosen source's damage was not prevented", got, life0)
	}
	if got := e.G.Players[1].Life; got != life1-3 {
		t.Fatalf("seat 1 life = %d, want %d: the prevented amount was not dealt to the source's controller", got, life1-3)
	}

	// An UNCHOSEN source's damage is untouched — the ValidSource$ gate is
	// scoped to the chosen source, not every source.
	e.damaging = other
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 2})
	e.damaging = 0
	if got := e.G.Players[0].Life; got != life0-2 {
		t.Fatalf("seat 0 life = %d, want %d: an unchosen source's damage must not be prevented", got, life0-2)
	}

	// The chosen source's damage to a DIFFERENT player is untouched — the
	// ValidTarget$ You gate scopes the promise to the spell's controller.
	life1 = e.G.Players[1].Life
	e.damaging = chosen
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 4})
	e.damaging = 0
	if got := e.G.Players[1].Life; got != life1-4 {
		t.Fatalf("seat 1 life = %d, want %d: ValidTarget$ You must not prevent damage to another player", got, life1-4)
	}
}

// TestDeflectingPalmPreventsOnlyTheNextDamage pins the CR 615 one-shot the
// card's own script spells: RPreventNextFromSource is registered by a DB$
// Effect whose ReplaceWith$ body chains SubAbility$ ExileEffect (`DB$
// ChangeZone | Defined$ Self | Origin$ Command | Destination$ Exile`),
// Forge's idiom for the implicit Command-zone effect object ending itself
// after one use. The first damage event from the chosen source is prevented
// and reflected; every later one is untouched. Without the effect-ending the
// promise would persist for the whole turn and prevent (and reflect) EVERY
// subsequent damage event from the chosen source -- the newly-live defect
// this pins.
func TestDeflectingPalmPreventsOnlyTheNextDamage(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	e.pending = nil
	chosen := onBoard(t, e, 1, "Name:Aggressor\nManaCost:R\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n")
	palm := e.G.AddObject(mustCorpusCard(t, reg, "Deflecting Palm"), 0)
	palm.Zone = state.ZStack

	resolveChooseSource(t, e, palm.ID, chosen)

	frames := func() int {
		n := 0
		for _, ce := range e.continuous {
			if ce.Source == palm.ID && ce.ReplacementEvent == "DamageDone" {
				n++
			}
		}
		return n
	}
	if n := frames(); n != 1 {
		t.Fatalf("precondition: %d DamageDone registrations from the spell, want 1", n)
	}
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	// First event: prevented, reflected to the source's controller.
	e.damaging = chosen
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 3})
	e.damaging = 0
	if got := e.G.Players[0].Life; got != life0 {
		t.Fatalf("seat 0 life = %d, want %d: the first damage was not prevented", got, life0)
	}
	if got := e.G.Players[1].Life; got != life1-3 {
		t.Fatalf("seat 1 life = %d, want %d: the prevented amount was not reflected", got, life1-3)
	}
	if n := frames(); n != 0 {
		t.Fatalf("after one application %d registrations remain, want the one-shot ended", n)
	}
	// Second event from the SAME source: the one-shot is spent, so it lands
	// in full and reflects nothing.
	life0, life1 = e.G.Players[0].Life, e.G.Players[1].Life
	e.damaging = chosen
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 4})
	e.damaging = 0
	if got := e.G.Players[0].Life; got != life0-4 {
		t.Fatalf("seat 0 life = %d, want %d: the one-shot must not prevent a second damage event", got, life0-4)
	}
	if got := e.G.Players[1].Life; got != life1 {
		t.Fatalf("seat 1 life = %d, want %d: a spent one-shot must not reflect a second time", got, life1)
	}
}

// TestChooseSourceNoCandidateDoesNotWedge pins the zero-candidate shape: a
// ChooseSource whose Choices$ filter matches nothing poses no decision (the
// empty-answer-only KChoose is absorbed by effects.Ask), records no chosen
// source, and the follow-up replacement therefore never fires — damage flows
// through untouched rather than the seat being stranded on an ask it cannot
// answer.
func TestChooseSourceNoCandidateDoesNotWedge(t *testing.T) {
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	e.pending = nil
	// Only a green creature exists, so Card.BlueSource matches nothing — not
	// even the (white/red) Deflecting Palm itself.
	onBoard(t, e, 1, "Name:Elf\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nOracle:x\n")
	palm := e.G.AddObject(mustCorpusCard(t, reg, "Deflecting Palm"), 0)
	palm.Zone = state.ZStack
	sa := palm.Face().SpellAbility()
	redSpec := *sa
	redSpec.Params = map[string]string{}
	for k, v := range sa.Params {
		redSpec.Params[k] = v
	}
	redSpec.Params["Choices"] = "Card.BlueSource"
	effects.Resolve(e, &effects.Ctx{Source: palm.ID, Controller: 0, SVars: palm.Face().SVars}, &redSpec)
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("zero-candidate ChooseSource posed a decision: %+v", d)
	}
	if len(palm.Chosen) != 0 {
		t.Fatalf("chosen = %+v, want nothing", palm.Chosen)
	}
}
