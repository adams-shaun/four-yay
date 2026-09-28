package rules

// Payment plans for the command-zone cast (ticket fb-20260927T163321Z-69285807):
// the auto-pay offer builder was hand-only by construction, so a commander in
// the command zone never received a "pay with a plan" action even though the
// ordinary "Cast <name>" option was offered.  These tests pin the command-zone
// admission (origin "command_zone", object in ZCommand), the commander-tax
// composition inside the plan, the offer-builder derivation of the origin from
// the object's zone, and the V1 mode gate (alternate command-zone casts stay
// manual).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func commandZoneCast(id state.ObjID) decision.PlannedCast {
	return decision.PlannedCast{Object: id, Face: 0, Origin: "command_zone"}
}

// The plain command-zone cast gets a plan, the offered witness validates, and
// cross-origin labels stay fail-closed: a hand object labeled command_zone and
// a command-zone object labeled hand are both refused, as is any object in a
// third zone (the origin's zone must be the object's zone).
func TestPaymentPlanCommandZoneCastGetsPlan(t *testing.T) {
	e, _, cmd := commanderTaxGame(t, 11)

	// Precondition: the commander object is exactly where the rule reads it.
	o := e.G.Obj(cmd)
	if o == nil || o.Zone != state.ZCommand || o.Owner != state.PlayerID(0) || o.Face() == nil {
		t.Fatalf("precondition: commander %d = %+v, want face-up object owned by seat 0 in ZCommand", cmd, o)
	}
	// Precondition: a distinct hand object exists for the cross-origin check.
	hand := e.G.Zone(state.ZHand, 0)
	if len(hand) == 0 {
		t.Fatal("precondition: seat 0 has no hand object to cross-check")
	}
	handID := hand[0]
	if handID == cmd || e.G.Obj(handID).Zone != state.ZHand {
		t.Fatalf("precondition: hand object %d is not a distinct object in ZHand", handID)
	}

	got := e.PlanCastPayment(0, commandZoneCast(cmd))
	if got.Plan == nil || got.Reason != "" {
		t.Fatalf("PlanCastPayment(command_zone) = reason %q detail %q, want a complete plan", got.Reason, got.Detail)
	}
	if err := e.ValidateCastPayment(0, commandZoneCast(cmd), *got.Plan); err != nil {
		t.Fatalf("ValidateCastPayment(command_zone): %v", err)
	}
	if got.Plan.Cost.Generic != 0 {
		t.Fatalf("first command-zone cast plan cost = %#v, want no commander tax yet", got.Plan.Cost)
	}

	// Cross-origin: a hand object labeled command_zone is refused ...
	if got := e.PlanCastPayment(0, decision.PlannedCast{Object: handID, Face: 0, Origin: "command_zone"}); got.Plan != nil || got.Reason != "unsupported" {
		t.Fatalf("PlanCastPayment(hand object as command_zone) = %#v, want unsupported", got)
	}
	// ... and the commander labeled hand is refused too.
	if got := e.PlanCastPayment(0, decision.PlannedCast{Object: cmd, Face: 0, Origin: "hand"}); got.Plan != nil || got.Reason != "unsupported" {
		t.Fatalf("PlanCastPayment(commander as hand) = %#v, want unsupported", got)
	}
	// Third zone: an object on the battlefield is refused for both origins.
	bf := onBoard(t, e, 0, "Name:Battle Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	if got := e.PlanCastPayment(0, decision.PlannedCast{Object: bf, Face: 0, Origin: "command_zone"}); got.Plan != nil || got.Reason != "unsupported" {
		t.Fatalf("PlanCastPayment(battlefield object as command_zone) = %#v, want unsupported", got)
	}
	if got := e.PlanCastPayment(0, decision.PlannedCast{Object: bf, Face: 0, Origin: "hand"}); got.Plan != nil || got.Reason != "unsupported" {
		t.Fatalf("PlanCastPayment(battlefield object as hand) = %#v, want unsupported", got)
	}
}

