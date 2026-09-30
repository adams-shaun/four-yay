package rules

// Origin$ zone-set coverage for the two remaining single-word readers of the
// parent ticket's class (agent-20260930T010244Z-9cd73a28):
//
//   - rules/replacement_match.go replacementConditionMatches, case "Moved"
//   - rules/statics.go panharmoniconEchoes
//
// The parent (agent-20260930T005535Z-b621b37a) fixed the ChangesZone trigger
// matcher to read Origin$ with effects.ParseZones. Both sites here read it
// with the single-word effects.ParseZone, which resolves a comma list like
// "Hand,Graveyard" to state.ZGraveyard -- so a list would silently degrade to
// graveyard-origin-only. Zero corpus carriers exist today, so this is a
// fail-closed class-close; the scripts below are authored inline from printed
// Oracle text and never copied from the GPL .cards/ corpus.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// originListPeaceSrc is a Rest-in-Peace-shaped replacement whose Origin$ is a
// comma set: "if a card would be put into a graveyard from your hand or a
// graveyard, exile it instead." ReplaceWith$ names the card it is acting on
// via Defined$ ReplacedCard.
const originListPeaceSrc = `Name:List Peace
ManaCost:1 W
Types:Enchantment
R:Event$ Moved | Origin$ Hand,Graveyard | Destination$ Graveyard | ReplaceWith$ Exile | Description$ If a card would be put into a graveyard from your hand or a graveyard, exile it instead.
SVar:Exile:DB$ ChangeZone | Defined$ ReplacedCard | Destination$ Exile
Oracle:x
`

// originListMoverSrc is a freely-authored creature with no abilities; it is
// only ever the object being moved.
const originListMoverSrc = `Name:List Mover
ManaCost:1 G
Types:Creature Bear
PT:2/2
Oracle:x
`

// originListEchoSrc is the Panharmonicon-shaped static under test: it doubles
// ChangesZone triggers whose causing move began in the controller's hand or a
// graveyard. No ValidCard$ scoping, so the static applies to any permanent
// whose zone-change trigger fires.
const originListEchoSrc = `Name:List Echo
ManaCost:2
Types:Artifact
S:Mode$ Panharmonicon | ValidMode$ ChangesZone | ValidCard$ Permanent.YouCtrl | Origin$ Hand,Graveyard | Description$ If a permanent entering from your hand or a graveyard causes a triggered ability of a permanent you control to trigger, that ability triggers an additional time.
Oracle:x
`

// originListProbeSrc has one ETB trigger and one death trigger, both a plain
// gain of 1 life, so the number of times a single trigger fires is directly
// observable in seat 0's life total.
const originListProbeSrc = `Name:List Probe
ManaCost:1 G
Types:Creature Bear
PT:2/2
T:Mode$ ChangesZone | Origin$ Hand,Graveyard | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ EtbLife | TriggerZones$ Battlefield
T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Card.Self | Execute$ DiesLife | TriggerZones$ Battlefield
SVar:EtbLife:DB$ GainLife | LifeAmount$ 1
SVar:DiesLife:DB$ GainLife | LifeAmount$ 1
Oracle:x
`

// originListZone asserts the mover really sits in from before the move, so a
// mis-seeded fixture fails loudly instead of making the assertion vacuous.
func originListZone(t *testing.T, e *Engine, id state.ObjID, from state.Zone) *state.Object {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil {
		t.Fatalf("precondition: mover %d missing", id)
	}
	if o.Zone != from {
		t.Fatalf("precondition: mover %d in %v, want %v", id, o.Zone, from)
	}
	return o
}

// TestOriginListSiblingsReplacementMovedReadsTheWholeSet is site 1: the
// R:Event$ Moved Origin$ reader. A hand->graveyard move and a
// graveyard-origin move must both be redirected by the list; a
// battlefield->graveyard move must not be. The hand case is the
// discriminator: the old single-word reader turned "Hand,Graveyard" into
// ZGraveyard and would have let the card reach the graveyard.
func TestOriginListSiblingsReplacementMovedReadsTheWholeSet(t *testing.T) {
	t.Parallel()

	t.Run("hand origin is redirected to the ReplaceWith destination", func(t *testing.T) {
		t.Parallel()
		e, _, mover := newFixtureDeck(t, 151, originListMoverSrc)
		onBoardCard(t, e, 0, card(t, originListPeaceSrc))
		originListZone(t, e, mover, state.ZHand)

		e.emit(events.Event{Kind: events.MoveZone, Obj: mover, From: state.ZHand, To: state.ZGraveyard})
		if got := e.G.Obj(mover).Zone; got != state.ZExile {
			t.Fatalf("hand-origin move landed in %v, want exile -- Origin$ Hand,Graveyard must read Hand, "+
				"not degrade to graveyard-only", got)
		}
		if state.ZGraveyard == state.ZExile {
			t.Fatal("precondition: the compared destination values are identical")
		}
	})

	t.Run("graveyard origin is redirected to the ReplaceWith destination", func(t *testing.T) {
		t.Parallel()
		e, _, _ := newFixtureDeck(t, 152, originListMoverSrc)
		onBoardCard(t, e, 0, card(t, originListPeaceSrc))
		// Eventless placement: a logged library->graveyard move would itself
		// be redirected by the replacement, so the mover would never reach
		// the graveyard the precondition needs.
		mover := placeInZone(t, e, 0, state.ZGraveyard, card(t, originListMoverSrc))
		originListZone(t, e, mover, state.ZGraveyard)

		e.emit(events.Event{Kind: events.MoveZone, Obj: mover, From: state.ZGraveyard, To: state.ZGraveyard})
		if got := e.G.Obj(mover).Zone; got != state.ZExile {
			t.Fatalf("graveyard-origin move landed in %v, want exile -- Graveyard is a member of the list", got)
		}
	})

	t.Run("battlefield origin is not redirected", func(t *testing.T) {
		t.Parallel()
		e, _, _ := newFixtureDeck(t, 153, originListMoverSrc)
		onBoardCard(t, e, 0, card(t, originListPeaceSrc))
		mover := onBoardCard(t, e, 0, card(t, originListMoverSrc))
		originListZone(t, e, mover, state.ZBattlefield)

		e.emit(events.Event{Kind: events.MoveZone, Obj: mover, From: state.ZBattlefield, To: state.ZGraveyard})
		if got := e.G.Obj(mover).Zone; got != state.ZGraveyard {
			t.Fatalf("battlefield-origin move landed in %v, want graveyard -- battlefield is NOT in the list", got)
		}
	})
}

