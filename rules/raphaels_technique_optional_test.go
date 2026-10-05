package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// TestRaphaelsTechniqueOptionalEmptyHandDrawsSeven resolves the actual linked
// corpus spell ability. WotC's 2026-01-27 ruling says a player may choose to
// discard their hand with zero cards; the affirmative choice must therefore
// feed RememberDiscardingPlayers$ independently of discard events.
func TestRaphaelsTechniqueOptionalEmptyHandDrawsSeven(t *testing.T) {
	reg := searchTestRegistry(t)
	raphael := searchCorpusCard(t, reg, "Raphael's Technique")
	if len(raphael.Faces) != 1 || raphael.Faces[0].Name != "Raphael's Technique" {
		t.Fatalf("precondition: resolved card is not Raphael's Technique: %+v", raphael.Faces)
	}
	var spell *cards.SA
	for _, sa := range raphael.Faces[0].Abilities {
		if sa.API == "Discard" && sa.Params["Mode"] == "Hand" {
			spell = sa
			break
		}
	}
	if spell == nil || spell.Params["Optional"] != "True" || spell.Params["RememberDiscardingPlayers"] != "True" || spell.Sub == nil ||
		spell.Sub.API != "Draw" || spell.Sub.Params["Defined"] != "Remembered" || spell.Sub.Params["NumCards"] != "7" {
		t.Fatalf("precondition: Raphael's Technique lacks its optional whole-hand discard and remembered-player draw-seven chain: %+v", spell)
	}
	if d := raphael.Link(); len(d) != 0 {
		t.Fatalf("link Raphael's Technique: %v", d)
	}

	for _, tc := range []struct {
		name        string
		hand        []string
		vote        string
		wantDraw    int
		wantDiscard bool
	}{
		{name: "empty hand yes remembers and draws", vote: "yes", wantDraw: 7},
		{name: "nonempty hand yes still discards and draws", hand: []string{kr0Creature("Frog")}, vote: "yes", wantDraw: 7, wantDiscard: true},
		{name: "empty hand no neither remembers nor draws", vote: "no"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := kr0EngineCorpus(t, 2)
			source := kr0Place(t, e, 0, raphael, state.ZStack)
			cardsInHand := kr0SetHand(t, e, 0, tc.hand...)
			kr0SetHand(t, e, 1) // The second player's decline must not draw.
			if tc.name == "empty hand no neither remembers nor draws" && len(e.G.Zone(state.ZHand, 0)) != 0 {
				t.Fatalf("precondition: expected seat 0's hand empty, got %d cards", len(e.G.Zone(state.ZHand, 0)))
			}
			if tc.name == "nonempty hand yes still discards and draws" && len(cardsInHand) != 1 {
				t.Fatalf("precondition: expected one test card in hand, got %d", len(cardsInHand))
			}
			for p := state.PlayerID(0); p < 2; p++ {
				kr0SetLibrary(t, e, p, "Name:Draw card\nTypes:Instant\nOracle:x\n", "Name:Draw card 2\nTypes:Instant\nOracle:x\n", "Name:Draw card 3\nTypes:Instant\nOracle:x\n", "Name:Draw card 4\nTypes:Instant\nOracle:x\n", "Name:Draw card 5\nTypes:Instant\nOracle:x\n", "Name:Draw card 6\nTypes:Instant\nOracle:x\n", "Name:Draw card 7\nTypes:Instant\nOracle:x\n")
			}
			if len(e.G.Zone(state.ZHand, 0)) != len(tc.hand) {
				t.Fatalf("precondition: seat 0 hand size = %d, want %d", len(e.G.Zone(state.ZHand, 0)), len(tc.hand))
			}

			d := kr0Run(t, e, spell, func() *effects.Ctx {
				return &effects.Ctx{Source: source, Controller: 0}
			}, nil)
			if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "discard_hand" || d.Player != 0 || len(d.Options) != 2 {
				t.Fatalf("precondition: Raphael's Technique did not offer seat 0 the yes/no hand-discard choice: %+v", d)
			}
			if got := kr0Kind(t, d, tc.vote); got < 0 {
				t.Fatalf("choice %q not offered", tc.vote)
			}
			d = kr0Answer(t, e, kr0Kind(t, d, tc.vote))
			if d == nil || d.Player != 1 || d.ResumeKind != "discard_hand" {
				t.Fatalf("second player did not receive their own hand-discard election: %+v", d)
			}
			d = kr0Answer(t, e, kr0Kind(t, d, "no"))
			if d != nil {
				t.Fatalf("resolution still pending after the second player's decline: %+v", d)
			}

			if len(e.G.Zone(state.ZHand, 0)) != tc.wantDraw {
				t.Fatalf("seat 0 hand after resolution = %d, want %d (remembered-player draw)", len(e.G.Zone(state.ZHand, 0)), tc.wantDraw)
			}
			if len(e.G.Zone(state.ZHand, 1)) != 0 {
				t.Fatalf("seat 1 hand after declining = %d, want 0", len(e.G.Zone(state.ZHand, 1)))
			}
			if tc.wantDiscard && !kr0In(e, state.ZGraveyard, 0, cardsInHand[0]) {
				t.Fatal("affirmative nonempty-hand choice did not discard its card")
			}
			if !tc.wantDiscard && len(cardsInHand) > 0 && !kr0In(e, state.ZHand, 0, cardsInHand[0]) {
				t.Fatal("decline moved the card out of hand")
			}
		})
	}
}
