package templates

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestActivateLevelBRowsAnnouncedCounts serves six example rows of the
// g10/g14 level-B activate classes the ticket (cli-20261009T031408Z-5823e7de)
// clears. Each case first asserts the PRECONDITION the served scenario
// depends on -- the source where the activation happens and the setup the
// payment needs -- then that the item generates and the scenario plays
// through gorge with the effect the scenario compares.
func TestActivateLevelBRowsAnnouncedCounts(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, key string
		// inGraveyard/inHand place the source in the activation zone the
		// requirement's sub-family names; counters seed the source's own
		// gate the scenario must satisfy (Cryptex's five unlock counters).
		inGraveyard, inHand bool
		preCounters         map[string]int
	}{
		// DFT: {2}{U}{B}, {T}, Exile X artifacts from your graveyard.
		// The announced <X/> count reads the fixture: one artifact in the
		// graveyard pays X=1.
		{"Winter, Cursed Rider", "activate#0.0", false, false, nil},
		// DSK: Exile this card and two other cards named Say Its Name from
		// your graveyard: the payment is three copies of the card itself,
		// which the source-in-graveyard placement plus two fixture copies pay.
		{"Say Its Name", "activate#0.1", true, false, nil},
		// MKM: Sac, Surveil 3, draw 3 -- only with five unlock counters on
		// Cryptex, seeded at setup.
		{"Cryptex", "activate#0.1", false, false, map[string]int{"UNLOCK": 5}},
		// EOE: {2}, {T}, Tap X untapped artifacts you control: target
		// creature gets +X/+0. The X is the tap election's own selection.
		{"Secluded Starforge", "activate#0.1", false, false, nil},
		// TMT: target attacking creature token -- the fixture token is given
		// haste (Fervor) because a summoning-sick token cannot attack.
		{"Old Hob, Alleycat Blues", "activate#0.0", false, false, nil},
		// FIN: target creature card exiled with The Darkness Crystal -- the
		// prelude kills p1's creature while the source is on the
		// battlefield, so its replacement exiles it bound to the source.
		{"The Darkness Crystal", "activate#0.0", false, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := activateRowRequirement(t, reg, tc.name, tc.key)
			if tc.inGraveyard && req.Sub != "activate.graveyard" {
				t.Fatalf("precondition: %s %s sub = %q, want activate.graveyard", tc.name, tc.key, req.Sub)
			}
			if tc.inHand && req.Sub != "activate.hand" {
				t.Fatalf("precondition: %s %s sub = %q, want activate.hand", tc.name, tc.key, req.Sub)
			}
			c, _ := reg.Lookup(tc.name)
			fa := c.Faces[req.Face]
			i, err := atoiSlot(req.Slot)
			if err != nil {
				t.Fatalf("precondition: %s slot %q: %v", tc.name, req.Slot, err)
			}
			sa := fa.Abilities[i]
			for kind, n := range tc.preCounters {
				// The gate the scenario must satisfy: the printed card does
				// not enter with the counters, so the served setup must seed
				// them; assert the requirement names the gate first.
				if !strings.Contains(sa.Line, "counters_GE"+strconv.Itoa(n)+"_"+kind) {
					t.Fatalf("precondition: %s %s names no %s gate", tc.name, tc.key, kind)
				}
			}
			it, skip := GenerateB(reg, tc.name, req)
			if skip != nil {
				t.Fatalf("%s %s skipped: %s", tc.name, tc.key, skip.Reason)
			}
			if len(it.Scenario.Setup["p0"].Battlefield)+len(it.Scenario.Setup["p0"].Hand)+len(it.Scenario.Setup["p0"].Graveyard) == 0 {
				t.Fatalf("%s %s generated an empty p0 setup", tc.name, tc.key)
			}
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 {
				t.Fatalf("%s %s does not play through gorge: ok=%v fails=%v", tc.name, tc.key, ok, res.Fails)
			}
			if len(res.Snapshots) == 0 {
				t.Fatalf("%s %s produced no snapshots", tc.name, tc.key)
			}
		})
	}
}

func activateRowRequirement(t *testing.T, reg *cards.Registry, name, key string) levelb.Requirement {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %q is not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("precondition: %s has no %s requirement", name, key)
	return levelb.Requirement{}
}

func atoiSlot(slot string) (int, error) { return strconv.Atoi(slot) }
