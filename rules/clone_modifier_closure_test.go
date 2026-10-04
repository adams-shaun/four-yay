package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func cloneHasType(types []string, want string) bool {
	for _, typ := range types {
		if typ == want {
			return true
		}
	}
	return false
}

func TestHallOfMirrorsCloneRemovesLegendary(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := searchEngine(t, reg, "Hall of Mirrors", "Isamaru, Hound of Konda", "Grizzly Bears")
	hall := searchMoveByName(t, e, "Hall of Mirrors", state.ZBattlefield)
	dog := searchMoveByName(t, e, "Isamaru, Hound of Konda", state.ZBattlefield)
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	h, d, b := e.G.Obj(hall), e.G.Obj(dog), e.G.Obj(bear)
	if h == nil || d == nil || b == nil || h.Zone != state.ZBattlefield || d.Zone != state.ZBattlefield || b.Zone != state.ZBattlefield || !cloneHasType(e.Derived(dog).Types, "Legendary") || cloneHasType(e.Derived(bear).Types, "Legendary") {
		t.Fatal("clone type precondition failed")
	}
	sa := cards.ResolveSVar(h.Face().SVars, "TrigCopyAll")
	if sa == nil || sa.Params["NonLegendary"] != "True" {
		t.Fatalf("Hall of Mirrors copy body missing: %+v", sa)
	}
	effects.Resolve(e, &effects.Ctx{Source: hall, Controller: 0, Targets: []state.Target{{Obj: dog}}, SVars: h.Face().SVars}, sa)
	if e.G.Obj(bear).Face().Name != d.Face().Name || cloneHasType(e.Derived(bear).Types, "Legendary") {
		t.Fatalf("nonlegendary clone = %v, types %v", e.G.Obj(bear).Face().Name, e.Derived(bear).Types)
	}
	replayCheck(t, e, cfg)
}

func TestCloneSubtypeModifiersStripBeforeAdding(t *testing.T) {
	t.Parallel()
	const mimic = "Name:Type Mimic\nManaCost:2\nTypes:Artifact\nA:AB$ Clone | Cost$ 1 | ValidTgts$ Creature | RemoveSubTypes$ True | SetCreatureTypes$ Shapeshifter\nOracle:x\n"
	const landCreature = "Name:Forest Ox\nManaCost:2\nTypes:Land Creature Forest Ox\nPT:2/2\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 218, mimic, landCreature)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	ox := moveSeeded(t, e, 0, landCreature, state.ZBattlefield)
	obj := e.G.Obj(id)
	if obj == nil || obj.Zone != state.ZBattlefield || e.G.Obj(ox).Zone != state.ZBattlefield || !cloneHasType(e.Derived(ox).Types, "Ox") || !cloneHasType(e.Derived(ox).Types, "Forest") || cloneHasType(e.Derived(id).Types, "Ox") {
		t.Fatal("type modifiers precondition failed")
	}
	sa := obj.Face().Abilities[0]
	if sa.Params["RemoveSubTypes"] != "True" || sa.Params["SetCreatureTypes"] != "Shapeshifter" {
		t.Fatal("fixture has no subtype riders")
	}
	effects.Resolve(e, &effects.Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: ox}}, SVars: obj.Face().SVars, OfferedSA: sa}, sa)
	ty := e.Derived(id).Types
	if e.G.Obj(id).Face().Name != "Forest Ox" || !cloneHasType(ty, "Creature") || !cloneHasType(ty, "Shapeshifter") || cloneHasType(ty, "Ox") || cloneHasType(ty, "Forest") {
		t.Fatalf("copied subtype list %v (face %s), want creature Shapeshifter without Ox or Forest", ty, e.G.Obj(id).Face().Name)
	}
	replayCheck(t, e, cfg)
}

func TestCloneAddAbilitiesGrantExpiresWithCopy(t *testing.T) {
	t.Parallel()
	const mimic = "Name:Gift Mimic\nManaCost:2\nTypes:Artifact\nA:AB$ Clone | Cost$ 1 | ValidTgts$ Creature | AddAbilities$ Gift | Duration$ UntilEndOfTurn\nSVar:Gift:AB$ Pump | Cost$ 1 | Defined$ Self | NumAtt$ 1 | NumDef$ 0\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 217, mimic, cloneOxSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	ox := moveSeeded(t, e, 0, cloneOxSrc, state.ZBattlefield)
	obj := e.G.Obj(id)
	if obj == nil || obj.Zone != state.ZBattlefield || e.G.Obj(ox).Zone != state.ZBattlefield || len(e.grantedAbilities(0, id)) != 0 {
		t.Fatal("grant precondition failed")
	}
	sa := obj.Face().Abilities[0]
	if sa.Params["AddAbilities"] != "Gift" || obj.Face().Name == e.G.Obj(ox).Face().Name {
		t.Fatal("distinct source and target or grant param missing")
	}
	effects.Resolve(e, &effects.Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: ox}}, SVars: obj.Face().SVars, OfferedSA: sa}, sa)
	if e.G.Obj(id).Face().Name != "Fixture Ox" || len(e.grantedAbilities(0, id)) != 1 || e.grantedAbilities(0, id)[0].SA.API != "Pump" {
		t.Fatalf("clone grant missing: face %v, grants %+v", e.G.Obj(id).Face().Name, e.grantedAbilities(0, id))
	}
	e.EndOfTurnCleanup()
	if e.G.Obj(id).Face().Name != "Gift Mimic" || len(e.grantedAbilities(0, id)) != 0 {
		t.Fatal("clone ability grant outlived copy")
	}
	replayCheck(t, e, cfg)
}
