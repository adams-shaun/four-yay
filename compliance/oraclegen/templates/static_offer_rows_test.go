package templates_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// servedStaticOfferRows is the levelb-static-zone-permissions Done-means: every
// appendix row measured on main 613545bd4 that the other-zone observation now
// serves. Twenty-eight are the positive "offered" observation (the probe is
// offered as a cast/play option); two (Heartflame Duelist, Lo and Li) are
// "spells you control have lifelink", which no snapshot carries as a keyword
// and so is observed through the life the granted spell gains its caster.
//
// The table is deliberately the full served set rather than a handful of
// examples: a regression that re-skips any one of them fails here by name, and
// the row's `no verdict for <card>/<key>/vN` measure is what the operator's
// host pass then confirms.
var servedStaticOfferRows = []struct {
	card, key string
	// lifelink marks the two rows served through the granted keyword's
	// consequence instead of an Offered expectation.
	lifelink bool
}{
	// MayPlay$ from the library top, graveyard, exile or hand.
	{"Festival of Embers", "static#0.0", false},
	{"Wickerfolk Indomitable", "static#0.0", false},
	{"Undead Sprinter", "static#0.0", false},
	{"Eirdu, Carrier of Dawn", "static#0.0", false},
	{"Mm'menon, the Right Hand", "static#0.1", false},
	{"Sami, Wildcat Captain", "static#0.0", false},
	{"Weftwalking", "static#0.0", false},
	{"Vizier of the Menagerie", "static#0.1", false},
	{"Noctis, Prince of Lucis", "static#0.0", false},
	{"Traveling Chocobo", "static#0.1", false},
	{"Omnipresence", "static#0.0", false},
	{"Assemble the Players", "static#0.1", false},
	{"Conspiracy Unraveler", "static#0.0", false},
	{"Ironheart, Clever Champion", "static#0.0", false},
	{"Ka-Zar of the Savage Land", "static#0.1", false},
	{"Mole Man, Moloid Master", "static#0.0", false},
	{"Fblthp, Lost on the Range", "static#0.1", false},
	{"Witherbloom, the Balancer", "static#0.0", false},
	{"Zaffai and the Tempests", "static#0.0", false},
	{"Madame Web, Clairvoyant", "static#0.1", false},
	{"Dracogenesis", "static#0.0", false},
	{"Hundred-Battle Veteran", "static#0.1", false},
	{"Hakoda, Selfless Commander", "static#0.1", false},
	{"Iroh, Grand Lotus", "static#0.0", false},
	{"Iroh, Grand Lotus", "static#0.1", false},
	{"Lo and Li, Twin Tutors", "static#0.1", true},
	{"Leonardo, Sewer Samurai", "static#0.0", false},
	{"Mikey & Don, Party Planners", "static#0.1", false},
	{"Heartflame Duelist", "static#0.0", true},
	{"Johann, Apprentice Sorcerer", "static#0.1", false},
}

// TestStaticOfferedRowsGenerateAndReplay locks every served appendix row. It
// is the generator half ("a scenario now exists"); TestStaticZonePermission
// IsOfferedOnlyWithTheSource is the engine half (gorge accepts the assertion
// and the source-removed control rejects it) for its four named cards.
func TestStaticOfferedRowsGenerateAndReplay(t *testing.T) {
	for _, r := range servedStaticOfferRows {
		t.Run(r.card+"/"+r.key, func(t *testing.T) {
			it, skip := zoneItem(t, r.card, r.key)
			if skip != nil {
				t.Fatalf("GenerateB: %s (the other-zone observation regressed to a skip)", skip.Reason)
			}
			if want := r.card + "/" + r.key + "/v1"; it.ID != want {
				t.Fatalf("identity = %q, want the level-B row %q", it.ID, want)
			}
			if it.Template != r.key {
				t.Fatalf("Template = %q, want %q", it.Template, r.key)
			}
			if !r.lifelink {
				// The offered probe must be a real, positive, non-battlefield
				// offer: a probe placed on the battlefield would be observable
				// by staticObserved and would not need the static at all.
				last := it.Steps[len(it.Steps)-1]
				if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Want == nil || !*last.Expect[0].Want {
					t.Fatalf("scenario lacks a positive offered assertion: %+v", last.Expect)
				}
				probe := strings.TrimPrefix(last.Expect[0].Offered.Card, "p0:")
				if probe == last.Expect[0].Offered.Card {
					t.Fatalf("offered assertion names %q, not a p0 probe", last.Expect[0].Offered.Card)
				}
				if !slices.Contains(offeredZoneCards(it.Setup["p0"]), probe) {
					t.Fatalf("precondition: probe %q is not in a p0 non-battlefield zone %+v",
						probe, offeredZoneCards(it.Setup["p0"]))
				}
			}
			// The engine must accept the scenario: the lifelink rows gain life
			// and the offered rows satisfy their Offered expectation.
			if res := runZone(t, it); len(res.Fails) != 0 {
				t.Fatalf("gorge does not satisfy the scenario: %v", res.Fails)
			}
		})
	}
}

// offeredZoneCards is the p0 cards the offer scenario can place a probe in:
// every zone a MayPlay$ AffectedZone$ can name. A probe on the battlefield is
// excluded on purpose -- the assertion is about a card OUTSIDE it.
func offeredZoneCards(seat oraclegen.Seat) []string {
	var out []string
	out = append(out, seat.Graveyard...)
	out = append(out, seat.LibraryTop...)
	out = append(out, seat.Exile...)
	out = append(out, seat.Hand...)
	return out
}

// TestStaticOfferedRowCountPinned pins the cluster size so a change that drops
// rows (or a corpus re-classification) is named here rather than silently
// shrinking the served set. It is the brief's ">= 30 of the 66 appendix rows"
// gate, held to the measured 30.
func TestStaticOfferedRowCountPinned(t *testing.T) {
	if got := len(servedStaticOfferRows); got < 30 {
		t.Fatalf("served other-zone rows = %d, want >= 30 (the brief's gate)", got)
	}
	// A corpus smoke check: every named card is present, so a card rename
	// surfaces as a clear failure rather than a silent skip in the loop above.
	reg := testutil.CorpusRegistry(t)
	for _, r := range servedStaticOfferRows {
		if _, ok := reg.Lookup(r.card); !ok {
			t.Errorf("precondition: %s absent from the corpus", r.card)
		}
	}
}
