package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// The PutCardToLibFrom<Zone><N/Pos/Spec> cost family (Forge CostPutCardToLib).
// Before this landed, unknown=[PutCardToLibFromBattlefield] etc. -- every
// carrier's cost fell through ParseCost to the unrecognised-symbol fallback,
// priced as ONE GENERIC MANA, and the card was never actually moved. The
// family's carriers are:
//
//	PutCardToLibFromGrave    -- Battlefield Scrounger, Ardent Dustspeaker
//	  (plus rebalanced/a-ardent_dustspeaker), Anurid Scavenger, Gurzigost
//	PutCardToLibFromHand     -- Leashling, Hidden Retreat, Penance,
//	  Tainted Specter
//	PutCardToLibFromBattlefield -- Timestream Navigator
//
// These are DISTINCT from the cumulative-upkeep PutCardToLibFromSameGrave
// action (rules/cumulative.go), which is a keyword-expansion action rather
// than a parsed Cost$ token, and from Forge's PutCardToLibFromSameGrave
// spelling (not matched by the cost regex on purpose).
//
// The library POSITION is the token's middle field: -1 is the bottom (Forge
// CostPutCardToLib's "-1"; Timestream Navigator, Battlefield Scrounger, Ardent
// Dustspeaker) and 0 is the top (Leashling, Hidden Retreat, Penance, Tainted
// Specter). MoveZone appends to the destination zone, so a plain move is the
// bottom; a top placement follows the move with one LibraryOrder per owner
// (settlePutToLibCost/putLibPicksOnTop).

// putToLibAbilityIndex finds the first activated ability whose parsed cost
// carries a PutToLib part, so the tests anchor on the compiled SA rather than
// a hand-authored one.
func putToLibAbilityIndex(t *testing.T, e *Engine, id state.ObjID) int {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatalf("source %d has no face", id)
	}
	for i, sa := range o.Face().Abilities {
		if len(ParseCost(sa.Params["Cost"]).PutToLib) > 0 {
			return i
		}
	}
	t.Fatalf("%s has no PutCardToLibFrom* ability", o.Face().Name)
	return -1
}

// TestParseCostModelsPutCardToLibTokens pins the grammar on the exact corpus
// strings and asserts Cost.Unknown no longer names any of the three heads.
func TestParseCostModelsPutCardToLibTokens(t *testing.T) {
	t.Parallel()
	cases := []struct {
		src  string
		n    int32
		zone state.Zone
		pos  int32
		spec string
	}{
		// Timestream Navigator (the census that found this): the source
		// itself, bottom of its owner's library.
		{"2 U U T PutCardToLibFromBattlefield<1/-1/CARDNAME>", 1, state.ZBattlefield, -1, "CARDNAME"},
		// Battlefield Scrounger: three cards from the graveyard, bottom.
		{"PutCardToLibFromGrave<3/-1/Card>", 3, state.ZGraveyard, -1, "Card"},
		// Ardent Dustspeaker: the ";" OR alternation folds to ",".
		{"PutCardToLibFromGrave<1/-1/Sorcery;Instant>", 1, state.ZGraveyard, -1, "Sorcery,Instant"},
		// Leashling/Penance/Tainted Specter: a hand card on TOP.
		{"PutCardToLibFromHand<1/0/Card>", 1, state.ZHand, 0, "Card"},
	}
	for _, tc := range cases {
		c := ParseCost(tc.src)
		if len(c.Unknown) != 0 {
			t.Errorf("ParseCost(%q).Unknown = %v, want none", tc.src, c.Unknown)
		}
		if len(c.PutToLib) != 1 {
			t.Errorf("ParseCost(%q).PutToLib = %+v, want exactly one part", tc.src, c.PutToLib)
			continue
		}
		part := c.PutToLib[0]
		if part.N != tc.n || part.Zone != tc.zone || part.LibraryPos != tc.pos || part.Spec != tc.spec {
			t.Errorf("ParseCost(%q).PutToLib[0] = %+v, want N=%d Zone=%d Pos=%d Spec=%q",
				tc.src, part, tc.n, tc.zone, tc.pos, tc.spec)
		}
		// The composed cost round-trips through formatCost (the wire/debug form).
		if got := ParseCost(formatCost(c)).PutToLib; len(got) != 1 || got[0] != part {
			t.Errorf("ParseCost(formatCost(%q)) = %q did not round-trip part %+v (got %+v)",
				tc.src, formatCost(c), part, got)
		}
	}
	// A recognised head with an INSTANCE this build cannot place -- an
	// out-of-range library position -- degrades to one generic and reports
	// the head, exactly like every other malformed cost token.
	if got := ParseCost("PutCardToLibFromGrave<1/7/Card>"); len(got.Unknown) != 1 || got.Unknown[0] != "PutCardToLibFromGrave" {
		t.Errorf("out-of-range position Unknown = %v, want [PutCardToLibFromGrave]", got.Unknown)
	}
	// An unnamed zone head is NOT modelled (the positive zone list), so it
	// still reports rather than silently landing in a wrong zone.
	if got := ParseCost("PutCardToLibFromExile<1/-1/Card>"); len(got.Unknown) != 1 || got.Unknown[0] != "PutCardToLibFromExile" {
		t.Errorf("unknown zone head Unknown = %v, want [PutCardToLibFromExile]", got.Unknown)
	}
}

