package rules

// Reference: Magic: The Gathering Comprehensive Rules, 2026-08-07.
// CR 506.4: 4155-4161; 508.1c/d: 4256-4274; 508.2: 4302;
// 508.8: 4411-4412; 509.1a: 4422-4425; 509.2: 4478;
// 510.1c: 4562-4571; 510.3/4: 4589-4601; 511.2/3: 4608-4612.
// This revision has NO damage-assignment order: 510.1c explicitly permits
// dividing damage as the controller chooses, not lethal-before-next-blocker.
// Boundary fixtures use compiled corpus cards on real repo decks. No engine
// decision helper is used to compute a rules expectation.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Enter a named boundary, bypassing unrelated main-phase play. Game fields
// still change exclusively through events; pending is engine continuation data.
func crCombatAt(e *Engine, s state.Step) {
	e.pending = nil
	e.emit(events.Event{Kind: events.StepChange, Step: s})
	e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
	e.Advance()
}

// Narrow whole-corpus invariant: vanilla positive-toughness creatures whose
// ONLY printed rule is unconditional self MustAttack. With Silent Arbiter on
// the opposing battlefield and an ordinary companion, exactly the required
// creature must attack. Choosing only the companion satisfies the restriction
// but not the maximum feasible requirements. Count EXAMINED fixtures, not bugs.
func TestCR508CorpusRequirementsUnderAttackRestriction(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	checked := 0
	for _, c := range reg.AllCards() {
		if len(c.Faces) != 1 {
			continue
		}
		f := c.Faces[0]
		_, def, ok := strings.Cut(f.PT, "/")
		n, err := strconv.Atoi(def)
		if !ok || err != nil || n <= 0 || !f.IsCreature() || len(f.Abilities)+len(f.Triggers)+len(f.Repls)+len(f.Keywords) != 0 || len(f.Statics) != 1 {
			continue
		}
		st := f.Statics[0]
		if st.Mode != "MustAttack" || st.Params["ValidCreature"] != "Card.Self" {
			continue
		}
		plain := true
		for k := range st.Params { // Only a commutative fixture filter, no events.
			if k != "Mode" && k != "ValidCreature" && k != "Description" {
				plain = false
			}
		}
		if !plain {
			continue
		}
		checked++
		e := crResolutionEngine(t, []string{f.Name, "Memnite"}, []string{"Silent Arbiter"})
		must := crAbortMove(t, e, 0, f.Name, state.ZBattlefield)
		other := crAbortMove(t, e, 0, "Memnite", state.ZBattlefield)
		arbiter := crAbortMove(t, e, 1, "Silent Arbiter", state.ZBattlefield)
		ss := e.G.Obj(arbiter).Face().Statics
		if len(ss) != 2 || ss[0].Mode != "AttackRestrict" || ss[0].Params["MaxAttackers"] != "1" {
			t.Fatal("CR 508.1c/d Silent Arbiter seq 0: restriction fixture changed")
		}
		e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
		crCombatAt(e, state.StepDeclareAttackers)
		d := e.Pending()
		if d == nil || d.Kind != decision.KAttackers {
			t.Fatalf("CR 508.1d %s/Memnite/Silent Arbiter seq %d: missing declaration decision", f.Name, len(e.L.Events))
		}
		for _, opt := range d.Options {
			if opt.Obj != other || opt.Player != 1 {
				continue
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err == nil {
				t.Errorf("CR 508.1c/d %s/Memnite/Silent Arbiter seq %d: accepted only Memnite; required creature %d can attack alone, so this obeys 0 of the feasible 1 requirements", f.Name, d.Seq, must)
			}
			break
		}
	}
	if checked == 0 {
		t.Fatal("CR 508.1d corpus seq 0: no unconditional self-MustAttack vanilla creatures examined")
	}
	t.Logf("MEASURED CR 508.1d examined=%d compiled unconditional self-MustAttack creatures with Silent Arbiter", checked)
}

func TestCR508PriorityAfterAttackDeclaration(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Memnite"}, []string{"Memnite"})
	a := crAbortMove(t, e, 0, "Memnite", state.ZBattlefield)
	crAbortMove(t, e, 1, "Memnite", state.ZBattlefield)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	crCombatAt(e, state.StepDeclareAttackers)
	crAbortAnswer(t, e, "Memnite", crAbortOption(t, e, "Memnite", "attacker", a))
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 || e.G.Step != state.StepDeclareAttackers {
		t.Errorf("CR 508.2 Memnite seq %d: want active-player priority in declare attackers, got step=%s pending=%+v", len(e.L.Events), e.G.Step, d)
	}
}

func TestCR508EmptyAttackSkipsBlockersAndDamage(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Memnite"}, nil)
	crAbortMove(t, e, 0, "Memnite", state.ZBattlefield)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	crCombatAt(e, state.StepDeclareAttackers)
	start := len(e.L.Events)
	crAbortAnswer(t, e, "Memnite")
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.StepChange && (ev.Step == state.StepDeclareBlockers || ev.Step == state.StepCombatDamage) {
			t.Errorf("CR 508.8 Memnite seq %d: empty declaration entered %s; both steps must be skipped", ev.Seq, ev.Step)
		}
	}
}

