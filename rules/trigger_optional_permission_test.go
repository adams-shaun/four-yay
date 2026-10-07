package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestPermissionOnlyMayTriggersAreMandatory pins the CR 603.5 census behind
// triggerOptionalSpec: a printed trigger whose script carries OptionalDecider$
// but whose only "may" is a play/cast permission it grants is mandatory. The
// census over the whole corpus must name exactly these cards, so a new
// carrier (or a description rewrite) is reviewed rather than silently
// flipped.
func TestPermissionOnlyMayTriggersAreMandatory(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	want := map[string]bool{"Strongbox Raider": true, "Whispersteel Dagger": true}
	got := map[string]bool{}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			for _, tr := range f.Triggers {
				if tr.Params["OptionalDecider"] != "" && triggerOptionalSpec(tr) == "" {
					got[f.Name] = true
				}
			}
		}
	}
	for n := range want {
		if !got[n] {
			t.Errorf("%s: its permission-only OptionalDecider$ trigger is still optional", n)
		}
	}
	for n := range got {
		if !want[n] {
			t.Errorf("%s: newly read as a permission-only may trigger; review and add it here", n)
		}
	}
	// A posed yes/no would be answered "no" and leave both cards in the
	// library; the exile is mandatory, so no such ask exists.
	runInlineOracle(t, `{
	  "name": "strongbox-raider-exile-is-mandatory",
	  "setup": {"p0": {"hand": ["Strongbox Raider"], "battlefield": ["Grizzly Bears"],
	                   "library_top": ["Lightning Bolt", "Shock"]}},
	  "steps": [
	    {"op": "attack", "seat": 0, "attackers": ["p0:Grizzly Bears"], "defender": "p1"},
	    {"op": "pass_to", "step": "main2"},
	    {"op": "cast", "seat": 0, "card": "p0:Strongbox Raider", "mana": "CCRR"},
	    {"op": "resolve", "answers": [{"kind": "choose", "pick": ["p0:Shock"]}]}
	  ],
	  "expect": [{"card": "p0:Lightning Bolt", "zone": "exile"}, {"card": "p0:Shock", "zone": "exile"}]
	}`)
}
