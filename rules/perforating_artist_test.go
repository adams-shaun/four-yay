package rules

// api:GenericChoice `TempRemember$ Chooser` / `FallbackAbility$` on the real
// corpus carrier Perforating Artist (FDN):
//
//	SVar:TrigTorment:DB$ GenericChoice | Defined$ Opponent | TempRemember$ Chooser
//	    | Choices$ SacNonland,Discard | FallbackAbility$ LoseLifeFallback
//	SVar:Discard:DB$ LoseLife | Defined$ Remembered | LifeAmount$ 3
//	    | UnlessCost$ Discard<1/Card> | UnlessPayer$ Remembered
//	SVar:SacNonland:DB$ LoseLife | Defined$ Remembered | LifeAmount$ 3
//	    | UnlessCost$ Sac<1/Permanent.nonLand/...> | UnlessPayer$ Remembered
//	SVar:LoseLifeFallback:DB$ LoseLife | Defined$ Remembered | LifeAmount$ 3
//
// Forge's ChooseGenericEffect.resolve: per chooser it removes the previously
// TempRemembered players, adds the chooser, resolves the chosen (or fallback)
// ability, then removes the chooser and restores the old set. It also drops
// every choice whose UnlessCost$ the chooser cannot currently pay; when the
// whole list is unpayable it resolves FallbackAbility$ instead of asking.
// These tests pin both halves on the trigger path: the chooser is the DEFINED
// opponent (never the Artist's controller) for the chosen body's UnlessPayer$,
// a payable choice still asks and pays, a decline loses 3, and an opponent
// with nothing to discard or sacrifice is never asked (the fallback runs).

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// perforatingArtistBoard builds a two-seat corpus game with Perforating
// Artist on seat 0's battlefield and one Grizzly Bears plus a stocked hand on
// seat 1. The preconditions assert the source is a real permanent controlled
// by seat 0 and seat 1 really holds a card and a nonland permanent, so the
// unless choice is a real one rather than a fallback no-op.
func perforatingArtistBoard(t *testing.T) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	artist := mustCorpusCard(t, reg, "Perforating Artist")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	cfg := seatZeroStart(Config{Seed: 311, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append([]*cards.Card{artist}, mountainDeck(t, 39)...),
			append([]*cards.Card{bear}, mountainDeck(t, 39)...),
		}})
	e := New(cfg)
	e.Advance()
	artistObj := placeInDeck(t, e, 0, artist, state.ZBattlefield)
	bearObj := placeInDeck(t, e, 1, bear, state.ZBattlefield)
	if z := e.G.Obj(artistObj).Zone; z != state.ZBattlefield || e.G.Obj(artistObj).Controller != 0 {
		t.Fatalf("precondition: Perforating Artist zone/controller = %s/%d, want Battlefield/0", z, e.G.Obj(artistObj).Controller)
	}
	if z := e.G.Obj(bearObj).Zone; z != state.ZBattlefield || e.G.Obj(bearObj).Controller != 1 {
		t.Fatalf("precondition: seat 1 bear zone/controller = %s/%d, want Battlefield/1", z, e.G.Obj(bearObj).Controller)
	}
	if n := len(e.G.Zone(state.ZHand, 1)); n < 2 {
		t.Fatalf("precondition: seat 1 hand = %d cards, want >= 2 (a discard and a spare)", n)
	}
	return e, cfg, artistObj
}

// raidToEndStep drives the real turn cycle to seat 0's NEXT turn (so the
// Artist is no longer summoning sick, CR 302.6, and the attack declaration is
// recorded in the event log rather than poked into the object), declares it
// as an attacker at seat 1 (satisfying the Raid
// CheckSVar$ Count$AttackersDeclared gate), then drives to that turn's end
// step where the trigger fires. The declared attacker is asserted so a silent
// Raid-gate failure cannot make the test vacuous.
func raidToEndStep(t *testing.T, e *Engine, artistObj state.ObjID) {
	t.Helper()
	driveToStepAll(t, e, e.G.Turn+2, 0, state.StepDeclareAttackers)
	var d *decision.Decision
	for i := 0; i < 40; i++ {
		d = e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			t.Fatal("no decision at seat 0's declare-attackers step")
		}
		if d.Kind == decision.KAttackers {
			break
		}
		if d.Kind == decision.KPriority {
			submitPass(t, e)
			continue
		}
		t.Fatalf("unexpected decision %+v before the attackers step", d)
	}
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("pending at seat 0's declare-attackers = %+v, want KAttackers", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == artistObj && o.Player == 1 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Artist not offered as an attacker at seat 1: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	found := false
	for _, id := range e.AttackersDeclaredThisTurn() {
		if id == artistObj {
			found = true
		}
	}
	if !found {
		t.Fatalf("precondition: Artist not recorded as an attacker this turn (raid gate would suppress the trigger)")
	}
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
}

