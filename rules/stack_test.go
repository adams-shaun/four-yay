package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func castFirst(t *testing.T, e *Engine, kind string) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no pending decision")
	}
	for _, o := range d.Options {
		if o.Kind == kind {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("submit: %v", err)
			}
			return
		}
	}
	t.Fatalf("no %q option in %+v", kind, d.Options)
}

func TestCastPutsSpellOnStackAndAsksForTargets(t *testing.T) {
	t.Parallel()
	bolt := card(t, "Name:Lightning Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n")
	e := handEngine(t, bolt)
	e.G.Players[0].Pool[state.MR] = 1
	e.askPriority(0)

	castFirst(t, e, "cast")

	// CR 601.2a: the spell is on the stack at the target decision -- the
	// stack move precedes the target choice (601.2c). Payment (601.2h) follows
	// the target, so at this unanswered decision the R is still unspent.
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the spell", e.G.Stack)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	if len(d.Options) < 2 {
		t.Fatalf("both players should be legal targets: %+v", d.Options)
	}
	// CR 601.2c-before-601.2h (not the old spend-at-target ordering): payment
	// is deferred until the target is chosen.
	if e.G.Players[0].Pool[state.MR] != 1 {
		t.Errorf("casting spent mana before the target was chosen; pool=%v, want the R unspent", e.G.Players[0].Pool)
	}
	// Choosing the target completes the transaction: pay, then the cast fires.
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("opponent not offered as a target: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatal(err)
	}
	if e.G.Players[0].Pool[state.MR] != 0 {
		t.Errorf("casting did not spend the R after the target was chosen; pool=%v", e.G.Players[0].Pool)
	}
}

func TestSpellResolvesAndAppliesItsEffect(t *testing.T) {
	t.Parallel()
	bolt := card(t, "Name:Lightning Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n")
	e := handEngine(t, bolt)
	e.G.Players[0].Pool[state.MR] = 1
	e.askPriority(0)
	castFirst(t, e, "cast")

	// Target the opponent.
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("opponent not offered as a target: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatal(err)
	}
	// Everyone passes; the spell resolves.
	for i := 0; i < 4 && len(e.G.Stack) > 0; i++ {
		castFirst(t, e, "pass")
	}
	if e.G.Players[1].Life != 17 {
		t.Fatalf("opponent life = %d, want 17", e.G.Players[1].Life)
	}
	if len(e.G.Stack) != 0 {
		t.Fatal("stack did not empty")
	}
	if len(e.G.Zone(state.ZGraveyard, 0)) != 1 {
		t.Fatal("the instant should be in its owner's graveyard")
	}
}

func TestPermanentResolvesToTheBattlefield(t *testing.T) {
	t.Parallel()
	bear := card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e := handEngine(t, bear)
	e.G.Players[0].Pool[state.MG] = 3
	e.askPriority(0)
	castFirst(t, e, "cast")
	for i := 0; i < 4 && len(e.G.Stack) > 0; i++ {
		castFirst(t, e, "pass")
	}
	bf := e.G.Zone(state.ZBattlefield, 0)
	if len(bf) != 1 {
		t.Fatalf("battlefield = %v", bf)
	}
	if !e.G.Obj(bf[0]).SummonSick {
		t.Error("a creature entering should be summoning sick")
	}
}

func TestLastInFirstOutResolution(t *testing.T) {
	t.Parallel()
	bolt := card(t, "Name:Lightning Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n")
	shock := card(t, "Name:Shock\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 2\nOracle:x\n")
	e := handEngine(t, bolt, shock)
	e.G.Players[0].Pool[state.MR] = 2
	e.askPriority(0)

	target := func() {
		d := e.Pending()
		for _, o := range d.Options {
			if o.Kind == "player" && o.Player == 1 {
				e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}})
				return
			}
		}
	}
	castFirst(t, e, "cast")
	target()
	castFirst(t, e, "cast")
	target()
	if len(e.G.Stack) != 2 {
		t.Fatalf("stack = %v, want two spells", e.G.Stack)
	}
	top := e.G.Obj(e.G.Stack[1]).Face().Name
	for i := 0; i < 8 && len(e.G.Stack) > 0; i++ {
		castFirst(t, e, "pass")
	}
	gy := e.G.Zone(state.ZGraveyard, 0)
	if len(gy) != 2 {
		t.Fatalf("graveyard = %v", gy)
	}
	if e.G.Obj(gy[0]).Face().Name != top {
		t.Fatalf("resolution order wrong: %s resolved first, stack top was %s",
			e.G.Obj(gy[0]).Face().Name, top)
	}
	if e.G.Players[1].Life != 15 {
		t.Fatalf("life = %d, want 15 after Bolt and Shock", e.G.Players[1].Life)
	}
}

