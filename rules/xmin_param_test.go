package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task cost:xmin-param: the ability-level `XMin$ N` parameter is a floor on
// the announced X of the activated ability's own cost, beside the already
// modelled cost-embedded XMin<N>/X1+ carriers. Before this ticket xAsk read
// only pc.cost.XMin and pc.suspendMinX, so an SA with a bare announced-X
// cost part and XMin$ 1 offered X = 0. The parameter folds by MAXIMUM with
// the other two floors, resolved through pcAbility (the ability actually
// being activated, granted or not).

const xMinParamRelicSrc = "Name:XMin Param Relic\nManaCost:0\nTypes:Artifact\n" +
	"A:AB$ GainLife | Cost$ ExileFromGrave<X/Creature> | XMin$ 1 | Defined$ You | LifeAmount$ 2 | SpellDescription$ x.\nOracle:x\n"
const xMinParamMaxSrc = "Name:XMin Param Max\nManaCost:0\nTypes:Artifact\n" +
	"A:AB$ GainLife | Cost$ XMin1 RemoveAnyCounter<X/P1P1/Creature> | XMin$ 2 | Defined$ You | LifeAmount$ 2 | SpellDescription$ x.\nOracle:x\n"
const xMinParamCostHiSrc = "Name:XMin Param CostHi\nManaCost:0\nTypes:Artifact\n" +
	"A:AB$ GainLife | Cost$ XMin3 RemoveAnyCounter<X/P1P1/Creature> | XMin$ 1 | Defined$ You | LifeAmount$ 2 | SpellDescription$ x.\nOracle:x\n"
const xMinParamBearSrc = "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// xMinParamAbility asserts the fixture's activation really carries the
// wanted XMin$ parameter and the wanted cost XMin floor, so the ask
// assertions below exercise the parameter and not a vacuous fixture.
func xMinParamAbility(t *testing.T, e *Engine, srcID state.ObjID, wantParam string, wantCostXMin int32) {
	t.Helper()
	o := e.G.Obj(srcID)
	if o == nil || o.Face() == nil || len(o.Face().Abilities) != 1 {
		t.Fatalf("fixture source = %+v, want one ability on its face", o)
	}
	ab := o.Face().Abilities[0]
	if ab.Params["XMin"] != wantParam {
		t.Fatalf("fixture ability XMin param = %q, want %q", ab.Params["XMin"], wantParam)
	}
	if got := ParseCost(ab.Params["Cost"]).XMin; got != wantCostXMin {
		t.Fatalf("fixture cost XMin = %d, want %d (the cost-embedded carrier)", got, wantCostXMin)
	}
}

// TestXMinParamFloorsAnnouncedExileX is the regression: an activated ability
// with a bare ExileFromGrave<X/Creature> cost and XMin$ 1 must not offer
// X = 0 -- with one exilable creature the ONLY offer is X = 1, and paying it
// exiles the creature and resolves the ability.
func TestXMinParamFloorsAnnouncedExileX(t *testing.T) {
	e, cfg, _ := newFixtureDeck(t, 91, xMinParamRelicSrc, xMinParamBearSrc)
	relicID := moveSeeded(t, e, 0, xMinParamRelicSrc, state.ZBattlefield)
	bearID := moveSeeded(t, e, 0, xMinParamBearSrc, state.ZGraveyard)
	e.pending = nil
	e.Advance()
	xMinParamAbility(t, e, relicID, "1", 0)
	if bear := e.G.Obj(bearID); bear == nil || bear.Zone != state.ZGraveyard || bear.Face() == nil ||
		len(bear.Face().Types) == 0 || bear.Face().Types[0] != "Creature" {
		t.Fatalf("fixture Bear not an exilable creature in the graveyard: %+v", bear)
	}
	startLife := e.G.Players[0].Life
	opt := abilityOption(t, e, relicID, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Prompt != "Choose a value for X" {
		t.Fatalf("X ask = %+v, want the announcement decision", d)
	}
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount == 0 {
			t.Fatalf("X options = %+v, want no X = 0: the ability's XMin$ 1 floors the announcement", d.Options)
		}
	}
	if len(d.Options) != 1 || d.Options[0].Kind != "x" || d.Options[0].Amount != 1 {
		t.Fatalf("X options = %+v, want exactly X = 1 (floor 1, one exilable creature caps the range)", d.Options)
	}
	chooseX(t, e, 1)
	// The exile payment then asks over the graveyard: the single option is
	// the Bear (precondition that the announcement actually settles).
	ex := e.Pending()
	if ex == nil || ex.Kind != decision.KChoose || len(ex.Options) != 1 || ex.Options[0].Obj != bearID {
		t.Fatalf("exile-cost ask = %+v, want one option naming the graveyard Bear", ex)
	}
	submitChoices(t, e, ex.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(bearID).Zone; got != state.ZExile {
		t.Fatalf("Bear zone after paying = %s, want exile", got)
	}
	if got := e.G.Players[0].Life; got != startLife+2 {
		t.Fatalf("life after resolving = %d, want %d (the ability resolved at the announced X)", got, startLife+2)
	}
	replayCheck(t, e, cfg)
}

// xMinParamCounters puts the fixture's second card (the Bear) on the
// battlefield with withCounters +1/+1 counters, after src itself was already
// moved onto the battlefield, and re-asks so the options reflect the board.
func xMinParamCounters(t *testing.T, e *Engine, bearSrc string, withCounters int) state.ObjID {
	t.Helper()
	bearID := moveSeeded(t, e, 0, bearSrc, state.ZBattlefield)
	if withCounters > 0 {
		e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "P1P1", Amount: int32(withCounters)})
	}
	e.pending = nil
	e.Advance()
	if bear := e.G.Obj(bearID); bear == nil || bear.Zone != state.ZBattlefield || bear.Counter("P1P1") != int32(withCounters) {
		t.Fatalf("setup: Bear = %+v, want on the battlefield with %d +1/+1 counters", bear, withCounters)
	}
	if e.G.Obj(bearID) == nil {
		t.Fatal("Bear id invalid after placement")
	}
	return bearID
}

