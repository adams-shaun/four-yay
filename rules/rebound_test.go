package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This file pins keyword Rebound end to end (CR 702.95):
//
//   - a spell cast from its controller's HAND is exiled as it resolves and
//     leaves a delayed promise to recast it at that player's next upkeep;
//   - the delayed trigger offers the card from EXILE for free, optionally,
//     and a decline leaves it exiled;
//   - the re-bound cast from exile does NOT rebound again (it resolves to
//     the graveyard and registers nothing);
//   - every step is event-sourced, so the log replays byte-identically.
//
// The corpus carrier Terramorph is exercised alongside an inline Rebound
// spell (never a committed .cards script -- GPL-3.0).

// reboundBoltSrc is an inline Rebound instant whose effect is observable
// (a life gain). The name is deliberately not a corpus card's.
const reboundBoltSrc = "Name:Test Rebound Bolt\nManaCost:R\nTypes:Instant\n" +
	"A:SP$ GainLife | LifeAmount$ 3\n" +
	"K:Rebound\n" +
	"Oracle:Gain 3 life. Rebound.\n"

// reboundPlainSrc is an inline NON-Rebound instant used to prove the control
// case: an ordinary instant still goes to the graveyard and leaves no promise.
const reboundPlainSrc = "Name:Test Plain Bolt\nManaCost:R\nTypes:Instant\n" +
	"A:SP$ GainLife | LifeAmount$ 3\n" +
	"Oracle:Gain 3 life.\n"

// reboundRegistrations counts the live delayed registrations whose Execute is
// the Rebound builtin -- the engine-side precondition that the promise was
// really created (a no-op assertion cannot pass with the feature absent).
func reboundRegistrations(e *Engine) int {
	n := 0
	for _, dt := range e.G.Delayed {
		if dt.Execute == "__kwReboundCast" {
			n++
		}
	}
	return n
}

// reboundCastInfoFlags returns the CastFlags word of the most recent CastInfo
// for id (the pay-time provenance stamp).
func reboundCastInfoFlags(e *Engine, id state.ObjID) uint64 {
	var flags uint64
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == id {
			flags = events.FlagsFrom(ev.Counter)
		}
	}
	return flags
}

// tappedLandCount counts seat p's tapped lands -- the observable proof that
// a cast paid mana (a free cast taps nothing).
func tappedLandCount(e *Engine, p state.PlayerID) int {
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Tapped && o.Face() != nil && o.Face().IsLand() {
			n++
		}
	}
	return n
}

// driveToReboundAsk advances until the pending decision is the Rebound
// delayed trigger's optional free-cast ask (a "play" KModes), answering only
// priorities, combat declarations and cleanup/simple chooses on the way.
func driveToReboundAsk(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 8000; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			t.Fatal("no decision pending while driving to the rebound ask")
		}
		if d.Kind == decision.KModes && d.ResumeKind == "play" {
			return d
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		case decision.KChoose:
			// A cleanup-step discard or another bounded choose crossed on the
			// way: answer the first option and keep driving.
			if len(d.Options) == 0 {
				t.Fatalf("KChoose with no options: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		default:
			t.Fatalf("unexpected decision %+v while driving to the rebound ask", d)
		}
	}
	t.Fatal("never reached the rebound play ask within the budget")
	return nil
}

// reboundEngine builds the fixture game and returns the Rebound card's id.
func reboundEngine(t *testing.T, seed uint64, src string) (*Engine, Config, state.ObjID) {
	t.Helper()
	return newFixtureDeck(t, seed, src)
}