func TestFizzleWhenNoLegalTargets(t *testing.T) {
	t.Parallel()
	// Announce with a legal target, then remove it before resolution. CR
	// 608.2b counters the spell because all of its targets are now illegal.
	kill := card(t, "Name:Kill\nManaCost:B\nTypes:Instant\nA:SP$ Destroy | ValidTgts$ Creature\nOracle:x\n")
	e := handEngine(t, kill)
	victim := onBoard(t, e, 1, "Name:Victim\nManaCost:G\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.G.Players[0].Pool[state.MB] = 1
	e.askPriority(0)
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected target decision, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == victim {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("victim not offered as target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	e.emit(events.Event{Kind: events.MoveZone, Obj: victim, From: state.ZBattlefield, To: state.ZGraveyard})
	passUntilStackEmpty(t, e, 8)
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want empty after a fizzle", e.G.Stack)
	}
	if len(e.G.Zone(state.ZGraveyard, 0)) != 1 {
		t.Fatal("the fizzled spell should be in the graveyard")
	}
}

// --- CR 608.2b: target legality is rechecked at resolution, not just at
// cast time (TestFizzleWhenNoLegalTargets above). These two tests build the
// stack object directly rather than through castSpell/askTarget, because
// M1's askTarget only ever asks for a single target (Min/Max 1) -- there is
// no way to reach a spell with two chosen targets, one legal and one not,
// through the public casting path the way real Magic's TargetMin/TargetMax
// would allow. Constructing the scenario directly and calling resolveTop is
// the same white-box style layers_test.go already uses for Derived/
// AddContinuous.

// TestSpellFizzlesWhenAllTargetsBecomeIllegalBeforeResolution: the spell's
// only target dies (to anything -- combat, another spell) after it was
// chosen but before this spell resolves. The whole spell must do nothing --
// not just skip the now-gone target, but also never run the SubAbility
// chained onto it, which names no target of its own and so would otherwise
// still fire even though CR 608.2b says the spell never resolves at all.
func TestSpellFizzlesWhenAllTargetsBecomeIllegalBeforeResolution(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	victim := onBoard(t, e, 1, "Name:Goat\nManaCost:1 G\nTypes:Creature Goat\nPT:2/2\nOracle:x\n")
	helix := card(t, "Name:Helix\nManaCost:R W\nTypes:Instant\n"+
		"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 3 | SubAbility$ DBLife\n"+
		"SVar:DBLife:DB$ GainLife | Defined$ You | LifeAmount$ 3\nOracle:x\n")
	o := e.G.AddObject(helix, 0)
	o.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{o.ID})
	o.Targets = []state.Target{{Obj: victim}}

	// The only target dies before the spell resolves.
	e.emit(events.Event{Kind: events.MoveZone, Obj: victim, From: state.ZBattlefield, To: state.ZGraveyard})
	beforeLife := e.G.Players[0].Life

	e.resolveTop()

	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want empty", e.G.Stack)
	}
	if got := e.G.Obj(o.ID).Zone; got != state.ZGraveyard {
		t.Fatalf("fizzled spell zone = %s, want graveyard", got)
	}
	if e.G.Players[0].Life != beforeLife {
		t.Fatalf("caster life = %d, want unchanged %d: the chained SubAbility must not run once every "+
			"target is illegal (CR 608.2b)", e.G.Players[0].Life, beforeLife)
	}
}

// TestResolveDoesAsMuchAsItCanWhenSomeTargetsAreStillLegal: two targets were
// chosen (an object target and a player target); the object target dies
// before resolution but the player target does not. CR 608.2b: the spell
// still resolves, applying its effect to the target that is still legal.
func TestResolveDoesAsMuchAsItCanWhenSomeTargetsAreStillLegal(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	dead := onBoard(t, e, 1, "Name:Goat\nManaCost:1 G\nTypes:Creature Goat\nPT:2/2\nOracle:x\n")
	thunder := card(t, "Name:Thunder\nManaCost:2 R\nTypes:Sorcery\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n")
	o := e.G.AddObject(thunder, 0)
	o.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{o.ID})
	o.Targets = []state.Target{{Obj: dead}, {Player: 1, IsPlayer: true}}

	// One of the two chosen targets dies before the spell resolves; the
	// player target does not.
	e.emit(events.Event{Kind: events.MoveZone, Obj: dead, From: state.ZBattlefield, To: state.ZGraveyard})

	e.resolveTop()

	if e.G.Players[1].Life != 17 {
		t.Fatalf("opponent life = %d, want 17: the still-legal player target must still take damage", e.G.Players[1].Life)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want empty", e.G.Stack)
	}
	if got := e.G.Obj(o.ID).Zone; got != state.ZGraveyard {
		t.Fatalf("spell zone = %s, want graveyard", got)
	}
}

