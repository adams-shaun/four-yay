package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The CantPutCounter restriction static (task cantputcounter1), pinned on the
// real corpus carriers' printed S: forms: Phila, Unsealed (player form, poison
// only), Melira's Keepers (object form, self, every kind) and Blightbeetle
// (object form, opponents' creatures, +1/+1 only). Each leaf places a counter
// through the ordinary CounterChange/PlayerCounterChange path and asserts the
// restriction swallows it, plus the fail-closed direction (a kind or a target
// the restriction does not name still places normally).

// philaReplGame seeds Phila, Unsealed onto seat 0's battlefield and returns
// the engine and cfg.
func philaReplGame(t *testing.T, seed uint64) (*Engine, Config) {
	t.Helper()
	phila := tokenReplCorpusCard(t, "Phila, Unsealed")
	e, cfg := tokenReplGame(t, seed, phila)
	moveSeededCard(t, e, 0, phila, state.ZBattlefield)
	return e, cfg
}

// TestCantPutCounterPhilaBlocksPoisonOnly is the player-form leaf: Phila's
// `ValidPlayer$ You | CounterType$ POISON` static must stop a poison placement
// on its controller (the restriction is consulted at the counter choke point,
// before any replacement), while a DIFFERENT counter kind on the same player
// places normally -- the kind-scoped fail-closed direction.
func TestCantPutCounterPhilaBlocksPoisonOnly(t *testing.T) {
	t.Parallel()
	e, cfg := philaReplGame(t, 211)
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "POISON", Amount: 2})
	if got := e.G.Players[0].Counter("POISON"); got != 0 {
		t.Fatalf("Phila: 2 poison -> %d, want 0 (you can't get poison counters)", got)
	}
	// A different counter kind is not named by the restriction.
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "ENERGY", Amount: 2})
	if got := e.G.Players[0].Counter("ENERGY"); got != 2 {
		t.Fatalf("Phila: 2 energy -> %d, want 2 (the restriction names POISON only)", got)
	}
	// The opponent is not the controller the restriction scopes ("You").
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 1, Counter: "POISON", Amount: 2})
	if got := e.G.Players[1].Counter("POISON"); got != 2 {
		t.Fatalf("Phila: opponent 2 poison -> %d, want 2 (the restriction scopes its controller)", got)
	}
	replayCheck(t, e, cfg)
}