// TestReboundHandCastExilesAndOffersFreeRecast is the main CR 702.95 leg: a
// hand cast is exiled on resolution, leaves the delayed promise, and at the
// controller's next upkeep offers the card from exile for free; the re-bound
// cast does not rebound again.
func TestReboundHandCastExilesAndOffersFreeRecast(t *testing.T) {
	t.Parallel()
	e, cfg, id := reboundEngine(t, 71, reboundBoltSrc)

	// Precondition: the card starts in hand, so "cast from hand" is the rule
	// this test actually exercises.
	if got := e.G.Obj(id).Zone; got != state.ZHand {
		t.Fatalf("precondition: Rebound card zone = %s, want hand", got)
	}
	addMana(t, e, 0, "R")
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	passUntilStackEmpty(t, e, 20)

	// The cast resolved (life actually changed) and was stamped FlagRebound.
	if flags := reboundCastInfoFlags(e, id); flags&state.FlagRebound == 0 {
		t.Fatalf("hand-cast CastInfo flags = %#x, want FlagRebound set (zone now %s)", flags, e.G.Obj(id).Zone)
	}
	if e.G.Players[0].Life != 23 {
		t.Fatalf("cast did not resolve: life = %d, want 23", e.G.Players[0].Life)
	}
	// CR 702.95a: exile on resolution, not the graveyard.
	if got := e.G.Obj(id).Zone; got != state.ZExile {
		t.Fatalf("resolved Rebound spell zone = %s, want exile", got)
	}
	// The promise is real and live.
	if n := reboundRegistrations(e); n != 1 {
		t.Fatalf("rebound registrations after resolution = %d, want 1", n)
	}
	replayCheck(t, e, cfg)

	// Seat 0's next upkeep is turn 3 in this two-seat game (turn 1 = seat 0,
	// turn 2 = seat 1). Drive there and let the delayed trigger resolve.
	d := driveToReboundAsk(t, e)
	// Precondition the ask depends on: the card is in exile and the ask
	// offers exactly that card.
	if got := e.G.Obj(id).Zone; got != state.ZExile {
		t.Fatalf("precondition before the ask: card zone = %s, want exile", got)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != id {
		t.Fatalf("rebound ask options = %+v, want exactly card %d", d.Options, id)
	}
	// Take the free cast. The deck is all Mountains, so a paid R cast would tap
	// one; a free cast taps none. Capture the tapped count to prove it.
	tappedBefore := tappedLandCount(e, 0)
	submitChoices(t, e, d.Options[0].Index)
	// Precondition: the cast actually began from exile (the rule's origin).
	if got := e.G.Obj(id).Zone; got != state.ZStack {
		t.Fatalf("re-bound cast did not reach the stack: zone = %s", got)
	}
	passUntilStackEmpty(t, e, 20)
	if got := tappedLandCount(e, 0); got != tappedBefore {
		t.Fatalf("re-bound cast tapped %d land(s), want 0 -- the recast must be free", got-tappedBefore)
	}

	// CR 702.95e: the re-bound cast does NOT rebound again -- it rests in the
	// graveyard and registered no new promise.
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("re-bound cast resting zone = %s, want graveyard (no repeat rebound)", got)
	}
	// The re-bound cast is flagless (a free Play from exile emits no
	// CastInfo at all), so the log holds exactly the hand cast's one.
	casts := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == id {
			casts++
			if events.FlagsFrom(ev.Counter)&state.FlagRebound == 0 {
				t.Fatalf("CastInfo %d for the card lacks FlagRebound: %q", casts, ev.Counter)
			}
		}
	}
	if casts != 1 {
		t.Fatalf("CastInfo events for the card = %d, want 1 (the re-bound cast adds none)", casts)
	}
	if n := reboundRegistrations(e); n != 0 {
		t.Fatalf("rebound registrations after the re-bound cast = %d, want 0", n)
	}
	replayCheck(t, e, cfg)
}

// TestReboundDeclineLeavesCardExiled proves the optional decline: answering
// the next-upkeep ask with the empty choice leaves the card exiled and grants
// no later permission.
func TestReboundDeclineLeavesCardExiled(t *testing.T) {
	t.Parallel()
	e, cfg, id := reboundEngine(t, 72, reboundBoltSrc)
	addMana(t, e, 0, "R")
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(id).Zone; got != state.ZExile {
		t.Fatalf("precondition: resolved Rebound spell zone = %s, want exile", got)
	}
	if n := reboundRegistrations(e); n != 1 {
		t.Fatalf("precondition: rebound registrations = %d, want 1", n)
	}

	d := driveToReboundAsk(t, e)
	if len(d.Options) == 0 {
		t.Fatalf("precondition: the rebound ask offered nothing to decline: %+v", d)
	}
	// Min 0 is the decline the Optional$ True Play allows.
	if d.Min != 0 {
		t.Fatalf("rebound ask Min = %d, want 0 (declinable)", d.Min)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
		t.Fatalf("decline the rebound ask: %v", err)
	}
	// The card stays exiled and the one-shot registration is gone, so no
	// stale permission survives into a later upkeep.
	if got := e.G.Obj(id).Zone; got != state.ZExile {
		t.Fatalf("after decline, card zone = %s, want exile", got)
	}
	if n := reboundRegistrations(e); n != 0 {
		t.Fatalf("after decline, rebound registrations = %d, want 0", n)
	}
	replayCheck(t, e, cfg)

	// Drive through the following turn's upkeep/end step: no ask must appear.
	for i := 0; i < 4000; i++ {
		if e.G.Over {
			break
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			continue
		}
		if d.Kind == decision.KModes && d.ResumeKind == "play" {
			t.Fatalf("a declined rebound offered a second free cast: %+v", d)
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		case decision.KChoose:
			if len(d.Options) == 0 {
				t.Fatalf("KChoose with no options: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		default:
			t.Fatalf("unexpected decision %+v after the decline", d)
		}
		if e.G.Turn >= 5 && e.G.Active == 0 && e.G.Step == state.StepUpkeep {
			break
		}
	}
	if got := e.G.Obj(id).Zone; got != state.ZExile {
		t.Fatalf("declined Rebound card left exile: zone = %s", got)
	}
}

// TestNonReboundSpellStaysInGraveyard is the control: an ordinary instant
// under the same driver rests in the graveyard, leaves no promise, and its
// CastInfo carries no FlagRebound -- so the positive tests above are not
// measuring a blanket exile.
func TestNonReboundSpellStaysInGraveyard(t *testing.T) {
	t.Parallel()
	e, _, id := reboundEngine(t, 73, reboundPlainSrc)
	addMana(t, e, 0, "R")
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	passUntilStackEmpty(t, e, 20)
	if e.G.Players[0].Life != 23 {
		t.Fatalf("control spell did not resolve: life = %d", e.G.Players[0].Life)
	}
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("non-Rebound instant zone = %s, want graveyard", got)
	}
	if flags := reboundCastInfoFlags(e, id); flags&state.FlagRebound != 0 {
		t.Fatalf("non-Rebound CastInfo flags = %#x, want FlagRebound clear", flags)
	}
	if n := reboundRegistrations(e); n != 0 {
		t.Fatalf("non-Rebound spell registrations = %d, want 0", n)
	}
}

// TestReboundCorpusCarrierExilesAndOffers pins the real corpus card
// Terramorph end to end: hand cast -> exile + promise -> free recast from
// exile -> graveyard, replaying byte-identically.
func TestReboundCorpusCarrierExilesAndOffers(t *testing.T) {
	t.Parallel()
	e, cfg, id, caster := corpusCardConfig(t, 74, "Terramorph")
	// Precondition: the real carrier is in hand and carries the keyword.
	if got := e.G.Obj(id).Zone; got != state.ZHand {
		t.Fatalf("precondition: Terramorph zone = %s, want hand", got)
	}
	if _, ok := e.G.Obj(id).Face().KeywordParam("Rebound"); !ok {
		t.Fatal("precondition: corpus Terramorph does not carry K:Rebound")
	}
	addMana(t, e, caster, "GGGG") // {3}{G}
	submitChoices(t, e, castOptionFor(t, e, id).Index)

	// Terramorph's library search may pose a choose; take the first offered
	// land, then drain.
	for i := 0; i < 20 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KChoose || d.Kind == decision.KTarget {
			if len(d.Options) == 0 {
				t.Fatalf("search ask with no options: %+v", d)
			}
			submitChoices(t, e, d.Options[0].Index)
			continue
		}
		if d.Kind == decision.KPriority {
			submitPass(t, e)
			continue
		}
		t.Fatalf("unexpected decision draining Terramorph: %+v", d)
	}
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(id).Zone; got != state.ZExile {
		t.Fatalf("resolved Terramorph zone = %s, want exile", got)
	}
	if n := reboundRegistrations(e); n != 1 {
		t.Fatalf("Terramorph rebound registrations = %d, want 1", n)
	}
	replayCheck(t, e, cfg)
}

