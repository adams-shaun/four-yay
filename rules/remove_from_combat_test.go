package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The api:RemoveFromCombat leaf tests (Forge's RemoveFromCombatEffect, CR
// 506.4's "a spell or ability causes it to be removed from combat"): the
// primitive emits one events.EndCombatReset{Obj: id} per resolved battlefield
// object -- the exact event regeneration uses -- and nothing else: the target
// stays tapped (untapping is the cards' own chained SubAbility), and
// RememberRemovedFromCombat$ feeds both halves of the remembered state. Every
// test drives a real corpus card through deck-built, fully logged setup so
// replayCheck means something: no eventless onBoard placement anywhere.

// rfcEngine builds a two-seat game from real corpus cards (seat 0's deck led
// by extras0, seat 1's by extras1, both filled out with Plains), pinned to
// seat 0's toss so the turn arithmetic is deterministic, and returns the
// engine and its config for replayCheck.
func rfcEngine(t *testing.T, reg *cards.Registry, extras0, extras1 []*cards.Card) (*Engine, Config) {
	t.Helper()
	planes, ok := reg.Lookup("Plains")
	if !ok {
		t.Fatal("corpus fixture: Plains missing")
	}
	fill := func(n int) []*cards.Card {
		out := make([]*cards.Card, n)
		for i := range out {
			out[i] = planes
		}
		return out
	}
	cfg := seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{append(append([]*cards.Card{}, extras0...), fill(40-len(extras0))...),
			append(append([]*cards.Card{}, extras1...), fill(40-len(extras1))...)}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// declineBlockers submits the pending blockers decision with the empty
// declaration (no blocks) -- the legal answer that keeps the combat alive.
func declineBlockers(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
		t.Fatalf("decline blockers: %v", err)
	}
}

// passPriorities passes priority decisions until a non-priority decision
// appears (the caller stops when the decision it wants is pending), or the
// budget runs out -- unlike drainPriority, which fatals on a non-priority ask.
func passPriorities(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			return
		}
		passPriorityOnce(t, e)
	}
	t.Fatal("priority did not drain within the budget")
}

// rfcDrive crosses every decision on the way to turn/active's step, bounded
// like driveToStep: priority is passed, an attackers or blockers ask in the
// way is declined empty (the legal no-op), and any other decision is answered
// with its first option -- so a combat step of the NON-active seat is a
// crossable waypoint, which driveToStep (priority-only) cannot do.
func rfcDrive(t *testing.T, e *Engine, turn int32, active state.PlayerID, step state.Step) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == step {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before reaching turn %d seat %d step %s (stopped at turn %d seat %d step %s)",
				turn, active, step, e.G.Turn, e.G.Active, e.G.Step)
		}
		if answerIfDiscard(t, e) {
			continue
		}
		d := e.Pending()
		if d == nil {
			t.Fatalf("no pending decision while driving to turn %d seat %d step %s", turn, active, step)
		}
		switch d.Kind {
		case decision.KPriority:
			passPriorityOnce(t, e)
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
				t.Fatalf("decline %v while driving: %v", d.Kind, err)
			}
		default:
			if len(d.Options) == 0 {
				t.Fatalf("empty decision %+v while driving", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit first option while driving: %v", err)
			}
		}
	}
	t.Fatal("rfcDrive did not converge")
}

// containsTargetObj reports whether ts carries an object entry for id.
func containsTargetObj(ts []state.Target, id state.ObjID) bool {
	for _, t := range ts {
		if !t.IsPlayer && t.Obj == id {
			return true
		}
	}
	return false
}

