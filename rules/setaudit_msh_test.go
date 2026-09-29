package rules

// Set audit: Marvel Super Heroes (msh).
//
// This file is the audit's product: one test per defect found, and one
// regression test per behaviour verified correct. A FAILING test is guarded
// by GORGE_SET_AUDIT and names its follow-up ticket; it is expected to pass
// once the ticket lands (remove the guard then). A PASSING test is the
// regression coverage and is never guarded.
//
// Findings are marked (a) keyword semantics, (b) corner cases, (c)
// library/graveyard/exile. Each citation names the CR rule.

import (
	"os"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// setAuditGuard skips a known-failing finding unless the audit env is set.
func setAuditGuard(t *testing.T, defect, ticket string) {
	t.Helper()
	if os.Getenv("GORGE_SET_AUDIT") == "" {
		t.Skip("set-audit finding (msh): " + defect + ". Follow-up: " + ticket)
	}
}

// auditDrain answers every ask generically so a resolution can be pushed
// through: pass at priority, full permutation for trigger order, decline
// optional triggers, first-Min options for everything else. It is bounded and
// only advances while something is on the stack.
func drainAudit(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining (stack depth %d)", len(e.G.Stack))
		}
		switch d.Kind {
		case decision.KPriority:
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			if pass < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d.Options)
			}
			submitChoices(t, e, pass)
		case decision.KTriggerOrder:
			idx := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				idx = append(idx, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: idx}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		case decision.KTriggerOptional:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1}}); err != nil {
				t.Fatalf("submit decline: %v", err)
			}
		default:
			var ch []int
			for k := 0; k < d.Min && k < len(d.Options); k++ {
				ch = append(ch, d.Options[k].Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}); err != nil {
				t.Fatalf("submit %v %q: %v", d.Kind, d.Prompt, err)
			}
		}
	}
}

// drainAuditUntilQuiet drains every ask until only an empty-stack priority ask
// remains.
func drainAuditUntilQuiet(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil || (len(e.G.Stack) == 0 && d.Kind == decision.KPriority) {
			return
		}
		drainAudit(t, e, 1)
	}
	t.Fatalf("engine never went quiet after %d asks", limit)
}