func TestCR509OneBlockerCannotBlockTwoAttackers(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Memnite", "Memnite"}, []string{"Memnite"})
	a := crAbortMove(t, e, 0, "Memnite", state.ZBattlefield)
	b := crAbortMove(t, e, 0, "Memnite", state.ZBattlefield)
	blocker := crAbortMove(t, e, 1, "Memnite", state.ZBattlefield)
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{a, b}})
	crCombatAt(e, state.StepDeclareBlockers)
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatal("CR 509.1a Memnite seq 0: missing block declaration")
	}
	var choices []int
	for _, opt := range d.Options {
		if opt.Obj == blocker && (opt.Attacker == a || opt.Attacker == b) {
			choices = append(choices, opt.Index)
		}
	}
	if len(choices) != 2 {
		t.Fatalf("CR 509.1a Memnite seq %d: want two alternative block options, got %v", d.Seq, choices)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err == nil {
		t.Errorf("CR 509.1a Memnite seq %d: accepted ordinary blocker %d blocking both %d and %d", d.Seq, blocker, a, b)
	}
}

func TestCR509PriorityAfterBlockDeclaration(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Memnite"}, []string{"Memnite"})
	a := crAbortMove(t, e, 0, "Memnite", state.ZBattlefield)
	b := crAbortMove(t, e, 1, "Memnite", state.ZBattlefield)
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{a}})
	crCombatAt(e, state.StepDeclareBlockers)
	crAbortAnswer(t, e, "Memnite", crAbortOption(t, e, "Memnite", "block", b))
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 || e.G.Step != state.StepDeclareBlockers || e.G.Obj(a).Zone != state.ZBattlefield || e.G.Obj(b).Zone != state.ZBattlefield {
		t.Errorf("CR 509.2 Memnite seq %d: want priority before damage with both creatures alive; step=%s zones=%s/%s", len(e.L.Events), e.G.Step, e.G.Obj(a).Zone, e.G.Obj(b).Zone)
	}
}

