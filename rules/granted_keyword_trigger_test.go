package rules

// Granted triggered keywords (CR 613.1f): a layer-6 AddKeyword$ grant of a
// keyword whose rules text is a triggered ability gives the recipient the
// same ability a printed K: line would, but the printed expansion only covers
// printed lines and a granted keyword has no face trigger. The granted
// walks synthesize those triggers. Wizard's Staff's "Equipped creature has
// prowess" is the card the oracle scenario caught; this file pins the
// mechanism's real-corpus behaviour and censuses every Equipment/Aura that
// grants a triggered keyword so a new one cannot slip in uncovered.

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestWizardsStaffGrantedProwessUsesRealCorpusCard drives Wizard's Staff's
// real compiled script: its equip grants the equipped Grizzly Bears Prowess,
// and casting a noncreature spell pumps the bear +1/+1 until end of turn.
// Without the granted-keyword walk the grant reaches the layer system (the
// Oracle scenario's equip-wizard-costs-one asserts the keyword is present)
// but no trigger fires, so the bear stays 2/2.
func TestWizardsStaffGrantedProwessUsesRealCorpusCard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	files := loadOracleFiles(t)
	const path = "testdata/oracle/equipment/wizards-staff.json"
	f, ok := files[path]
	if !ok {
		t.Fatalf("scenario file %s is missing", path)
	}
	var found bool
	for i := range f.Scenarios {
		sc := f.Scenarios[i]
		if sc.Name != "equipped-creature-gains-prowess" {
			continue
		}
		found = true
		fails, transcript, run := runOracleScenario(reg, sc)
		if run.e == nil {
			t.Fatalf("scenario did not build an engine:\n%s", strings.Join(transcript, "\n"))
		}
		if len(fails) != 0 {
			t.Fatalf("granted Prowess did not fire: %s\n  transcript:\n    %s",
				strings.Join(fails, "\n  "), strings.Join(transcript, "\n    "))
		}
	}
	if !found {
		t.Fatal("scenario equipped-creature-gains-prowess is missing")
	}
}

// grantedTriggerKeywordHeads returns every keyword head whose rules text is a
// triggered ability, derived from the corpus itself: a printed keyword
// expansion (cards/keywords.go addKeywordTrigger) tags its minted trigger with
// KeywordLine, so any head that appears on a Trigger.KeywordLine mints a
// trigger. Deriving the set keeps this census from drifting with
// cards/keywords.go.
func grantedTriggerKeywordHeads(reg *cards.Registry) map[string]bool {
	heads := map[string]bool{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			for i := range f.Triggers {
				line := f.Triggers[i].ParamStr(cards.PKKeywordLine)
				if line == "" {
					continue
				}
				heads[cards.KeywordHead(line)] = true
			}
		}
	}
	return heads
}

// grantedTriggerKeywordWalk maps each granted triggered head to the rules
// walk that fires it, so the census can tell a covered grant from an
// uncovered one. A head missing from this map is uncovered: its trigger does
// not fire when granted, which is the defect class this census ratchets.
var grantedTriggerKeywordWalk = map[string]string{
	// Synthesized from the keyword line by grantedKeywordTrigger.
	"Prowess": "checkGrantedStaticTriggersUsing",
	// Dedicated per-keyword walks.
	"Afflict":           "checkGrantedAfflictTriggers",
	"Cumulative upkeep": "checkGrantedCumulativeUpkeepTriggers",
	"Flanking":          "queueGrantedFlanking",
	"Mentor":            "checkGrantedMentorTriggers",
	"Ward":              "checkGrantedWardTriggers",
}