// --- Task 13 gap-closing coverage -----------------------------------------
//
// legal_test.go and fix1_test.go's Submit-driven tests (TestPlayLandReplay-
// ThroughSubmit, TestActivateManaAbilityReplayThroughSubmit) could not cover
// the "cast" branch or activate's mana-pool effect, because castSpell and
// resolveAbility were still this task's empty stubs when they were written.
// The two tests below close that gap, following the same shape as their
// fix1_test.go siblings: drive the action through Submit only, then replay
// the resulting log alone (no Engine, no rules code involved) and confirm it
// reproduces the live state.
//
// The replay comparison is deliberately scoped to player-indexed scalar
// state (Pool, Life, Passes, Priority) rather than anything object-shaped
// (Stack contents, Object.Targets): genesis -- state.NewGame plus the
// AddObject calls that place cards, whether via Engine.New's deck loop or a
// test's own direct placement -- is never logged (see the rules package
// doc), so a fresh game built from cfg.Names has an empty object arena no
// matter how the live game got its cards. That is the same, already-
// established boundary TestPlayLandReplayThroughSubmit and
// TestActivateManaAbilityReplayThroughSubmit observe for LandsPlayed/Tapped;
// Task 24 (replay) is what recombines a Config with a log to reconstruct
// object-scoped state.

// TestCastReplayThroughSubmit drives a full cast -> target -> resolve cycle
// through Submit and checks both the live mutations castSpell/resolveAbility
// make (mana spent, the spell on the stack, its target recorded, the target
// player's life reduced) and that the scalar half of that state replays.
func TestCastReplayThroughSubmit(t *testing.T) {
	t.Parallel()
	names := []string{"a", "b"}
	cfg := Config{Seed: 11, Names: names,
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}
	e := New(cfg)
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	bolt := card(t, "Name:Lightning Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n")
	o := e.G.AddObject(bolt, 0)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{o.ID})
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	// Setup only (not the action under test): fund the cast. Emitted, not a
	// direct Pool write, so the replay comparison below -- which reconstructs
	// the pool from the log alone -- has a starting balance to reconstruct;
	// a direct write here would be invisible to replay and desync fresh's
	// pool from the live one the moment castSpell spends against it.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 1})

	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected player 0's priority, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "cast" && opt.Obj == o.ID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for the hand spell: %+v", d.Options)
	}

	// The action under test: a cast intent through Submit.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}

	// CR 601.2a: the spell is on the stack before its target is chosen, so
	// at the (still unanswered) target decision the R is not yet spent --
	// payment (601.2h) follows the target (601.2c), not precedes it.
	if len(e.G.Stack) != 1 || e.G.Stack[0] != o.ID {
		t.Fatalf("stack = %v, want [%d]", e.G.Stack, o.ID)
	}
	if e.G.Players[0].Pool[state.MR] != 1 {
		t.Fatalf("pool = %v, want the R still unspent at the target decision", e.G.Players[0].Pool)
	}

	target := e.Pending()
	if target == nil || target.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", target)
	}
	tIdx := -1
	for _, opt := range target.Options {
		if opt.Kind == "player" && opt.Player == 1 {
			tIdx = opt.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("opponent not offered as a target: %+v", target.Options)
	}
	// Also through Submit: exercises handleTarget's TargetsChosen emit.
	if err := e.Submit(decision.Intent{Seq: target.Seq, Player: 0, Choices: []int{tIdx}}); err != nil {
		t.Fatalf("submit target: %v", err)
	}
	if got := e.G.Obj(o.ID).Targets; len(got) != 1 || !got[0].IsPlayer || got[0].Player != 1 {
		t.Fatalf("targets = %+v, want a single player-1 target", got)
	}
	// Payment happens when the target is chosen: the R is now spent.
	if e.G.Players[0].Pool[state.MR] != 0 {
		t.Fatalf("pool = %v, want the R spent after the target was chosen", e.G.Players[0].Pool)
	}

	// Both players pass; the spell resolves.
	for i := 0; i < 4 && len(e.G.Stack) > 0; i++ {
		if passAll(t, e, 1) == 0 {
			t.Fatal("expected priority decisions until the spell resolves")
		}
	}
	if e.G.Players[1].Life != 17 {
		t.Fatalf("life = %d, want 17 after Bolt resolves", e.G.Players[1].Life)
	}

	fresh := state.NewGame(cfg.Names)
	for _, ev := range e.L.Events {
		events.Apply(fresh, ev)
	}
	if fresh.Players[0].Pool != e.G.Players[0].Pool {
		t.Errorf("replayed pool = %v, want %v (live)", fresh.Players[0].Pool, e.G.Players[0].Pool)
	}
	if fresh.Players[1].Life != e.G.Players[1].Life {
		t.Errorf("replayed life = %d, want %d (live)", fresh.Players[1].Life, e.G.Players[1].Life)
	}
	if fresh.Passes != e.G.Passes {
		t.Errorf("replayed Passes = %d, want %d (live)", fresh.Passes, e.G.Passes)
	}
	if fresh.Priority != e.G.Priority {
		t.Errorf("replayed Priority = %d, want %d (live)", fresh.Priority, e.G.Priority)
	}
}

