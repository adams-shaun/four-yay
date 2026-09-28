package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The ChangeNum$ pin for api:ChangeZoneAll.ChangeNum. effChangeZoneAll always
// swept every ChangeType$ match and never read the count; the corpus writes
// ChangeNum$ All (Expert-Level Safe's DBOpenSafe) and a numeric cap
// (bone_dancer's DBChangeZone: ChangeNum$ 1).

// changeNumExileFixture puts a seat-0 battlefield source stand-in down and
// exiles n seat-0 hand cards WITH the source provenance the real face-down
// exile path records (the MoveZone IDs carrier that events.Apply folds into
// o.ExiledWith), so Card.ExiledWithSource matches them against that source.
// It returns the exiled ids in exile order -- the sweep's scan order for
// Origin$ Exile under seat 0 -- and leaves the game at a live priority round.
func changeNumExileFixture(t *testing.T, e *Engine, src state.ObjID, n int) []state.ObjID {
	t.Helper()
	var ids []state.ObjID
	for i := 0; i < n; i++ {
		id := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZExile,
			IDs: []state.ObjID{src}})
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZExile {
			t.Fatalf("fixture exile did not land: card %d in %+v", id, o)
		}
		ids = append(ids, id)
	}
	return ids
}

// inHand reports whether id sits in seat 0's hand.
func inHand(e *Engine, id state.ObjID) bool {
	for _, x := range e.G.Zone(state.ZHand, 0) {
		if x == id {
			return true
		}
	}
	return false
}

// TestExpertLevelSafeChangeNumAllReturnsEveryExiledCard is the brief's named
// real-card pin: Expert-Level Safe's matched branch (DBSacrifice ->
// DBOpenSafe) writes `DB$ ChangeZoneAll | ChangeNum$ All | Origin$ Exile |
// Destination$ Hand | ChangeType$ Card.ExiledWithSource`, and "All" must
// return EVERY card exiled with the Safe -- which is what the primitive did
// before the param read, so this test pins that the All spelling keeps the
// uncapped sweep rather than degrading with the numeric branch.
func TestExpertLevelSafeChangeNumAllReturnsEveryExiledCard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	safe := searchCorpusCard(t, reg, "Expert-Level Safe")
	e, cfg := mordorEngine(t, reg, 911, "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears")
	src := moveToBattlefieldByName(t, e, 0, "Grizzly Bears")

	exiled := changeNumExileFixture(t, e, src, 3)
	// A fourth exile WITHOUT the provenance marker (ExiledWith stays 0): the
	// ChangeType$ filter must still apply under an All cap, so this card
	// stays exiled while the three marked ones return.
	unmarked := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
	e.emit(events.Event{Kind: events.MoveZone, Obj: unmarked, From: state.ZHand, To: state.ZExile})
	// PRECONDITION: three exiles carry ExiledWith == src and one does not,
	// and none of them is in a hand -- the values the assertions below
	// compare must actually differ.
	for _, id := range exiled {
		if o := e.G.Obj(id); o.ExiledWith != src {
			t.Fatalf("exiled card %d ExiledWith = %d, want the source %d", id, o.ExiledWith, src)
		}
	}
	if o := e.G.Obj(unmarked); o.ExiledWith != 0 {
		t.Fatalf("unmarked exile picked up provenance %d", o.ExiledWith)
	}
	if inHand(e, exiled[0]) {
		t.Fatal("precondition broken: an exiled card is already in hand")
	}

	e.priorityRound()
	sa := cards.ResolveSVar(safe.Faces[0].SVars, "DBOpenSafe")
	if sa == nil {
		t.Fatal("Expert-Level Safe's DBOpenSafe SVar did not resolve")
	}
	if sa.API != "ChangeZoneAll" {
		t.Fatalf("resolved SVar API = %q, want ChangeZoneAll", sa.API)
	}
	if got := sa.Params["ChangeNum"]; got != "All" {
		t.Fatalf("compiled DBOpenSafe ChangeNum = %q, want All (the spelling this pin reads)", got)
	}
	if got := sa.Params["ChangeType"]; got != "Card.ExiledWithSource" {
		t.Fatalf("compiled DBOpenSafe ChangeType = %q, want Card.ExiledWithSource", got)
	}
	ctx := &effects.Ctx{Source: src, Controller: 0, ResolvingObj: src}
	effects.Resolve(e, ctx, sa)

	for i, id := range exiled {
		if !inHand(e, id) {
			t.Fatalf("exiled-with-source card %d (of %v) did not return to its owner's hand: hand=%v",
				i, exiled, e.G.Zone(state.ZHand, 0))
		}
	}
	if o := e.G.Obj(unmarked); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the unmarked exile must stay exiled (ChangeType$ filter): %+v", o)
	}
	replayCheck(t, e, cfg)
}

