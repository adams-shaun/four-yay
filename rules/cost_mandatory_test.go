package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The cost:Mandatory regression pin (task costmand1). A census agent reported
// that ParseCost treated Forge's `Mandatory` cost head as an unknown token and
// substituted one generic mana of it, so a `Cost$ Mandatory Sac<1/CARDNAME>`
// trigger body (Colfenor's Urn's end-step ability) was priced {1} too high.
//
// The fix -- `rules/mana.go`'s explicit `Mandatory` case, a skip-the-token
// `continue` -- is already in at this build's HEAD; this file is the direct
// regression pin the report asked for, on the real corpus prevalence. It pins
// three halves:
//
//  1. ParseCost for every `Cost$ Mandatory ...` head the corpus actually
//     carries (a table, re-measured 2026-09-20: Sac 15, PayEnergy 8,
//     PayLife 3, tapXType 1, ExileFromGrave 1, Exile 1 -- 29 raw lines) parses
//     with Generic == 0 and no Unknown entry.
//
//  2. The real card's own script: Colfenor's Urn's `Cost$ Mandatory
//     Sac<1/CARDNAME>` end-step trigger charges the sacrifice (the Urn leaves
//     the battlefield for the graveyard) rather than a phantom generic mana.
//     The fixture carries the card's exact script inline (never a committed
//     .txt -- Forge scripts are GPL-3.0 and this repo is Apache-2.0); the
//     Urn is in no repo deck, so TestHeads is independent of it.
//
//  3. The same card's gate: the end-step trigger's intervening-if
//     (`CheckSVar$ X | SVarCompare$ GE3`, X reading `SVar:X:ExiledWith$Amount`)
//     must actually evaluate -- the trigger stays silent below the threshold
//     and fires at it. The count evaluator needed the ExiledWith ref (and the
//     Amount property) for this; see effects/count.go's refTargets.

// TestMandatoryCostHeadParsesWithoutGeneric is the ParseCost half: every
// Mandatory-prefixed cost shape measured in the corpus parses with a zero
// generic component and no Unknown token. Before the fix the Mandatory head
// fell through to ParseCost's unknown-token default, which substitutes one
// generic mana.
func TestMandatoryCostHeadParsesWithoutGeneric(t *testing.T) {
	t.Parallel()
	// Every head shape the corpus carries, one representative line each
	// (/usr/bin/grep -rhoE 'Cost\$ Mandatory [A-Za-z]+' measured at the
	// current corpus pin: Sac, PayEnergy, PayLife, tapXType, ExileFromGrave,
	// Exile).
	costs := []string{
		"Mandatory Sac<1/CARDNAME>",                               // the Colfenor's Urn shape (15 lines)
		"Mandatory Sac<1/CARDNAME/this artifact>",                 // the third-field display form (1 line)
		"Mandatory PayEnergy<2>",                                  // 8 lines
		"Mandatory PayEnergy<X>",                                  // the announced form
		"Mandatory PayLife<X>",                                    // 3 lines
		"Mandatory tapXType<X/Artifact>",                          // 1 line (yotia_declares_war)
		"Mandatory ExileFromGrave<2/Card>",                        // 1 line
		"Mandatory Exile<1/Creature.nonDalek/non-Dalek creature>", // 1 line (Dalek Intensive Care)
		"Mandatory", // a bare marker with no payment: still zero generic, no Unknown
	}
	for _, raw := range costs {
		c := ParseCost(raw)
		if c.Generic != 0 {
			t.Errorf("ParseCost(%q).Generic = %d, want 0 (the Mandatory head is a payment-mode marker, not a payment)", raw, c.Generic)
		}
		if len(c.Unknown) != 0 {
			t.Errorf("ParseCost(%q).Unknown = %v, want empty (Mandatory must never fall through to the unknown-token default)", raw, c.Unknown)
		}
	}
}

// colfenorUrnScript is Colfenor's Urn's exact compiled script, inlined. The
// corpus copy is `.cards/cardsfolder/c/colfenors_urn.txt`; this fixture is
// textually identical so the settle under test is the real card's. The
// end-step trigger is a `T:Mode$ Phase` ability whose body's Cost is the
// Mandatory Sac<1/CARDNAME> under test.
const colfenorUrnScript = "Name:Colfenor's Urn\n" +
	"ManaCost:3\n" +
	"Types:Artifact\n" +
	"T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | TriggerZones$ Battlefield | ValidCard$ Creature.toughnessGE4+YouOwn | OptionalDecider$ You | Execute$ TrigExile | TriggerDescription$ Whenever a creature with toughness 4 or greater is put into your graveyard from the battlefield, you may exile it.\n" +
	"SVar:TrigExile:DB$ ChangeZone | Origin$ Graveyard | Destination$ Exile | Defined$ TriggeredNewCardLKICopy\n" +
	"T:Mode$ Phase | Phase$ End of Turn | TriggerZones$ Battlefield | CheckSVar$ X | SVarCompare$ GE3 | Execute$ TrigReturnAll | TriggerDescription$ At the beginning of the end step, if three or more cards have been exiled with CARDNAME, sacrifice it. If you do, return those cards to the battlefield under their owner's control.\n" +
	"SVar:TrigReturnAll:AB$ ChangeZone | Cost$ Mandatory Sac<1/CARDNAME> | Defined$ ExiledWith | Origin$ Exile | Destination$ Battlefield\n" +
	"SVar:X:ExiledWith$Amount\n" +
	"Oracle:Whenever a creature with toughness 4 or greater is put into your graveyard from the battlefield, you may exile it.\\nAt the beginning of the end step, if three or more cards have been exiled with Colfenor's Urn, sacrifice it. If you do, return those cards to the battlefield under their owner's control.\n"

