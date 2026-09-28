package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// combatEngine builds a two-player engine (seats 0 and 1) with active/step
// left for the test to set, ready for onBoard placements. Reuses layerEngine's
// own construction (mountain decks, no creatures) since combat tests care
// only about the creatures they place themselves.
func combatEngine(t *testing.T) *Engine {
	t.Helper()
	e := layerEngine(t)
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	return e
}

// onBoardReady places a creature via onBoard already able to attack --
// SummonSick cleared, as if it had been under its controller's control since
// the beginning of their most recent turn (CR 302.6). onBoard itself now
// defaults to SummonSick true (Ruling T21-f), matching a real battlefield
// entry (events.Move's ZBattlefield case); most of this file's attackers want
// the opposite of that default, which is what this wrapper is for. Blockers
// never need it -- summoning sickness has no bearing on blocking -- so every
// call site below that places a blocker still uses plain onBoard.
func onBoardReady(t *testing.T, e *Engine, p state.PlayerID, src string) state.ObjID {
	t.Helper()
	id := onBoard(t, e, p, src)
	e.G.Obj(id).SummonSick = false
	return id
}

// drainCombatPriority answers "pass" for every priority decision the engine
// offers while it is still inside a combat declaration step. CR 508.2 and
// 509.2 give players priority in the declare steps AFTER the declaration is
// made, so a fixture that declares attackers or blockers and then wants the
// NEXT combat event has to cross that window first. Before that window
// existed the declaration handlers advanced the step themselves and these
// helpers returned straight into the following step; the window is the
// rules-correct behaviour, and draining it here is what keeps the fixtures
// expressing "declare, then get on with combat" rather than encoding how
// many decisions combat happens to contain.
func drainCombatPriority(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 64; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			return
		}
		if e.G.Step != state.StepDeclareAttackers && e.G.Step != state.StepDeclareBlockers {
			return
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("combat priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("pass combat priority: %v", err)
		}
	}
	t.Fatal("combat priority window did not close within the pass budget")
}

// drainCombatDamagePriority answers "pass" for every priority decision the
// engine offers while still inside the combat damage step. Task jj-cmb (F37)
// introduced the between-passes priority round (CR 510.4): a first-strike
// attacker deals its damage, then players gain priority in the combat damage
// step before the regular pass runs. A fixture that declared a first striker
// and wants "the rest of combat" has to cross that window first, exactly as
// drainCombatPriority crosses the 508.2/509.2 windows.
func drainCombatDamagePriority(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 64; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority || e.G.Step != state.StepCombatDamage {
			return
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
				break
			}
		}
		if idx < 0 {
			t.Fatalf("combat-damage priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("pass combat-damage priority: %v", err)
		}
	}
	t.Fatal("combat-damage priority window did not close within the pass budget")
}

// submitDivision answers the combat damage step's controller damage-division
// decision (CR 510.1c, Task jj-cmb F40) with the option whose per-blocker
// split equals want. The engine's pending ask already exposes the split
// table (askOptions) in lockstep with the option list, so a fixture names the
// division it wants rather than guessing an option index.
func submitDivision(t *testing.T, e *Engine, want []int32) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected a damage-division decision, got %+v", d)
	}
	for i, sp := range e.combatRound.askOptions {
		if reflect.DeepEqual(sp, want) {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}); err != nil {
				t.Fatalf("submit damage division: %v", err)
			}
			return
		}
	}
	t.Fatalf("no damage-division option for split %v (have %v)", want, e.combatRound.askOptions)
}

// submitAttackers finds the KAttackers options naming each of ids and submits
// exactly those. Order does not matter for attackers (every one of them ends
// up sharing the same M1-fixed defender), unlike submitBlockers below.
func submitAttackersOnly(t *testing.T, e *Engine, ids ...state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	want := make(map[state.ObjID]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var choices []int
	for _, o := range d.Options {
		if want[o.Obj] {
			choices = append(choices, o.Index)
		}
	}
	if len(choices) != len(ids) {
		t.Fatalf("only %d of %d requested attackers were offered: %+v", len(choices), len(ids), d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("submit attackers: %v", err)
	}
}

// submitAttackers declares and then crosses the CR 508.2 priority window, so
// a fixture that only wants "attackers are declared, now show me the next
// combat event" reads the same as it did before that window existed. A test
// about the window ITSELF must use submitAttackersOnly and drive priority by
// hand -- draining here would answer the very decision it means to inspect.
func submitAttackers(t *testing.T, e *Engine, ids ...state.ObjID) {
	t.Helper()
	submitAttackersOnly(t, e, ids...)
	drainCombatPriority(t, e)
}

// submitBlockers finds, for each blockerID in the caller's own order, the
// KBlockers option that names it, and submits those indices in that same
// order. Order is significant here (unlike submitAttackers): it becomes the
// Pairs order in the DeclareBlockers event, which BlockedBy preserves, which
// is what dealCombatDamage's damage-assignment loop reads as blocker order.
func submitBlockersOnly(t *testing.T, e *Engine, blockerIDs ...state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	var choices []int
	for _, bid := range blockerIDs {
		idx := -1
		for _, o := range d.Options {
			if o.Obj == bid {
				idx = o.Index
				break
			}
		}
		if idx < 0 {
			t.Fatalf("no block option for %d: %+v", bid, d.Options)
		}
		choices = append(choices, idx)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("submit blockers: %v", err)
	}
}

// submitBlockers crosses the CR 509.2 priority window, for the same reason
// submitAttackers crosses CR 508.2's. submitBlockersOnly stops before it.
func submitBlockers(t *testing.T, e *Engine, blockerIDs ...state.ObjID) {
	t.Helper()
	submitBlockersOnly(t, e, blockerIDs...)
	drainCombatPriority(t, e)
}

func TestBlockerDeclarationLegality(t *testing.T) {
	t.Parallel()
	const creature = "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n"

	t.Run("one blocker cannot block two attackers", func(t *testing.T) {
		e := combatEngine(t)
		first := onBoardReady(t, e, 0, creature)
		second := onBoardReady(t, e, 0, creature)
		blocker := onBoard(t, e, 1, creature)
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{first, second}})
		e.G.Step = state.StepDeclareBlockers
		e.askBlockers()

		d := e.Pending()
		if d == nil || d.Kind != decision.KBlockers {
			t.Fatalf("expected a blockers decision, got %+v", d)
		}
		var choices []int
		for _, o := range d.Options {
			if o.Obj == blocker {
				choices = append(choices, o.Index)
			}
		}
		if len(choices) != 2 {
			t.Fatalf("block options for blocker %d = %v, want one per attacker", blocker, choices)
		}
		beforeIntents, beforeEvents := len(e.L.Intents), len(e.L.Events)
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err == nil {
			t.Fatal("one ordinary blocker was allowed to block two attackers")
		}
		if e.Pending() != d {
			t.Fatalf("rejected declaration consumed pending decision: got %+v, want %+v", e.Pending(), d)
		}
		if len(e.L.Intents) != beforeIntents || len(e.L.Events) != beforeEvents {
			t.Fatalf("rejected declaration changed log: intents %d -> %d, events %d -> %d", beforeIntents, len(e.L.Intents), beforeEvents, len(e.L.Events))
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices[:1]}); err != nil {
			t.Fatalf("pending decision was not answerable with a legal block: %v", err)
		}
	})

	t.Run("two blockers may block one attacker", func(t *testing.T) {
		e := combatEngine(t)
		attacker := onBoardReady(t, e, 0, creature)
		onBoard(t, e, 1, creature)
		onBoard(t, e, 1, creature)
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{attacker}})
		e.G.Step = state.StepDeclareBlockers
		e.askBlockers()

		d := e.Pending()
		if d == nil || d.Kind != decision.KBlockers || len(d.Options) != 2 {
			t.Fatalf("expected two block options for one attacker, got %+v", d)
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index, d.Options[1].Index}}
		if err := e.Submit(in); err != nil {
			t.Fatalf("valid multi-block was rejected: %v", err)
		}
	})
}

