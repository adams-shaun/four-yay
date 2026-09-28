package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestRemoveAnyCounterCostOffersCounterKindChoice pins the wildcard cost on a
// Fain-shaped activation: both the permanent and the counter kind are legal
// choices, and the selected kind is the one removed by payment.
func TestRemoveAnyCounterCostOffersCounterKindChoice(t *testing.T) {
	t.Parallel()
	const fain = "Name:Fain, the Broker\nManaCost:2 B\nTypes:Legendary Creature Human Warlock\nPT:3/3\nK:Haste\n" +
		"A:AB$ Token | Cost$ T RemoveAnyCounter<1/Any/Creature> | TokenScript$ c_a_treasure_sac | SpellDescription$ Create a Treasure token.\nOracle:x\n"
	targetCard := "Name:Counter Creature\nTypes:Creature\nPT:2/2\nOracle:x\n"
	e, cfg, source := newFixtureDeck(t, 61, fain, targetCard)
	target := moveSeeded(t, e, 0, targetCard, state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: target, Counter: "P1P1", Amount: 1})
	e.emit(events.Event{Kind: events.CounterChange, Obj: target, Counter: "CHARGE", Amount: 1})
	if e.G.Obj(target).Counter("P1P1") != 1 || e.G.Obj(target).Counter("CHARGE") != 1 {
		t.Fatal("setup: target must have both counters on the battlefield")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: e.G.Obj(source).Zone, To: state.ZBattlefield})
	e.pending = nil
	e.Advance()
	if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("source setup: %+v", o)
	}
	opt := abilityOption(t, e, source, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("counter-kind decision = %+v, want two choices", d)
	}
	chosen := -1
	for _, o := range d.Options {
		if o.Obj == target && o.Counter == "CHARGE" {
			chosen = o.Index
		}
	}
	if chosen < 0 {
		t.Fatalf("CHARGE choice missing: %+v", d.Options)
	}
	submitChoices(t, e, chosen)
	if got := e.G.Obj(target).Counter("CHARGE"); got != 0 {
		t.Fatalf("selected CHARGE counter = %d, want 0", got)
	}
	if got := e.G.Obj(target).Counter("P1P1"); got != 1 {
		t.Fatalf("unselected +1/+1 counter = %d, want 1", got)
	}
	replayCheck(t, e, cfg)
}

func TestParseRemoveAnyCounterCost(t *testing.T) {
	t.Parallel()
	c := ParseCost("T RemoveAnyCounter<1/Any/Creature>")
	if len(c.Unknown) != 0 || len(c.SubCounter) != 1 {
		t.Fatalf("parsed cost = %+v, want one modelled counter-removal part", c)
	}
	p := c.SubCounter[0]
	if p.N != 1 || p.Spec != "Any" || p.Target != "Creature" {
		t.Fatalf("counter-removal part = %+v", p)
	}
}

// fainCorpusEngine deals seat 0 a corpus deck led by Fain, the Broker and all
// Forests, with a Grizzly Bears beside it. Fain and the Bears are moved onto
// the battlefield and two counters of distinct kinds are placed on the Bears.
// It returns Fain's id and the Bears' id. No Forge script text is committed
// here: both cards come from the compiled corpus.
func fainCorpusEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	fain := searchCorpusCard(t, reg, "Fain, the Broker")
	forest := searchCorpusCard(t, reg, "Forest")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{fain, bear}
	for len(deck) < 40 {
		deck = append(deck, forest)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = forest
	}
	cfg := seatZeroStart(Config{Seed: 7711, Names: []string{"fain", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens, NameUniverse: reg.Cards})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	fainID := searchMoveByName(t, e, "Fain, the Broker", state.ZBattlefield)
	bearID := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	if e.G.Obj(fainID).Zone != state.ZBattlefield || e.G.Obj(bearID).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Fain %s Bears %s", e.G.Obj(fainID).Zone, e.G.Obj(bearID).Zone)
	}
	// Fain has no haste, so its {T} abilities need a log-derivable turn to
	// have passed: drive to seat 0's next main phase, where the untap step
	// has cleared the summoning sickness Fain entered with. Setting
	// SummonSick directly would not replay from the log.
	driveToStep(t, e, e.G.Turn+2, 0, state.StepMain1)
	if e.G.Obj(fainID).SummonSick {
		t.Fatal("precondition: Fain is still summoning-sick on its next turn")
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "P1P1", Amount: 1})
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "CHARGE", Amount: 1})
	e.pending = nil
	e.priorityRound()
	return e, cfg, fainID, bearID
}

