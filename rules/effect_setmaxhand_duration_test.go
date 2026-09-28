package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// An Effect-delivered SetMaxHandSize grant uses the same absent-Duration
// default as every other Effect registration. In particular, normalization
// to "Permanent" must not make the state flag permanent: cleanup must remove
// the grant even while its creature source remains on the battlefield.
func TestEffectDeliveredSetMaxHandSizeAbsentDurationEndsAtCleanup(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	source := onBoard(t, e, 0, "Name:Effect source\nTypes:Creature\nPT:2/2\nOracle:x\n")
	c, diags := cards.ParseBytes("effect-duration.txt", []byte("Name:Effect\nTypes:Sorcery\nA:DB$ Effect | StaticAbilities$ HandSize | Affected$ You\nOracle:x\nSVar:HandSize:Mode$ Continuous | Affected$ You | SetMaxHandSize$ Unlimited\n"))
	if len(diags) != 0 {
		t.Fatalf("parse diagnostics: %v", diags)
	}
	c.Link()
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0, SVars: c.Faces[0].SVars}, c.Faces[0].Abilities[0])
	if len(e.continuous) != 1 {
		t.Fatalf("precondition: continuous registrations = %d, want 1", len(e.continuous))
	}
	grant := e.continuous[0]
	if grant.SetMaxHandSize != "Unlimited" || !grant.UntilEOT || grant.Permanent {
		t.Fatalf("precondition: grant = %+v, want UntilEOT and not Permanent", grant)
	}
	if got := e.maxHandSizeFor(0); got != unlimitedHandSize {
		t.Fatalf("precondition: hand size = %d, want unlimited", got)
	}
	if e.G.Obj(source).Zone != state.ZBattlefield {
		t.Fatalf("precondition: source zone = %v, want battlefield", e.G.Obj(source).Zone)
	}

	e.EndOfTurnCleanup()
	if got := e.maxHandSizeFor(0); got != maxHandSize {
		t.Fatalf("absent-Duration SetMaxHandSize survived cleanup: hand size = %d, want %d", got, maxHandSize)
	}
	if e.G.Obj(source).Zone != state.ZBattlefield {
		t.Fatal("precondition: cleanup moved the source off the battlefield")
	}
}
