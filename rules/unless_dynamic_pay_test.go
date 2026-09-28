package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// unlessSVarSA returns the first SA reachable from the face whose UnlessCost$
// parameter equals cost, so the tests below drive the real corpus parameter
// values (SVar bodies, DefinedCost suffixes) rather than hand-trimmed ones.
func unlessSVarSA(t *testing.T, c *cards.Card, cost string) *cards.SA {
	t.Helper()
	var found *cards.SA
	for _, f := range c.Faces {
		walkAllSAs(f, func(sa *cards.SA) {
			if found == nil && strings.TrimSpace(sa.Params["UnlessCost"]) == cost {
				found = sa
			}
		})
	}
	if found == nil {
		t.Fatalf("%s has no UnlessCost$ %s SA", c.Faces[0].Name, cost)
	}
	return found
}

// mvArtifactSrc is a mana-value-3 artifact for the DefinedCost_Self probe;
// mvFiveCreatureSrc a mana-value-5 creature for the Remembered/Chosen probes.
// bearSrc (rules/additional_cost_offer_test.go) is the mana-value-2 creature
// fixture the payer-side tests put in play.
const mvArtifactSrc = "Name:Juggernaut Fixture\nManaCost:3\nTypes:Artifact\nOracle:x\n"
const mvFiveCreatureSrc = "Name:Big Beast\nManaCost:3 G G\nTypes:Creature Beast\nPT:5/5\nOracle:x\n"

// TestUnlessPayAnnouncedXEndToEnd drives a real Power Sink cast end to end:
// the cast announces X=3 (CR 601.2b), the unless ask prices exactly {3}, the
// payer pays from its pool, and the targeted spell survives. Precondition
// asserted: the pay option is offered (the gate priced {3} as reachable) and
// its label names the announced amount.
func TestUnlessPayAnnouncedXEndToEnd(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Power Sink"), mustCorpusCard(t, reg, "Grizzly Bears"))
	ids := handIDsByFace(e)
	sinkID, bearID := ids["Power Sink"], ids["Grizzly Bears"]
	if sinkID == 0 || bearID == 0 {
		t.Fatalf("hand missing Power Sink or Grizzly Bears: %v", ids)
	}
	// Pool covers the X announcement (3 + the {U}) and the unless {3}.
	e.G.Players[0].Pool[state.MU] = 9
	e.G.Players[0].Pool[state.MG] = 9
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, bearID))
	submitChoices(t, e, passToCast(t, e, sinkID))
	// The cast-time X ask: announce 3.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the cast-time X ask for Power Sink, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount == 3 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no X=3 option in %+v", d.Options)
	}
	submitChoices(t, e, idx)
	// Target the bear spell.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the target ask, got %+v", d)
	}
	submitChoices(t, e, targetOptionFor(t, e, bearID))
	pay := drainUntilUnlessPay(t, e, 30)
	if pay == nil {
		t.Fatal("no unless_pay ask posed for Power Sink")
	}
	if len(pay.Options) != 2 {
		t.Fatalf("announced X=3 must expose the Pay option: %+v", pay.Options)
	}
	if !strings.Contains(pay.Options[0].Label, "{3}") {
		t.Fatalf("pay label = %q, want the announced {3}", pay.Options[0].Label)
	}
	submitChoices(t, e, pay.Options[0].Index)
	passUntilStackEmpty(t, e, 30)
	if z := e.G.Obj(bearID).Zone; z != state.ZBattlefield {
		t.Fatalf("paying the announced {3} should save the spell: bear zone = %s", z)
	}
	if z := e.G.Obj(sinkID).Zone; z != state.ZGraveyard {
		t.Fatalf("the sink itself resolves off the stack: zone = %s", z)
	}
}

