package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestAuroraShifterCloneGrantsNamedTriggerAndSVars(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Aurora Shifter", "Grizzly Bears")
	shifter := searchMoveByName(t, e, "Aurora Shifter", state.ZBattlefield)
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	o, target := e.G.Obj(shifter), e.G.Obj(bear)
	if o == nil || target == nil || o.Zone != state.ZBattlefield || target.Zone != state.ZBattlefield || o.Face().Name == target.Face().Name {
		t.Fatal("clone operands must be distinct battlefield permanents")
	}
	sa := cards.ResolveSVar(o.Face().SVars, "TrigCopy")
	if sa == nil || sa.Params["AddTriggers"] != "TrigDamage" || sa.Params["AddSVars"] != "TrigDamage,TrigEnergy" {
		t.Fatalf("Aurora Shifter grant precondition failed: %+v", sa)
	}
	effects.Resolve(e, &effects.Ctx{Source: shifter, Controller: o.Controller, Targets: []state.Target{{Obj: bear}}, SVars: o.Face().SVars, OfferedSA: sa}, sa)
	if e.G.Obj(shifter).Face().Name != "Grizzly Bears" {
		t.Fatal("clone did not replace face")
	}
	if _, ok := e.grantedSVarsFor(shifter)["TrigEnergy"]; !ok {
		t.Fatal("clone lost the granted energy SVar")
	}
	if _, ok := e.grantedSVarsFor(shifter)["TrigDamage"]; !ok {
		t.Fatal("clone lost the granted damage SVar")
	}
	found := false
	for _, ce := range e.continuous {
		if ce.Source == shifter && ce.AddTrigger != nil && ce.AddTrigger.Mode == "DamageDone" {
			found = true
		}
	}
	if !found {
		t.Fatal("clone did not grant the named damage trigger")
	}
	replayCheck(t, e, cfg)
}