// knownEquipAuraTriggerKeywords is the exact set of Equipment/Aura cards that
// grant a triggered keyword at FORGE_REF, each mapped to the granted head and
// whether the build fires it. Ruling R-20's two-direction contract: a new
// grant fails naming the card and head, and a stale entry fails too. The
// uncovered rows are a known gap (the ticket that removes this census must
// fire them); the covered rows must stay covered.
var knownEquipAuraTriggerKeywords = map[string][2]string{
	"A-Plate Armor":         {"Ward", "covered"},
	"Aegis of the Legion":   {"Mentor", "covered"},
	"Agility":               {"Flanking", "covered"},
	"Armguard Familiar":     {"Ward", "covered"},
	"Brotherhood Regalia":   {"Ward", "covered"},
	"Chains of Custody":     {"Ward", "covered"},
	"Combat Research":       {"Ward", "covered"},
	"Crystal Carapace":      {"Ward", "covered"},
	"Dagger of the Worthy":  {"Afflict", "covered"},
	"Decomposition":         {"Cumulative upkeep", "covered"},
	"Dwarven Mattock":       {"Ward", "covered"},
	"Falcon's Wing Harness": {"Ward", "covered"},
	"Hardlight Containment": {"Ward", "covered"},
	"Hunter's Bow":          {"Ward", "covered"},
	"Lavaspur Boots":        {"Ward", "covered"},
	"Leather Armor":         {"Ward", "covered"},
	"Leyline Immersion":     {"Ward", "covered"},
	"Mana Chains":           {"Cumulative upkeep", "covered"},
	"Paladin's Arms":        {"Ward", "covered"},
	"Plate Armor":           {"Ward", "covered"},
	"Psychic Paper":         {"Ward", "covered"},
	"Secret Invasion":       {"Ward", "covered"},
	"Sheltered by Ghosts":   {"Ward", "covered"},
	"Super Strength":        {"Ward", "covered"},
	"Thran Power Suit":      {"Ward", "covered"},
	"Tome of Gadwick":       {"Ward", "covered"},
	"Winged Boots":          {"Ward", "covered"},
	"Wizard's Staff":        {"Prowess", "covered"},
	// uncovered: no granted walk fires the head yet (file a ticket per head).
	"Blade of Selves":      {"Myriad", "uncovered"},
	"Eldrazi Conscription": {"Annihilator", "uncovered"},
	"Infantry Shield":      {"Mobilize", "uncovered"},
	"Nazgûl Battle-Mace":   {"Annihilator", "uncovered"},
}

func TestEquipAuraGrantedTriggerKeywordCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	triggered := grantedTriggerKeywordHeads(reg)
	if len(triggered) == 0 {
		t.Fatal("no printed keyword expansion carries KeywordLine: the census cannot classify heads")
	}
	measured := map[string][2]string{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			isEquipAura := false
			for _, ty := range f.Types {
				switch strings.ToLower(strings.TrimSpace(ty)) {
				case "equipment", "aura":
					isEquipAura = true
				}
			}
			if !isEquipAura {
				continue
			}
			for i := range f.Statics {
				st := &f.Statics[i]
				raw := st.ParamStr(cards.PKAddKeyword)
				if strings.TrimSpace(raw) == "" {
					continue
				}
				for _, line := range cards.SplitKeywordList(raw) {
					head := cards.KeywordHead(line)
					if !triggered[head] {
						continue
					}
					coverage := "uncovered"
					if _, ok := grantedTriggerKeywordWalk[head]; ok {
						coverage = "covered"
					}
					if prev, dup := measured[f.Name]; dup && prev != [2]string{head, coverage} {
						t.Errorf("%s grants triggered keywords %v and %v; the census assumes one head per card",
							f.Name, prev, [2]string{head, coverage})
					}
					measured[f.Name] = [2]string{head, coverage}
				}
			}
		}
	}
	// Precondition: the census must actually have found the card the ticket
	// is about; an empty or short scan would make every assertion vacuous.
	if _, ok := measured["Wizard's Staff"]; !ok {
		t.Fatal("precondition: Wizard's Staff was not measured as an Equipment granting a triggered keyword")
	}
	var got, want []string
	for name, v := range measured {
		got = append(got, name+" -> "+v[0]+" ("+v[1]+")")
	}
	for name, v := range knownEquipAuraTriggerKeywords {
		want = append(want, name+" -> "+v[0]+" ("+v[1]+")")
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Equipment/Aura granted triggered-keyword census changed.\n got:\n%s\nwant:\n%s\n"+
			"Covered means the granted trigger fires; uncovered is the known gap.",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Every head the census calls covered must have a walk AND, for the
	// synthesized ones, a trigger the synthesizer can build -- otherwise the
	// census is asserting coverage that does not exist.
	for name, v := range measured {
		if v[1] != "covered" {
			continue
		}
		if v[0] == "Prowess" {
			line := "Prowess"
			if grantedKeywordTrigger(line) == nil {
				t.Errorf("%s: census calls Prowess covered but grantedKeywordTrigger returns nil", name)
			}
		}
	}
}
