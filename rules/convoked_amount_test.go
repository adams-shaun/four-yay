package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// End-to-end `Count$Convoked$Amount` tests (CR 702.66) on the two real corpus
// carriers:
//
//   - Ancient Imperiosaur: `SVar:X:Convoked$Amount/Twice` drives
//     `K:etbCounter:P1P1:X`, so it enters with TWO +1/+1 counters per creature
//     that convoked it.
//   - Knight-Errant of Eos: `SVar:X:Convoked$Amount` sizes the ETB Dig's
//     `ChangeValid$ Creature.cmcLEX`, so X is the number of convoking
//     creatures (it read 0 -- making the Dig find only cmc-0 creatures --
//     before the head resolved).
//
// The cast drives the real convoke announcement (rules/cast.go's convokeAsk)
// so both Object.Convoked and the head read the same provenance. No Forge
// script text is committed; the cards come from the compiled corpus.

func convokedCorpusEngine(t *testing.T, seat0 []string) (*Engine, Config) {
	t.Helper()
	reg := searchTestRegistry(t)
	deck := make([]*cards.Card, 0, 40)
	for _, name := range seat0 {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	forest := searchCorpusCard(t, reg, "Forest")
	for len(deck) < 40 {
		deck = append(deck, forest)
	}
	opp := make([]*cards.Card, 0, 40)
	mountain := searchCorpusCard(t, reg, "Mountain")
	for len(opp) < 40 {
		opp = append(opp, mountain)
	}
	var e *Engine
	var cfg Config
	for seed := uint64(9107); ; seed++ {
		cfg = Config{Seed: seed, Names: []string{"convoker", "opponent"},
			Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens}
		e = New(cfg)
		e.Advance()
		if e.G.Active == 0 {
			break
		}
	}
	toMain1(t, e)
	return e, cfg
}

// castWithConvokeAt adds mana for the named spell (minus the convoke
// contributions), submits the cast, and answers the convoke announcement
// with every offered generic option for the given creatures.
func castWithConvokeAt(t *testing.T, e *Engine, spell state.ObjID, mana string, convokers ...state.ObjID) {
	t.Helper()
	addMana(t, e, 0, mana)
	d := e.Pending()
	castIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("no cast option for %d: %+v", spell, d.Options)
	}
	submitChoices(t, e, castIdx)
	// The convoke announcement precedes the target ask (CR 601.2b) and is ONE
	// KChoose whose answers are every creature to tap; submit each convoker's
	// first offered convoke option in a single Intent.
	deadline := 0
	for {
		d = e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			break
		}
		if deadline++; deadline > 8 {
			t.Fatalf("convoke announcement did not clear: %+v", d.Options)
		}
		var picks []int
		for _, want := range convokers {
			for _, o := range d.Options {
				if o.Obj == want && strings.HasPrefix(o.Kind, "convoke_") {
					picks = append(picks, o.Index)
					break
				}
			}
		}
		if len(picks) == 0 {
			break
		}
		submitChoices(t, e, picks...)
	}
}