// The composed plan carries the CR 903.8 commander tax: +2 generic per prior
// command-zone cast of that commander, and the offered witness still settles.
func TestPaymentPlanCommandZoneTaxInPlan(t *testing.T) {
	e, _, cmd := commanderTaxGame(t, 12)

	if o := e.G.Obj(cmd); o == nil || o.Zone != state.ZCommand {
		t.Fatalf("precondition: commander %d not in ZCommand (%+v)", cmd, o)
	}
	e.G.Players[0].CmdCasts[0] = 1
	if e.G.Players[0].CmdCasts[0] != 1 {
		t.Fatal("precondition: CmdCasts was not recorded")
	}
	// A pool-only plan: {0} cost + {2} tax = 2 generic paid from the pool.
	e.G.Players[0].Pool[state.MC] = 2
	if e.G.Players[0].Pool[state.MC] != 2 {
		t.Fatal("precondition: pool funding did not stick")
	}

	got := e.PlanCastPayment(0, commandZoneCast(cmd))
	if got.Plan == nil {
		t.Fatalf("PlanCastPayment after one prior cast = reason %q detail %q, want a taxed plan", got.Reason, got.Detail)
	}
	if got.Plan.Cost.Generic != 2 {
		t.Fatalf("plan cost = %#v, want Generic 2 (one prior command-zone cast)", got.Plan.Cost)
	}
	if err := e.ValidateCastPayment(0, commandZoneCast(cmd), *got.Plan); err != nil {
		t.Fatalf("ValidateCastPayment(taxed plan): %v", err)
	}
}

// At a commander Main1 priority the offer builder derives the origin from the
// object's zone and offers exactly one payment action for the commander, whose
// BaseOptionIndex points at the ordinary "Cast <name>" option.
func TestPaymentPlanPriorityOffersCommanderPlan(t *testing.T) {
	e, _, cmd := commanderTaxGame(t, 13)

	// Add a zero-cost ordinary hand spell so this mixed-origin priority pins
	// that admitting command-zone casts leaves the existing hand offer intact.
	handSpell := e.G.AddObject(card(t, `Name:Hand Plan Probe
ManaCost:0
Types:Creature Wizard
PT:1/1
Oracle:x
`), 0)
	handSpell.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), handSpell.ID))
	e.pending = nil
	e.Advance()

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("precondition: pending decision = %+v, want seat 0 priority", d)
	}
	opt := commanderCastOption(e, cmd)
	if opt == nil {
		t.Fatalf("precondition: no plain cast option for the commander among %+v", d.Options)
	}
	if opt.Mode != "" || opt.AltCostIndex != 0 {
		t.Fatalf("precondition: commander cast option has Mode %q AltCostIndex %d, want plain", opt.Mode, opt.AltCostIndex)
	}

	actions := e.EnsurePaymentActions()
	if len(actions) != 2 {
		t.Fatalf("EnsurePaymentActions = %d actions (%+v), want the commander and existing hand spell", len(actions), actions)
	}
	var commanderAction, handAction *decision.PaymentAction
	for i := range actions {
		switch actions[i].Cast.Object {
		case cmd:
			commanderAction = &actions[i]
		case handSpell.ID:
			handAction = &actions[i]
		}
	}
	a := commanderAction
	if a == nil || a.Cast.Origin != "command_zone" || a.Cast.Face != 0 {
		t.Fatalf("commander action = %+v, want origin command_zone", a)
	}
	if handAction == nil || handAction.Cast.Origin != "hand" {
		t.Fatalf("hand action = %+v, want the unchanged hand-origin action", handAction)
	}
	handPlan := e.PlanCastPayment(0, decision.PlannedCast{Object: handSpell.ID, Face: 0, Origin: "hand"})
	if handPlan.Plan == nil || len(handAction.Plans) != 1 || handAction.Plans[0].Cost != handPlan.Plan.Cost || handAction.Plans[0].PoolSpend != handPlan.Plan.PoolSpend || handAction.Plans[0].PoolAfter != handPlan.Plan.PoolAfter || len(handAction.Plans[0].Activations) != len(handPlan.Plan.Activations) {
		t.Fatalf("hand action plan = %+v, direct hand plan = %+v; existing hand-cast offer changed", handAction, handPlan.Plan)
	}
	if handAction.BaseOptionIndex == nil || d.Options[*handAction.BaseOptionIndex].Obj != handSpell.ID {
		t.Fatalf("hand action BaseOptionIndex = %v, want the plain hand spell option", handAction.BaseOptionIndex)
	}
	if a.BaseOptionIndex == nil {
		t.Fatal("commander action has no BaseOptionIndex")
	}
	base := d.Options[*a.BaseOptionIndex]
	if base.Kind != "cast" || base.Obj != cmd || base.Mode != "" || base.AltCostIndex != 0 {
		t.Fatalf("BaseOptionIndex %d -> %+v, want the plain Cast option for the commander", *a.BaseOptionIndex, base)
	}
	if len(a.Plans) != 1 {
		t.Fatalf("action plans = %+v, want exactly one witness", a.Plans)
	}
	if err := e.ValidateCastPayment(0, a.Cast, a.Plans[0]); err != nil {
		t.Fatalf("ValidateCastPayment(offered action): %v", err)
	}
}

