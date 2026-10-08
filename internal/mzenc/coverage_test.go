package mzenc

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// coverageView is a synthetic omniscient View that exercises EVERY emit path
// (globals, stack, mana, battlefield/perm/creature, hand, graveyard, exile)
// with no game and no corpus. It is deliberately non-empty: an empty view
// would make TestExtractorCoverageRatchetMatches vacuous, because no emit and
// no unsupported registration would ever fire.
func coverageView() view.View {
	bolt := view.CardView{ID: 100, Name: "Lightning Bolt", Types: "Instant", ManaCost: "R", SpellAPI: "DealDamage"}
	return view.View{
		Viewer: 0,
		Active: 0,
		Step:   "main1",
		Phase:  "main1",
		Players: []view.PlayerView{
			{
				ID:          0,
				Life:        20,
				LibrarySize: 40,
				HandSize:    1,
				Pool:        map[string]int32{"W": 2, "U": 1},
				Hand: []view.CardView{
					{ID: 20, Name: "Counterspell", Types: "Instant", ManaCost: "U U"},
				},
				Graveyard: []view.CardView{
					{ID: 21, Name: "Shock", Types: "Instant", ManaCost: "R"},
				},
				Exile: []view.CardView{
					{ID: 22, Name: "Banished Creature", Types: "Creature"},
				},
				Battlefield: []view.CardView{
					{ID: 10, Name: "Grizzly Bears", Types: "Creature Bear", ManaCost: "1 G", Power: 2, Toughness: 2,
						SummonSick: true, Keywords: []string{"Flying"}, Flags: view.FlagMonstrous,
						Imprinted: []state.ObjID{22}, Paired: 12, ExiledCards: []state.ObjID{22}},
					{ID: 11, Name: "Sol Ring", Types: "Artifact", Tapped: true},
					{ID: 12, Name: "Wolf Ally", Types: "Creature Wolf", ManaCost: "W/U", Power: 1, Toughness: 1},
					{ID: 13, Name: "Pacifism", Types: "Enchantment Aura", ManaCost: "1 W", AttachedTo: 10},
					{ID: 14, Name: "Curse of Thirst", Types: "Enchantment Aura", ManaCost: "3 B",
						AttachedToPlayer: true, AttachedPlayer: 1},
				},
				Counters: map[string]int32{"poison": 2, "energy": 1},
				Commanders: []view.CardView{
					{ID: 40, Name: "Krenko, Mob Boss", Types: "Creature"},
				},
			},
			{
				ID:          1,
				Life:        18,
				LibrarySize: 50,
				HandSize:    1,
				Pool:        map[string]int32{},
				Hand: []view.CardView{
					{ID: 30, Name: "Forest", Types: "Land"},
				},
			},
		},
		Stack: []view.StackView{
			{ID: 100, Name: "Lightning Bolt", Kind: "spell", Controller: 0,
				Card: &bolt, X: 2, Kicks: 1, Modes: []string{"DBDamage"},
				Targets: []view.TargetView{{Obj: 10, Label: "Any target"}}},
		},
	}
}

// TestExtractorCoverageRatchetMatches is the both-directions ratchet over the
// design's §5 feature families (mzenc design §7). It is stronger than a
// one-way "no new family" check:
//
//   - a REGISTER entry that the walker now emits is stale;
//   - a register entry no walk site reaches is an orphan registration;
//   - a register entry not named in spec §5 is an orphan spelling;
//   - a family the walker emits that spec §5 does not name is unknown;
//   - a family the walker records unsupported that spec §5 does not name is
//     unknown;
//   - an unsupported walk key with no register entry is an orphan key;
//   - a spec family neither emitted nor unsupported is a ratchet hole.
//
// The register keys and every w.unsupported[...] walk-site key are the SAME
// constants (coverage.go), so the spellings cannot drift.
func TestExtractorCoverageRatchetMatches(t *testing.T) {
	emitted, unsupported := ProcessStateReport(coverageView(), nil, 0, 0, "x")

	for fam := range unsupportedFeatures {
		if emitted[fam] {
			t.Errorf("stale register entry %q: walker now emits it", fam)
		}
		if !unsupported[fam] {
			t.Errorf("orphan register entry %q: no walk site records it", fam)
		}
		if !specFamilies[fam] {
			t.Errorf("register entry %q not in spec §5", fam)
		}
	}
	for fam := range emitted {
		if !specFamilies[fam] {
			t.Errorf("walker emitted unknown family %q not in spec §5", fam)
		}
	}
	for fam := range unsupported {
		if !specFamilies[fam] {
			t.Errorf("walker recorded unsupported family %q not in spec §5", fam)
		}
		if _, ok := unsupportedFeatures[fam]; !ok {
			t.Errorf("orphan walk key %q: unsupported but not registered", fam)
		}
	}
	for fam := range specFamilies {
		if !emitted[fam] && !unsupported[fam] {
			t.Errorf("spec family %q neither emitted nor registered — the ratchet has a hole", fam)
		}
	}

	t.Logf("mzenc extractor coverage: %d spec families, %d emitted, %d unsupported",
		len(specFamilies), len(emitted), len(unsupported))
	if len(emitted) == 0 || len(unsupported) == 0 {
		t.Errorf("coverageView is vacuous: emitted=%d unsupported=%d", len(emitted), len(unsupported))
	}
}

// TestProcessStateReportDoesNotAffectIDs pins that the reporting walk runs the
// SAME walk as ProcessState: the id set is identical whether or not the
// emitted recording is on, so the nil-on-the-hot-path optimisation is inert.
func TestProcessStateReportDoesNotAffectIDs(t *testing.T) {
	v := coverageView()
	want := ProcessState(v, nil, 0, 0, "x")
	got, _ := processState(v, nil, 0, 0, "x", map[string]bool{}, map[string]bool{})
	if len(got) != len(want) {
		t.Fatalf("report id set differs: %d vs %d", len(got), len(want))
	}
	for id := range want {
		if _, ok := got[id]; !ok {
			t.Fatalf("report missing id %d", id)
		}
	}
}
