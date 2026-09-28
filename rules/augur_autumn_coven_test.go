package rules

// Ticket agent-20260918T233200Z-1c109bcc: Augur of Autumn's Coven gate --
// S:Mode$ Continuous | CheckSVar$ X | SVarCompare$ GE3 |
//   Affected$ Creature.TopLibrary+nonLand+YouCtrl | AffectedZone$ Library |
//   MayPlay$ True
// SVar:X:Count$Valid Creature.YouCtrl$DifferentCardPower
//
// The count head's $DifferentCardPower property was unread when the World
// Shaper (eoc commander precon) deck census filed the ticket, so X always
// read 0 and the Coven "may cast creature spells from the top of your
// library" static never activated. The Different* distinct-set family is
// implemented since diffcount1 (effects/count.go differentPropertyKindOf)
// and pinned at the evaluator level in effects/count_different_test.go; the
// statics side of the gate (CheckSVar$/SVarCompare$ on a may-play static) is
// pinned by TestVergeRangersOffersTopLibraryLandWhenOpponentAhead. What was
// missing is the card's own end-to-end leaf: the Coven gate OPENING with
// three creatures of different powers and staying SHUT on duplicates.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const augurCreatureTop = "Name:Elvish Guide\nManaCost:1 G\nTypes:Creature Elf\nPT:2/2\nOracle:x\n"

// augurCovenEngine builds an engine with the real Augur of Autumn on seat
// 0's battlefield and the given creature sources beneath it, a creature on
// top of seat 0's library and a sorcery beneath it.
func augurCovenEngine(t *testing.T, seed uint64, augur *cards.Card, creatures []string, topSrc string) (*Engine, *state.Object, *state.Object) {
	t.Helper()
	e, _, _ := newFixtureDeck(t, seed, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	ao := e.G.AddObject(augur, 0)
	ao.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), ao.ID))
	for _, src := range creatures {
		o := e.G.AddObject(card(t, src), 0)
		o.Zone = state.ZBattlefield
		e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), o.ID))
	}
	top := e.G.AddObject(card(t, topSrc), 0)
	beneath := e.G.AddObject(card(t, "Name:Blank Rite\nTypes:Sorcery\nOracle:x\n"), 0)
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{top.ID, beneath.ID},
		e.G.Zone(state.ZLibrary, 0)...))
	e.G.Players[0].Pool[state.MG] = 2 // {1}{G} for the top creature
	e.G.Step, e.G.Active, e.G.Priority, e.G.Turn = state.StepMain1, 0, 0, 1
	e.pending = nil
	e.Advance()
	return e, top, beneath
}

// augurCastOptions scans a pending priority decision for a mayplay cast offer
// naming obj and for any offer naming beneath (which must never be offered).
func augurCastOptions(t *testing.T, e *Engine, top, beneath *state.Object) (int, string) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority = %+v, want priority for seat 0", d)
	}
	castIdx, mode := -1, ""
	for _, o := range d.Options {
		if (o.Kind == "cast" || o.Kind == "play_land") && o.Obj == beneath.ID {
			t.Fatalf("non-top library card offered: %+v", o)
		}
		if o.Kind == "cast" && o.Obj == top.ID {
			castIdx, mode = o.Index, o.Mode
		}
	}
	return castIdx, mode
}

