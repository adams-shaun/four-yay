package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestEmpowerClassParamCensus is the class census behind api:Empower,
// count:YouScryThisTurn/YouSurveilThisTurn and stat:DisableTriggers: it walks
// the WHOLE corpus (not only the repo decks the paramcensus ratchet covers),
// finds every card carrying one of the three, and asserts the parameter
// census (cardCensusLabels -- the code-derived read sets) labels none of the
// class's own parameters as unread. A new carrier shape -- a Defined$ on an
// Empower line, a DisableTriggers parameter the gate does not read -- fails
// here by name instead of silently resolving as a default.
//
// The carrier counts are pinned so a corpus pin bump that adds carriers is
// noticed and re-censused: 35 Empower carriers, 4 scry/surveil-this-turn
// readers, 9 DisableTriggers lines over 8 cards (measured at the corpus pin,
// `grep -rlE '(DB|SP|AB)\$ Empower' .cards/cardsfolder`).
func TestEmpowerClassParamCensus(t *testing.T) {
	_, d := measureParamCensus(t, nil)
	reg := testutil.CorpusRegistry(t)
	var empower, scry, disable []string
	var bad []string
	for _, c := range reg.Cards {
		var isEmpower, isScry, isDisable bool
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			var walk func(sa *cards.SA)
			walk = func(sa *cards.SA) {
				for ; sa != nil; sa = sa.Sub {
					if sa.API == "Empower" {
						isEmpower = true
					}
				}
			}
			for _, a := range f.Abilities {
				walk(a)
			}
			for _, tr := range f.Triggers {
				walk(tr.Effect)
			}
			for name := range f.SVars {
				walk(cards.ResolveSVar(f.SVars, name))
				body := f.SVars[name]
				if strings.Contains(body, "Count$YouScryThisTurn") || strings.Contains(body, "Count$YouSurveilThisTurn") {
					isScry = true
				}
			}
			for _, st := range f.Statics {
				if st.Mode == "DisableTriggers" {
					isDisable = true
				}
			}
		}
		if !isEmpower && !isScry && !isDisable {
			continue
		}
		name := c.Faces[0].Name
		if isEmpower {
			empower = append(empower, name)
		}
		if isScry {
			scry = append(scry, name)
		}
		if isDisable {
			disable = append(disable, name)
		}
		for _, l := range cardCensusLabels(c, d, nil) {
			if strings.HasPrefix(l, "param:api:Empower.") || strings.HasPrefix(l, "param:stat:DisableTriggers.") {
				bad = append(bad, name+": "+l)
			}
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("class carriers with an unread parameter:\n  %s", strings.Join(bad, "\n  "))
	}
	if len(empower) != 35 || len(scry) != 4 || len(disable) != 8 {
		t.Errorf("carrier counts moved (re-census the new shapes): Empower %d (want 35), scry/surveil readers %d (want 4) %v, DisableTriggers %d (want 8) %v",
			len(empower), len(scry), scry, len(disable), disable)
	}
	t.Logf("census: %d Empower carriers, %d scry/surveil-this-turn readers, %d DisableTriggers cards; 0 unread class params", len(empower), len(scry), len(disable))
}
