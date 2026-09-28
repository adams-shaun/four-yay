package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The "&"-joined AddTrigger$ family: Forge joins several SVar trigger names
// with " & " (Mirror Shield's "TrigBlocks & TrigBecomeBlocked"). The printed
// static scanner once read the whole value as ONE SVar name, looked up a nil
// body and silently emitted NO grant, so the card gained nothing at all via
// its triggered half. These tests pin the split on five real corpus carriers.

// addTriggerGrantsOn returns the live AddTrigger grants whose Source is id
// (the static's own host object, per staticEffects' base.Source = id).
func addTriggerGrantsOn(e *Engine, id state.ObjID) []ContinuousEffect {
	var out []ContinuousEffect
	for _, ce := range e.active() {
		if ce.AddTrigger != nil && ce.Source == id {
			out = append(out, ce)
		}
	}
	return out
}

// TestSplitGrantNames pins the exported cards splitter directly (the grammar
// home the rules static scanner and the K:Class: grant path now share).

// TestMirrorShieldMultiNameAddTriggerRegisters pins the registration half: the
// real Mirror Shield's "TrigBlocks & TrigBecomeBlocked" must yield exactly TWO
// live grants (the primary and its Secondary$ mirror), both
// AttackerBlockedByCreature. A one-name fix yields one grant and fails.
func TestMirrorShieldMultiNameAddTriggerRegisters(t *testing.T) {
	t.Parallel()
	shield := mshCorpusCardPath(t, "Mirror Shield", "m/mirror_shield.txt")
	e := combatEngine(t)
	shieldID := onBoardCard(t, e, 0, shield)
	bear := onBoardReady(t, e, 0, "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	// Equip the shield so its Affected$ Card.EquippedBy has a recipient; a
	// grant with no recipient would still register but prove nothing about
	// the card being live.
	e.emit(events.Event{Kind: events.Attach, Obj: shieldID, IDs: []state.ObjID{bear}})

	// Preconditions: the shield is on the battlefield and its printed static
	// is the "&"-joined AddTrigger$ shape under test; the bear is the
	// equipped recipient.
	if o := e.G.Obj(shieldID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Mirror Shield battlefield precondition: %+v", o)
	}
	var rawTrigger string
	for _, st := range shield.Faces[0].Statics {
		if v := st.Params["AddTrigger"]; v != "" {
			rawTrigger = v
		}
	}
	if rawTrigger != "TrigBlocks & TrigBecomeBlocked" {
		t.Fatalf("Mirror Shield AddTrigger$ = %q, want the &-joined shape", rawTrigger)
	}
	if e.G.Obj(bear).AttachedTo == shieldID || e.G.Obj(shieldID).AttachedTo != bear {
		t.Fatalf("equip precondition: shield.AttachedTo=%d, bear=%d", e.G.Obj(shieldID).AttachedTo, bear)
	}

	grants := addTriggerGrantsOn(e, shieldID)
	if len(grants) != 2 {
		t.Fatalf("Mirror Shield live AddTrigger grants = %d, want 2 (TrigBlocks + the Secondary$ TrigBecomeBlocked); a one-name fix registers 1", len(grants))
	}
	secondary := 0
	for _, ce := range grants {
		if ce.AddTrigger.Mode != "AttackerBlockedByCreature" {
			t.Fatalf("Mirror Shield grant mode = %q, want AttackerBlockedByCreature", ce.AddTrigger.Mode)
		}
		if ce.AddTrigger.Params["Secondary"] != "" {
			secondary++
		}
	}
	if secondary != 1 {
		t.Fatalf("Mirror Shield Secondary$ grants = %d, want 1 (BOTH names must have registered)", secondary)
	}
}

// TestMirrorShieldGrantDestroysDeathtouchBlocker proves the registered grant
// actually FIRES end to end: a creature equipped with the real Mirror Shield is
// blocked by a deathtouch creature; the granted AttackerBlockedByCreature
// trigger destroys that blocker.
func TestMirrorShieldGrantDestroysDeathtouchBlocker(t *testing.T) {
	t.Parallel()
	shield := mshCorpusCardPath(t, "Mirror Shield", "m/mirror_shield.txt")
	e := combatEngine(t)
	shieldID := onBoardCard(t, e, 0, shield)
	bear := onBoardReady(t, e, 0, "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Test Assassin\nManaCost:1 B\nTypes:Creature Assassin\nPT:1/1\nK:Deathtouch\nOracle:x\n")
	e.emit(events.Event{Kind: events.Attach, Obj: shieldID, IDs: []state.ObjID{bear}})

	// Preconditions: the equipped bear prints no become-blocked trigger of its
	// own (so the observed destroy is the GRANT, not a printed line), and the
	// blocker is a live deathtouch creature on the battlefield (so the
	// ValidBlocker$ Creature.withDeathtouch filter can match and the
	// destruction is not vacuous).
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("bear battlefield precondition: %+v", o)
	}
	for _, tr := range e.G.Obj(bear).Face().Triggers {
		if tr.Mode == "AttackerBlocked" || tr.Mode == "AttackerBlockedByCreature" {
			t.Fatalf("bear prints %s; the scenario is not the granted path", tr.Mode)
		}
	}
	if o := e.G.Obj(blk); o == nil || o.Zone != state.ZBattlefield || !o.Face().HasKeyword("Deathtouch") {
		t.Fatalf("deathtouch blocker precondition: %+v", e.G.Obj(blk))
	}

	e.askAttackers()
	submitAttackersOnly(t, e, bear)
	drainCombatPriority(t, e)
	if d := e.Pending(); d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	submitBlockersOnly(t, e, blk)

	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.GrantTriggerPush }); n != 1 {
		t.Fatalf("Mirror Shield granted AttackerBlockedByCreature GrantTriggerPush events = %d, want 1", n)
	}
	e.resolveTop()
	if got := e.G.Obj(blk).Zone; got != state.ZGraveyard {
		t.Fatalf("deathtouch blocker zone after the granted destroy = %s, want graveyard", got)
	}
}