// TestChangeZoneAllChangeNumCapsTheSweep pins the numeric half of the param:
// an authored bone_dancer-shaped sweep (ChangeNum$ 1 over three matching
// exiled cards) moves exactly the FIRST match in the sweep's scan order and
// leaves the rest; the same sweep WITHOUT ChangeNum (and with All) moves all
// three, so the cap -- not something else -- is what narrowed it. A
// ChangeNum$ value Num cannot resolve degrades to 0 (Num's documented
// convention: the card did nothing), so a second sweep over fresh exiles
// with an unresolvable value moves nothing at all.
func TestChangeZoneAllChangeNumCapsTheSweep(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := mordorEngine(t, reg, 4242, "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears")
	src := moveToBattlefieldByName(t, e, 0, "Grizzly Bears")

	author := func(changeNum string) *cards.SA {
		params := map[string]string{
			"Origin": "Exile", "Destination": "Hand", "ChangeType": "Card.ExiledWithSource",
		}
		if changeNum != "" {
			params["ChangeNum"] = changeNum
		}
		return &cards.SA{Kind: "DB", API: "ChangeZoneAll", Params: params}
	}

	// PRECONDITION: three matching exiles, none in a hand.
	exiled := changeNumExileFixture(t, e, src, 3)
	if inHand(e, exiled[0]) || inHand(e, exiled[2]) {
		t.Fatal("precondition broken: an exiled card is already in hand")
	}

	e.priorityRound()
	ctx := &effects.Ctx{Source: src, Controller: 0, ResolvingObj: src}
	effects.Resolve(e, ctx, author("1"))
	if !inHand(e, exiled[0]) {
		t.Fatalf("the capped sweep did not move the first scan-order match %d: hand=%v",
			exiled[0], e.G.Zone(state.ZHand, 0))
	}
	for i, id := range exiled[1:] {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZExile {
			t.Fatalf("cap of 1 swept more than one: exiled[%d] = %+v", i+1, o)
		}
	}

	// The uncapped forms on the two cards the cap spared.
	ctx2 := &effects.Ctx{Source: src, Controller: 0, ResolvingObj: src}
	effects.Resolve(e, ctx2, author(""))
	if !inHand(e, exiled[1]) || !inHand(e, exiled[2]) {
		t.Fatalf("the no-ChangeNum sweep did not move the spared matches: hand=%v", e.G.Zone(state.ZHand, 0))
	}

	// Unresolvable value: Num degrades it to 0, so a fresh pair of matching
	// exiles stays put entirely (fail closed -- "the card did nothing").
	fresh := changeNumExileFixture(t, e, src, 2)
	e.priorityRound()
	ctx3 := &effects.Ctx{Source: src, Controller: 0, ResolvingObj: src}
	effects.Resolve(e, ctx3, author("NotANumber"))
	for i, id := range fresh {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZExile {
			t.Fatalf("unresolvable ChangeNum$ swept fresh[%d]: %+v", i, o)
		}
	}
	replayCheck(t, e, cfg)
}