// TestUnlessPayZeroAnnouncedXEndToEnd is the announced-zero half of CR
// 601.2b: Power Sink cast for X=0 leaves o.X at zero, and WITHOUT the
// announcement bit that is indistinguishable from never-announced -- the raw
// "X" token hard-declines, the ask is decline-only and the spell dies even
// though its controller legally "pays" {0}. With the bit, the unless ask
// prices {0}, the pay option is offered, and paying it (which charges
// nothing) saves the spell.
func TestUnlessPayZeroAnnouncedXEndToEnd(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Power Sink"), mustCorpusCard(t, reg, "Grizzly Bears"))
	ids := handIDsByFace(e)
	sinkID, bearID := ids["Power Sink"], ids["Grizzly Bears"]
	// A minimal pool: the announcement is {U} only (X=0), and the unless {0}
	// needs nothing.
	e.G.Players[0].Pool[state.MU] = 9
	e.G.Players[0].Pool[state.MG] = 9
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, bearID))
	submitChoices(t, e, passToCast(t, e, sinkID))
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the cast-time X ask for Power Sink, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount == 0 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no X=0 option in %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the target ask, got %+v", d)
	}
	submitChoices(t, e, targetOptionFor(t, e, bearID))
	pay := drainUntilUnlessPay(t, e, 30)
	if pay == nil {
		t.Fatal("no unless_pay ask posed for Power Sink X=0")
	}
	if len(pay.Options) != 2 {
		t.Fatalf("announced X=0 must expose the Pay ({0}) option: %+v", pay.Options)
	}
	if !strings.Contains(pay.Options[0].Label, "{0}") {
		t.Fatalf("pay label = %q, want the announced {0}", pay.Options[0].Label)
	}
	submitChoices(t, e, pay.Options[0].Index)
	passUntilStackEmpty(t, e, 30)
	if z := e.G.Obj(bearID).Zone; z != state.ZBattlefield {
		t.Fatalf("paying {0} saves the spell (CR 118.3 -- {0} is payable): bear zone = %s", z)
	}
}

// driveUntilUnlessPay passes every priority (and empty combat declarations)
// across turns until an unless_pay KModes ask is pending. Unlike
// drainUntilUnlessPay it does not require a non-empty stack: a phase trigger
// fires turns after the spell that registered it resolved.
func driveUntilUnlessPay(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit; i++ {
		if e.G.Over {
			t.Fatal("game ended while seeking the unless_pay ask")
		}
		d := e.Pending()
		if d == nil {
			continue
		}
		switch d.Kind {
		case decision.KModes:
			if d.ResumeKind == "unless_pay" {
				return d
			}
			t.Fatalf("unexpected non-unless KModes ask: %+v", d)
		case decision.KPriority:
			castFirst(t, e, "pass")
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
				t.Fatalf("submit empty combat declaration: %v", err)
			}
		default:
			t.Fatalf("unexpected decision %+v while seeking the unless_pay ask", d)
		}
	}
	return nil
}

// TestUnlessPayEnergyEndToEnd drives Electrozoa end to end: at the NEXT
// turn's main phase its upkeep trigger asks "tap Electrozoa unless you pay
// {E}" — a fixed PayEnergy<1> the offer gate prices against the payer's
// energy counters (granted by the creature's own ETB at resolution). Paying
// spends one energy counter and spares the tap; declining taps it.
func TestUnlessPayEnergyEndToEnd(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	zoa := mustCorpusCard(t, reg, "Electrozoa")
	e := handEngine(t, zoa)
	ids := handIDsByFace(e)
	zoaID := ids["Electrozoa"]
	if zoaID == 0 {
		t.Fatal("hand missing Electrozoa")
	}
	e.G.Players[0].Pool[state.MC] = 2
	e.G.Players[0].Pool[state.MU] = 2
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, zoaID))
	passUntilStackEmpty(t, e, 30)
	// Precondition: the ETB granted 2 energy — the pool the unless pay draws
	// from.
	if en := e.G.Players[0].Counter("ENERGY"); en != 2 {
		t.Fatalf("Electrozoa ETB energy = %d, want 2", en)
	}
	if o := e.G.Obj(zoaID); o.Tapped {
		t.Fatal("Electrozoa entered untapped")
	}
	pay := driveUntilUnlessPay(t, e, 400)
	if pay == nil {
		t.Fatal("no unless_pay ask from Electrozoa's main-phase trigger")
	}
	if len(pay.Options) != 2 {
		t.Fatalf("PayEnergy<1> with 2 energy must expose the Pay option: %+v", pay.Options)
	}
	submitChoices(t, e, pay.Options[0].Index)
	passUntilStackEmpty(t, e, 30)
	if en := e.G.Players[0].Counter("ENERGY"); en != 1 {
		t.Fatalf("energy after paying = %d, want 1 (the charge must actually spend)", en)
	}
	if o := e.G.Obj(zoaID); o.Tapped {
		t.Fatal("paying the energy must spare the tap")
	}
}