func TestMenaceBlockDeclarationNeedsZeroOrAtLeastTwoBlockers(t *testing.T) {
	t.Parallel()
	const menace = "Name:Goblin Glory Chaser\nManaCost:R\nTypes:Creature Goblin Warrior\nPT:1/1\nK:Menace\nOracle:x\n"
	const blocker = "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n"

	for _, tc := range []struct {
		name    string
		blocks  int
		wantErr bool
	}{
		{name: "unblocked", blocks: 0},
		{name: "one blocker", blocks: 1, wantErr: true},
		{name: "two blockers", blocks: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := combatEngine(t)
			attacker := onBoardReady(t, e, 0, menace)
			onBoard(t, e, 1, blocker)
			onBoard(t, e, 1, blocker)
			e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{attacker}})
			e.G.Step = state.StepDeclareBlockers
			e.askBlockers()

			d := e.Pending()
			if d == nil || d.Kind != decision.KBlockers || len(d.Options) != 2 {
				t.Fatalf("expected two block options, got %+v", d)
			}
			choices := make([]int, tc.blocks)
			for i := range choices {
				choices[i] = d.Options[i].Index
			}
			beforeIntents, beforeEvents := len(e.L.Intents), len(e.L.Events)
			err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices})
			if tc.wantErr {
				if err == nil {
					t.Fatal("a menace attacker was blocked by exactly one creature")
				}
				if e.Pending() != d || len(e.L.Intents) != beforeIntents || len(e.L.Events) != beforeEvents {
					t.Fatal("rejected menace declaration consumed the decision or changed the log")
				}
				return
			}
			if err != nil {
				t.Fatalf("legal declaration with %d blockers was rejected: %v", tc.blocks, err)
			}
		})
	}
}

func TestCombatPriorityDoesNotRepeatDeclarations(t *testing.T) {
	t.Parallel()
	const creature = "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n"

	passOnce := func(t *testing.T, e *Engine) {
		t.Helper()
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("expected priority, got %+v", d)
		}
		idx := -1
		for _, opt := range d.Options {
			if opt.Kind == "pass" {
				idx = opt.Index
				break
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision has no pass option: %+v", d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("pass priority: %v", err)
		}
	}
	countEvents := func(e *Engine, kind events.Kind) int {
		n := 0
		for _, ev := range e.L.Events {
			if ev.Kind == kind {
				n++
			}
		}
		return n
	}

	t.Run("attackers", func(t *testing.T) {
		e := combatEngine(t)
		attacker := onBoardReady(t, e, 0, creature)
		e.askAttackers()
		submitAttackersOnly(t, e, attacker)
		before := countEvents(e, events.DeclareAttackers)

		passOnce(t, e)
		if d := e.Pending(); d == nil || d.Kind != decision.KPriority || e.G.Step != state.StepDeclareAttackers {
			t.Fatalf("post-pass decision = %+v at %s, want opponent priority in declare attackers", d, e.G.Step)
		}
		if got := countEvents(e, events.DeclareAttackers); got != before {
			t.Fatalf("DeclareAttackers events = %d after priority advanced, want %d (no redeclaration)", got, before)
		}
	})

	t.Run("blockers", func(t *testing.T) {
		e := combatEngine(t)
		attacker := onBoardReady(t, e, 0, creature)
		onBoard(t, e, 1, creature)
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{attacker}})
		e.G.Step = state.StepDeclareBlockers
		e.askBlockers()
		submitBlockersOnly(t, e)
		before := countEvents(e, events.DeclareBlockers)

		passOnce(t, e)
		if d := e.Pending(); d == nil || d.Kind != decision.KPriority || e.G.Step != state.StepDeclareBlockers {
			t.Fatalf("post-pass decision = %+v at %s, want opponent priority in declare blockers", d, e.G.Step)
		}
		if got := countEvents(e, events.DeclareBlockers); got != before {
			t.Fatalf("DeclareBlockers events = %d after priority advanced, want %d (no redeclaration)", got, before)
		}
	})
}

func TestUnblockedAttackerDamagesTheDefendingPlayer(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardReady(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	e.askAttackers()
	submitAttackers(t, e, atk)

	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("defender life = %d, want 18", got)
	}
}

func TestWallOfOmensWithDefenderCannotAttack(t *testing.T) {
	t.Parallel()
	const wallOfOmens = "Name:Wall of Omens\nManaCost:1 W\nTypes:Creature Wall\nPT:0/4\nK:Defender\nOracle:x\n"
	const bear = "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

	e := combatEngine(t)
	wall := onBoardReady(t, e, 0, wallOfOmens)
	legal := onBoardReady(t, e, 0, bear)
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision for the other creature, got %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != legal {
		t.Fatalf("attacker options = %+v, want only the non-Defender creature %d", d.Options, legal)
	}
	if e.canAttack(wall) {
		t.Fatal("Wall of Omens with Defender may attack")
	}

	// A fabricated/stale decision cannot bypass the declaration-time guard.
	e2 := combatEngine(t)
	wall = onBoardReady(t, e2, 0, wallOfOmens)
	e2.ask(&decision.Decision{Player: 0, Kind: decision.KAttackers, Min: 0, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "attacker", Obj: wall, Player: 1}}})
	d = e2.Pending()
	beforeIntents, beforeEvents := len(e2.L.Intents), len(e2.L.Events)
	if err := e2.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err == nil {
		t.Fatal("a hand-built intent declaring Wall of Omens as an attacker was accepted")
	}
	if e2.Pending() != d || len(e2.L.Intents) != beforeIntents || len(e2.L.Events) != beforeEvents {
		t.Fatal("rejected Defender declaration consumed the decision or changed the log")
	}
}

