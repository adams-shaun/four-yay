package templates

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestActivateTokenCostPrelude pins the token-maker prelude that serves a Sac
// or tapXType cost naming a token no catalogue fixture can place. For each
// case it asserts the maker card reaches p0's hand, a cast step for that maker
// runs before the activate step, the activate step's XMage answers name the
// token the way XMage's permanent picker does ("<Subtype> Token"), and the
// whole scenario replays through gorge. It also asserts the cost is NOT paid
// by a battlefield fixture, so a template that regressed to the fixture table
// (and left the token unmade) fails here rather than passing on a bare board.
func TestActivateTokenCostPrelude(t *testing.T) {
	reg := loadGenRegistry(t)
	cases := []struct {
		name, key, maker, token string
	}{
		{"Greta, Sweettooth Scourge", "activate#0.0", "Gilded Goose", "Food Token"},
		{"Jolene, Plundering Pugilist", "activate#0.0", "Strike It Rich", "Treasure Token"},
		{"Hardened Tactician", "activate#0.0", "Dragon Fodder", "Goblin Token"},
		{"Baylen, the Haymaker", "activate#0.0", "Dragon Fodder", "Goblin Token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			it, req := activateRequirement(t, reg, tc.name, tc.key)
			if !strings.HasPrefix(req.Sub, "activate.") {
				t.Fatalf("precondition: %s %s sub = %q, want an activate family", tc.name, tc.key, req.Sub)
			}
			c, _ := reg.Lookup(tc.name)
			idx, err := strconv.Atoi(req.Slot)
			if err != nil || req.Face < 0 || req.Face >= len(c.Faces) || idx < 0 || idx >= len(c.Faces[req.Face].Abilities) {
				t.Fatalf("precondition: %s slot %q names no ability", tc.name, req.Slot)
			}
			cost := c.Faces[req.Face].Abilities[idx].ParamStr(cards.PKCost)
			if len(activationTokenNeeds(cost)) == 0 {
				t.Fatalf("precondition: %s cost %q carries no token need", tc.name, cost)
			}
			p0 := it.Scenario.Setup["p0"]
			if !contains(p0.Hand, tc.maker) {
				t.Fatalf("token prelude must cast %s from p0's hand: hand=%v", tc.maker, p0.Hand)
			}
			step := activateStepIndex(it.Scenario.Steps)
			seenCast := false
			for i := 0; i < step; i++ {
				if it.Scenario.Steps[i].Op == "cast" && it.Scenario.Steps[i].Card == "p0:"+tc.maker {
					seenCast = true
				}
			}
			if !seenCast {
				t.Fatalf("no cast step for %s before the activate step: %+v", tc.maker, it.Scenario.Steps)
			}
			gotToken := false
			for _, ans := range it.XAnswers[step] {
				for _, part := range strings.Split(ans.Value, "^") {
					if ans.Seat == 0 && ans.Kind == "choice" && answerNamesToken(part, tc.token) {
						gotToken = true
					}
				}
			}
			if !gotToken {
				t.Fatalf("XMage answers for %s name no %q: %+v", tc.name, tc.token, it.XAnswers[step])
			}
			if tc.name == "Baylen, the Haymaker" {
				var queue []string
				for _, ans := range it.XAnswers[step] {
					if ans.Seat == 0 && ans.Kind == "choice" {
						queue = append(queue, ans.Value)
					}
				}
				want := []string{"Goblin Token", "Goblin Token", "White"}
				if strings.Join(queue, "|") != strings.Join(want, "|") {
					t.Fatalf("Baylen FIFO choices = %v, want cost token picks before resolving mana colour %v", queue, want)
				}
			}
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Snapshots) == 0 {
				t.Fatalf("%s token-cost scenario does not replay through gorge", tc.name)
			}
		})
	}
}

// TestActivateNICKNAMEDiscardFromHand pins Mjölnir's channel-style discard:
// NICKNAME is the source's own name, so the activation starts the card in
// p0's hand, needs no extra fixture card, and the card is discarded when the
// cost is paid.
func TestActivateNICKNAMEDiscardFromHand(t *testing.T) {
	const name = "Mjölnir, Hammer of Thor"
	it, hand, graveyard, _ := zoneActivateItem(t, name, "activate#0.0", "activate.hand")
	p0 := it.Scenario.Setup["p0"]
	if !contains(p0.Hand, name) || contains(p0.Battlefield, name) {
		t.Fatalf("setup must hold %s in p0's hand only: hand=%v bf=%v", name, p0.Hand, p0.Battlefield)
	}
	// A Discard<1/NICKNAME> cost brings no fixture discard card of its own.
	if len(p0.Hand) != 1 {
		t.Errorf("a Discard<1/NICKNAME> cost must not add a fixture card: hand=%v", p0.Hand)
	}
	if contains(hand, name) || !contains(graveyard, name) {
		t.Errorf("after activation %s must be discarded: hand=%v graveyard=%v", name, hand, graveyard)
	}
}

// answerNamesToken reports an answer that picks a token named token, either by
// name or by the exact-ref alias an ambiguous same-name pick is spelled with
// ("@p0:token:Goblin Token#2").
func answerNamesToken(answer, token string) bool {
	if answer == token {
		return true
	}
	_, ref, ok := strings.Cut(answer, ":token:")
	if !ok || !strings.HasPrefix(answer, "@") {
		return false
	}
	if i := strings.LastIndexByte(ref, '#'); i >= 0 {
		ref = ref[:i]
	}
	return ref == token
}
