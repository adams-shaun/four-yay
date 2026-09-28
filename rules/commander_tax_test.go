package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// commanderBeatstickSrc and commanderBattleGolemSrc are the m31 fixture
// commanders: minimal, behaviour-free 1/1 creatures with a {0} cost and no
// abilities. A creature is sorcery-speed (no Flash), so the command-zone cast
// only ever fires in a main phase -- which driveToStep's StepMain1 provides.
// Giving them no abilities keeps casting and resolution decision-free, which
// is exactly what the tax tests need (nothing to target, nothing to choose).
const (
	commanderBeatstickSrc = `Name:Beatstick
ManaCost:0
Types:Legendary Creature Zombie
PT:1/1
K:Partner
Oracle:x
`
	commanderBattleGolemSrc = `Name:Battle Golem
ManaCost:0
Types:Legendary Creature Golem
PT:1/1
K:Partner
Oracle:x
`
)

// commanderDeck builds a 40-card deck whose index 0 is a single commander.
func commanderDeck(t *testing.T, cmdSrc string) []*cards.Card {
	t.Helper()
	cmd := card(t, cmdSrc)
	return append([]*cards.Card{cmd}, mountainDeck(t, 39)...)
}

// twoCommanderDeck builds a 40-card deck whose indices 0 and 1 are two
// distinct commanders (used by the replay test, which casts each once).
func twoCommanderDeck(t *testing.T, cmd0Src, cmd1Src string) []*cards.Card {
	t.Helper()
	c0 := card(t, cmd0Src)
	c1 := card(t, cmd1Src)
	return append([]*cards.Card{c0, c1}, mountainDeck(t, 38)...)
}

// commanderTaxGame is the m31 commander-tax fixture, now a thin wrapper over
// the ONE shared commanderGame (rules/commander_damage_test.go): both seats
// lead their deck with the same decision-free Beatstick commander (one per
// seat, at deck index 0), life is the default 20 (StartingLife 0 -- the zero
// value New maps to 20), and the game is driven to turn 1 Main1, the only
// point a command-zone cast can be offered (a creature is sorcery-speed: no
// Flash). The wrapper exists because the tax suite needs a DRIVEN session
// with the commander object extracted, while the shared fixture deliberately
// returns the bare, un-advanced engine the damage and zone-replacement suites
// hand-drive (their swing / fieldCommander helpers write Active/Step
// themselves); one function serving both postures would have to hide that
// driving behind a flag and return a third value, which makes the fixture's
// contract ambiguous. The seed passes through untouched, so each tax test
// keeps the exact stream it always measured.
func commanderTaxGame(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg := commanderGame(t, seed, FormatCommander, 0, [][]string{{commanderBeatstickSrc}, {commanderBeatstickSrc}})
	e.Advance()
	driveToStep(t, e, 1, 0, state.StepMain1)
	return e, cfg, e.G.Players[0].Commanders[0]
}

// commanderCastOption returns the current priority decision's "cast" option
// for id, or nil if it is not offered.
func commanderCastOption(e *Engine, id state.ObjID) *decision.Option {
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		return nil
	}
	for i := range d.Options {
		if d.Options[i].Kind == "cast" && d.Options[i].Obj == id {
			return &d.Options[i]
		}
	}
	return nil
}

// fundAndRefresh funds seat 0's pool with `generic` colourless mana and then
// regenerates seat 0's Main1 priority so the pending decision reflects the
// funded pool (the offer gates on castable, which gates on the pool).
func fundAndRefresh(t *testing.T, e *Engine, generic int32) {
	t.Helper()
	e.G.Players[0].Pool[state.MC] = generic
	e.pending = nil
	e.Advance()
}

// castCommanderAndReturn drives ONE full command-zone cast cycle through the
// real legal-action path: it picks the "cast" option for id out of the pending
// priority decision, casts it, drains the stack (the commander resolves onto
// the battlefield), then returns the commander to the command zone. The
// MoveZone back to ZCommand is a test fixture -- the CR 903.9
// "return a commander to the command zone" replacement is a later milestone
// (m33) -- so it stands in for the post-return game state rather than
// inventing that rule here.
func castCommanderAndReturn(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	opt := commanderCastOption(e, id)
	if opt == nil {
		t.Fatalf("no command-zone cast option for commander %d: %+v", id, e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 30)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZCommand})
	e.pending = nil
	e.Advance()
}

