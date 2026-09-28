package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The live soft-lock this file pins (reported from the demo vs-bot game):
// casting Squadron Hawk and ACCEPTING its optional ETB trigger with no other
// Hawk in the library used to wedge the game. The trigger's DB$ ChangeZone
// search clamped max to len(eligible) = 0 and still posted a Min 0 / Max 0
// KChoose with no options -- a decision whose only legal answer is the empty
// one, which no client can render or send. The fix (effects.Ask's shared
// guard, backed by Engine.ask's boundary tripwire) resolves the shape
// silently: the search still shuffles, the fail-to-find is legitimate under
// CR 701.23b, and priority returns.
func TestSquadronHawkFailToFindResolvesWithoutAsking(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	hawk, ok := reg.Lookup("Squadron Hawk")
	if !ok {
		t.Fatal("missing corpus Squadron Hawk")
	}
	_ = hawk
	e, _ := searchEngine(t, reg, "Squadron Hawk")
	id := searchMoveByName(t, e, "Squadron Hawk", state.ZHand)
	addMana(t, e, 0, "WW")
	castFixture(t, e, id, -1)

	// Both seats pass priority until the optional ETB trigger asks its
	// accept/decline question (KTriggerOptional, decider You).
	opt := passUntilNonPriority(t, e, 20)
	if opt.Kind != decision.KTriggerOptional || opt.Player != 0 {
		t.Fatalf("pending = %+v, want the Squadron Hawk optional-trigger ask for seat 0", opt)
	}
	yesIdx := -1
	for _, o := range opt.Options {
		if o.Kind == "yes" {
			yesIdx = o.Index
		}
	}
	if yesIdx < 0 {
		t.Fatalf("optional-trigger ask carries no yes option: %+v", opt)
	}
	start := len(e.L.Events)
	submitChoices(t, e, yesIdx)

	// Drive the accepted trigger's resolution. The ONE assertion that is the
	// bug: no search KChoose may ever pend. Before the fix the very next
	// pending decision was the Min 0 / Max 0 wedge; after it the search
	// resolves silently and the game moves on. searchmay1 adds the
	// ShuffleNonMandatory$ may-shuffle confirm the fail-to-find shape now
	// poses (the search still shuffles, CR 701.23b): that is a real two-option
	// ask, not the empty-answer wedge, so it is answered with "yes --
	// shuffle" and the drive continues. Only a `search` KChoose is the wedge.
	searchAsked := false
	for i := 0; i < 40 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "search" {
			searchAsked = true
			break
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "search_mayshuffle" {
			yesIdx := -1
			for _, o := range d.Options {
				if o.Kind == "yes" {
					yesIdx = o.Index
				}
			}
			if yesIdx < 0 {
				t.Fatalf("may-shuffle confirm carries no yes option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{yesIdx}}); err != nil {
				t.Fatalf("submit the may-shuffle confirm: %v", err)
			}
			continue
		}
		if d.Kind != decision.KPriority {
			break // the game moved past the trigger without asking a search
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
	if searchAsked {
		t.Fatal("the fail-to-find search posed a search KChoose; it must resolve silently (CR 701.23b)")
	}
	if e.G.Over {
		t.Fatal("game ended; the accepted trigger should resolve into an ordinary priority round")
	}
	// The trigger left the stack when it resolved.
	for _, sid := range e.G.Stack {
		o := e.G.Obj(sid)
		if o != nil && o.Source == id {
			t.Fatalf("the accepted Squadron Hawk trigger is still on the stack: %+v", o)
		}
	}

	// The resolution's physical trace: exactly one seat-0 shuffle (the
	// search's unconditional shuffle) and NO card moved into hand -- nothing
	// was found. A second Hawk anywhere would be a deck-fixture bug, not an
	// engine one.
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
		t.Fatalf("seat-0 shuffles = %d, want exactly 1: %+v", shuffles, e.L.Events[start:])
	}
	if moves != 0 {
		t.Fatalf("%d card(s) moved to hand on a fail-to-find: %+v", moves, e.L.Events[start:])
	}
}

// TestAskBoundaryRejectsAnEmptyAnswerOnlyDecision is the boundary tripwire
// the fix adds at Engine.ask -- the ONE place a decision is posted. An
// asking primitive that bypasses effects.Ask and posts a decision whose only
// legal answer is the empty one must fail loudly here, not wedge a seat.
func TestAskBoundaryRejectsAnEmptyAnswerOnlyDecision(t *testing.T) {
	t.Parallel()
	e := New(Config{Seed: 5, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})

	panicked := func() (msg string) {
		defer func() {
			if r := recover(); r != nil {
				msg, _ = r.(string)
			}
		}()
		// Max 0 with Min 0: the empty answer is the only legal one.
		e.ask(&decision.Decision{Player: 0, Kind: decision.KChoose, Min: 0, Max: 0,
			Prompt: "choose up to 0"})
		return ""
	}()
	if panicked == "" {
		t.Fatal("Engine.ask posted a Min 0 / Max 0 decision with no options; the boundary tripwire must fire")
	}

	panicked = func() (msg string) {
		defer func() {
			if r := recover(); r != nil {
				msg, _ = r.(string)
			}
		}()
		// Zero options with Min 0 and Max > 0: the empty answer is still the
		// only one any client could send.
		e.ask(&decision.Decision{Player: 0, Kind: decision.KChoose, Min: 0, Max: 2,
			Prompt: "pick up to 2 of nothing"})
		return ""
	}()
	if panicked == "" {
		t.Fatal("Engine.ask posted a zero-option Min 0 decision; the boundary tripwire must fire")
	}

	// A decision with a real nonempty answer space posts normally.
	e.ask(&decision.Decision{Player: 0, Kind: decision.KChoose, Min: 0, Max: 1,
		Prompt:  "pick up to 1",
		Options: []decision.Option{{Index: 0, Kind: "x", Label: "X = 0"}}})
	e.pending = nil
}