// TestXMinParamTakesMaxWithCostFloor pins the fold direction: the ability's
// XMin$ 2 raises a cost whose own XMin1 carrier floors at 1, so the offered
// range starts at 2, not at the cost's floor and never at 0 or 1.
func TestXMinParamTakesMaxWithCostFloor(t *testing.T) {
	e, _, _ := newFixtureDeck(t, 92, xMinParamMaxSrc, xMinParamBearSrc)
	relicID := moveSeeded(t, e, 0, xMinParamMaxSrc, state.ZBattlefield)
	xMinParamAbility(t, e, relicID, "2", 1)
	bearID := xMinParamCounters(t, e, xMinParamBearSrc, 3)
	startLife := e.G.Players[0].Life
	opt := abilityOption(t, e, relicID, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Prompt != "Choose a value for X" {
		t.Fatalf("X ask = %+v, want the announcement decision", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("X options = %+v, want exactly X=2 and X=3 (max(param 2, cost 1) starts the range)", d.Options)
	}
	for _, o := range d.Options {
		if o.Kind != "x" || (o.Amount != 2 && o.Amount != 3) {
			t.Fatalf("X options = %+v, want only X=2 and X=3 -- below the XMin$ 2 floor", d.Options)
		}
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(bearID).Counter("P1P1"); got != 1 {
		t.Fatalf("+1/+1 counters after paying X=2 = %d, want 1 (3 minus the announced 2)", got)
	}
	if got := e.G.Players[0].Life; got != startLife+2 {
		t.Fatalf("life after resolving = %d, want %d (the ability resolved at the announced X)", got, startLife+2)
	}
}

// TestXMinParamDoesNotLowerCostFloor is the other fold direction: a cost
// whose own XMin3 token floors ABOVE the ability's XMin$ 1 keeps the higher
// cost floor -- the parameter never lowers an existing bound.
func TestXMinParamDoesNotLowerCostFloor(t *testing.T) {
	e, _, _ := newFixtureDeck(t, 93, xMinParamCostHiSrc, xMinParamBearSrc)
	relicID := moveSeeded(t, e, 0, xMinParamCostHiSrc, state.ZBattlefield)
	xMinParamAbility(t, e, relicID, "1", 3)
	xMinParamCounters(t, e, xMinParamBearSrc, 4)
	opt := abilityOption(t, e, relicID, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Prompt != "Choose a value for X" {
		t.Fatalf("X ask = %+v, want the announcement decision", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("X options = %+v, want exactly X=3 and X=4 (the cost's XMin3 floor stands)", d.Options)
	}
	for _, o := range d.Options {
		if o.Kind != "x" || (o.Amount != 3 && o.Amount != 4) {
			t.Fatalf("X options = %+v, want only X=3 and X=4 -- the XMin$ 1 parameter must not lower the cost's floor", d.Options)
		}
	}
}

// TestCorpseweftXMinParamOffersNoZeroExileX pins the real corpus carrier
// (script text stays in the gitignored .cards/ tree): Corpseweft's
// "{1}{B}, Exile one or more creature cards from your graveyard" is
// `Cost$ 1 B ExileFromGrave<X/Creature> | XMin$ 1`, and with exactly one
// creature in the graveyard the X ask offers ONLY X = 1.
func TestCorpseweftXMinParamOffersNoZeroExileX(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cw := mustCorpusCard(t, reg, "Corpseweft")
	// Precondition: the corpus card's activation carries XMin$ 1 on the same
	// ability that carries the announced exile cost.
	found := false
	for _, ab := range cw.Faces[0].Abilities {
		if ab.API == "Token" && ab.Params["XMin"] == "1" {
			found = true
		}
	}
	if !found {
		t.Fatal("corpus Corpseweft lost its XMin$ 1 Token ability -- the pin below is vacuous")
	}
	// The corpus token registry is loaded (the resolving ability creates a
	// real Zombie Horror token from the script's TokenScript$).
	bearCard := card(t, xMinParamBearSrc)
	cfg := seatZeroStart(Config{Seed: 94, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append([]*cards.Card{cw, bearCard}, mountainDeck(t, 38)...),
			mountainDeck(t, 40),
		}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	cwID := findByName(e, "Corpseweft", 0)
	if cwID == 0 {
		t.Fatal("corpus Corpseweft not in seat 0's zones")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: cwID, From: e.G.Obj(cwID).Zone, To: state.ZBattlefield})
	var bearID state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Bear" {
				bearID = id
			}
		}
	}
	if bearID == 0 {
		t.Fatal("fixture Bear not in seat 0's hand or library")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: bearID, From: e.G.Obj(bearID).Zone, To: state.ZGraveyard})
	e.pending = nil
	e.Advance()
	if bear := e.G.Obj(bearID); bear == nil || bear.Zone != state.ZGraveyard {
		t.Fatalf("setup: Bear = %+v, want in the graveyard", bear)
	}
	if cw := e.G.Obj(cwID); cw == nil || cw.Zone != state.ZBattlefield {
		t.Fatalf("setup: Corpseweft = %+v, want on the battlefield", cw)
	}
	addMana(t, e, 0, "CB")
	opt := abilityOption(t, e, cwID, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Prompt != "Choose a value for X" {
		t.Fatalf("X ask = %+v, want the announcement decision", d)
	}
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount == 0 {
			t.Fatalf("X options = %+v, want no X = 0: Corpseweft's XMin$ 1 floors the announcement", d.Options)
		}
	}
	if len(d.Options) != 1 || d.Options[0].Amount != 1 {
		t.Fatalf("X options = %+v, want exactly X = 1", d.Options)
	}
	chooseX(t, e, 1)
	ex := e.Pending()
	if ex == nil || ex.Kind != decision.KChoose || len(ex.Options) == 0 || ex.Options[0].Obj != bearID {
		t.Fatalf("exile-cost ask = %+v, want the graveyard Bear offered", ex)
	}
	submitChoices(t, e, ex.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(bearID).Zone; got != state.ZExile {
		t.Fatalf("Bear zone after paying = %s, want exile", got)
	}
	// The ability resolved at the announced X: the mint event for the script
	// named token ran. (The minted token's P/T is the ExiledCards$Amount/Twice
	// SVar's business, a separate effect-resolution concern this ticket does
	// not touch -- see the report.)
	minted := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.TokenCreate && ev.Text == "b_x_x_zombie_horror" {
			minted = true
		}
	}
	if !minted {
		t.Fatal("no Zombie Horror token mint event: the ability never resolved")
	}
	replayCheck(t, e, cfg)
}

// TestXMinParamOfferGateWithholdsUnpayableMinimum is the offer regression:
// one generic mana pays X=1 but not the ability's XMin$ 2 floor, so no
// activation is offered. With two generic the offer appears, X starts at 2,
// and payment resolves the ability at that value.
func TestXMinParamOfferGateWithholdsUnpayableMinimum(t *testing.T) {
	e, cfg, _ := newFixtureDeck(t, 96, xMinOfferGateSrc, xMinOfferGateBearSrc)
	relicID := moveSeeded(t, e, 0, xMinOfferGateSrc, state.ZBattlefield)
	e.pending = nil
	e.Advance()
	xMinOfferGatePrecondition(t, e, relicID, "2", 0, 1)

	addMana(t, e, 0, "C")
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority decision after funding = %+v, want a live priority ask", d)
	}
	if _, ok := findAbilityOption(e, relicID, 0); ok {
		t.Fatal("activation offered with one generic mana: XMin$ 2 needs two, the offer must be withheld")
	}
	addMana(t, e, 0, "C")
	opt, ok := findAbilityOption(e, relicID, 0)
	if !ok {
		t.Fatalf("activation not offered with two generic mana: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Prompt != "Choose a value for X" {
		t.Fatalf("X ask = %+v, want the announcement decision", d)
	}
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount < 2 {
			t.Fatalf("X options = %+v, want none below 2", d.Options)
		}
	}
	if len(d.Options) == 0 || d.Options[0].Kind != "x" || d.Options[0].Amount != 2 {
		t.Fatalf("X options = %+v, want range to start at 2", d.Options)
	}
	startLife := e.G.Players[0].Life
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != startLife+2 {
		t.Fatalf("life after paying X=2 = %d, want %d", got, startLife+2)
	}
	replayCheck(t, e, cfg)
}