func TestBotCombatDeclarationsRemainLegalWithDefenderAndMenace(t *testing.T) {
	t.Parallel()
	const wall = "Name:Wall of Omens\nManaCost:1 W\nTypes:Creature Wall\nPT:0/4\nK:Defender\nOracle:x\n"
	const bear = "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	const menace = "Name:Goblin Glory Chaser\nManaCost:R\nTypes:Creature Goblin Warrior\nPT:3/3\nK:Menace\nOracle:x\n"
	const blocker = "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n"

	t.Run("Defender is never submitted as an attacker", func(t *testing.T) {
		e := combatEngine(t)
		wallID := onBoardReady(t, e, 0, wall)
		bearID := onBoardReady(t, e, 0, bear)
		e.askAttackers()
		d := e.Pending()
		bot := newTestBot(1)
		in := bot.answer(e, d)
		for _, o := range d.Chosen(in) {
			if o.Obj == wallID {
				t.Fatal("bot submitted a Defender creature as an attacker")
			}
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("bot attacker declaration was rejected: %v", err)
		}
		if e.G.Obj(bearID) == nil {
			t.Fatal("test setup lost the legal attacker")
		}
	})

	t.Run("Menace gets a legal two-creature block", func(t *testing.T) {
		e := combatEngine(t)
		e.G.Active = 1
		attacker := onBoardReady(t, e, 1, menace)
		onBoard(t, e, 0, blocker)
		onBoard(t, e, 0, blocker)
		e.G.Players[0].Life = 2 // make the otherwise-unblocked attack lethal
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{attacker}})
		e.G.Step = state.StepDeclareBlockers
		e.askBlockers()
		d := e.Pending()
		bot := newTestBot(2)
		in := bot.answer(e, d)
		if len(in.Choices) != 2 {
			t.Fatalf("bot chose %d blockers for lethal Menace attacker, want 2: %+v", len(in.Choices), in)
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("bot menace declaration was rejected (would deadlock the step): %v", err)
		}
	})
}

func TestSummoningSickCreatureCannotAttackWithoutHaste(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	sick := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Obj(sick).SummonSick = true

	e.askAttackers()
	if d := e.Pending(); d != nil {
		t.Fatalf("expected no attackers decision (no legal attacker), got %+v", d)
	}
	if e.canAttack(sick) {
		t.Fatal("a summoning-sick creature without Haste should not be able to attack")
	}

	// Reset combat and try again with a hasty creature: now there is exactly
	// one attacker option.
	e2 := combatEngine(t)
	hasty := onBoard(t, e2, 0, "Name:Raider\nManaCost:1 R\nTypes:Creature Goblin\nPT:2/2\nK:Haste\nOracle:x\n")
	e2.G.Obj(hasty).SummonSick = true

	e2.askAttackers()
	d := e2.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	if len(d.Options) != 1 {
		t.Fatalf("attacker options = %d, want 1 (Haste overrides summoning sickness)", len(d.Options))
	}
	if !e2.canAttack(hasty) {
		t.Fatal("a summoning-sick creature with Haste should be able to attack")
	}
}

func TestVigilanceAttackerStaysUntapped(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardReady(t, e, 0, "Name:Sentinel\nManaCost:2 W\nTypes:Creature Soldier\nPT:2/2\nK:Vigilance\nOracle:x\n")

	e.askAttackers()
	submitAttackers(t, e, atk)

	if e.G.Obj(atk).Tapped {
		t.Fatal("a Vigilance attacker should not tap when declared")
	}
}

func TestBlockedAttackerAndBlockerTradeDamage(t *testing.T) {
	t.Parallel()
	// Toughness 4 on both sides, well above either creature's power, so the
	// exchange is observable afterward: events.Move clears Object.Damage the
	// instant something actually dies (see TestDeathtouchKillsRegardlessOf-
	// Toughness and friends for that case instead), so a trade that is meant
	// to be inspected for exact damage amounts must be one neither side dies
	// from.
	e := combatEngine(t)
	atk := onBoardReady(t, e, 0, "Name:Ox\nManaCost:2 G\nTypes:Creature Ox\nPT:2/4\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:1/4\nOracle:x\n")

	e.askAttackers()
	submitAttackers(t, e, atk)
	submitBlockers(t, e, blk)

	if got := e.G.Obj(atk).Damage; got != 1 {
		t.Fatalf("attacker damage = %d, want 1 (the blocker's power)", got)
	}
	if got := e.G.Obj(blk).Damage; got != 2 {
		t.Fatalf("blocker damage = %d, want 2 (the attacker's power)", got)
	}
	if o := e.G.Obj(atk); o.Zone != state.ZBattlefield {
		t.Fatalf("attacker zone = %v, want battlefield (1 damage on a 2/4 is not lethal)", o.Zone)
	}
	if o := e.G.Obj(blk); o.Zone != state.ZBattlefield {
		t.Fatalf("blocker zone = %v, want battlefield (2 damage on a 1/4 is not lethal)", o.Zone)
	}
}

func TestFlyingCannotBeBlockedByGroundCreature(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	flier := onBoard(t, e, 0, "Name:Griffin\nManaCost:2 W\nTypes:Creature Griffin\nPT:2/2\nK:Flying\nOracle:x\n")
	ground := onBoard(t, e, 1, "Name:Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/2\nOracle:x\n")

	// Declare the attacker directly (state only, not through the decision
	// flow) so askBlockers can be exercised in isolation, without its own
	// result cascading straight through combat damage via Submit's Advance.
	fo := e.G.Obj(flier)
	fo.IsAttacking = true
	fo.Attacking = 1

	e.askBlockers()
	if d := e.Pending(); d != nil {
		t.Fatalf("a ground creature should not be offered as a blocker for a flier, got %+v", d)
	}
	if e.canBlock(ground, flier) {
		t.Fatal("canBlock should refuse a ground creature blocking a flier")
	}

	e2 := combatEngine(t)
	flier2 := onBoard(t, e2, 0, "Name:Griffin\nManaCost:2 W\nTypes:Creature Griffin\nPT:2/2\nK:Flying\nOracle:x\n")
	reach := onBoard(t, e2, 1, "Name:Archer\nManaCost:1 G\nTypes:Creature Elf\nPT:1/2\nK:Reach\nOracle:x\n")
	fo2 := e2.G.Obj(flier2)
	fo2.IsAttacking = true
	fo2.Attacking = 1

	e2.askBlockers()
	d := e2.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	if len(d.Options) != 1 {
		t.Fatalf("block options = %d, want 1 (the Reach creature)", len(d.Options))
	}
	if !e2.canBlock(reach, flier2) {
		t.Fatal("canBlock should allow a Reach creature blocking a flier")
	}
}

