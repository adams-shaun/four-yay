package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The TargetsOffered marker's two shapes, both end to end on real corpus
// cards. A placement-offered Min-0 target the chooser elects ZERO of leaves
// the stack object's Targets empty -- identical, to effects, to a targeting
// that was never offered -- so effChangeZone's mid-resolution ask
// (changeZoneChosenTargets) used to re-pose the same question at resolution,
// and a bot's KChoose option-0 default would have OVERRIDDEN the decline.
// rules/stack.go's resolveTop marks Ctx.TargetsOffered from the SA the
// placement ask actually covered: the outer SA for a non-modal trigger
// (Wreck Remover), the first target-bearing chosen mode's sub for a modal
// one (Kami of Restless Shadows' RaiseScoundrel -- the shape commit
// 6b737baf's outer-SA read missed).

// targetsOfferedDeck seats seat 0 a deck whose opening hand starts with
// toGrave then protagonist, fills with basics, and returns the engine parked
// at seat 0's Main 1 with the toss forced to seat 0 (seatZeroStart).
func targetsOfferedDeck(t *testing.T, reg *cards.Registry, protagonist, toGrave string) *Engine {
	t.Helper()
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	forest := searchCorpusCard(t, reg, "Forest")
	deck := []*cards.Card{
		searchCorpusCard(t, reg, toGrave),
		searchCorpusCard(t, reg, protagonist),
	}
	for len(deck) < 40 {
		deck = append(deck, forest, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = forest
	}
	cfg := seatZeroStart(Config{Seed: 9317, Names: []string{"protagonist", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e
}

// drivePastResolution passes every priority decision and answers any
// cleanup-step discard with the first offered card, until the engine reaches
// turn beyond wantTurn or the game ends. Any OTHER non-priority decision is
// a failure -- the shape this file exists to prove absent (a re-posed
// targeting ask) would surface exactly there.
func drivePastResolution(t *testing.T, e *Engine, wantTurn int32) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Turn > wantTurn {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before turn %d", wantTurn+1)
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch {
		case d.Kind == decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
		case d.Kind == decision.KChoose && d.ResumeKind == "" && strings.HasPrefix(d.Prompt, "turn "):
			// A cleanup-step discard down to the hand-size limit (CR 514.1)
			// crossed on the way: answer it with the first offered card.
			submitChoices(t, e, 0)
		default:
			t.Fatalf("unexpected non-priority decision after the elected-zero targeting: kind %v prompt %q resume %q options %+v",
				d.Kind, d.Prompt, d.ResumeKind, d.Options)
		}
	}
	t.Fatalf("never reached turn %d", wantTurn+1)
}

// TestWreckRemoverElectedZeroIsNotReasked is the NON-MODAL shape: the ETB
// trigger's Execute IS the targeting SA (DB$ ChangeZone | ValidTgts$ Card |
// TargetMin$ 0 | TargetMax$ 1), so pushTrigger's placement ask offers it
// directly. Electing zero must leave the graveyard card where it is, run the
// chain's DBGainLife tail anyway, and pose no second "Select target card in
// a graveyard to exile" ask at resolution.
func TestWreckRemoverElectedZeroIsNotReasked(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := targetsOfferedDeck(t, reg, "Wreck Remover", "Divine Favor")
	grave := searchMoveByName(t, e, "Divine Favor", state.ZGraveyard)
	searchMoveByName(t, e, "Wreck Remover", state.ZBattlefield)

	d := passUntilAsk(t, e)
	if d == nil || d.Kind != decision.KTarget || d.Min != 0 || d.Max != 1 {
		t.Fatalf("placement ask = %+v, want the Min 0 / Max 1 KTarget", d)
	}
	graveIdx := -1
	for _, o := range d.Options {
		if o.Obj == grave {
			graveIdx = o.Index
		}
	}
	if graveIdx < 0 {
		t.Fatalf("the graveyard card was not offered: %+v", d.Options)
	}
	// Elect ZERO of the up-to-one target.
	submitChoices(t, e)

	// No re-posed ask anywhere on the way to the opponent's turn; the chain
	// still ran (the GainLife SubAbility$ is not gated on a target).
	lifeBefore := e.G.Players[0].Life
	drivePastResolution(t, e, 1)
	if got := e.G.Obj(grave); got == nil || got.Zone != state.ZGraveyard {
		t.Fatalf("the elected-zero exile still moved the graveyard card (obj %+v)", got)
	}
	if e.G.Players[0].Life != lifeBefore+1 {
		t.Fatalf("life %d -> %d, want the DBGainLife tail to have run (+1)",
			lifeBefore, e.G.Players[0].Life)
	}
}

// TestKamiOfRestlessShadowsModalElectedZeroIsNotReasked is the MODAL shape
// the outer-SA read missed: the ETB trigger is a Charm (Choices$
// RaiseScoundrel,HauntedHarvest), the placement asks the mode FIRST, and
// handleModes' placement branch then asks the CHOSEN MODE's sub -- whose
// ValidTgts$ (Creature.Ninja+YouOwn,Creature.Rogue+YouOwn, Min 0 / Max 1)
// the outer Charm SA never carries. Electing zero of the mode's target must
// not re-pose the identical question when the answered Charm resolves.
func TestKamiOfRestlessShadowsModalElectedZeroIsNotReasked(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := targetsOfferedDeck(t, reg, "Kami of Restless Shadows", "Thieves' Guild Enforcer")
	grave := searchMoveByName(t, e, "Thieves' Guild Enforcer", state.ZGraveyard)
	searchMoveByName(t, e, "Kami of Restless Shadows", state.ZBattlefield)

	// CR 603.3c placement: the mode ask first.
	dm := passUntilAsk(t, e)
	if dm == nil || dm.Kind != decision.KModes {
		t.Fatalf("placement ask = %+v, want the Charm's KModes", dm)
	}
	submitChoices(t, e, 0) // RaiseScoundrel, the first choice

	// Then the chosen mode's targeting.
	dt := passUntilAsk(t, e)
	if dt == nil || dt.Kind != decision.KTarget || dt.Min != 0 || dt.Max != 1 {
		t.Fatalf("mode target ask = %+v, want the Min 0 / Max 1 KTarget", dt)
	}
	graveIdx := -1
	for _, o := range dt.Options {
		if o.Obj == grave {
			graveIdx = o.Index
		}
	}
	if graveIdx < 0 {
		t.Fatalf("the graveyard Rogue was not offered: %+v", dt.Options)
	}
	// Elect ZERO of the up-to-one Ninja or Rogue.
	submitChoices(t, e)

	// No re-posed "Select up to one target Ninja or Rogue ..." anywhere on
	// the way to the opponent's turn.
	drivePastResolution(t, e, 1)
	if got := e.G.Obj(grave); got == nil || got.Zone != state.ZGraveyard {
		t.Fatalf("the elected-zero return still moved the graveyard Rogue (obj %+v)", got)
	}
}
