package rules

// Task agent-20261009T091513Z-3c5153cc: a Forge "AB$ ChooseColor |
// SubAbility$ DBMana" head is an activated mana ability (CR 605.1a), so it
// must be offered by the mana wheel, resolve off the stack at the activation
// checkpoint, and never also appear as an ordinary priority "ability". These
// leaves drive the four real corpus carriers end to end.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// battlefieldCorpusCard seeds a real corpus card directly onto seat p's
// battlefield as test setup (the handEngine/graveCreature direct-seed idiom),
// so a card's own enters-the-battlefield trigger does not have to be driven.
// The caller asserts the landed state.
func battlefieldCorpusCard(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card) state.ObjID {
	t.Helper()
	o := e.G.AddObject(c, p)
	o.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, p, append(e.G.Zone(state.ZBattlefield, p), o.ID))
	return o.ID
}

// chainHeadIndex returns the face-local index of the activated ChooseColor
// ability on id's face, plus the head itself.
func chainHeadIndex(t *testing.T, e *Engine, id state.ObjID) (int, *cards.SA) {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatalf("precondition: object %d has no face: %+v", id, o)
	}
	for i, a := range o.Face().Abilities {
		if a != nil && a.IsActivated() && a.API == "ChooseColor" {
			return i, a
		}
	}
	t.Fatalf("precondition: object %d has no activated ChooseColor ability", id)
	return -1, nil
}

// answerChooseColor submits the named colour from the pending choosecolor
// ask.
func answerChooseColor(t *testing.T, e *Engine, name string) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choosecolor" {
		t.Fatalf("expected the choosecolor ask, got %+v", d)
	}
	idx := optionByLabel(d.Options, name)
	if idx < 0 {
		t.Fatalf("choosecolor ask offers no %s option: %+v", name, d.Options)
	}
	submitChoices(t, e, idx)
}

// TestWickermawChooseColorChainIsAnImmediateManaAbility is the end-to-end pin
// for Foraging Wickermaw's "{1}: Add one mana of any color. This creature
// becomes that color until end of turn. Activate only once each turn.":
// (1) the ability is in the face's mana-ability list and offered through the
// wheel, never as an ordinary "ability"; (2) activating it resolves the
// colour ask and the production immediately, with nothing ever pushed on the
// stack; (3) the DBAnimate rider overwrites the creature's colour to the
// ANSWERED colour -- Blue, deliberately not the first WUBRG option and not
// the colourless printed face, so neither a first-colour fallback nor a
// no-rider resolution can pass.
func TestWickermawChooseColorChainIsAnImmediateManaAbility(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, nil, nil)
	id := battlefieldCorpusCard(t, e, 0, lookup(t, reg, "Foraging Wickermaw"))

	// PRECONDITION: the permanent is on the battlefield, colourless, with no
	// recorded choice -- a vacuous setup (wrong zone, pre-coloured, stale
	// ChosenColor) must fail here, not slip past the assertions below.
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Wickermaw not on the battlefield: %+v", o)
	}
	if got := e.Colors(id); got != "" {
		t.Fatalf("precondition: Wickermaw already coloured %q before any choice", got)
	}
	if got := o.ChosenColor; got != "" {
		t.Fatalf("precondition: ChosenColor = %q before the real ask", got)
	}
	headIdx, head := chainHeadIndex(t, e, id)
	if !cards.IsManaAbilitySA(head) {
		t.Fatalf("precondition: the head is not classified as a mana ability: %q", head.Line)
	}
	if !containsSA(o.Face().ManaAbilities(), head) {
		t.Fatalf("precondition: the head is absent from Face.ManaAbilities()")
	}

	e.G.Players[0].Pool[state.MC] = 1 // Wickermaw's {1}
	e.priorityRound()

	// The ordinary activated-ability offer must NOT list the head: a naive
	// head-word fix would leave it in both the wheel and the ability list.
	if _, ok := findAbilityOption(e, id, headIdx); ok {
		t.Fatalf("the chain head is offered as an ordinary ability as well as a mana ability (double offer): %+v", e.Pending().Options)
	}
	submitChoices(t, e, activateOption(t, e, id))

	// The ask is pending, the pool is still empty (the production follows the
	// answer), and no stack object was created -- a mana ability never uses
	// the stack (CR 605.3).
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choosecolor" {
		t.Fatalf("expected the mid-activation choosecolor ask, got %+v", d)
	}
	if got := poolString(e.G.Players[0].Pool); got != "" {
		t.Fatalf("pool = %q at the colour ask, want empty (production must follow the answer)", got)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("the mana activation pushed a stack object: %+v", e.G.Stack)
	}
	answerChooseColor(t, e, "Blue")

	// Activation checkpoint: the mana is in the pool NOW, the rider ran, and
	// the stack is still empty.
	if got := poolString(e.G.Players[0].Pool); got != "U" {
		t.Fatalf("pool = %q at the activation checkpoint, want the answered U", got)
	}
	if got := e.Colors(id); got != "U" {
		t.Fatalf("Wickermaw colours = %q after choosing Blue, want U (the DBAnimate rider must overwrite the colour)", got)
	}
	if got := e.G.Obj(id).ChosenColor; got != "U" {
		t.Fatalf("ChosenColor = %q, want the answered U", got)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("a stack object exists after the mana activation: %+v", e.G.Stack)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected priority after the activation, got %+v", d)
	}
}

