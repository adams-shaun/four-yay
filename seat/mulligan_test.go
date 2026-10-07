package seat

import (
	"context"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// TestLandMulliganOnRealRound drives real London rounds (Config.Mulligans 2,
// SampleDecks' 17-land 40-card decks) with EnableLandMulligan bots and checks
// every KMulligan answer against the rule computed from the engine's own
// hand and mulligan count: both adapter halves (the View half, Decide, and
// the game half, DecideBoard over BoardFromGame) give the same answer, the
// keep/mulligan answer is the land table's, and a bottoming of m cards keeps
// lands - min(max(lands - cap, 0), m) lands, cap = max(ceil(K/2), min(2, K))
// for K kept cards, and never fewer than min(2, lands). It also pins the prompt the
// rule reads the mulligan count from (rules/mulligan.go's
// keepMulliganPrompt), which is the policy's only source for it.
func TestLandMulliganOnRealRound(t *testing.T) {
	names, decks := testutil.SampleDecks(t, 2)
	ctx := context.Background()
	mulls, keeps, bottoms := 0, 0, 0
	for seed := uint64(0); seed < 60; seed++ {
		e := rules.New(rules.Config{Seed: seed, Names: names, Decks: decks, Mulligans: 2})
		e.Advance()
		bots := []*Bot{NewBot(seed).EnableLandMulligan(), NewBot(seed ^ 7).EnableLandMulligan()}
		taken := map[state.PlayerID]int{}
		for e.Pending() != nil && e.Pending().Kind == decision.KMulligan {
			d := e.Pending()
			b := bots[d.Player]
			viewIn, err := b.Decide(ctx, view.Project(e.G, e, d.Player, d), *d)
			if err != nil {
				t.Fatal(err)
			}
			boardIn, err := b.DecideBoard(ctx, botpolicy.BoardFromGame(e.G, e, d.Player), *d)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(viewIn.Choices, boardIn.Choices) {
				t.Fatalf("seed %d seat %d: view half %v, game half %v", seed, d.Player, viewIn.Choices, boardIn.Choices)
			}
			hand := e.G.Zone(state.ZHand, d.Player)
			lands := 0
			for _, id := range hand {
				if e.G.Obj(id).Face().IsLand() {
					lands++
				}
			}
			chosen := d.Chosen(viewIn)
			if d.Options[0].Kind == "bottom" {
				bottoms++
				kept := lands
				for _, o := range chosen {
					if e.G.Obj(o.Obj).Face().IsLand() {
						kept--
					}
				}
				m := len(chosen)
				maxLands := max((len(hand)-m+1)/2, min(2, len(hand)-m))
				if want := lands - min(max(lands-maxLands, 0), m); kept != want || kept < min(2, lands) {
					t.Errorf("seed %d seat %d: bottoming %d of %d (%d lands) kept %d lands, want %d",
						seed, d.Player, m, len(hand), lands, kept, want)
				}
			} else {
				canMull := len(d.Options) > 1
				k := len(hand) - taken[d.Player] // two players: no free mulligan
				wantKeep := true
				switch {
				case !canMull || k <= 4:
				case k >= 6:
					wantKeep = lands >= 2 && lands <= 5
				default:
					wantKeep = lands >= 1 && lands < len(hand)
				}
				if gotKeep := chosen[0].Kind == "keep"; gotKeep != wantKeep {
					t.Errorf("seed %d seat %d: %d lands after %d mulligans: keep = %v, want %v (prompt %q)",
						seed, d.Player, lands, taken[d.Player], gotKeep, wantKeep, d.Prompt)
				}
				if chosen[0].Kind == "mulligan" {
					taken[d.Player]++
					mulls++
				} else {
					keeps++
				}
			}
			if err := e.Submit(viewIn); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Logf("%d mulligans, %d keeps, %d bottomings", mulls, keeps, bottoms)
	if mulls == 0 || keeps == 0 || bottoms == 0 {
		t.Fatalf("vacuous: %d mulligans, %d keeps, %d bottomings", mulls, keeps, bottoms)
	}
}

// The zero rule is the default bot: a WithMulligan(MulliganCoin) bot draws
// the same coin as NewBot over a keep/mulligan ask.
func TestWithMulliganCoinIsTheDefault(t *testing.T) {
	d := decision.Decision{Seq: 5, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "keep"}, {Index: 1, Kind: "mulligan"}}}
	for seed := uint64(0); seed < 30; seed++ {
		a, _ := NewBot(seed).DecideBoard(context.Background(), botpolicy.Board{}, d)
		b, _ := NewBot(seed).WithMulligan(botpolicy.MulliganCoin).DecideBoard(context.Background(), botpolicy.Board{}, d)
		if !slices.Equal(a.Choices, b.Choices) {
			t.Fatalf("seed %d: default %v, coin %v", seed, a.Choices, b.Choices)
		}
	}
}
