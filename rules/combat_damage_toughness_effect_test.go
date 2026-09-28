package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestPlagonEffectDeliveredCombatDamageToughness(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	plagon := mustCorpusCard(t, reg, "Plagon, Lord of the Beach")
	targetedCard := card(t, toughnessBeast)
	untargetedCard := card(t, "Name:Untargeted Beast\nManaCost:3 G\nTypes:Creature Beast\nPT:6/3\nOracle:x\n")
	blockerCard := card(t, "Name:Damage Wall\nManaCost:1 W\nTypes:Creature Wall\nPT:0/8\nOracle:x\n")
	strongWall := card(t, "Name:Strong Wall\nManaCost:1 W\nTypes:Creature Wall\nPT:0/12\nOracle:x\n")
	e, cfg := restrictionGame(t, 9231,
		[][]*cards.Card{nil, nil},
		[][]*cards.Card{{plagon, targetedCard, untargetedCard}, {blockerCard, strongWall}})

	var plagonID, target, untargeted state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		switch e.G.Obj(id).Face().Name {
		case "Plagon, Lord of the Beach":
			plagonID = id
		case "Combat Beast":
			target = id
		case "Untargeted Beast":
			untargeted = id
		}
	}
	var blocker, strongBlocker state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 1) {
		if e.G.Obj(id).Face().Name == "Damage Wall" {
			blocker = id
		} else if e.G.Obj(id).Face().Name == "Strong Wall" {
			strongBlocker = id
		}
	}
	for label, id := range map[string]state.ObjID{"Plagon": plagonID, "target": target, "untargeted creature": untargeted, "blocker": blocker, "untargeted blocker": strongBlocker} {
		if o := e.G.Obj(id); id == 0 || o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %s is not on the battlefield (id %d, object %+v)", label, id, o)
		}
	}
	if p, toughness := e.Power(target), e.Toughness(target); p != 5 || toughness != 2 || p == toughness {
		t.Fatalf("precondition: target P/T = %d/%d, want distinct 5/2", p, toughness)
	}
	if p, toughness := e.Power(untargeted), e.Toughness(untargeted); p != 6 || toughness != 3 || p == toughness {
		t.Fatalf("precondition: untargeted P/T = %d/%d, want distinct 6/3", p, toughness)
	}
	if e.G.Obj(plagonID).Face().Name != "Plagon, Lord of the Beach" {
		t.Fatal("precondition: effect source is not Plagon")
	}

	addMana(t, e, 0, "WU")
	e.priorityRound()
	ability, ok := findAbilityOption(e, plagonID, 0)
	if !ok {
		t.Fatalf("Plagon's {W/U} activated ability is not offered: %+v", e.Pending())
	}
	submitChoices(t, e, ability.Index)
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		// Pay one of the hybrid symbols using the already-added white mana.
		submitChoices(t, e, 0)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Plagon target decision, got %+v", d)
	}
	foundTarget := false
	for _, option := range d.Options {
		if option.Obj == target {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{option.Index}}); err != nil {
				t.Fatalf("submit Plagon target: %v", err)
			}
			foundTarget = true
			break
		}
	}
	if !foundTarget {
		t.Fatalf("Plagon did not offer the intended creature as a target: %+v", d.Options)
	}
	passUntilStackEmpty(t, e, 60)

	registered := 0
	for _, ce := range e.active() {
		if ce.AssignmentStaticMode == "CombatDamageToughness" {
			registered++
			if ce.Source != plagonID || len(ce.Remembered) != 1 || ce.Remembered[0] != target {
				t.Fatalf("Plagon registration lost its source/remembered target: %+v", ce)
			}
		}
	}
	if registered != 1 || !e.combatDamageToughnessMatches(target) {
		t.Fatalf("Plagon assignment static registration/match = %d/%v, want 1/true", registered, e.combatDamageToughnessMatches(target))
	}
	if e.combatDamageToughnessMatches(untargeted) {
		t.Fatal("Plagon's remembered-target static matched the untargeted creature")
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Obj == plagonID && strings.Contains(ev.Text, "unimplemented CombatDamageToughness") {
			t.Fatalf("Plagon's readable assignment static was not registered: %q", ev.Text)
		}
	}

	driveToStep(t, e, e.G.Turn, 0, state.StepDeclareAttackers)
	submitAttackers(t, e, target, untargeted)
	// One blocker per attacker, named explicitly: a shared blocker list would
	// let submitBlockers pick the same attacker twice and route the pair into
	// the two-blocker damage-division ask instead of the plain assignment this
	// leaf reads.
	submitBlockerPairs(t, e, [2]state.ObjID{target, blocker}, [2]state.ObjID{untargeted, strongBlocker})
	if got := e.G.Obj(blocker).Damage; got != 2 {
		t.Fatalf("targeted attacker's damage = %d, want toughness 2 rather than power 5 (step %s, target attacking=%v, blocker=%+v, pending=%+v)", got, e.G.Step, e.G.Obj(target).IsAttacking, e.G.Obj(blocker), e.Pending())
	}
	if got := e.G.Obj(strongBlocker).Damage; got != 6 {
		t.Fatalf("untargeted attacker's damage = %d, want power 6 rather than toughness 3", got)
	}
	if z := e.G.Obj(plagonID).Zone; z != state.ZBattlefield {
		t.Fatalf("precondition: Plagon left the battlefield before cleanup (zone %v)", z)
	}

	e.EndOfTurnCleanup()
	if e.G.Obj(plagonID).Zone != state.ZBattlefield {
		t.Fatal("precondition: Plagon must remain on the battlefield through cleanup")
	}
	if got := e.combatDamageToughnessMatches(target); got {
		t.Fatal("Effect-delivered CombatDamageToughness remained active after end-of-turn cleanup")
	}
	for _, ce := range e.active() {
		if ce.AssignmentStaticMode == "CombatDamageToughness" {
			t.Fatalf("Effect-delivered assignment registration survived cleanup: %+v", ce)
		}
	}
	replayCheck(t, e, cfg)
}
