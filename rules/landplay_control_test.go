package rules

import "testing"

// TestPlayedLandYouDontOwnEntersUnderYourControl: CR 305.1/110.2, a land the
// player plays from a zone another seat controls it in (Tinybones's stashed
// opponent card here) enters under the PLAYER's control, so the player's own
// landfall sees it and the owner's does not. The control change rides before
// the battlefield entry, the land-play twin of the cast path's CR 601.2a
// ControlChange.
func TestPlayedLandYouDontOwnEntersUnderYourControl(t *testing.T) {
	t.Parallel()
	runInlineOracle(t, `{
	  "name": "stashed-land-enters-under-the-players-control",
	  "setup": {"p0": {"battlefield": ["Tinybones, Bauble Burglar", "Steppe Lynx"]},
	            "p1": {"hand": ["Forest"], "battlefield": ["Steppe Lynx"]}},
	  "steps": [
	    {"op": "activate", "seat": 0, "card": "p0:Tinybones, Bauble Burglar", "mana": "CCCB"},
	    {"op": "resolve"},
	    {"op": "play", "seat": 0, "card": "p1:Forest"},
	    {"op": "resolve"}
	  ],
	  "expect": [
	    {"card": "p1:Forest", "zone": "battlefield"},
	    {"count": {"seat": 0, "zone": "battlefield", "name": "Forest"}, "eq": 1},
	    {"count": {"seat": 1, "zone": "battlefield", "name": "Forest"}, "eq": 0},
	    {"card": "p0:Steppe Lynx", "pt": "2/3"},
	    {"card": "p1:Steppe Lynx", "pt": "0/1"}
	  ]
	}`)
}
