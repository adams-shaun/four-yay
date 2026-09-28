package effects

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// golgari_thug2: Forge's ChangeZoneEffect and ChangeZoneAllEffect compute the
// library position with a default of 0 (TOP) when LibraryPosition$ is ABSENT
// (ChangeZoneEffect.changeKnownOriginResolve, ChangeZoneAllEffect.java:102),
// so a card whose script never writes the parameter -- "put target creature
// card from your graveyard on top of your library" (Golgari Thug, Academy
// Ruins, Volrath's Stronghold, Unholy Grotto, Mystic Sanctuary, ...) -- must
// place its card ON TOP, not at the MoveZone bottom append. These tests pin
// the absent spelling on the object-target path, the ChangeZoneAll path, and
// a real corpus carrier of each.

// TestTargetedChangeZoneAbsentLibraryPositionIsTop drives a targeted
// Origin$ Graveyard | Destination$ Library ChangeZone with NO LibraryPosition$
// on a board with two library fillers and asserts the moved card is at
// library index 0.
func TestTargetedChangeZoneAbsentLibraryPositionIsTop(t *testing.T) {
	h := newHost(t, 2)
	fills := lzFills(t, h, 2)
	tgt := lzCreature(t, h, 0, state.ZGraveyard, "Vanished")
	if z := h.g.Obj(tgt).Zone; z != state.ZGraveyard {
		t.Fatalf("precondition: target zone = %v, want graveyard", z)
	}
	if lib := lzLibraryOrderIDs(h, 0); slices.Equal(lib, []state.ObjID{tgt}) {
		t.Fatalf("precondition: library = %v, want the two fillers only", lib)
	}
	sa := sa(t, "DB$ ChangeZone | ValidTgts$ Creature | Origin$ Graveyard | Destination$ Library")
	if sa.Params["LibraryPosition"] != "" {
		t.Fatalf("precondition: SA carries LibraryPosition$ %q, want absent", sa.Params["LibraryPosition"])
	}
	Resolve(h, &Ctx{Controller: 0, Source: tgt, TargetsOffered: true,
		Targets: []state.Target{{Obj: tgt}}}, sa)

	if z := h.g.Obj(tgt).Zone; z != state.ZLibrary {
		t.Fatalf("target zone = %v, want library (the move itself must still land)", z)
	}
	want := append([]state.ObjID{tgt}, fills...)
	if lib := lzLibraryOrderIDs(h, 0); !slices.Equal(lib, want) {
		t.Fatalf("library = %v, want %v (absent LibraryPosition$ = TOP, Forge's default)", lib, want)
	}
	for _, e := range h.log {
		if e.Kind == events.Note {
			t.Fatalf("unexpected Note %+v (the absent default must not be loud)", e)
		}
	}
}

// TestTargetedChangeZoneAbsentPositionIsTopOnRealCorpusCard drives the
// ACTUAL compiled SA for Academy Ruins' activated ability ("Put target
// artifact card from your graveyard on top of your library" -- no
// LibraryPosition$ in the script) through the object-target path and asserts
// the card lands on top.
func TestTargetedChangeZoneAbsentPositionIsTopOnRealCorpusCard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	ruinsCard, ok := reg.Lookup("Academy Ruins")
	if !ok {
		t.Fatal("corpus has no Academy Ruins")
	}
	var ab *cards.SA
	for _, a := range ruinsCard.Faces[0].Abilities {
		if a.API == "ChangeZone" {
			ab = a
			break
		}
	}
	if ab == nil {
		t.Fatal("Academy Ruins has no ChangeZone ability in the corpus")
	}
	if ab.Params["LibraryPosition"] != "" {
		t.Fatalf("precondition: Academy Ruins carries LibraryPosition$ %q, want absent", ab.Params["LibraryPosition"])
	}
	if ab.Params["ValidTgts"] != "Artifact.YouCtrl" || ab.Params["Origin"] != "Graveyard" ||
		ab.Params["Destination"] != "Library" {
		t.Fatalf("precondition: compiled params = %v, want ValidTgts$ Artifact.YouCtrl / Origin$ Graveyard / Destination$ Library", ab.Params)
	}

	h := newHost(t, 2)
	fills := lzFills(t, h, 2)
	relic := mkCard(t, "Name:Relic\nTypes:Artifact\nOracle:x\n")
	o := h.g.AddObject(relic, 0)
	o.Zone = state.ZGraveyard
	h.g.SetZone(state.ZGraveyard, 0, append(h.g.Zone(state.ZGraveyard, 0), o.ID))
	relicID := o.ID
	if z := h.g.Obj(relicID).Zone; z != state.ZGraveyard {
		t.Fatalf("precondition: Relic zone = %v, want graveyard", z)
	}

	source := lzAdd(t, h, 0, state.ZBattlefield, ruinsCard)
	Resolve(h, &Ctx{Controller: 0, Source: source, TargetsOffered: true,
		Targets: []state.Target{{Obj: relicID}}}, ab)

	if z := h.g.Obj(relicID).Zone; z != state.ZLibrary {
		t.Fatalf("Relic zone = %v, want library (the move itself must still land)", z)
	}
	want := append([]state.ObjID{relicID}, fills...)
	if lib := lzLibraryOrderIDs(h, 0); !slices.Equal(lib, want) {
		t.Fatalf("library = %v, want %v (Academy Ruins puts the artifact on TOP)", lib, want)
	}
}

