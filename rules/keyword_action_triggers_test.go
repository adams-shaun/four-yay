package rules

// trig:Forage / trig:ManifestDread / trig:CollectEvidence (cli-20261006T024353Z-ce4a0d59):
// each keyword action emits a marker carrying the acting player, and the
// matcher reads ValidPlayer$ against it.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func drainKeywordAction(t *testing.T, e *Engine) {
	t.Helper()
	e.pending = nil
	e.priorityRound()
	drainSetMechanic(t, e, firstOption)
}

// TestKeywordActionTriggersFireOnlyForTheActingPlayer emits each marker for
// the controller (must fire) and then for the opponent (must not).
func TestKeywordActionTriggersFireOnlyForTheActingPlayer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cult := lookup(t, reg, "Corpseberry Cultivator")
	exam := lookup(t, reg, "Evidence Examiner")
	e := corpusEngine(t, reg, []*cards.Card{cult, exam}, nil)
	cID := addToBattlefield(t, e, cult, 0)
	xID := addToBattlefield(t, e, exam, 0)
	for _, id := range []state.ObjID{cID, xID} {
		if e.G.Obj(id).Zone != state.ZBattlefield {
			t.Fatalf("precondition: %d is not on the battlefield", id)
		}
	}
	clues := func() int { return countTokensNamed(t, e, "Clue Token") }
	if counterCount(e.G.Obj(cID), "P1P1") != 0 || clues() != 0 {
		t.Fatal("precondition: counters or Clues already present")
	}
	// The opponent acting must not fire either trigger.
	e.emit(events.Event{Kind: events.ForageAction, Player: 1})
	drainKeywordAction(t, e)
	e.emit(events.Event{Kind: events.CollectEvidenceAction, Player: 1})
	drainKeywordAction(t, e)
	if n := counterCount(e.G.Obj(cID), "P1P1"); n != 0 {
		t.Fatalf("opponent's forage fired Corpseberry Cultivator (%d counters)", n)
	}
	if n := clues(); n != 0 {
		t.Fatalf("opponent's collect evidence fired Evidence Examiner (%d Clues)", n)
	}
	// An unrelated marker must not fire them.
	e.emit(events.Event{Kind: events.ManifestDreadAction, Player: 0})
	drainKeywordAction(t, e)
	if counterCount(e.G.Obj(cID), "P1P1") != 0 || clues() != 0 {
		t.Fatal("a manifest dread marker fired the forage / collect evidence triggers")
	}
	e.emit(events.Event{Kind: events.ForageAction, Player: 0})
	drainKeywordAction(t, e)
	e.emit(events.Event{Kind: events.CollectEvidenceAction, Player: 0})
	drainKeywordAction(t, e)
	if n := counterCount(e.G.Obj(cID), "P1P1"); n != 1 {
		t.Fatalf("own forage gave %d counters, want 1", n)
	}
	if n := clues(); n != 1 {
		t.Fatalf("own collect evidence made %d Clues, want 1", n)
	}
}

// TestParanormalAnalystReturnsACardPutIntoGraveyardThisWay drives a real
// manifest dread (an inline sorcery) and checks the trigger's "this way" pool
// is exactly the cards the action milled.
func TestParanormalAnalystReturnsACardPutIntoGraveyardThisWay(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	an := lookup(t, reg, "Paranormal Analyst")
	spell := card(t, "Name:Dread Spell\nManaCost:0\nTypes:Sorcery\nA:SP$ ManifestDread\nOracle:x\n")
	e := corpusEngine(t, reg, []*cards.Card{an, spell}, nil)
	aID := addToBattlefield(t, e, an, 0)
	sID := moveByName(t, e, 0, "Dread Spell", state.ZHand)
	if e.G.Obj(aID).Zone != state.ZBattlefield {
		t.Fatal("precondition: Paranormal Analyst is not on the battlefield")
	}
	gyBefore := len(e.G.Zone(state.ZGraveyard, 0))
	e.pending = nil
	e.priorityRound()
	castIdx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == sID {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("precondition: Dread Spell not castable: %+v", e.Pending().Options)
	}
	submitChoices(t, e, castIdx)
	drainSetMechanic(t, e, firstOption)
	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.ManifestDreadAction && ev.Player == 0 }); n != 1 {
		t.Fatalf("%d ManifestDreadAction markers, want 1", n)
	}
	var milled []state.ObjID
	for _, ev := range e.L.Events {
		if ev.Kind == events.ManifestDreadAction {
			milled = ev.IDs
		}
	}
	if len(milled) != 1 {
		t.Fatalf("manifest dread milled %v, want exactly one card", milled)
	}
	if got := e.G.Obj(milled[0]).Zone; got != state.ZHand {
		t.Fatalf("the card put into the graveyard this way is in %s, want hand", got)
	}
	// Spell went to the graveyard (+1), the milled card came back out.
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != gyBefore+1 {
		t.Fatalf("graveyard %d -> %d, want only the spell added", gyBefore, got)
	}
}

// TestRealForageAndCollectEvidenceCostsEmitTheMarker pays the two real costs
// (forage by sacrificing a Food; collect evidence from the graveyard) and
// requires the matching marker for the paying seat.
func TestRealForageAndCollectEvidenceCostsEmitTheMarker(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	forager := card(t, "Name:Forager\nManaCost:0\nTypes:Creature Elf\nPT:1/1\nA:AB$ Pump | Cost$ Forage | NumAtt$ 1\nOracle:x\n")
	sleuth := card(t, "Name:Sleuth\nManaCost:0\nTypes:Creature Elf\nPT:1/1\nA:AB$ Pump | Cost$ CollectEvidence<1> | NumAtt$ 1\nOracle:x\n")
	food := lookup(t, reg, "Bagel and Schmear")
	fodder := lookup(t, reg, "Llanowar Elves")
	e := corpusEngine(t, reg, []*cards.Card{forager, sleuth, food, fodder}, nil)
	fID := addToBattlefield(t, e, forager, 0)
	sID := addToBattlefield(t, e, sleuth, 0)
	addToBattlefield(t, e, food, 0)
	g := moveByName(t, e, 0, "Llanowar Elves", state.ZGraveyard)
	if e.G.Obj(g).Zone != state.ZGraveyard {
		t.Fatal("precondition: the evidence card is not in the graveyard")
	}
	count := func(k events.Kind) int {
		return countEvents(e, func(ev events.Event) bool { return ev.Kind == k && ev.Player == 0 })
	}
	if count(events.ForageAction)+count(events.CollectEvidenceAction) != 0 {
		t.Fatal("precondition: a marker was already emitted")
	}
	for _, id := range []state.ObjID{fID, sID} {
		e.pending = nil
		e.priorityRound()
		opt := abilityOption(t, e, id, 0)
		submitChoices(t, e, opt.Index)
		drainSetMechanic(t, e, firstOption)
	}
	if n := count(events.ForageAction); n != 1 {
		t.Fatalf("%d ForageAction markers, want 1", n)
	}
	if n := count(events.CollectEvidenceAction); n != 1 {
		t.Fatalf("%d CollectEvidenceAction markers, want 1", n)
	}
}
