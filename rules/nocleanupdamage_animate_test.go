package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
	"testing"
)

func TestNoCleanupDamageAnimateDelivery(t *testing.T) {
	e := threeSeatEngine(t)
	source := onBoard(t, e, 0, "Name:Test Melt\nTypes:Enchantment\nSVar:Grant:DB$ Animate | Defined$ Targeted | staticAbilities$ Keep | Duration$ UntilEOT\nSVar:Keep:Mode$ NoCleanupDamage | ValidCard$ Card.Self\nOracle:x\n")
	target := onBoard(t, e, 0, "Name:Target\nTypes:Creature Bear\nPT:5/5\nOracle:x\n")
	control := onBoard(t, e, 0, "Name:Control\nTypes:Creature Bear\nPT:5/5\nOracle:x\n")
	face := e.G.Obj(source).Face()
	sa := cards.ResolveSVar(face.SVars, "Grant")
	if sa == nil {
		t.Fatal("precondition: animate ability absent")
	}
	e.G.Obj(target).Damage = 2
	e.G.Obj(control).Damage = 2
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0, SVars: face.SVars, Targets: []state.Target{{Obj: target}}, TargetsOffered: true}, sa)
	live := false
	for _, ce := range e.active() {
		if ce.Restriction == "NoCleanupDamage" {
			live = true
		}
	}
	if !live {
		t.Fatal("Animate did not register NoCleanupDamage")
	}
	e.cleanupBody()
	if got := e.G.Obj(target).Damage; got != 2 {
		t.Fatalf("animated target damage = %d, want 2", got)
	}
	if got := e.G.Obj(control).Damage; got != 0 {
		t.Fatalf("control damage = %d, want 0", got)
	}
}
