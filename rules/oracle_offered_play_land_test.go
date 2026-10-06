package rules

import "testing"

// TestOracleOfferedPlayMatchesALandPlay: a scenario's offered kind "play" is
// how the XMage side names playing a land; gorge poses it as "play_land".
// The positive assertion must find it, and the negative one must still hold
// for a land in the library (not playable) so the alias cannot match
// everything.
func TestOracleOfferedPlayMatchesALandPlay(t *testing.T) {
	t.Parallel()
	runInlineOracle(t, `{
	  "name": "offered-play-land",
	  "setup": {"p0": {"hand": ["Forest"], "library": ["Mountain"]}},
	  "steps": [
	    {"op": "pass_to", "seat": 0, "step": "main1", "decision": "priority",
	     "expect": [{"offered": {"seat": 0, "kind": "play", "card": "p0:Forest"}, "want": true},
	                {"offered": {"seat": 0, "kind": "play", "card": "p0:Mountain"}, "want": false}]}
	  ]
	}`)
}
