package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The ForgetPlayed$ pin (task param:api:Play.ForgetPlayed) on Vaan, Street
// Thief's REAL compiled corpus script. Vaan's combat-damage trigger digs the
// top card of the damaged player's library to exile, remembers it, and
// chains DB$ Play | Defined$ Remembered | ValidSA$ Spell | Optional$ True |
// ForgetPlayed$ True followed by DB$ Token gated on ConditionDefined$
// Remembered | ConditionPresent$ Card | ConditionCompare$ GT0 — the "you may
// cast it. If you don't, create a Treasure token." oracle. Before the fix
// the played card stayed in the remembered set, so the Treasure gate passed
// even when the cast was taken: a player who took the free cast also got the
// Treasure. The fix reads ForgetPlayed$ at effPlay's PlayDone re-entry
// branch and drops the played card from BOTH remembered halves through the
// one shared forget body (effects.forgetRememberedOne, the ForgetChanged$
// machinery — the same "forget-remembered" Choose event, no new event kind).
//
// Boundaries recorded:
//   - The DECLINE path needs no forget and keeps none: an empty answer
//     leaves ctx.Play at 0, so the guard `id != 0` never fires and the
//     Treasure is created exactly when nothing was played (the second test).
//   - The R-9 no-host branch (bottom of effPlay: a fuzz/no-engine host
//     "plays" the first candidate without beginning any cast) is deliberately
//     LEFT UNTOUCHED: no cast was begun there, the card never moves, and the
//     pre-existing stand-in divergence is not this ticket's business; a
//     forget on that path would also move fuzz-only event streams.
//   - An Amount$ Play that begins SEVERAL cards would only forget the first
//     (ctx.Play holds one card) — measured corpus-unreachable: every
//     ForgetPlayed$ carrier (19 files) plays exactly one card.
//
// Vaan, Street Thief is in NO repo deck and NO legacy golden deck (measured:
// grepping the 19 ForgetPlayed carrier names against
// internal/testutil/decks/*.json returns nothing), so no chain head depends
// on this card and TestHeads is safe by construction.
//
// The helpers come from search_library_test.go (same package): the deck is
// built from compiled corpus cards only, so no Forge script text is
// committed here either. The attack/damage half follows
// combat_damage_trigger_test.go's pattern.

// vaanEngine deals seat 0 Vaan (on the battlefield by turn 3) and seats
// seat 1 a deck whose single non-mountain card is a Grizzly Bears parked at
// the TOP of its library — the card Vaan's Dig exiles. The Bears is a
// target-free vanilla creature, so the answered play's cast ({1}{G}, paid
// from a raw ManaAdd pool) commits synchronously inside the resume arm.
func vaanEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck0 := []*cards.Card{searchCorpusCard(t, reg, "Vaan, Street Thief")}
	for i := 0; i < 8; i++ {
		deck0 = append(deck0, forest, mountain)
	}
	for len(deck0) < 40 {
		deck0 = append(deck0, bear)
	}
	deck1 := []*cards.Card{bear}
	for len(deck1) < 40 {
		deck1 = append(deck1, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 7401, Names: []string{"vaan", "defender"},
		Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	vaanID := searchMoveByName(t, e, "Vaan, Street Thief", state.ZBattlefield)
	// The Bears is parked on seat 1's library top only AFTER the drive to
	// turn 3: seat 1's turn-2 draw step would otherwise take it into hand
	// and the Dig would dig a mountain.
	return e, cfg, vaanID, 0
}

// seatLibraryTop parks the (only) copy of name in player p's deck at the
// TOP of p's library (index 0, what a draw and a Dig take first), wherever
// the seeded shuffle put it: a hand-borne copy is moved back to the library
// through a logged MoveZone, then one logged LibraryOrder (events.Apply
// sets the whole zone; the emitted list must be the COMPLETE library) puts
// the card first. All placement is on the log, so the game still replays.
func seatLibraryTop(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	id := state.ObjID(0)
	from := state.Zone(0)
	for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
		for _, oid := range e.G.Zone(z, p) {
			o := e.G.Obj(oid)
			if o != nil && o.Face() != nil && o.Face().Name == name {
				id, from = oid, z
			}
		}
	}
	if id == 0 {
		t.Fatalf("seat %d has no %q in library or hand", p, name)
	}
	if from != state.ZLibrary {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: state.ZLibrary, Player: p})
	}
	lib := e.G.Zone(state.ZLibrary, p)
	want := []state.ObjID{id}
	for _, oid := range lib {
		if oid != id {
			want = append(want, oid)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: p, IDs: want, Secret: true})
	return id
}

