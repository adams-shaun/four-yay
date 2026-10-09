package templates

import (
	"testing"
)

// TestActivateRemoveCountersSourceSelfRef generates the level-B activate item
// for cards whose Forge Oracle spells the source of a counter-removal cost as
// "this creature" / "this artifact" and pins the XMage rule-text prefix to the
// "{this}" placeholder RemoveCountersSourceCost renders. Each item must also
// generate and play through gorge (assertActivateItem runs PlaysThrough).
func TestActivateRemoveCountersSourceSelfRef(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, wantPrefix string }{
		{"District Mascot", "activate#0.0", "{1}{G}, Remove two +1/+1 counters from {this}"},
		{"Brambleback Brute", "activate#0.0", "{1}{R}, Remove a counter from {this}"},
		{"Burdened Stoneback", "activate#0.0", "{1}{W}, Remove a counter from {this}"},
		{"Moonlit Lamenter", "activate#0.0", "{1}{W}, Remove a counter from {this}"},
		{"Loch Mare", "activate#0.0", "{1}{U}, Remove a counter from {this}"},
		{"Hovel Hurler", "activate#0.0", "{R/W}{R/W}, Remove a counter from {this}"},
		{"Reaping Willow", "activate#0.0", "{1}{W/B}, Remove two counters from {this}"},
		{"Rimefire Torque", "activate#0.0", "{T}, Remove three charge counters from {this}"},
		{"Weather Maker", "activate#0.1", "{T}, Remove two charge counters from {this}"},
		{"Weather Maker", "activate#0.2", "{T}, Remove three charge counters from {this}"},
	} {
		t.Run(tc.name+" "+tc.key, func(t *testing.T) {
			assertActivateItem(t, reg, tc.name, tc.key, tc.wantPrefix)
		})
	}
}
