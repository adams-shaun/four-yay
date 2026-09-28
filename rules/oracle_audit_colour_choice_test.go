package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

func TestOracleColourChoice(t *testing.T) {
	options := []decision.Option{
		{Index: 0, Kind: "mana", Label: "Add B", ManaSymbol: "B"},
		{Index: 1, Kind: "mana", Label: "Add R", ManaSymbol: "R"},
	}
	withWhite := append(append([]decision.Option(nil), options...), decision.Option{Index: 2, Kind: "mana", Label: "Add W", ManaSymbol: "W"})
	if len(withWhite) == len(options) {
		t.Fatal("test precondition: compared option sets must differ")
	}
	if options[1].ManaSymbol != "R" {
		t.Fatalf("test precondition: Add R option has ManaSymbol %q", options[1].ManaSymbol)
	}

	observe := oracleObserve{Kind: "choose", Has: []string{"Black", "Red"}, Not: []string{"White"}}
	br := &decision.Decision{Kind: decision.KChoose, Options: options}
	brw := &decision.Decision{Kind: decision.KChoose, Options: withWhite}
	if got := oracleObserveMismatches(br, observe); len(got) != 0 {
		t.Fatalf("B/R options unexpectedly mismatch: %v", got)
	}
	if got := oracleObserveMismatches(brw, observe); len(got) != 1 {
		t.Fatalf("B/R/W options should mismatch absent-colour assertion, got %v", got)
	}
	missingRed := &decision.Decision{Kind: decision.KChoose, Options: options[:1]}
	if got := oracleObserveMismatches(missingRed, observe); len(got) != 1 {
		t.Fatalf("options without Red should mismatch required-colour assertion, got %v", got)
	}

	r := &oracleRun{}
	idx, err := r.matchPick(br, "Red", map[int]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if idx != 1 || br.Options[idx].ManaSymbol != "R" {
		t.Fatalf("Red matched option %d (%+v), want Add R with ManaSymbol R", idx, br.Options[idx])
	}
}
