package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestCrewedThisTurnCensus is the MECHANISM-CLASS census for Forge's
// Creature.CrewedThisTurn / Card.CrewedThisTurn filter (CR 702.122, task
// crewedthisturn1). It scans the compiled corpus for every face whose specs
// name the token and pins the carrier set, so a body added to or removed from
// the corpus is caught here rather than silently changing what the filter must
// support. The engine's predicate (effects/filter.go's "CrewedThisTurn") is
// source-relative and crew-event-backed, not card-specific, so all six share
// one implementation:
//
//   - Turtle Van            -- "put a +1/+1 counter on target creature that
//     crewed it this turn" (the end-to-end carrier in
//     TestSetAudit_tmt_TurtleVan_CrewedThisTurnTarget)
//   - Getaway Car           -- "return up to one target creature that crewed
//     it this turn to its owner's hand"
//   - Golden Argosy         -- "exile each creature that crewed it this turn"
//     (a non-target ChangeType$ filter, proving the source binding reaches
//     the effect walk too)
//   - Leisure Bicycle       -- "target creature that crewed it this turn
//     explores"
//   - Smogbelcher Chariot   -- "target creature that crewed it this turn
//     perpetually gains ..." (spelled Card.CrewedThisTurn)
//   - Subterranean Schooner -- "target creature that crewed it this turn
//     explores"
func TestCrewedThisTurnCensus(t *testing.T) {
	const token = "CrewedThisTurn"
	reg := testutil.CorpusRegistry(t)
	if reg == nil {
		t.Skip("crew census corpus unavailable")
	}

	got := map[string]bool{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if faceNamesToken(f, token) {
				got[f.Name] = true
			}
		}
	}
	// Precondition: the scan must find something, or a corpus/API change has
	// made this test vacuous (it would pass with the token entirely absent).
	if len(got) == 0 {
		t.Fatalf("census found no face naming %q: did the corpus cache change shape?", token)
	}

	want := []string{
		"Turtle Van",
		"Getaway Car",
		"Golden Argosy",
		"Leisure Bicycle",
		"Smogbelcher Chariot",
		"Subterranean Schooner",
	}
	var missing []string
	for _, name := range want {
		if !got[name] {
			missing = append(missing, name)
		}
	}
	var extra []string
	for name := range got {
		found := false
		for _, w := range want {
			if name == w {
				found = true
				break
			}
		}
		if !found {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("Creature.CrewedThisTurn carriers drifted: missing %v, unexpected %v (corpus set = %v)",
			missing, extra, sortedCrewNames(got))
	}
}

// faceNamesToken reports whether any spec on the face names token: the SVar
// bodies (where ValidTgts$/ChangeType$ live for every current carrier), the
// root abilities and their Sub chains, the triggers, statics and replacements.
// Display text (Oracle/TriggerDescription) is deliberately NOT scanned -- it
// prints the phrase with spaces, never the filter token, so a scan of it could
// only produce false positives.
func faceNamesToken(f *cards.Face, token string) bool {
	for _, body := range f.SVars {
		if strings.Contains(body, token) {
			return true
		}
	}
	for _, sa := range f.Abilities {
		if saNamesToken(sa, token) {
			return true
		}
	}
	for _, tr := range f.Triggers {
		if paramsNameToken(tr.Params, token) || saNamesToken(tr.Effect, token) {
			return true
		}
	}
	for _, st := range f.Statics {
		if paramsNameToken(st.Params, token) {
			return true
		}
	}
	for _, rp := range f.Repls {
		if paramsNameToken(rp.Params, token) || saNamesToken(rp.With, token) {
			return true
		}
	}
	return false
}

func saNamesToken(sa *cards.SA, token string) bool {
	for sa != nil {
		if paramsNameToken(sa.Params, token) {
			return true
		}
		sa = sa.Sub
	}
	return false
}

func paramsNameToken(params map[string]string, token string) bool {
	for _, v := range params {
		if strings.Contains(v, token) {
			return true
		}
	}
	return false
}

func sortedCrewNames(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
