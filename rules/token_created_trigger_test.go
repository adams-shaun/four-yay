package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The "whenever you create a token" trigger class (trig:TokenCreated /
// trig:TokenCreatedOnce), pinned end to end on real corpus cards: Mirkwood
// Bats, Rosie Cotton of South Lane, Voldaren Bloodcaster and Akim, the
// Soaring Wind. The token creator is the same authored fixture artifact the
// token-replacement tests use (an AB$ Token ability whose TokenScript$ names
// a real corpus token script), so every mint rides the engine's own effToken
// emit path -- the TokenCreate event the matcher reads is the real one.

// untapUntaps emits a real Untap event on id so a Cost$ T maker can activate
// again the same turn.
func untapUntaps(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	e.emit(events.Event{Kind: events.Untap, Obj: id})
}

// passUntilDecision passes priority decisions until a decision of kind kind
// is pending (a mid-resolution ask) -- for a trigger whose Execute$ poses one
// -- or the stack is empty.
func passUntilDecision(t *testing.T, e *Engine, kind decision.Kind) *decision.Decision {
	t.Helper()
	for i := 0; i < 40 && !e.G.Over; i++ {
		if d := e.Pending(); d != nil {
			if d.Kind == kind {
				return d
			}
			if d.Kind != decision.KPriority {
				t.Fatalf("non-priority, non-%s decision while driving: %+v", kind, d)
			}
		} else if len(e.G.Stack) == 0 {
			t.Fatalf("stack emptied before a %s decision appeared", kind)
		}
		idx := -1
		d := e.Pending()
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	t.Fatalf("did not reach a %s decision within the pass budget", kind)
	return nil
}

// indexOfObjOption returns the option index targeting object id, or -1.
func indexOfObjOption(d *decision.Decision, id state.ObjID) int {
	for _, o := range d.Options {
		if o.Obj == id {
			return o.Index
		}
	}
	return -1
}

// TestTokenCreatedMirkwoodBatsLosesLifePerToken is the filing card: one Food
// token mint -> each opponent loses exactly 1 life; a second mint in the same
// turn loses 1 more (per-token firing, NOT once-per-turn). Before the fix the
// trigger never fired and no life moved.
func TestTokenCreatedMirkwoodBatsLosesLifePerToken(t *testing.T) {
	t.Parallel()
	bats := tokenReplCorpusCard(t, "Mirkwood Bats")
	maker := cardByName(t, tokenForgeSrc("c_a_food_sac"))
	e, cfg := tokenReplGame(t, 41, bats, maker)
	moveSeededCard(t, e, 0, bats, state.ZBattlefield)
	makerID := moveSeededCard(t, e, 0, maker, state.ZBattlefield)
	before := e.G.Players[1].Life

	activateTokenForge(t, e, makerID)
	if got := e.G.Players[1].Life; got != before-1 {
		t.Fatalf("after the first Food token seat 1 life = %d, want %d (one loss per token)", got, before-1)
	}

	untapUntaps(t, e, makerID)
	activateTokenForge(t, e, makerID)
	if got := e.G.Players[1].Life; got != before-2 {
		t.Fatalf("after the second Food token seat 1 life = %d, want %d (the trigger fires per token, not once per turn)", got, before-2)
	}
	replayCheck(t, e, cfg)
}

// answerTargetAsksWith passes priority (draining the stack) and answers every
// KTarget ask with the option targeting opt, returning how many asks it
// answered -- for Rosie's per-mint counter asks (her own ETB Food and the
// maker's Food each fire the trigger). forbidden must be absent from every
// target option list.
func answerTargetAsksWith(t *testing.T, e *Engine, opt, forbidden state.ObjID) int {
	t.Helper()
	answered := 0
	for i := 0; i < 60; i++ {
		d := e.Pending()
		if d != nil && d.Kind == decision.KTarget {
			if idx := indexOfObjOption(d, opt); idx < 0 {
				t.Fatalf("target ask does not offer obj %d: %+v", opt, d.Options)
			} else if forbiddenIdx := indexOfObjOption(d, forbidden); forbiddenIdx >= 0 {
				t.Fatalf("target ask illegally offers excluded obj %d at option %d: %+v", forbidden, forbiddenIdx, d.Options)
			} else {
				submitChoices(t, e, idx)
				answered++
			}
			continue
		}
		if len(e.G.Stack) == 0 {
			return answered // drain finished (the pending decision, if any, is not ours)
		}
		if d == nil {
			t.Fatalf("no decision while the stack is draining (depth %d)", len(e.G.Stack))
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision while draining: %+v", d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	t.Fatal("drain budget exhausted")
	return answered
}

// TestTokenCreatedRosieCottonAsksCounterTarget: each Food mint (Rosie's own
// ETB Food plus the maker's) fires Rosie's trigger, whose DB$ PutCounter
// poses the real target ask (ValidTgts$ Creature.YouCtrl+Other), and each
// answered choice puts a +1/+1 counter on a creature other than Rosie.
func TestTokenCreatedRosieCottonAsksCounterTarget(t *testing.T) {
	t.Parallel()
	rosie := tokenReplCorpusCard(t, "Rosie Cotton of South Lane")
	maker := cardByName(t, tokenForgeSrc("c_a_food_sac"))
	bear := card(t, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg := tokenReplGame(t, 41, rosie, maker, bear)
	rosieID := moveSeededCard(t, e, 0, rosie, state.ZBattlefield)
	makerID := moveSeededCard(t, e, 0, maker, state.ZBattlefield)
	bearID := moveSeededCard(t, e, 0, bear, state.ZBattlefield)
	if rosieID == bearID {
		t.Fatalf("Rosie and Bear unexpectedly share object id %d", rosieID)
	}
	if o := e.G.Obj(rosieID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Rosie is not on the battlefield: %+v", o)
	}
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Bear is not on the battlefield: %+v", o)
	}

	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, makerID, 0).Index)
	answered := answerTargetAsksWith(t, e, bearID, rosieID)
	mints := countTokensNamedOnSeat(t, e, 0, "Food Token")
	if mints != 2 {
		t.Fatalf("Food Tokens on the battlefield = %d, want 2 (Rosie's ETB + the maker's)", mints)
	}
	if answered != mints {
		t.Fatalf("the trigger posed %d counter asks for %d Food mints", answered, mints)
	}
	if got := e.G.Obj(bearID).Counter("P1P1"); got != int32(mints) {
		t.Fatalf("Bear +1/+1 counters = %d, want %d (one per mint)", got, mints)
	}
	if got := e.G.Obj(rosieID).Counter("P1P1"); got != 0 {
		t.Fatalf("Rosie took a counter: %d", got)
	}
	replayCheck(t, e, cfg)
}

// TestTokenCreatedOnceFiresOnTheFirstMintOfEachTurn pins the Once mode's
// once-per-turn latch: Akim's Bird token is created on the FIRST token mint
// of the turn, a second mint the same turn queues nothing, and the next
// turn's first mint fires again. (Akim's TrigToken creates a Bird token --
// itself a TokenCreate event -- so without the latch the mode would
// self-feed forever.)
func TestTokenCreatedOnceFiresOnTheFirstMintOfEachTurn(t *testing.T) {
	t.Parallel()
	akim := tokenReplCorpusCard(t, "Akim, the Soaring Wind")
	maker := cardByName(t, tokenForgeSrc("c_a_food_sac"))
	e, cfg := tokenReplGame(t, 41, akim, maker)
	moveSeededCard(t, e, 0, akim, state.ZBattlefield)
	makerID := moveSeededCard(t, e, 0, maker, state.ZBattlefield)

	activateTokenForge(t, e, makerID) // the turn's first mint -> one Bird
	if got := countTokensNamedOnSeat(t, e, 0, "Bird Token"); got != 1 {
		t.Fatalf("after the first mint Bird Tokens = %d, want 1", got)
	}

	untapUntaps(t, e, makerID)
	activateTokenForge(t, e, makerID) // a second mint the same turn -> latched
	if got := countTokensNamedOnSeat(t, e, 0, "Bird Token"); got != 1 {
		t.Fatalf("the Once mode fired on the turn's second mint: Bird Tokens = %d, want 1", got)
	}

	// Next seat-0 turn: the latch resets and the first mint fires again.
	driveToStep(t, e, e.G.Turn+2, 0, state.StepMain1)
	addMana(t, e, 0, "")
	activateTokenForge(t, e, makerID)
	if got := countTokensNamedOnSeat(t, e, 0, "Bird Token"); got != 2 {
		t.Fatalf("after next turn's first mint Bird Tokens = %d, want 2", got)
	}
	replayCheck(t, e, cfg)
}

// TestTokenCreatedVoldarenBloodcasterFiveBloodGate pins the condition gate:
// Voldaren's IsPresent$ Blood.token+YouCtrl | PresentCompare$ GE5 is
// evaluated at trigger time (the mint has already landed), so four Blood
// tokens queue nothing and the FIFTH transforms CARDNAME.
func TestTokenCreatedVoldarenBloodcasterFiveBloodGate(t *testing.T) {
	t.Parallel()
	vol := tokenReplCorpusCard(t, "Voldaren Bloodcaster")
	maker := cardByName(t, tokenForgeSrc("c_a_blood_draw"))
	e, cfg := tokenReplGame(t, 41, vol, maker)
	volID := moveSeededCard(t, e, 0, vol, state.ZBattlefield)
	makerID := moveSeededCard(t, e, 0, maker, state.ZBattlefield)

	for i := 0; i < 4; i++ {
		activateTokenForge(t, e, makerID)
		if got := countTokensNamedOnSeat(t, e, 0, "Blood Token"); got != i+1 {
			t.Fatalf("Blood Tokens = %d, want %d", got, i+1)
		}
		if name := e.G.Obj(volID).Face().Name; name != "Voldaren Bloodcaster" {
			t.Fatalf("with %d Blood tokens CARDNAME transformed early to %q", i+1, name)
		}
		untapUntaps(t, e, makerID)
	}

	activateTokenForge(t, e, makerID) // the fifth Blood token
	if got := countTokensNamedOnSeat(t, e, 0, "Blood Token"); got != 5 {
		t.Fatalf("Blood Tokens = %d, want 5", got)
	}
	if name := e.G.Obj(volID).Face().Name; name != "Bloodbat Summoner" {
		t.Fatalf("with 5 Blood tokens CARDNAME did not transform (face = %q)", name)
	}
	replayCheck(t, e, cfg)
}
