package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPlayAmountAllCastsEachChosenCardInTurn: CR 608.2g, an Amount$ All
// Play ("you may cast any number of spells from among them") casts every
// chosen card, each cast completed before the next begins -- even when an
// earlier cast parks on its own target ask. Epic Experiment is the
// spell-side sibling of Etali, Primal Storm's trigger: its chained
// "put the cards that weren't cast into your graveyard" runs after the
// casts, so a cast card is never buried.
func TestPlayAmountAllCastsEachChosenCardInTurn(t *testing.T) {
	t.Parallel()
	run := runInlineOracle(t, `{
	  "name": "epic-experiment-casts-both",
	  "setup": {"p0": {"hand": ["Epic Experiment"], "library_top": ["Lightning Bolt", "Shock", "Forest"]}},
	  "steps": [
	    {"op": "cast", "seat": 0, "card": "p0:Epic Experiment", "mana": "CCCUR",
	     "answers": [{"kind": "choose", "pick": ["X = 3"]}]},
	    {"op": "resolve", "answers": [{"kind": "modes", "pick": ["Play Lightning Bolt", "Play Shock"]},
	                                  {"kind": "target", "pick": ["p1"]},
	                                  {"kind": "target", "pick": ["p1"]}]}
	  ],
	  "expect": [{"life": {"p1": 15}}, {"card": "p0:Lightning Bolt", "zone": "graveyard"},
	             {"card": "p0:Shock", "zone": "graveyard"}, {"card": "p0:Forest", "zone": "graveyard"}]
	}`)
	// Each chosen card is cast from exile, where it was chosen: the chain's
	// graveyard sweep runs only after both casts, so Shock is never buried
	// and then cast from the graveyard.
	shock := run.refs["p0:Shock"]
	for _, ev := range run.e.L.Events {
		if ev.Kind == events.PutOnStack && ev.Obj == shock && ev.From != state.ZExile {
			t.Fatalf("Shock was cast from %s, want exile", ev.From)
		}
		if ev.Kind == events.MoveZone && ev.Obj == shock && ev.From == state.ZExile && ev.To == state.ZGraveyard {
			t.Fatal("Shock was put into the graveyard by the sweep before it was cast")
		}
	}
}