// TestRemoveFromCombatActivatedReconnaissanceUntapsAttacker drives the real
// corpus Reconnaissance end to end: activated at the declare-blockers
// priority window, its {0} ability targets seat 0's own attacker, the
// resolution emits exactly one EndCombatReset for that creature (no longer
// IsAttacking, CR 506.4) and the chained `DB$ Untap | Defined$ Targeted`
// untaps it -- the card's own words, not the primitive's.
func TestRemoveFromCombatActivatedReconnaissanceUntapsAttacker(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := rfcEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Reconnaissance"), lookup(t, reg, "Grizzly Bears")},
		[]*cards.Card{lookup(t, reg, "Runeclaw Bear")})
	recon := moveByName(t, e, 0, "Reconnaissance", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	// Seat 1's bear only makes its blocker decision real; it declines it.
	moveByName(t, e, 1, "Runeclaw Bear", state.ZBattlefield)

	// Turn 3 is seat 0's second turn: the bear entered on turn 1, so CR
	// 302.6 no longer holds it back.
	rfcDrive(t, e, 3, 0, state.StepDeclareAttackers)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected seat 0's attackers ask, got %+v", d)
	}
	submitAttackersOnly(t, e, bear)
	drainCombatPriority(t, e)
	declineBlockers(t, e)
	// The CR 509.2 window: the ACTIVE player (seat 0, the attacker) gets
	// priority first -- this very decision carries Reconnaissance's {0}.
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected seat 0's priority at declare blockers, got %+v", d)
	}
	var act decision.Option
	found := false
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == recon {
			act, found = o, true
			break
		}
	}
	if !found {
		t.Fatalf("Reconnaissance's ability not offered: %+v", d.Options)
	}
	submitChoices(t, e, act.Index)

	// The ValidTgts$ Creature.attacking+YouCtrl ask: only the attacking bear.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target ask, got %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("attacking bear not offered as a target: %+v", d.Options)
	}
	submitChoices(t, e, tgt)

	passUntilStackEmpty(t, e, 30)
	o := e.G.Obj(bear)
	if o.IsAttacking {
		t.Fatal("the removed attacker is still IsAttacking (CR 506.4)")
	}
	if o.Tapped {
		t.Fatal("Reconnaissance's chained Untap did not untap the bear")
	}
	if n := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.EndCombatReset && ev.Obj == bear
	}); n != 1 {
		t.Fatalf("EndCombatReset for the bear = %d, want exactly 1", n)
	}
	if n := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.Note && ev.Obj == recon && ev.Text == "unimplemented API RemoveFromCombat"
	}); n != 0 {
		t.Fatalf("%d unimplemented-API notes for the activation", n)
	}
	replayCheck(t, e, cfg)
}