// TestRemoveAnyCounterRealCorpusFainPays pins the wildcard cost on the REAL
// compiled corpus card Fain, the Broker: its second ability
// (`Cost$ T RemoveAnyCounter<1/Any/Creature>`, oracle "{T}, Remove a counter
// from a creature you control: Create a Treasure token.") is offered, the
// counter-kind choice is posed over the two kinds on the chosen creature, and
// payment removes the selected counter and mints the Treasure token from the
// real token registry. Because Fain comes from the corpus, a parser or
// compiler regression on the card's own Cost$ makes this test fail.
func TestRemoveAnyCounterRealCorpusFainPays(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, fainID, bearID := fainCorpusEngine(t, reg)
	if e.G.Tokens["c_a_treasure_sac"] == nil {
		t.Fatal("precondition: Treasure token script absent from the corpus token registry")
	}
	opt := abilityOptionByLabel(t, e, fainID, "Treasure")
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("counter-kind decision = %+v, want a KChoose", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("counter-kind options = %+v, want one per counter kind on the Bears", d.Options)
	}
	chosen := -1
	for _, o := range d.Options {
		if o.Obj == bearID && o.Counter == "CHARGE" {
			chosen = o.Index
		}
	}
	if chosen < 0 {
		t.Fatalf("CHARGE choice missing: %+v", d.Options)
	}
	submitChoices(t, e, chosen)
	if got := e.G.Obj(bearID).Counter("CHARGE"); got != 0 {
		t.Fatalf("selected CHARGE counter = %d, want 0", got)
	}
	if got := e.G.Obj(bearID).Counter("P1P1"); got != 1 {
		t.Fatalf("unselected +1/+1 counter = %d, want 1", got)
	}
	// The Treasure token is minted when the activated ability RESOLVES, so
	// drain the stack before asserting it.
	passUntilStackEmpty(t, e, 20)
	if !hasEvent(e, events.TokenCreate, 0) {
		t.Fatalf("no Treasure token created by the real corpus Fain activation")
	}
	replayCheck(t, e, cfg)
}

// TestRemoveAnyCounterMultiUnitSpansKinds closes the fix-round MAJOR: a
// wildcard cost of TWO counters (`RemoveAnyCounter<2/Any/Creature>`, the
// Quilled Greatwurm / Reaping Willow shape) may be paid by one creature
// carrying one +1/+1 counter and one CHARGE counter -- two counters of two
// kinds -- while the availability gate admits the creature on its total
// counter count. Each unit is chosen separately, so the activation is offered
// and settles rather than aborting as "kind no longer payable".
func TestRemoveAnyCounterMultiUnitSpansKinds(t *testing.T) {
	t.Parallel()
	const src = "Name:Two-Any\nManaCost:0\nTypes:Artifact\n" +
		"A:AB$ GainLife | Cost$ T RemoveAnyCounter<2/Any/Creature> | Defined$ You | LifeAmount$ 2 | SpellDescription$ Gain 2 life.\nOracle:x\n"
	const bear = "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, srcID := newFixtureDeck(t, 73, src, bear)
	bearID := putCreature(t, e, 0, bear)
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "P1P1", Amount: 1})
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "CHARGE", Amount: 1})
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "LORE", Amount: 1})
	if e.G.Obj(bearID).Counter("P1P1") != 1 || e.G.Obj(bearID).Counter("CHARGE") != 1 || e.G.Obj(bearID).Counter("LORE") != 1 {
		t.Fatal("setup: the creature must carry one +1/+1, one CHARGE and one LORE counter")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: srcID, From: e.G.Obj(srcID).Zone, To: state.ZBattlefield})
	e.pending = nil
	e.Advance()

	// Precondition: the total-count availability gate admits the ability even
	// though no single kind holds two counters.
	opt := abilityOption(t, e, srcID, 0)
	startLife := e.G.Players[0].Life
	submitChoices(t, e, opt.Index)

	// The two units are chosen one at a time. Three kinds are available, so
	// after the first pick two remain and a second KChoose is genuinely posed
	// (a sole remaining candidate would be recorded without an ask).
	first := e.Pending()
	if first == nil || first.Kind != decision.KChoose || len(first.Options) != 3 {
		t.Fatalf("first unit decision = %+v, want three kinds", first)
	}
	pickCHARGE := -1
	for _, o := range first.Options {
		if o.Counter == "CHARGE" {
			pickCHARGE = o.Index
		}
	}
	if pickCHARGE < 0 {
		t.Fatalf("CHARGE missing from first unit: %+v", first.Options)
	}
	submitChoices(t, e, pickCHARGE)
	second := e.Pending()
	if second == nil || second.Kind != decision.KChoose {
		t.Fatalf("second unit decision = %+v, want a KChoose for the remaining counters", second)
	}
	for _, o := range second.Options {
		if o.Counter == "CHARGE" {
			t.Fatalf("the consumed CHARGE counter re-offered: %+v", second.Options)
		}
	}
	pickLORE := -1
	for _, o := range second.Options {
		if o.Counter == "LORE" {
			pickLORE = o.Index
		}
	}
	if pickLORE < 0 {
		t.Fatalf("LORE missing from the second unit: %+v", second.Options)
	}
	submitChoices(t, e, pickLORE)
	if got := e.G.Obj(bearID).Counter("CHARGE"); got != 0 {
		t.Fatalf("CHARGE counter = %d, want 0", got)
	}
	if got := e.G.Obj(bearID).Counter("LORE"); got != 0 {
		t.Fatalf("LORE counter = %d, want 0", got)
	}
	if got := e.G.Obj(bearID).Counter("P1P1"); got != 1 {
		t.Fatalf("unselected +1/+1 counter = %d, want 1", got)
	}
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != startLife+2 {
		t.Fatalf("life after paying the two-counter cost = %d, want %d (the ability resolved)", got, startLife+2)
	}
	replayCheck(t, e, cfg)
}

