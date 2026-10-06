package rules

import (
	"regexp"
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
	hidden := map[string]state.Zone{"Library": state.ZLibrary, "Hand": state.ZHand}
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
		if got, known := presentZoneFromParam(zone); !known || got != want {
			t.Errorf("%s %s: PresentZone$ %s maps to (%v, %v), want (%v, true)", name, kind, zone, got, known, want)
		}
	}
	visit := func(name, kind string, params map[string]string) {
		if strings.TrimSpace(params["IsPresent"]) == "" {
			return
		}
		check(name, kind, params["PresentZone"])
	}
	for _, c := range reg.Cards {
		for _, face := range c.Faces {
			if face == nil {
				continue
			}
			for _, s := range face.Statics {
				visit(face.Name, "S:", s.Params)
			}
			for _, tr := range face.Triggers {
				visit(face.Name, "T:", tr.Params)
			}
			for _, r := range face.Repls {
				visit(face.Name, "R:", r.Params)
			}
			for _, ab := range face.Abilities {
				if ab != nil {
					visit(face.Name, "A:", ab.Params)
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
	// Floors measured 2026-10-05 with
	// grep -rhoE 'PresentZone\$ [A-Za-z,]+' .cards/cardsfolder | sort | uniq -c:
	// 10 Library lines, 24 Hand lines. A census below them read nothing.
	if counts["Library"] < 10 || counts["Hand"] < 24 {
		t.Fatalf("census precondition: Library=%d (want >= 10), Hand=%d (want >= 24) carriers", counts["Library"], counts["Hand"])
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
