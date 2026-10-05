package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Carbonize exercises the qualified ThisTargetedCard.Creature selector at
// DealDamage's shared replacement-registration point.
func TestDealDamageReplaceDyingThisTargetedCard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	carbonize := mustCorpusCard(t, reg, "Carbonize")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg := censusEngine(t, 8401, []*cards.Card{carbonize}, []*cards.Card{bear})
	cardToHand(t, e, carbonize)
	bearID := moveOwnerCard(t, e, 1, bear, state.ZBattlefield)
	if o := e.G.Obj(bearID); o.Zone != state.ZBattlefield || e.Toughness(bearID) != 2 {
		t.Fatalf("precondition: target bear is zone %s with toughness %d, want battlefield 2/2", o.Zone, e.Toughness(bearID))
	}
	addMana(t, e, 0, "RRR")
	castFirst(t, e, "cast")
	targetObject(t, e, bearID)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(bearID); o.Zone != state.ZExile {
		t.Fatalf("targeted lethal-damage bear should be exiled, got %s", o.Zone)
	}
	assertReplaceDyingRemembers(t, e, bearID)
	replayCheck(t, e, cfg)
}

// Unnatural Aggression's Fight damages both participants, but its bare
// ThisTargetedCard replacement belongs only to the opponent's chosen target.
func TestFightReplaceDyingThisTargetedCard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	aggression := mustCorpusCard(t, reg, "Unnatural Aggression")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	giant := mustCorpusCard(t, reg, "Hill Giant")
	e, cfg := censusEngine(t, 8402, []*cards.Card{aggression, giant}, []*cards.Card{bear})
	cardToHand(t, e, aggression)
	fighter := moveOwnerCard(t, e, 0, giant, state.ZBattlefield)
	target := moveOwnerCard(t, e, 1, bear, state.ZBattlefield)
	if o := e.G.Obj(fighter); o.Zone != state.ZBattlefield || e.Power(fighter) <= e.Toughness(target) {
		t.Fatalf("precondition: fighter zone=%s power=%d, target toughness=%d; expected battlefield fighter whose power is lethal", o.Zone, e.Power(fighter), e.Toughness(target))
	}
	if o := e.G.Obj(target); o.Zone != state.ZBattlefield || e.Power(target) >= e.Toughness(fighter) {
		t.Fatalf("precondition: target zone=%s power=%d, fighter toughness=%d; expected battlefield target that does not kill fighter", o.Zone, e.Power(target), e.Toughness(fighter))
	}
	addMana(t, e, 0, "2GG")
	castFirst(t, e, "cast")
	targetObject(t, e, fighter)
	targetObject(t, e, target)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(target); o.Zone != state.ZExile {
		t.Fatalf("targeted opponent creature should be exiled, got %s", o.Zone)
	}
	if o := e.G.Obj(fighter); o.Zone != state.ZBattlefield {
		t.Fatalf("non-target fighter should remain on battlefield, got %s", o.Zone)
	}
	assertReplaceDyingRemembers(t, e, target)
	replayCheck(t, e, cfg)
}

func assertReplaceDyingRemembers(t *testing.T, e *Engine, want state.ObjID) {
	t.Helper()
	n := 0
	for _, ce := range e.continuous {
		if ce.ReplacementEvent != "Moved" {
			continue
		}
		n++
		if len(ce.Remembered) != 1 || ce.Remembered[0] != want {
			t.Fatalf("replacement remembered %v, want only targeted object %d", ce.Remembered, want)
		}
	}
	if n != 1 {
		t.Fatalf("want exactly one Moved replacement, got %d", n)
	}
}
