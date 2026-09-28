package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func readyExchangeCard(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
		t.Fatalf("exchange source precondition: %+v", e.G.Obj(id))
	}
	// The real activation is a tap ability; give the permanent a turn before
	// asking priority so summoning sickness cannot make this test vacuous.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.pending = nil
	e.priorityRound()
}

func activateExchange(t *testing.T, e *Engine, source state.ObjID, target state.PlayerID) {
	t.Helper()
	o := e.G.Obj(source)
	if o == nil || o.Face() == nil || len(o.Face().Abilities) == 0 || o.Face().Abilities[0].API != "ExchangeLifeVariant" {
		t.Fatalf("source has no ExchangeLifeVariant ability: %+v", o)
	}
	opt := abilityOption(t, e, source, 0)
	submitChoices(t, e, opt.Index)
	if target != 0 {
		d := e.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("target decision = %+v", d)
		}
		idx := indexOfPlayerOption(d, target)
		if idx < 0 {
			t.Fatalf("target %d absent from %+v", target, d.Options)
		}
		submitChoices(t, e, idx)
	}
	passUntilStackEmpty(t, e, 40)
}

func TestTreeOfPerditionExchangesTargetLifeAndToughness(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Tree of Perdition")
	tree := searchMoveByName(t, e, "Tree of Perdition", state.ZBattlefield)
	readyExchangeCard(t, e, tree)
	// Make the target's old life and the Tree's characteristic visibly distinct.
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -16})
	before := e.Toughness(tree)
	oldLife := e.G.Players[1].Life
	if before == oldLife || before <= 0 {
		t.Fatalf("precondition values: Tree toughness %d, opponent life %d", before, oldLife)
	}
	activateExchange(t, e, tree, 1)
	if !e.G.Obj(tree).Tapped {
		t.Fatal("Tree of Perdition was not tapped by its real activation")
	}
	if got := e.G.Players[1].Life; got != before {
		t.Fatalf("opponent life %d, want old toughness %d", got, before)
	}
	if got := e.Toughness(tree); got != oldLife {
		t.Fatalf("Tree toughness %d, want old life %d", got, oldLife)
	}
	if e.Power(tree) != 0 {
		t.Fatalf("Tree power %d changed during toughness exchange", e.Power(tree))
	}
	// Mark damage against the newly lowered, real derived toughness and drive
	// the ordinary SBA boundary.
	e.emit(events.Event{Kind: events.Damage, Obj: tree, Amount: 5})
	e.checkStateBased()
	if e.G.Obj(tree).Zone != state.ZGraveyard {
		t.Fatalf("Tree zone %s, want Graveyard after marked damage and lowered toughness", e.G.Obj(tree).Zone)
	}
	replayCheck(t, e, cfg)
}

func TestTreeOfRedemptionExchangesControllerLifeAndToughness(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Tree of Redemption")
	tree := searchMoveByName(t, e, "Tree of Redemption", state.ZBattlefield)
	readyExchangeCard(t, e, tree)
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -16})
	before := e.Toughness(tree)
	oldLife := e.G.Players[0].Life
	if before == oldLife || before <= 0 {
		t.Fatalf("precondition values: Tree toughness %d, controller life %d", before, oldLife)
	}
	activateExchange(t, e, tree, 0)
	if e.G.Players[0].Life != before || e.Toughness(tree) != oldLife || e.Power(tree) != 0 {
		t.Fatalf("redemption exchange: life=%d P/T=%d/%d, want %d %d/%d", e.G.Players[0].Life, e.Power(tree), e.Toughness(tree), before, 0, oldLife)
	}
	replayCheck(t, e, cfg)
}

// exchangeVariantSettersOn is the white-box half of the replacement-boundary
// assertion: ExchangeLifeVariant installs exactly this source's layer-7b
// one-component setter only after its life event was accepted unchanged.
func exchangeVariantSettersOn(e *Engine, source state.ObjID) int {
	count := 0
	for _, ce := range e.continuous {
		if ce.Source == source && ce.Layer == state.LPT && ce.Sub == state.SubSet && ce.StaticSet &&
			(ce.SetPowerPresent || ce.SetToughnessPresent) {
			count++
		}
	}
	return count
}

