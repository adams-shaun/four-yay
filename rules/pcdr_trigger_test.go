package rules

// PlayerCountDefinedRegistered$<Property> carriers, end to end on the REAL
// corpus faces. The four cards are Knight of the Ebon Legion and Y'shtola,
// Night's Blessed (end-step `HighestLifeLostThisTurn` gates), Lost Monarch of
// Ifnir (second-main `HasPropertywasDealtCombatDamageThisTurnBy Zombie GE1`,
// backed by the engine's per-turn combat-damage ledger) and Ludevic,
// Necro-Alchemist (`HasPropertyLostLifeThisTurn` on the `.Other` group, the
// ConditionCheckSVar$ gate whose fail direction was OPEN).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// pcdrEngine builds a two-seat game, seat 0 active (the CR 103.1 toss pinned
// via seatZeroStart), with each named REAL corpus card parked on seat 0's
// battlefield and no pending decision.
func pcdrEngine(t *testing.T, names ...string) (*Engine, map[string]state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	deck := mountainDeck(t, 40)
	for _, name := range names {
		card, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus fixture: %s missing", name)
		}
		deck = append(deck, card)
	}
	e := New(seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 41)}}))
	e.Advance()
	ids := map[string]state.ObjID{}
	for _, name := range names {
		ids[name] = crAbortMove(t, e, 0, name, state.ZBattlefield)
	}
	e.pending = nil
	return e, ids
}

// pcdrCombat drives ONE combat-damage assignment to a player through the real
// runCombatAssignments path, so the engine's per-turn combat-damage ledger is
// populated exactly as it is in a live attack.
func pcdrCombat(e *Engine, from state.ObjID, to state.PlayerID, amount int32) {
	e.combatRound.assignments = []assignment{{toPlayer: to, amount: amount, from: from}}
	e.runCombatAssignments()
}

// pcdrStep emits the StepChange the engine's own step advance would emit and
// clears any queued triggers.
func pcdrStep(e *Engine, s state.Step) {
	e.emit(events.Event{Kind: events.StepChange, Step: s})
	e.pendingTriggers = nil
}

// pcdrQueue emits a StepChange, counts the triggers queued for src and clears
// the queue -- the emit-then-observe-then-drain shape assertPhaseFires uses.
func pcdrQueue(e *Engine, s state.Step, src state.ObjID) int {
	e.emit(events.Event{Kind: events.StepChange, Step: s})
	n := queuedPhaseTriggers(e, src)
	e.pendingTriggers = nil
	return n
}

// TestLostMonarchOfIfnirZombieCombatDamageGatesMill pins the card's real
// intervening-if: its second-main trigger fires ONLY when a player was dealt
// combat damage by a Zombie this turn, and never for a non-Zombie source, for
// non-combat damage, or at the first main phase.
func TestLostMonarchOfIfnirZombieCombatDamageGatesMill(t *testing.T) {
	t.Parallel()
	e, ids := pcdrEngine(t, "Lost Monarch of Ifnir", "Grizzly Bears")
	monarch := ids["Lost Monarch of Ifnir"]
	bears := ids["Grizzly Bears"]

	// First main never fires, and neither does the second when there is no
	// qualifying combat damage.
	if n := pcdrQueue(e, state.StepMain1, monarch); n != 0 {
		t.Fatalf("first main queued %d triggers with no Zombie damage, want 0", n)
	}
	if n := pcdrQueue(e, state.StepMain2, monarch); n != 0 {
		t.Fatalf("second main queued %d triggers with no Zombie damage, want 0", n)
	}

	// Non-combat damage from the Zombie itself: the ledger only records the
	// COMBAT damage site, so this must not satisfy the condition.
	e.damaging = monarch
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 3})
	e.damaging = 0
	if n := pcdrQueue(e, state.StepMain2, monarch); n != 0 {
		t.Fatalf("second main queued %d triggers for NON-combat Zombie damage, want 0", n)
	}

	// Combat damage from a non-Zombie: the source spec fails, no fire.
	pcdrCombat(e, bears, 1, 3)
	if n := pcdrQueue(e, state.StepMain2, monarch); n != 0 {
		t.Fatalf("second main queued %d triggers for a non-Zombie, want 0", n)
	}

	// Combat damage from the Zombie (the Monarch itself): the condition holds
	// and the second main fires. The first main still does not.
	pcdrCombat(e, monarch, 1, 3)
	if n := pcdrQueue(e, state.StepMain1, monarch); n != 0 {
		t.Fatalf("first main queued %d triggers after Zombie combat damage, want 0", n)
	}
	if n := pcdrQueue(e, state.StepMain2, monarch); n != 1 {
		t.Fatalf("second main queued %d triggers after Zombie combat damage, want 1", n)
	}
}

