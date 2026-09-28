package rules

// Strive reversal (cardfuzz batch2 lines 19/22/30/41/45, batch3 1/6/7/10/15/
// 19/22): a bot fired a Strive spell (Phalanx Formation, Setessan Tactics,
// Silence the Believers ...) at every legal creature, the extra-target
// payments were unpayable, and the cast reversed (CR 733.1). The target
// choice had already queued a "becomes the target of a spell" trigger
// (Giggling Skitterspike, Ward, Silverfur Partisan), which the reversal left
// on the queue; its push was a state change, so it cleared the F05-2
// no-progress suppression and the identical cast was re-offered forever --
// each attempt dealing damage / minting a Wolf. These tests pin the engine
// half: a reversed cast queues nothing (CR 733.1 "No abilities trigger ...
// as a result of an undone action"), the second identical reversal holds the
// cast out, and the target ask carries the affordable-count hint the bot
// uses (Decision.AffordableTargets).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// striveTriggerFixture puts Phalanx Formation (K:Strive:1 W, "any number of
// target creatures") in seat 0's hand and Giggling Skitterspike plus two
// vanilla Raiders on seat 0's battlefield at main-phase priority, with no
// mana source on the battlefield (so only the pool pays).
func striveTriggerFixture(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID, []state.ObjID) {
	t.Helper()
	phalanx := choiceCorpusCard(t, "Phalanx Formation")
	spike := choiceCorpusCard(t, "Giggling Skitterspike")
	deck := []*cards.Card{phalanx, spike, card(t, gateRaiderSrc), card(t, gateRaiderSrc)}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append(deck, mountainDeck(t, 40-len(deck))...),
			mountainDeck(t, 40),
		},
		Tokens: map[string]*cards.Card{},
	})
	e := New(cfg)
	e.Advance()
	pf := findInZones(t, e, 0, "Phalanx Formation")
	if inZone(e, state.ZLibrary, 0, pf) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: pf, From: state.ZLibrary, To: state.ZHand})
		e.pending = nil
		e.Advance()
	}
	toMain1(t, e)
	sp := gateMoveFromLibrary(t, e, "Giggling Skitterspike", state.ZBattlefield)
	r1 := gateMoveFromLibrary(t, e, "Raider", state.ZBattlefield)
	r2 := gateMoveFromLibrary(t, e, "Raider", state.ZBattlefield)
	// Seat 0's opening hand may hold Mountains; clear the battlefield of any
	// untapped mana source so the reversal is decided by the pool alone.
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Mountain" {
			t.Fatalf("fixture expects no Mountain on seat 0's battlefield, found %d", id)
		}
	}
	return e, cfg, pf, sp, []state.ObjID{r1, r2}
}

func skitterspikeTriggerPushes(e *Engine, spike state.ObjID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == spike {
			n++
		}
	}
	return n
}

// TestReversedStriveCastQueuesNoBecomesTargetTrigger: with only the base
// {2}{W} in the pool, choosing Skitterspike and a Raider prices {3}{W}{W};
// the cast reverses and Skitterspike's "becomes the target of a spell"
// trigger must NOT be queued or pushed (CR 733.1), so the opponent takes no
// damage. The second identical reversal then holds the cast out of the
// window (F05-2), which the leaked trigger's push used to defeat.
func TestReversedStriveCastQueuesNoBecomesTargetTrigger(t *testing.T) {
	t.Parallel()
	e, cfg, pf, spike, raiders := striveTriggerFixture(t, 951)
	addMana(t, e, 0, "WWW")
	oppLife := e.G.Players[1].Life

	for attempt := 1; attempt <= 2; attempt++ {
		submitChoices(t, e, striveCastOption(t, e, pf).Index)
		d := e.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("attempt %d: expected the Strive target ask, got %+v", attempt, d)
		}
		if d.AffordableTargets != 1 {
			t.Fatalf("attempt %d: AffordableTargets = %d, want 1 (only the base {2}{W} is payable)", attempt, d.AffordableTargets)
		}
		chooseStriveTargets(t, e, spike, raiders[0])
		if o := e.G.Obj(pf); o.Zone != state.ZHand {
			t.Fatalf("attempt %d: reversed cast left Phalanx Formation in %v, want hand", attempt, o.Zone)
		}
		if n := len(e.pendingTriggers); n != 0 {
			t.Fatalf("attempt %d: reversed cast left %d queued trigger(s)", attempt, n)
		}
		if n := skitterspikeTriggerPushes(e, spike); n != 0 {
			t.Fatalf("attempt %d: Skitterspike's becomes-target trigger was pushed %d time(s) for a reversed cast", attempt, n)
		}
		if got := e.G.Players[1].Life; got != oppLife {
			t.Fatalf("attempt %d: opponent life %d, want untouched %d", attempt, got, oppLife)
		}
		if got := e.G.Players[0].Pool.Total(); got != 3 {
			t.Fatalf("attempt %d: pool %d after the reversal, want the untouched 3", attempt, got)
		}
	}
	for _, o := range castOptions(t, e) {
		if o.Obj == pf {
			t.Fatalf("Phalanx Formation still offered after two identical no-progress reversals: %+v", o)
		}
	}
	replayCheck(t, e, cfg)
}

// TestStriveCastWithAffordableTargetsFiresTheTrigger is the positive
// control: at the affordable count (two targets, {3}{W}{W} from five white)
// the cast completes and Skitterspike's trigger fires exactly once, so the
// CR 733.1 drop is scoped to reversed proposals only.
func TestStriveCastWithAffordableTargetsFiresTheTrigger(t *testing.T) {
	t.Parallel()
	e, cfg, pf, spike, raiders := striveTriggerFixture(t, 952)
	addMana(t, e, 0, "WWWWW")
	oppLife := e.G.Players[1].Life

	submitChoices(t, e, striveCastOption(t, e, pf).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the Strive target ask, got %+v", d)
	}
	// Three creatures are legal; {2}{W} + two {1}{W} = seven exceeds five, so
	// the hint is two -- the count the pool can actually pay.
	if d.AffordableTargets != 2 {
		t.Fatalf("AffordableTargets = %d, want 2 (five white pays base + one strive)", d.AffordableTargets)
	}
	if d.Max < 3 {
		t.Fatalf("the hint must not narrow Max (CR 601.2c any number): Max = %d", d.Max)
	}
	chooseStriveTargets(t, e, spike, raiders[0])
	if o := e.G.Obj(pf); o.Zone != state.ZStack {
		t.Fatalf("affordable two-target cast: Phalanx Formation zone %v, want stack", o.Zone)
	}
	passUntilStackEmpty(t, e, 20)
	if n := skitterspikeTriggerPushes(e, spike); n != 1 {
		t.Fatalf("Skitterspike's becomes-target trigger pushed %d time(s), want 1", n)
	}
	if got := e.G.Players[1].Life; got != oppLife-1 {
		t.Fatalf("opponent life %d, want %d (Skitterspike deals its power 1)", got, oppLife-1)
	}
	replayCheck(t, e, cfg)
}