// vaanPlayAsk drives the real game to turn 3's declare-attackers step
// (turn 2 is seat 1's; Vaan entered the battlefield on turn 1, so its first
// attack turn is 3), attacks with Vaan unblocked, and returns the play ask
// the damage trigger's Dig → DB$ Play chain poses once the exiled card is
// remembered.
func vaanPlayAsk(t *testing.T, e *Engine, vaanID state.ObjID) (state.ObjID, *decision.Decision) {
	t.Helper()
	driveToStepAll(t, e, 3, 0, state.StepMain1)
	bearID := seatLibraryTop(t, e, 1, "Grizzly Bears")
	// Continue through seat 0's main phase to the declare-attackers ask: the
	// placement events are ordinary logged moves (searchMoveByName's
	// precedent), and driveToStepAll answers the priority asks between here
	// and the combat step.
	driveToStepAll(t, e, 3, 0, state.StepDeclareAttackers)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("pending = %+v, want the attackers decision", d)
	}
	submitAttackersOnly(t, e, vaanID)
	drainCombatPriority(t, e)
	d = passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "play" {
		t.Fatalf("after the attack: %+v, want the DB$ Play KModes ask", d)
	}
	if d.Player != 0 {
		t.Fatalf("play ask player = %d, want 0 (Vaan's controller)", d.Player)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != bearID {
		t.Fatalf("play options = %+v, want exactly the exiled Bears %d", d.Options, bearID)
	}
	return bearID, d
}

// TestVaanStreetThiefAnsweredPlayCreatesNoTreasure: the play is ANSWERED,
// the cast of the exiled Bears commits and resolves onto the battlefield,
// ForgetPlayed$ drops it from the remembered set, and the chained Treasure
// gate fails — no Treasure token exists.
func TestVaanStreetThiefAnsweredPlayCreatesNoTreasure(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, vaanID, _ := vaanEngine(t, reg)
	bearID, _ := vaanPlayAsk(t, e, vaanID)
	// The Bears' cast pays {1}{G}; raw ManaAdd emits need no live priority
	// decision (the play-cost test's precedent — the settle pays from the
	// pool directly). Two green + one red covers the pip and the generic.
	for _, r := range "GGR" {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(r), Amount: 1})
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 60)

	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the played card is %v, want on the battlefield (the cast committed)", o)
	}
	if o := e.G.Obj(bearID); o != nil && o.Owner == 0 {
		t.Fatalf("the exiled card's ownership moved to %d, want seat 1 (a Play never changes ownership)", o.Owner)
	}
	if n := countTokensNamedOnSeat(t, e, 0, "Treasure Token"); n != 0 {
		t.Fatalf("%d Treasure token(s) after the ANSWERED play, want 0 (the played card left the remembered set)", n)
	}
	if o := e.G.Obj(vaanID); o.Zone != state.ZBattlefield {
		t.Fatalf("Vaan left the battlefield: %v", o.Zone)
	}
	replayCheck(t, e, cfg)
}

// TestVaanStreetThiefDeclinedPlayCreatesTheTreasure: the play is DECLINED
// (the empty answer of the Min-0 Optional ask), the exiled Bears stays in
// exile, the remembered set still holds it, and the chained Treasure gate
// passes — exactly one Treasure token is created.
func TestVaanStreetThiefDeclinedPlayCreatesTheTreasure(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, vaanID, _ := vaanEngine(t, reg)
	bearID, _ := vaanPlayAsk(t, e, vaanID)
	submitChoices(t, e) // empty answer = the decline of an Optional$ Play
	passUntilStackEmpty(t, e, 60)

	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the declined card moved: %v, want still in exile", o)
	}
	if o := e.G.Obj(vaanID); o.Zone != state.ZBattlefield {
		t.Fatalf("Vaan left the battlefield: %v", o.Zone)
	}
	if n := countTokensNamedOnSeat(t, e, 0, "Treasure Token"); n != 1 {
		t.Fatalf("%d Treasure token(s) after the DECLINED play, want exactly 1", n)
	}
	replayCheck(t, e, cfg)
}
