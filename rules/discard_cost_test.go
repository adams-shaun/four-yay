package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

const discardCostJunk = "Name:Discard Fodder\nManaCost:1\nTypes:Artifact\nOracle:x\n"

func TestLionsEyeDiamondDiscardsWholeHandWithoutMana(t *testing.T) {
	t.Parallel()
	const led = "Name:Lion's Eye Diamond\nTypes:Artifact\n" +
		"A:AB$ Mana | Cost$ Sac<1/CARDNAME> Discard<0/Hand> | Produced$ Any | Amount$ 3 | InstantSpeed$ True\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 701, led, discardCostJunk, discardCostJunk, discardCostJunk)
	moveSeeded(t, e, 0, led, state.ZBattlefield)
	e.Advance()

	hand := append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...)
	if len(hand) < 3 {
		t.Fatalf("fixture hand has %d cards, want at least 3", len(hand))
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatal("fixture unexpectedly has floating mana")
	}
	activateMana(t, e, id)

	// The discard-all and forced self-sacrifice are costs, not choices. The
	// only question after activation is which colour the already-paid ability
	// produces; the old parser instead demanded one generic mana and withheld
	// the activation entirely.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 5 || d.Options[0].Kind != "mana" {
		t.Fatalf("post-activation decision = %+v, want only the colour choice", d)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 0 {
		t.Fatalf("hand after LED cost = %d, want empty", got)
	}
	for _, oid := range hand {
		if e.G.Obj(oid).Zone != state.ZGraveyard {
			t.Fatalf("hand card %d ended in %s, want graveyard", oid, e.G.Obj(oid).Zone)
		}
	}
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatalf("LED ended in %s, want graveyard", e.G.Obj(id).Zone)
	}
	submitChoices(t, e, manaOption(t, d, "R"))
	if got := e.G.Players[0].Pool[state.MR]; got != 3 {
		t.Fatalf("red mana after LED = %d, want 3", got)
	}
	replayCheck(t, e, cfg)

	t.Run("Forge one-count Hand spelling", func(t *testing.T) {
		const source = "Name:Discard Hand Engine\nTypes:Artifact\n" +
			"A:AB$ Mana | Cost$ Discard<1/Hand> | Produced$ C\nOracle:x\n"
		e, _, id := newFixtureDeck(t, 705, source, discardCostJunk, discardCostJunk)
		moveSeeded(t, e, 0, source, state.ZBattlefield)
		e.Advance()
		if len(e.G.Zone(state.ZHand, 0)) < 2 {
			t.Fatal("fixture needs a nonempty hand")
		}
		activateMana(t, e, id)
		if got := len(e.G.Zone(state.ZHand, 0)); got != 0 {
			t.Fatalf("Discard<1/Hand> left %d cards; Hand means the whole hand", got)
		}
	})
}

func TestDiscardCardCostAsksPlayerAndCommitsChosenCards(t *testing.T) {
	t.Parallel()
	const source = "Name:Discard Engine\nTypes:Artifact\n" +
		"A:AB$ Mana | Cost$ Discard<2/Card> | Produced$ B\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 702, source, discardCostJunk, discardCostJunk, discardCostJunk)
	moveSeeded(t, e, 0, source, state.ZBattlefield)
	e.Advance()

	before := append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...)
	activateMana(t, e, id)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 2 || d.Max != 2 || len(d.Options) < 3 || d.Options[0].Kind != "mana_discard" {
		t.Fatalf("discard-cost decision = %+v, want choose exactly two from hand", d)
	}
	chosen := []state.ObjID{d.Options[0].Obj, d.Options[2].Obj}
	submitChoices(t, e, d.Options[0].Index, d.Options[2].Index)

	for _, oid := range chosen {
		if e.G.Obj(oid).Zone != state.ZGraveyard {
			t.Fatalf("chosen card %d ended in %s, want graveyard", oid, e.G.Obj(oid).Zone)
		}
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != len(before)-2 {
		t.Fatalf("hand size after discard cost = %d, want %d", got, len(before)-2)
	}
	if got := e.G.Players[0].Pool[state.MB]; got != 1 {
		t.Fatalf("black mana after discard cost = %d, want 1", got)
	}
	if hasEvent(e, events.AbilityPush, id) {
		t.Fatal("mana ability incorrectly used the stack")
	}
	replayCheck(t, e, cfg)
}

func TestRandomDiscardCostUsesEngineRNGWithoutChoice(t *testing.T) {
	t.Parallel()
	const source = "Name:Random Discard Engine\nTypes:Artifact\n" +
		"A:AB$ GainLife | Cost$ Discard<2/Random> | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 703, source, discardCostJunk, discardCostJunk, discardCostJunk)
	moveSeeded(t, e, 0, source, state.ZBattlefield)
	e.Advance()

	beforeHand := append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...)
	beforeDraws := e.RNGDraws()
	submitChoices(t, e, abilityOption(t, e, id, 0).Index)
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("random discard offered a card choice: %+v", d)
	}
	if got := e.RNGDraws() - beforeDraws; got != 2 {
		t.Fatalf("random discard RNG draws = %d, want 2", got)
	}
	moved := 0
	for _, oid := range beforeHand {
		if e.G.Obj(oid).Zone == state.ZGraveyard {
			moved++
		}
	}
	if moved != 2 || len(e.G.Zone(state.ZHand, 0)) != len(beforeHand)-2 {
		t.Fatalf("random discard moved %d cards; hand %d -> %d", moved, len(beforeHand), len(e.G.Zone(state.ZHand, 0)))
	}
	if !hasEvent(e, events.AbilityPush, id) {
		t.Fatal("ability was not pushed after random discard")
	}
	replayCheck(t, e, cfg)
}

func TestDiscardSelfReferencesMatchOnlyTheSourceInHand(t *testing.T) {
	t.Parallel()
	for _, spec := range []string{"CARDNAME", "NICKNAME"} {
		t.Run(spec, func(t *testing.T) {
			source := "Name:Channel Self " + spec + "\nTypes:Creature Spirit\nPT:1/1\nOracle:x\n"
			e, _, id := newFixtureDeck(t, 704, source, source)
			candidates := pay.DiscardCandidates(asPayer(e), 0, id, CostPart{N: 1, Spec: spec}, false, nil)
			if len(candidates) != 1 || candidates[0] != id {
				t.Fatalf("%s candidates = %v, want only source %d (never the same-name copy)", spec, candidates, id)
			}
		})
	}
}