// TestLostMonarchOfIfnirTriggerMatchesWithCondition is the un-deleted variant
// of the Phase-gate pin: the FULL compiled trigger (including CheckSVar$ X)
// now matches exactly when the ledger holds a qualifying hit, at the second
// main phase only. Before the count head existed the condition could not be
// evaluated and triggerMatches returned false at every step.
func TestLostMonarchOfIfnirTriggerMatchesWithCondition(t *testing.T) {
	t.Parallel()
	e, ids := pcdrEngine(t, "Lost Monarch of Ifnir")
	monarch := ids["Lost Monarch of Ifnir"]
	tr := crTriggerFixture(t, e, monarch, "Phase", "Mill")
	if tr.Params["CheckSVar"] == "" {
		t.Fatal("corpus fixture changed: the Lost Monarch trigger no longer carries CheckSVar$")
	}

	for s := state.Step(0); s <= state.StepCleanup; s++ {
		pcdrStep(e, s)
		if got := e.triggerMatches(tr, monarch, events.Event{Kind: events.StepChange, Step: s}, nil); got {
			t.Fatalf("step %s matched the full trigger with no qualifying damage", s)
		}
	}

	pcdrCombat(e, monarch, 1, 3)

	// Second main matches; the first main and every other step do not.
	sawSecondMain := false
	for s := state.Step(0); s <= state.StepCleanup; s++ {
		pcdrStep(e, s)
		got := e.triggerMatches(tr, monarch, events.Event{Kind: events.StepChange, Step: s}, nil)
		want := s == state.StepMain2
		if got != want {
			t.Fatalf("step %s: triggerMatches=%v, want %v (Zombie combat damage already dealt)", s, got, want)
		}
		if want {
			sawSecondMain = true
		}
	}
	if !sawSecondMain {
		t.Fatal("the second main phase never matched")
	}
}

// TestKnightOfTheEbonLegionEndStepLifeLossGate pins the end-step
// HighestLifeLostThisTurn gate: it fires only once a player has lost 4 or
// more life this turn.
func TestKnightOfTheEbonLegionEndStepLifeLossGate(t *testing.T) {
	t.Parallel()
	e, ids := pcdrEngine(t, "Knight of the Ebon Legion")
	knight := ids["Knight of the Ebon Legion"]

	// 3 life lost this turn: below GE4, no fire.
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -3})
	if n := pcdrQueue(e, state.StepEnd, knight); n != 0 {
		t.Fatalf("end step queued %d triggers at 3 life lost, want 0", n)
	}

	// A fourth point of life lost pushes the extreme to 4: the gate holds.
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -1})
	if n := pcdrQueue(e, state.StepEnd, knight); n != 1 {
		t.Fatalf("end step queued %d triggers at 4 life lost, want 1", n)
	}
}

// TestYshtolaNightsBlessedEndStepLifeLossGate pins the same
// HighestLifeLostThisTurn route on the second carrier, whose end-step trigger
// fires for every player's end step (no ValidPlayer$).
func TestYshtolaNightsBlessedEndStepLifeLossGate(t *testing.T) {
	t.Parallel()
	e, ids := pcdrEngine(t, "Y'shtola, Night's Blessed")
	yshtola := ids["Y'shtola, Night's Blessed"]

	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -3})
	if n := pcdrQueue(e, state.StepEnd, yshtola); n != 0 {
		t.Fatalf("end step queued %d triggers at 3 life lost, want 0", n)
	}

	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -1})
	if n := pcdrQueue(e, state.StepEnd, yshtola); n != 1 {
		t.Fatalf("end step queued %d triggers at 4 life lost, want 1", n)
	}
}

