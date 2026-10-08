package rules

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestLivingConundrumPresentZoneLibrary(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	if _, ok := reg.Lookup("Living Conundrum"); !ok {
		t.Fatal("corpus precondition: Living Conundrum is missing")
	}
	e, _ := searchEngine(t, reg, "Living Conundrum")
	colorsBoardPut(t, e, "Living Conundrum")
	var id state.ObjID
	for _, candidate := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(candidate); o != nil && o.Face() != nil && o.Face().Name == "Living Conundrum" {
			id = candidate
			break
		}
	}
	if id == 0 {
		t.Fatal("precondition: Living Conundrum did not reach the battlefield")
	}
	library := e.G.Zone(state.ZLibrary, 0)
	if len(library) == 0 {
		t.Fatal("precondition: controller library must be non-empty")
	}
	if got := e.Derived(id); got.Power != 2 || got.Toughness != 5 || e.HasKeyword(id, "Flying") || e.HasKeyword(id, "Vigilance") {
		t.Fatalf("with %d cards in library: got %d/%d, flying=%v vigilance=%v; want 2/5 without either keyword", len(library), got.Power, got.Toughness, e.HasKeyword(id, "Flying"), e.HasKeyword(id, "Vigilance"))
	}
	for _, cardID := range append([]state.ObjID(nil), library...) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: cardID, From: state.ZLibrary, To: state.ZGraveyard})
	}
	if n := len(e.G.Zone(state.ZLibrary, 0)); n != 0 {
		t.Fatalf("precondition: library has %d cards after emptying", n)
	}
	got := e.Derived(id)
	if got.Power != 10 || got.Toughness != 10 || !e.HasKeyword(id, "Flying") || !e.HasKeyword(id, "Vigilance") {
		t.Fatalf("with empty library: got %d/%d, flying=%v vigilance=%v; want 10/10 with both keywords", got.Power, got.Toughness, e.HasKeyword(id, "Flying"), e.HasKeyword(id, "Vigilance"))
	}
}