// TestRemoveFromCombatHollowhengeSpiritRemovesAttackerStillTapped casts the
// real corpus Hollowhenge Spirit with its flash during an opponent's
// declare-blockers step: the ETB trigger's `ValidTgts$ Creature.attacking,
// Creature.blocking` ask targets the attacking bear, and the resolution
// removes it while it STAYS tapped (the primitive never untaps -- a removed
// attacker keeps its tapped state, CR 506.4).
func TestRemoveFromCombatHollowhengeSpiritRemovesAttackerStillTapped(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := rfcEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Hollowhenge Spirit"), lookup(t, reg, "Grizzly Bears")},
		[]*cards.Card{lookup(t, reg, "Grizzly Bears")})
	// The Spirit's seed may have dealt it into the opening hand already; if
	// not, move it up from the library (logged either way).
	inHand := false
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && o.Face().Name == "Hollowhenge Spirit" {
			inHand = true
		}
	}
	if !inHand {
		moveByName(t, e, 0, "Hollowhenge Spirit", state.ZHand)
	}
	blocker0 := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield) // declined blocker
	moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)             // the attacker

	// Seat 1's first turn: it attacks with the bear; seat 0 declines to
	// block, and at the CR 509.2 window casts the Spirit (flash).
	rfcDrive(t, e, 2, 1, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected seat 1's attackers ask, got %+v", d)
	}
	var bear1 state.ObjID
	for _, o := range d.Options {
		if o.Obj != 0 {
			bear1 = o.Obj
		}
	}
	if bear1 == 0 {
		t.Fatalf("no attacker option for seat 1: %+v", d.Options)
	}
	submitAttackersOnly(t, e, bear1)
	drainCombatPriority(t, e)
	// Fund the {3}{W} through logged ManaAdd events (replay folds them) NOW,
	// while the declare-blockers step is live: a pool empties at every step
	// change (CR 500.1), so mana added at declare-attackers would be gone by
	// the cast window, but mana added here survives to it -- the blocker
	// declaration and the 509.2 window are the same step.
	for _, sym := range "CCCW" {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(sym), Amount: 1})
	}
	declineBlockers(t, e)
	passPriorityOnce(t, e) // the active player's half of the 509.2 window

	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected seat 0's priority at declare blockers, got %+v", d)
	}
	var castOpt decision.Option
	found := false
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj != 0 && e.G.Obj(o.Obj) != nil &&
			e.G.Obj(o.Obj).Face().Name == "Hollowhenge Spirit" {
			castOpt, found = o, true
			break
		}
	}
	if !found {
		t.Fatalf("flash cast of Hollowhenge Spirit not offered: %+v", d.Options)
	}
	submitChoices(t, e, castOpt.Index)

	// Drain to the ETB trigger's target ask (Task 7 poses it at push time),
	// answer it with the attacking bear, then let the stack empty.
	for i := 0; i < 30; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision while draining to the ETB trigger's target ask")
		}
		if d.Kind == decision.KTarget {
			break
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision before the ETB target ask: %+v", d)
		}
		passPriorityOnce(t, e)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the ETB trigger's target ask, got %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == bear1 {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("attacking bear not offered to the trigger: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 30)

	o := e.G.Obj(bear1)
	if o.IsAttacking {
		t.Fatal("the attacker is still attacking after the Spirit resolved (CR 506.4)")
	}
	if !o.Tapped {
		t.Fatal("the removed attacker must STAY tapped (the primitive never untaps)")
	}
	if n := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.EndCombatReset && ev.Obj == bear1
	}); n != 1 {
		t.Fatalf("EndCombatReset for the bear = %d, want exactly 1", n)
	}
	// The unrelated creature -- seat 0's own bear, never attacking or
	// blocking this combat -- was NOT removed.
	if n := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.EndCombatReset && ev.Obj == blocker0
	}); n != 0 {
		t.Fatalf("EndCombatReset for the bystander bear = %d, want 0", n)
	}
	replayCheck(t, e, cfg)
}

// TestRemoveFromCombatGustcloakSaviorRemovesItsOwnAttacker drives the real
// corpus Gustcloak Savior's AttackerBlocked trigger: the optional ask is
// answered "yes", and the body's `DB$ Untap | Defined$ TriggeredAttackerLKICopy
// | SubAbility$ DBRemoveCombat` untaps the remembered attacker and removes it
// from combat -- the trigger-role capture feeding Defined$.
func TestRemoveFromCombatGustcloakSaviorRemovesItsOwnAttacker(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := rfcEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Gustcloak Savior")},
		[]*cards.Card{lookup(t, reg, "Storm Crow")})
	savior := moveByName(t, e, 0, "Gustcloak Savior", state.ZBattlefield)
	bear1 := moveByName(t, e, 1, "Storm Crow", state.ZBattlefield)

	rfcDrive(t, e, 3, 0, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected seat 0's attackers ask, got %+v", d)
	}
	submitAttackersOnly(t, e, savior)
	drainCombatPriority(t, e)
	// Seat 1 blocks with its bear; the blocker declaration pushes the
	// AttackerBlocked trigger onto the stack.
	d = e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	submitBlockersOnly(t, e, bear1)

	// Priority window, then the trigger resolves: OptionalDecider$ You poses
	// the may-ask to seat 0 -- answer it with "yes" (option 0 is the offer's
	// first entry, whatever its wire kind).
	for i := 0; i < 10; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision while the Gustcloak trigger resolves")
		}
		if d.Kind == decision.KTriggerOptional {
			break
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision before the optional ask: %+v", d)
		}
		passPriorityOnce(t, e)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("expected the trigger's optional ask, got %+v", d)
	}
	if len(d.Options) == 0 {
		t.Fatalf("optional ask with no options: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 30)

	o := e.G.Obj(savior)
	if o.IsAttacking {
		t.Fatal("the Savior is still attacking after its own removal")
	}
	if o.Tapped {
		t.Fatal("the Savior's body untapped it before removing it")
	}
	if n := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.EndCombatReset && ev.Obj == savior
	}); n != 1 {
		t.Fatalf("EndCombatReset for the Savior = %d, want exactly 1", n)
	}
	// The blocker it was blocking stays blocked (zero tombstones, CR 509.1h):
	// removal never "unblocks" anyone.
	replayCheck(t, e, cfg)
}