// castCommanderOnce casts id from the command zone and drains the stack, but
// does NOT return it to the command zone. Used only by the replay test, where
// every event must be produced by the recorded Intents alone (a fixture
// MoveZone back would not replay).
func castCommanderOnce(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	opt := commanderCastOption(e, id)
	if opt == nil {
		t.Fatalf("no command-zone cast option for commander %d: %+v", id, e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 30)
}

// TestCommanderCastTaxesEveryCastBeyondTheFirst is the CR 903.8 money test:
// the first cast from the command zone is untaxed, the second costs {2} more
// and the third {4} more, CmdCasts increments on each, and each additional
// cost actually lands in the legality gate (an unfunded {2}-taxed cast is not
// offered at all).
func TestCommanderCastTaxesEveryCastBeyondTheFirst(t *testing.T) {
	t.Parallel()
	e, _, cmd0 := commanderTaxGame(t, 31)

	// First cast: untaxed (CmdCasts[0] == 0), offered with an empty pool.
	if c := e.commanderTaxFor(0, cmd0, Cost{}); c.Generic != 0 {
		t.Fatalf("first cast tax = %+v, want untaxed", c)
	}
	if commanderCastOption(e, cmd0) == nil {
		t.Fatalf("first command-zone cast not offered (empty pool, untaxed cost must still cast): %+v", e.Pending().Options)
	}
	castCommanderAndReturn(t, e, cmd0)
	if got := e.G.Players[0].CmdCasts[0]; got != 1 {
		t.Fatalf("CmdCasts[0] after first cast = %d, want 1", got)
	}

	// Second cast: {2} more. With the pool empty the taxed cast is NOT
	// offered -- legality and payment agree, both from commanderTaxFor.
	if commanderCastOption(e, cmd0) != nil {
		t.Fatalf("{2}-taxed cast offered with no mana to pay it: %+v", e.Pending().Options)
	}
	if c := e.commanderTaxFor(0, cmd0, Cost{}); c.Generic != 2 {
		t.Fatalf("second cast tax = %+v, want {2}", c)
	}
	fundAndRefresh(t, e, 2)
	if c := e.commanderTaxFor(0, cmd0, Cost{}); c.Generic != 2 {
		t.Fatalf("second cast tax after funding = %+v, want {2}", c)
	}
	if commanderCastOption(e, cmd0) == nil {
		t.Fatalf("funded second command-zone cast not offered: %+v", e.Pending().Options)
	}
	castCommanderAndReturn(t, e, cmd0)
	if got := e.G.Players[0].CmdCasts[0]; got != 2 {
		t.Fatalf("CmdCasts[0] after second cast = %d, want 2", got)
	}

	// Third cast: {4} more.
	fundAndRefresh(t, e, 4)
	if c := e.commanderTaxFor(0, cmd0, Cost{}); c.Generic != 4 {
		t.Fatalf("third cast tax = %+v, want {4}", c)
	}
	if commanderCastOption(e, cmd0) == nil {
		t.Fatalf("funded third command-zone cast not offered: %+v", e.Pending().Options)
	}
	castCommanderAndReturn(t, e, cmd0)
	if got := e.G.Players[0].CmdCasts[0]; got != 3 {
		t.Fatalf("CmdCasts[0] after third cast = %d, want 3", got)
	}

	// CmdCasts must survive Clone (the bound slice is deep-copied by
	// Game.Clone; the engine is at an intent boundary after castCommanderAndReturn).
	if c := e.Clone(); c.G.Players[0].CmdCasts[0] != 3 {
		t.Fatalf("cloned engine CmdCasts[0] = %d, want 3", c.G.Players[0].CmdCasts[0])
	}
}

// TestCommanderCastPathActuallyPaysTheTax is the till-side test the other
// commander tests miss: every one of them asserts on commanderTaxFor's RETURN
// (the calculator) or on the offer, so a beginCast that computes the tax and
// never spends it passes the whole suite. This test asserts on what the pool
// ACTUALLY PAID through the real decision path -- the mana commitCast deducted
// when a seat's cast intent resolves -- so the tax must be applied by the cast
// flow itself, not merely derived. Fund exactly the taxed cost, cast, and
// require the pool to fall by exactly that cost: {0} base + {2} tax on the
// second command-zone cast and {4} on the third. Delete the
// `cost = e.commanderTaxFor(p, id, cost)` line in beginCast and this fails by
// name -- the tax function still returns {2}/{4}, but commitCast pays {0}.
func TestCommanderCastPathActuallyPaysTheTax(t *testing.T) {
	t.Parallel()
	e, _, cmd0 := commanderTaxGame(t, 38)

	// First cast from the command zone: {0} base cost, tax {0}. The option is
	// offered with an empty pool (castable holds for zero), and the cast pays
	// nothing -- the untaxed baseline the second cast must exceed by exactly
	// the {2} tax.
	castCommanderAndReturn(t, e, cmd0)

	// Second cast: CR 903.8 adds {2} on top of the {0} base. Fund exactly the
	// taxed cost through the real path, and assert on the deduction, not on
	// commanderTaxFor's return.
	fundAndRefresh(t, e, 2)
	opt := commanderCastOption(e, cmd0)
	if opt == nil {
		t.Fatalf("funded second command-zone cast not offered: %+v", e.Pending().Options)
	}
	before := e.G.Players[0].Pool[state.MC]
	submitChoices(t, e, opt.Index)
	if paid := before - e.G.Players[0].Pool[state.MC]; paid != 2 {
		t.Fatalf("second command-zone cast paid %d mana from the pool, want exactly 2 ({0} base + {2} tax)", paid)
	}
	passUntilStackEmpty(t, e, 30)
	e.emit(events.Event{Kind: events.MoveZone, Obj: cmd0, From: state.ZBattlefield, To: state.ZCommand})
	e.pending = nil
	e.Advance()

	// Third cast: the tax scales -- two prior command-zone casts push it to
	// {4}, so a tax that is applied once but never grows would fail HERE.
	fundAndRefresh(t, e, 4)
	opt = commanderCastOption(e, cmd0)
	if opt == nil {
		t.Fatalf("funded third command-zone cast not offered: %+v", e.Pending().Options)
	}
	before = e.G.Players[0].Pool[state.MC]
	submitChoices(t, e, opt.Index)
	if paid := before - e.G.Players[0].Pool[state.MC]; paid != 4 {
		t.Fatalf("third command-zone cast paid %d mana from the pool, want exactly 4 ({0} base + {4} tax)", paid)
	}
}

// TestCommanderCounteredSpellStillRaisesTheTax pins the cast-time (not
// resolve-time) increment: CmdCasts is incremented the instant the spell is
// put on the stack, so a commander spell that is countered before it resolves
// still pushes the next cast's tax up by {2}.
func TestCommanderCounteredSpellStillRaisesTheTax(t *testing.T) {
	t.Parallel()
	e, _, cmd0 := commanderTaxGame(t, 33)

	opt := commanderCastOption(e, cmd0)
	if opt == nil {
		t.Fatalf("first command-zone cast not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)

	// The spell is on the stack, not resolved, and the counter already moved.
	if e.G.Obj(cmd0).Zone != state.ZStack {
		t.Fatalf("spell should still be on the stack, is in %s", e.G.Obj(cmd0).Zone)
	}
	if got := e.G.Players[0].CmdCasts[0]; got != 1 {
		t.Fatalf("CmdCasts[0] at cast time = %d, want 1 (incremented before resolution)", got)
	}

	// Counter it: the spell goes to the graveyard instead of resolving.
	e.emit(events.Event{Kind: events.MoveZone, Obj: cmd0, From: state.ZStack, To: state.ZGraveyard})
	if got := e.G.Players[0].CmdCasts[0]; got != 1 {
		t.Fatalf("CmdCasts[0] after the counter = %d, want 1 (a counter does not undo a cast)", got)
	}

	// Returned to the command zone, the next cast is now taxed {2}.
	e.emit(events.Event{Kind: events.MoveZone, Obj: cmd0, From: state.ZGraveyard, To: state.ZCommand})
	if c := e.commanderTaxFor(0, cmd0, Cost{}); c.Generic != 2 {
		t.Fatalf("tax after a countered cast = %+v, want {2}", c)
	}
}

// TestCommanderCastFromHandIsNeitherTaxedNorCounted pins the from-command-zone-
// only condition: casting the commander from the hand is a plain cast -- no
// tax, and no increment.
func TestCommanderCastFromHandIsNeitherTaxedNorCounted(t *testing.T) {
	t.Parallel()
	e, _, cmd0 := commanderTaxGame(t, 34)

	// The commander is in the command zone; move it to the hand (a fixture --
	// getting it there only ever happens through some other effect). The
	// engine route is not available: emitting that MoveZone is itself a CR
	// 903.9 event ("from anywhere" includes the command zone, so the owner
	// would be asked whether to keep the commander in the command zone
	// instead), so the zone lists are written directly -- the fixture
	// convention commander_zone_replacement_test.go uses for the same
	// reason (m32 fix round 1: this test predates the replacement, and its
	// raw emit of a replaceable event was what the CR 903.9 change broke).
	e.G.Obj(cmd0).Zone = state.ZHand
	e.G.SetZone(state.ZCommand, 0, withoutID(e.G.Zone(state.ZCommand, 0), cmd0))
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), cmd0))
	e.pending = nil
	e.Advance()

	if c := e.commanderTaxFor(0, cmd0, Cost{}); c.Generic != 0 {
		t.Fatalf("hand cast taxed = %+v, want untaxed (tax applies to command-zone casts only)", c)
	}
	opt := commanderCastOption(e, cmd0)
	if opt == nil {
		t.Fatalf("hand cast not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Players[0].CmdCasts[0]; got != 0 {
		t.Fatalf("CmdCasts[0] after a hand cast = %d, want 0 (not incremented from the hand)", got)
	}
}

// TestCommanderNoOfferWithoutPayableTax is the legality/payment agreement
// test: legalActions offers a command-zone cast exactly when castable holds
// for the SAME taxed cost beginCast charges. Unfunded the offer is absent;
// funded it is present; and the exact cost derived by commanderTaxFor is
// castable. An offer without a payable cost -- the shape stalledCastLimit used
// to bound -- is structurally impossible here because both sides derive from
// commanderTaxFor.
func TestCommanderNoOfferWithoutPayableTax(t *testing.T) {
	t.Parallel()
	e, _, cmd0 := commanderTaxGame(t, 35)

	// Get the tax to {2} through a real cast+return.
	castCommanderAndReturn(t, e, cmd0)
	if c := e.commanderTaxFor(0, cmd0, Cost{}); c.Generic != 2 {
		t.Fatalf("precondition: tax should be {2}, got %+v", c)
	}

	// Empty pool: not offered.
	if commanderCastOption(e, cmd0) != nil {
		t.Fatalf("taxed cast offered with no mana: %+v", e.Pending().Options)
	}
	// Funded: offered.
	fundAndRefresh(t, e, 2)
	if commanderCastOption(e, cmd0) == nil {
		t.Fatal("taxed cast not offered once funded")
	}
	// And the exact cost commanderTaxFor derives is castable.
	base := e.adjustedCost(0, cmd0)
	taxed := e.commanderTaxFor(0, cmd0, base)
	if !e.castable(0, cmd0, taxed, false) {
		t.Fatalf("offered cast not castable: adjusted %+v taxed %+v", base, taxed)
	}
}

// TestCommanderCastCountReplaysExactlyFromTheEventStream proves CmdCasts is
// derived state: a faithful replay from the recorded Intents alone (replayFor)
// lands on the identical count. Two distinct commanders are each cast once
// FROM the command zone through the real decision path with no fixture
// MoveZone between them, so every event the live game emitted is one the
// replay re-produces; the replayed engine's CmdCasts must equal the live one's.
func TestCommanderCastCountReplaysExactlyFromTheEventStream(t *testing.T) {
	t.Parallel()
	deck0 := twoCommanderDeck(t, commanderBeatstickSrc, commanderBattleGolemSrc)
	deck1 := twoCommanderDeck(t, commanderBeatstickSrc, commanderBattleGolemSrc)
	cfg := Config{Seed: 36, Names: []string{"a", "b"},
		Decks:      [][]*cards.Card{deck0, deck1},
		Commanders: [][]int{{0, 1}, {0, 1}},
		Format:     FormatCommander,
	}
	e := New(cfg)
	e.Advance()
	driveToStep(t, e, 1, 0, state.StepMain1)
	cmd0, cmd1 := e.G.Players[0].Commanders[0], e.G.Players[0].Commanders[1]

	// Cast commander 0 then commander 1, both from the command zone. Each is
	// a first cast, so both are free; neither is returned to the command zone.
	castCommanderOnce(t, e, cmd0)
	castCommanderOnce(t, e, cmd1)

	want := [2]int32{1, 1}
	if e.G.Players[0].CmdCasts[0] != want[0] || e.G.Players[0].CmdCasts[1] != want[1] {
		t.Fatalf("live CmdCasts = %v, want %v", e.G.Players[0].CmdCasts, want)
	}

	re, err := replayFor(cfg, e.L)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if re.G.Players[0].CmdCasts[0] != want[0] || re.G.Players[0].CmdCasts[1] != want[1] {
		t.Fatalf("replayed CmdCasts = %v, want %v (derived from the event stream alone)", re.G.Players[0].CmdCasts, want)
	}
	if re.L.Head() != e.L.Head() {
		t.Fatalf("chain differs: live %s replay %s", e.L.Head(), re.L.Head())
	}
}

// TestCommanderTaxGatedOffOutsideCommanderFormat is the explicit-format-gate
// test. A Constructed-format game (Format left at its zero value) with a
// commander deliberately present in the command zone (m30 genesis places
// commanders on Config.Commanders regardless of format) must exercise NONE of
// the Commander rules -- not because the command zone is empty (it is not),
// but because the format gate says so. All three enforcement points are
// covered: the offer walk, the tax composition and the counter increment.
func TestCommanderTaxGatedOffOutsideCommanderFormat(t *testing.T) {
	t.Parallel()
	deck0, deck1 := commanderDeck(t, commanderBeatstickSrc), commanderDeck(t, commanderBeatstickSrc)
	cfg := Config{Seed: 37, Names: []string{"a", "b"},
		Decks:      [][]*cards.Card{deck0, deck1},
		Commanders: [][]int{{0}, {0}},
		// Format deliberately NOT set: FormatConstructed is the zero value.
	}
	// Seat 0 is the protagonist; seatZeroStart advances the seed until the
	// CR 103.1 toss starts seat 0 (the effective seed travels in cfg).
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	driveToStep(t, e, 1, 0, state.StepMain1)
	cmd0 := e.G.Players[0].Commanders[0]
	if len(e.G.Zone(state.ZCommand, 0)) != 1 {
		t.Fatal("precondition: the commander must be present in the command zone of a Constructed game")
	}

	// (a) The command-zone offer walk is gated: the cast is not offered even
	// though the commander is right there.
	if commanderCastOption(e, cmd0) != nil {
		t.Fatalf("command-zone cast offered in Constructed format: %+v", e.Pending().Options)
	}
	// (b) The tax composition is gated: commanderTaxFor passes the base
	// through unchanged.
	if c := e.commanderTaxFor(0, cmd0, Cost{}); c.Generic != 0 {
		t.Fatalf("tax composed in Constructed format = %+v, want 0", c)
	}
	// (c) The counter is gated: even a direct increment does nothing.
	e.recordCmdCast(0, cmd0)
	if got := e.G.Players[0].CmdCasts[0]; got != 0 {
		t.Fatalf("recordCmdCast in Constructed format set CmdCasts[0] = %d, want 0", got)
	}
	// (d) The tax composition gate is independent of the counter, not the
	// counter being zero: even a non-zero prior cast (here seeded as a
	// fixture) must not produce tax in Constructed. This is the guard whose
	// deletion a bare "CmdCasts happens to be 0 in Constructed" assertion
	// would silently pass.
	e.G.Players[0].CmdCasts[0] = 1
	if c := e.commanderTaxFor(0, cmd0, Cost{}); c.Generic != 0 {
		t.Fatalf("tax composed in Constructed format with a prior cast = %+v, want 0 (format gate suppresses it)", c)
	}
}