// colfenorUrnFixture enters the Urn (from the inline script) on seat 0's
// battlefield and seats `ids` in seat 0's exile with the ExiledWith
// association the Urn's first trigger would have recorded
// (state.Object.ExiledWith is what both the gate's ExiledWith$Amount count
// and the body's Defined$ ExiledWith resolve against).
func colfenorUrnFixture(t *testing.T, e *Engine, exiledScripts ...string) (urn state.ObjID, exiled []state.ObjID) {
	t.Helper()
	u := e.G.AddObject(card(t, colfenorUrnScript), 0)
	u.Zone = state.ZBattlefield
	u.SummonSick = true
	e.G.Clock++
	u.Timestamp = e.G.Clock
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), u.ID))
	e.staticEpoch = -1
	e.activeEpoch = -1

	for _, s := range exiledScripts {
		id := e.G.AddObject(card(t, s), 0).ID
		o := e.G.Obj(id)
		o.Zone = state.ZExile
		o.ExiledWith = u.ID
		exiled = append(exiled, id)
	}
	e.G.SetZone(state.ZExile, 0, exiled)
	return u.ID, exiled
}

const (
	bearFixtureScript = "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	lionFixtureScript = "Name:Savannah Lions\nManaCost:W\nTypes:Creature Cat\nPT:2/1\nOracle:x\n"
)

// TestColfenorUrnGateBelowThresholdStaysSilent pins the gate half of the
// same real card: with only two cards exiled with the Urn the CheckSVar$ X
// (SVar:X:ExiledWith$Amount, SVarCompare$ GE3) gate is NOT met, so the
// end-step trigger stays silent -- the Urn stays on the battlefield, the
// exiled cards stay in exile and no settle runs. Before the count evaluator
// learned the ExiledWith ref this gate read 0 at EVERY threshold, so this pin
// exercises the exact expression the fix added (effects/count.go
// refTargets' ExiledWith case plus evalRefProperty's Amount property).
func TestColfenorUrnGateBelowThresholdStaysSilent(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	urnID, exiled := colfenorUrnFixture(t, e, bearFixtureScript, bearFixtureScript)

	e.setStep(state.StepEnd)
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(urnID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Colfenor's Urn left the battlefield below the GE3 threshold; the gate must stay closed")
	}
	for _, ev := range e.L.Events {
		if events.IsSacrifice(ev) && ev.Obj == urnID {
			t.Fatalf("the Urn was sacrificed below the gate threshold: %+v", ev)
		}
	}
	for _, id := range exiled {
		if o := e.G.Obj(id); o != nil && o.Zone != state.ZExile {
			t.Fatalf("exiled card %d moved below the gate threshold (zone %s)", id, o.Zone)
		}
	}
}

// TestColfenorUrnMandatorySacrificeChargesTheUrn is the real-card half: at the
// beginning of the end step, with three creatures exiled with the Urn, the
// `Cost$ Mandatory Sac<1/CARDNAME>` body must sacrifice the Urn (a real
// events.Sacrifice putting it into the graveyard) rather than being bought for
// a phantom generic, and only then return the exiled cards.
//
// The trigger's own gate (`CheckSVar$ X | SVarCompare$ GE3`, X reading
// `ExiledWith$Amount`) is what makes the three-creature setup load-bearing:
// the gate must evaluate against the real exile association, and the settle
// the armed trigger runs must charge the sacrifice with no ask (the sole
// CARDNAME-eligible permanent is the Urn itself) before the body returns the
// exiled cards.
func TestColfenorUrnMandatorySacrificeChargesTheUrn(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	urnID, exiled := colfenorUrnFixture(t, e, bearFixtureScript, lionFixtureScript, bearFixtureScript)

	// The Urn's end-step trigger fires only at the end step; drive there.
	// The gate is met (three cards exiled with the Urn), so the trigger must
	// drain.
	e.setStep(state.StepEnd)
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)

	// The Urn is sacrificed, not bought for a phantom generic.
	uo := e.G.Obj(urnID)
	if uo == nil || uo.Zone == state.ZBattlefield {
		t.Fatalf("Colfenor's Urn is still on the battlefield; the Mandatory Sac<1/CARDNAME> cost was not charged")
	}
	if uo.Zone != state.ZGraveyard {
		t.Fatalf("Colfenor's Urn zone = %s, want the graveyard after being sacrificed", uo.Zone)
	}
	sawSac := false
	for _, ev := range e.L.Events {
		if events.IsSacrifice(ev) && ev.Obj == urnID {
			sawSac = true
		}
	}
	if !sawSac {
		t.Fatalf("no events.Sacrifice for Colfenor's Urn; the Mandatory cost was never settled")
	}
	// No phantom generic was charged against the pool (handEngine starts with
	// an empty pool; a phantom {1} would either be unpaid or paid from a
	// source the fixture does not have -- either way the pool must stay empty).
	if p := e.G.Players[0].Pool; p != (state.Mana{}) {
		t.Fatalf("mana pool after the cost = %+v, want empty (Mandatory is a marker, not a {1})", p)
	}
	// A mandatory cost has no "may": no pay/decline election was posed.
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && (ev.Text == "trigger_cost_pay" || ev.Text == "trigger_cost_decline") {
			t.Fatalf("a Mandatory cost posed a pay/decline election: %+v", ev)
		}
	}
	// The body ran: the three exiled cards returned to the battlefield.
	onBoard := 0
	for _, id := range exiled {
		if o := e.G.Obj(id); o != nil && o.Zone == state.ZBattlefield {
			onBoard++
		}
	}
	if onBoard != 3 {
		t.Fatalf("exiled cards returned to the battlefield = %d, want 3 (the paid body must run and return them)", onBoard)
	}
}