func TestDeathtouchKillsRegardlessOfToughness(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardReady(t, e, 0, "Name:Ogre\nManaCost:4 R\nTypes:Creature Ogre\nPT:5/5\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Adder\nManaCost:G\nTypes:Creature Snake\nPT:1/1\nK:Deathtouch\nOracle:x\n")

	e.askAttackers()
	submitAttackers(t, e, atk)
	submitBlockers(t, e, blk)

	if o := e.G.Obj(atk); o.Zone != state.ZGraveyard {
		t.Fatalf("attacker zone = %v, want graveyard (a single deathtouch point should be lethal to a 5/5)", o.Zone)
	}
}

func TestTrampleAssignsExcessToThePlayer(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardReady(t, e, 0, "Name:Rhino\nManaCost:3 G G\nTypes:Creature Rhino\nPT:5/5\nK:Trample\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/2\nOracle:x\n")

	e.askAttackers()
	submitAttackers(t, e, atk)
	submitBlockers(t, e, blk)

	if got := e.G.Players[1].Life; got != 17 {
		t.Fatalf("defender life = %d, want 17 (20 - 3 trample excess)", got)
	}
	// The blocker took exactly its own toughness in damage (2, the minimum
	// Trample must assign before spilling over) and so died to state-based
	// actions -- which is also why its Damage cannot be asserted here
	// directly: events.Move clears it the instant the creature actually
	// leaves the battlefield.
	if o := e.G.Obj(blk); o.Zone != state.ZGraveyard {
		t.Fatalf("blocker zone = %v, want graveyard (assigned exactly lethal damage)", o.Zone)
	}
}

func TestLifelinkGainsLifeOnCombatDamage(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardReady(t, e, 0, "Name:Cleric\nManaCost:2 W\nTypes:Creature Cleric\nPT:3/3\nK:Lifelink\nOracle:x\n")

	e.askAttackers()
	submitAttackers(t, e, atk)

	if got := e.G.Players[0].Life; got != 23 {
		t.Fatalf("attacker controller life = %d, want 23 (20 + 3 lifelink)", got)
	}
	if got := e.G.Players[1].Life; got != 17 {
		t.Fatalf("defender life = %d, want 17", got)
	}
}

func TestFirstStrikeKillsBeforeTheNormalDamageStep(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardReady(t, e, 0, "Name:Duelist\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/1\nK:First Strike\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Peasant\nManaCost:B\nTypes:Creature Human\nPT:1/1\nOracle:x\n")

	e.askAttackers()
	submitAttackers(t, e, atk)
	submitBlockers(t, e, blk)

	if o := e.G.Obj(blk); o.Zone != state.ZGraveyard {
		t.Fatalf("blocker zone = %v, want graveyard (killed in the first-strike step)", o.Zone)
	}
	if got := e.G.Obj(atk).Damage; got != 0 {
		t.Fatalf("first striker damage = %d, want 0 (the blocker died before dealing any)", got)
	}
	if o := e.G.Obj(atk); o.Zone != state.ZBattlefield {
		t.Fatalf("first striker zone = %v, want battlefield (it took no damage back)", o.Zone)
	}
}