// artistTriggerAsk drives to seat 0's end step and returns the trigger's
// per-chooser KModes ask, asserting it is seat 1's GenericChoice ask (never a
// CR 603.3c placement ask to the controller).
func artistTriggerAsk(t *testing.T, e *Engine, artistObj state.ObjID) *decision.Decision {
	t.Helper()
	raidToEndStep(t, e, artistObj)
	d := passUntilAskKind(t, e, decision.KModes, 60)
	if d.Player != 1 {
		t.Fatalf("chooser = seat %d, want the Defined$ opponent seat 1 (never the controller 0)", d.Player)
	}
	if d.ResumeKind != "generic_players" {
		t.Fatalf("chooser ResumeKind = %q, want generic_players (a placement ask would be %q)", d.ResumeKind, "modes")
	}
	if len(d.Options) != 2 {
		t.Fatalf("chooser options = %d, want 2 (Discard/SacNonland)", len(d.Options))
	}
	return d
}

// optionWithLabel returns the index of the option whose label contains sub.
func optionWithLabel(t *testing.T, d *decision.Decision, sub string) int {
	t.Helper()
	for _, o := range d.Options {
		if strings.Contains(strings.ToLower(o.Label), sub) {
			return o.Index
		}
	}
	t.Fatalf("no option matching %q in %+v", sub, d.Options)
	return -1
}

// unlessPayAsk returns the pending unless_pay ask, draining any priority in
// between.
func unlessPayAsk(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	d := passUntilAskKind(t, e, decision.KModes, 40)
	if d.ResumeKind != "unless_pay" {
		t.Fatalf("pending ask ResumeKind = %q, want unless_pay (%+v)", d.ResumeKind, d)
	}
	return d
}

// settleFirstPicks drains the rest of the resolution, answering any
// mid-resolution pick (the Discard payment's card choice) with its first
// offered option, so the pay branch completes deterministically.
func settleFirstPicks(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil || len(e.G.Stack) == 0 {
			return
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KChoose, decision.KTarget, decision.KModes:
			if len(d.Options) == 0 {
				t.Fatalf("mid-resolution %s ask with no options: %+v", d.Kind, d)
			}
			submitChoices(t, e, d.Options[0].Index)
		default:
			t.Fatalf("unexpected decision %+v while settling the payment", d)
		}
	}
	t.Fatalf("resolution did not settle within %d steps", limit)
}