// TestVeteransArmamentsMultiNameAddTriggerFires proves the split is not
// specific to the AttackerBlockedByCreature family: the real Veteran's
// Armaments grants "HeroAttack & HeroBlock" (Attacks + Blocks), and the
// granted Attacks half must pump the equipped attacker.
func TestVeteransArmamentsMultiNameAddTriggerFires(t *testing.T) {
	t.Parallel()
	arm := mshCorpusCardPath(t, "Veteran's Armaments", "v/veterans_armaments.txt")
	e := combatEngine(t)
	armID := onBoardCard(t, e, 0, arm)
	bear := onBoardReady(t, e, 0, "Name:Test Soldier\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.Attach, Obj: armID, IDs: []state.ObjID{bear}})

	// Preconditions: the armament's AddTrigger$ is the &-joined shape, the
	// bearer is equipped, and it prints no Attacks/Blocks trigger of its own.
	var rawTrigger string
	for _, st := range arm.Faces[0].Statics {
		if v := st.Params["AddTrigger"]; v != "" {
			rawTrigger = v
		}
	}
	if rawTrigger != "HeroAttack & HeroBlock" {
		t.Fatalf("Veteran's Armaments AddTrigger$ = %q, want the &-joined shape", rawTrigger)
	}
	if e.G.Obj(armID).AttachedTo != bear {
		t.Fatalf("equip precondition: armament.AttachedTo=%d, want %d", e.G.Obj(armID).AttachedTo, bear)
	}
	for _, tr := range e.G.Obj(bear).Face().Triggers {
		if tr.Mode == "Attacks" || tr.Mode == "Blocks" {
			t.Fatalf("bear prints %s; the scenario is not the granted path", tr.Mode)
		}
	}
	if grants := addTriggerGrantsOn(e, armID); len(grants) != 2 {
		t.Fatalf("Veteran's Armaments live AddTrigger grants = %d, want 2 (HeroAttack + HeroBlock)", len(grants))
	}
	if e.Power(bear) != 2 || e.Toughness(bear) != 2 {
		t.Fatalf("bear before the attack = %d/%d, want 2/2", e.Power(bear), e.Toughness(bear))
	}

	e.askAttackers()
	submitAttackersOnly(t, e, bear)
	e.putTriggersOnStack()
	e.resolveTop()

	// ArmamentsX is Count$Valid Creature.attacking = 1 here, so the granted
	// Attacks trigger pumps the bearer +1/+1 until end of turn.
	if e.Power(bear) != 3 || e.Toughness(bear) != 3 {
		t.Fatalf("bear after the granted Attacks pump = %d/%d, want 3/3", e.Power(bear), e.Toughness(bear))
	}
}