// TestFirstStrikeReplacementOrderSettlesBeforeRegularPass pins a CR 510.4
// continuation: a replacement-order question during first-strike damage must
// hold both the remaining pass and its SBA until answered. Otherwise a blocker
// lethally hit by the answered first-strike damage gets an illegal regular hit.
func TestFirstStrikeReplacementOrderSettlesBeforeRegularPass(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := combatEngine(t)
	fiery := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Fiery Emancipation"))
	onBoard(t, e, 0, "Name:Plus Two\nTypes:Enchantment\n"+
		"R:Event$ DamageDone | ActiveZones$ Battlefield | ValidSource$ Card.YouCtrl | ValidTarget$ Creature | ReplaceWith$ D\n"+
		"SVar:D:DB$ ReplaceEffect | VarName$ DamageAmount | VarValue$ X\n"+
		"SVar:X:ReplaceCount$DamageAmount/Plus.2\nOracle:x\n")
	atk := onBoardReady(t, e, 0, "Name:Duelist\nTypes:Creature Soldier\nPT:2/2\nK:First Strike\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Guard\nTypes:Creature Soldier\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{atk}})
	e.emit(events.Event{Kind: events.DeclareBlockers, Player: 1, Pairs: [][2]state.ObjID{{atk, blk}}})
	e.combatRound = combatRound{hasFirst: true, pass: true, active: true}
	e.finishCombatPass()
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement {
		t.Fatalf("pending = %+v, want first-strike replacement order", d)
	}
	if got := e.G.Obj(blk).Damage; got != 0 {
		t.Fatalf("blocker damage before answer = %d, want parked", got)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Obj == fiery {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Fiery missing from %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatal(err)
	}
	if o := e.G.Obj(blk); o.Zone != state.ZGraveyard {
		t.Fatalf("blocker zone = %s, want graveyard before regular pass", o.Zone)
	}
	if got := e.G.Obj(atk).Damage; got != 0 {
		t.Fatalf("attacker damage = %d, want 0: dead blocker cannot deal regular damage", got)
	}
}

func TestMultipleBlockersEachTakeDamage(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardReady(t, e, 0, "Name:Rhino\nManaCost:3 G G\nTypes:Creature Rhino\nPT:3/5\nK:Trample\nOracle:x\n")
	first := onBoard(t, e, 1, "Name:First Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/2\nOracle:x\n")
	second := onBoard(t, e, 1, "Name:Second Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/2\nOracle:x\n")

	e.askAttackers()
	submitAttackers(t, e, atk)
	// Declared in this exact order: the attacker's 3 power assigns 2 (lethal)
	// to the first-declared blocker before the second-declared blocker sees
	// any of the remainder.
	submitBlockers(t, e, first, second)

	// The first-declared blocker was assigned its full lethal need (2) before
	// the second saw any of the remainder, and died to state-based actions --
	// which is also why only the survivor's exact Damage is asserted directly:
	// events.Move clears the field the instant something actually dies.
	if got := e.G.Obj(second).Damage; got != 1 {
		t.Fatalf("second-declared blocker damage = %d, want 1 (only the leftover)", got)
	}
	if o := e.G.Obj(first); o.Zone != state.ZGraveyard {
		t.Fatalf("first-declared blocker zone = %v, want graveyard (assigned exactly lethal damage)", o.Zone)
	}
	if o := e.G.Obj(second); o.Zone != state.ZBattlefield {
		t.Fatalf("second-declared blocker zone = %v, want battlefield (only 1 damage, not lethal)", o.Zone)
	}
}

func TestCombatDamageUsesDerivedPowerNotPrinted(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardReady(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{Source: atk, Timestamp: 1, Layer: LPT, Sub: SubModify,
		Affects: "Card.Self", Controller: 0, AddPower: 3, AddToughness: 3})

	e.askAttackers()
	submitAttackers(t, e, atk)

	if got := e.G.Players[1].Life; got != 15 {
		t.Fatalf("defender life = %d, want 15 (20 - 5 derived power, not the printed 2)", got)
	}
}

// TestCleanupClearsCombatDamageDeathtouchAndUntilEOTEffects is the regression
// test for a gap outside this task's own 11 named tests but explicitly in its
// scope: EndOfTurnCleanup (layers.go, built and tested since Task 19c) was
// never called from anywhere in the engine, so a resolved pump effect used to
// survive forever instead of expiring at the end of its own turn -- and
// nothing cleared marked damage or the Deathtouched counter this task
// introduces either. This drives the real cleanup path (priorityRound, not a
// direct EndOfTurnCleanup call) so the wiring itself is under test, not just
// the primitive layers_test.go already covers in isolation.
func TestCleanupClearsCombatDamageDeathtouchAndUntilEOTEffects(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	id := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	o := e.G.Obj(id)
	o.Damage = 1
	o.AddCounter("Deathtouched", 1)
	e.AddContinuous(ContinuousEffect{Source: id, Timestamp: 1, Layer: LPT, Sub: SubModify,
		Affects: "Card.Self", Controller: 0, AddPower: 3, AddToughness: 3, UntilEOT: true})

	if got := e.Power(id); got != 5 {
		t.Fatalf("power before cleanup = %d, want 5 (2 printed + 3 until-end-of-turn)", got)
	}

	e.G.Step = state.StepCleanup
	e.priorityRound()

	if o.Damage != 0 {
		t.Fatalf("damage after cleanup = %d, want 0", o.Damage)
	}
	if o.Counter("Deathtouched") != 0 {
		t.Fatalf("Deathtouched after cleanup = %d, want 0", o.Counter("Deathtouched"))
	}
	if got := e.Power(id); got != 2 {
		t.Fatalf("power after cleanup = %d, want 2 (the until-end-of-turn effect should have expired)", got)
	}
}

// --- Fix round 1 (code review) ---------------------------------------------

// TestFirstStrikeAttackerTakesSurvivingBlockersRegularDamage is Ruling
// T21-a's carrier: damageStep used to gate the "blockers hit back" section
// on whether the ATTACKER itself acted this step, so once a first-strike
// attacker's own first-strike-step turn was done, the whole rest of its
// block was skipped too -- including a blocker that survived the
// first-strike step and was due to swing back in the regular step.
// TestFirstStrikeKillsBeforeTheNormalDamageStep alone could not catch this:
// its blocker dies in the first-strike step and so never gets a chance to
// hit back regardless of the bug. This one uses a blocker with enough
// toughness to survive the first-strike step specifically to reach the
// broken path -- reproducing the reviewer's own repro (a 2/5 first striker
// vs. a 3/5 vanilla blocker used to leave the attacker on 0 damage; it
// should be 3).
func TestFirstStrikeAttackerTakesSurvivingBlockersRegularDamage(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardReady(t, e, 0, "Name:Duelist\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/5\nK:First Strike\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Guard\nManaCost:2 W\nTypes:Creature Soldier\nPT:3/5\nOracle:x\n")

	e.askAttackers()
	submitAttackers(t, e, atk)
	submitBlockers(t, e, blk)
	// Task jj-cmb (F37): the first-strike pass dealt 2 to the blocker and
	// then granted priority in the combat damage step (CR 510.4) before the
	// regular pass runs. Cross that window so the surviving blocker swings
	// back in the regular step.
	drainCombatDamagePriority(t, e)

	if got := e.G.Obj(blk).Damage; got != 2 {
		t.Fatalf("blocker damage = %d, want 2 (the first striker's power, dealt in the first-strike step)", got)
	}
	if got := e.G.Obj(atk).Damage; got != 3 {
		t.Fatalf("first striker damage = %d, want 3 (the surviving blocker's regular-step damage)", got)
	}
	if o := e.G.Obj(atk); o.Zone != state.ZBattlefield {
		t.Fatalf("first striker zone = %v, want battlefield (3 damage on a 2/5 is not lethal)", o.Zone)
	}
	if o := e.G.Obj(blk); o.Zone != state.ZBattlefield {
		t.Fatalf("blocker zone = %v, want battlefield (2 damage on a 3/5 is not lethal)", o.Zone)
	}
}

// TestNonTrampleMultipleBlockersAssignLethalThenSpill is Ruling T21-b's
// carrier: without Trample, damageStep used to hand the ENTIRE remaining
// power to whichever blocker was declared first (uncapped, regardless of
// need) and leave every later blocker at exactly 0 -- CR 510.1c instead
// requires at least lethal to each blocker, in declaration order, before any
// spills to the next. TestMultipleBlockersEachTakeDamage does not exercise
// this: its attacker has Trample, which was already a different code path.
// Before this fix, this test's second-declared blocker would read 0 damage
// and stay fully healthy, not 1.
func TestNonTrampleMultipleBlockersAssignLethalThenSpill(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardReady(t, e, 0, "Name:Ox\nManaCost:2 G\nTypes:Creature Ox\nPT:3/4\nOracle:x\n")
	first := onBoard(t, e, 1, "Name:First Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:1/2\nOracle:x\n")
	second := onBoard(t, e, 1, "Name:Second Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:1/5\nOracle:x\n")

	e.askAttackers()
	submitAttackers(t, e, atk)
	submitBlockers(t, e, first, second)
	// Task jj-cmb (F40): the no-order CR 510.1c lets the attacker's
	// controller divide 3 damage between the two blockers however they
	// choose; the engine now asks that division before dealing. This fixture
	// wants the (2,1) split -- exact lethal to the 1/2 first blocker, the
	// remainder to the second -- which yields this test's asserted result.
	submitDivision(t, e, []int32{2, 1})

	// The first-declared blocker was assigned its full lethal need (2)
	// before the second saw any of the remainder, and died to state-based
	// actions -- which is also why only the survivor's exact Damage is
	// asserted directly: events.Move clears the field the instant something
	// actually dies.
	if got := e.G.Obj(second).Damage; got != 1 {
		t.Fatalf("second-declared blocker damage = %d, want 1 (the leftover after the first got its lethal 2)", got)
	}
	if o := e.G.Obj(first); o.Zone != state.ZGraveyard {
		t.Fatalf("first-declared blocker zone = %v, want graveyard (assigned its full lethal 2 before any spilled past it)", o.Zone)
	}
	if o := e.G.Obj(second); o.Zone != state.ZBattlefield {
		t.Fatalf("second-declared blocker zone = %v, want battlefield (only 1 damage on a 1/5)", o.Zone)
	}
}

// TestAskBlockersSkipsAnAttackerWithNoFace is Ruling T21-c's carrier:
// DeclareAttackers's own events.Apply case sets IsAttacking on any existing
// object, with no check that the object has a Face() at all -- so a
// malformed or tampered event (or, as reproduced here, a nil-Card object:
// the same shape an ability's own stack object already has, per Ruling F3)
// naming an "attacker" used to reach askBlockers' option-label build, which
// read e.G.Obj(aid).Face().Name unconditionally. One goroutine runs the
// whole match, so that nil-pointer dereference was a remote kill the instant
// a legal blocker for it also existed -- exactly the situation this test
// sets up (a real, legal blocker on the other side).
func TestAskBlockersSkipsAnAttackerWithNoFace(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	ghost := e.G.AddObject(nil, 0)
	ghost.Zone = state.ZBattlefield
	e.G.Clock++
	ghost.Timestamp = e.G.Clock
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), ghost.ID))
	ghost.IsAttacking = true
	ghost.Attacking = 1
	onBoard(t, e, 1, "Name:Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/2\nOracle:x\n")

	e.askBlockers() // must not panic

	if d := e.Pending(); d != nil {
		t.Fatalf("a Face()-less object should never be treated as a real attacker, got %+v", d)
	}
}