// TestActivateManaAbilityProducesMana is
// TestActivateManaAbilityReplayThroughSubmit's sibling: same setup, but
// carried through to the assertion that fix1_test.go's version explicitly
// deferred -- that activating actually adds mana to the pool now that
// resolveAbility is no longer a stub -- plus the replay comparison for that
// pool.
func TestActivateManaAbilityProducesMana(t *testing.T) {
	t.Parallel()
	names := []string{"a", "b"}
	cfg := Config{Seed: 12, Names: names,
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}
	e := New(cfg)
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	mtn := card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	o := e.G.AddObject(mtn, 0)
	o.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{o.ID})
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1

	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected player 0's priority, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "activate" && opt.Obj == o.ID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no activate option for the battlefield land: %+v", d.Options)
	}

	// The action under test: an activate intent through Submit.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit activate: %v", err)
	}

	if !o.Tapped {
		t.Fatal("activating the mana ability should tap the land")
	}
	if e.G.Players[0].Pool[state.MR] != 1 {
		t.Fatalf("pool = %v, want 1 red mana from the Mountain", e.G.Players[0].Pool)
	}

	fresh := state.NewGame(cfg.Names)
	for _, ev := range e.L.Events {
		events.Apply(fresh, ev)
	}
	if fresh.Players[0].Pool != e.G.Players[0].Pool {
		t.Errorf("replayed pool = %v, want %v (live)", fresh.Players[0].Pool, e.G.Players[0].Pool)
	}
}

// --- Fix round 1: Ruling T14-e -- the caster keeps priority ---------------
//
// CR 117.3c: the player who cast a spell (or chose its target) keeps
// priority. All three Priority emits Ruling T14-a moved behind events used
// e.G.Active as the value, which is right only when the caster happens to
// be the active player -- every test above casts as the active player
// (seat 0), so none of them could have caught this. These two tests build a
// non-active caster (seat 1, priority passed to it by seat 0) and drive a
// cast through Submit, once with no target and once with one, asserting
// priority ends with the caster rather than snapping back to seat 0.

// passToPlayerOne has player 0 pass priority once, without resolving
// anything, so player 1 (not the active player) is the one asked next --
// setup shared by the three tests below, not itself the action under test.
func passToPlayerOne(t *testing.T, e *Engine) {
	t.Helper()
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected player 0's priority, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "pass" {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no pass option: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit pass: %v", err)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority || d.Player != 1 {
		t.Fatalf("expected player 1's priority after the pass (active stays 0), got %+v", d)
	}
}

// TestNonActiveCasterKeepsPriorityNoTarget: player 1 casts a no-target
// instant. Before Ruling T14-e, castSpell's trailing Priority emit used
// e.G.Active (0), clobbering the correct value legal.go's "cast" case had
// just emitted.
func TestNonActiveCasterKeepsPriorityNoTarget(t *testing.T) {
	t.Parallel()
	names := []string{"a", "b"}
	cfg := Config{Seed: 21, Names: names,
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}
	e := New(cfg)
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	// A vanilla instant with no ability at all: SpellAbility() is nil, so
	// castSpell never asks for a target.
	quiet := card(t, "Name:Quiet Instant\nManaCost:R\nTypes:Instant\nOracle:x\n")
	o := e.G.AddObject(quiet, 1)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, []state.ObjID{o.ID})
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	e.emit(events.Event{Kind: events.ManaAdd, Player: 1, Counter: "R", Amount: 1})

	passToPlayerOne(t, e)
	d := e.Pending()
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "cast" && opt.Obj == o.ID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for player 1's instant: %+v", d.Options)
	}

	// The action under test: player 1 (non-active) casts.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}

	if e.G.Priority != 1 {
		t.Fatalf("Priority = %d, want 1 (the caster) per CR 117.3c", e.G.Priority)
	}
	next := e.Pending()
	if next == nil || next.Kind != decision.KPriority || next.Player != 1 {
		t.Fatalf("next decision = %+v, want another priority decision for player 1 (the caster)", next)
	}
}