// TestCantPutCounterMelirasKeepersBlocksSelfOnly is the object-form leaf with
// an unscoped CounterType$: Melira's Keepers' `ValidCard$ Card.Self` static
// must stop EVERY counter kind on the Keepers itself, while another creature
// (and another player) place normally -- the object-scope fail-closed
// direction.
func TestCantPutCounterMelirasKeepersBlocksSelfOnly(t *testing.T) {
	t.Parallel()
	keepers := tokenReplCorpusCard(t, "Melira's Keepers")
	other := card(t, "Name:Other Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg := tokenReplGame(t, 213, keepers, other)
	keepersID := moveSeededCard(t, e, 0, keepers, state.ZBattlefield)
	otherID := moveSeededCard(t, e, 0, other, state.ZBattlefield)

	e.emit(events.Event{Kind: events.CounterChange, Obj: keepersID, Counter: "P1P1", Amount: 2})
	if got := e.G.Obj(keepersID).Counter("P1P1"); got != 0 {
		t.Fatalf("Melira's Keepers: 2 P1P1 on itself -> %d, want 0 (can't have counters put on it)", got)
	}
	// The kind is unscoped, so a second kind is blocked too.
	e.emit(events.Event{Kind: events.CounterChange, Obj: keepersID, Counter: "STUN", Amount: 1})
	if got := e.G.Obj(keepersID).Counter("STUN"); got != 0 {
		t.Fatalf("Melira's Keepers: 1 STUN on itself -> %d, want 0 (no kind is named, all kinds are blocked)", got)
	}
	// Another creature is not Card.Self.
	e.emit(events.Event{Kind: events.CounterChange, Obj: otherID, Counter: "P1P1", Amount: 2})
	if got := e.G.Obj(otherID).Counter("P1P1"); got != 2 {
		t.Fatalf("Melira's Keepers: 2 P1P1 on another creature -> %d, want 2 (the restriction is self-scoped)", got)
	}
	replayCheck(t, e, cfg)
}

// TestCantPutCounterBlightbeetleScopesOpponentsP1P1 is the opponent-scoped
// object-form leaf: Blightbeetle's `ValidCard$ Creature.OppCtrl | CounterType$
// P1P1` must stop +1/+1 counters on the OPPONENT's creature (the static's
// controller reads the controller of the source, so the opponent's creature is
// OppCtrl), while its controller's own creature and a different kind on the
// opponent's creature place normally.
func TestCantPutCounterBlightbeetleScopesOpponentsP1P1(t *testing.T) {
	t.Parallel()
	beetle := tokenReplCorpusCard(t, "Blightbeetle")
	mine := card(t, "Name:My Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	theirs := card(t, "Name:Their Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg := tokenReplGameSeats(t, 215, []*cards.Card{beetle, mine}, []*cards.Card{theirs})
	moveSeededCard(t, e, 0, beetle, state.ZBattlefield)
	mineID := moveSeededCard(t, e, 0, mine, state.ZBattlefield)
	theirID := moveSeededCard(t, e, 1, theirs, state.ZBattlefield)

	e.emit(events.Event{Kind: events.CounterChange, Obj: theirID, Counter: "P1P1", Amount: 2})
	if got := e.G.Obj(theirID).Counter("P1P1"); got != 0 {
		t.Fatalf("Blightbeetle: 2 P1P1 on an opponent's creature -> %d, want 0", got)
	}
	// A different kind on the opponent's creature is not named.
	e.emit(events.Event{Kind: events.CounterChange, Obj: theirID, Counter: "STUN", Amount: 1})
	if got := e.G.Obj(theirID).Counter("STUN"); got != 1 {
		t.Fatalf("Blightbeetle: 1 STUN on an opponent's creature -> %d, want 1 (the restriction names P1P1 only)", got)
	}
	// The controller's own creature is not OppCtrl.
	e.emit(events.Event{Kind: events.CounterChange, Obj: mineID, Counter: "P1P1", Amount: 2})
	if got := e.G.Obj(mineID).Counter("P1P1"); got != 2 {
		t.Fatalf("Blightbeetle: 2 P1P1 on the controller's own creature -> %d, want 2 (the restriction is OppCtrl-scoped)", got)
	}
	replayCheck(t, e, cfg)
}

// TestMeliraLockExpiresAndTheReplacementRestarts pins the lock's LIFETIME:
// Melira's oracle text is "you can't get additional poison counters THIS
// TURN", so the Effect-registered CantPutCounter the rider installs must be
// a this-turn lock (UntilEOT, dropped by EndOfTurnCleanup), never a
// Permanent one. A Permanent registration would swallow a fresh poison
// source on every LATER turn outright (total stuck at 1) and would disable
// Melira's own R:Event$ AddCounter replacement (2 -> 1) forever, the
// non-permissive direction for a restriction.
func TestMeliraLockExpiresAndTheReplacementRestarts(t *testing.T) {
	t.Parallel()
	melira := tokenReplCorpusCard(t, "Melira, the Living Cure")
	e, cfg := tokenReplGame(t, 105, melira)
	moveSeededCard(t, e, 0, melira, state.ZBattlefield)
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "POISON", Amount: 3})
	if got := e.G.Players[0].Counter("POISON"); got != 1 {
		t.Fatalf("Melira turn 1: 3 poison -> %d, want 1 (Amount$ 1)", got)
	}
	// The lock stops a second source the same turn (the cantputcounter1
	// behaviour, restated here as this leaf's own setup).
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "POISON", Amount: 2})
	if got := e.G.Players[0].Counter("POISON"); got != 1 {
		t.Fatalf("Melira turn 1, second source of 2: -> %d, want 1 (the lock)", got)
	}
	// The turn ends. The lock must die with it.
	e.EndOfTurnCleanup()
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	// On the later turn Melira's own replacement (R:Event$ AddCounter |
	// ReplaceWith$ OnlyOnePoison) fires again: the fresh 2-poison source is
	// REWRITTEN to 1, not swallowed, so the total becomes 2.
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "POISON", Amount: 2})
	if got := e.G.Players[0].Counter("POISON"); got != 2 {
		t.Fatalf("Melira turn 2: a fresh 2-poison source -> %d, want 2 (the lock expired; the replacement rewrote 2 -> 1)", got)
	}
	replayCheck(t, e, cfg)
}

