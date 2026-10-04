package rules

// The krrik-unless-window regression: K'rrik, Son of Yawgmoth's
// PayLifeInsteadOf:B grant must not CLOSE an unless-cost mana window. The
// cast-window fix (rakdos_params_paylifeinsteadof_test.go,
// TestKrrikGrantPosesManaWindowWhenSourceCanPay) taught the CR 601.2g cast
// window to ask whether the POOL ALONE pays; the same silent-life shape
// survived in the two mid-resolution unless windows:
//
//   - rules/unless_payment.go advanceUnlessPayment (the standalone /
//     choice-bearing UnlessCost$ continuation), reached here with a real
//     mixed `Sac<1/Creature> B` cost -- the shape that carries both a choice
//     part and a mana pip, and the only shape that reaches this gate (a
//     mana-only cost goes through the shared window below); and
//   - rules/unless_mana.go unlessManaWindowNeeded -> rules/ward.go askWardMana
//     (the shared mid-resolution Ward/unless window opened by resolution.go's
//     unless_pay election), reached here with the real corpus card
//     Whipstitched Zombie ("...sacrifice CARDNAME unless you pay {B}").
//
// With a {B} pip, an empty pool and an untapped Swamp, the grant made the
// cost look pool-payable, so neither window opened and the pip was silently
// charged as 2 life with the Swamp still untapped. The window gates now read
// costPayableClassLife(..., false); the payment sites keep the grant, so a
// payer with no source to tap still pays the granted life.
//
// Corpus provenance: `PayLifeInsteadOf` appears in exactly one corpus file
// (.cards/cardsfolder/k/krrik_son_of_yawgmoth.txt); Whipstitched Zombie's
// SVar:TrigUpkeep is the real `UnlessCost$ B` trigger
// (.cards/cardsfolder/w/whipstitched_zombie.txt). No corpus card carries a
// mixed choice+mana unless cost with a {B} pip, so the advanceUnlessPayment
// fixture is a minimal synthetic ability whose UnlessCost$ parameter is the
// real parser input.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// krrikMixedUnlessScript is the synthetic upkeep ability for the
// advanceUnlessPayment path: "At the beginning of your upkeep, sacrifice
// CARDNAME unless you pay a creature and {B}." The `Sac<1/Creature> B` cost
// is the mixed choice+mana shape the brief names.
const krrikMixedUnlessScript = "Name:Krrik Mixed Monger\nManaCost:B\nTypes:Creature Zombie\nPT:2/2\n" +
	"T:Mode$ Phase | Phase$ Upkeep | ValidPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigUpkeep | TriggerDescription$ sac unless pay\n" +
	"SVar:TrigUpkeep:DB$ Sacrifice | UnlessPayer$ You | UnlessCost$ Sac<1/Creature> B\nOracle:x\n"

