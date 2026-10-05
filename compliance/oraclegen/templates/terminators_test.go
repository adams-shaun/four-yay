package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestGeneratedTerminatorsAndOptionalCosts is the end-to-end ratchet for the
// std3 answer fixes: each card below is generated through the real template
// and its xmage_answers are inspected, so a regression in xanswers or in
// castWith fails here rather than only in an XMage replay. Every assertion
// also checks the card produced a scenario (a skip would make the loop
// vacuous).
func TestGeneratedTerminatorsAndOptionalCosts(t *testing.T) {
	reg := loadGenRegistry(t)
	tests := []struct {
		card  string
		check func(t *testing.T, it oraclegen.Item)
	}{
		{
			// OTJ Spree: a mode ask with one affordable mode still needs the
			// [mode_skip] terminator XMage's chooseMode waits for.
			card: "Caught in the Crossfire",
			check: func(t *testing.T, it oraclegen.Item) {
				if !hasAnswer(it.XAnswers, 0, "mode", "[mode_skip]") {
					t.Fatalf("mode_skip missing: %+v", it.XAnswers)
				}
			},
		},
		{
			// TMT Armaggon: "destroy up to three" with fewer picks needs the
			// [target_skip] terminator.
			card: "Armaggon, Future Shark",
			check: func(t *testing.T, it oraclegen.Item) {
				if !hasAnswer(it.XAnswers, 1, "target", "[target_skip]") {
					t.Fatalf("target_skip missing: %+v", it.XAnswers)
				}
			},
		},
		{
			// LCI Kitesail Larcenist: TargetsForEachPlayer$ answered per seat
			// in seat order, seat 0 skipping before seat 1's pick.
			card: "Kitesail Larcenist",
			check: func(t *testing.T, it oraclegen.Item) {
				as := stepAnswers(it.XAnswers, 1)
				if len(as) < 2 || as[0].Kind != "target" || as[0].Value != "[target_skip]" || as[1].Value == "[target_skip]" {
					t.Fatalf("per-player order wrong: %+v", it.XAnswers)
				}
			},
		},
		{
			// TDM Rakshasa's Bargain: one Dig decision's two card picks are
			// ONE '^'-joined definition.
			card: "Rakshasa's Bargain",
			check: func(t *testing.T, it oraclegen.Item) {
				if !hasJoined(it.XAnswers, 1, "Wastes^Wastes") {
					t.Fatalf("joined dig pick missing: %+v", it.XAnswers)
				}
			},
		},
		{
			// BLB Offspring: XMage asks to pay the additional cost before the
			// cast resolves; the declined offer is answered "no".
			card: "Bushy Bodyguard",
			check: func(t *testing.T, it oraclegen.Item) {
				if !hasAnswer(it.XAnswers, 0, "choice", "no") {
					t.Fatalf("cast-step no missing: %+v", it.XAnswers)
				}
			},
		},
		{
			// HOB Kicker {2}{W}{W}: the cast fixture has too little
			// white mana to pay this optional cost, so XMage does not ask.
			card: "The Eagles Are Coming!",
			check: func(t *testing.T, it oraclegen.Item) {
				assertUnpayableCastCost(t, reg, it, "2 W W")
				if hasAnswer(it.XAnswers, 0, "choice", "no") {
					t.Fatalf("unpayable kicker queued a decline: %+v", it.XAnswers)
				}
			},
		},
		{
			// TLA waterbend {4}: the base-cost fixture cannot pay four
			// additional mana, so no decline belongs on this cast step.
			card: "Ruinous Waterbending",
			check: func(t *testing.T, it oraclegen.Item) {
				assertUnpayableCastCost(t, reg, it, "4")
				if hasAnswer(it.XAnswers, 0, "choice", "no") {
					t.Fatalf("unpayable waterbend queued a decline: %+v", it.XAnswers)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.card, func(t *testing.T) {
			it, skip := Generate(reg, tt.card)
			if skip != nil {
				t.Fatalf("no scenario (vacuous test): %s", skip.Reason)
			}
			if len(it.Steps) == 0 {
				t.Fatal("scenario has no steps")
			}
			tt.check(t, it)
		})
	}
}

func assertUnpayableCastCost(t *testing.T, reg *cards.Registry, it oraclegen.Item, cost string) {
	t.Helper()
	card, ok := reg.Lookup(it.Card)
	if !ok || len(card.Faces) == 0 {
		t.Fatalf("missing card face for %s", it.Card)
	}
	found := false
	for _, c := range oraclegen.CastOptionalCosts(card.Faces[0]) {
		if c == cost {
			found = true
		}
	}
	if !found || len(it.Steps) == 0 || it.Steps[0].Op != "cast" || it.Steps[0].Card != "p0:"+it.Card {
		t.Fatalf("optional cost %q / cast step missing for %s: %+v", cost, it.Card, it.Steps)
	}
	if got := oraclegen.OptionalCostCastNo(card.Faces[0], it.Steps[0].Mana); got != 0 {
		t.Fatalf("cost %q was payable from pool %q (%d asks)", cost, it.Steps[0].Mana, got)
	}
}

func stepAnswers(xans [][]oraclegen.XAnswer, step int) []oraclegen.XAnswer {
	if step < 0 || step >= len(xans) {
		return nil
	}
	return xans[step]
}

func hasAnswer(xans [][]oraclegen.XAnswer, step int, kind, value string) bool {
	for _, a := range stepAnswers(xans, step) {
		if a.Kind == kind && a.Value == value {
			return true
		}
	}
	return false
}

func hasJoined(xans [][]oraclegen.XAnswer, step int, joined string) bool {
	for _, a := range stepAnswers(xans, step) {
		if a.Kind == "choice" && strings.Contains(a.Value, "^") && a.Value == joined {
			return true
		}
	}
	return false
}
