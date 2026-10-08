// kw:Job select (CR 702.182) is the For Mirrodin! / Living Weapon shape with
// the c_1_1_hero token instead of the Rebel or the Phyrexian Germ: an
// enters-the-battlefield trigger on the Equipment itself that mints the
// token, remembers it, and chains a DB$ Attach naming it.
//
// These read their cards and the token definition out of the compiled corpus
// registry at test time -- never copied into this file (Forge scripts are
// GPL-3.0) -- and assert against the registry's own definitions. The real
// carrier test is the pin the registration rests on: the keyword being
// registered is proven by Black Mage's Rod, the SpellBench Affinity driver.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestJobSelectRegisteredMakesCarriersFullySupported walks the whole compiled
// corpus for every card carrying a K:Job select keyword line and asserts the
// registration makes each fully supported. The count precondition makes the
// test fail if the corpus or the keyword spelling ever drifts: an empty walk
// would otherwise pass vacuously with the registration reverted.
func TestJobSelectRegisteredMakesCarriersFullySupported(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()

	// Precondition: the registration is the thing under test, so it must be
	// present, or reverting it would leave this test green.
	if !supported["kw:Job select"] {
		t.Fatal("kw:Job select is not registered in effects.Supported()")
	}
	carriers := 0
	for _, c := range reg.AllCards() {
		if !cardCarriesJobSelect(c) {
			continue
		}
		carriers++
		if m := reg.Unsupported(c, supported); len(m) > 0 {
			t.Errorf("%s carries K:Job select but is still unsupported: %v", c.Faces[0].Name, m)
		}
	}
	if carriers == 0 {
		t.Fatal("no corpus card carries K:Job select -- the walk is vacuous, the keyword spelling may have changed")
	}
	t.Logf("K:Job select carriers fully supported: %d", carriers)
}

// cardCarriesJobSelect reports whether any face of c prints a K:Job select
// line, matching by keyword head so trailing parameters (none today) cannot
// hide a carrier.
func cardCarriesJobSelect(c *cards.Card) bool {
	for _, f := range c.Faces {
		for _, k := range f.Keywords {
			if cards.KeywordHead(k) == "Job select" {
				return true
			}
		}
	}
	return false
}

// TestBlackMagesRodJobSelectCreatesHeroAndAttaches casts the real Black
// Mage's Rod ({1}{B}), drains the stack (its own spell, then the Job select
// trigger its entry queues), and checks the whole contract: exactly one token
// lands on the battlefield under the caster's control, it is the corpus's 1/1
// colorless Hero, and the Equipment is attached to it (the chained
// __kwJSAttach sub-ability). It then asserts the Rod's own static reaches the
// equipped Hero: +1/+0 and the Wizard type.
func TestBlackMagesRodJobSelectCreatesHeroAndAttaches(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	rod, ok := reg.Lookup("Black Mage's Rod")
	if !ok {
		t.Fatal("Black Mage's Rod not found in the compiled corpus registry")
	}
	heroDef, ok := reg.Tokens["c_1_1_hero"]
	if !ok {
		t.Fatal("c_1_1_hero not found in the compiled corpus registry's token scripts")
	}
	wantHeroName := heroDef.Faces[0].Name

	cfg := seatZeroStart(Config{Seed: 911, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{rod}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
		},
		Tokens: reg.Tokens,
	})
	e := New(cfg)
	e.Advance()

	var id state.ObjID
	for _, cand := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(cand).Face().Name == "Black Mage's Rod" {
			id = cand
		}
	}
	if id == 0 {
		for _, cand := range e.G.Zone(state.ZLibrary, 0) {
			if e.G.Obj(cand).Face().Name == "Black Mage's Rod" {
				id = cand
			}
		}
		if id == 0 {
			t.Fatal("Black Mage's Rod not found in seat 0's hand or library")
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
	}

	// Black Mage's Rod is {1}{B}; fund white and black symbols and re-ask.
	addMana(t, e, 0, "BB")

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
		t.Fatalf("no cast option for Black Mage's Rod: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}
	if len(e.G.Stack) != 1 || e.G.Stack[0] != id {
		t.Fatalf("stack = %v, want [%d] right after casting Black Mage's Rod", e.G.Stack, id)
	}

	// The spell resolves (the Rod lands), then its own Job select trigger is
	// queued and resolves: the TokenCreate mints the Hero and the chained
	// Attach binds the Equipment to it.
	passUntilStackEmpty(t, e, 60)
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v after draining -- Black Mage's Rod's spell and/or its Job select "+
			"trigger did not resolve", e.G.Stack)
	}
	if got := e.G.Obj(id).Zone; got != state.ZBattlefield {
		t.Fatalf("Black Mage's Rod zone = %s, want battlefield", got)
	}

	// Exactly one token, and it is the corpus's Hero definition.
	var tokens []state.ObjID
	for _, pid := range e.G.Zone(state.ZBattlefield, 0) {
		if e.G.Obj(pid).IsToken {
			tokens = append(tokens, pid)
		}
	}
	if len(tokens) != 1 {
		t.Fatalf("battlefield tokens = %v, want exactly one Hero", tokens)
	}
	hero := tokens[0]

	var tokenCreate *events.Event
	for i := range e.L.Events {
		ev := &e.L.Events[i]
		if ev.Kind == events.TokenCreate && ev.Text == "c_1_1_hero" {
			tokenCreate = ev
		}
	}
	if tokenCreate == nil {
		t.Fatalf("no TokenCreate event for c_1_1_hero: %+v", e.L.Events)
	}
	if tokenCreate.Player != 0 {
		t.Fatalf("TokenCreate.Player = %d, want 0 (TokenOwner$ You, cast and controlled by seat 0)",
			tokenCreate.Player)
	}

	h := e.G.Obj(hero)
	if h.Owner != 0 || h.Controller != 0 {
		t.Fatalf("hero token owner=%d controller=%d, want both 0", h.Owner, h.Controller)
	}
	hf := h.Face()
	if got := hf.Name; got != wantHeroName {
		t.Fatalf("token name = %q, want %q (c_1_1_hero)", got, wantHeroName)
	}
	if hf.Power() != 1 || hf.Toughness() != 1 {
		t.Fatalf("hero token base PT = %d/%d, want 1/1", hf.Power(), hf.Toughness())
	}
	if hf.Colors != "" {
		t.Fatalf("hero token colours = %q, want colorless (empty)", hf.Colors)
	}
	if !hasTypeWord(hf.Types, "Hero") || !hasTypeWord(hf.Types, "Creature") {
		t.Fatalf("hero token types = %v, want a Creature Hero", hf.Types)
	}

	// The chained Attach actually ran: the Equipment names the token.
	if got := e.G.Obj(id).AttachedTo; got != hero {
		t.Fatalf("Black Mage's Rod.AttachedTo = %d, want %d (the Hero token)", got, hero)
	}

	// The Rod's own static reaches the equipped Hero: +1/+0 and the Wizard
	// type. Assert against the layer-computed values, not the token's printed
	// face -- the printed face is 1/1 and has no Wizard type.
	if got := e.Power(hero); got != 2 {
		t.Fatalf("equipped Hero power = %d, want 2 (1/1 plus the Rod's +1/+0)", got)
	}
	if !hasTypeWord(e.Derived(hero).Types, "Wizard") {
		t.Fatalf("equipped Hero types = %v, want Wizard added by the Rod's static", e.Derived(hero).Types)
	}
}