// TestLudevicOtherLostLifeConditionEvaluates pins the fail-direction
// correction on the shared ConditionCheckSVar$ evaluator: Ludevic's
// `OtherLost` body (`PlayerCountDefinedRegistered.Other$HasPropertyLostLifeThisTurn`)
// now RESOLVES, and holds exactly when a player other than the controller
// lost life this turn. Before the count head existed the body was unresolved,
// and the condition call site fails OPEN on an unresolved body — so the draw
// offer was made even when nobody but the controller lost life.
func TestLudevicOtherLostLifeConditionEvaluates(t *testing.T) {
	t.Parallel()
	e, ids := pcdrEngine(t, "Ludevic, Necro-Alchemist")
	ludevic := ids["Ludevic, Necro-Alchemist"]
	face := e.G.Obj(ludevic).Face()
	if face.SVars["OtherLost"] == "" {
		t.Fatal("corpus fixture changed: Ludevic's OtherLost SVar is gone")
	}
	ctx := &effects.Ctx{Source: ludevic, Controller: 0, SVars: face.SVars}

	// Only the CONTROLLER lost life: resolves, and does not hold.
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -4})
	holds, evaluated := effects.CheckSVarHolds(e, ctx, "OtherLost", "")
	if !evaluated {
		t.Fatal("OtherLost is UNRESOLVED -- the fail-open direction the fix closes")
	}
	if holds {
		t.Fatal("OtherLost held with only the controller losing life")
	}

	// An opponent lost life: resolves and holds.
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -2})
	holds, evaluated = effects.CheckSVarHolds(e, ctx, "OtherLost", "")
	if !evaluated || !holds {
		t.Fatalf("OtherLost after an opponent lost life = (holds=%v, evaluated=%v), want (true, true)", holds, evaluated)
	}
}