// TestUnlessPayEnergyDeclineTaps is the decline half of the same trigger.
func TestUnlessPayEnergyDeclineTaps(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	zoa := mustCorpusCard(t, reg, "Electrozoa")
	e := handEngine(t, zoa)
	ids := handIDsByFace(e)
	zoaID := ids["Electrozoa"]
	e.G.Players[0].Pool[state.MC] = 2
	e.G.Players[0].Pool[state.MU] = 2
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, zoaID))
	passUntilStackEmpty(t, e, 30)
	if en := e.G.Players[0].Counter("ENERGY"); en != 2 {
		t.Fatalf("Electrozoa ETB energy = %d, want 2", en)
	}
	pay := driveUntilUnlessPay(t, e, 400)
	if pay == nil {
		t.Fatal("no unless_pay ask from Electrozoa's main-phase trigger")
	}
	if len(pay.Options) != 2 {
		t.Fatalf("decline half must also see the two-option ask: %+v", pay.Options)
	}
	submitChoices(t, e, pay.Options[1].Index) // decline
	passUntilStackEmpty(t, e, 30)
	if en := e.G.Players[0].Counter("ENERGY"); en != 2 {
		t.Fatalf("declining must not spend energy: %d", en)
	}
	if o := e.G.Obj(zoaID); !o.Tapped {
		t.Fatal("declining the pay must tap Electrozoa")
	}
}

// TestUnlessPayReturnNonLairLandEndToEnd drives Darigaaz's Caldera's ETB
// trigger end to end: "sacrifice it unless you return a non-Lair land you
// control to its owner's hand". The unless cost is a Return component, so
// the payer's pay election continues into the unless-payment continuation,
// which records the pick and returns the chosen land to its OWNER's hand.
// Precondition asserted: a Mountain is on the battlefield (a reachable
// candidate); without it the gate declines and the caldera dies instead.
func TestUnlessPayReturnNonLairLandEndToEnd(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	caldera := mustCorpusCard(t, reg, "Darigaaz's Caldera")
	e := handEngine(t, caldera)
	ids := handIDsByFace(e)
	calderaID := ids["Darigaaz's Caldera"]
	mtnID := onBoard(t, e, 0, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	e.askPriority(0)
	// Play the caldera as the turn's land; its ETB trigger fires.
	d := e.Pending()
	land := -1
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == calderaID {
			land = o.Index
		}
	}
	if land < 0 {
		t.Fatalf("caldera not offered as a land play: %+v", d.Options)
	}
	submitChoices(t, e, land)
	pay := drainUntilUnlessPay(t, e, 30)
	if pay == nil {
		t.Fatal("no unless_pay ask from the caldera's ETB trigger")
	}
	if len(pay.Options) != 2 {
		t.Fatalf("Return cost with one eligible land must expose the Pay option: %+v", pay.Options)
	}
	submitChoices(t, e, pay.Options[0].Index)
	passUntilStackEmpty(t, e, 30)
	if z := e.G.Obj(mtnID).Zone; z != state.ZHand {
		t.Fatalf("the returned land's zone = %s, want Hand (owner's hand)", z)
	}
	if z := e.G.Obj(calderaID).Zone; z != state.ZBattlefield {
		t.Fatalf("paying the return spares the caldera: zone = %s", z)
	}
	if !hasEventKind(e, events.MoveZone) {
		t.Fatal("no MoveZone event recorded the return-to-hand payment")
	}
}