func TestCR510ControllerChoosesMultiBlockDamageDivision(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Centaur Courser"}, []string{"Memnite", "Ornithopter"})
	a := crAbortMove(t, e, 0, "Centaur Courser", state.ZBattlefield)
	b := crAbortMove(t, e, 1, "Memnite", state.ZBattlefield)
	c := crAbortMove(t, e, 1, "Ornithopter", state.ZBattlefield)
	if e.G.Obj(a).Face().PT != "3/3" || e.G.Obj(b).Face().PT != "1/1" || e.G.Obj(c).Face().PT != "0/2" {
		t.Fatal("CR 510.1c Centaur Courser/Memnite/Ornithopter seq 0: printed P/T fixture changed")
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{a}})
	e.emit(events.Event{Kind: events.DeclareBlockers, Pairs: [][2]state.ObjID{{a, b}, {a, c}}})
	start := len(e.L.Events)
	crCombatAt(e, state.StepCombatDamage)
	// 3/0, 2/1, 1/2 and 0/3 are all legal. No policy was installed and
	// the controller has not answered any damage division decision.
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Damage && (ev.Obj == b || ev.Obj == c) {
			t.Errorf("CR 510.1c Centaur Courser/Memnite/Ornithopter seq %d: dealt %d to %d before controller chose a division", ev.Seq, ev.Amount, ev.Obj)
		}
	}
	d := e.Pending()
	if d == nil || d.Player != 0 || d.Kind == decision.KPriority {
		t.Errorf("CR 510.1c Centaur Courser seq %d: no controller damage-division ask; pending=%+v", len(e.L.Events), d)
	}
}

func TestCR510PriorityBetweenDoubleStrikeDamageSteps(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Boros Swiftblade"}, nil)
	a := crAbortMove(t, e, 0, "Boros Swiftblade", state.ZBattlefield)
	if e.G.Obj(a).Face().PT != "1/2" || !e.HasKeyword(a, "Double Strike") {
		t.Fatal("CR 510.4 Boros Swiftblade seq 0: double-strike fixture changed")
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{a}})
	before := e.G.Players[1].Life
	crCombatAt(e, state.StepCombatDamage)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 || e.G.Step != state.StepCombatDamage || e.G.Players[1].Life != before-1 {
		t.Errorf("CR 510.3/4 Boros Swiftblade seq %d: want priority after only first 1 damage, got life %d -> %d step=%s", len(e.L.Events), before, e.G.Players[1].Life, e.G.Step)
	}
}

func TestCR506ReturnedBlockerIsRemovedFromCombat(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Memnite"}, []string{"Memnite", "Ghostly Flicker"})
	a := crAbortMove(t, e, 0, "Memnite", state.ZBattlefield)
	b := crAbortMove(t, e, 1, "Memnite", state.ZBattlefield)
	land := crAbortMove(t, e, 1, "Plains", state.ZBattlefield)
	blink := crAbortMove(t, e, 1, "Ghostly Flicker", state.ZHand)
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{a}})
	e.emit(events.Event{Kind: events.DeclareBlockers, Pairs: [][2]state.ObjID{{a, b}}})
	f := e.G.Obj(blink).Face()
	sa := f.SpellAbility()
	if sa == nil || sa.API != "ChangeZone" || sa.Params["Destination"] != "Exile" || sa.Params["RememberChanged"] != "True" || sa.Params["TargetMin"] != "2" || sa.Sub == nil || sa.Sub.Params["Destination"] != "Battlefield" {
		t.Fatal("CR 506.4 Ghostly Flicker seq 0: real two-target exile/return fixture changed")
	}
	start := len(e.L.Events)
	e.resolveAbility(blink, 1, []state.Target{{Obj: b}, {Obj: land}}, sa, f.SVars)
	departed, returned := false, false
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.MoveZone && ev.Obj == b {
			departed = departed || ev.To == state.ZExile
			returned = returned || (departed && ev.To == state.ZBattlefield)
		}
	}
	if !departed || !returned || e.G.Obj(b).Zone != state.ZBattlefield {
		t.Fatalf("CR 506.4 Ghostly Flicker/Memnite seq %d: blocker did not demonstrably leave and return", start)
	}
	before := e.G.Players[1].Life
	e.dealCombatDamage()
	if e.G.Obj(a).Damage != 0 || e.G.Obj(b).Damage != 0 || e.G.Players[1].Life != before {
		t.Errorf("CR 506.4/509.1h/510.1c-d Ghostly Flicker/Memnite seq %d: returned object exchanged damage (%d/%d), life=%d want %d; attacker stays blocked but neither creature assigns damage", start, e.G.Obj(a).Damage, e.G.Obj(b).Damage, e.G.Players[1].Life, before)
	}
}

