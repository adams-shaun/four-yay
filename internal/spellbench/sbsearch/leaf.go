package sbsearch

import (
	"math"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// Leaf evaluators (Config.Leaf).
const (
	// LeafMaterial is searchprobe.LeafValue, the frozen material leaf.
	LeafMaterial = iota
	// LeafFitted is fittedLeaf, a logistic win-probability model fitted to
	// sb-tactical mirror outcomes.
	LeafFitted
)

// fittedLeaf is the searching seat's win probability at a rollout's cut
// point: a logistic model over public, seat-visible board facts (both life
// totals, hand and library SIZES, battlefields), fitted by maximum
// likelihood (L2 1.0) to the eventual winner of 1,500 sb-tactical mirror
// games over the pauper-kernel benchmark pool (8 decks, rows at each turn's
// first decision, 50,020 samples). Five-fold game-grouped CV log loss is
// 0.559 against 0.611 for searchprobe.LeafValue's score refitted to the
// same data (0.661 at LeafValue's own scale); it is the better predictor on
// every deck. Every term is a me-minus-opponent difference:
//
//   - life, capped at 30 (a lifegain loop's 300 life is no better than 30),
//     plus exp(-life/4) for the danger of a low total;
//   - hand size, lands, other noncreature permanents, creatures, total
//     creature power and flying power;
//   - the clock: power over the opponent's life, capped at 2;
//   - exp(-library/3), the decking danger;
//   - +-1 for whose turn it is.
//
// The weights carry no card names; the model is deck-agnostic by
// construction.
func fittedLeaf(v view.View, actor state.PlayerID) float64 {
	if v.Over {
		switch {
		case v.Draw:
			return 0.5
		case v.Winner != nil && *v.Winner == actor:
			return 1
		}
		return 0
	}
	type side struct {
		life, hand, lib, lands, other, ncre, pow, fly float64
	}
	var me, op side
	for i := range v.Players {
		p := &v.Players[i]
		s := side{life: float64(p.Life), hand: float64(p.HandSize), lib: float64(p.LibrarySize)}
		for j := range p.Battlefield {
			c := &p.Battlefield[j]
			switch {
			case strings.Contains(c.Types, "Land"):
				s.lands++
			case strings.Contains(c.Types, "Creature"):
				s.ncre++
				pw := float64(max(c.Power, 0))
				s.pow += pw
				for _, k := range c.Keywords {
					if strings.EqualFold(cards.KeywordHead(k), "flying") {
						s.fly += pw
						break
					}
				}
			default:
				s.other++
			}
		}
		if p.ID == actor {
			me = s
		} else {
			op = s
		}
	}
	lowLife := func(l float64) float64 { return math.Exp(-l / 4) }
	lowLib := func(n float64) float64 { return math.Exp(-n / 3) }
	clock := func(pow, life float64) float64 { return min(pow/max(life, 1), 2) }
	active := -1.0
	if v.Active == actor {
		active = 1
	}
	z := 0.0455*(min(me.life, 30)-min(op.life, 30)) +
		-0.7812*(lowLife(me.life)-lowLife(op.life)) +
		0.2107*(me.hand-op.hand) +
		0.2074*(me.lands-op.lands) +
		0.0910*(me.other-op.other) +
		0.2368*(me.ncre-op.ncre) +
		0.0532*(me.pow-op.pow) +
		0.0612*(me.fly-op.fly) +
		1.2712*(clock(me.pow, op.life)-clock(op.pow, me.life)) +
		-7.7601*(lowLib(me.lib)-lowLib(op.lib)) +
		0.2594*active
	return 1 / (1 + math.Exp(-z))
}
