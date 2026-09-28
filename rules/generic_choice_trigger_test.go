package rules

// The api:GenericChoice TRIGGER-BODY per-Defined$-player path (ticket
// agent-20260922T214711Z-24c67764). The spell half landed in commit c0a04453;
// this file pins the trigger half: a triggered ability whose body is
// `DB$ GenericChoice` and whose Defined$ names a player other than the
// trigger's controller must NOT get a CR 603.3c placement ask to the trigger
// controller, and its chosen body must run exactly once per chooser with that
// chooser bound as Ctx.Remembered.
//
// Hag of Ceaseless Torment is the real carrier:
//
//	T:Mode$ Phase | Phase$ Upkeep | ValidPlayer$ You | Execute$ TrigChoose
//	SVar:TrigChoose:DB$ GenericChoice | Defined$ Opponent | Choices$ SacNonland,Discard
//	SVar:SacNonland:DB$ LoseLife | Defined$ Remembered | LifeAmount$ 3 | UnlessCost$ Sac<...> | UnlessPayer$ Remembered
//	SVar:Discard:DB$ LoseLife | Defined$ Remembered | LifeAmount$ 3 | UnlessCost$ Discard<1/Card> | UnlessPayer$ Remembered
//
// Before the fix the engine posed the placement ask to seat 0 (the Hag's
// controller), recorded it on the stack, then ran that answer's body once
// UNBOUND (so seat 0 was the one asked to pay the 3-life unless), and only
// then asked the real chooser -- one body execution too many and the wrong
// player asked the mode.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// hagBoard builds a two-seat corpus game with Hag of Ceaseless Torment on
// seat 0's battlefield and one Grizzly Bears plus a stocked hand on seat 1.
// The return trip is why seat 0 must hold nothing seat 1's unless-pay could
// touch: the assertions below all point at seat 1.
func hagBoard(t *testing.T) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	hag := mustCorpusCard(t, reg, "Hag of Ceaseless Torment")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	cfg := seatZeroStart(Config{Seed: 207, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append([]*cards.Card{hag}, mountainDeck(t, 39)...),
			append([]*cards.Card{bear}, mountainDeck(t, 39)...),
		}})
	e := New(cfg)
	e.Advance()
	hagObj := placeInDeck(t, e, 0, hag, state.ZBattlefield)
	bearObj := placeInDeck(t, e, 1, bear, state.ZBattlefield)
	// PRECONDITIONS the assertions below depend on: the trigger's source is a
	// real battlefield permanent controlled by seat 0, and seat 1 really has
	// a nonland permanent to sacrifice and a hand card to discard, so the
	// unless-pay ask is a real choice rather than a no-op.
	if z := e.G.Obj(hagObj).Zone; z != state.ZBattlefield || e.G.Obj(hagObj).Controller != 0 {
		t.Fatalf("Hag zone/controller = %s/%d, want Battlefield/0", z, e.G.Obj(hagObj).Controller)
	}
	if z := e.G.Obj(bearObj).Zone; z != state.ZBattlefield || e.G.Obj(bearObj).Controller != 1 {
		t.Fatalf("seat 1 bear zone/controller = %s/%d, want Battlefield/1", z, e.G.Obj(bearObj).Controller)
	}
	if n := len(e.G.Zone(state.ZHand, 1)); n < 1 {
		t.Fatalf("seat 1 hand = %d cards, want >= 1 for the discard branch", n)
	}
	return e, cfg, hagObj
}