// TestNonActiveCasterKeepsPriorityWithTarget is the targeted counterpart:
// player 1 casts Lightning Bolt and chooses its target. Before Ruling
// T14-e, handleTarget's trailing Priority emit used e.G.Active (0) instead
// of the submitting player.
func TestNonActiveCasterKeepsPriorityWithTarget(t *testing.T) {
	t.Parallel()
	names := []string{"a", "b"}
	cfg := Config{Seed: 22, Names: names,
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}
	e := New(cfg)
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	bolt := card(t, "Name:Lightning Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n")
	o := e.G.AddObject(bolt, 1)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, []state.ObjID{o.ID})
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	e.emit(events.Event{Kind: events.ManaAdd, Player: 1, Counter: "R", Amount: 1})

	passToPlayerOne(t, e)
	d := e.Pending()
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "cast" && opt.Obj == o.ID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for player 1's instant: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}

	target := e.Pending()
	if target == nil || target.Kind != decision.KTarget || target.Player != 1 {
		t.Fatalf("expected player 1's target decision, got %+v", target)
	}
	tIdx := -1
	for _, opt := range target.Options {
		if opt.Kind == "player" && opt.Player == 0 {
			tIdx = opt.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("active player not offered as a target: %+v", target.Options)
	}

	// The action under test: player 1 (non-active) chooses a target.
	if err := e.Submit(decision.Intent{Seq: target.Seq, Player: 1, Choices: []int{tIdx}}); err != nil {
		t.Fatalf("submit target: %v", err)
	}

	if e.G.Priority != 1 {
		t.Fatalf("Priority = %d, want 1 (the caster) per CR 117.3c", e.G.Priority)
	}
	next := e.Pending()
	if next == nil || next.Kind != decision.KPriority || next.Player != 1 {
		t.Fatalf("next decision = %+v, want another priority decision for player 1 (the caster)", next)
	}
}

