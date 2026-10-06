package rules

// The IsPresent$ count sites that used to scan only the battlefield and ignore
// the PresentZone$ beside them: the MayPlay$ static family, UntapOtherPlayer,
// the three combat-damage statics (AssignCombatDamageAsUnblocked,
// CombatDamageToughness, CombatDamageNegatePower) and the life replacement
// gate. No corpus card prints a non-battlefield PresentZone$ on these modes
// today, so every test is SYNTHETIC: the counted object sits ONLY in the named
// zone, a battlefield copy of the same spec is proven not to satisfy the gate,
// and the gate is asserted in both directions.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

const presentZoneBauble = "Name:Bauble\nTypes:Artifact\nOracle:x\n"

// presentZoneProbe drives gate (which reports whether the carrier's gate
// holds) against a counted artifact: first with the artifact on the BATTLEFIELD
// only (the gate must stay shut -- it names zone), then with it in zone only
// (the gate must open), then with the zone emptied again.
func presentZoneProbe(t *testing.T, e *Engine, zone state.Zone, gate func() bool) {
	t.Helper()
	bf := onBoard(t, e, 0, presentZoneBauble)
	if n := e.presentZoneCount(state.ZBattlefield, "Artifact.YouCtrl", bf, 0); n != 1 {
		t.Fatalf("precondition: %d artifacts on the battlefield, want 1", n)
	}
	if n := e.presentZoneCount(zone, "Artifact.YouCtrl", bf, 0); n != 0 {
		t.Fatalf("precondition: %d artifacts in %v before placement, want 0", n, zone)
	}
	if gate() {
		t.Fatalf("gate held on a battlefield artifact although PresentZone$ names %v", zone)
	}
	inZoneCard(t, e, 0, zone, presentZoneBauble)
	if n := e.presentZoneCount(zone, "Artifact.YouCtrl", bf, 0); n != 1 {
		t.Fatalf("precondition: %d artifacts in %v after placement, want 1", n, zone)
	}
	if !gate() {
		t.Fatalf("gate stayed shut with a matching artifact in %v: PresentZone$ unread", zone)
	}
	e.G.SetZone(zone, 0, nil)
	e.staticEpoch, e.activeEpoch = -1, -1
	if gate() {
		t.Fatalf("gate held after the %v emptied", zone)
	}
}

func TestMayPlayPresentZoneHand(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	host := onBoard(t, e, 0, "Name:Host\nTypes:Creature Human\nPT:1/1\nOracle:x\n")
	target := inZoneCard(t, e, 0, state.ZGraveyard, "Name:Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	params := map[string]string{
		"MayPlay": "True", "Affected": "Card.YouOwn", "AffectedZone": "Graveyard",
		"IsPresent": "Artifact.YouCtrl", "PresentZone": "Hand",
	}
	presentZoneProbe(t, e, state.ZHand, func() bool {
		applies, grants, _, _, _, _ := e.mayPlayStatic(params, target, 0, host)
		return applies && grants
	})
	// An unrecognised zone fails closed rather than counting the battlefield.
	params["PresentZone"] = "Nowhere"
	onBoard(t, e, 0, presentZoneBauble)
	if applies, _, _, _, _, _ := e.mayPlayStatic(params, target, 0, host); applies {
		t.Fatal("MayPlay$ static applied under an unrecognised PresentZone$")
	}
}

func TestUntapPresentZoneGraveyard(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	host := onBoard(t, e, 0, "Name:Host\nTypes:Creature Human\nPT:1/1\n"+
		"S:Mode$ UntapOtherPlayer | ValidCard$ Card.Self | IsPresent$ Artifact.YouCtrl | PresentZone$ Graveyard\nOracle:x\n")
	st := e.G.Obj(host).Face().Statics[0]
	if st.Mode != "UntapOtherPlayer" || st.ParamStr(cards.PKPresentZone) != "Graveyard" {
		t.Fatalf("precondition: static is %q / zone %q", st.Mode, st.ParamStr(cards.PKPresentZone))
	}
	presentZoneProbe(t, e, state.ZGraveyard, func() bool { return e.staticPresentHolds(st, host) })
	// A PresentCompare$ a zero satisfies must not rescue an unknown zone.
	bad := cards.Static{Mode: "UntapOtherPlayer", Params: map[string]string{
		"IsPresent": "Artifact.YouCtrl", "PresentZone": "Nowhere", "PresentCompare": "EQ0"}}
	if e.staticPresentHolds(bad, host) {
		t.Fatal("unrecognised PresentZone$ counted 0 and satisfied EQ0 instead of failing closed")
	}
}

func TestAssignmentPresentZoneExile(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode  string
		holds func(e *Engine, id state.ObjID) bool
	}{
		{"AssignCombatDamageAsUnblocked", func(e *Engine, id state.ObjID) bool { m, _ := e.asUnblockedStaticMatches(id); return m }},
		{"CombatDamageToughness", func(e *Engine, id state.ObjID) bool { return e.combatDamageToughnessMatches(id) }},
		{"CombatDamageNegatePower", func(e *Engine, id state.ObjID) bool { return e.combatDamageNegatePowerMatches(id) }},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()
			e := layerEngine(t)
			host := onBoard(t, e, 0, "Name:Host\nTypes:Creature Human\nPT:1/1\n"+
				"S:Mode$ "+tc.mode+" | ValidCard$ Card.Self | IsPresent$ Artifact.YouCtrl | PresentZone$ Exile\nOracle:x\n")
			if st := e.G.Obj(host).Face().Statics[0]; st.Mode != tc.mode || st.ParamStr(cards.PKPresentZone) != "Exile" {
				t.Fatalf("precondition: static is %q / zone %q", st.Mode, st.ParamStr(cards.PKPresentZone))
			}
			presentZoneProbe(t, e, state.ZExile, func() bool { return tc.holds(e, host) })
		})
	}
}

func TestReplacementPresentZoneExile(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	host := onBoard(t, e, 0, "Name:Host\nTypes:Enchantment\n"+
		"R:Event$ GainLife | ActiveZones$ Battlefield | ValidPlayer$ You | ReplaceWith$ X | IsPresent$ Artifact.YouCtrl | PresentZone$ Exile | Description$ x\n"+
		"SVar:X:DB$ ReplaceEffect | VarName$ LifeGained | VarValue$ Y\nSVar:Y:ReplaceCount$LifeGained/Twice\nOracle:x\n")
	r := &e.G.Obj(host).Face().Repls[0]
	if r.ParamStr(cards.PKPresentZone) != "Exile" {
		t.Fatalf("precondition: replacement zone %q, want Exile", r.ParamStr(cards.PKPresentZone))
	}
	presentZoneProbe(t, e, state.ZExile, func() bool { return e.replacementCondition(host, r) })
}