// TestUnlessPayReturnNoCandidatesDeclines is the fail-closed half: with NO
// non-Lair land on the battlefield the Return cost is unreachable, so the
// ask is decline-only and the caldera is sacrificed.
func TestUnlessPayReturnNoCandidatesDeclines(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	caldera := mustCorpusCard(t, reg, "Darigaaz's Caldera")
	e := handEngine(t, caldera)
	ids := handIDsByFace(e)
	calderaID := ids["Darigaaz's Caldera"]
	e.askPriority(0)
	d := e.Pending()
	land := -1
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == calderaID {
			land = o.Index
		}
	}
	if land < 0 {
		t.Fatalf("caldera not offered as a land play: %+v", d.Options)
	}
	submitChoices(t, e, land)
	pay := drainUntilUnlessPay(t, e, 30)
	if pay == nil {
		t.Fatal("no unless_pay ask from the caldera's ETB trigger")
	}
	if len(pay.Options) != 1 {
		t.Fatalf("Return cost with no candidates must be decline-only: %+v", pay.Options)
	}
	submitChoices(t, e, pay.Options[0].Index)
	passUntilStackEmpty(t, e, 30)
	if z := e.G.Obj(calderaID).Zone; z != state.ZGraveyard {
		t.Fatalf("an unreachable Return cost declines and the caldera is sacrificed: zone = %s", z)
	}
}

// TestUnlessPayLifeTotalHalfUpEndToEnd drives Temporal Extortion's cast
// trigger end to end: "any player may pay half their life, rounded up. If a
// player does, counter CARDNAME." The unless amount folds against the
// PAYER's own life total at pay time (8 -> 4), the charge actually moves
// life, and the switched counter runs (no extra turn). The decline branch
// resolves the spell and takes the extra turn.
func TestUnlessPayLifeTotalHalfUpEndToEnd(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		choice     int // 0 = pay, 1 = decline
		wantLife   int32
		wantTurnEv bool
	}{
		{"pay half of 8", 0, 4, false},
		{"decline", 1, 8, true},
		{"pay rounds up (5 -> 3)", 0, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := testutil.CorpusRegistry(t)
			extortion := mustCorpusCard(t, reg, "Temporal Extortion")
			e := handEngine(t, extortion)
			ids := handIDsByFace(e)
			extortionID := ids["Temporal Extortion"]
			if extortionID == 0 {
				t.Fatal("hand missing Temporal Extortion")
			}
			e.G.Players[0].Life = 8
			e.G.Players[1].Life = 8
			if tc.name == "pay rounds up (5 -> 3)" {
				e.G.Players[0].Life = 5
			}
			e.G.Players[0].Pool[state.MB] = 4
			e.askPriority(0)
			submitChoices(t, e, passToCast(t, e, extortionID))
			pay := drainUntilUnlessPay(t, e, 30)
			if pay == nil {
				t.Fatal("no unless_pay ask from Temporal Extortion's cast trigger")
			}
			if pay.Player != 0 {
				t.Fatalf("UnlessPayer$ Player asks seat %d first, want seat 0", pay.Player)
			}
			if len(pay.Options) != 2 {
				t.Fatalf("LifeTotalHalfUp with a nonzero life total must expose the Pay option: %+v", pay.Options)
			}
			submitChoices(t, e, pay.Options[tc.choice].Index)
			if tc.choice == 1 {
				// Seat 0's decline moves the ask to the next payer.
				pay2 := drainUntilUnlessPay(t, e, 30)
				if pay2 == nil {
					t.Fatal("no second unless_pay ask after seat 0 declined")
				}
				submitChoices(t, e, pay2.Options[1].Index)
			}
			passUntilStackEmpty(t, e, 30)
			if got := e.G.Players[0].Life; got != tc.wantLife {
				t.Fatalf("seat 0 life after = %d, want %d", got, tc.wantLife)
			}
			if hasEventKind(e, events.ExtraTurn) != tc.wantTurnEv {
				t.Fatalf("ExtraTurn event = %v, want %v (paying counters the spell; declining resolves it)", !tc.wantTurnEv, tc.wantTurnEv)
			}
		})
	}
}