// TestChangeZoneAllAbsentLibraryPositionIsTop drives effChangeZoneAll for
// Origin$ Graveyard | Destination$ Library with NO LibraryPosition$ and
// asserts the swept card lands on top of its library (Guiding Spirit's
// shape: "put that card on top of that player's library").
func TestChangeZoneAllAbsentLibraryPositionIsTop(t *testing.T) {
	h := newHost(t, 2)
	fills := lzFills(t, h, 2)
	tgt := lzCreature(t, h, 0, state.ZGraveyard, "Vanished")
	if z := h.g.Obj(tgt).Zone; z != state.ZGraveyard {
		t.Fatalf("precondition: target zone = %v, want graveyard", z)
	}
	if lib := lzLibraryOrderIDs(h, 0); slices.Equal(lib, []state.ObjID{tgt}) {
		t.Fatalf("precondition: library = %v, want the two fillers only", lib)
	}
	sa := sa(t, "SP$ ChangeZoneAll | Origin$ Graveyard | Destination$ Library | ChangeType$ Creature")
	if sa.Params["LibraryPosition"] != "" {
		t.Fatalf("precondition: SA carries LibraryPosition$ %q, want absent", sa.Params["LibraryPosition"])
	}
	Resolve(h, &Ctx{Controller: 0, Source: tgt}, sa)

	if z := h.g.Obj(tgt).Zone; z != state.ZLibrary {
		t.Fatalf("target zone = %v, want library (the sweep itself must still land)", z)
	}
	want := append([]state.ObjID{tgt}, fills...)
	if lib := lzLibraryOrderIDs(h, 0); !slices.Equal(lib, want) {
		t.Fatalf("library = %v, want %v (absent LibraryPosition$ = TOP, Forge's default)", lib, want)
	}
	for _, e := range h.log {
		if e.Kind == events.Note {
			t.Fatalf("unexpected Note %+v (the absent default must not be loud)", e)
		}
	}
}

// absentAffectedCount pins the measured census of the absent-spelling class
// at the current corpus pin. The predicate (measured with the same compiled-IR
// walk TestTargetedLibraryPositionCensus uses -- Abilities/Triggers/Repls
// chains plus SVar bodies, one carrier per chain):
//
//	API ChangeZone or ChangeZoneAll; Destination$ contains Library;
//	ValidTgts$ or Defined$ non-empty; NO LibraryPosition$;
//	no Shuffle$ True; no DestinationAlternative$ / AlternativeDecider$.
//
// Measured 2026-09-28: 44 ChangeZone carriers (Golgari Thug, Academy Ruins,
// Volrath's Stronghold, Mystic Sanctuary, Unholy Grotto, Champion of Stray
// Souls, ...) and 4 ChangeZoneAll carriers (Guiding Spirit, Head Games,
// Jester's Mask, Mirror of Fate). One ChangeZone carrier is Origin$ Library
// (the searched-library path, placeLibraryObjects) and is NOT fixed by
// golgari_thug2 -- the remaining 43 route through the fixed paths. The pin is
// bidirectional: a corpus bump or support change that alters the count fails
// here and forces a deliberate re-measure.
const absentAffectedCount = 44
const absentAffectedAllCount = 4

// TestAbsentLibraryPositionCensus counts the absent-spelling carriers over
// the compiled registry (golgari_thug2). A count drift means a corpus bump
// added or removed a "put ... on top of (your|its owner's) library" card, or
// a support change reclassified one; re-measure and re-pin deliberately.
func TestAbsentLibraryPositionCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	seen := map[*cards.SA]bool{}
	tzCarriers := map[string]bool{}
	allCarriers := map[string]bool{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			var walk func(sa *cards.SA)
			walk = func(sa *cards.SA) {
				for ; sa != nil; sa = sa.Sub {
					if seen[sa] {
						continue
					}
					seen[sa] = true
					if (sa.API != "ChangeZone" && sa.API != "ChangeZoneAll") ||
						!strings.Contains(sa.Params["Destination"], "Library") {
						continue
					}
					if strings.TrimSpace(sa.Params["ValidTgts"]) == "" &&
						strings.TrimSpace(sa.Params["Defined"]) == "" {
						continue
					}
					if strings.TrimSpace(sa.Params["LibraryPosition"]) != "" ||
						strings.EqualFold(sa.Params["Shuffle"], "True") ||
						sa.Params["DestinationAlternative"] != "" ||
						sa.Params["AlternativeDecider"] != "" {
						continue
					}
					if sa.API == "ChangeZone" {
						tzCarriers[f.Name] = true
					} else {
						allCarriers[f.Name] = true
					}
					break // one carrier per chain, not per sub
				}
			}
			for _, ab := range f.Abilities {
				walk(ab)
			}
			for _, tr := range f.Triggers {
				walk(tr.Effect)
			}
			for _, rp := range f.Repls {
				walk(rp.With)
			}
			for name := range f.SVars {
				walk(cards.ResolveSVar(f.SVars, name))
			}
		}
	}
	if len(tzCarriers) != absentAffectedCount || len(allCarriers) != absentAffectedAllCount {
		tz := make([]string, 0, len(tzCarriers))
		for n := range tzCarriers {
			tz = append(tz, n)
		}
		all := make([]string, 0, len(allCarriers))
		for n := range allCarriers {
			all = append(all, n)
		}
		slices.Sort(tz)
		slices.Sort(all)
		t.Errorf("absent LibraryPosition$ census drifted: measured %d ChangeZone carriers (pin %d), %d ChangeZoneAll carriers (pin %d)\n  ChangeZone: %v\n  ChangeZoneAll: %v",
			len(tzCarriers), absentAffectedCount, len(allCarriers), absentAffectedAllCount, tz, all)
	}
	if len(tzCarriers) == 0 || len(allCarriers) == 0 {
		t.Fatalf("census measured an empty class -- the corpus walk found nothing; a pin of %d/%d cannot be trusted",
			absentAffectedCount, absentAffectedAllCount)
	}
}
