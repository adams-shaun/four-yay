package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

const targetedNamesakeSpec = "TargetedCard.Self,Permanent.NotDefinedTargeted+sharesNameWith Targeted"

func addBattlefieldCard(t *testing.T, g *state.Game, name, types string) state.ObjID {
	t.Helper()
	o := g.AddObject(mkCard(t, "Name:"+name+"\nTypes:"+types+"\nOracle:x\n"), 0)
	o.Zone = state.ZBattlefield
	g.SetZone(state.ZBattlefield, 0, append(g.Zone(state.ZBattlefield, 0), o.ID))
	return o.ID
}

func TestDestroyAllTargetedCardSelf(t *testing.T) {
	g, ids := board(t)
	targetID := ids["myBear"]
	namesakeID := addBattlefieldCard(t, g, "Bear", "Creature Bear")
	otherID := ids["myFlier"]
	target, namesake, other := g.Obj(targetID), g.Obj(namesakeID), g.Obj(otherID)
	if target.Zone != state.ZBattlefield || namesake.Zone != state.ZBattlefield || other.Zone != state.ZBattlefield || targetID == namesakeID || targetID == otherID || namesakeID == otherID {
		t.Fatal("precondition: all three objects must be distinct permanents on the battlefield")
	}
	if target.Face().Name != namesake.Face().Name || target.Face().Name == other.Face().Name {
		t.Fatalf("precondition: names must be equal for target/namesake and differ for bystander: %q, %q, %q", target.Face().Name, namesake.Face().Name, other.Face().Name)
	}
	Resolve(&fakeHost{g: g}, &Ctx{Controller: 0, Targets: []state.Target{{Obj: targetID}}}, sa(t, "DB$ DestroyAll | ValidCards$ TargetedCard.Self,Permanent.NotDefinedTargeted+sharesNameWith Targeted"))
	if g.Obj(targetID).Zone != state.ZGraveyard || g.Obj(namesakeID).Zone != state.ZGraveyard || g.Obj(otherID).Zone != state.ZBattlefield {
		t.Fatalf("zones after DestroyAll: target=%s namesake=%s bystander=%s; want graveyard, graveyard, battlefield", g.Obj(targetID).Zone, g.Obj(namesakeID).Zone, g.Obj(otherID).Zone)
	}
}

func TestChangeZoneAllTargetedCardSelf(t *testing.T) {
	g, ids := board(t)
	targetID := ids["myBear"]
	namesakeID := addBattlefieldCard(t, g, "Bear", "Creature Bear")
	otherID := ids["myFlier"]
	target, namesake, other := g.Obj(targetID), g.Obj(namesakeID), g.Obj(otherID)
	if target.Zone != state.ZBattlefield || namesake.Zone != state.ZBattlefield || other.Zone != state.ZBattlefield || targetID == namesakeID || targetID == otherID || namesakeID == otherID {
		t.Fatal("precondition: all three objects must be distinct permanents on the battlefield")
	}
	if target.Face().Name != namesake.Face().Name || target.Face().Name == other.Face().Name {
		t.Fatalf("precondition: names must be equal for target/namesake and differ for bystander: %q, %q, %q", target.Face().Name, namesake.Face().Name, other.Face().Name)
	}
	Resolve(&fakeHost{g: g}, &Ctx{Controller: 0, Targets: []state.Target{{Obj: targetID}}}, sa(t, "DB$ ChangeZoneAll | Origin$ Battlefield | Destination$ Hand | ChangeType$ TargetedCard.Self,Permanent.NotDefinedTargeted+sharesNameWith Targeted"))
	if g.Obj(targetID).Zone != state.ZHand || g.Obj(namesakeID).Zone != state.ZHand || g.Obj(otherID).Zone != state.ZBattlefield {
		t.Fatalf("zones after ChangeZoneAll: target=%s namesake=%s bystander=%s; want hand, hand, battlefield", g.Obj(targetID).Zone, g.Obj(namesakeID).Zone, g.Obj(otherID).Zone)
	}
}

func TestSharesNameWithTargetedReferent(t *testing.T) {
	g, ids := board(t)
	targetID := ids["myBear"]
	namesakeID := addBattlefieldCard(t, g, "Bear", "Creature Bear")
	otherID := ids["myFlier"]
	target, namesake, other := g.Obj(targetID), g.Obj(namesakeID), g.Obj(otherID)
	if target.Zone != state.ZBattlefield || namesake.Zone != state.ZBattlefield || other.Zone != state.ZBattlefield || targetID == namesakeID || targetID == otherID || namesakeID == otherID {
		t.Fatal("precondition: all three objects must be distinct permanents on the battlefield")
	}
	if target.Face().Name != namesake.Face().Name || target.Face().Name == other.Face().Name {
		t.Fatalf("precondition: target/namesake names must equal and differ from bystander: %q, %q, %q", target.Face().Name, namesake.Face().Name, other.Face().Name)
	}
	sc := SpecContext{You: 0, ResolutionTargets: []state.Target{{Obj: targetID}}, Resolving: true}
	if got := UnknownPredicates("Permanent.sharesNameWith Targeted"); len(got) != 0 {
		t.Fatalf("UnknownPredicates = %v, want empty", got)
	}
	if !MatchesObjectCtx(g, "Permanent.sharesNameWith Targeted", namesake, sc) {
		t.Error("sharesNameWith Targeted must match the namesake")
	}
	if MatchesObjectCtx(g, "Permanent.sharesNameWith Targeted", other, sc) {
		t.Error("sharesNameWith Targeted must not match the unrelated permanent")
	}
	unbound := SpecContext{You: 0}
	for _, spec := range []string{"Permanent.sharesNameWith Targeted", "Permanent.!sharesNameWith Targeted"} {
		if MatchesObjectCtx(g, spec, namesake, unbound) {
			t.Errorf("unbound %s must fail closed", spec)
		}
	}
}
