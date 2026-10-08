package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// briberyEngine builds a two-seat game whose seat 0 hand holds the corpus
// Bribery and whose seat 1 library is entirely corpus Grizzly Bears, so the
// Bribery search has a creature to find and the found permanent's owner
// (seat 1) differs from the GainControl$ caster (seat 0).
func briberyEngine(t *testing.T) (*Engine, Config) {
	t.Helper()
	reg := searchTestRegistry(t)
	bribery, ok := reg.Lookup("Bribery")
	if !ok {
		t.Fatal("missing corpus Bribery")
	}
	bears, ok := reg.Lookup("Grizzly Bears")
	if !ok {
		t.Fatal("missing corpus Grizzly Bears")
	}
	forest, ok := reg.Lookup("Forest")
	if !ok {
		t.Fatal("missing corpus Forest")
	}
	deck := make([]*cards.Card, 0, 40)
	deck = append(deck, bribery)
	for len(deck) < 40 {
		deck = append(deck, forest)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = bears
	}
	cfg := Config{Seed: 9202, Names: []string{"searcher", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens, NameUniverse: reg.Universe()}
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// TestBriberyEndToEndPutsFetchedCreatureUnderCastersControl is the engine
// carrier for the hidden-library ChangeZone GainControl$ sub-shape: Bribery
// searches the target opponent's library and puts the found creature onto
// the battlefield under the CASTER's control (CR 610), not under the
// library's owner. Before the fix the fetched permanent kept its existing
// controller (seat 1).
func TestBriberyEndToEndPutsFetchedCreatureUnderCastersControl(t *testing.T) {
	t.Parallel()
	e, cfg := briberyEngine(t)
	bribery := searchMoveByName(t, e, "Bribery", state.ZHand)
	addMana(t, e, 0, "UUUCCCCCCCC")
	if d := e.Pending(); d == nil {
		t.Fatal("no priority decision before casting Bribery")
	}
	// Cast, target the opponent, then resolve to the hidden search ask.
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bribery {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Bribery: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		chooseTargetPlayer(t, e, 1)
	}
	d = passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("Bribery did not pose a hidden search: %+v", d)
	}
	if len(d.Options) == 0 {
		t.Fatalf("Bribery search offered no creature: %+v", d)
	}
	picked := d.Options[0].Obj
	// Precondition: the picked creature is in seat 1's library, owned and
	// controlled by seat 1, which differs from the caster (seat 0).
	if got := e.G.Obj(picked).Zone; got != state.ZLibrary {
		t.Fatalf("precondition: picked creature zone=%s, want library", got)
	}
	if e.G.Obj(picked).Owner != 1 || e.G.Obj(picked).Controller != 1 {
		t.Fatalf("precondition: picked owner=%d controller=%d, want 1/1",
			e.G.Obj(picked).Owner, e.G.Obj(picked).Controller)
	}
	start := len(e.L.Events)
	submitChoices(t, e, d.Options[0].Index)
	if got := e.G.Obj(picked).Zone; got != state.ZBattlefield {
		t.Fatalf("picked creature zone=%s, want battlefield", got)
	}
	if got := e.G.Obj(picked).Controller; got != 0 {
		t.Fatalf("picked creature controller=%d, want 0 (the Bribery caster)", got)
	}
	control := false
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.ControlChange && ev.Obj == picked {
			control = true
			if ev.Player != 0 {
				t.Fatalf("ControlChange player=%d, want 0", ev.Player)
			}
		}
	}
	if !control {
		t.Fatalf("no ControlChange for the fetched creature after %d events: %+v", len(e.L.Events)-start, e.L.Events[start:])
	}
	replayCheck(t, e, cfg)
}
