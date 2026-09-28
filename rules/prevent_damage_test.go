package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task api-prevent-damage: effects' PreventDamage primitive installs a real
// prevention shield ("prevent the next N damage this turn", CR 615) — the
// one-shot, rules-generated shape the printed prevention REPLACEMENT machinery
// (R:DamageDone Prevent$ True, DB$ ReplaceDamage bodies) cannot create. The
// pins below run the REAL compiled corpus SVar bodies: Angel of Salvation's
// divided five-point shield across two targets and Dawnfluke's single-target
// Amount$ 3, both ETB triggers whose placement ask the test answers with a
// real Intent, plus Vengeful Archon's PreventionSubAbility$ rider (the shield
// deals what it prevented, to the parent's target).

// answerTargetAsk submits the pending target decision selecting the given
// options (by their Obj / Player identity in the offered order).
func answerTargetAsk(t *testing.T, e *Engine, objs []state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want a KTarget placement ask", d)
	}
	var ch []int
	for _, want := range objs {
		found := false
		for _, o := range d.Options {
			if o.Obj == want {
				ch = append(ch, o.Index)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("option for obj %d not offered", want)
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}); err != nil {
		t.Fatalf("submit target intent: %v", err)
	}
}

// countShieldCEs counts active prevention shields in the continuous registry.
func countShieldCEs(e *Engine) int {
	n := 0
	for _, ce := range e.continuous {
		if strings.EqualFold(strings.TrimSpace(ce.ReplacementParams["PreventionShield"]), "True") {
			n++
		}
	}
	return n
}

// countPreventedNotes counts prevention Notes carrying exactly the given
// amount (applyReplaceDamageBody's per-application record).
func countPreventedNotes(e *Engine, amount int32) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Amount == amount &&
			strings.Contains(strings.ToLower(ev.Text), "prevented") {
			n++
		}
	}
	return n
}

// drainPendingTargetThenResolve answers the pending placement ask (when one is
// outstanding) and drains the trigger onto the stack and through resolution.
func drainPendingTargetThenResolve(t *testing.T, e *Engine, targets []state.ObjID) {
	t.Helper()
	e.putTriggersOnStack()
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		answerTargetAsk(t, e, targets)
	}
	for i := 0; i < 20 && len(e.G.Stack) > 0; i++ {
		e.resolveTop()
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack not drained: %d left", len(e.G.Stack))
	}
}

// TestAngelOfSalvationDividedPreventionShield pins the headline shape: the
// Angel's ETB triggers "prevent the next 5 damage ... to any number of
// targets, divided as you choose". Two recipients take the round-robin
// division stand-in (3 + 2), each shield depletes across events in its own
// pool, and a spent shield prevents nothing and is gone from the registry.
func TestAngelOfSalvationDividedPreventionShield(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := combatTriggerBoard(t, reg, []string{"Angel of Salvation"},
		[]string{"Name:Bear A\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n",
			"Name:Bear B\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"}, nil, nil)
	angel := findBattlefield(t, e, 0, "Angel of Salvation", 0)
	bearA := findBattlefield(t, e, 0, "Bear A", 0)
	bearB := findBattlefield(t, e, 0, "Bear B", 0)
	drainPendingTargetThenResolve(t, e, []state.ObjID{bearA, bearB})
	if got := countShieldCEs(e); got != 2 {
		t.Fatalf("shields = %d, want 2 (one per divided recipient)", got)
	}

	// Bear A's pool is 3, Bear B's 2 (round-robin over the answer order).
	damage := func(target state.ObjID, amount int32) {
		e.damaging = angel
		e.emit(events.Event{Kind: events.Damage, Obj: target, Amount: amount})
		e.damaging = 0
	}
	damage(bearA, 2)
	if got := e.G.Obj(bearA).Damage; got != 0 {
		t.Fatalf("Bear A took %d damage, want 0 (2 of its 3-point pool prevented)", got)
	}
	if got := countShieldCEs(e); got != 2 {
		t.Fatalf("shields after first hit = %d, want 2", got)
	}
	// The second hit spends Bear A's remaining 1 and lets the last point land.
	damage(bearA, 2)
	if got := e.G.Obj(bearA).Damage; got != 1 {
		t.Fatalf("Bear A damage = %d, want 1 (1 prevented, 1 landed)", got)
	}
	if got := countShieldCEs(e); got != 1 {
		t.Fatalf("shields after Bear A's pool spent = %d, want 1", got)
	}
	damage(bearB, 5)
	if got := e.G.Obj(bearB).Damage; got != 3 {
		t.Fatalf("Bear B damage = %d, want 3 (2 prevented, 3 landed)", got)
	}
	if got := countShieldCEs(e); got != 0 {
		t.Fatalf("shields after both pools spent = %d, want 0", got)
	}
	// The three applications' log record: one prevention Note each, in the
	// order they were applied (2, then 1, then 2).
	var got []int32
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(strings.ToLower(ev.Text), "prevented") {
			got = append(got, ev.Amount)
		}
	}
	want := []int32{2, 1, 2}
	if len(got) != len(want) {
		t.Fatalf("prevention Notes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("prevention Notes = %v, want %v", got, want)
		}
	}
}