// TestCantPutCounterDoesNotBlockRemovalOrShield is the sign/marker guard: a
// CantPutCounter static with an unscoped CounterType$ (Melira's Keepers) must
// NOT block a counter REMOVAL (CR 122.1's "put" is a placement; the engine
// logs a removal as a negative CounterChange) nor the engine's own status
// markers (a regeneration Shield, a Deathtouched mark), which are not
// counters. Without the sign/marker gate the restriction would freeze the
// marker bookkeeping and strip a permanent of regeneration under any
// "counters can't be put on it" static.
func TestCantPutCounterDoesNotBlockRemovalOrShield(t *testing.T) {
	t.Parallel()
	keepers := tokenReplCorpusCard(t, "Melira's Keepers")
	e, cfg := tokenReplGame(t, 219, keepers)
	keepersID := moveSeededCard(t, e, 0, keepers, state.ZBattlefield)
	// A positive regeneration Shield marker on the Keepers must pass -- it is
	// the engine's, not a counter a card put on (marker exclusion).
	e.emit(events.Event{Kind: events.CounterChange, Obj: keepersID, Counter: "Shield", Amount: 1})
	if got := e.G.Obj(keepersID).Counter("Shield"); got != 1 {
		t.Fatalf("Melira's Keepers: one regeneration Shield marker -> %d, want 1 (a status marker is not a counter)", got)
	}
	// ... and the REMOVAL that consumes it must pass through too (a removal is
	// not a placement, CR 122.1): the Shield must fall back to 0, which a
	// swallowed event would leave at 1.
	e.emit(events.Event{Kind: events.CounterChange, Obj: keepersID, Counter: "Shield", Amount: -1})
	if got := e.G.Obj(keepersID).Counter("Shield"); got != 0 {
		t.Fatalf("Melira's Keepers: consuming the Shield -> %d, want 0 (a removal is not a placement)", got)
	}
	replayCheck(t, e, cfg)
}

// TestCantPutCounterUnscopedPlayerStaticBlocksEveryKind pins the unscoped
// player form on the real Solemnity script (`ValidPlayer$ Player`, no
// CounterType$): every player's every counter kind is blocked, so no
// ValidCounterType$ key silently widens the reading back to nothing.
func TestCantPutCounterUnscopedPlayerStaticBlocksEveryKind(t *testing.T) {
	t.Parallel()
	solemnity := tokenReplCorpusCard(t, "Solemnity")
	e, cfg := tokenReplGame(t, 217, solemnity)
	moveSeededCard(t, e, 0, solemnity, state.ZBattlefield)
	for _, p := range []state.PlayerID{0, 1} {
		e.emit(events.Event{Kind: events.PlayerCounterChange, Player: p, Counter: "POISON", Amount: 1})
		if got := e.G.Players[p].Counter("POISON"); got != 0 {
			t.Fatalf("Solemnity: player %d 1 poison -> %d, want 0 (players can't get counters)", p, got)
		}
	}
	replayCheck(t, e, cfg)
}
