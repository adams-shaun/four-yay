// kw:For Mirrodin's expansion (cards/keywords.go) is the Living Weapon shape
// with the Rebel token instead of the Phyrexian Germ: an enters-the-
// battlefield trigger on the Equipment itself that mints the token,
// remembers it, and chains a DB$ Attach naming it.
//
// Like token_integration_test.go's Batterskull test this reads its card and
// its token definition out of the compiled corpus registry at test time --
// never copied into this file -- and asserts against the registry's own
// token definition rather than Forge's script text. It is the real-card pin
// the ratchet and the report's playable count both rest on: the keyword
// being registered is proven by a real carrier, not by the fixture's shape.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestGlimmerLensForMirrodinCreatesARebelAndAttaches casts the real Glimmer
// Lens, drains the stack (its own spell, then the For Mirrodin! trigger its
// entry queues), and checks the whole contract: exactly one token lands on
// the battlefield under the caster's control, it is the corpus's 2/2 red
// Rebel, and the Equipment is attached to it (the chained __kwFMAttach
// sub-ability, not a leftover from the cast).
func TestGlimmerLensForMirrodinCreatesARebelAndAttaches(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	lens, ok := reg.Lookup("Glimmer Lens")
	if !ok {
		t.Fatal("Glimmer Lens not found in the compiled corpus registry")
	}
	rebelDef, ok := reg.Tokens["r_2_2_rebel"]
	if !ok {
		t.Fatal("r_2_2_rebel not found in the compiled corpus registry's token scripts")
	}
	wantRebelName := rebelDef.Faces[0].Name

	cfg := seatZeroStart(Config{Seed: 911, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{lens}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
		},
		Tokens: reg.Tokens,
	})
	e := New(cfg)
	e.Advance()

	var id state.ObjID
	for _, cand := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(cand).Face().Name == "Glimmer Lens" {
			id = cand
		}
	}
	if id == 0 {
		for _, cand := range e.G.Zone(state.ZLibrary, 0) {
			if e.G.Obj(cand).Face().Name == "Glimmer Lens" {
				id = cand
			}
		}
		if id == 0 {
			t.Fatal("Glimmer Lens not found in seat 0's hand or library")
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
	}

	// Glimmer Lens is {1}{W}; fund two white symbols and re-ask priority.
	addMana(t, e, 0, "WW")

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected seat 0's priority after funding mana, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "cast" && opt.Obj == id {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Glimmer Lens: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}
	if len(e.G.Stack) != 1 || e.G.Stack[0] != id {
		t.Fatalf("stack = %v, want [%d] right after casting Glimmer Lens", e.G.Stack, id)
	}

	// The spell resolves (Glimmer Lens lands), then its own For Mirrodin!
	// trigger is queued and resolves: the TokenCreate mints the Rebel and the
	// chained Attach binds the Equipment to it.
	passUntilStackEmpty(t, e, 60)
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v after draining -- Glimmer Lens's spell and/or its For Mirrodin! "+
			"trigger did not resolve", e.G.Stack)
	}
	if got := e.G.Obj(id).Zone; got != state.ZBattlefield {
		t.Fatalf("Glimmer Lens zone = %s, want battlefield", got)
	}

	// Exactly one token, and it is the corpus's Rebel definition.
	var tokens []state.ObjID
	for _, pid := range e.G.Zone(state.ZBattlefield, 0) {
		if e.G.Obj(pid).IsToken {
			tokens = append(tokens, pid)
		}
	}
	if len(tokens) != 1 {
		t.Fatalf("battlefield tokens = %v, want exactly one Rebel", tokens)
	}
	rebel := tokens[0]

	var tokenCreate *events.Event
	for i := range e.L.Events {
		ev := &e.L.Events[i]
		if ev.Kind == events.TokenCreate && ev.Text == "r_2_2_rebel" {
			tokenCreate = ev
		}
	}
	if tokenCreate == nil {
		t.Fatalf("no TokenCreate event for r_2_2_rebel: %+v", e.L.Events)
	}
	if tokenCreate.Player != 0 {
		t.Fatalf("TokenCreate.Player = %d, want 0 (TokenOwner$ You, cast and controlled by seat 0)",
			tokenCreate.Player)
	}

	r := e.G.Obj(rebel)
	if r.Owner != 0 || r.Controller != 0 {
		t.Fatalf("rebel token owner=%d controller=%d, want both 0", r.Owner, r.Controller)
	}
	if got := r.Face().Name; got != wantRebelName {
		t.Fatalf("token name = %q, want %q (r_2_2_rebel)", got, wantRebelName)
	}
	if got := e.Power(rebel); got != 2 {
		t.Fatalf("rebel power = %d, want 2", got)
	}
	if got := e.Toughness(rebel); got != 2 {
		t.Fatalf("rebel toughness = %d, want 2", got)
	}
	// The chained Attach actually ran: the Equipment names the token.
	if got := e.G.Obj(id).AttachedTo; got != rebel {
		t.Fatalf("Glimmer Lens.AttachedTo = %d, want %d (the Rebel token)", got, rebel)
	}
}
