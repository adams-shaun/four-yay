package templates

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

func TestStaticContinuousSpiderHamBearMatches(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	spiderHam, ok := reg.Lookup("Spider-Ham, Peter Porker")
	if !ok || len(spiderHam.Faces) == 0 || len(spiderHam.Faces[0].Statics) == 0 {
		t.Fatal("precondition: Spider-Ham static#0 is missing from the corpus")
	}
	bearCard, ok := reg.Lookup("Grizzly Bears")
	if !ok || len(bearCard.Faces) == 0 {
		t.Fatal("precondition: Grizzly Bears is missing from the corpus")
	}
	if !slices.Contains(bearCard.Faces[0].Types, "Bear") {
		t.Fatalf("precondition: Grizzly Bears types %v do not contain Bear", bearCard.Faces[0].Types)
	}
	affected := spiderHam.Faces[0].Statics[0].ParamStr(cards.PKAffected)
	words := affectedWords(affected)
	if !slices.Contains(words, "Bear") {
		t.Fatalf("precondition: Spider-Ham static#0 Affected$ %q has no Bear word (words %v)", affected, words)
	}

	it, final := servedFinal(t, reg, "Spider-Ham, Peter Porker", "static#0.0")
	if got := it.Setup["p0"].Battlefield; len(got) != 1 || got[0] != "Grizzly Bears" {
		t.Fatalf("served p0 battlefield %v, want only Grizzly Bears (the zero probe plan)", got)
	}
	p0Bear, ok := permanent(final, 0, "Grizzly Bears")
	if !ok {
		t.Fatal("precondition: served p0 Grizzly Bears is missing from final battlefield")
	}
	p1Bear, ok := permanent(final, 1, "Grizzly Bears")
	if !ok {
		t.Fatal("precondition: p1 Grizzly Bears is missing from final battlefield")
	}
	printed := printedPT(t, reg, "Grizzly Bears")
	if printed == "3/3" || p0Bear.PT == printed || p1Bear.PT == "3/3" {
		t.Fatalf("precondition: expected distinct pumped/control values, printed=%q p0=%q p1=%q", printed, p0Bear.PT, p1Bear.PT)
	}
	if p0Bear.PT != "3/3" {
		t.Errorf("p0 Grizzly Bears P/T %q, want 3/3", p0Bear.PT)
	}
	if p1Bear.PT != printed {
		t.Errorf("p1 Grizzly Bears P/T %q, want printed %q", p1Bear.PT, printed)
	}

	merfolk, ok := reg.Lookup("Coral Merfolk")
	if !ok || len(merfolk.Faces) == 0 || !slices.Contains(merfolk.Faces[0].Types, "Merfolk") {
		t.Fatal("precondition: Coral Merfolk is missing or is not a Merfolk")
	}
	merfolkPrinted := printedPT(t, reg, "Coral Merfolk")
	if merfolkPrinted != "2/1" {
		t.Fatalf("precondition: Coral Merfolk printed P/T %q, want 2/1", merfolkPrinted)
	}
	control := oraclegen.Scenario{Setup: map[string]oraclegen.Seat{
		"p0": {Battlefield: []string{"Spider-Ham, Peter Porker", "Coral Merfolk"}},
		"p1": {},
	}}
	data, err := json.Marshal(control)
	if err != nil {
		t.Fatalf("marshal negative-control scenario: %v", err)
	}
	result, err := rules.RunOracleScenarioJSON(reg, data)
	if err != nil || len(result.Fails) != 0 || len(result.Snapshots) == 0 {
		t.Fatalf("negative-control scenario does not replay: err=%v fails=%v", err, result.Fails)
	}
	controlMerfolk, ok := permanent(result.Snapshots[len(result.Snapshots)-1], 0, "Coral Merfolk")
	if !ok {
		t.Fatal("precondition: Coral Merfolk is not on p0's final battlefield")
	}
	if controlMerfolk.PT != "2/1" {
		t.Errorf("unmatched Coral Merfolk was broadened by Spider-Ham: P/T %q, want 2/1", controlMerfolk.PT)
	}
}