// TestPerforatingArtistDiscardPaysNoLifeLost is the pay branch: seat 1
// chooses the discard option and pays Discard<1/Card>, so its own 3-life
// UnlessPayer$ body is satisfied and neither player loses life. The
// unless_pay ask being posed to seat 1 (not the controller) is the
// TempRemember$ pin.
func TestPerforatingArtistDiscardPaysNoLifeLost(t *testing.T) {
	t.Parallel()
	e, cfg, artistObj := perforatingArtistBoard(t)
	d := artistTriggerAsk(t, e, artistObj)

	// Choose the Discard option (the unless cost is Discard<1/Card>, payable
	// from seat 1's stocked hand).
	submitChoices(t, e, optionWithLabel(t, d, "discard"))

	d = unlessPayAsk(t, e)
	if d.Player != 1 {
		t.Fatalf("unless_pay payer = seat %d, want the chooser seat 1 (Remembered must be the opponent)", d.Player)
	}
	// PRECONDITION: a real pay option exists (seat 1 has a card to discard).
	if len(d.Options) < 2 {
		t.Fatalf("unless ask options = %+v, want a Pay option (seat 1 has a hand card)", d.Options)
	}
	life0 := e.G.Players[0].Life
	life1 := e.G.Players[1].Life
	hand1 := len(e.G.Zone(state.ZHand, 1))

	pay := -1
	for _, o := range d.Options {
		if o.Mode == decision.ModeUnlessPay {
			pay = o.Index
		}
	}
	if pay < 0 {
		t.Fatalf("unless ask has no ModeUnlessPay option: %+v", d.Options)
	}
	submitChoices(t, e, pay)
	settleFirstPicks(t, e, 60)

	if got := e.G.Players[1].Life; got != life1 {
		t.Fatalf("seat 1 life = %d, want %d (paying the discard spares the 3 life)", got, life1)
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != hand1-1 {
		t.Fatalf("seat 1 hand = %d, want %d (the Discard<1/Card> payment discarded exactly one)", got, hand1-1)
	}
	if got := e.G.Players[0].Life; got != life0 {
		t.Fatalf("controller life = %d, want %d (never a payer, never a chooser)", got, life0)
	}
	// No seat-0 ModeChosen may exist: the controller must never be a chooser.
	for _, ev := range e.L.Events {
		if ev.Kind == events.ModeChosen && ev.Player == 0 {
			t.Fatalf("ModeChosen by the controller: the opponent must be the only chooser/payer")
		}
	}
	replayCheck(t, e, cfg)
}

// TestPerforatingArtistSacrificeDeclinedLosesThree is the decline branch:
// seat 1 chooses the sacrifice option and refuses the Sac<...> payment, so
// its own LoseLife body deals 3 to seat 1 -- and to nobody else.
func TestPerforatingArtistSacrificeDeclinedLosesThree(t *testing.T) {
	t.Parallel()
	e, cfg, artistObj := perforatingArtistBoard(t)
	d := artistTriggerAsk(t, e, artistObj)

	submitChoices(t, e, optionWithLabel(t, d, "sacrifice"))

	d = unlessPayAsk(t, e)
	if d.Player != 1 {
		t.Fatalf("unless_pay payer = seat %d, want the chooser seat 1 (Remembered must be the opponent)", d.Player)
	}
	life0 := e.G.Players[0].Life
	life1 := e.G.Players[1].Life
	decline := d.Options[len(d.Options)-1].Index
	submitChoices(t, e, decline)
	settleFirstPicks(t, e, 60)

	if got := e.G.Players[1].Life; got != life1-3 {
		t.Fatalf("seat 1 life = %d, want %d (declining the sacrifice loses 3)", got, life1-3)
	}
	if got := e.G.Players[0].Life; got != life0 {
		t.Fatalf("controller life = %d, want %d (the LoseLife body names the Remembered chooser, not the Artist's controller)", got, life0)
	}
	replayCheck(t, e, cfg)
}

// TestPerforatingArtistNoPayableChoiceRunsFallback pins Forge's
// ChooseGenericEffect fallback arm: when the chooser can pay NEITHER
// UnlessCost$ (no hand card to discard, no nonland permanent to sacrifice),
// the GenericChoice is never posed and FallbackAbility$ -- the bare
// LoseLife 3 -- runs against the Remembered chooser. This is also the
// FallbackAbility$ read: before the fix the engine asked anyway and the
// opponent could pick a choice it could not pay.
func TestPerforatingArtistNoPayableChoiceRunsFallback(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	artist := mustCorpusCard(t, reg, "Perforating Artist")
	cfg := seatZeroStart(Config{Seed: 312, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append([]*cards.Card{artist}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
		}})
	e := New(cfg)
	e.Advance()
	artistObj := placeInDeck(t, e, 0, artist, state.ZBattlefield)
	if z := e.G.Obj(artistObj).Zone; z != state.ZBattlefield {
		t.Fatalf("precondition: Artist zone = %s, want Battlefield", z)
	}

	raidToEndStep(t, e, artistObj)
	// Empty seat 1's hand now -- after the drive (whose draw steps refilled
	// it) and before the trigger's choice resolves -- so BOTH unless costs
	// are unpayable. The MoveZone events are logged, so the replay sees the
	// same board.
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZHand, 1)...) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
	}
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZBattlefield, 1)...) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	}
	if n := len(e.G.Zone(state.ZHand, 1)) + len(e.G.Zone(state.ZBattlefield, 1)); n != 0 {
		t.Fatalf("precondition: seat 1 has %d cards in hand+battlefield, want 0 so neither unless cost is payable", n)
	}
	life0 := e.G.Players[0].Life
	life1 := e.G.Players[1].Life

	// Drain the trigger: no KModes ask to seat 1 may appear -- the fallback
	// resolves it directly. Answer only priority.
	sawModes := false
	for i := 0; i < 60; i++ {
		d := e.Pending()
		if d == nil || (len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0) {
			break
		}
		if d.Kind == decision.KModes {
			sawModes = true
			break
		}
		if d.Kind == decision.KPriority {
			submitPass(t, e)
			continue
		}
		t.Fatalf("unexpected decision %+v while draining the fallback", d)
	}
	if sawModes {
		t.Fatalf("seat 1 was asked to choose even though NO choice was payable; FallbackAbility$ must run instead")
	}
	passUntilStackEmpty(t, e, 60)

	if got := e.G.Players[1].Life; got != life1-3 {
		t.Fatalf("seat 1 life = %d, want %d (the FallbackAbility lost 3 for the Remembered chooser)", got, life1-3)
	}
	if got := e.G.Players[0].Life; got != life0 {
		t.Fatalf("controller life = %d, want %d (the fallback names the chooser, never the controller)", got, life0)
	}
	losses := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.LifeChange && ev.Player == 1 && ev.Amount == -3 {
			losses++
		}
	}
	if losses != 1 {
		t.Fatalf("seat 1 -3 life events = %d, want exactly 1 (one fallback body)", losses)
	}
	replayCheck(t, e, cfg)
}
