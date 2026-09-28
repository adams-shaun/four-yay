// fb_softlock_reverify_test.go — re-verification of the reported
// fb-20260914T220605Z-0aadcdb8 soft-lock ("boomerang -> valkavoth = game
// lock") after the class fix in inbox-engine-empty-choose-softlock merged.
// The class fix (effects.Ask's shared empty-answer guard, backed by
// Engine.ask's boundary tripwire) resolves a decision whose only legal
// answer is the empty one silently; these tests re-run the report's own
// reproductions against the merged tree.
//
// TestFBSoftlockSearchWithNoEligibleResolvesSilently is the triage test's
// exact scenario (rules: TestTriageSearchNoEligiblePosesEmptyChoose, which
// reproduced the wedge on the pre-fix tree): a ChangeZone | Origin$ Library
// sorcery whose ChangeType$ matches nothing in the library. Before the fix
// it posted a pending `KChoose Min:0 Max:0 Options:[]`; after it the search
// resolves silently -- one shuffle, nothing found, CR 701.23b fail-to-find
// -- and priority returns.
//
// TestFBSoftlockKeenVsValgavothPlays plays the reported deck pair (the two
// commander decks the demo game g3 used: foundations-keen-engineering with
// its Boomerang, valgavoth-endless-punishment with its tutors and
// fetchlands -- the decks whose hidden-library searches can hit an empty
// eligible set) to completion with the boundary tripwire live: a zero-option
// choose posted anywhere in the game panics in Engine.ask, so a finished,
// byte-identically replaying game is proof the deck pair cannot wedge.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const fbSoftlockSearchSrc = "Name:Dragon Hunt\nManaCost:1 U\nTypes:Sorcery\n" +
	"A:SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Card.Dragon | ChangeNum$ 1 | SpellDescription$ Search your library for a Dragon card.\n"

// fbDrivePastSearch drives the game forward, answering every priority
// window with pass and every other (legitimate, non-empty) ask with option
// 0, until turn 3 (the cast turn's and the next turn's cleanup are behind
// it) or the game ends. The ONE assertion that is the bug lives inline: a
// pending empty-answer-only decision -- the pre-fix wedge's exact shape --
// is a failure; the boundary tripwire in Engine.ask also panics on it.
func fbDrivePastSearch(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 400 && !e.G.Over && e.G.Turn < 3; i++ {
		d := e.Pending()
		if d == nil {
			return
		}
		if d.Min == 0 && (d.Max == 0 || len(d.Options) == 0) {
			t.Fatalf("the empty-answer-only wedge is back: %+v", d)
		}
		idx := 0
		if d.Kind == decision.KPriority {
			idx = -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit answer to %+v: %v", d, err)
		}
	}
}

func TestFBSoftlockSearchWithNoEligibleResolvesSilently(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	hunt := card(t, fbSoftlockSearchSrc)
	// Seat 0's deck: the Dragon-search sorcery over Mountains -- deliberately
	// NO Dragon anywhere in the library, so the search's eligible set is
	// empty and the pre-fix tree posted the Min 0 / Max 0 wedge.
	hd := append([]*cards.Card{hunt}, mountainDeck(t, 39)...)
	fd := mountainDeck(t, 40)
	cfg := seatZeroStart(func(s uint64) Config {
		return Config{Seed: s, Names: []string{"a", "b"},
			Decks:  [][]*cards.Card{hd, fd},
			Tokens: reg.Tokens}
	}(77))
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	id := searchMoveByName(t, e, "Dragon Hunt", state.ZHand)
	addMana(t, e, 0, "1U")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", id, d.Options)
	}
	start := len(e.L.Events)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}

	// Both seats pass priority; the spell resolves. On the pre-fix tree the
	// wedge pended `Kind:choose Min:0 Max:0 Options:[]` right here and the
	// seat could never answer it; fbDrivePastSearch fails on that shape and
	// otherwise answers every real ask until the game has moved on.
	fbDrivePastSearch(t, e)

	if e.G.Over {
		t.Fatal("game ended; the failed search should hand play back to the ordinary turn flow")
	}
	// The resolution's physical trace: exactly one seat-0 shuffle (the
	// search's unconditional shuffle -- a fail-to-find still shuffles,
	// CR 701.23b) and NO card moved into hand.
	shuffles, moves := 0, 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			shuffles++
		}
		if ev.Kind == events.MoveZone && ev.To == state.ZHand && ev.Player == 0 {
			moves++
		}
	}
	if shuffles != 1 {
		t.Fatalf("seat-0 shuffles = %d, want exactly 1 (the fail-to-find still shuffles): %+v", shuffles, e.L.Events[start:])
	}
	if moves != 0 {
		t.Fatalf("%d card(s) moved to hand on a fail-to-find: %+v", moves, e.L.Events[start:])
	}
}

// TestFBSoftlockKeenVsValgavothPlays is the reported deck pair played to
// completion. The boundary tripwire (Engine.ask panics on an
// empty-answer-only decision) is live for the whole game, so finishing is
// itself the assertion that no zero-option choose is ever posted; the
// byte-identical replay pins determinism.
func TestFBSoftlockKeenVsValgavothPlays(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	hero := testutil.RepoDeck(t, reg, "foundations-keen-engineering")
	foe := testutil.RepoDeck(t, reg, "valgavoth-endless-punishment")
	build := func(s uint64) Config {
		return Config{Seed: s, Names: []string{"keen", "valgavoth"},
			Decks:  [][]*cards.Card{hero, foe},
			Tokens: reg.Tokens,
			// R-M1: mulligans on, as the acceptance games run them, so the
			// keep/mulligan and bottoming round is exercised too.
			Mulligans: 1}
	}
	cfg := seatZeroStart(build(42))
	e := New(cfg)
	b := newTestBot(7)
	e.Advance()
	n := 0
	for !e.G.Over && e.Pending() != nil && n < 400000 {
		if err := e.Submit(b.answer(e, e.Pending())); err != nil {
			t.Fatalf("intent %d: %v", n, err)
		}
		n++
	}
	if !e.G.Over {
		t.Fatalf("the reported deck pair did not finish (turn %d, %d intents) -- the wedge may be back", e.G.Turn, n)
	}
	re, err := replayFor(cfg, e.L)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if re.L.Head() != e.L.Head() {
		t.Fatalf("chain %s, replay %s", e.L.Head(), re.L.Head())
	}
	t.Logf("keen-vs-valgavoth finished: %d intents, turn %d, head %s", n, e.G.Turn, e.L.Head())
}
