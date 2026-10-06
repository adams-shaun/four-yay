package oraclediff

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

func TestCanonicalOfferedSortsSourceKindLabelAndIsOptIn(t *testing.T) {
	s := rules.OracleSnapshot{Checkpoint: "priority", Offered: []rules.OracleSnapOffered{
		{Source: "p1:B", Kind: "cast", Label: "z"},
		{Source: "p0:A", Kind: "ability", Label: "tap"},
		{Source: "p0:A", Kind: "cast", Label: "a"},
	}}
	base := Canonical([]rules.OracleSnapshot{s})
	if got := CanonicalOpts([]rules.OracleSnapshot{s}, nil); got != base {
		t.Fatalf("nil compare changed canonical form:\n%s\n!=\n%s", got, base)
	}
	got := CanonicalOpts([]rules.OracleSnapshot{s}, []string{CompareOffered})
	want := "offered: p0:A|ability|tap\np0:A|cast|a\np1:B|cast|z\n"
	if !strings.Contains(got, want) {
		t.Fatalf("offered field not canonically sorted; got:\n%s", got)
	}
	if err := ValidateCompare([]string{CompareOffered}); err != nil {
		t.Fatalf("offered compare rejected: %v", err)
	}
}