// TestAugurOfAutumnCovenGateActivatesWithDistinctPowers drives the positive
// arm end to end: three creatures of DIFFERENT powers on the battlefield make
// the distinct-power count read 3, the GE3 gate holds, and the creature on
// top of the library is offered as a may-play cast that resolves onto the
// battlefield.
func TestAugurOfAutumnCovenGateActivatesWithDistinctPowers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	augur := lookup(t, reg, "Augur of Autumn")
	if d := augur.Link(); len(d) != 0 {
		t.Fatalf("link Augur of Autumn: %v", d)
	}
	// PRECONDITION: the Coven count really is 3 on this board -- three
	// creatures with powers 1, 2 and 5. Assert the board state directly so a
	// silently-empty battlefield cannot pass the offer assertion for the
	// wrong reason.
	creatures := []string{
		"Name:Wisp\nManaCost:G\nTypes:Creature Spirit\nPT:1/1\nOracle:x\n",
		"Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n",
		"Name:Giant\nManaCost:3 R\nTypes:Creature Giant\nPT:5/5\nOracle:x\n",
	}
	e, top, beneath := augurCovenEngine(t, 231, augur, creatures, augurCreatureTop)
	if got := len(e.G.Zone(state.ZBattlefield, 0)); got != 4 {
		t.Fatalf("seat 0 battlefield holds %d permanents, want 4 (Augur + 3 creatures)", got)
	}
	powers := map[state.ObjID]int{}
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if f := e.G.Obj(id).Face(); f != nil {
			powers[id] = f.Power()
		}
	}
	distinct := map[int]bool{}
	for _, v := range powers {
		distinct[v] = true
	}
	if len(distinct) != 3 {
		t.Fatalf("board carries %d distinct powers (%v), want 3", len(distinct), powers)
	}

	castIdx, mode := augurCastOptions(t, e, top, beneath)
	if castIdx < 0 {
		t.Fatalf("three creatures with different powers: top-of-library creature not offered: %+v", e.Pending().Options)
	}
	if mode != "mayplay" {
		t.Fatalf("cast option mode %q, want mayplay (the Coven grant, not an ordinary hand cast)", mode)
	}
	if err := e.Submit(decision.Intent{Seq: e.Pending().Seq, Player: 0, Choices: []int{castIdx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}
	if top.Zone != state.ZStack {
		t.Fatalf("creature zone %v, want stack after the granted cast", top.Zone)
	}
	passUntilStackEmpty(t, e, 40)
	if top.Zone != state.ZBattlefield {
		t.Fatalf("creature zone %v, want battlefield after resolution", top.Zone)
	}
}

// TestAugurOfAutumnCovenGateWithheldOnDuplicatePowers drives the negative
// arms. NOTE the gate's own shape: Augur of Autumn is ITSELF a 2-power
// creature you control, so its power always joins the distinct set -- the
// arms below are chosen so the board's distinct-power set reads exactly 2.
// With three creatures where two share a power (Bear 2/Elf 2/Giant 5) the
// distinct set is {2, 5}; with only two creatures (Bear 2/Giant 5) it is
// {2, 5} as well -- in neither arm may the top-of-library creature be
// offered. (A two-creature arm whose powers both differ from Augur's own
// would read {Augur 2, x, y} = 3 and the Coven would legitimately open.)
func TestAugurOfAutumnCovenGateWithheldOnDuplicatePowers(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	augur := lookup(t, reg, "Augur of Autumn")

	// Arm 1: three creatures, powers 2/2/5 -- duplicates collapse the
	// distinct set to {Augur 2, 5}.
	dupes := []string{
		"Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n",
		"Name:Elf\nManaCost:1 G\nTypes:Creature Elf\nPT:2/2\nOracle:x\n",
		"Name:Giant\nManaCost:3 R\nTypes:Creature Giant\nPT:5/5\nOracle:x\n",
	}
	e, top, beneath := augurCovenEngine(t, 232, augur, dupes, augurCreatureTop)
	if got := len(e.G.Zone(state.ZBattlefield, 0)); got != 4 {
		t.Fatalf("seat 0 battlefield holds %d permanents, want 4", got)
	}
	if d := distinctCreaturePowers(t, e, 0); d != 2 {
		t.Fatalf("board's distinct creature powers = %d, want 2 (Augur 2, duplicates 2, Giant 5)", d)
	}
	if idx, _ := augurCastOptions(t, e, top, beneath); idx >= 0 {
		t.Fatalf("duplicate powers (distinct set {2,5}): top-of-library creature offered: %+v", e.Pending().Options)
	}

	// Arm 2: only two creatures, powers 2/5 -- the distinct set is {Augur 2,
	// 5} = 2, below the GE3 threshold even though the two creatures differ
	// from each other.
	two := []string{
		"Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n",
		"Name:Giant\nManaCost:3 R\nTypes:Creature Giant\nPT:5/5\nOracle:x\n",
	}
	e2, top2, beneath2 := augurCovenEngine(t, 233, augur, two, augurCreatureTop)
	if got := len(e2.G.Zone(state.ZBattlefield, 0)); got != 3 {
		t.Fatalf("seat 0 battlefield holds %d permanents, want 3 (Augur + 2 creatures)", got)
	}
	if d := distinctCreaturePowers(t, e2, 0); d != 2 {
		t.Fatalf("board's distinct creature powers = %d, want 2 (Augur 2, Bear 2, Giant 5)", d)
	}
	if idx, _ := augurCastOptions(t, e2, top2, beneath2); idx >= 0 {
		t.Fatalf("two creatures only (distinct set {2,5}): top-of-library creature offered: %+v", e2.Pending().Options)
	}
}

// distinctCreaturePowers counts the distinct FACE powers among a seat's
// battlefield creatures -- the precondition read for the Coven arms.
func distinctCreaturePowers(t *testing.T, e *Engine, p state.PlayerID) int {
	t.Helper()
	distinct := map[int]bool{}
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if f := o.Face(); f != nil && o.EffectiveIsCreature() {
			distinct[f.Power()] = true
		}
	}
	return len(distinct)
}

// TestAugurOfAutumnFullySupported pins the deck-side premise: the World
// Shaper census that filed the ticket measured Augur of Autumn's gap through
// the count head, and with the Different* family read the card's measured gap
// set is EMPTY -- a future deck import would admit it into the ratchet's
// supported set rather than into knownUnsupported.
func TestAugurOfAutumnFullySupported(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	augur := lookup(t, reg, "Augur of Autumn")
	if m := reg.Unsupported(augur, effects.Supported()); len(m) != 0 {
		t.Fatalf("Augur of Autumn still reports gaps %v, want none", m)
	}
}
