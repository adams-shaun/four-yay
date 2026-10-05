package effects

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// digNoChangeNumCarriers pins every corpus card whose Dig SA omits
// ChangeNum$. Forge's DigEffect defaults ChangeNum to 1, so each of these
// takes at most one matching card from its DigNum window. A new script with
// an omitted ChangeNum$ is a NEW carrier whose take the default governs; it
// must appear here (and its window shape get a behavioural test) rather than
// silently falling through.
//
// The set was measured at FORGE_REF by walking every face ability and SVar
// for api:Dig with no ChangeNum$ param.
var digNoChangeNumCarriers = []string{
	"A-Lantern of Revealing",
	"A-Nael, Avizoa Aeronaut",
	"Aid from the Cowl",
	"Ajani, Sleeper Agent",
	"Arbiter of the Ideal",
	"Celestus Sanctifier",
	"Clone Shell",
	"Corpse Appraiser",
	"Counterbalance",
	"Court Hussar",
	"Creative Outburst",
	"Crown of Convergence",
	"Discerning Taste",
	"Discover the Impossible",
	"Dragonlord Ojutai",
	"Ellyn Harbreeze, Busybody",
	"Feral Encounter",
	"Fires of Mount Doom",
	"Firja, Judge of Valor",
	"Fomori Vault",
	"Forbidden Alchemy",
	"Glimpse the Future",
	"Glint Raker",
	"Hoshi Sato, Exolinguist",
	"Industrial Advancement",
	"Interdisciplinary Mascot",
	"Jace, the Living Guildpact",
	"Jewel Mine Overseer",
	"Judge Unworthy",
	"Lantern of Revealing",
	"Lone Revenant",
	"Lurking Predators",
	"Machinate",
	"Maestros Charm",
	"Malevolent Rumble",
	"Mayael the Anima Avatar",
	"Memories Returning",
	"Moonring Island",
	"Muzzio, Visionary Architect",
	"Nael, Avizoa Aeronaut",
	"Necrosynthesis",
	"Nissa's Revelation",
	"Niv-Mizzet Reborn",
	"Plunge into Darkness",
	"Prophetic Bolt",
	"Prophetic Titan",
	"Psychotic Episode",
	"Puresight Merrow",
	"Putrid Cyclops",
	"Ral's Outburst",
	"Ransack the Lab",
	"Record Store",
	"Rediscover the Way",
	"Riddle of Lightning",
	"Rootwater Mystic",
	"Scion of Halaster",
	"Sea Gate Oracle",
	"Shrine of Piercing Vision",
	"Sight Beyond Sight",
	"Sleight of Hand",
	"Soulcipher Board",
	"Stolen Strategy",
	"Strategic Planning",
	"Taigam, Sidisi's Hand",
	"Tapping at the Window",
	"Teferi, Temporal Archmage",
	"The Ruinous Powers",
	"Thief of Sanity",
	"Thoughtpicker Witch",
	"Tomorrow, Azami's Familiar",
	"Track Down",
	"Urianger Augurelt",
	"Vivien, Champion of the Wilds",
	"Wail of the Forgotten",
	"Warlock Class",
	"Worldly Counsel",
	"Wu Spy",
	"Zurgo and Ojutai",
}

// TestDigNoChangeNumCensus walks every corpus Dig SA and asserts the set of
// cards that omit ChangeNum$ equals digNoChangeNumCarriers exactly. A new
// carrier -- a script the ratchet has never seen -- fails loudly, and a
// stale entry the corpus no longer carries fails too. It also asserts the
// compiled default path: every such SA reports ChangeNum.Present false, so
// effDig's Forge default of one is the one that governs its take.
func TestDigNoChangeNumCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	got := map[string]bool{}
	sawDig := 0
	visit := func(name string, sa *cards.SA) {
		if sa == nil || sa.API != "Dig" {
			return
		}
		sawDig++
		if _, present := sa.Param(cards.PKChangeNum); present {
			return
		}
		got[name] = true
		// The compiled reader must agree the param is absent: the default
		// (one) is what governs, never a bogus present-zero.
		if dp := DigOf(sa); dp.ChangeNum.Present {
			t.Errorf("%s: Dig SA omits ChangeNum$ but DigOf reports Present=true (%q)", name, dp.ChangeNum.Text)
		}
	}
	for _, card := range reg.Cards {
		for _, f := range card.Faces {
			for _, sa := range f.Abilities {
				visit(f.Name, sa)
			}
			names := make([]string, 0, len(f.SVars))
			for n := range f.SVars {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				if strings.Contains(f.SVars[n], "Dig") {
					visit(f.Name, cards.ResolveSVar(f.SVars, n))
				}
			}
		}
	}
	if sawDig < 200 {
		t.Fatalf("census saw only %d Dig SAs: the scan is not reading the corpus", sawDig)
	}
	want := map[string]bool{}
	for _, n := range digNoChangeNumCarriers {
		want[n] = true
	}
	var added, removed []string
	for n := range got {
		if !want[n] {
			added = append(added, n)
		}
	}
	for n := range want {
		if !got[n] {
			removed = append(removed, n)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	if len(added) > 0 {
		t.Errorf("new Dig carriers omit ChangeNum$ (Forge default 1 governs their take): %v", added)
	}
	if len(removed) > 0 {
		t.Errorf("pinned carriers no longer omit ChangeNum$ (stale ratchet entries): %v", removed)
	}
	t.Logf("%d Dig SAs, %d absent-ChangeNum carriers", sawDig, len(got))
}