// TestUnlessCostResolvedDefinedCostShapes pins the DefinedCost token fold on
// the real corpus cards: the SA's own defined card's mana value (Disruption
// Aura's "pay its mana cost"), the remembered card's value with the Minus
// modifier (Flash), the chosen card's value (Tariff), and Ice Cave's
// DefinedSACost — the triggering spell's whole mana cost string, colours
// included, which the strict parser must then price.
func TestUnlessCostResolvedDefinedCostShapes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	// Disruption Aura: the granted trigger's SOURCE is the enchanted
	// artifact, so DefinedCost_Self is the artifact's mana value.
	aura := mustCorpusCard(t, reg, "Disruption Aura")
	e := handEngine(t, aura)
	artID := onBoard(t, e, 0, mvArtifactSrc)
	if got := effects.UnlessCostResolved(e, &effects.Ctx{Source: artID, Controller: 0}, unlessSVarSA(t, aura, "DefinedCost_Self")); got != "{3}" {
		t.Fatalf("Disruption Aura DefinedCost_Self = %q, want {3}", got)
	}

	// Flash: the remembered card's mana value minus 2 (5 -> 3).
	flash := mustCorpusCard(t, reg, "Flash")
	e = handEngine(t, flash)
	flashID := onBoard(t, e, 0, "Name:Flash\nManaCost:2 W W\nTypes:Instant\nOracle:x\n")
	beastID := onBoard(t, e, 0, mvFiveCreatureSrc)
	ctx := &effects.Ctx{Source: flashID, Controller: 0, Remembered: []state.Target{{Obj: beastID}}}
	if got := effects.UnlessCostResolved(e, ctx, unlessSVarSA(t, flash, "DefinedCost_Remembered_Minus2")); got != "{3}" {
		t.Fatalf("Flash DefinedCost_Remembered_Minus2 = %q, want {3}", got)
	}

	// Tariff: the chosen (greatest-CMC) creature's mana value.
	tariff := mustCorpusCard(t, reg, "Tariff")
	e = handEngine(t, tariff)
	tariffID := onBoard(t, e, 0, "Name:Tariff\nManaCost:1 W\nTypes:Sorcery\nOracle:x\n")
	chosenID := onBoard(t, e, 0, mvFiveCreatureSrc)
	ctx = &effects.Ctx{Source: tariffID, Controller: 0, Chosen: []state.Target{{Obj: chosenID}}, ChosenValid: true}
	if got := effects.UnlessCostResolved(e, ctx, unlessSVarSA(t, tariff, "DefinedCost_ChosenCard")); got != "{5}" {
		t.Fatalf("Tariff DefinedCost_ChosenCard = %q, want {5}", got)
	}

	// Ice Cave: the triggering spell's whole mana cost, colours included —
	// the strict parser must price it as generic + coloured pips.
	ice := mustCorpusCard(t, reg, "Ice Cave")
	e = handEngine(t, ice)
	iceID := onBoard(t, e, 0, "Name:Ice Cave\nManaCost:3 U U\nTypes:Enchantment\nOracle:x\n")
	boltSpell := onBoard(t, e, 0, "Name:Lightning Bolt\nManaCost:R\nTypes:Instant\nOracle:x\n")
	ctx = &effects.Ctx{Source: iceID, Controller: 0, Remembered: []state.Target{{Obj: boltSpell}}}
	got := effects.UnlessCostResolved(e, ctx, unlessSVarSA(t, ice, "DefinedSACost_TriggeredSpellAbility"))
	if got != "R" {
		t.Fatalf("Ice Cave DefinedSACost = %q, want the triggering spell's mana cost R", got)
	}
	parsed, ok := ParseUnlessCost(got)
	if !ok {
		t.Fatalf("DefinedSACost string %q is not priceable by the strict parser", got)
	}
	if parsed.Colored.Total() != 1 || parsed.Generic != 0 {
		t.Fatalf("parsed DefinedSACost = %+v, want one coloured pip", parsed)
	}
}

