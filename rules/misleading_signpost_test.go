package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestMisleadingSignpostRetargetsAttackAfterAnsweredChoice is the end-to-end
// corpus regression for api:ChangeCombatants (task changecombat1): Misleading
// Signpost's real TrigChangeAttacker SA — "you may reselect which player ...
// target attacking creature is attacking" — must ask which DEFENDER the
// attacker is re-pointed at, and the answered choice must re-point
// state.Game's attack and unblock it.
//
// The trigger-matching half (the T: line's Phase$ Declare Attackers /
// ChangesZone gating) is a different primitive's job and is bypassed here the
// same way TestCommandeerChangesTargetAfterAnsweredChoice bypasses casting:
// the trigger wrapper is pushed and targeted directly, and everything from
// resolveTop's CR 608.2b target recheck through the Engine ask, the
// suspension, the "choice" resume and the state mutation is the real engine.
func TestMisleadingSignpostRetargetsAttackAfterAnsweredChoice(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	// The signpost enters BEFORE the declare-attackers step is staged, so its
	// ChangesZone/Phase$ trigger never queues from the placement itself; the
	// wrapper below is the trigger's resolution, not its matching.
	sp := onBoardCard(t, e, 0, choiceCorpusCard(t, "Misleading Signpost"))
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	gob := onBoardReady(t, e, 0, "Name:Grizzly Bears\nTypes:Creature\nPT:2/2\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Memnite\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{gob}})
	e.emit(events.Event{Kind: events.DeclareBlockers, Pairs: [][2]state.ObjID{{gob, blk}}})
	if got := e.G.Obj(gob); !got.IsAttacking || got.Attacking != 1 || len(got.BlockedBy) != 1 {
		t.Fatalf("setup attack = %+v attacking %d blocked by %v, want seat 1 blocked by %d",
			got.IsAttacking, got.Attacking, got.BlockedBy, blk)
	}
	// Push the signpost's own trigger (T: line index 0) and record the
	// placement ask's target: the attacking Bear.
	e.emit(events.Event{Kind: events.TriggerPush, Player: 0, Obj: sp, Amount: 0, Text: "triggered ability"})
	wid := e.G.Stack[len(e.G.Stack)-1]
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: wid, IDs: []state.ObjID{gob}})
	e.resolveTop()

	// CR 603.5 via resolveTop's askOptionalAtResolution: the OptionalDecider$
	// "you may" is chosen as the trigger RESOLVES. Answer yes, and the same
	// continued resolution poses the defender ask.
	opt := e.Pending()
	if opt == nil || opt.Kind != decision.KTriggerOptional || len(opt.Options) != 2 ||
		opt.Options[0].Kind != "yes" {
		t.Fatalf("optional-decider ask = %+v, want the trigger's yes/no", opt)
	}
	submitChoices(t, e, 0)

	// The defender ask: KChoose for the trigger's controller (seat 0), one
	// "player" option per legal defender — every living seat except the
	// attacker's controller, in AliveFrom seat order (seats 1 and 2).
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 {
		t.Fatalf("defender ask = %+v, want KChoose for seat 0", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("defender options = %+v, want the two non-controller seats", d.Options)
	}
	for i, o := range d.Options {
		if o.Kind != "player" || o.Player == state.PlayerID(i) {
			t.Fatalf("option %d = %+v, want player seat %d", i, o, i+1)
		}
	}
	// Answer with seat 2 — NOT the original defender.
	want := -1
	for _, o := range d.Options {
		if o.Player == 2 {
			want = o.Index
		}
	}
	if want < 0 {
		t.Fatalf("no seat-2 option offered: %+v", d.Options)
	}
	submitChoices(t, e, want)
	if got := e.G.Obj(gob); got.Attacking != 2 {
		t.Fatalf("Bear attacks %d, want the answered seat 2", got.Attacking)
	}
	if len(e.G.Obj(gob).BlockedBy) != 0 {
		t.Fatalf("BlockedBy = %v, want cleared (the re-pointed attack is unblocked)",
			e.G.Obj(gob).BlockedBy)
	}
	// The reselect is not a declaration: the re-pointed attack must not have
	// re-counted or re-triggered the declaration.
	if got := e.G.Obj(gob).AttacksThisTurn; got != 1 {
		t.Fatalf("AttacksThisTurn = %d, want 1 (a reselect is not a declaration)", got)
	}
}

// TestMisleadingSignpostKeepCurrentDefenderPreservesTheBlock pins that the
// ask lets the chooser KEEP the current defender (Forge offers every possible
// defender) AND that answering it is a true no-op: Forge's addToCombat acts
// only when the chosen defender does not already have the attacker, so the
// keep answer must not emit a CombatRetarget and must not disturb an
// existing block — clearing BlockedBy on a keep answer would silently
// unblock a blocked attack and send its damage to the player instead of the
// blocker (CR 506.3b / CR 509.1h shape).
func TestMisleadingSignpostKeepCurrentDefenderPreservesTheBlock(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	sp := onBoardCard(t, e, 0, choiceCorpusCard(t, "Misleading Signpost"))
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	gob := onBoardReady(t, e, 0, "Name:Grizzly Bears\nTypes:Creature\nPT:2/2\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Memnite\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{gob}})
	e.emit(events.Event{Kind: events.DeclareBlockers, Pairs: [][2]state.ObjID{{gob, blk}}})
	if got := e.G.Obj(gob); !got.IsAttacking || got.Attacking != 1 || len(got.BlockedBy) != 1 {
		t.Fatalf("setup attack = %+v attacking %d blocked by %v, want seat 1 blocked by %d",
			got.IsAttacking, got.Attacking, got.BlockedBy, blk)
	}
	e.emit(events.Event{Kind: events.TriggerPush, Player: 0, Obj: sp, Amount: 0, Text: "triggered ability"})
	wid := e.G.Stack[len(e.G.Stack)-1]
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: wid, IDs: []state.ObjID{gob}})
	e.resolveTop()
	opt := e.Pending()
	if opt == nil || opt.Kind != decision.KTriggerOptional || len(opt.Options) != 2 ||
		opt.Options[0].Kind != "yes" {
		t.Fatalf("optional-decider ask = %+v, want the trigger's yes/no", opt)
	}
	submitChoices(t, e, 0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("defender ask = %+v, want KChoose", d)
	}
	keep := -1
	for _, o := range d.Options {
		if o.Player == 1 {
			keep = o.Index
		}
	}
	if keep < 0 {
		t.Fatalf("the current defender (seat 1) was not offered: %+v", d.Options)
	}
	retargetsBefore := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.CombatRetarget {
			retargetsBefore++
		}
	}
	submitChoices(t, e, keep)
	if got := e.G.Obj(gob); got.Attacking != 1 || len(got.BlockedBy) != 1 || got.BlockedBy[0] != blk {
		t.Fatalf("keep answer: attacking %d blocked by %v, want attack on seat 1 with block %d intact",
			got.Attacking, got.BlockedBy, blk)
	}
	retargetsAfter := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.CombatRetarget {
			retargetsAfter++
		}
	}
	if retargetsAfter != retargetsBefore {
		t.Fatalf("keep answer emitted %d CombatRetarget events, want 0 (a true no-op)",
			retargetsAfter-retargetsBefore)
	}
}

// threeSeatEngine is combatEngine widened to three seats, so a reselect has
// somewhere to move the attack TO. Reuses layerEngine's own construction
// (mountain decks, no creatures) the same way combatEngine does.
func threeSeatEngine(t *testing.T) *Engine {
	t.Helper()
	e := New(Config{Seed: 716, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}})
	return e
}
