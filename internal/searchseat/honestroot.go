package searchseat

import (
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
)

// HonestRoot builds one honest root engine for a seat that needs an engine
// handle at its own decision: a single searchprobe.Redealer deal of the live
// position, taken from the seat's own observation feed, so the only engine a
// hosted bot ever touches has its hidden cards redealt from the pool the
// seat's observation can derive.
//
// setup is the declared public game (its deck lists and tokens), f the
// actor's feed captured through this very decision (f.History()'s last frame
// must be e's current boundary, which the feed guarantees), and e the live
// engine -- read for public state, zone sizes and the hidden objects to
// permute, never for which hidden card is where. seed is the deal seed (the
// host derives it per decision, bots.RootSeed).
//
// The returned engine is a hypothetical clone, owned by the caller for the
// duration of the decision and sharing nothing mutable with e. A refused or
// failed deal returns nil plus the reason; the caller plays its fallback and
// counts the refusal -- never a clairvoyant fallback.
func HonestRoot(setup searchprobe.PublicGame, f *Feed, e *rules.Engine, seed [2]uint64) (*rules.Engine, string) {
	if f == nil || !f.Live() || f.Frames() == 0 {
		return nil, "no live observation feed"
	}
	known, err := f.Known()
	if err != nil {
		return nil, "known-card projection: " + err.Error()
	}
	r, reason := searchprobe.NewRedealer(setup, f.History(), known, searchprobe.RedealBase{Engine: e, Observer: f.Collector()})
	if reason != "" {
		return nil, reason
	}
	return r.Deal(seed, nil)
}
