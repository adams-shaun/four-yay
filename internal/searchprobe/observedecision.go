package searchprobe

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
)

// ObserveDecision introduces the objects the actor's own decision d names --
// its source, then every option's object and attacker, in option order --
// and returns d's observed form, the shape Capture records as
// Frame.Decision. Unlike Capture it projects no board, encodes nothing and
// reads no event burst, so it is cheap enough to call at every searched
// decision of a private engine clone walked past the last Capture
// (internal/azmcts's simulations), where Actions and Match would otherwise
// refuse objects no frame has shown ("action references an unobserved
// object").
//
// The result is c's reusable storage: it is valid until the next
// ObserveDecision on c, and a caller that keeps it copies it.
//
// It mutates c: identities are assigned in introduction order. A caller that
// still needs c's Capture stream -- a searchseat.Feed's collector -- must
// Clone it first, or the feed's next frame would omit these identities.
// Options are read raw, not through view.Project's label rewrite, so the
// returned actions agree with Actions and Match, which read the raw decision
// too. e may be nil only when d names no object (every Source, Obj and
// Attacker zero).
func (c *Collector) ObserveDecision(e *rules.Engine, d *decision.Decision) (*ObservedDecision, error) {
	if d == nil {
		return nil, fmt.Errorf("no decision to observe")
	}
	if d.Player != c.actor {
		return nil, fmt.Errorf("opponent private decision entered observation")
	}
	c.introduce(e, d.Source)
	for i := range d.Options {
		c.introduce(e, d.Options[i].Obj)
		c.introduce(e, d.Options[i].Attacker)
	}
	od, err := c.observeDecisionInto(&c.obsDec, c.obsOpts[:0], d)
	if cap(c.obsDec.Options) > cap(c.obsOpts) {
		c.obsOpts = c.obsDec.Options
	}
	return od, err
}
