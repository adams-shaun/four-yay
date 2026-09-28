package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// grantTargetEffect stacks an inline Effect spell that grants a named static
// and asks for its target, returning the engine and its pending decision. The
// shape mirrors Whirler Rogue's real activated ability (an Effect SA naming an
// SVar whose body is a CantBlockBy static) so the payload test exercises the
// same resolution path: describeTargetEffect reads StaticAbilities$ back
// against the face's SVar table.
func grantTargetEffect(t *testing.T, staticName, staticBody string) (*Engine, *cards.SA) {
	t.Helper()
	e := newSeats(t, 2)
	c := card(t, "Name:Grant Bolt\nManaCost:2 U\nTypes:Sorcery\n"+
		"A:SP$ Effect | ValidTgts$ Creature | StaticAbilities$ "+staticName+"\n"+
		"SVar:"+staticName+":"+staticBody+"\n"+
		"Oracle:x\n")
	o := e.G.AddObject(c, 0)
	e.emit(events.Event{Kind: events.PutOnStack, Obj: o.ID, Player: 0,
		From: state.ZLibrary, To: state.ZStack, Text: "grant fidelity"})
	o = e.G.Obj(o.ID)
	if o == nil || o.Zone != state.ZStack {
		t.Fatalf("fixture precondition: spell is not on stack: %+v", o)
	}
	sa := o.Face().SpellAbility()
	if sa == nil || sa.API != "Effect" {
		t.Fatalf("fixture precondition: spell ability = %+v, want an Effect SA", sa)
	}
	// Precondition: the face SVar really carries the mode the payload should
	// republish -- otherwise a nil Statics would be indistinguishable from a
	// broken SVar lookup.
	if body := o.Face().SVars[staticName]; body == "" {
		t.Fatalf("fixture precondition: face SVar %q is empty", staticName)
	}
	// A legal creature target must exist or the ask is not posed at all.
	creature := card(t, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	co := e.G.AddObject(creature, 1)
	e.emit(events.Event{Kind: events.MoveZone, Obj: co.ID, From: state.ZLibrary, To: state.ZBattlefield})
	if got := len(e.G.Zone(state.ZBattlefield, 1)); got == 0 {
		t.Fatalf("fixture precondition: no battlefield creature to target (zone %d)", got)
	}
	e.askTarget(0, o.ID, sa)
	if e.Pending() == nil || e.Pending().Kind != decision.KTarget {
		t.Fatalf("fixture precondition: target decision = %+v", e.Pending())
	}
	return e, sa
}

// TestTargetEffectPublishesGrantedStaticModes is the fb-20260927T153930Z
// engine-side half: an Effect SA granting a CantBlockBy static must publish
// that Mode$ on its target decision, because the bot's polarity branch reads
// TargetEffect.Statics to know the grant is a boon. Before the fix the payload
// carried statics nowhere and the bot aimed the grant at an opponent.
func TestTargetEffectPublishesGrantedStaticModes(t *testing.T) {
	t.Parallel()
	e, _ := grantTargetEffect(t, "Unblockable", "Mode$ CantBlockBy | ValidAttacker$ Card.IsRemembered")
	d := e.Pending()
	if d.TargetEffect == nil {
		t.Fatal("target effect is absent")
	}
	if d.TargetEffect.API != "Effect" {
		t.Fatalf("target effect API = %q, want Effect", d.TargetEffect.API)
	}
	if got := d.TargetEffect.Statics; len(got) != 1 || got[0] != "CantBlockBy" {
		t.Fatalf("published statics = %v, want [CantBlockBy]", got)
	}
}

// TestTargetEffectPublishesCompoundGrantModes checks a compound/multi-name
// StaticAbilities$ list publishes every readable mode, in the body's own
// order, while a name with no SVar body is skipped rather than guessed.
func TestTargetEffectPublishesCompoundGrantModes(t *testing.T) {
	t.Parallel()
	e, _ := grantTargetEffect(t, "Unblockable",
		"Mode$ CantBlockBy | ValidAttacker$ Card.IsRemembered")
	if got := e.Pending().TargetEffect.Statics; len(got) != 1 || got[0] != "CantBlockBy" {
		t.Fatalf("published statics = %v, want [CantBlockBy]", got)
	}

	// A compound Mode$ body (two modes over one shared parameter map) must
	// publish both -- splitStaticModes is the one home for the split.
	e2 := newSeats(t, 2)
	c := card(t, "Name:Compound Grant\nManaCost:U\nTypes:Sorcery\n"+
		"A:SP$ Effect | ValidTgts$ Creature | StaticAbilities$ Both\n"+
		"SVar:Both:Mode$ CantBlockBy,CanAttackDefender | ValidAttacker$ Card.IsRemembered\n"+
		"Oracle:x\n")
	o := e2.G.AddObject(c, 0)
	e2.emit(events.Event{Kind: events.PutOnStack, Obj: o.ID, Player: 0,
		From: state.ZLibrary, To: state.ZStack, Text: "compound grant"})
	sa2 := e2.G.Obj(o.ID).Face().SpellAbility()
	creature := card(t, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	co := e2.G.AddObject(creature, 1)
	e2.emit(events.Event{Kind: events.MoveZone, Obj: co.ID, From: state.ZLibrary, To: state.ZBattlefield})
	e2.askTarget(0, o.ID, sa2)
	d2 := e2.Pending()
	if d2 == nil || d2.TargetEffect == nil {
		t.Fatalf("fixture precondition: no target effect: %+v", d2)
	}
	got := d2.TargetEffect.Statics
	if len(got) != 2 || got[0] != "CantBlockBy" || got[1] != "CanAttackDefender" {
		t.Fatalf("compound statics = %v, want [CantBlockBy CanAttackDefender]", got)
	}
}

// TestTargetEffectLeavesNonEffectStaticsEmpty guards the scope: only an
// Effect SA publishes Statics, so a removal-shaped ask keeps an empty list.
func TestTargetEffectLeavesNonEffectStaticsEmpty(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	e.askTarget(0, 0, &cards.SA{API: "Destroy", Params: map[string]string{
		"ValidTgts": "Player", "StaticAbilities": "Unblockable"}})
	d := e.Pending()
	if d == nil || d.TargetEffect == nil {
		t.Fatalf("fixture precondition: target decision = %+v", d)
	}
	if len(d.TargetEffect.Statics) != 0 {
		t.Fatalf("non-Effect API published statics: %v", d.TargetEffect.Statics)
	}
}