// TestAncientImperiosaurEntersWithTwoCountersPerConvoker pins the /Twice
// composition end-to-end: two creatures convoke the cast, so the ETB places
// FOUR +1/+1 counters (two each), not zero (the reported bug) and not two.
func TestAncientImperiosaurEntersWithTwoCountersPerConvoker(t *testing.T) {
	t.Parallel()
	e, _ := convokedCorpusEngine(t, []string{"Ancient Imperiosaur", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears"})
	bearA := conniveMoveTo(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	bearB := conniveMoveTo(t, e, 0, "Grizzly Bears", state.ZBattlefield, bearA)
	other := conniveMoveTo(t, e, 0, "Grizzly Bears", state.ZBattlefield, bearA, bearB)
	if bearA == bearB {
		t.Fatalf("fixture precondition: two distinct bears required")
	}
	spell := conniveMoveTo(t, e, 0, "Ancient Imperiosaur", state.ZHand)
	// 5GG = 7 mana; two green convoke contributions cover two of it, so add
	// the full cost and let the convoke announcement select the bears.
	castWithConvokeAt(t, e, spell, "GGGGGGG", bearA, bearB)
	passUntilStackEmpty(t, e, 30)

	o := e.G.Obj(spell)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Ancient Imperiosaur zone = %+v, want battlefield", o)
	}
	// Precondition the assertion depends on: the convoked set really carried
	// two creatures (otherwise a 0 or 4 reading could be confused).
	if len(o.Convoked) != 2 || !containsID(o.Convoked, bearA) || !containsID(o.Convoked, bearB) || containsID(o.Convoked, other) {
		t.Fatalf("precondition: cast captured exactly the two chosen convokers, got %v; nonconvoker %d", o.Convoked, other)
	}
	if third := e.G.Obj(other); third == nil || third.Zone != state.ZBattlefield || third.Tapped {
		t.Fatalf("precondition: nonconvoker must remain untapped on battlefield: %+v", third)
	}
	if got := o.Counter("P1P1"); got != 4 {
		t.Fatalf("Ancient Imperiosaur P1P1 counters = %d, want 4 (2 convokers x /Twice)", got)
	}
}

// TestKnightErrantOfEosXCountsConvokers pins the plain head end-to-end: the
// resolved permanent's preserved Object.Convoked makes the card's own
// `SVar:X:Convoked$Amount` read the number of convoking creatures.
func TestKnightErrantOfEosXCountsConvokers(t *testing.T) {
	t.Parallel()
	e, _ := convokedCorpusEngine(t, []string{"Knight-Errant of Eos", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Ornithopter", "Hill Giant", "Serra Angel"})
	bearA := conniveMoveTo(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	bearB := conniveMoveTo(t, e, 0, "Grizzly Bears", state.ZBattlefield, bearA)
	spell := conniveMoveTo(t, e, 0, "Knight-Errant of Eos", state.ZHand)
	// Order a six-card Dig window with MV 0, 2, 4 and 5. Only the
	// zero- and two-drops qualify at X=2; at the broken X=0 only the
	// Ornithopter qualifies. Keep all movements and ordering replay-visible.
	window := []state.ObjID{
		conniveMoveTo(t, e, 0, "Ornithopter", state.ZLibrary),
		conniveMoveTo(t, e, 0, "Grizzly Bears", state.ZLibrary, bearA, bearB),
		conniveMoveTo(t, e, 0, "Hill Giant", state.ZLibrary),
		conniveMoveTo(t, e, 0, "Serra Angel", state.ZLibrary),
	}
	lib := e.G.Zone(state.ZLibrary, 0)
	ordered := append([]state.ObjID(nil), window...)
	for _, id := range lib {
		if !containsID(window, id) {
			ordered = append(ordered, id)
		}
	}
	if len(ordered) != len(lib) {
		t.Fatalf("precondition: window cards must all be in library: %v vs %v", window, lib)
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: ordered})
	for i, id := range window {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZLibrary {
			t.Fatalf("precondition: window object %d must be in library: %+v", id, o)
		} else if want := []int32{0, 2, 4, 5}[i]; o.Face().ManaValue() != want {
			t.Fatalf("precondition: window card %d MV = %d, want %d", id, o.Face().ManaValue(), want)
		}
	}
	// 4W = 5 mana; two white convoke contributions cover two of it.
	castWithConvokeAt(t, e, spell, "WWWWW", bearA, bearB)

	// While the spell is on the stack the head must read 2 off the source.
	if o := e.G.Obj(spell); o == nil || len(o.Convoked) != 2 {
		t.Fatalf("precondition: Object.Convoked on the stack = %+v, want 2", o)
	}
	// Resolve the cast and its ETB until the actual Dig ask, not a
	// post-resolution count read (which cannot prove the filter used X).
	for n := 0; n < 30; n++ {
		d := e.Pending()
		if d != nil && d.Kind == decision.KChoose && d.ResumeKind == "dig" {
			if d.Source != spell || d.Min != 0 || d.Max != 2 {
				t.Fatalf("Knight Dig decision = %+v, want optional 0..2 from Knight", d)
			}
			eligible := map[state.ObjID]bool{}
			for _, opt := range d.Options {
				eligible[opt.Obj] = true
			}
			if len(eligible) != 2 || !eligible[window[0]] || !eligible[window[1]] || eligible[window[2]] || eligible[window[3]] {
				t.Fatalf("Dig offered %v, want only MV 0 and MV 2 (%d, %d)", eligible, window[0], window[1])
			}
			for _, opt := range d.Options {
				if opt.Obj == window[1] {
					submitChoices(t, e, opt.Index) // take the MV-2 bear, impossible at X=0
					goto answeredDig
				}
			}
			t.Fatal("MV-2 Dig option missing")
		}
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("before Knight Dig, unexpected decision: %+v", d)
		}
		for _, opt := range d.Options {
			if opt.Kind == "pass" {
				submitChoices(t, e, opt.Index)
				goto nextDecision
			}
		}
		t.Fatalf("no pass option before Knight Dig: %+v", d)
	nextDecision:
	}
	t.Fatal("Knight Dig never offered a choice")
answeredDig:
	passUntilStackEmpty(t, e, 30)
	o := e.G.Obj(spell)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Knight-Errant of Eos zone = %+v, want battlefield", o)
	}
	if picked := e.G.Obj(window[1]); picked == nil || picked.Zone != state.ZHand {
		t.Fatalf("precondition: answered Dig pick must land in hand: %+v", picked)
	}
	// The provenance survives the stack->battlefield move: the ETB Dig's X
	// (the card's own SVar X body) reads 2 off the resolved permanent.
	if len(o.Convoked) != 2 {
		t.Fatalf("precondition: Object.Convoked = %v, want 2 creatures", o.Convoked)
	}
	body := o.Face().SVars["X"]
	if body == "" {
		t.Fatalf("precondition: Knight-Errant SVar X absent")
	}
	ctx := &effects.Ctx{Source: spell, Controller: 0}
	if got, ok := effects.EvalCountOK(e, ctx, body); !ok || got != 2 {
		t.Fatalf("SVar X (%q) off the resolved permanent = (%d,%v), want (2,true)", body, got, ok)
	}
}
