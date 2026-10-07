package rules

import (
	"fmt"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// assertEveryPriorityOptionActs is the inert-action sweep
// (rules/priority_guard.go): on the pending priority decision, every offered
// option except pass and concede, submitted to a clone, must either be
// rejected by Submit (a loud error) or emit at least one event beyond the
// DecisionMade bookkeeping and the priority reset -- and never the inert
// backstop's Note, which only a disagreement between the offer walk and the
// handler can produce. The live engine is untouched.
func assertEveryPriorityOptionActs(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("assertEveryPriorityOptionActs: pending = %+v, want a priority decision", d)
	}
	if msg := priorityOptionsInert(e, d); msg != "" {
		t.Fatal(msg)
	}
}

// priorityOptionsInert returns a description of the first inert option of
// d, or "".
func priorityOptionsInert(e *Engine, d *decision.Decision) string {
	for _, o := range d.Options {
		if o.Kind == "pass" || o.Kind == "concede" {
			continue
		}
		c := e.Clone()
		mark := len(c.L.Events)
		if err := c.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
			continue
		}
		acted := false
		for _, ev := range c.L.Events[mark:] {
			if ev.Kind == events.Note && strings.HasPrefix(ev.Text, InertPriorityNotePrefix) {
				return fmt.Sprintf("turn %d %s: option %d %s %q (obj %d) was inert: %s",
					e.G.Turn, e.G.Step, o.Index, o.Kind, o.Label, o.Obj, ev.Text)
			}
			if ev.Kind != events.DecisionMade && ev.Kind != events.Priority && ev.Kind != events.DecisionAsk {
				acted = true
			}
		}
		if !acted && c.Pending() != nil && c.Pending().Kind == decision.KPriority {
			return fmt.Sprintf("turn %d %s: option %d %s %q (obj %d) emitted nothing and re-offered priority",
				e.G.Turn, e.G.Step, o.Index, o.Kind, o.Label, o.Obj)
		}
	}
	return ""
}

// TestEveryOfferedPriorityOptionActs sweeps real-card boards: 2-seat games
// over pairs of the pinned Legacy repo decks, driven by the package test bot,
// and at each of the first priority decisions with anything to do, submits
// every offered action to a clone (assertEveryPriorityOptionActs).
func TestEveryOfferedPriorityOptionActs(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	all := testutil.LegacyDeckNames()
	const perGame = 400
	for g := 0; g+1 < len(all); g += 2 {
		names := []string{all[g], all[g+1]}
		decks := [][]*cards.Card{testutil.RepoDeck(t, reg, all[g]), testutil.RepoDeck(t, reg, all[g+1])}
		cfg := Config{Seed: uint64(9100 + g), Names: names, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.Universe()}
		e := New(cfg)
		e.Advance()
		b := newTestBot(uint64(g) + 3)
		swept := 0
		for n := 0; !e.G.Over && e.Pending() != nil && n < 20000 && swept < perGame; n++ {
			d := e.Pending()
			if d.Kind == decision.KPriority && len(d.Options) > 2 {
				if msg := priorityOptionsInert(e, d); msg != "" {
					t.Fatalf("%s vs %s: %s", names[0], names[1], msg)
				}
				swept++
			}
			if err := e.Submit(b.answer(e, d)); err != nil {
				t.Fatalf("%s vs %s, intent %d: %v", names[0], names[1], n, err)
			}
		}
		t.Logf("%s vs %s: swept %d priority decisions to turn %d", names[0], names[1], swept, e.G.Turn)
		if swept == 0 {
			t.Fatalf("%s vs %s: no priority decision with an action was swept", names[0], names[1])
		}
	}
}

// TestStalePriorityOptionIsRejectedOrHeldOut pins both guards directly with
// options injected into a live priority decision the way a mis-offer would
// arrive (so the log is not replayable afterwards and the test does not
// replay it):
//
//   - an "unlock" naming a permanent that is not a Room fails the handler's
//     own first guard (unlockRoomCost), so Submit rejects it before anything
//     is recorded and the decision stays pending;
//   - a "cast" with Mode room_alt naming a non-Room passes the Submit guard
//     (the object exists) but beginCast returns at roomAlternateCastFace, so
//     the backstop records the inert Note and holds that option out until
//     the next state-changing event.
func TestStalePriorityOptionIsRejectedOrHeldOut(t *testing.T) {
	t.Parallel()
	bears := tokenReplCorpusCard(t, "Grizzly Bears")
	e, _ := tokenReplGame(t, 9183, bears)
	bearID := moveSeededCard(t, e, 0, bears, state.ZBattlefield)
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	unlock := decision.Option{Index: len(d.Options), Kind: "unlock", Label: "Unlock Grizzly Bears", Obj: bearID}
	d.Options = append(d.Options, unlock)
	intents := len(e.L.Intents)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{unlock.Index}}); err == nil {
		t.Fatalf("a stale unlock option was accepted")
	}
	if len(e.L.Intents) != intents || e.Pending() != d {
		t.Fatalf("the rejected answer was recorded or consumed the decision")
	}

	cast := decision.Option{Index: len(d.Options), Kind: "cast", Mode: "room_alt", Label: "Cast Grizzly Bears (room)", Obj: bearID}
	d.Options = append(d.Options, cast)
	mark := len(e.L.Events)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{cast.Index}}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	noted := false
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Note && strings.HasPrefix(ev.Text, InertPriorityNotePrefix) && ev.Obj == bearID {
			noted = true
		}
	}
	if !noted {
		t.Fatalf("the inert cast was not recorded with the backstop Note")
	}
	if !e.inertHeldOut[inertKeyOf(cast)] {
		t.Fatalf("the inert option is not held out")
	}
	kept := e.filterInertHeldOut([]decision.Option{cast, {Index: 1, Kind: "pass", Label: "Pass priority"}})
	if len(kept) != 1 || kept[0].Kind != "pass" || kept[0].Index != 0 {
		t.Fatalf("held-out filter kept %+v, want only pass re-indexed to 0", kept)
	}
	if c := e.Clone(); !c.inertHeldOut[inertKeyOf(cast)] {
		t.Fatalf("Clone dropped the held-out set")
	}
	e.emit(events.Event{Kind: events.Tap, Obj: bearID})
	if e.inertHeldOut != nil {
		t.Fatalf("a state-changing event did not clear the held-out set")
	}
}