// TestCorpusPresentZoneHiddenZones is the census of every corpus carrier whose
// IsPresent$ gate counts a hidden zone (PresentZone$ Library or Hand): S:
// statics, T: triggers, R: replacements, A: abilities and SVar-defined lines
// (Temporal Aperture's StillTopCheck, Thoughtweft's Call's delayed trigger).
// Before the fix the static/delayed-trigger mapper knew no "Library" word, so
// an unknown zone failed closed to a count of 0 and every PresentCompare$ EQ0
// static (Living Conundrum) applied with a full library. Every hidden-zone
// word must now map, through the one shared mapper, to the zone it names.
func TestCorpusPresentZoneHiddenZones(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	hidden := map[string][]state.Zone{"Library": {state.ZLibrary}, "Hand": {state.ZHand}, "Battlefield,Graveyard": {state.ZBattlefield, state.ZGraveyard}}
	svarZone := regexp.MustCompile(`PresentZone\$ ([A-Za-z,]+)`)
	counts := map[string]int{}
	var carriers []string
	check := func(name, kind, zone string) {
		zone = strings.TrimSpace(zone)
		want, ok := hidden[zone]
		if !ok {
			return
		}
		counts[zone]++
		carriers = append(carriers, name+" "+kind+" "+zone)
		if got, known := presentZoneFromParam(zone); !known || !slices.Equal(got, want) {
			t.Errorf("%s %s: PresentZone$ %s maps to (%v, %v), want (%v, true)", name, kind, zone, got, known, want)
		}
	}
	// zoneAwareSites is the class invariant: the dispatch key (kind + Mode$/
	// Event$/API) of every IsPresent$ site that reads PresentZone$ through
	// the one shared mapper (presentZoneFromParam) or, for the R: events the
	// match path owns, through replacementConditionHolds. A carrier that
	// puts a non-battlefield PresentZone$ on a key not listed here is read by
	// a battlefield-only count and fails below -- route its site through
	// presentCountInZones / presentZoneFromParam, then list it.
	zoneAwareSites := map[string]bool{
		// trigger, ability: presentClauseHolds / abilityPresentHolds.
		"T:*": true, "A:*": true,
		// layer statics and the cast-side statics: continuousGateHolds,
		// staticTimingGate, costConditionHolds -> presentGate -> countStaticPresent.
		"S:Continuous": true, "S:CantAttack": true, "S:CantBlock": true, "S:CantBlockBy": true,
		"S:CanAttackDefender": true, "S:CastWithFlash": true, "S:MinMaxBlocker": true, "S:ReduceCost": true,
		// the former battlefield-only sites, now through presentCountInZones.
		"S:MayPlay": true, "S:UntapOtherPlayer": true, "S:AssignCombatDamageAsUnblocked": true,
		"S:CombatDamageToughness": true, "S:CombatDamageNegatePower": true,
		"R:GainLife": true, "R:LoseLife": true, "R:PayLife": true, "R:DamageDone": true,
		// the rest of the R: events: replacementConditionHolds.
		"R:Counter": true, "R:Draw": true, "R:Learn": true, "R:Planeswalk": true,
	}
	siteKey := func(kind, mode string, params map[string]string) string {
		if kind == "S:" && strings.TrimSpace(params["MayPlay"]) != "" {
			return "S:MayPlay"
		}
		if kind == "T:" || kind == "A:" {
			return kind + "*"
		}
		return kind + mode
	}
	nonBattlefield := 0
	visit := func(name, kind, mode string, params map[string]string) {
		if strings.TrimSpace(params["IsPresent"]) == "" {
			return
		}
		zone := strings.TrimSpace(params["PresentZone"])
		if zone != "" && zone != "Battlefield" {
			nonBattlefield++
			if key := siteKey(kind, mode, params); !zoneAwareSites[key] {
				t.Errorf("%s %s Mode/Event %q: IsPresent$ with PresentZone$ %s is dispatched by %q, which is not a zone-aware site", name, kind, mode, zone, key)
			}
		}
		check(name, kind, zone)
	}
	for _, c := range reg.AllCards() {
		for _, face := range c.Faces {
			if face == nil {
				continue
			}
			for _, s := range face.Statics {
				visit(face.Name, "S:", s.Mode, s.Params)
			}
			for _, tr := range face.Triggers {
				visit(face.Name, "T:", tr.Params["Mode"], tr.Params)
			}
			for _, r := range face.Repls {
				visit(face.Name, "R:", r.Event, r.Params)
			}
			for _, ab := range face.Abilities {
				if ab != nil {
					visit(face.Name, "A:", "", ab.Params)
				}
			}
			keys := make([]string, 0, len(face.SVars))
			for k := range face.SVars {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				v := face.SVars[k]
				if !strings.Contains(v, "IsPresent$") {
					continue
				}
				for _, m := range svarZone.FindAllStringSubmatch(v, -1) {
					check(face.Name, "SVar:"+k, m[1])
				}
			}
		}
	}
	// Floors measured 2026-10-06 with
	// /usr/bin/grep -rhoE 'PresentZone$ [A-Za-z,]+' .cards/cardsfolder | sort | uniq -c.
	if counts["Library"] < 10 || counts["Hand"] < 24 || counts["Battlefield,Graveyard"] < 8 {
		t.Fatalf("census precondition: Library=%d (want >= 10), Hand=%d (want >= 24), Battlefield,Graveyard=%d (want >= 8) carriers", counts["Library"], counts["Hand"], counts["Battlefield,Graveyard"])
	}
	// Floor measured 2026-10-06 (the 150-odd non-battlefield carriers above);
	// a census that stopped seeing them would pass the invariant vacuously.
	if nonBattlefield < 100 {
		t.Fatalf("census precondition: %d non-battlefield IsPresent$ carriers, want >= 100", nonBattlefield)
	}
	sort.Strings(carriers)
	for _, c := range carriers {
		t.Log(c)
	}
	t.Logf("hidden-zone PresentZone$ carriers: Library=%d Hand=%d", counts["Library"], counts["Hand"])
	found := false
	for _, c := range carriers {
		if c == "Living Conundrum S: Library" {
			found = true
		}
	}
	if !found {
		t.Fatal("census precondition: Living Conundrum's S: Library static is not represented")
	}
}