// TestWickermawChainActivationLimitGatesTheWheel pins the ActivationLimit$ 1
// half for the chain shape: the wheel offer must pass the same limit gate the
// ordinary path did, so the second activation is withheld and exactly one
// ManaActivate marker is logged.
func TestWickermawChainActivationLimitGatesTheWheel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, nil, nil)
	id := battlefieldCorpusCard(t, e, 0, lookup(t, reg, "Foraging Wickermaw"))
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Wickermaw not on the battlefield: %+v", o)
	}
	e.G.Players[0].Pool[state.MC] = 1
	e.priorityRound()
	submitChoices(t, e, activateOption(t, e, id))
	answerChooseColor(t, e, "White")
	if got := poolString(e.G.Players[0].Pool); got != "W" {
		t.Fatalf("precondition: first activation produced %q, want W", got)
	}

	// Fund a second activation and re-ask: the limit must withhold the offer.
	e.G.Players[0].Pool[state.MC] = 1
	e.priorityRound()
	if hasActivateOption(e, id) {
		t.Fatalf("Wickermaw's ActivationLimit$ 1 chain ability was offered a second time: %+v", e.Pending().Options)
	}
	markers := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.ManaActivate && ev.Obj == id {
			markers++
		}
	}
	if markers != 1 {
		t.Fatalf("ManaActivate markers for Wickermaw = %d, want exactly 1", markers)
	}
}

// TestNyxLotusChainProducesDevotionInTheChosenColour pins the Count$
// Devotion.Chosen amount on the real Nyx Lotus: with two white pips on the
// battlefield, answering White must add TWO white mana immediately (and
// answering a colour with no devotion would add none), which proves both the
// immediate pool and that the amount reads the answered colour.
func TestNyxLotusChainProducesDevotionInTheChosenColour(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, nil, nil)
	id := battlefieldCorpusCard(t, e, 0, lookup(t, reg, "Nyx Lotus"))
	pip1 := battlefieldCreature(t, e, "Name:White Pip One\nManaCost:W\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	pip2 := battlefieldCreature(t, e, "Name:White Pip Two\nManaCost:W\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")

	// PRECONDITION: the source is untapped on the battlefield and the two
	// devotion sources really are there -- the compared devotions (White 2,
	// Green 0) actually differ, so the answered colour provably governs.
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("precondition: Nyx Lotus not an untapped battlefield permanent: %+v", o)
	}
	for _, pip := range []state.ObjID{pip1, pip2} {
		if o := e.G.Obj(pip); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: devotion source %d not on the battlefield: %+v", pip, o)
		}
	}
	e.priorityRound()
	submitChoices(t, e, activateOption(t, e, id))
	answerChooseColor(t, e, "White")
	if got := poolString(e.G.Players[0].Pool); got != "WW" {
		t.Fatalf("pool = %q after choosing White, want WW (devotion to White is 2)", got)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("Nyx Lotus's mana activation pushed a stack object: %+v", e.G.Stack)
	}
}