// TestDawnflukeSingleTargetShield pins the simple shape: one recipient, the
// full Amount$ 3 pool, one partial application (3 of 4 prevented, 1 lands,
// shield spent and dropped).
func TestDawnflukeSingleTargetShield(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := combatTriggerBoard(t, reg, []string{"Dawnfluke"},
		[]string{"Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"}, nil, nil)
	bear := findBattlefield(t, e, 0, "Bear", 0)
	drainPendingTargetThenResolve(t, e, []state.ObjID{bear})
	if got := countShieldCEs(e); got != 1 {
		t.Fatalf("shields = %d, want 1", got)
	}
	source := findBattlefield(t, e, 0, "Dawnfluke", 0)
	e.damaging = source
	e.emit(events.Event{Kind: events.Damage, Obj: bear, Amount: 4})
	e.damaging = 0
	if got := e.G.Obj(bear).Damage; got != 1 {
		t.Fatalf("Bear damage = %d, want 1 (3 prevented, 1 landed)", got)
	}
	if got := countShieldCEs(e); got != 0 {
		t.Fatalf("shields = %d, want 0 (the 3-point pool is spent)", got)
	}
	// A further hit is untouched: no shield remains.
	e.damaging = source
	e.emit(events.Event{Kind: events.Damage, Obj: bear, Amount: 1})
	e.damaging = 0
	if got := e.G.Obj(bear).Damage; got != 2 {
		t.Fatalf("Bear damage = %d, want 2 after the shield was spent", got)
	}
	if got := countPreventedNotes(e, 3); got != 1 {
		t.Fatalf("prevention Notes of amount 3 = %d, want 1", got)
	}
}

// TestVengefulArchonShieldRiderDealsPrevented pins the PreventionSubAbility$
// rider: ArchonPrevention's shield on Defined$ You with Amount$ X (resolved
// through Count$xPaid here — the direct-resolution stand-in for the X-cost
// activation), and ArchonsVengeance's DB$ DealDamage |
// Defined$ ShieldEffectTarget | NumDmg$ PreventedDamage dealing what the
// application prevented to the PARENT's target.
func TestVengefulArchonShieldRiderDealsPrevented(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := newSeats(t, 2)
	archon := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Vengeful Archon"))
	sa := cards.ResolveSVar(e.G.Obj(archon).Face().SVars, "ArchonPrevention")
	if sa == nil {
		t.Fatalf("ArchonPrevention SVar unresolved")
	}
	ctx := &effects.Ctx{Source: archon, Controller: 0, X: 2,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}}
	effects.Resolve(e, ctx, sa)
	if got := countShieldCEs(e); got != 1 {
		t.Fatalf("shields = %d, want 1 (seat 0, pool 2)", got)
	}
	p0, p1 := e.G.Players[0].Life, e.G.Players[1].Life
	e.damaging = archon
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 5})
	e.damaging = 0
	if got := e.G.Players[0].Life; got != p0-3 {
		t.Fatalf("seat 0 life = %d, want %d (2 prevented, 3 landed)", got, p0-3)
	}
	// The rider dealt the prevented 2 to the parent's target (seat 1).
	if got := e.G.Players[1].Life; got != p1-2 {
		t.Fatalf("seat 1 life = %d, want %d (the retribution rider)", got, p1-2)
	}
	if got := countShieldCEs(e); got != 0 {
		t.Fatalf("shields = %d, want 0 (the 2-point pool is spent)", got)
	}
	if got := countPreventedNotes(e, 2); got != 1 {
		t.Fatalf("prevention Notes of amount 2 = %d, want 1", got)
	}
}

// TestPreventDamageShieldExpiresAtCleanup pins the shield's this-turn
// lifetime: an unspent shield is still active during the turn and gone after
// the cleanup step's EndOfTurnCleanup.
func TestPreventDamageShieldExpiresAtCleanup(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := combatTriggerBoard(t, reg, []string{"Dawnfluke"},
		[]string{"Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"}, nil, nil)
	bear := findBattlefield(t, e, 0, "Bear", 0)
	drainPendingTargetThenResolve(t, e, []state.ObjID{bear})
	if got := countShieldCEs(e); got != 1 {
		t.Fatalf("shields = %d, want 1", got)
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepCleanup})
	e.EndOfTurnCleanup()
	if got := countShieldCEs(e); got != 0 {
		t.Fatalf("shields after cleanup = %d, want 0 (UntilEOT)", got)
	}
}
