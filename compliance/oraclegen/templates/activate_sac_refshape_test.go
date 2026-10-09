package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// TestObservedSacPickRefShape guards observedSacPick's alias form against a
// malformed ref: a Sac cost that names Room may observe a pick whose ref is
// not a "p<seat>:<name>" object ref (a bare label, e.g. a future Sac filter
// read from an option that carries no object identity). The "@" prefix is
// only meaningful before a scenario ref the driver can bind by object id
// (isScenarioRef on the Java side); "@<bare label>" would reach XMage's
// choice queue verbatim and fail the scenario on "Found wrong choice
// command". A shaped ref keeps the alias; anything else falls back to the
// plain-name path the other Sac cards agree on (agent-20261009T041321Z-84a3a3ce
// round 2).
func TestObservedSacPickRefShape(t *testing.T) {
	// A Room-naming Sac token: the same cost Intruding Soulrager carries.
	tok := "Sac<1/Room>"
	d := rules.OracleDecision{
		Picks:           []string{"Bottomless Pool", "Llanowar Elves"},
		PickRefs:        []string{"Room", "p0:Llanowar Elves"},
		PickRefsInexact: []bool{false, false},
	}
	if got := observedSacPick(tok, d, 0); got != "Bottomless Pool" {
		t.Fatalf("unshaped ref must fall back to the plain-name pick, got %q", got)
	}
	if got := observedSacPick(tok, d, 1); got != "@p0:Llanowar Elves" {
		t.Fatalf("shaped ref keeps the exact-ref alias, got %q", got)
	}
}