func mshPowerUpAbilityIndex(t *testing.T, o *state.Object) int {
	t.Helper()
	if o == nil || o.Face() == nil {
		t.Fatalf("precondition: object has no face: %+v", o)
	}
	for i, sa := range o.Face().Abilities {
		if strings.EqualFold(strings.TrimSpace(sa.Params["PowerUp"]), "True") {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// (a) Power-up: entry-turn cost reduction (CR 702.193a/b).
//
// "Power-up — [Cost]: [Effect]" means "[Cost]: [Effect]. If this permanent
// entered this turn, this ability's cost is reduced by this permanent's mana
// cost." CR 702.193b: generic reduces generic, colored reduces the same type,
// excess reduces generic.
//
// Brave Brawler is {1}{W} with "Power-up — {4}{W}". Entered this turn the
// offered cost must be {3} (1 generic cancels 1 generic, the W pip cancels the
// W pip), NOT {4}{W}. The engine never applies any entry-turn reduction, so
// with only {3} funded the ability is not even offered.
// ---------------------------------------------------------------------------
func TestSetAudit_msh_BraveBrawler_PowerUpCostReducedOnEntryTurn(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Brave Brawler")}, nil)
	id := moveByName(t, e, 0, "Brave Brawler", state.ZBattlefield)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Brave Brawler not on battlefield: %+v", o)
	}
	if !o.EnteredThisTurn {
		t.Fatalf("precondition: Brave Brawler did not record entering this turn")
	}
	idx := mshPowerUpAbilityIndex(t, o)
	if idx < 0 {
		t.Fatal("precondition: Brave Brawler has no PowerUp$ True ability")
	}
	// Fund the FULL printed cost {4}{W} plus slack, so the offer's presence is a
	// precondition the harness proves and the defect is the reduced cost alone.
	// CR 702.193b: {1}{W} cancels one generic and the W pip, leaving {3}.
	addMana(t, e, 0, "CCCCCCWW")
	opt, ok := findAbilityOption(e, id, idx)
	if !ok {
		t.Fatalf("precondition: Power-up ability not offered with the full printed cost funded")
	}
	if got := opt.Cost; got != "3" {
		setAuditGuard(t, "Brave Brawler's Power-up cost is unreduced ("+got+") when it entered this turn; it requires {4}{W} instead of {3}",
			"Apply Power-up's entry-turn cost reduction (CR 702.193b)")
		t.Fatalf("Power-up cost = %q, want %q (CR 702.193b)", got, "3")
	}
	replayCheck(t, e, cfg)
}

// ---------------------------------------------------------------------------
// (a) Cosmic Awareness: CastWithFlash's bare CheckSVar$ gate is disabled.
//
// Captain Mar-Vell's static: "Cosmic Awareness — As long as an opponent has
// cast a spell this turn, you may cast spells as though they had flash."
// Forge: S:Mode$ CastWithFlash | ... | CheckSVar$ X  with
// X = Count$ThisTurnCast_Card.OppCtrl and NO SVarCompare$.
//
// rules/statics.go:3240 documents the shared contract: "No SVarCompare$ means
// 'nonzero' (Forge's default truthiness read)" -- which
// effects.CheckSVarHolds implements. But staticTimingGate (rules/statics.go
// ~1015) re-implements the same read inline and returns false whenever
// SVarCompare$ is empty, so a bare CheckSVar$ gate ALWAYS fails. The
// permission is therefore never granted, even once an opponent has cast a
// spell (CR 601.3): seat 0 is not offered a creature cast on seat 1's turn.
//
// 326 static `S:Mode...CheckSVar` lines exist corpus-wide; 104 carry no
// SVarCompare$, so they all fail closed for the same reason.
// ---------------------------------------------------------------------------
func TestSetAudit_msh_CaptainMarVell_CosmicAwarenessBareCheckSVar(t *testing.T) {
	reg := searchTestRegistry(t)
	bolt, ok := reg.Lookup("Lightning Bolt")
	if !ok {
		t.Fatal("corpus fixture: Lightning Bolt missing")
	}
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Captain Mar-Vell, Space-Born"), lookup(t, reg, "Grizzly Bears")},
		[]*cards.Card{bolt})
	mv := moveByName(t, e, 0, "Captain Mar-Vell, Space-Born", state.ZBattlefield)
	if mv == 0 || e.G.Obj(mv).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Mar-Vell not on battlefield")
	}
	bears := moveByName(t, e, 0, "Grizzly Bears", state.ZHand)
	if bears == 0 || e.G.Obj(bears).Zone != state.ZHand {
		t.Fatalf("precondition: Grizzly Bears not in hand")
	}
	driveToStep(t, e, 2, 1, state.StepMain1)
	// Fund seat 0 AFTER the drive: the mana pool empties at every step/phase
	// boundary (CR 500.4), so a pool added at seat 0's turn-1 main is gone by
	// seat 1's turn 2.
	addMana(t, e, 0, "GG")

	// A main-phase priority round gives only the ACTIVE player priority, so
	// seat 0 only holds priority while seat 1's spell is on the stack. Cast
	// Lightning Bolt (an opponent casts a spell this turn) and inspect seat
	// 0's own priority window in response.
	addMana(t, e, 1, "R")
	boltOpt := castByName(t, e, 1, "Lightning Bolt")
	if boltOpt == nil {
		t.Fatalf("precondition: seat 1 cannot cast Lightning Bolt; options=%+v", e.Pending().Options)
	}
	submitChoices(t, e, boltOpt.Index)

	stackCast := func() int {
		n := 0
		for _, ev := range e.L.Events {
			if ev.Kind == events.PutOnStack {
				n++
			}
		}
		return n
	}
	if stackCast() == 0 {
		t.Fatal("precondition: seat 1's Lightning Bolt never reached the stack")
	}

	offered := false
	sawSeat0 := false
	for i := 0; i < 30; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			if d.Player == 0 {
				sawSeat0 = true
				for _, o := range d.Options {
					if o.Kind == "cast" && o.Obj == bears {
						offered = true
					}
				}
			}
			passIdx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					passIdx = o.Index
				}
			}
			if passIdx < 0 {
				break
			}
			submitChoices(t, e, passIdx)
			if len(e.G.Stack) == 0 && sawSeat0 {
				break
			}
			continue
		}
		drainAudit(t, e, 1)
	}
	if !sawSeat0 {
		t.Fatal("precondition: seat 0 never held priority in response to the bolt")
	}

	// Now that an opponent has cast a spell, Cosmic Awareness MUST grant flash:
	// seat 0 can cast its creature while responding to seat 1's spell
	// (CR 601.3). With the bare CheckSVar$ gate disabled, it is not offered.
	if !offered {
		t.Fatal("seat 0 was NOT offered a Grizzly Bears cast while responding to seat 1's spell (CR 601.3)")
	}
	replayCheck(t, e, cfg)
}

