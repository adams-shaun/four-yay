package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// plotCastMode is the cast_mode of the Plot special action: gorge offers it as
// a cast option priced at the card's Plot cost, and the driver replays it as
// XMage's PlotAbility activation (castModeSupported, ScenarioReplay).
const plotCastMode = "plot"

// plotCostProbes derives probes for a "plotting cards from your hand cost
// less" static (ValidSpell$ Static.Plotting). The probe is a plot action on a
// card in p0's hand whose Plot cost has the reduction's generic mana and
// something left over; the reduced price is paid on the plot action itself.
// XAbility at that step carries the braced "Plot {cost}" rule text XMage
// activates it by.
func plotCostProbes(reg *cards.Registry, name string, reduction int, base costProbe) []costProbe {
	var cands []costCandidate
	pay, full, text := map[*cards.Face]string{}, map[*cards.Face]string{}, map[*cards.Face]string{}
	for _, card := range reg.AllCards() {
		if len(card.Faces) != 1 || card.Faces[0] == nil {
			continue
		}
		face := card.Faces[0]
		if face.Name == name || !probeNameUsable(face.Name) {
			continue
		}
		cost, ok := face.KeywordCostParam("Plot")
		if !ok || cost == "" {
			continue
		}
		pool, why := oraclegen.PoolFor(cost)
		if why != "" {
			continue
		}
		mana, ok := removeGenericMana(pool, reduction)
		if !ok || mana == "" {
			continue
		}
		pay[face], full[face], text[face] = mana, pool, "Plot "+xmageManaText(cost)
		cands = append(cands, costCandidate{face: face, key: [4]int{costFaceRank(face), costFaceComplexity(face), len(cost), 0}})
	}
	var out []costProbe
	for _, face := range rankCostCandidates(cands) {
		p := base
		p.spell, p.mana, p.full = face.Name, pay[face], full[face]
		p.castMode, p.plotText = plotCastMode, text[face]
		out = append(out, p)
	}
	return out
}

// plotStepIndex is the index of the plot cast step in an item's steps, or -1.
func plotStepIndex(steps []oraclegen.Step) int {
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].Op == "cast" && strings.EqualFold(steps[i].CastMode, plotCastMode) {
			return i
		}
	}
	return -1
}