// TestUnlessCostResolvedDynamicSVarChains pins the SVar-chain fold on the
// real corpus cards: Rune Snag's Z (Number$2/Plus.Y against a graveyard
// count) and Essence Vortex's PayLife<X> (the targeted creature's
// toughness), both through ctx-bound SVar bodies. The unbound-X half is the
// fail-closed control: an UnlessCost$ X with no announcement stays raw and
// the strict parser rejects it.
func TestUnlessCostResolvedDynamicSVarChains(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	// Rune Snag: one copy in each graveyard -> Y = 2 * 2 = 4 -> Z = 2 + 4 = 6.
	snag := mustCorpusCard(t, reg, "Rune Snag")
	e := handEngine(t, snag)
	for p := state.PlayerID(0); p < 2; p++ {
		o := e.G.AddObject(snag, p)
		o.Zone = state.ZGraveyard
		e.G.SetZone(state.ZGraveyard, p, append(e.G.Zone(state.ZGraveyard, p), o.ID))
	}
	e.G.Clock++
	snagOnStack := e.G.Obj(e.G.Zone(state.ZGraveyard, 0)[0])
	ctx := &effects.Ctx{Source: snagOnStack.ID, Controller: 0, SVars: snag.Faces[0].SVars}
	if got := effects.UnlessCostResolved(e, ctx, unlessSVarSA(t, snag, "Z")); got != "{6}" {
		t.Fatalf("Rune Snag Z with one snag in each graveyard = %q, want {6}", got)
	}

	// Essence Vortex: the unless life is the targeted creature's toughness.
	vortex := mustCorpusCard(t, reg, "Essence Vortex")
	e = handEngine(t, vortex)
	bearID := onBoard(t, e, 0, bearSrc)
	ctx = &effects.Ctx{Source: e.G.Zone(state.ZHand, 0)[0], Controller: 0,
		SVars: vortex.Faces[0].SVars, Targets: []state.Target{{Obj: bearID}}}
	got := effects.UnlessCostResolved(e, ctx, unlessSVarSA(t, vortex, "PayLife<X>"))
	if got != "PayLife<2>" {
		t.Fatalf("Essence Vortex PayLife<X> = %q, want PayLife<2> (the bear's toughness)", got)
	}
	parsed, ok := ParseUnlessCost(got)
	if !ok || parsed.Life != 2 {
		t.Fatalf("parsed %q = %+v ok=%v, want a fixed life charge of 2", got, parsed, ok)
	}

	// Fail-closed control: an unannounced X stays raw and unpriceable. The
	// carrier is a synthetic Counter with no SVar:X -- Power Sink's own
	// Count$xPaid body legitimately binds zero, the fold above it -- so the
	// control isolates the announcement channel itself.
	fake := card(t, "Name:Bare Counter\nManaCost:X U\nTypes:Instant\nA:SP$ Counter | UnlessCost$ X | ValidTgts$ Card\nOracle:x\n")
	fakeSA := unlessSVarSA(t, fake, "X")
	raw := effects.UnlessCostResolved(e, &effects.Ctx{}, fakeSA)
	if raw != "X" {
		t.Fatalf("unannounced X resolved to %q, want the raw token", raw)
	}
	if _, ok := ParseUnlessCost(raw); ok {
		t.Fatal("an unannounced X must not price at the strict parser")
	}
	if e.UnlessCostPayable(0, raw) {
		t.Fatal("an unannounced X must not be offered as payable")
	}
	// The announced-ZERO channel (CR 601.2b): the same carrier, resolution
	// bound with XAnnounced -- an X=0 cast legitimately pays {0}. This is the
	// plumbing the Power Sink e2e never isolates (its Count$xPaid body binds
	// zero through the SVar route); under the revert this resolves to the raw
	// token and the assertion fails.
	zero := effects.UnlessCostResolved(e, &effects.Ctx{XAnnounced: true}, fakeSA)
	if zero != "{0}" {
		t.Fatalf("announced-zero X resolved to %q, want {0}", zero)
	}
	if _, ok := ParseUnlessCost(zero); !ok {
		t.Fatal("an announced {0} must price at the strict parser")
	}
}

