package oraclediff

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

func TestCanonicalOfferedSortsSourceKindLabelAndIsOptIn(t *testing.T) {
	s := rules.OracleSnapshot{Checkpoint: "priority", Offered: []rules.OracleSnapOffered{
		{Source: "p1:B", Kind: "cast", Label: "z"},
		{Source: "p0:A", Kind: "activate", Label: "tap"},
		{Source: "p0:A", Kind: "activate", Label: "Add {R}"},
		{Source: "p0:A", Kind: "cast", Label: "a"},
	}}
	base := Canonical([]rules.OracleSnapshot{s})
	if got := CanonicalOpts([]rules.OracleSnapshot{s}, nil); got != base {
		t.Fatalf("nil compare changed canonical form:\n%s\n!=\n%s", got, base)
	}
	got := CanonicalOpts([]rules.OracleSnapshot{s}, []string{CompareOffered})
	want := "offered: p0:A|activate|addr\np0:A|activate|tap\np0:A|cast|\np1:B|cast|\n"
	if !strings.Contains(got, want) {
		t.Fatalf("offered field not canonically sorted; got:\n%s", got)
	}
	if err := ValidateCompare([]string{CompareOffered}); err != nil {
		t.Fatalf("offered compare rejected: %v", err)
	}
}
