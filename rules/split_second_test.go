package rules

// K:Split second (CR 702.62) pins, on real corpus cards: while a
// split-second spell is on the stack the priority-offer walk withholds
// every cast and non-mana ability option and keeps mana abilities, land
// plays, pass and concede; the offers return the moment the spell leaves
// the stack. The granted-keyword path is pinned on Molten Disaster's
// kicked-gated AddKeyword$ Split second CharacteristicDefining static
// (PresentZone$ Stack), with the unkicked cast as the control proving the
// gate keys on the keyword and not on the card's presence.
//
// V.A.T.S. is the deck carrier named in the ticket (Hail, Caesar pip
// census); no repo deck plays any of the 22 K:Split second carriers, so the
// chain heads and the support ratchet are untouched by construction.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// splitSecondEngine deals seat 0 a 40-card corpus deck whose first cards are
// the named spells, then Golem Foundry, a Forest, 8 Forest/8 Mountain pairs
// and Grizzly Bears; moves Golem Foundry, a Forest and a Grizzly Bears onto
// the battlefield (the foundry charged with 3 CHARGE so its instant-speed
// non-mana ability is offered, the Forest so a mana ability is offered, the
// Bear so V.A.T.S.'s equal-toughness target ask has an option to decline)
// and leaves seat 0 at its Main1 priority. No Forge script text is
// committed: every card comes from the compiled corpus registry.
func splitSecondEngine(t *testing.T, reg *cards.Registry, spells ...string) (*Engine, Config, state.ObjID) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range spells {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	deck = append(deck, searchCorpusCard(t, reg, "Golem Foundry"), forest)
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 9210, Names: []string{"suddenseat", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	foundry := searchMoveByName(t, e, "Golem Foundry", state.ZBattlefield)
	searchMoveByName(t, e, "Forest", state.ZBattlefield)
	searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: foundry, Counter: "CHARGE", Amount: 3})
	e.pending = nil
	e.priorityRound()
	return e, cfg, foundry
}

// splitSecondStackID returns the stack object carrying the given card name,
// failing the test when absent.
func splitSecondStackID(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZStack, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("%q not on the stack", name)
	return 0
}

// castOpt returns the priority decision's cast option for id (any mode),
// failing when absent -- the local wrapper over the shared castOptionIdx
// helper, which reads a decision rather than the engine.
func castOpt(t *testing.T, e *Engine, id state.ObjID) int {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	idx := castOptionIdx(d, "cast", id)
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", id, d.Options)
	}
	return idx
}

// castModeOpt returns the priority decision's cast option for id with the
// exact Mode ("" = the plain cast -- Molten Disaster offers plain and kicked
// side by side, and the shared castOptionIdx helper returns the FIRST match,
// which is the plain one), failing when absent.
func castModeOpt(t *testing.T, e *Engine, id state.ObjID, mode string) int {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id && o.Mode == mode {
			return o.Index
		}
	}
	t.Fatalf("no %q cast option for %d: %+v", mode, id, d.Options)
	return -1
}

// hasOptionKind reports whether any option carries the kind.
func hasOptionKind(opts []decision.Option, kind string) bool {
	for _, o := range opts {
		if o.Kind == kind {
			return true
		}
	}
	return false
}