// passUntilLifeReplacement drives a real activated ability through its
// priority passes but stops at the CR 616.1 choice it creates.
func passUntilLifeReplacement(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for n := 0; n < limit; n++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while waiting for life replacement (stack=%v)", e.G.Stack)
		}
		if d.Kind == decision.KReplacement {
			return d
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("pending = %+v, want priority or life replacement", d)
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
				break
			}
		}
		if pass < 0 {
			t.Fatalf("priority decision has no pass: %+v", d)
		}
		submitChoices(t, e, pass)
	}
	t.Fatalf("life replacement did not arrive in %d decisions", limit)
	return nil
}

func TestEvraExchangeFailsClosedForParkedLifeReplacement(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Evra, Halcyon Witness", "Alhammarret's Archive", "Cleric Class")
	archive := searchMoveByName(t, e, "Alhammarret's Archive", state.ZBattlefield)
	cleric := searchMoveByName(t, e, "Cleric Class", state.ZBattlefield)
	evra := searchMoveByName(t, e, "Evra, Halcyon Witness", state.ZBattlefield)
	if e.G.Obj(archive).Zone != state.ZBattlefield || e.G.Obj(cleric).Zone != state.ZBattlefield {
		t.Fatalf("replacement sources not on battlefield: archive=%s cleric=%s", e.G.Obj(archive).Zone, e.G.Obj(cleric).Zone)
	}
	readyExchangeCard(t, e, evra)
	// Evra's printed power is 4, so life 2 makes its exchange a +2 gain that
	// both real replacement effects modify in a non-commuting order.
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -18})
	oldPower, oldLife := e.Power(evra), e.G.Players[0].Life
	if oldPower != 4 || oldLife != 2 || oldPower <= oldLife {
		t.Fatalf("exchange precondition: Evra power=%d life=%d, want 4 and 2", oldPower, oldLife)
	}
	addMana(t, e, 0, "CCCC")
	opt := abilityOption(t, e, evra, 0)
	submitChoices(t, e, opt.Index)
	d := passUntilLifeReplacement(t, e, 20)
	if d.Player != 0 || len(d.Options) != 2 {
		t.Fatalf("replacement decision = %+v, want two choices for Evra controller", d)
	}
	seenArchive, seenCleric := false, false
	for _, o := range d.Options {
		seenArchive = seenArchive || o.Obj == archive
		seenCleric = seenCleric || o.Obj == cleric
	}
	if !seenArchive || !seenCleric {
		t.Fatalf("replacement options = %+v, want Archive %d and Cleric Class %d", d.Options, archive, cleric)
	}
	if got := exchangeVariantSettersOn(e, evra); got != 0 || e.Power(evra) != oldPower {
		t.Fatalf("parked exchange installed %d setters and changed Evra power to %d", got, e.Power(evra))
	}
	// The selected replacement settles the actual life change; the exchange
	// continuation then installs the matching characteristic half.
	submitChoices(t, e, d.Options[0].Index)
	if got := e.G.Players[0].Life; got <= oldLife {
		t.Fatalf("replacement answer did not settle the gain: life=%d, want >%d", got, oldLife)
	}
	if got := exchangeVariantSettersOn(e, evra); got != 1 || e.Power(evra) != oldLife {
		t.Fatalf("answered replacement installed %d setters and set Evra power to %d, want one setter to %d", got, e.Power(evra), oldLife)
	}
	// The original activation remains suspended while the engine drains the
	// resolved replacement path; this checkpoint is the replayable boundary
	// where the P/T half must still be absent.
	replayCheck(t, e, cfg)
}

func TestEvraExchangesControllerLifeAndPower(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Evra, Halcyon Witness")
	evra := searchMoveByName(t, e, "Evra, Halcyon Witness", state.ZBattlefield)
	readyExchangeCard(t, e, evra)
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 6})
	beforePower, beforeToughness := e.Power(evra), e.Toughness(evra)
	oldLife := e.G.Players[0].Life
	if beforePower == oldLife || beforePower <= 0 || beforeToughness <= 0 {
		t.Fatalf("precondition values: Evra P/T=%d/%d, life=%d", beforePower, beforeToughness, oldLife)
	}
	addMana(t, e, 0, "CCCC")
	activateExchange(t, e, evra, 0)
	if e.G.Players[0].Life != beforePower || e.Power(evra) != oldLife || e.Toughness(evra) != beforeToughness {
		t.Fatalf("Evra exchange: life=%d P/T=%d/%d, want %d %d/%d", e.G.Players[0].Life, e.Power(evra), e.Toughness(evra), beforePower, oldLife, beforeToughness)
	}
	replayCheck(t, e, cfg)
}