// TestOriginListSiblingsPanharmoniconReadsTheWholeSet is site 2: the
// panharmoniconEchoes Origin$ reader. A hand->battlefield entry doubles its
// ETB trigger; a battlefield->graveyard death does not (battlefield is not in
// the list); a graveyard->battlefield entry doubles (Graveyard is a member).
// The no-static baseline proves the +2 is real doubling and the +1 death is a
// fired trigger, not a no-op.
func TestOriginListSiblingsPanharmoniconReadsTheWholeSet(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	enter := func(t *testing.T, withStatic bool) int32 {
		t.Helper()
		var extras []*cards.Card
		if withStatic {
			extras = append(extras, card(t, originListEchoSrc))
		}
		extras = append(extras, card(t, originListProbeSrc))
		e := corpusEngine(t, reg, extras, nil)
		if withStatic {
			onBoardCard(t, e, 0, card(t, originListEchoSrc))
		}
		probe := moveByName(t, e, 0, "List Probe", state.ZHand)
		originListZone(t, e, probe, state.ZHand)
		before := e.G.Players[0].Life
		e.emit(events.Event{Kind: events.MoveZone, Obj: probe, From: state.ZHand, To: state.ZBattlefield})
		answerQuiet(t, e, 60)
		if z := e.G.Obj(probe).Zone; z != state.ZBattlefield {
			t.Fatalf("precondition: probe in %v, want battlefield", z)
		}
		return e.G.Players[0].Life - before
	}

	// Baseline: the ETB trigger alone gains exactly 1 life.
	if got := enter(t, false); got != 1 {
		t.Fatalf("baseline ETB gained %d life, want 1 -- the trigger must fire exactly once with no static", got)
	}
	// The discriminator: with the static, the hand-origin entry doubles to 2.
	if got := enter(t, true); got != 2 {
		t.Fatalf("hand-origin entry gained %d life, want 2 -- Origin$ Hand,Graveyard must read Hand and "+
			"double the ETB trigger", got)
	}

	t.Run("death from the battlefield is not doubled", func(t *testing.T) {
		t.Parallel()
		e := corpusEngine(t, reg, []*cards.Card{card(t, originListEchoSrc), card(t, originListProbeSrc)}, nil)
		onBoardCard(t, e, 0, card(t, originListEchoSrc))
		probe := onBoardCard(t, e, 0, card(t, originListProbeSrc))
		originListZone(t, e, probe, state.ZBattlefield)
		before := e.G.Players[0].Life
		e.emit(events.Event{Kind: events.MoveZone, Obj: probe, From: state.ZBattlefield, To: state.ZGraveyard})
		answerQuiet(t, e, 60)
		if z := e.G.Obj(probe).Zone; z != state.ZGraveyard {
			t.Fatalf("precondition: probe in %v, want graveyard", z)
		}
		if got := e.G.Players[0].Life - before; got != 1 {
			t.Fatalf("battlefield death gained %d life, want 1 -- battlefield is NOT in Origin$ Hand,Graveyard, "+
				"so the dies trigger must fire once", got)
		}
	})

	t.Run("graveyard origin is doubled", func(t *testing.T) {
		t.Parallel()
		e := corpusEngine(t, reg, []*cards.Card{card(t, originListEchoSrc), card(t, originListProbeSrc)}, nil)
		onBoardCard(t, e, 0, card(t, originListEchoSrc))
		probe := moveByName(t, e, 0, "List Probe", state.ZGraveyard)
		originListZone(t, e, probe, state.ZGraveyard)
		before := e.G.Players[0].Life
		e.emit(events.Event{Kind: events.MoveZone, Obj: probe, From: state.ZGraveyard, To: state.ZBattlefield})
		answerQuiet(t, e, 60)
		if z := e.G.Obj(probe).Zone; z != state.ZBattlefield {
			t.Fatalf("precondition: probe in %v, want battlefield", z)
		}
		if got := e.G.Players[0].Life - before; got != 2 {
			t.Fatalf("graveyard-origin entry gained %d life, want 2 -- Graveyard is a member of the list", got)
		}
	})
}