func TestVATSSplitSecondWithholdsCastsAndActivations(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, foundry := splitSecondEngine(t, reg, "V.A.T.S.", "Lightning Bolt")
	bolt := searchMoveByName(t, e, "Lightning Bolt", state.ZHand)
	vats := searchMoveByName(t, e, "V.A.T.S.", state.ZHand)
	addMana(t, e, 0, "BBBRR")

	// Pre-conditions, no split second on the stack: both casts and the
	// foundry's instant-speed non-mana ability are offered.
	if !hasCastOption(e.legalActions(0), bolt) || !hasCastOption(e.legalActions(0), vats) {
		t.Fatalf("pre-cast offers missing: %+v", e.legalActions(0))
	}
	if !hasOptionKind(e.legalActions(0), "ability") {
		t.Fatalf("Golem Foundry's activated ability not offered pre-cast: %+v", e.legalActions(0))
	}

	// Cast V.A.T.S. and decline its equal-toughness target ask (TargetMin$ 0:
	// the empty answer is legal and destroys nothing, so the Bear survives as
	// a witness that the release check below is not a SBA side effect).
	submitChoices(t, e, castOpt(t, e, vats))
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		submitChoices(t, e)
	}
	stackID := splitSecondStackID(t, e, "V.A.T.S.")
	if !e.HasKeyword(stackID, "Split second") {
		t.Fatalf("V.A.T.S. on the stack lacks its printed keyword")
	}

	// While V.A.T.S. is on the stack: no cast, no non-mana ability -- but the
	// Forest's mana ability and pass remain (CR 702.62's exact carve-out).
	opts := e.legalActions(0)
	if hasOptionKind(opts, "cast") {
		t.Fatalf("cast options offered while V.A.T.S. is on the stack: %+v", opts)
	}
	if hasOptionKind(opts, "ability") {
		t.Fatalf("non-mana ability options offered while V.A.T.S. is on the stack: %+v", opts)
	}
	if !hasOptionKind(opts, "activate") {
		t.Fatalf("mana-ability activation not offered while V.A.T.S. is on the stack: %+v", opts)
	}
	if !hasOptionKind(opts, "pass") {
		t.Fatalf("pass not offered while V.A.T.S. is on the stack: %+v", opts)
	}
	if e.G.Obj(foundry).Counters == nil || len(e.G.Obj(foundry).Counters) == 0 {
		t.Fatalf("the split-second window must not have consumed the foundry's counters")
	}

	// Release: both seats pass, V.A.T.S. resolves (nothing destroyed), and
	// the withheld offers return with the spell off the stack.
	submitPass(t, e)
	submitPass(t, e)
	if d := e.Pending(); d == nil {
		t.Fatal("no decision pending after V.A.T.S. resolved")
	}
	if len(e.G.Zone(state.ZStack, 0)) != 0 {
		t.Fatalf("stack not empty after V.A.T.S. resolved: %v", e.G.Zone(state.ZStack, 0))
	}
	opts = e.legalActions(0)
	if !hasCastOption(opts, bolt) {
		t.Fatalf("Lightning Bolt not re-offered after V.A.T.S. left the stack: %+v", opts)
	}
	if !hasOptionKind(opts, "ability") {
		t.Fatalf("Golem Foundry's activated ability not re-offered after V.A.T.S. left the stack: %+v", opts)
	}
	replayCheck(t, e, cfg)
}

func TestMoltenDisasterKickedGrantWithholds(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	t.Run("kicked", func(t *testing.T) {
		e, cfg, _ := splitSecondEngine(t, reg, "Molten Disaster", "Lightning Bolt")
		md := searchMoveByName(t, e, "Molten Disaster", state.ZHand)
		bolt := searchMoveByName(t, e, "Lightning Bolt", state.ZHand)
		addMana(t, e, 0, "GGRRRR")
		// Cast the KICKED spell: the printed CDA static then grants the stack
		// object split second while it is on the stack.
		submitChoices(t, e, castModeOpt(t, e, md, "kicked"))
		if d := e.Pending(); d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
			t.Fatalf("expected the X announcement ask, got %+v", d)
		}
		submitChoices(t, e, 0) // X = 0
		splitSecondStackID(t, e, "Molten Disaster")
		if !e.splitSecondHolds() {
			t.Fatalf("kicked Molten Disaster did not grant split second (stack %v)", e.G.Zone(state.ZStack, 0))
		}
		opts := e.legalActions(0)
		if hasOptionKind(opts, "cast") || hasOptionKind(opts, "ability") {
			t.Fatalf("kicked Molten Disaster failed to withhold: %+v", opts)
		}
		// Release: the spell resolves (X = 0: zero damage) and casts return.
		submitPass(t, e)
		submitPass(t, e)
		if len(e.G.Zone(state.ZStack, 0)) != 0 {
			t.Fatalf("stack not empty after Molten Disaster resolved")
		}
		if !hasCastOption(e.legalActions(0), bolt) {
			t.Fatalf("Lightning Bolt not re-offered after Molten Disaster left the stack: %+v", e.legalActions(0))
		}
		replayCheck(t, e, cfg)
	})
	t.Run("unkicked control", func(t *testing.T) {
		e, cfg, _ := splitSecondEngine(t, reg, "Molten Disaster", "Lightning Bolt")
		md := searchMoveByName(t, e, "Molten Disaster", state.ZHand)
		bolt := searchMoveByName(t, e, "Lightning Bolt", state.ZHand)
		addMana(t, e, 0, "RRRR")
		// The control casts the PLAIN spell: no kick, no grant, casts offered.
		submitChoices(t, e, castModeOpt(t, e, md, ""))
		if d := e.Pending(); d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
			t.Fatalf("expected the X announcement ask, got %+v", d)
		}
		submitChoices(t, e, 0)
		if e.splitSecondHolds() {
			t.Fatalf("unkicked Molten Disaster must not hold split second")
		}
		if !hasCastOption(e.legalActions(0), bolt) {
			t.Fatalf("unkicked control withheld casts: %+v", e.legalActions(0))
		}
		replayCheck(t, e, cfg)
	})
}