// TestRemoveFromCombatIllusionistsGambitRemembersAndUntaps resolves the real
// corpus Illusionist's Gambit's compiled SA directly (its ActivationPhases$
// cast gate is a separate ticket): `Defined$ Valid Creature.attacking`
// removes EVERY attacking creature, `RememberRemovedFromCombat$ True` feeds
// both halves of the remembered state, and the chained
// `DB$ Untap | Defined$ Remembered` untaps exactly that set.
func TestRemoveFromCombatIllusionistsGambitRemembersAndUntaps(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := rfcEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Illusionist's Gambit"), lookup(t, reg, "Grizzly Bears")},
		[]*cards.Card{lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Runeclaw Bear")})
	gambit := moveByName(t, e, 0, "Illusionist's Gambit", state.ZHand)
	blocker0 := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield) // declined blocker
	bear1 := moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	bear2 := moveByName(t, e, 1, "Runeclaw Bear", state.ZBattlefield)

	// Seat 1's first turn: it attacks with both bears; seat 0 declines to
	// block. Then seat 0 resolves the Gambit's SA by hand.
	rfcDrive(t, e, 2, 1, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected seat 1's attackers ask, got %+v", d)
	}
	var attackers []state.ObjID
	for _, o := range d.Options {
		if o.Obj != 0 {
			attackers = append(attackers, o.Obj)
		}
	}
	if len(attackers) != 2 {
		t.Fatalf("want both bears offered as attackers: %+v", d.Options)
	}
	submitAttackersOnly(t, e, attackers...)
	drainCombatPriority(t, e)
	declineBlockers(t, e)
	passPriorityOnce(t, e) // the active player's half of the 509.2 window

	gc := lookup(t, reg, "Illusionist's Gambit")
	sa := gc.Faces[0].SpellAbility()
	effects.Resolve(e, &effects.Ctx{Source: gambit, Controller: 0,
		SVars: gc.Faces[0].SVars}, sa)

	for _, id := range []state.ObjID{bear2, bear1} {
		o := e.G.Obj(id)
		if o.IsAttacking {
			t.Fatalf("attacker %d is still attacking (CR 506.4)", id)
		}
		if o.Tapped {
			t.Fatalf("attacker %d was not untapped by the chained Defined$ Remembered Untap", id)
		}
		if n := countEvents(e, func(ev events.Event) bool {
			return ev.Kind == events.EndCombatReset && ev.Obj == id
		}); n != 1 {
			t.Fatalf("EndCombatReset for %d = %d, want exactly 1", id, n)
		}
	}
	// The bystander -- seat 0's own bear, on the battlefield but never
	// attacking -- was not removed: the sweep matched Creature.attacking only.
	if n := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.EndCombatReset && ev.Obj == blocker0
	}); n != 0 {
		t.Fatalf("EndCombatReset for the bystander bear = %d, want 0", n)
	}
	// The source's event-backed remembered half carries the removed set
	// (Card.IsRemembered reads it later).
	for _, id := range attackers {
		if !containsTargetObj(e.G.Obj(gambit).Remembered, id) {
			t.Fatalf("gambit does not remember removed attacker %d", id)
		}
	}
	replayCheck(t, e, cfg)
}
