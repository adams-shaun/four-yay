package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// plotZoneWalk offers Plot for the top card of this player's library only
// when an active PlotZone static explicitly selects it.
func plotZoneWalk(w *legalWalk) {
	e, p := w.e, w.p
	if !w.sorcery {
		return
	}
	library := e.G.Zone(state.ZLibrary, p)
	if len(library) == 0 {
		return
	}
	id := library[0]
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil || o.Face().IsLand() || !plotZoneAllowed(e, id) {
		return
	}
	f := o.Face()
	raw, ok := f.KeywordParam("Plot")
	if !ok {
		raw, ok = e.derivedKeywordParamH(id, kwHeadOf("Plot"))
	}
	if !ok {
		return
	}
	cost := ParseCost(raw)
	if strings.EqualFold(strings.TrimSpace(raw), "CardManaCost") {
		cost = pay.RawBaseCost(asPayer(e), p, id)
	}
	if w.offerCastable(p, id, cost, spellScope("plot"), false) {
		w.out = append(w.out, decision.Option{Index: len(w.out), Kind: "cast",
			Label: "Plot " + f.Name, Obj: id, Mode: "plot"})
	}
}