// TestPutCardToLibFromBattlefieldMovesSourceToBottom pins Timestream
// Navigator's compiled activation end to end: {2}{U}{U}, {T}, Put Timestream
// Navigator on the bottom of its owner's library. Its Activation$ Blessing
// gate fails closed (there is no city's-blessing state), so the offer is
// deliberately bypassed by calling beginActivation directly, the way
// TestCardnameSacrificeCostCannotUseAnotherCopy reaches a stale activation --
// the gate is a separate, known concern, and the COST path is what this test
// owns.
func TestPutCardToLibFromBattlefieldMovesSourceToBottom(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Timestream Navigator")
	id := searchMoveByName(t, e, "Timestream Navigator", state.ZBattlefield)
	addMana(t, e, 0, "UUUU")
	idx := putToLibAbilityIndex(t, e, id)
	libBefore := len(e.G.Zone(state.ZLibrary, 0))

	e.beginActivation(0, decision.Option{Obj: id, Ability: idx})
	if e.cast != nil {
		t.Fatalf("activation did not complete payment in one pass: cast pending %+v", e.cast)
	}
	if got := e.G.Obj(id).Zone; got != state.ZLibrary {
		t.Fatalf("Timestream Navigator zone = %v, want library", got)
	}
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) != libBefore+1 {
		t.Fatalf("library grew by %d, want 1", len(lib)-libBefore)
	}
	if lib[len(lib)-1] != id {
		t.Fatalf("Timestream Navigator is not the BOTTOM card of its owner's library (lib[%d] != %d)", len(lib)-1, id)
	}
	if !hasEvent(e, events.MoveZone, id) || !hasEvent(e, events.AbilityPush, id) {
		t.Fatal("expected a MoveZone and an AbilityPush for the paid activation")
	}
	resolveAndReplay(t, e, cfg)
}

// TestPutCardToLibSelfCostLeavesNoStaleTap pins the ORDER of the {T} tap and
// the self-placement settle: a cost carrying BOTH T and
// PutCardToLibFromBattlefield<1/-1/CARDNAME> (the only corpus shape is
// Timestream Navigator) must emit the tap while the source is still on the
// battlefield. Settling first left the library card Tapped -- a state that
// does not exist (CR 110.5) -- and because a Move's library→battlefield ENTRY
// arm does not clear Tapped, a direct put-onto-battlefield later re-entered
// the permanent TAPPED.
func TestPutCardToLibSelfCostLeavesNoStaleTap(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Timestream Navigator")
	id := searchMoveByName(t, e, "Timestream Navigator", state.ZBattlefield)
	addMana(t, e, 0, "UUUU")
	idx := putToLibAbilityIndex(t, e, id)

	e.beginActivation(0, decision.Option{Obj: id, Ability: idx})
	if e.cast != nil {
		t.Fatalf("activation did not complete payment in one pass: cast pending %+v", e.cast)
	}
	o := e.G.Obj(id)
	if o.Zone != state.ZLibrary {
		t.Fatalf("Timestream Navigator zone = %v, want library", o.Zone)
	}
	if o.Tapped {
		t.Fatal("the moved library card is Tapped -- tapping a card in a library is not a state that exists (CR 110.5)")
	}
	// A direct library→battlefield move (any ChangeZone Origin$ Library
	// Destination$ Battlefield) must not bring the permanent back in TAPPED.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	if o := e.G.Obj(id); o.Tapped {
		t.Fatal("STALE TAP: the permanent re-entered the battlefield Tapped after its self-placement cost")
	}
	resolveAndReplay(t, e, cfg)
}

// TestPutCardToLibFromHandChoosesCardAndPutsItOnTop pins Leashling's compiled
// activation through the REAL offer/ask/pay flow: its cost is
// PutCardToLibFromHand<1/0/Card> (no Activation$ gate), so the ability is
// offered, the player picks a hand card, and that card goes on TOP of the
// library while Leashling returns to hand on resolution.
func TestPutCardToLibFromHandChoosesCardAndPutsItOnTop(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Leashling")
	id := searchMoveByName(t, e, "Leashling", state.ZBattlefield)
	e.Advance()
	idx := putToLibAbilityIndex(t, e, id)
	opt := abilityOption(t, e, id, idx)

	// Pick a specific hand card BEFORE paying, and capture its current
	// library position (it is a hidden-zone move; index 0 is the top).
	hand := e.G.Zone(state.ZHand, 0)
	if len(hand) < 2 {
		t.Fatalf("hand holds %d cards, want at least 2 for a real choice", len(hand))
	}
	want := hand[len(hand)-1]

	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the puttolibcost KChoose, got %+v", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Kind != "puttolibcost" {
			t.Fatalf("option %+v is not a puttolibcost option", o)
		}
		if o.Obj == want {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("the chosen hand card %d is not among the offered options %+v", want, d.Options)
	}
	submitChoices(t, e, pick)

	if got := e.G.Obj(want).Zone; got != state.ZLibrary {
		t.Fatalf("chosen hand card zone = %v, want library", got)
	}
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) == 0 || lib[0] != want {
		t.Fatalf("chosen hand card %d is not the TOP of the library (lib[0]=%v)", want, lib[:1])
	}
	resolveAndReplay(t, e, cfg)
}