// TestHagOfCeaselessTormentTriggerNeverAsksTheController is the headline
// regression: the Hag's upkeep trigger must ask the DEFINED player (seat 1)
// for the mode first, never seat 0, and the unless-pay must be posed to seat
// 1 as well. Seat 0 must never be handed a mode decision at all.
func TestHagOfCeaselessTormentTriggerNeverAsksTheController(t *testing.T) {
	t.Parallel()
	e, cfg, hagObj := hagBoard(t)

	// Seat 0's next upkeep (turn 3 -- seat 1 takes turn 2 in between). The
	// Phase$ Upkeep trigger fires as the step begins. Without the fix the
	// placement KModes to seat 0 interrupts the drive.
	driveToStepAll(t, e, e.G.Turn+2, 0, state.StepUpkeep)
	if hagObj == 0 {
		t.Fatal("no Hag object")
	}

	// The first modes ask is the per-chooser ask to seat 1, NOT a placement
	// ask to seat 0. A placement ask carries ResumeKind "modes"; the
	// per-chooser ask carries "generic_players".
	d := passUntilAskKind(t, e, decision.KModes, 40)
	if d.Player != 1 {
		t.Fatalf("first mode chooser = seat %d, want the Defined$ opponent seat 1 (never the controller seat 0)", d.Player)
	}
	if d.ResumeKind != "generic_players" {
		t.Fatalf("first mode ask ResumeKind = %q, want generic_players (a CR 603.3c placement ask is %q, wrongly posed to the controller)", d.ResumeKind, "modes")
	}
	if len(d.Options) != 2 {
		t.Fatalf("mode ask options = %d, want 2 (SacNonland/Discard)", len(d.Options))
	}

	// Choose SacNonland: the unless-pay ask must be posed to seat 1, since
	// the chosen body reads Defined$ Remembered as the chooser.
	submitChoices(t, e, 0)
	d = passUntilAskKind(t, e, decision.KModes, 40)
	if d.Player != 1 {
		t.Fatalf("unless_pay ask player = seat %d, want the chooser seat 1 (an unbound fresh-resolution body would ask seat 0)", d.Player)
	}
	if d.ResumeKind != "unless_pay" {
		t.Fatalf("unless_pay ask ResumeKind = %q, want unless_pay", d.ResumeKind)
	}
	// Declining runs the LoseLife body against the chooser.
	decline := -1
	for _, o := range d.Options {
		if o.Kind == "no" || o.Kind == "decline" {
			decline = o.Index
		}
	}
	if decline < 0 {
		// The unless ask may spell the decline as the option whose Kind is
		// not "pay"; fall back to the last option.
		decline = d.Options[len(d.Options)-1].Index
	}
	life1 := e.G.Players[1].Life
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 40)

	// The body ran EXACTLY once: seat 1 lost exactly 3 life, and seat 0 --
	// never a chooser -- lost none.
	if got := e.G.Players[1].Life; got != life1-3 {
		t.Fatalf("chooser life = %d, want %d (exactly one LoseLife body ran for seat 1)", got, life1-3)
	}
	if got := e.G.Players[0].Life; got != 20 {
		t.Fatalf("controller life = %d, want 20 (no unbound body may run against the controller)", got)
	}

	// Seat 0 is NEVER a ModeChosen chooser. (ModeChosen also records the
	// unless-pay answer, which is seat 1's, so seat 1 legitimately appears
	// twice: once for the GenericChoice mode and once for the unless.)
	var choosers []state.PlayerID
	losses := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.ModeChosen {
			choosers = append(choosers, ev.Player)
		}
		if ev.Kind == events.LifeChange && ev.Player == 1 && ev.Amount == -3 {
			losses++
		}
	}
	if len(choosers) == 0 {
		t.Fatalf("no ModeChosen recorded; the GenericChoice ask never happened")
	}
	for _, p := range choosers {
		if p == 0 {
			t.Fatalf("ModeChosen players = %v, want no seat 0 (the controller must never be a chooser)", choosers)
		}
	}
	if losses != 1 {
		t.Fatalf("seat 1 -3 life-loss events = %d, want exactly 1 (the body ran once per chooser)", losses)
	}
	replayCheck(t, e, cfg)
}

// TestTormentOfScarabsTriggeredPlayerAskedOnce pins the Defined$
// TriggeredPlayer shape (an Aura Curse whose trigger fires on the enchanted
// player's upkeep): the triggering player is asked exactly once with no
// placement ask to the controller and no double body execution. The card's
// bodies read Defined$ TriggeredPlayer rather than Remembered, so the
// chooser binding is not what proves this one -- the ask COUNT and the
// absence of a controller ask are.
func TestTormentOfScarabsTriggeredPlayerAskedOnce(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	scarabs := mustCorpusCard(t, reg, "Torment of Scarabs")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	cfg := seatZeroStart(Config{Seed: 208, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append([]*cards.Card{scarabs}, mountainDeck(t, 39)...),
			append([]*cards.Card{bear}, mountainDeck(t, 39)...),
		}})
	e := New(cfg)
	e.Advance()
	scarabsObj := placeInDeck(t, e, 0, scarabs, state.ZBattlefield)
	// Enchant seat 1 so the Aura's "enchanted player's upkeep" trigger names
	// seat 1. Attach through the engine's own Attach event.
	if z := e.G.Obj(scarabsObj).Zone; z != state.ZBattlefield {
		t.Fatalf("precondition: Torment of Scarabs zone = %s, want Battlefield", z)
	}
	e.emit(events.Event{Kind: events.Attach, Obj: scarabsObj, Text: "attach to player", Player: 1})
	if o := e.G.Obj(scarabsObj); !o.HasAttachedPlayer || o.AttachedPlayer != 1 {
		t.Fatalf("precondition: Torment of Scarabs enchants player %d (has=%v), want seat 1", o.AttachedPlayer, o.HasAttachedPlayer)
	}

	// Seat 1's next upkeep (turn 2).
	driveToStepAll(t, e, e.G.Turn+1, 1, state.StepUpkeep)

	d := passUntilAskKind(t, e, decision.KModes, 40)
	if d.Player != 1 {
		t.Fatalf("mode chooser = seat %d, want the enchanted player seat 1 (never the controller seat 0)", d.Player)
	}
	if d.ResumeKind != "generic_players" {
		t.Fatalf("mode ask ResumeKind = %q, want generic_players (no 603.3c placement ask)", d.ResumeKind)
	}
	submitChoices(t, e, 0)
	d = passUntilAskKind(t, e, decision.KModes, 40)
	if d.Player != 1 || d.ResumeKind != "unless_pay" {
		t.Fatalf("unless_pay = player %d kind %q, want seat 1 unless_pay", d.Player, d.ResumeKind)
	}
	decline := d.Options[len(d.Options)-1].Index
	life1 := e.G.Players[1].Life
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 40)

	if got := e.G.Players[1].Life; got != life1-3 {
		t.Fatalf("enchanted player life = %d, want %d (one body ran for seat 1)", got, life1-3)
	}
	if got := e.G.Players[0].Life; got != 20 {
		t.Fatalf("controller life = %d, want 20 (never a chooser)", got)
	}
	var choosers []state.PlayerID
	for _, ev := range e.L.Events {
		if ev.Kind == events.ModeChosen {
			choosers = append(choosers, ev.Player)
		}
	}
	if len(choosers) == 0 {
		t.Fatalf("no ModeChosen recorded; the GenericChoice ask never happened")
	}
	for _, p := range choosers {
		if p != 1 {
			t.Fatalf("ModeChosen players = %v, want only seat 1 (the enchanted player; never the controller seat 0)", choosers)
		}
	}
	replayCheck(t, e, cfg)
}

