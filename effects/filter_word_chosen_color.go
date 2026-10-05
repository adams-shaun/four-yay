package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
)

func chosenColorMatches(g *state.Game, o *state.Object, sc SpecContext) bool {
	if sc.Source == 0 {
		return false
	}
	src := g.Obj(sc.Source)
	if src == nil {
		return false
	}
	chosen := colourLetter(src.ChosenColor)
	return chosen != 0 && strings.Contains(colorsCtx(o, &sc), string(chosen))
}