// TestNykthosChainSharesTheWheelWithItsPlainManaAbility: Nykthos has a plain
// AB$ Mana and the ChooseColor chain, so activating it poses the wheel. Both
// options must be present, the chain's label must name its production (the
// head carries no Produced$ of its own), and picking the chain must resolve
// through the same immediate path.
func TestNykthosChainSharesTheWheelWithItsPlainManaAbility(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, nil, nil)
	id := battlefieldCorpusCard(t, e, 0, lookup(t, reg, "Nykthos, Shrine to Nyx"))
	pip1 := battlefieldCreature(t, e, "Name:Green Pip One\nManaCost:G\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	pip2 := battlefieldCreature(t, e, "Name:Green Pip Two\nManaCost:G\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Nykthos not on the battlefield: %+v", o)
	}
	for _, pip := range []state.ObjID{pip1, pip2} {
		if o := e.G.Obj(pip); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: devotion source %d not on the battlefield: %+v", pip, o)
		}
	}
	e.G.Players[0].Pool[state.MC] = 2 // the chain's {2}; the plain ability needs {T} only
	e.priorityRound()
	submitChoices(t, e, activateOption(t, e, id))

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the mana-ability wheel, got %+v", d)
	}
	chain := -1
	plain := -1
	for _, o := range d.Options {
		switch {
		case o.Label == "Add C":
			plain = o.Index
		case strings.HasSuffix(o.Label, "Add chosen color"):
			chain = o.Index
		}
	}
	if plain < 0 || chain < 0 {
		t.Fatalf("wheel = %+v, want both the plain \"Add C\" and the chain's \"Add chosen color\"", d.Options)
	}
	submitChoices(t, e, chain)
	answerChooseColor(t, e, "Green")
	if got := poolString(e.G.Players[0].Pool); got != "GG" {
		t.Fatalf("pool = %q after the chain wheel pick + Green, want GG (the {2} spent, devotion 2)", got)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("Nykthos's chain activation pushed a stack object: %+v", e.G.Stack)
	}
}

// TestRhysticCaveChainIsOfferedAndProducesImmediately: the fourth carrier.
// Its UnlessCost$ rider is not the assertion -- the activation must still be
// a wheel mana ability with the mana in the pool at the checkpoint, and the
// unless offer must not leave a stack object behind.
func TestRhysticCaveChainIsOfferedAndProducesImmediately(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, nil, nil)
	id := battlefieldCorpusCard(t, e, 0, lookup(t, reg, "Rhystic Cave"))
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Rhystic Cave not on the battlefield: %+v", o)
	}
	e.priorityRound()
	submitChoices(t, e, activateOption(t, e, id))

	answerChooseColor(t, e, "Blue")
	// The DB$ Mana's UnlessCost$ 1 rider is "unless any player pays {1}", so
	// the tape poses the pay offer to each player in turn; decline every one
	// so the production is the path under test.
	for i := 0; i < 8; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KModes || d.ResumeKind != "unless_pay" {
			break
		}
		idx := optionByLabel(d.Options, "Don't pay")
		if idx < 0 {
			t.Fatalf("unless offer has no decline option: %+v", d.Options)
		}
		submitChoices(t, e, idx)
	}
	if got := poolString(e.G.Players[0].Pool); got != "U" {
		t.Fatalf("pool = %q after choosing Blue, want the immediate U", got)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("Rhystic Cave's mana activation pushed a stack object: %+v", e.G.Stack)
	}
}

func containsSA(sas []*cards.SA, want *cards.SA) bool {
	for _, sa := range sas {
		if sa == want {
			return true
		}
	}
	return false
}