// TestAttackerWhoseBlockerIsRemovedDealsNoDamageWithoutTrample is Ruling
// T21-d's carrier (CR 509.1h): a creature that was blocked remains blocked
// for the rest of combat even if every creature blocking it is removed
// before damage is dealt -- it deals no combat damage at all (absent
// Trample), not full damage to the defending player as though it had never
// been blocked. M1 offers no priority window between blocks being declared
// and damage being dealt, so this is dormant in ordinary play today; it is
// reachable from a tampered log, and will stop being dormant the moment
// instant-speed removal exists.
//
// Reproduced by declaring the block through the real decision flow and then
// removing the blocker directly. handleBlockers is called directly rather
// than through Submit: Submit's own Advance() would cascade straight through
// dealCombatDamage before this test gets a chance to remove the blocker, so
// there would be no gap left to reproduce the bug in.
func TestAttackerWhoseBlockerIsRemovedDealsNoDamageWithoutTrample(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardReady(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/2\nOracle:x\n")

	e.askAttackers()
	submitAttackers(t, e, atk)
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == blk {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no block option for %d: %+v", blk, d.Options)
	}
	e.pending = nil
	e.handleBlockers(d, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}})

	// Remove the blocker from combat entirely (as if by some hypothetical
	// instant-speed effect) before damage is dealt.
	e.emit(events.Event{Kind: events.MoveZone, Obj: blk, From: state.ZBattlefield, To: state.ZExile})

	e.dealCombatDamage()

	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("defender life = %d, want 20 (a blocked attacker with no live blockers left deals nothing without Trample)", got)
	}
}

// TestMenaceStaysBlockedWhenOneOfTwoBlockersLeavesCombat pins CR 509.1h and
// 506.4 for Menace specifically: legality is checked once, on the declaration
// as a whole, and is not rechecked when one blocker later leaves combat.
func TestMenaceStaysBlockedWhenOneOfTwoBlockersLeavesCombat(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardReady(t, e, 0, "Name:Goblin Glory Chaser\nManaCost:R\nTypes:Creature Goblin Warrior\nPT:3/3\nK:Menace\nOracle:x\n")
	first := onBoard(t, e, 1, "Name:Guard One\nManaCost:1 W\nTypes:Creature Soldier\nPT:1/4\nOracle:x\n")
	second := onBoard(t, e, 1, "Name:Guard Two\nManaCost:1 W\nTypes:Creature Soldier\nPT:1/4\nOracle:x\n")

	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{atk}})
	e.G.Step = state.StepDeclareBlockers
	e.askBlockers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers || len(d.Options) != 2 {
		t.Fatalf("expected two block options, got %+v", d)
	}
	e.pending = nil
	e.handleBlockers(d, decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{d.Options[0].Index, d.Options[1].Index}})

	// Falling from two live blockers to one later does not undo the fact that
	// this attacker was blocked, so it cannot hit the player without Trample.
	e.emit(events.Event{Kind: events.MoveZone, Obj: first, From: state.ZBattlefield, To: state.ZExile})
	e.dealCombatDamage()
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("defender life = %d, want 20 after one Menace blocker left combat", got)
	}
	if got := e.G.Obj(second).Damage; got != 3 {
		t.Fatalf("remaining blocker damage = %d, want 3", got)
	}
}