// TestPutCardToLibFromGraveChoosesAndPutsOnBottom pins Battlefield Scrounger's
// compiled activation through beginActivation (its Activation$ Threshold gate
// fails closed below seven graveyard cards, so the offer is bypassed): a
// THREE-card graveyard pick (Min=Max=N over more candidates than N, so a real
// choice is posed) goes to the bottom of the library.
func TestPutCardToLibFromGraveChoosesAndPutsOnBottom(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Battlefield Scrounger")
	id := searchMoveByName(t, e, "Battlefield Scrounger", state.ZBattlefield)
	// Seed four graveyard cards so N=3 is a strict subset (a real pick).
	var seeded []state.ObjID
	for _, oid := range e.G.Zone(state.ZLibrary, 0) {
		if len(seeded) == 4 {
			break
		}
		o := e.G.Obj(oid)
		if o != nil && o.Face() != nil && o.Face().Name == "Mountain" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: oid, From: state.ZLibrary, To: state.ZGraveyard})
			seeded = append(seeded, oid)
		}
	}
	if len(seeded) != 4 {
		t.Fatalf("seeded %d graveyard cards, want 4", len(seeded))
	}
	idx := putToLibAbilityIndex(t, e, id)

	e.beginActivation(0, decision.Option{Obj: id, Ability: idx})
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 3 || d.Max != 3 {
		t.Fatalf("expected the Min=Max=3 puttolibcost ask, got %+v", d)
	}
	want := seeded[:3]
	var picks []int
	for _, w := range want {
		for _, o := range d.Options {
			if o.Obj == w {
				picks = append(picks, o.Index)
			}
		}
	}
	if len(picks) != 3 {
		t.Fatalf("did not find all three chosen graveyard cards among %+v", d.Options)
	}
	submitChoices(t, e, picks...)

	lib := e.G.Zone(state.ZLibrary, 0)
	tail := lib[len(lib)-3:]
	for i, w := range want {
		if tail[i] != w {
			t.Fatalf("bottom of library = %v, want the chosen %v in pick order", tail, want)
		}
		if got := e.G.Obj(w).Zone; got != state.ZLibrary {
			t.Fatalf("chosen graveyard card %d zone = %v, want library", w, got)
		}
	}
	resolveAndReplay(t, e, cfg)
}

// resolveAndReplay drains any pushed ability to completion (so the moved
// card's own later effect runs) and then re-derives the whole game from the
// log, proving the new cost events replay byte-identically.
func resolveAndReplay(t *testing.T, e *Engine, cfg Config) {
	t.Helper()
	passUntilStackEmpty(t, e, 30)
	replayCheck(t, e, cfg)
}

// TestPutCardToLibOfferGateRequiresCandidates pins the offer half of the
// class: nonManaCastable withholds a PutToLib cost whose zone cannot supply
// N matching cards, and a Battlefield CARDNAME part tracks the source itself
// on and off the battlefield. Without this the cost would be offered and only
// abort at payment time (an illegal game action, the direction the engine's
// offer gate exists to prevent).
func TestPutCardToLibOfferGateRequiresCandidates(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Battlefield Scrounger")
	id := searchMoveByName(t, e, "Battlefield Scrounger", state.ZBattlefield)
	if pay.NonManaCastable(asPayer(e), 0, id, ParseCost("PutCardToLibFromGrave<99/-1/Card>"), true, "") {
		t.Fatal("offered a graveyard PutCardToLib cost the graveyard cannot pay")
	}
	if !pay.NonManaCastable(asPayer(e), 0, id, ParseCost("PutCardToLibFromBattlefield<1/-1/CARDNAME>"), true, "") {
		t.Fatal("withheld a self PutCardToLib cost while the source is in play")
	}
	// The singleton self-reference fast path only covers N=1: with a larger N
	// the offer gate must fall through to the general candidate walk, which
	// cannot supply CARDNAME twice, rather than offering on the source alone
	// and aborting at payment time. (0 corpus carriers -- latent.)
	if pay.NonManaCastable(asPayer(e), 0, id, ParseCost("PutCardToLibFromBattlefield<2/-1/CARDNAME>"), true, "") {
		t.Fatal("offered a self PutCardToLib cost needing 2 candidates when only the source matches CARDNAME")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	if pay.NonManaCastable(asPayer(e), 0, id, ParseCost("PutCardToLibFromBattlefield<1/-1/CARDNAME>"), true, "") {
		t.Fatal("offered a self PutCardToLib cost while the source is not in play")
	}
}