func TestCR511AttackerPersistsThroughEndCombatStep(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Memnite"}, nil)
	a := crAbortMove(t, e, 0, "Memnite", state.ZBattlefield)
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{a}})
	e.pending = nil
	e.setStep(state.StepEndCombat)
	e.Advance()
	if !e.G.Obj(a).IsAttacking {
		t.Errorf("CR 511.3 Memnite seq %d: stopped attacking on entry to end-of-combat priority, rather than when the step ends", len(e.L.Events))
	}
}

func TestCR511UntilEndCombatPumpExpiresBeforeMain(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	e := crResolutionEngine(t, []string{"Murk Dwellers"}, nil)
	a := crAbortMove(t, e, 0, "Murk Dwellers", state.ZBattlefield)
	f := e.G.Obj(a).Face()
	if f.PT != "2/2" || len(f.Triggers) != 1 || f.Triggers[0].Effect == nil || f.Triggers[0].Effect.API != "Pump" || f.Triggers[0].Effect.Params["Duration"] != "UntilEndOfCombat" || f.Triggers[0].Effect.Params["NumAtt"] != "+2" {
		t.Fatal("CR 511.2 Murk Dwellers seq 0: real until-end-of-combat +2/+0 fixture changed")
	}
	// Isolate expiry, not the separate AttackerUnblocked trigger dispatcher.
	e.resolveAbility(a, 0, nil, f.Triggers[0].Effect, f.SVars)
	e.setStep(state.StepEndCombat)
	if e.Power(a) != 4 {
		t.Fatalf("CR 511.2 Murk Dwellers seq %d: pump must still apply DURING end of combat", len(e.L.Events))
	}
	e.advanceStep()
	if e.G.Step != state.StepMain2 || e.Power(a) != 2 {
		t.Errorf("CR 511.2 Murk Dwellers seq %d: step=%s power=%d; want postcombat main with printed power 2", len(e.L.Events), e.G.Step, e.Power(a))
	}
}

// TestCR509MinMaxBlockerCountRestriction is the CR-lane leaf for the CR
// 509.1a block-count restriction family (the block restriction printed by
// S:Mode$ MinMaxBlocker; CR 702.1/702.10's static-ability umbrella): "can't
// be blocked except by N or more creatures" is a restriction on the WHOLE
// announce-blockers declaration, not a per-pair one. Troll of Khazad-dûm's
// Min$ 3 refuses a two-creature declaration and admits the three-creature
// one. The engine's own option list is the source of the answer, and the
// rejected intent leaves the pending decision intact.
func TestCR509MinMaxBlockerCountRestriction(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	troll := searchCorpusCard(t, reg, "Troll of Khazad-dûm")
	bear := card(t, staticBearFixture)
	e, _ := restrictionGame(t, 7310,
		[][]*cards.Card{nil, nil},
		[][]*cards.Card{{troll}, {bear, bear, bear}})
	attacker := corpusBoardID(t, e, 0, "Troll of Khazad-dûm")
	d := minMaxBlockDecision(t, e, attacker)
	if d == nil || d.Kind != decision.KBlockers || len(d.Options) != 3 {
		t.Fatalf("CR 509.1a Troll of Khazad-dûm seq %d: want three block options, got %+v", len(e.L.Events), d)
	}
	two := []int{d.Options[0].Index, d.Options[1].Index}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: two}); err == nil {
		t.Errorf("CR 509.1a Troll of Khazad-dûm seq %d: accepted a two-creature block under Min$ 3", len(e.L.Events))
	}
	if e.Pending() != d {
		t.Errorf("CR 509.1a Troll of Khazad-dûm seq %d: the rejected declaration consumed the pending decision", len(e.L.Events))
	}
	three := []int{d.Options[0].Index, d.Options[1].Index, d.Options[2].Index}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: three}); err != nil {
		t.Errorf("CR 509.1a Troll of Khazad-dûm seq %d: refused the legal three-creature block: %v", len(e.L.Events), err)
	}
}