// TestReplayReconstructsPostCombatStateExactly is the reviewer's own proof
// method for Ruling T21-e: play a real game through a full combat round
// (attackers declared, blocked, damage dealt, into end of combat), then fold
// the log alone -- via events.Apply on a clone taken just before combat --
// forward, and expect the reconstruction to match the live game exactly.
// Before this fix, turn.go's setStep wrote Object.IsAttacking/BlockedBy
// directly instead of emitting an event when leaving combat, so this
// diverged: the live game correctly cleared both fields, but the log-only
// reconstruction was never told to.
//
// The clone is taken right after board setup (onBoard, which itself bypasses
// events for placement, same as every other test in this file), not from an
// empty state.NewGame: genesis -- the deck-building AddObject calls -- is
// documented as never logged (rules/engine.go's package doc, Ruling D), so a
// truly-from-scratch state.NewGame has an empty object arena no matter what
// the live game's board looks like. From an identical starting point,
// though, "does the rest of the log alone reproduce the rest of the game" is
// exactly what is under test here, and it is answered with a full
// reflect.DeepEqual of both entire Games, not a hand-picked subset of
// fields.
//
// Both creatures survive combat (2/4 vs. 1/4, as in TestBlockedAttackerAnd-
// BlockerTradeDamage) on purpose: a creature that dies has its
// IsAttacking/BlockedBy cleared anyway, by the already-event-sourced Move
// (leaving the battlefield resets both), so a mutual-kill scenario would
// never actually exercise the buggy direct write this test exists to catch.
//
// Two controls prove the comparison itself discriminates rather than
// trivially agreeing regardless of input: truncating the log, and
// perturbing one real (Damage) event's payload, must both cause the
// reconstruction to diverge from the live game.
func TestReplayReconstructsPostCombatStateExactly(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardReady(t, e, 0, "Name:Ox\nManaCost:2 G\nTypes:Creature Ox\nPT:2/4\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:1/4\nOracle:x\n")

	before := e.G.Clone()
	firstNewEvent := len(e.L.Events)

	e.askAttackers()
	submitAttackers(t, e, atk)
	submitBlockers(t, e, blk)

	// CR 511.3 removes creatures from combat as the end of combat step ENDS,
	// so submitBlockers -- which stops at end-of-combat priority -- leaves the
	// attacker still in combat. Drive on into postcombat main, which is where
	// the reset has actually happened. This assertion is the reason the test
	// exists: if it were simply dropped, the captured window would no longer
	// contain the EndCombatReset and the reconstruction below would agree
	// with the live game whether or not the reset was event-sourced.
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepMain2)

	if e.G.Obj(atk).IsAttacking || e.G.Obj(atk).BlockedBy != nil {
		t.Fatal("setup sanity: the live game should have cleared IsAttacking/BlockedBy by postcombat main")
	}
	if e.G.Obj(atk).Zone != state.ZBattlefield || e.G.Obj(blk).Zone != state.ZBattlefield {
		t.Fatal("setup sanity: both creatures should have survived combat")
	}

	newEvents := e.L.Events[firstNewEvent:]
	if len(newEvents) < 6 {
		t.Fatalf("combat produced only %d events, too few for a meaningful truncation/perturbation control", len(newEvents))
	}

	full := before.Clone()
	for _, ev := range newEvents {
		events.Apply(full, ev)
	}
	if !reflect.DeepEqual(full, e.G) {
		t.Fatalf("log-only reconstruction diverges from the live game after a full combat round:\nreplayed = %+v\nlive     = %+v", full, e.G)
	}

	// Control 1: truncating the log must change the reconstruction.
	truncated := before.Clone()
	for _, ev := range newEvents[:len(newEvents)/2] {
		events.Apply(truncated, ev)
	}
	if reflect.DeepEqual(truncated, e.G) {
		t.Fatal("truncated-log control unexpectedly matched the live game -- the comparison above is not discriminating")
	}

	// Control 2: perturbing one real, mutating event's payload must also
	// change the reconstruction.
	perturbed := before.Clone()
	tampered := append([]events.Event(nil), newEvents...)
	found := false
	for i, ev := range tampered {
		if ev.Kind == events.Damage {
			tampered[i].Amount++
			found = true
			break
		}
	}
	if !found {
		t.Fatal("test setup: expected at least one Damage event to perturb")
	}
	for _, ev := range tampered {
		events.Apply(perturbed, ev)
	}
	if reflect.DeepEqual(perturbed, e.G) {
		t.Fatal("perturbed-log control unexpectedly matched the live game -- the comparison above is not discriminating")
	}
}

// ---------------------------------------------------------------------------
// Task m34: free-for-all attack targeting -- every attacker may choose its
// own defender (CR 506.2 / CR 903.14).

// TestSplitAttackAcrossTwoDefenders is the behaviour this task exists to
// add: in a four-seat game one combat declares an attacker against seat 2
// and a different attacker against seat 3, and BOTH deal damage. No
// pre-m34 build could express this -- askAttackers stamped the next living
// seat onto every option, so an attack could never point at two opponents
// at once, let alone land on both.
func TestSplitAttackAcrossTwoDefenders(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 4)
	atk2 := onBoardReady(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	atk3 := onBoardReady(t, e, 0, "Name:Wolf\nManaCost:2 G\nTypes:Creature Wolf\nPT:3/3\nOracle:x\n")
	driveToStep(t, e, 1, 0, state.StepDeclareAttackers)

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	// Six options: each creature offered against each of the three living
	// opponents (seats 1, 2, 3), defender-major.
	if len(d.Options) != 6 {
		t.Fatalf("attacker options = %d, want 6 (2 creatures x 3 defenders)", len(d.Options))
	}
	idx := func(id state.ObjID, def state.PlayerID) int {
		for _, o := range d.Options {
			if o.Obj == id && o.Player == def {
				return o.Index
			}
		}
		return -1
	}
	i1, i2 := idx(atk2, 2), idx(atk3, 3)
	if i1 < 0 || i2 < 0 {
		t.Fatalf("split-attack options missing: %d@2 = %d, %d@3 = %d", atk2, i1, atk3, i2)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i1, i2}}); err != nil {
		t.Fatalf("submit split attack: %v", err)
	}
	drainCombatPriority(t, e)
	// Neither defender has a creature to block with (mountain-only seats),
	// so combat cascades straight to damage: seat 2 took the 2/2, seat 3
	// took the 3/3, and seat 1 -- the next living seat, the ONLY pre-m34
	// legal defender -- was never attacked at all.
	if got := e.G.Players[2].Life; got != 18 {
		t.Fatalf("seat 2 life = %d, want 18 (the 2/2 landed on it)", got)
	}
	if got := e.G.Players[3].Life; got != 17 {
		t.Fatalf("seat 3 life = %d, want 17 (the 3/3 landed on it)", got)
	}
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("seat 1 life = %d, want 20 (it was not attacked)", got)
	}
}

