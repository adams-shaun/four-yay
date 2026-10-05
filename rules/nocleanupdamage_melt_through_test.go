package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestNoCleanupDamageMeltThroughCorpus resolves Melt Through's real DBAnimate
// SVar and its PerpetualEffect static body against a real battlefield target.
func TestNoCleanupDamageMeltThroughCorpus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	melt := mustCorpusCard(t, reg, "Melt Through")
	e := threeSeatEngine(t)
	target := onBoardCard(t, e, 0, card(t, "Name:Damage Target\nTypes:Creature Bear\nPT:5/5\nOracle:x\n"))
	control := onBoardCard(t, e, 0, card(t, "Name:Damage Control\nTypes:Creature Bear\nPT:5/5\nOracle:x\n"))
	spell := e.G.AddObject(melt, 0)
	spell.Zone = state.ZStack

	face := melt.Faces[0]
	animate := cards.ResolveSVar(face.SVars, "DBAnimate")
	if animate == nil {
		t.Fatal("precondition: corpus Melt Through DBAnimate SVar did not resolve")
	}
	if !strings.Contains(face.SVars["DBAnimate"], "staticAbilities$ PerpetualEffect") ||
		!strings.Contains(face.SVars["PerpetualEffect"], "Mode$ NoCleanupDamage") {
		t.Fatal("precondition: Melt Through's actual DBAnimate/PerpetualEffect path is absent")
	}
	if e.G.Obj(target) == nil || e.G.Obj(target).Zone != state.ZBattlefield || !e.IsCreature(target) ||
		e.G.Obj(control) == nil || e.G.Obj(control).Zone != state.ZBattlefield || !e.IsCreature(control) {
		t.Fatal("precondition: target and control must both be battlefield creatures")
	}
	e.G.Obj(target).Damage = 2
	e.G.Obj(control).Damage = 3
	if e.G.Obj(target).Damage <= 0 || e.G.Obj(control).Damage <= 0 || e.G.Obj(target).Damage == e.G.Obj(control).Damage {
		t.Fatal("precondition: both creatures need distinct positive marked damage")
	}

	effects.Resolve(e, &effects.Ctx{
		Source: spell.ID, Controller: 0, SVars: face.SVars,
		Targets: []state.Target{{Obj: target}}, TargetsOffered: true,
	}, animate)

	// The real perpetual grant must be live and match this target, but not the
	// otherwise comparable control creature.
	selected, live := false, false
	for _, ce := range e.active() {
		if ce.Restriction != effects.ModeNoCleanupDamage {
			continue
		}
		live = true
		if e.restrictionApplies(&ce, target) {
			selected = true
		}
		if e.restrictionApplies(&ce, control) {
			t.Fatal("precondition: Melt Through's grant also selects the control creature")
		}
	}
	if !live || !selected {
		t.Fatalf("precondition: real Melt Through grant is not live on target (live=%v selected=%v)", live, selected)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "Animate staticAbilities$ PerpetualEffect") {
			t.Fatalf("Melt Through Animate handler degraded instead of granting the static: %s", ev.Text)
		}
	}

	e.cleanupBody()
	if got := e.G.Obj(target).Damage; got != 2 {
		t.Fatalf("Melt Through target damage after cleanup = %d, want 2", got)
	}
	if got := e.G.Obj(control).Damage; got != 0 {
		t.Fatalf("control damage after cleanup = %d, want 0", got)
	}
}