// TestRemoveAnyCounterMixedFixedAndWildcardParts closes the fix-round MAJOR
// on pick indexing: a cost mixing a fixed-kind filtered SubCounter part before
// a wildcard RemoveAnyCounter part must settle the WILDCARD from the player's
// selected kind, not from a positional list that the fixed part's empty entry
// shifted. The fixed part POSES a target decision (two creatures carry +1/+1
// counters), appending its empty entry, and the wildcard then offers the other
// creature's CHARGE and P1P1 kinds; picking CHARGE proves the selected kind is
// what settles, not a positional fallback that would have removed the first
// kind in counter-list order.
func TestRemoveAnyCounterMixedFixedAndWildcardParts(t *testing.T) {
	t.Parallel()
	const src = "Name:Mixed\nManaCost:0\nTypes:Artifact\n" +
		"A:AB$ GainLife | Cost$ T SubCounter<1/P1P1/Creature> RemoveAnyCounter<1/Any/Creature> | Defined$ You | LifeAmount$ 2 | SpellDescription$ Gain 2 life.\nOracle:x\n"
	const bearA = "Name:Bear A\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	const bearB = "Name:Bear B\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, srcID := newFixtureDeck(t, 79, src, bearA, bearB)
	// Bear A carries a +1/+1 AND a CHARGE counter: it is the wildcard's later
	// candidate, and CHARGE is the second kind in its counter-list order.
	bearAID := putCreature(t, e, 0, bearA)
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearAID, Counter: "P1P1", Amount: 1})
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearAID, Counter: "CHARGE", Amount: 1})
	// Bear B carries only a +1/+1 counter: the fixed part's chosen target, so
	// the fixed part must pose an object decision.
	bearBID := putCreature(t, e, 0, bearB)
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearBID, Counter: "P1P1", Amount: 1})
	if e.G.Obj(bearAID).Counter("P1P1") != 1 || e.G.Obj(bearAID).Counter("CHARGE") != 1 || e.G.Obj(bearBID).Counter("P1P1") != 1 {
		t.Fatal("setup: counter placement failed")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: srcID, From: e.G.Obj(srcID).Zone, To: state.ZBattlefield})
	e.pending = nil
	e.Advance()
	opt := abilityOption(t, e, srcID, 0)
	submitChoices(t, e, opt.Index)

	// The fixed part's object decision is posed first: choose Bear B.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("fixed-part object decision = %+v, want a KChoose", d)
	}
	fixedPick := -1
	for _, o := range d.Options {
		if o.Kind == "subcounter" && o.Obj == bearBID && o.Counter == "" {
			fixedPick = o.Index
		}
	}
	if fixedPick < 0 {
		t.Fatalf("fixed-part option for Bear B missing: %+v", d.Options)
	}
	submitChoices(t, e, fixedPick)

	// The wildcard then asks over Bear A's two kinds (Bear B is reserved).
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("wildcard unit decision = %+v, want a KChoose over (permanent, kind)", d)
	}
	for _, o := range d.Options {
		if o.Obj == bearBID && o.Counter == "P1P1" {
			t.Fatalf("wildcard re-offered Bear B's fixed-cost counter: %+v", d.Options)
		}
	}
	picked := -1
	for _, o := range d.Options {
		if o.Obj == bearAID && o.Counter == "CHARGE" {
			picked = o.Index
		}
	}
	if picked < 0 {
		t.Fatalf("CHARGE choice on Bear A missing: %+v", d.Options)
	}
	submitChoices(t, e, picked)
	if got := e.G.Obj(bearAID).Counter("CHARGE"); got != 0 {
		t.Fatalf("wildcard selected CHARGE counter = %d, want 0 (the player's kind must settle, not a positional fallback)", got)
	}
	if got := e.G.Obj(bearAID).Counter("P1P1"); got != 1 {
		t.Fatalf("unselected +1/+1 counter on Bear A = %d, want 1", got)
	}
	if got := e.G.Obj(bearBID).Counter("P1P1"); got != 0 {
		t.Fatalf("fixed part +1/+1 counter on Bear B = %d, want 0", got)
	}
	replayCheck(t, e, cfg)
}