// TestSameAttackerCannotAttackTwoDefenders is the Option-A legality guard
// (CR 506.2): the KAttackers option list offers every (attacker, defender)
// pair, so a client COULD pick one creature against two defenders. That
// intent must be rejected by the engine -- the legality of an attacker set
// is a property of the declaration, not of any single option -- and the
// pending decision must survive for a legal answer.
func TestSameAttackerCannotAttackTwoDefenders(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 4)
	atk := onBoardReady(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	driveToStep(t, e, 1, 0, state.StepDeclareAttackers)

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	idx := func(def state.PlayerID) int {
		for _, o := range d.Options {
			if o.Obj == atk && o.Player == def {
				return o.Index
			}
		}
		return -1
	}
	i1, i2 := idx(1), idx(3)
	if i1 < 0 || i2 < 0 {
		t.Fatalf("options missing: %d@1 = %d, %d@3 = %d", atk, i1, atk, i2)
	}
	before := len(e.L.Intents)
	// The same creature against two defenders: rejected, and the rejection
	// records nothing and consumes nothing.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i1, i2}}); err == nil {
		t.Fatal("the same attacker declared against two defenders was accepted")
	}
	if e.Pending() == nil {
		t.Fatal("the rejected intent consumed the pending decision")
	}
	if len(e.L.Intents) != before {
		t.Fatalf("rejected intent was recorded: %v", e.L.Intents[before:])
	}
	// A legal answer -- the creature at exactly one defender -- now works.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i1}}); err != nil {
		t.Fatalf("legal attack rejected after the guard: %v", err)
	}
	drainCombatPriority(t, e)
	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("seat 1 life = %d, want 18 (the lone swing landed)", got)
	}
}

// TestAttackersOfferEveryLivingOpponentOnce is the option-list gate for the
// Task-m34 option construction: the attack decision offers each legal
// attacker against every LIVING opponent, defender-major, in ascending seat
// order, and never against the active player or a departed seat. In a
// two-player game the same construction is exactly one option per attacker
// -- the M1 shape -- which is what keeps pre-m34 two-player behaviour
// byte-identical (the 2-seat acceptance chain head does not move under this
// task).
func TestAttackersOfferEveryLivingOpponentOnce(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 4)
	a := onBoardReady(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	b := onBoardReady(t, e, 0, "Name:Wolf\nManaCost:2 G\nTypes:Creature Wolf\nPT:3/3\nOracle:x\n")
	// Eliminate seat 3: a departed seat must never be offered as a defender.
	e.emit(events.Event{Kind: events.PlayerLost, Player: 3})
	driveToStep(t, e, 1, 0, state.StepDeclareAttackers)

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	// Seat 3 is dead: 2 living defenders (1, 2) x 2 attackers = 4 options,
	// defender-major.
	if len(d.Options) != 4 {
		t.Fatalf("attacker options = %d, want 4 (2 attackers x living seats 1,2)", len(d.Options))
	}
	for i, o := range d.Options {
		if o.Player == 0 {
			t.Fatalf("option %d offers attacking the active player's own seat", i)
		}
		if o.Player == 3 {
			t.Fatalf("option %d offers a departed seat as defender", i)
		}
	}
	wantObj := []state.ObjID{a, b, a, b}
	wantDef := []state.PlayerID{1, 1, 2, 2}
	for i, o := range d.Options {
		if o.Obj != wantObj[i] {
			t.Fatalf("option %d = attacker %d, want %d (defender-major order)", i, o.Obj, wantObj[i])
		}
		if o.Player != wantDef[i] {
			t.Fatalf("option %d = defender %d, want %d (ascending seat order)", i, o.Player, wantDef[i])
		}
	}
	// Two-player identity: the same construction is one option per attacker
	// at the sole legal defender.
	e2 := combatEngine(t)
	a2 := onBoardReady(t, e2, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e2.askAttackers()
	d2 := e2.Pending()
	if d2 == nil || len(d2.Options) != 1 || d2.Options[0].Obj != a2 || d2.Options[0].Player != 1 {
		t.Fatalf("two-player attackers decision = %+v, want one option (bear at seat 1)", d2)
	}
}

// TestAskBlockersGroupsOptionsByBlocker is the wire contract for the Group
// field: askBlockers tags every (blocker, attacker) pair naming one blocker
// with the SAME Group and pairs for different blockers with DIFFERENT Groups,
// and the value is never a display string. That is exactly what lets a
// rules-ignorant client enforce CR 509.1a (one creature blocks one attacker)
// from the wire alone, without learning what a blocker is.
func TestAskBlockersGroupsOptionsByBlocker(t *testing.T) {
	t.Parallel()
	const creature = "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n"

	e := combatEngine(t)
	first := onBoardReady(t, e, 0, creature)
	second := onBoardReady(t, e, 0, creature)
	blockerA := onBoard(t, e, 1, creature)
	blockerB := onBoard(t, e, 1, creature)
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{first, second}})
	e.G.Step = state.StepDeclareBlockers
	e.askBlockers()

	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}

	group := map[state.ObjID]string{}
	counts := map[state.ObjID]int{}
	for _, o := range d.Options {
		if o.Kind != "block" {
			t.Fatalf("unexpected non-block option in a blockers decision: %+v", o)
		}
		if o.Group == "" {
			t.Fatalf("block option for blocker %d has an empty Group", o.Obj)
		}
		if o.Group == o.Label {
			t.Fatalf("block option Group %q is a display string (its label)", o.Group)
		}
		group[o.Obj] = o.Group
		counts[o.Obj]++
	}

	// Every blocker gets one option per attacker, all sharing one Group.
	for _, b := range []state.ObjID{blockerA, blockerB} {
		if counts[b] != 2 {
			t.Fatalf("blocker %d has %d block options, want one per attacker (2)", b, counts[b])
		}
	}

	// Different blockers never share a Group, and a Group never leaks onto a
	// block option for a different blocker.
	if group[blockerA] == group[blockerB] {
		t.Fatalf("different blockers share a Group %q", group[blockerA])
	}
	for _, o := range d.Options {
		if o.Obj == blockerA && o.Group != group[blockerA] {
			t.Fatalf("blocker %d option carries group %q, want %q", o.Obj, o.Group, group[blockerA])
		}
		if o.Obj == blockerB && o.Group != group[blockerB] {
			t.Fatalf("blocker %d option carries group %q, want %q", o.Obj, o.Group, group[blockerB])
		}
		if o.Group == group[blockerA] && o.Obj != blockerA {
			t.Fatalf("group %q carried by a block option for a different blocker %d", group[blockerA], o.Obj)
		}
		if o.Group == group[blockerB] && o.Obj != blockerB {
			t.Fatalf("group %q carried by a block option for a different blocker %d", group[blockerB], o.Obj)
		}
	}
}