// TestGenericChoiceControllerCarrierStillGetsPlacementAsk is the guard that
// the fix did not widen: a GenericChoice whose Defined$ is `You` must still
// get its CR 603.3c placement ask (ResumeKind "modes") to its controller,
// exactly as before. The assertion is structural -- no corpus card needs to
// be driven, the script below is inline so the licensing rule holds.
const genericChoiceControllerBody = "Name:Trial of Self\nManaCost:R\nTypes:Enchantment\n" +
	"T:Mode$ Phase | Phase$ Upkeep | ValidPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigYou | TriggerDescription$ At the beginning of your upkeep, choose.\n" +
	"SVar:TrigYou:DB$ GenericChoice | Defined$ You | Choices$ PickA,PickB | SpellDescription$ You choose.\n" +
	"SVar:PickA:DB$ GainLife | Defined$ You | LifeAmount$ 3 | SpellDescription$ A\n" +
	"SVar:PickB:DB$ LoseLife | Defined$ You | LifeAmount$ 3 | SpellDescription$ B\n" +
	"Oracle:x\n"

// TestGenericChoiceControllerCarrierStillGetsPlacementAsk pins the retained
// path: with `Defined$ You` the per-chooser handler declines (single chooser
// is the controller), so askTriggerModes must keep posing the placement ask
// to seat 0 with ResumeKind "modes".
func TestGenericChoiceControllerCarrierStillGetsPlacementAsk(t *testing.T) {
	t.Parallel()
	trial := card(t, genericChoiceControllerBody)
	cfg := seatZeroStart(Config{Seed: 209, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{trial}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
		}})
	e := New(cfg)
	e.Advance()
	id := placeInDeck(t, e, 0, trial, state.ZBattlefield)
	if z := e.G.Obj(id).Zone; z != state.ZBattlefield {
		t.Fatalf("precondition: Trial of Self zone = %s, want Battlefield", z)
	}

	// Seat 0's next upkeep (turn 3).
	driveToStepAll(t, e, e.G.Turn+2, 0, state.StepUpkeep)

	d := passUntilAskKind(t, e, decision.KModes, 40)
	if d.Player != 0 {
		t.Fatalf("placement ask player = seat %d, want controller seat 0", d.Player)
	}
	if d.ResumeKind != "modes" {
		t.Fatalf("placement ask ResumeKind = %q, want modes (a Defined$ You carrier keeps its CR 603.3c placement ask)", d.ResumeKind)
	}
	// Answer it (PickA) and confirm the body ran for the controller.
	life0 := e.G.Players[0].Life
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Players[0].Life; got != life0+3 {
		t.Fatalf("controller life = %d, want %d (its PickA body ran)", got, life0+3)
	}
	var choosers []state.PlayerID
	for _, ev := range e.L.Events {
		if ev.Kind == events.ModeChosen {
			choosers = append(choosers, ev.Player)
		}
	}
	if len(choosers) != 1 || choosers[0] != 0 {
		t.Fatalf("ModeChosen players = %v, want exactly [0] (the placement ask retained)", choosers)
	}
	replayCheck(t, e, cfg)
}
