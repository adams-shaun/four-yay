package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Entry-characteristic counters (task addcounter1/2) are the counters a
// permanent gives ITSELF as it enters the battlefield -- a planeswalker's
// starting loyalty (CR 306.5b), a Battle's defense counters (CR 310.6), a
// Saga's lore counter (CR 702.151a), Riot's and Unleash's +1/+1 election
// (CR 702.54 / 702.86). They used to be folded straight into events.Move,
// which cannot emit, so the CR 614 AddCounter replacement class and the
// CantPutCounter prohibition never saw them. They are now placed through
// real CounterChange events (rules/entry_counters.go).

// entryCounterBattle is the authored Battle fixture: Types:Battle Siege and
// Defence:5, so an entry grants five DEFENSE counters.
func entryCounterBattle(t testing.TB) *cards.Card {
	return card(t, "Name:Entry Battle\nTypes:Battle Siege\nDefense:5\nOracle:x\n")
}

// TestEntryCounterStayDoesNotRegrant is the engine-side wasBattlefield
// boundary the entry-counter snapshot now owns: a battlefield-internal move
// is not a new object and must not re-grant the entry counters. The
// snapshot skips any move whose object is already on the battlefield, so a
// second MoveZone within the zone leaves the count at 5.
func TestEntryCounterStayDoesNotRegrant(t *testing.T) {
	t.Parallel()
	battle := entryCounterBattle(t)
	e, cfg := tokenReplGame(t, 151, battle)
	bid := moveSeededCard(t, e, 0, battle, state.ZBattlefield)
	if o := e.G.Obj(bid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: battle not on the battlefield")
	}
	if got := e.G.Obj(bid).Counter("DEFENSE"); got != 5 {
		t.Fatalf("precondition: battle entry = %d defense, want 5", got)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: bid, From: state.ZBattlefield, To: state.ZBattlefield})
	if got := e.G.Obj(bid).Counter("DEFENSE"); got != 5 {
		t.Fatalf("battlefield-internal move re-granted: %d defense, want 5", got)
	}
	replayCheck(t, e, cfg)
}

// entryCounterWalker is the authored planeswalker fixture: Types:Planeswalker
// and a printed Loyalty:4, so an ordinary entry grants exactly 4 loyalty
// counters through the real corpus-free card parse (no corpus .txt is
// committed; the card source is inline, per the licensing rule).
func entryCounterWalker(t testing.TB) *cards.Card {
	return card(t, "Name:Entry Walker\nTypes:Planeswalker Entry\nLoyalty:4\nOracle:x\n")
}

// entryCounterEtbCreature is the authored "enters with two +1/+1 counters"
// carrier: K:etbCounter expands to a Moved replacement whose ReplaceWith$ is
// a DB$ PutCounter | ETB$ True body, so the placement runs INSIDE a
// replacement body -- the exact in-flight-replacement window this task
// closes a hole in.
func entryCounterEtbCreature(t testing.TB) *cards.Card {
	return card(t, "Name:Entry Etb\nTypes:Creature Bear\nPT:2/2\n"+
		"K:etbCounter:P1P1:2:no Condition:CARDNAME enters with two +1/+1 counters on it.\n")
}

// TestEntryCounterMoveFoldIsReplacementVisible is the replacement half of
// task addcounter1/2: a planeswalker's starting loyalty is a real
// CounterChange, so an all-kinds AddCounter replacement (Vorinclex,
// Monstrous Raider) doubles it. Before the fix the fold inside events.Move
// placed the counters with no event, so the replacement never ran and the
// entry stayed at the printed 4.
func TestEntryCounterMoveFoldIsReplacementVisible(t *testing.T) {
	t.Parallel()
	// Precondition: with no replacement on the board the walker still enters
	// with its printed 4 loyalty -- the entry path must place the counters,
	// not silently drop them.
	walker := entryCounterWalker(t)
	e, cfg := tokenReplGame(t, 131, walker)
	base := moveSeededCard(t, e, 0, walker, state.ZBattlefield)
	if o := e.G.Obj(base); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: walker not on the battlefield: %+v", e.G.Obj(base))
	}
	if got := e.G.Obj(base).Counter("LOYALTY"); got != 4 {
		t.Fatalf("precondition: base entry loyalty = %d, want 4", got)
	}
	replayCheck(t, e, cfg)

	// Vorinclex doubles every counter its controller puts. The entry loyalty
	// must therefore be 8, and the doubled value must be a REAL event (8 !=
	// the printed 4, which is what the pre-fix silent fold would leave).
	vori := tokenReplCorpusCard(t, "Vorinclex, Monstrous Raider")
	walker = entryCounterWalker(t)
	e, cfg = tokenReplGame(t, 137, vori, walker)
	voriID := moveSeededCard(t, e, 0, vori, state.ZBattlefield)
	if o := e.G.Obj(voriID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Vorinclex not on the battlefield")
	}
	// A real cast publishes its controller as the counter adder; the harness
	// moves the card directly, so publish it here to stand in for the cast.
	e.SetCounterAdder(0)
	wid := moveSeededCard(t, e, 0, walker, state.ZBattlefield)
	if o := e.G.Obj(wid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: walker not on the battlefield")
	}
	if got := e.G.Obj(wid).Counter("LOYALTY"); got != 8 {
		t.Fatalf("Vorinclex: entry loyalty = %d, want 8 (printed 4 doubled by the AddCounter replacement)", got)
	}
	replayCheck(t, e, cfg)
}

// TestReplacementBodyCounterIsBlockedByCantPutCounter is the prohibition
// half of task addcounter1/2: a counter placed from INSIDE a replacement
// body (the K:etbCounter PutCounter|ETB$ True body) must still obey a
// CantPutCounter restriction. The engine used to skip applyReplacements --
// and with it the prohibition -- while the body was in flight, so Solemnity
// was ignored and the creature entered with its two counters anyway.
func TestReplacementBodyCounterIsBlockedByCantPutCounter(t *testing.T) {
	t.Parallel()
	// Precondition: without Solemnity the body really does place 2 counters
	// (2 != 0, so the blocked assertion below cannot pass vacuously).
	cre := entryCounterEtbCreature(t)
	e, cfg := tokenReplGame(t, 139, cre)
	cid := moveSeededCard(t, e, 0, cre, state.ZBattlefield)
	if o := e.G.Obj(cid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: creature not on the battlefield: %+v", e.G.Obj(cid))
	}
	if got := e.G.Obj(cid).Counter("P1P1"); got != 2 {
		t.Fatalf("precondition: etbCounter body entry = %d, want 2", got)
	}
	replayCheck(t, e, cfg)

	// Solemnity's object line names creatures, so the body's placement on the
	// entering creature must be swallowed: 2 -> 0.
	sol := tokenReplCorpusCard(t, "Solemnity")
	cre = entryCounterEtbCreature(t)
	e, cfg = tokenReplGame(t, 149, sol, cre)
	moveSeededCard(t, e, 0, sol, state.ZBattlefield)
	cid = moveSeededCard(t, e, 0, cre, state.ZBattlefield)
	if o := e.G.Obj(cid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: creature not on the battlefield")
	}
	if got := e.G.Obj(cid).Counter("P1P1"); got != 0 {
		t.Fatalf("Solemnity: etbCounter body entry = %d, want 0 (the CantPutCounter restriction must reach a counter placed inside a replacement body)", got)
	}
	replayCheck(t, e, cfg)
}