// TestReboundCopiedSpellRegistersNothing is the cast-provenance leg (CR
// 702.95a with CR 707.10): a copy of the hand-cast Rebound spell is PUT on
// the stack, never cast, so the StackCopy mint must strip the provenance bit
// -- and the resolving copy leaves no delayed promise of its own.
func TestReboundCopiedSpellRegistersNothing(t *testing.T) {
	t.Parallel()
	e, cfg, id := reboundEngine(t, 75, reboundBoltSrc)
	addMana(t, e, 0, "R")
	submitChoices(t, e, castOptionFor(t, e, id).Index)

	// Precondition: the spell sits on the stack already carrying the pay-time
	// provenance, so the copy below really would inherit a set flag were the
	// mint not stripping the cast-provenance bits.
	if o := e.G.Obj(id); o.Zone != state.ZStack {
		t.Fatalf("setup: cast spell zone %s, want stack", o.Zone)
	}
	if e.G.Obj(id).CastFlags&state.FlagRebound == 0 {
		t.Fatal("setup: the hand cast stamped no FlagRebound on the stack object")
	}

	// Copy the spell on the stack (the effects/copy.go emission shape).
	before := state.ObjID(len(e.G.Objs))
	e.emit(events.Event{Kind: events.StackCopy, Obj: id, Player: 0})
	copyID := state.ObjID(len(e.G.Objs))
	if copyID != before+1 {
		t.Fatalf("setup: StackCopy minted %d objects, want 1", copyID-before)
	}
	if o := e.G.Obj(copyID); o == nil || !o.IsCopy || o.Zone != state.ZStack {
		t.Fatalf("setup: minted copy %+v, want a stack copy", o)
	}
	// The measured defect: the copy inherited the cast provenance, resolved
	// into exile and minted a second upkeep promise nobody earned.
	if e.G.Obj(copyID).CastFlags&state.FlagRebound != 0 {
		t.Fatal("a stack copy inherited FlagRebound; a copy is put on the stack, never cast (CR 707.10)")
	}

	passUntilStackEmpty(t, e, 20)

	// The copy really resolved (it gains the same 3 life) and the original was
	// really cast from hand: exiled, with its one promise.
	if e.G.Players[0].Life != 26 {
		t.Fatalf("life = %d, want 26 (both the cast spell and its copy resolved)", e.G.Players[0].Life)
	}
	if got := e.G.Obj(id).Zone; got != state.ZExile {
		t.Fatalf("resolved Rebound spell zone = %s, want exile", got)
	}
	if n := reboundRegistrations(e); n != 1 {
		t.Fatalf("rebound registrations after resolution = %d, want 1 (the copy registers none)", n)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedRegister && ev.Obj == copyID {
			t.Fatalf("a DelayedRegister names the never-cast copy %d: %+v", copyID, ev)
		}
	}
	replayCheck(t, e, cfg)
}