// TestUnlessPayableEnergyAndHalfUpGates pins the offer gate's dynamic reads
// at unit level: an announced PayEnergy the payer cannot cover is not
// payable, an unannounced PayEnergy<X> never is, and LifeTotalHalfUp folds
// against the payer's own life.
func TestUnlessPayableEnergyAndHalfUpGates(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Temporal Extortion"))

	// Energy: announced X, exactly enough.
	if got := e.UnlessCostPayableFromCtx(0, "PayEnergy<2>", &effects.Ctx{XAnnounced: true, X: 2}); got {
		t.Fatal("PayEnergy<2> announced with 0 energy must be unpayable (the payer starts with 0)")
	}
	grantEnergyForTest(t, e, 0, 2)
	if got := e.UnlessCostPayableFromCtx(0, "PayEnergy<2>", &effects.Ctx{XAnnounced: true, X: 2}); !got {
		t.Fatal("PayEnergy<2> announced with 2 energy must be payable")
	}
	if got := e.UnlessCostPayableFromCtx(0, "PayEnergy<3>", &effects.Ctx{XAnnounced: true, X: 2}); got {
		t.Fatal("PayEnergy<3> announced with 2 energy must be unpayable")
	}
	if got := e.UnlessCostPayableFromCtx(0, "PayEnergy<X>", &effects.Ctx{X: 2}); got {
		t.Fatal("PayEnergy<X> with an UNANNOUNCED X must be unpayable, never free")
	}

	// LifeTotalHalfUp: fold against the payer's own life; zero life cannot
	// pay.
	e.G.Players[0].Life = 9
	if got := e.UnlessCostPayable(0, "LifeTotalHalfUp"); !got {
		t.Fatal("LifeTotalHalfUp at 9 life must be payable")
	}
	e.G.Players[0].Life = 0
	if got := e.UnlessCostPayable(0, "LifeTotalHalfUp"); got {
		t.Fatal("LifeTotalHalfUp at 0 life must be unpayable")
	}

	// Return: reachable only with a matching candidate.
	e.G.Players[0].Life = 8
	if got := e.UnlessCostPayable(0, "Return<1/Land.nonLair/non-Lair land>"); got {
		t.Fatal("Return<1/Land.nonLair> with no land must be unpayable")
	}
	mtn := onBoard(t, e, 0, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	if mtn == 0 {
		t.Fatal("fixture land missing")
	}
	if got := e.UnlessCostPayable(0, "Return<1/Land.nonLair/non-Lair land>"); !got {
		t.Fatal("Return<1/Land.nonLair> with a Mountain in play must be payable")
	}
}

// grantEnergyForTest grants the payer n energy counters through a real event
// (the same PlayerCounterChange the charge site spends).
func grantEnergyForTest(t *testing.T, e *Engine, p state.PlayerID, n int32) {
	t.Helper()
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: p, Counter: "ENERGY", Amount: n})
}