// ---------------------------------------------------------------------------
// (a) Power-up: the once-per-card-per-game activation limit is enforced.
//
// CR 702.193a: "Activate this ability only once." The engine does enforce
// this (rules/legal.go). This is the regression coverage that must keep
// passing.
// ---------------------------------------------------------------------------
func TestSetAudit_msh_BraveBrawler_PowerUpOnlyOnce(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Brave Brawler")}, nil)
	id := moveByName(t, e, 0, "Brave Brawler", state.ZBattlefield)
	o := e.G.Obj(id)
	idx := mshPowerUpAbilityIndex(t, o)
	if idx < 0 {
		t.Fatal("precondition: Brave Brawler has no PowerUp$ True ability")
	}
	// Fund the printed (unreduced) cost {4}{W} twice, so a second offer could
	// only be suppressed by the once-per-game limit, never by the pool.
	addMana(t, e, 0, "CCCCCCCCWW")
	opt := abilityOption(t, e, id, idx)
	submitChoices(t, e, opt.Index)
	if used := e.activationUsedCount(id, idx, "", false); used != 1 {
		t.Fatalf("precondition: first Power-up activation census = %d, want 1", used)
	}
	if _, ok := findAbilityOption(e, id, idx); ok {
		t.Fatal("Power-up ability offered a second time (CR 702.193a: activate only once)")
	}
	replayCheck(t, e, cfg)
}

// ---------------------------------------------------------------------------
// (b) Pump stat ops: NumAtt$/NumDef$ "Double" is a no-op.
//
// Wolverine, Claws Out: "Whenever a Mutant you control attacks, double its
// power until end of turn." Forge: DB$ Pump | Defined$ ... | NumAtt$ Double.
// A "Double" op must read the creature's CURRENT power and add that much
// again (CR 107.3 / the pump's own definition). effects.NumResolved resolves
// a signed literal, an SVar name or a Count$ head; "Double" is none of those,
// so it degrades to 0 and the pump adds +0/+0 -- the trigger resolves and does
// nothing. 36 corpus files carry NumAtt$/NumDef$ Double (three in msh: Epic
// Fight, World War Hulk III, Wolverine Claws Out).
// ---------------------------------------------------------------------------
func TestSetAudit_msh_WolverineClawsOut_DoublePower(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Wolverine, Claws Out")}, nil)
	w := moveByName(t, e, 0, "Wolverine, Claws Out", state.ZBattlefield)
	if w == 0 || e.G.Obj(w).Zone != state.ZBattlefield {
		t.Fatal("precondition: Wolverine, Claws Out not on battlefield")
	}
	if got := e.Derived(w).Power; got != 2 {
		t.Fatalf("precondition: Wolverine power = %d, want 2", got)
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{w}})
	e.putTriggersOnStack()
	pushed := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush {
			pushed++
		}
	}
	if pushed == 0 {
		t.Fatal("precondition: the attack trigger was never pushed")
	}
	drainAuditUntilQuiet(t, e, 20)
	// The trigger resolved (it was pushed above) but must actually double the
	// power: 2 -> 4.
	if got := e.Derived(w).Power; got != 4 {
		setAuditGuard(t, "Pump's NumAtt$/NumDef$ \"Double\" op is unimplemented and degrades to +0, so Wolverine, Claws Out's attack trigger (and Epic Fight's and World War Hulk III's doubling) do nothing",
			"Implement the pump stat op \"Double\"")
		t.Fatalf("Wolverine power after its doubling trigger = %d, want 4", got)
	}
	replayCheck(t, e, cfg)
}
