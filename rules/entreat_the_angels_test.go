package rules

// Every test here drives the REAL repo-deck card -- Entreat the Angels,
// compiled from the corpus (.cards/cardsfolder/e/entreat_the_angels.txt,
// shipped in internal/testutil/decks/uw-control.json) and looked up through
// testutil.CorpusRegistry, never a hand-written fixture that happens to
// share its name. That is the point: the fixture-shaped twin ("Name:Entreat")
// this file's first draft asserted passed even if the corpus card's compiled
// script diverged or the corpus lost the card entirely, so it defended
// nothing. entreatCorpusCard fails the test if the card is missing or its
// audited shape (TokenAmount$ X -> SVar:X:Count$xPaid -> the
// w_4_4_angel_flying token script, plus the Miracle keyword) is not what the
// parser produced, and entreatCorpusEngine puts that exact card pointer into
// seat 0's deck so the object the cast flow moves is provably the corpus
// card (asserted by pointer identity, not just by name).

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// entreatCorpusCard looks Entreat the Angels up in the compiled corpus and
// asserts the audited script shape the X-binding defect lives in: the spell
// body is SP$ Token with TokenAmount$ X naming the SVar:X:Count$xPaid idiom,
// the token script exists in the corpus token registry, and the Miracle
// keyword the miracle test drives is present.
func entreatCorpusCard(t *testing.T, reg *cards.Registry) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup("Entreat the Angels")
	if !ok {
		t.Fatal("corpus has no Entreat the Angels -- the repo-deck card this defect is about is untestable")
	}
	f := c.Faces[0]
	if f.Name != "Entreat the Angels" {
		t.Fatalf("corpus lookup returned %q", f.Name)
	}
	var tokenSA *cards.SA
	for _, a := range f.Abilities {
		if a.Kind == "SP" && a.API == "Token" {
			tokenSA = a
		}
	}
	if tokenSA == nil {
		t.Fatalf("corpus Entreat the Angels carries no SP$ Token ability: %+v", f.Abilities)
	}
	if tokenSA.Params["TokenAmount"] != "X" || tokenSA.Params["TokenScript"] != "w_4_4_angel_flying" {
		t.Fatalf("corpus Entreat the Angels token params = %v, want TokenAmount$ X -> w_4_4_angel_flying", tokenSA.Params)
	}
	miracle := false
	for _, k := range f.Keywords {
		if strings.HasPrefix(k, "Miracle") {
			miracle = true
		}
	}
	if !miracle {
		t.Fatalf("corpus Entreat the Angels keywords %v carry no Miracle", f.Keywords)
	}
	if reg.Tokens["w_4_4_angel_flying"] == nil {
		t.Fatal("corpus token registry has no w_4_4_angel_flying for Entreat's TokenScript$")
	}
	return c
}

// entreatCorpusEngine builds a game whose seat 0 deck is the corpus Entreat
// plus Plains filler (no other ability can interfere with the cast flow) and
// returns the engine with Entreat in seat 0's hand, the config for
// replayCheck, and the Entreat object's id. The corpus token registry backs
// the TokenCreate, so the angels a resolution mints are the real w_4_4_
// angel_flying token, not a test fixture.
func entreatCorpusEngine(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, *cards.Card) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	entreat := entreatCorpusCard(t, reg)
	deck := []*cards.Card{entreat}
	if plains, ok := reg.Lookup("Plains"); ok {
		for len(deck) < 31 {
			deck = append(deck, plains)
		}
	}
	cfg := Config{Seed: seed, Names: []string{"caster", "opponent"},
		Decks:  [][]*cards.Card{deck, testutil.RepoDeck(t, reg, "death-n-taxes")},
		Tokens: reg.Tokens}
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	var id state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, cand := range e.G.Zone(z, 0) {
			if e.G.Obj(cand).Face().Name == "Entreat the Angels" {
				id = cand
			}
		}
	}
	if id == 0 {
		t.Fatal("Entreat the Angels absent from seat 0's hand and library")
	}
	if e.G.Obj(id).Zone != state.ZHand {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
	}
	return e, cfg, id, entreat
}

// countAngelTokens counts seat p's battlefield Angels named "Angel Token"
// (the corpus w_4_4_angel_flying token's face name).
func countAngelTokens(t *testing.T, e *Engine, p state.PlayerID) int {
	t.Helper()
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if f := e.G.Obj(id).Face(); f != nil && f.Name == "Angel Token" {
			n++
		}
	}
	return n
}

// TestEntreatTheAngelsCreatesItsChosenNumberOfAngels is CR 107.3i end to
// end on the real card: the value chosen for a spell's {X} is what every
// numeric parameter that reads the paid X sees as the spell resolves.
// resolveTop's spell branch binds the stack object's CastInfo-recorded X
// (events.CastInfo -> o.X) into effects.Ctx.X, so SVar:X:Count$xPaid answers
// the chosen value and TokenAmount$ X creates that many tokens -- not zero,
// which is what a Ctx without the binding made Entreat resolve to (the
// user-reported live-demo defect).
func TestEntreatTheAngelsCreatesItsChosenNumberOfAngels(t *testing.T) {
	t.Parallel()
	e, cfg, id, entreat := entreatCorpusEngine(t, 111)
	addMana(t, e, 0, "CCCCWWW") // X = 2: {X}{X} folds to {2}{2} generic + {W}{W}{W}
	opts := castOptions(t, e)
	idx := -1
	for _, o := range opts {
		if o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Entreat the Angels: %+v", opts)
	}
	submitChoices(t, e, idx)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("X decision %+v", d)
	}
	submitChoices(t, e, 2)
	// The card on the stack is the corpus card itself, pointer and all --
	// not a fixture that merely shares its name.
	stackObj := e.G.Stack[len(e.G.Stack)-1]
	if o := e.G.Obj(stackObj); o.Card != entreat {
		t.Fatalf("stack object %d is not the corpus Entreat the Angels card", stackObj)
	}
	passUntilStackEmpty(t, e, 20)
	if got := countAngelTokens(t, e, 0); got != 2 {
		t.Fatalf("Angels on the battlefield = %d, want 2", got)
	}
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatalf("Entreat the Angels zone %s after resolving", e.G.Obj(id).Zone)
	}
	// The transport the fix reads: the CastInfo that recorded the chosen X
	// onto the stack object.
	castInfo := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == stackObj && ev.Amount == 2 {
			castInfo = true
		}
	}
	if !castInfo {
		t.Fatalf("no CastInfo(X = 2) for stack object %d in the log", stackObj)
	}
	replayCheck(t, e, cfg)
}
