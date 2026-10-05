package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestCruelAllianceTeamworkedBattlefieldSubTargetAnnouncedBeforePayment(t *testing.T) {
	t.Parallel()
	e, cfg, mine, theirs := cr601Board(t, 61014,
		map[string]state.Zone{"Cruel Alliance": state.ZHand, "Grizzly Bears": state.ZBattlefield},
		map[string]state.Zone{"Grizzly Bears": state.ZBattlefield, "Craw Wurm": state.ZBattlefield})
	spell, teamworkBear := mine["Cruel Alliance"], mine["Grizzly Bears"]
	rootTarget, linkedTarget := theirs["Grizzly Bears"], theirs["Craw Wurm"]
	for name, id := range map[string]state.ObjID{"spell": spell, "Teamwork creature": teamworkBear} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand && name == "spell" || o.Zone != state.ZBattlefield && name != "spell" {
			t.Fatalf("precondition: %s object %d is %+v", name, id, o)
		}
	}
	if e.G.Obj(rootTarget).Zone != state.ZBattlefield || e.G.Obj(linkedTarget).Zone != state.ZBattlefield {
		t.Fatal("precondition: root and linked target candidates must be on the battlefield")
	}
	if rootTarget == linkedTarget || e.G.Obj(rootTarget).Face().ManaValue() > 3 || e.G.Obj(linkedTarget).Face().ManaValue() <= 3 {
		t.Fatalf("precondition: root candidate must have mana value <=3 and differ from linked candidate: root=%+v linked=%+v", e.G.Obj(rootTarget), e.G.Obj(linkedTarget))
	}
	addMana(t, e, 0, "BBB")
	cr601Cast(t, e, spell, "teamworked")
	teamwork := e.Pending()
	if teamwork == nil || teamwork.Kind != decision.KChoose || teamwork.MinSum != 2 || !teamwork.AllowNone {
		t.Fatalf("precondition: Cruel Alliance Teamwork cost is payable/declinable: %+v", teamwork)
	}
	submitChoices(t, e, teamworkOption(t, teamwork, teamworkBear))

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "cast_sub" {
		kind, resume := decision.Kind("<nil>"), ""
		if d != nil {
			kind, resume = d.Kind, d.ResumeKind
		}
		t.Fatalf("pending kind=%s resume=%q, want cast_sub KTarget before payment", kind, resume)
	}
	sub := castSubAsk(t, e)
	if sub.Min != 1 || sub.Max != 1 {
		t.Fatalf("linked target bounds = %d..%d, want exactly one", sub.Min, sub.Max)
	}
	offered := map[state.ObjID]bool{}
	for _, option := range sub.Options {
		offered[option.Obj] = true
	}
	if !offered[linkedTarget] || !offered[rootTarget] {
		t.Fatalf("linked battlefield target options %+v omit the distinct creature candidates", sub.Options)
	}
	if got := e.G.Players[0].Pool.Total(); got != 3 {
		t.Fatalf("precondition: mana was paid before cast-time targets, pool=%d want 3", got)
	}
	answerCastSubObj(t, e, linkedTarget)
	so := e.G.Obj(spell)
	if so == nil || so.Zone != state.ZStack || so.CastFlags&state.FlagTeamworkPaid == 0 || !e.G.Obj(teamworkBear).Tapped {
		t.Fatalf("precondition: Teamwork-paid spell should be on stack and its creature tapped after announcement: spell=%+v creature=%+v", so, e.G.Obj(teamworkBear))
	}
	if len(so.SubTargets) != 1 || so.SubTargets[0].Obj != linkedTarget {
		t.Fatalf("chain targets %+v, want Craw Wurm %d", so.SubTargets, linkedTarget)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority || len(e.G.Stack) != 1 {
		t.Fatalf("after linked target announcement pending=%+v stack=%v, want priority and spell on stack", d, e.G.Stack)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("spell payment after target announcement left pool=%d, want 0", got)
	}
	cr601ResolveQuietly(t, e)
	if got := e.G.Obj(linkedTarget); got == nil || got.Zone != state.ZExile {
		t.Fatalf("announced linked target %d is %+v, want exiled", linkedTarget, got)
	}
	if got := e.G.Obj(rootTarget); got == nil || got.Zone != state.ZBattlefield {
		t.Fatalf("different candidate %d is %+v, want untouched on battlefield", rootTarget, got)
	}
	replayCheck(t, e, cfg)
}

func TestChangeZoneBattlefieldSubTargetAnnouncementZonesAreNarrow(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		admitted   bool
		zone       state.Zone
	}{
		{name: "inferred battlefield", line: "DB$ ChangeZone | ValidTgts$ Creature | TargetMin$ Y | TargetMax$ Y | Origin$ Battlefield | Destination$ Exile", admitted: true, zone: state.ZBattlefield},
		{name: "explicit matching battlefield", line: "DB$ ChangeZone | ValidTgts$ Creature | TargetMin$ Y | TargetMax$ Y | TgtZone$ Battlefield | Origin$ Battlefield | Destination$ Exile", admitted: true, zone: state.ZBattlefield},
		{name: "unbounded battlefield shape remains unsupported", line: "DB$ ChangeZone | ValidTgts$ Creature | Origin$ Battlefield | Destination$ Exile", admitted: false, zone: state.ZBattlefield},
		{name: "explicit zone wins but mismatch rejects", line: "DB$ ChangeZone | ValidTgts$ Card | TgtZone$ Graveyard | Origin$ Battlefield | Destination$ Exile", admitted: false, zone: state.ZGraveyard},
		{name: "multi-zone explicit rejected", line: "DB$ ChangeZone | ValidTgts$ Card | TgtZone$ Battlefield,Graveyard | Origin$ Battlefield | Destination$ Exile", admitted: false},
		{name: "multi-origin inferred rejected", line: "DB$ ChangeZone | ValidTgts$ Creature | Origin$ Battlefield,Graveyard | Destination$ Exile", admitted: false, zone: state.ZBattlefield},
		{name: "unsupported exile origin", line: "DB$ ChangeZone | ValidTgts$ Card | Origin$ Exile | Destination$ Library", admitted: false, zone: state.ZBattlefield},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sa := kr0SA(t, tc.line)
			if got := castSubChangeZoneAnnounceable(sa); got != tc.admitted {
				t.Fatalf("announcement admitted=%v, want %v", got, tc.admitted)
			}
			zones := targetZones(sa)
			if tc.zone != 0 && (len(zones) != 1 || zones[0] != tc.zone) {
				t.Fatalf("targetZones=%v, want [%v]", zones, tc.zone)
			}
		})
	}
}