// TestNonActiveCasterReplayThroughSubmit extends TestCastReplayThroughSubmit's
// replay coverage (rather than rewriting it and losing the active-caster
// case) to the scenario Finding 1 exposed: player 1, not the active player,
// casts and resolves a spell targeting the active player. Confirms the live
// priority sequence -- caster keeps it through casting and targeting,
// active player gets it back only once the spell actually resolves (CR
// 117.5, unrelated to and unaffected by this fix) -- and that the scalar
// half of that state replays.
func TestNonActiveCasterReplayThroughSubmit(t *testing.T) {
	t.Parallel()
	names := []string{"a", "b"}
	cfg := Config{Seed: 23, Names: names,
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}
	e := New(cfg)
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	bolt := card(t, "Name:Lightning Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n")
	o := e.G.AddObject(bolt, 1)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, []state.ObjID{o.ID})
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	e.emit(events.Event{Kind: events.ManaAdd, Player: 1, Counter: "R", Amount: 1})

	passToPlayerOne(t, e)
	d := e.Pending()
	castIdx := -1
	for _, opt := range d.Options {
		if opt.Kind == "cast" && opt.Obj == o.ID {
			castIdx = opt.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("no cast option: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{castIdx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}

	target := e.Pending()
	if target == nil || target.Kind != decision.KTarget || target.Player != 1 {
		t.Fatalf("expected player 1's target decision, got %+v", target)
	}
	tIdx := -1
	for _, opt := range target.Options {
		if opt.Kind == "player" && opt.Player == 0 {
			tIdx = opt.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("active player not offered as a target: %+v", target.Options)
	}
	if err := e.Submit(decision.Intent{Seq: target.Seq, Player: 1, Choices: []int{tIdx}}); err != nil {
		t.Fatalf("submit target: %v", err)
	}
	if e.G.Priority != 1 {
		t.Fatalf("Priority = %d, want 1 (the caster) per CR 117.3c", e.G.Priority)
	}

	// Both players pass; the spell resolves.
	for i := 0; i < 4 && len(e.G.Stack) > 0; i++ {
		if passAll(t, e, 1) == 0 {
			t.Fatal("expected priority decisions until the spell resolves")
		}
	}
	if e.G.Players[0].Life != 17 {
		t.Fatalf("life = %d, want 17 after Bolt resolves", e.G.Players[0].Life)
	}

	fresh := state.NewGame(cfg.Names)
	for _, ev := range e.L.Events {
		events.Apply(fresh, ev)
	}
	if fresh.Players[1].Pool != e.G.Players[1].Pool {
		t.Errorf("replayed pool = %v, want %v (live)", fresh.Players[1].Pool, e.G.Players[1].Pool)
	}
	if fresh.Players[0].Life != e.G.Players[0].Life {
		t.Errorf("replayed life = %d, want %d (live)", fresh.Players[0].Life, e.G.Players[0].Life)
	}
	if fresh.Passes != e.G.Passes {
		t.Errorf("replayed Passes = %d, want %d (live)", fresh.Passes, e.G.Passes)
	}
	if fresh.Priority != e.G.Priority {
		t.Errorf("replayed Priority = %d, want %d (live)", fresh.Priority, e.G.Priority)
	}
}

// putLands places n fixture Mountains directly onto seat p's battlefield with
// the same bypass-the-events direct-zone write the package's other onBoard
// helper uses, and returns their IDs. The brief's "three Mountains onto seat
// 1's battlefield" arms a multi-target cast trigger with candidates.
func putLands(t *testing.T, e *Engine, p state.PlayerID, n int) []state.ObjID {
	t.Helper()
	out := make([]state.ObjID, 0, n)
	for i := 0; i < n; i++ {
		o := e.G.AddObject(card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n"), p)
		o.Zone = state.ZBattlefield
		e.G.Clock++
		o.Timestamp = e.G.Clock
		e.G.SetZone(state.ZBattlefield, p, append(e.G.Zone(state.ZBattlefield, p), o.ID))
		out = append(out, o.ID)
	}
	return out
}

// TestStackZoneCastTriggerAsksForTwoTargetsAndExilesBoth pins a cast trigger
// with an EXPLICIT TriggerZones$ Stack zone, Ulamog's shape reduced: "when
// you cast this spell, exile two target permanents". The trigger's controller
// is asked for EXACTLY two targets (TargetMin 2 / TargetMax 2) over the three
// mountains on the only nonempty battlefield, the answer is recorded onto the
// trigger's own stack object (which sits on the stack above the spell), and
// the two chosen mountains actually leave the battlefield for exile while the
// Titan spell resolves to the battlefield.
//
// Why the TriggerZones$ Stack is load-bearing here instead of the brief's
// unmodified fixture (a bare `T:Mode$ SpellCast` with no TriggerZones$): the
// spell's own source sits ON the stack when its cast trigger fires, and
// zoneGate's default trigger zone is Battlefield, so a spell-on-stack source
// never qualifies and the trigger would not fire at all -- the same pre-
// existing gap the d2e2bf4 commit records for Ulamog/World Breaker, outside
// this task's file scope. Naming the Stack zone explicitly is what lets this
// test drive the cast-trigger-takes-two-targets behaviour at all.
func TestStackZoneCastTriggerAsksForTwoTargetsAndExilesBoth(t *testing.T) {
	t.Parallel()
	src := "Name:Titan\nManaCost:1\nTypes:Creature Eldrazi\nPT:10/10\n" +
		"T:Mode$ SpellCast | TriggerZones$ Stack | ValidCard$ Card.Self | Execute$ TrigChange | TriggerDescription$ When you cast this spell, exile two target permanents.\n" +
		"SVar:TrigChange:DB$ ChangeZone | ValidTgts$ Permanent | TargetMin$ 2 | TargetMax$ 2 | Origin$ Battlefield | Destination$ Exile\nOracle:x\n"
	e, _, titan := newFixtureDeck(t, 11, src)
	addMana(t, e, 0, "R") // the cast option is only offered once {1} is payable
	lands := putLands(t, e, 1, 3)
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 2 || d.Max != 2 || len(d.Options) != 3 {
		t.Fatalf("decision %+v", d)
	}
	submitChoices(t, e, 0, 1)
	top := e.G.Obj(e.G.Stack[len(e.G.Stack)-1])
	if top.Ability == nil || len(top.Targets) != 2 {
		t.Fatalf("top of stack %+v", top)
	}
	passUntilStackEmpty(t, e, 50)
	exiled := 0
	for _, id := range lands {
		if e.G.Obj(id).Zone == state.ZExile {
			exiled++
		}
	}
	if exiled != 2 || e.G.Obj(titan).Zone != state.ZBattlefield {
		t.Fatalf("exiled %d, titan in %s", exiled, e.G.Obj(titan).Zone)
	}
}

// TestTargetMinZeroResolvesWithNoTargets is requirement N2: a spell whose
// ValidTgts enumeration comes up empty but whose TargetMin$ is 0 is NOT
// countered and does NOT ask a zero-option decision -- it goes on the stack
// untargeted and still resolves its untargeted rider (the draw). The hand
// count is unchanged because casting took one card and the rider drew one.
func TestTargetMinZeroResolvesWithNoTargets(t *testing.T) {
	t.Parallel()
	src := "Name:Optional\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ DealDamage | ValidTgts$ Creature | TargetMin$ 0 | TargetMax$ 1 | NumDmg$ 2 | SubAbility$ DBDraw | SpellDescription$ x\n" +
		"SVar:DBDraw:DB$ Draw | Defined$ You | NumCards$ 1\nOracle:x\n"
	e, _, _ := newFixtureDeck(t, 12, src)
	addMana(t, e, 0, "R") // the cast option is only offered once {R} is payable
	hand := len(e.G.Zone(state.ZHand, 0))
	castFirst(t, e, "cast")
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatal("asked for a target with none available and TargetMin 0")
	}
	passUntilStackEmpty(t, e, 50)
	if len(e.G.Zone(state.ZHand, 0)) != hand { // cast -1, draw +1
		t.Fatal("the untargeted spell did not resolve its rider")
	}
}

// TestTgtZoneGraveyardOffersGraveyardCards is the Snapcaster ETB shape,
// reduced: a trigger that targets "instant or sorcery card in your
// graveyard" (TgtZone$ Graveyard) must offer exactly the graveyard card that
// satisfies the YouCtrl spec -- and NOT any player, even though "YouCtrl"
// contains the substring "You" that the coarse targetsPlayers heuristic keys
// on. Routing to an off-board zone is what suppresses the player slots.
func TestTgtZoneGraveyardOffersGraveyardCards(t *testing.T) {
	t.Parallel()
	boltSrc := "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n"
	src := "Name:Mage\nManaCost:1 U\nTypes:Creature Human Wizard\nPT:2/1\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigPump | TriggerDescription$ x\n" +
		"SVar:TrigPump:DB$ Pump | ValidTgts$ Instant.YouCtrl,Sorcery.YouCtrl | TgtZone$ Graveyard | KW$ Flashback | PumpZone$ Graveyard\nOracle:x\n"
	e, _, mage := newFixtureDeck(t, 13, src, boltSrc)
	bolt := addToGraveyard(t, e, 0, boltSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: mage, From: state.ZHand, To: state.ZBattlefield})
	e.Advance()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || len(d.Options) != 1 || d.Options[0].Obj != bolt {
		t.Fatalf("decision %+v", d)
	}
}

// TestTargetBoundsClampsTargetMaxNotDiscardsIt is fix-round-1's rendering of
// targetBounds' documented contract: the rule is clamp, not ignore. A
// TargetMax$ that parses to 0 (or a negative number) is taken seriously and
// clamped to >= 1 and >= Min -- never silently dropped back to the default of
// 1, which would behave differently the moment TargetMin$ is also present in
// the same script. The observable result for a lone TargetMax$ 0 is the same
// either way, which is what makes "discard" and "clamp" easy to conflate;
// this table pins the clamped behaviour (and the Min-0 untargeted stand-outs
// that must NOT be clobbered).
func TestTargetBoundsClampsTargetMaxNotDiscardsIt(t *testing.T) {
	t.Parallel()
	sa := func(params ...string) *cards.SA {
		m := map[string]string{}
		for _, p := range params {
			k, v, ok := strings.Cut(p, "$")
			if !ok {
				t.Fatalf("bad param %q", p)
			}
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
		return &cards.SA{Params: m}
	}
	for _, tc := range []struct {
		name             string
		sa               *cards.SA
		wantMin, wantMax int
	}{
		{"absent defaults to one each", sa(), 1, 1},
		{"TargetMax zero clamps to one", sa("TargetMax$ 0"), 1, 1},
		{"TargetMax negative clamps to one", sa("TargetMax$ -3"), 1, 1},
		{"TargetMin zero stays zero (N2 untargeted)", sa("TargetMin$ 0"), 0, 1},
		{"TargetMin zero keeps an explicit TargetMax zero at one", sa("TargetMin$ 0", "TargetMax$ 0"), 0, 1},
		{"TargetMax below TargetMin clamps up to Min", sa("TargetMin$ 3", "TargetMax$ 1"), 3, 3},
		{"both present and sane are kept", sa("TargetMin$ 2", "TargetMax$ 4"), 2, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			min, max := targetBounds(tc.sa)
			if min != tc.wantMin || max != tc.wantMax {
				t.Fatalf("targetBounds = (%d, %d), want (%d, %d)", min, max, tc.wantMin, tc.wantMax)
			}
		})
	}
}

// TestResolvedTargetBoundsDynamic extends the clamp contract to the dynamic
// bounds resolvedTargetBounds serves: a literal token passes through
// unchanged; TargetMax$ X resolves through the effects numeric grammar (an
// SVar name, or the bare X bound to the caller's x); a token the grammar
// cannot resolve keeps today's default-1 semantics (never a degrade-to-zero,
// which would open resolveTop's N2 exemption for a mandatory Min); and the
// clamps apply AFTER resolution. The fixture face is reached through a hand
// object, the same anchor targetBoundCtx uses for a spell -- the end-to-end
// ask behaviour is pinned in targetmax_x_test.go and the Mantle pin in
// changezone_attachedto_test.go.
func TestResolvedTargetBoundsDynamic(t *testing.T) {
	t.Parallel()
	xVolley := card(t, "Name:Bound Volley\nManaCost:R\nTypes:Instant\n"+
		"A:SP$ Draw | Defined$ You | NumCards$ 1 | ValidTgts$ Creature | TargetMax$ X\n"+
		"SVar:X:Count$Valid Creature.YouCtrl\nOracle:x\n")
	xMin := card(t, "Name:Bound Min\nManaCost:R\nTypes:Instant\n"+
		"A:SP$ Draw | Defined$ You | NumCards$ 1 | ValidTgts$ Creature | TargetMin$ X | TargetMax$ X\n"+
		"SVar:X:Count$Valid Creature.YouCtrl\nOracle:x\n")
	bareX := card(t, "Name:Bound Bare\nManaCost:X R\nTypes:Sorcery\n"+
		"A:SP$ Draw | Defined$ You | NumCards$ 1 | ValidTgts$ Creature | TargetMax$ X\nOracle:x\n")
	unresolvable := card(t, "Name:Bound Y\nManaCost:R\nTypes:Instant\n"+
		"A:SP$ Draw | Defined$ You | NumCards$ 1 | ValidTgts$ Creature | TargetMin$ Y | TargetMax$ Y\nOracle:x\n")
	bear := card(t, testBearSrc)
	e := handEngine(t, xVolley, xMin, bareX, unresolvable, bear, bear)
	ids := map[string]state.ObjID{}
	for _, id := range e.G.Zone(state.ZHand, 0) {
		o := e.G.Obj(id)
		ids[o.Face().Name] = id
		if o.Face().Name == "Bear" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
		}
	}
	for _, tc := range []struct {
		name             string
		id               state.ObjID
		x                int32
		wantMin, wantMax int
	}{
		{"X with a resolvable SVar reads the count", ids["Bound Volley"], 0, 1, 2},
		{"TargetMin$ X reads the count and clamps Max up to Min", ids["Bound Min"], 0, 2, 2},
		{"bare X reads the caller's x", ids["Bound Bare"], 3, 1, 3},
		{"bare X zero clamps back to one", ids["Bound Bare"], 0, 1, 1},
		{"unresolvable Y keeps the default, not a zero", ids["Bound Y"], 0, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sa := e.G.Obj(tc.id).Face().SpellAbility()
			min, max := e.resolvedTargetBounds(0, tc.id, sa, tc.x)
			if min != tc.wantMin || max != tc.wantMax {
				t.Fatalf("resolvedTargetBounds = (%d, %d), want (%d, %d)", min, max, tc.wantMin, tc.wantMax)
			}
		})
	}
	// The resolved bound feeds resolveTop's N2 gate: an unresolvable
	// TargetMin$ X must NOT open the resolve-untargeted exemption.
	if got := e.resolvedTargetMin(0, ids["Bound Y"], e.G.Obj(ids["Bound Y"]).Face().SpellAbility(), 0); got != 1 {
		t.Fatalf("unresolvable TargetMin$ X resolved to %d, want the default 1", got)
	}
}