// TestLedgerSkipsRedirectedPlayerCombatDamage is the class guard for the
// combat-damage ledger's capture site: a damage-redirection replacement
// (Protector of the Crown's `R:Event$ DamageDone | ValidTarget$ You |
// DamageTarget$ Self` body) rewrites the held player-targeted Damage event
// into a PERMANENT-targeted one — ev.Obj becomes the receiving permanent and
// ev.Player is zeroed — yet the event Kind is still events.Damage, so the
// capture must not record a hit for a player who was never dealt damage. The
// commander tally two lines above the ledger already guards exactly this
// (`ev.Obj == 0`); before this guard the ledger appended a false hit and made
// Lost Monarch's intervening-if hold with no player damage.
func TestLedgerSkipsRedirectedPlayerCombatDamage(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	// Seat 0: Lost Monarch (the rider reading the ledger). Seat 1: Protector
	// of the Crown, whose controller is the redirected-to player.
	deck0 := mountainDeck(t, 40)
	for _, name := range []string{"Lost Monarch of Ifnir"} {
		card, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus fixture: %s missing", name)
		}
		deck0 = append(deck0, card)
	}
	deck1 := mountainDeck(t, 41)
	prot, ok := reg.Lookup("Protector of the Crown")
	if !ok {
		t.Fatalf("corpus fixture: Protector of the Crown missing")
	}
	deck1 = append(deck1, prot)
	e := New(seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck0, deck1}}))
	e.Advance()
	monarch := crAbortMove(t, e, 0, "Lost Monarch of Ifnir", state.ZBattlefield)
	protector := crAbortMove(t, e, 1, "Protector of the Crown", state.ZBattlefield)
	e.pending = nil

	// Combat damage aimed at seat 1 is redirected onto seat 1's Protector:
	// no player is dealt damage, so the ledger stays empty and Lost Monarch's
	// second-main trigger must not fire.
	e.combatRound.assignments = []assignment{{toPlayer: 1, amount: 3, from: monarch}}
	e.runCombatAssignments()
	e.pendingTriggers = nil
	if hits := e.CombatDamageToPlayersThisTurn(); len(hits) != 0 {
		t.Fatalf("redirected combat damage recorded %d ledger hits, want 0: %+v", len(hits), hits)
	}
	if n := pcdrQueue(e, state.StepMain2, monarch); n != 0 {
		t.Fatalf("second main queued %d triggers after redirected damage, want 0", n)
	}

	// Sanity: without the Protector (removed from the battlefield) the same
	// assignment lands on seat 1 and the ledger records it.
	e.emit(events.Event{Kind: events.MoveZone, Obj: protector, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pendingTriggers = nil
	e.combatRound.assignments = []assignment{{toPlayer: 1, amount: 3, from: monarch}}
	e.runCombatAssignments()
	e.pendingTriggers = nil
	if hits := e.CombatDamageToPlayersThisTurn(); len(hits) != 1 {
		t.Fatalf("unredirected combat damage recorded %d ledger hits, want 1", len(hits))
	}
	if n := pcdrQueue(e, state.StepMain2, monarch); n != 1 {
		t.Fatalf("second main queued %d triggers after landed Zombie damage, want 1", n)
	}
}

// TestLedgerRecordsParkedDamageReplacement is the class guard for the
// ledger's SECOND append site (finishChosenDamage): a player-targeted combat
// damage whose CR 616.1 competition is POSED — any Optional$ True damage
// replacement alone qualifies, and Battletide Alchemist ("you may prevent X …
// where X is the number of Clerics you control", here 0) is a real corpus
// carrier — parks a KReplacement and returns a Note from runCombatAssignments,
// so the capture site's append is SKIPPED for a hit that lands. Before the
// finishChosenDamage append existed the parked hit never entered the ledger
// and Lost Monarch's intervening-if read 0 in exactly the games this ticket
// exists to fix.
func TestLedgerRecordsParkedDamageReplacement(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	deck0 := mountainDeck(t, 40)
	mon, ok := reg.Lookup("Lost Monarch of Ifnir")
	if !ok {
		t.Fatalf("corpus fixture: Lost Monarch of Ifnir missing")
	}
	deck0 = append(deck0, mon)
	deck1 := mountainDeck(t, 41)
	batt, ok := reg.Lookup("Battletide Alchemist")
	if !ok {
		t.Fatalf("corpus fixture: Battletide Alchemist missing")
	}
	deck1 = append(deck1, batt)
	e := New(seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck0, deck1}}))
	e.Advance()
	monarch := crAbortMove(t, e, 0, "Lost Monarch of Ifnir", state.ZBattlefield)
	battletide := crAbortMove(t, e, 1, "Battletide Alchemist", state.ZBattlefield)
	_ = battletide
	e.pending = nil

	// The assignment parks on the optional prevention ask (posed to seat 1,
	// the Battletide's controller — OptionalDecider$ You), and the ledger is
	// empty until the parked event is resolved.
	e.combatRound.assignments = []assignment{{toPlayer: 1, amount: 3, from: monarch}}
	e.runCombatAssignments()
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || d.Player != 1 {
		t.Fatalf("pending = %+v, want Battletide's optional-prevention ask to seat 1", d)
	}
	if hits := e.CombatDamageToPlayersThisTurn(); len(hits) != 0 {
		t.Fatalf("parked combat damage recorded %d ledger hits before the answer, want 0", len(hits))
	}

	// DECLINE the ask ("do not apply an optional replacement"): the damage
	// lands through finishChosenDamage, which must append the ledger hit the
	// parked capture site skipped. The resumed pass's completion re-poses a
	// priority ask (the same device damage_prevented_once_test.go clears);
	// clear it the pcdr way so the later runs see no pending decision.
	submitChoices(t, e, len(d.Options)-1)
	e.pending = nil
	e.pendingTriggers = nil
	if hits := e.CombatDamageToPlayersThisTurn(); len(hits) != 1 {
		t.Fatalf("declined-park combat damage recorded %d ledger hits, want 1: %+v", len(hits), hits)
	}
	if hits := e.CombatDamageToPlayersThisTurn(); len(hits) == 1 && (hits[0].Player != 1 || hits[0].Amount != 3) {
		t.Fatalf("ledger hit = %+v, want {Player:1 Amount:3}", hits[0])
	}
	if n := pcdrQueue(e, state.StepMain2, monarch); n != 1 {
		t.Fatalf("second main queued %d triggers after the parked hit landed, want 1", n)
	}

	// Control: choosing the replacement instead (0 Clerics = prevents 0) also
	// lands the damage through the same finishChosenDamage path. A fresh
	// turn's ledger starts empty, so this isolates the apply arm.
	e.emit(events.Event{Kind: events.TurnChange, Player: 1})
	e.pending = nil
	e.pendingTriggers = nil
	e.combatRound.assignments = []assignment{{toPlayer: 1, amount: 2, from: monarch}}
	e.runCombatAssignments()
	d = e.Pending()
	if d == nil || d.Kind != decision.KReplacement {
		t.Fatalf("pending = %+v, want the optional-prevention ask again", d)
	}
	submitChoices(t, e, 0)
	e.pendingTriggers = nil
	if hits := e.CombatDamageToPlayersThisTurn(); len(hits) != 1 {
		t.Fatalf("applied-park combat damage recorded %d ledger hits, want 1 (new turn, 0-Cleric prevention): %+v", len(hits), hits)
	}
}