// The V1 mode gate holds in the command zone: an alternate-cost command-zone
// cast (evoke) is offered as an option but gets NO payment action -- only the
// plain taxed cast is planned.
func TestPaymentPlanCommandZoneEvokeVariantStaysManual(t *testing.T) {
	evokeCommanderSrc := `Name:Evoke Stick
ManaCost:0
Types:Legendary Creature Golem
PT:1/1
K:Partner
K:Evoke:2
Oracle:x
`
	e, cfg := commanderGame(t, 14, FormatCommander, 0, [][]string{{evokeCommanderSrc}, {commanderBeatstickSrc}})
	e.Advance()
	driveToStep(t, e, 1, 0, state.StepMain1)
	cmd := e.G.Players[0].Commanders[0]

	// Fund the pool so the evoked alternative is affordable and therefore
	// actually offered; Pool alone never trips the pool-OK gate.
	e.G.Players[0].Pool[state.MC] = 2
	e.pending = nil
	e.Advance()

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("precondition: pending decision = %+v, want seat 0 priority", d)
	}
	var plain, evoked *decision.Option
	for i := range d.Options {
		o := &d.Options[i]
		if o.Kind != "cast" || o.Obj != cmd {
			continue
		}
		switch o.Mode {
		case "":
			plain = o
		case "evoked":
			evoked = o
		}
	}
	if plain == nil {
		t.Fatalf("precondition: no plain cast option for the commander among %+v", d.Options)
	}
	if evoked == nil {
		t.Fatalf("precondition: no evoked cast option for the commander among %+v", d.Options)
	}

	actions := e.EnsurePaymentActions()
	if len(actions) != 1 {
		t.Fatalf("EnsurePaymentActions = %d actions (%+v), want exactly one", len(actions), actions)
	}
	a := actions[0]
	if a.Cast.Object != cmd || a.Cast.Origin != "command_zone" {
		t.Fatalf("action cast = %+v, want the commander with origin command_zone", a.Cast)
	}
	if a.BaseOptionIndex == nil || d.Options[*a.BaseOptionIndex].Mode != "" {
		t.Fatalf("BaseOptionIndex %v -> %+v, want the PLAIN cast option, never the evoked variant", a.BaseOptionIndex, d.Options[*a.BaseOptionIndex])
	}
	if got := e.PlanCastPayment(0, commandZoneCast(cmd)); got.Plan == nil {
		t.Fatalf("plain command-zone cast declined: reason %q detail %q", got.Reason, got.Detail)
	}
	_ = cfg
}
