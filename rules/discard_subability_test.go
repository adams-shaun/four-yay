package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// countLoseLife reports how many life-losing events were emitted for player
// p — the raw count, not the net. It is the B1 metric: for a chained
// Discard | SubAbility$ DBLoseLife, the SubAbility must fire exactly once,
// so exactly one -2 LifeChange lands on the caster.
func countLoseLife(e *Engine, p state.PlayerID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.LifeChange && ev.Player == p && ev.Amount < 0 {
			n++
		}
	}
	return n
}

// TestThoughtseizeLosesExactlyTwoLife is the B1 regression, at the engine
// level with Thoughtseize's real corpus shape: SP$ Discard | Mode$
// RevealYouChoose | SubAbility$ DBLoseLife. Because Discard is an asking
// primitive sitting at the head of a SubAbility chain, effects.Resolve used
// to keep descending into sa.Sub after the ask suspended — so DBLoseLife
// fired on the first pass (before the answer existed) AND again when the
// answered decision re-entered at the Discard SA: the caster lost 4 life.
// The fix suspends the chain, so DBLoseLife fires exactly once, after the
// chosen card is discarded, for exactly 2.
func TestThoughtseizeLosesExactlyTwoLife(t *testing.T) {
	t.Parallel()
	ts := "Name:PiT\nManaCost:B\nTypes:Sorcery\n" +
		"A:SP$ Discard | ValidTgts$ Player | NumCards$ 1 | Mode$ RevealYouChoose | DiscardValid$ Card.nonLand | SubAbility$ DBLoseLife\n" +
		// No Defined$ -- this mirrors the real thoughtseize.txt verbatim
		// (SVar:DBLoseLife:DB$ LoseLife | LifeAmount$ 2). The card omits it, so
		// the life loss routes through Defined()'s empty case -> {Obj: c.Source}
		// -> the source's controller. A fixture carrying Defined$ You takes a
		// different path and would leave the real card's route unguarded, which
		// is exactly what the gate caught here.
		"SVar:DBLoseLife:DB$ LoseLife | LifeAmount$ 2\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 96, ts)
	frog := e.G.AddObject(card(t, "Name:Frog\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	bird := e.G.AddObject(card(t, "Name:Bird\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	frog.Zone = state.ZHand
	bird.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, []state.ObjID{frog.ID, bird.ID})
	addMana(t, e, 0, "B")

	life := e.G.Players[0].Life
	d := castFixture(t, e, id, 1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected a KModes discard decision, got %+v", d)
	}
	// Choose the second card (bird), deliberately not hand[0] (frog).
	submitChoices(t, e, d.Options[1].Index)
	passUntilStackEmpty(t, e, 20)

	if z := e.G.Obj(bird.ID).Zone; z != state.ZGraveyard {
		t.Fatalf("chosen card (bird) zone = %s, want Graveyard", z)
	}
	if z := e.G.Obj(frog.ID).Zone; z != state.ZHand {
		t.Fatalf("un-chosen card (frog) zone = %s, want Hand", z)
	}
	// The whole point: the SubAbility fired ONCE, not twice.
	if got := e.G.Players[0].Life; got != life-2 {
		t.Fatalf("caster life = %d, want %d (lost exactly 2, not 4) — DBLoseLife ran twice", got, life-2)
	}
	if got := countLoseLife(e, 0); got != 1 {
		t.Fatalf("life-losing events on the caster = %d, want exactly 1 (the SubAbility ran %d times)", got, got)
	}
	if !hasEventKind(e, events.ModeChosen) {
		t.Fatal("no ModeChosen event recorded the discard choice")
	}
	// No replayCheck here: the synthetic frog/bird hand is built with direct
	// AddObject calls outside the event log, so a log-only replay cannot
	// reconstruct it by construction. The real replay-determinism of the B1
	// fix is exercised by the acceptance heads (TestRepoDeckGamesReplayExactly
	// over dimir-tempo's actual Thoughtseize); this test exists to pin the
	// life accounting and the SubAbility's once-only firing.
}

// TestCharmSubAbilityRunsOnce is the modes-caller guard (B1 test #2): an
// existing modal caller with a SubAbility$ must still run its chain exactly
// once. effCharm poses the KModes ask; before the fix, effects.Resolve kept
// walking into sa.Sub on the first pass AND the resume pass, so the
// SubAbility fired twice around the chosen mode. The fix suspends the chain
// generically for every asking primitive (not just Discard), so a Charm with
// a SubAbility$ fires that SubAbility once, after the chosen mode.
//
// The shape is deliberately the one the brief says must keep working: a Charm
// carrying a SubAbility$. Net effect with the mode "Lose 5 life" chosen:
// -5 (the mode) +1 (the SubAbility) = -4. The buggy build stacked a second
// +1 on the first pass for a net -3.
func TestCharmSubAbilityRunsOnce(t *testing.T) {
	t.Parallel()
	charm := "Name:PiC\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ Charm | Choices$ DoGain,DoLose | SubAbility$ DBLife\n" +
		"SVar:DoGain:DB$ GainLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Gain 5 life\n" +
		"SVar:DoLose:DB$ LoseLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Lose 5 life\n" +
		"SVar:DBLife:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 91, charm)
	addMana(t, e, 0, "R")

	life := e.G.Players[0].Life
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected a KModes decision, got %+v", d)
	}
	submitChoices(t, e, 1) // mode 2: Lose 5 life, the CHOSEN mode
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Players[0].Life; got != life-4 {
		t.Fatalf("life = %d, want %d (chosen mode -5 plus SubAbility +1), not %d", got, life-4, got)
	}
	if got := countLoseLife(e, 0); got != 1 {
		t.Fatalf("life-losing events on the caster = %d, want 1 (the chosen mode)", got)
	}
	if !hasEventKind(e, events.ModeChosen) {
		t.Fatal("no ModeChosen event recorded the modal pick")
	}
	replayCheck(t, e, cfg)
}
