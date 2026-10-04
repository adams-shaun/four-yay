package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestCassHandOfVengeanceReturnsOnlyTheWereAttachedAurasKernel drives Cass's
// own bodies on a board where a seat-1 creature wearing an Aura and an
// Equipment died: DBAttach re-attaches the were-attached Equipment to the
// trigger's target (answering its Optional$ True election yes), and
// DBChangeZone's `ChooseFromDefined$ AttachedTo TriggeredCardLKICopy.Aura`
// offers only the were-attached Aura (never the decoy), returns the picked
// Aura attached to the target and then poses DBAttach's yes/no election.
func TestCassHandOfVengeanceReturnsOnlyTheWereAttachedAurasKernel(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	setup := func() (*Engine, Config, state.ObjID, map[string]state.ObjID) {
		t.Helper()
		e, cfg := corpusEngineCfg(t, reg,
			[]*cards.Card{lookup(t, reg, "Cass, Hand of Vengeance"), lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Unholy Strength")},
			[]*cards.Card{lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Bonesplitter"), lookup(t, reg, "Unholy Strength")})
		cass := moveByName(t, e, 0, "Cass, Hand of Vengeance", state.ZBattlefield)
		ids := attachedRulesBoard(t, e)
		e.emit(events.Event{Kind: events.MoveZone, Obj: ids["bear"], From: state.ZBattlefield, To: state.ZGraveyard})
		e.checkStateBased()
		if e.G.Obj(ids["aura"]).Zone != state.ZGraveyard || e.G.Obj(ids["aura"]).LastBearer != ids["bear"] {
			t.Fatalf("precondition: swept Aura %+v, want in graveyard with LastBearer %d", e.G.Obj(ids["aura"]), ids["bear"])
		}
		if got := e.G.Obj(ids["decoy"]).LastBearer; got != 0 {
			t.Fatalf("precondition: decoy Aura LastBearer = %d, want 0", got)
		}
		if got := e.G.Obj(ids["equip"]); got.AttachedTo != 0 || got.LastBearer != ids["bear"] {
			t.Fatalf("precondition: Equipment AttachedTo %d LastBearer %d, want detached from %d", got.AttachedTo, got.LastBearer, ids["bear"])
		}
		return e, cfg, cass, ids
	}
	ctxFor := func(cass state.ObjID, ids map[string]state.ObjID) func() *effects.Ctx {
		return func() *effects.Ctx {
			return &effects.Ctx{Source: cass, Controller: 0, Remembered: []state.Target{{Obj: ids["bear"]}},
				Targets: []state.Target{{Obj: ids["dest"]}}}
		}
	}

	// Phase A -- DBAttach alone: answer its Optional$ True election yes.
	eA, cfgA, cassA, idsA := setup()
	dba := resolveSourceFaceSA(t, eA, cassA, "DBAttach")
	if dba.Params["Object"] != "AttachedTo TriggeredCardLKICopy.Equipment" || dba.Params["Defined"] != "Targeted" || dba.Params["Optional"] != "True" {
		t.Fatalf("precondition: DBAttach params %v", dba.Params)
	}
	kr4Resolve(eA, ctxFor(cassA, idsA), dba)
	da := eA.Pending()
	if da == nil || da.Kind != decision.KChoose || len(da.Options) < 2 || da.Options[0].Kind != "yes" {
		t.Fatalf("DBAttach's Optional$ True election was not posed as yes/no: %+v", da)
	}
	submitChoices(t, eA, da.Options[0].Index)
	if got := eA.G.Obj(idsA["equip"]).AttachedTo; got != idsA["dest"] {
		t.Fatalf("DBAttach: the were-attached Equipment AttachedTo = %d, want the target %d (Cass is %d)", got, idsA["dest"], cassA)
	}
	if got := eA.G.Obj(idsA["equip"]).LastBearer; got != 0 {
		t.Fatalf("DBAttach left LastBearer = %d, want 0 after the re-attach", got)
	}
	replayCheck(t, eA, cfgA)

	// Phase B/C -- DBChangeZone: the offer, then the answered pick.
	e, cfg, cass, ids := setup()
	sa := resolveSourceFaceSA(t, e, cass, "DBChangeZone")
	if cfd := sa.Params["ChooseFromDefined"]; cfd != "AttachedTo TriggeredCardLKICopy.Aura" {
		t.Fatalf("precondition: ChooseFromDefined = %q", cfd)
	}
	kr4Resolve(e, ctxFor(cass, ids), sa)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("the ChooseFromDefined pick was not posed: %+v", d)
	}
	if kr4Offers(d, ids["decoy"]) {
		t.Fatalf("the decoy Aura %d was offered by ChooseFromDefined", ids["decoy"])
	}
	submitChoices(t, e, kr4Option(t, d, ids["aura"]))
	if z := e.G.Obj(ids["aura"]).Zone; z != state.ZBattlefield {
		t.Fatalf("the returned Aura zone = %s, want battlefield", z)
	}
	if got := e.G.Obj(ids["aura"]).AttachedTo; got != ids["dest"] {
		t.Fatalf("returned Aura AttachedTo = %d, want the target %d", got, ids["dest"])
	}
	if z := e.G.Obj(ids["decoy"]).Zone; z != state.ZGraveyard {
		t.Fatalf("the decoy Aura left the graveyard (zone %s)", z)
	}
	da = e.Pending()
	if da == nil || da.Kind != decision.KChoose || len(da.Options) < 2 || da.Options[0].Kind != "yes" {
		t.Fatalf("DBAttach's Optional$ True election was not posed after the return: %+v", da)
	}
	replayCheck(t, e, cfg)
}