// krrikMixedUnlessFixture puts K'rrik, the mixed unless-cost permanent, a
// spare creature for the sacrifice part and (when withSource) an untapped
// Swamp on seat 0's battlefield, with an EMPTY pool. It returns the engine,
// the Monger id, the spare creature id and the Swamp id (0 when absent).
func krrikMixedUnlessFixture(t *testing.T, withSource bool) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	e := handEngine(t, corpusAlternativeCard(t, "K'rrik, Son of Yawgmoth"))
	moved := false
	for _, o := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(o).Face().Name == "K'rrik, Son of Yawgmoth" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o, From: state.ZHand, To: state.ZBattlefield})
			moved = true
		}
	}
	if !moved {
		t.Fatal("precondition: K'rrik was not in seat 0's hand to put on the battlefield")
	}
	monger := e.G.AddObject(card(t, krrikMixedUnlessScript), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: monger.ID, From: state.ZLibrary, To: state.ZBattlefield})
	spare := e.G.AddObject(card(t, "Name:Spare\nManaCost:1\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: spare.ID, From: state.ZLibrary, To: state.ZBattlefield})
	var swamp state.ObjID
	if withSource {
		so := e.G.AddObject(card(t, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n"), 0)
		e.emit(events.Event{Kind: events.MoveZone, Obj: so.ID, From: state.ZLibrary, To: state.ZBattlefield})
		swamp = so.ID
	}
	e.G.Players[0].Pool = state.Mana{}
	e.G.Players[0].Life = 20
	if !asEval(e).PayLifeInsteadOfB(0) {
		t.Fatal("precondition: K'rrik's PayLifeInsteadOf:B grant is not active for seat 0")
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 0 {
		t.Fatalf("precondition: pool must be empty, got %+v", pool)
	}
	if swamp != 0 && !e.untappedManaSource(0, swamp) {
		t.Fatal("precondition: the untapped Swamp is not a window mana source")
	}
	e.askPriority(0)
	return e, monger.ID, spare.ID, swamp
}

// driveToUnlessPay passes priority until the upkeep unless-pay election is
// pending, and returns it. A Pay answer must be submitted by the caller.
func driveToUnlessPay(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 600 && !e.G.Over; i++ {
		if answerIfDiscard(t, e) {
			continue
		}
		d := e.Pending()
		if d == nil {
			t.Fatalf("engine stalled before the upkeep unless-pay ask")
		}
		if d.Kind == decision.KModes && d.ResumeKind == "unless_pay" {
			return d
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected %v before the upkeep unless-pay ask: %+v", d.Kind, d)
		}
		castFirst(t, e, "pass")
	}
	t.Fatal("the upkeep unless-pay ask was never posed")
	return nil
}

// windowOption returns the index of the first option matching kind (and obj
// when obj != 0) in the pending window, or -1.
func windowOption(d *decision.Decision, kind string, obj state.ObjID) int {
	for _, o := range d.Options {
		if o.Kind == kind && (obj == 0 || o.Obj == obj) {
			return o.Index
		}
	}
	return -1
}

// TestKrrikGrantPosesUnlessManaWindowWhenSourceCanPay drives the standalone
// UnlessCost$ continuation (advanceUnlessPayment) with a real mixed
// `Sac<1/Creature> B` cost. Before the fix the gate derived the grant, so an
// empty pool + untapped Swamp looked payable: no window opened and the {B}
// was silently charged as 2 life. The window must now be posed, offer the
// Swamp, and tapping it must pay the {B} with no life loss.
func TestKrrikGrantPosesUnlessManaWindowWhenSourceCanPay(t *testing.T) {
	t.Parallel()
	e, _, _, swamp := krrikMixedUnlessFixture(t, true)
	pay := driveToUnlessPay(t, e)
	if pay.Player != 0 {
		t.Fatalf("unless-pay payer = seat %d, want seat 0", pay.Player)
	}
	submitChoices(t, e, pay.Options[0].Index) // Pay

	w := e.Pending()
	if w == nil || w.Kind != decision.KChoose || w.ResumeKind != "unless_mana" {
		t.Fatalf("the unless mana window must be posed when the pool alone cannot pay the {B} but an untapped source can, got %+v", w)
	}
	swampOpt := windowOption(w, "activate", swamp)
	if swampOpt < 0 {
		t.Fatalf("the untapped Swamp must be offered in the window: %+v", w.Options)
	}
	submitChoices(t, e, swampOpt)

	// The {B} is now floating; the sacrifice part is selected, then the
	// payment completes.
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_cost" {
		t.Fatalf("the sacrifice part must be asked after the mana is paid, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 30)

	if life := e.G.Players[0].Life; life != 20 {
		t.Fatalf("life=%d, want 20 (the {B} must be paid with the tapped Swamp's mana, not 2 granted life)", life)
	}
	if !e.G.Obj(swamp).Tapped {
		t.Fatalf("the Swamp must be tapped to pay the {B}")
	}
}

// TestKrrikGrantPaysLifeFromUnlessWindowWhenNoSourceCan is the companion: with
// no untapped source the window cannot open, and the granted 2 life must still
// pay the {B} through advanceUnlessPayment's retained grant-bearing check.
func TestKrrikGrantPaysLifeFromUnlessWindowWhenNoSourceCan(t *testing.T) {
	t.Parallel()
	e, _, _, _ := krrikMixedUnlessFixture(t, false)
	pay := driveToUnlessPay(t, e)
	submitChoices(t, e, pay.Options[0].Index) // Pay

	// No source to tap: the next ask is the sacrifice part, not a window.
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_cost" {
		t.Fatalf("with no untapped source the window must not open; expected the sacrifice ask, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 30)

	if life := e.G.Players[0].Life; life != 18 {
		t.Fatalf("life=%d, want 18 (the {B} pip's 2 granted life)", life)
	}
}

// krrikZombieFixture puts K'rrik, the real corpus Whipstitched Zombie and an
// untapped Swamp on seat 0's battlefield with an EMPTY pool, and returns the
// engine plus the ids. The Zombie's upkeep trigger is the real corpus
// `UnlessCost$ B` shape that drives resolution.go's unless_pay election into
// the SHARED window (unlessManaWindowNeeded -> askWardMana).
func krrikZombieFixture(t *testing.T) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	extras := []*cards.Card{
		lookup(t, reg, "K'rrik, Son of Yawgmoth"),
		lookup(t, reg, "Whipstitched Zombie"),
		lookup(t, reg, "Swamp"),
	}
	e := corpusEngine(t, reg, extras, nil)
	moveByName(t, e, 0, "K'rrik, Son of Yawgmoth", state.ZBattlefield)
	zombie := moveByName(t, e, 0, "Whipstitched Zombie", state.ZBattlefield)
	swamp := moveByName(t, e, 0, "Swamp", state.ZBattlefield)
	e.G.Players[0].Pool = state.Mana{}
	e.G.Players[0].Life = 20
	if !asEval(e).PayLifeInsteadOfB(0) {
		t.Fatal("precondition: K'rrik's PayLifeInsteadOf:B grant is not active for seat 0")
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 0 {
		t.Fatalf("precondition: pool must be empty, got %+v", pool)
	}
	if o := e.G.Obj(swamp); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("precondition: the Swamp must be an untapped battlefield permanent, got %+v", o)
	}
	if !e.untappedManaSource(0, swamp) {
		t.Fatal("precondition: the untapped Swamp is not a window mana source")
	}
	return e, zombie, swamp
}

// TestKrrikGrantPosesWardManaWindowWhenSourceCanPay drives the SHARED
// mid-resolution window (the same one a Ward payment uses): resolution.go's
// unless_pay election calls unlessManaWindowNeeded to decide whether to open
// it, and askWardMana enumerates its sources. Both used the grant-bearing
// costPayableClass, so with a granted {B} the predicate said "not needed" and
// askWardMana's payable closure said "payable", offering only Done. Both must
// now ask whether the POOL ALONE pays: the window opens, the Swamp is offered,
// and tapping it pays the {B} without life loss.
func TestKrrikGrantPosesWardManaWindowWhenSourceCanPay(t *testing.T) {
	t.Parallel()
	e, zombie, swamp := krrikZombieFixture(t)
	pay := driveToUnlessPay(t, e)
	submitChoices(t, e, pay.Options[0].Index) // Pay

	w := e.Pending()
	if w == nil || w.Kind != decision.KChoose || w.ResumeKind != "unless_mana" {
		t.Fatalf("the shared unless/ward window must be posed, got %+v", w)
	}
	swampOpt := windowOption(w, "activate", swamp)
	if swampOpt < 0 {
		t.Fatalf("the untapped Swamp must be offered in the shared window: %+v", w.Options)
	}
	submitChoices(t, e, swampOpt)

	// One {B} fills the pip: the window reopens offering only Done.
	w = e.Pending()
	if w == nil || w.ResumeKind != "unless_mana" {
		t.Fatalf("the window must reopen after the tap, got %+v", w)
	}
	doneOpt := windowOption(w, "done", 0)
	if doneOpt < 0 {
		t.Fatalf("the window must offer Done: %+v", w.Options)
	}
	submitChoices(t, e, doneOpt)
	passUntilStackEmpty(t, e, 30)

	if life := e.G.Players[0].Life; life != 20 {
		t.Fatalf("life=%d, want 20 (the {B} must be paid with the tapped Swamp's mana, not 2 granted life)", life)
	}
	if !e.G.Obj(swamp).Tapped {
		t.Fatalf("the Swamp must be tapped to pay the {B}")
	}
	if z := e.G.Obj(zombie).Zone; z != state.ZBattlefield {
		t.Fatalf("Whipstitched Zombie's {B} was paid but it went to %s", z)
	}
}

// TestKrrikGrantPaysLifeFromWardWindowWhenNoSourceCan answers the posed shared
// window with Done: the {B} then spends the granted 2 life through
// payUnlessCost (answerWardMana's Done arm), proving the window did not remove
// the life route. The Swamp stays untapped and the Zombie survives.
func TestKrrikGrantPaysLifeFromWardWindowWhenNoSourceCan(t *testing.T) {
	t.Parallel()
	e, zombie, swamp := krrikZombieFixture(t)
	pay := driveToUnlessPay(t, e)
	submitChoices(t, e, pay.Options[0].Index) // Pay

	w := e.Pending()
	if w == nil || w.Kind != decision.KChoose || w.ResumeKind != "unless_mana" {
		t.Fatalf("the shared window must be posed, got %+v", w)
	}
	doneOpt := windowOption(w, "done", 0)
	if doneOpt < 0 {
		t.Fatalf("the window must offer Done: %+v", w.Options)
	}
	submitChoices(t, e, doneOpt)
	passUntilStackEmpty(t, e, 30)

	if life := e.G.Players[0].Life; life != 18 {
		t.Fatalf("life=%d, want 18 (Done spends the {B} pip's 2 granted life)", life)
	}
	if e.G.Obj(swamp).Tapped {
		t.Fatalf("the Swamp must stay untapped when the payer answers Done")
	}
	if z := e.G.Obj(zombie).Zone; z != state.ZBattlefield {
		t.Fatalf("Whipstitched Zombie's {B} was paid but it went to %s", z)
	}
}
