package effects

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("SacrificeAll", effSacrificeAll) }

// effSacrificeAll implements the mass-sacrifice primitive (CR 701.16). With a
// Defined$ object target it sacrifices those battlefield objects; otherwise
// every player sacrifices every permanent matching ValidCards$ (default
// Permanent). RememberSacrificed$ preserves LKI for a following sub-ability.
//
// UnlessCost$ ("sacrifice it unless you pay ...") is owned entirely by the
// shared unless gate in Resolve (unlessProceed): it poses the pay ask to the
// UnlessPayer$, rules' unless_pay arm charges the cost, and a paid answer
// skips this body. This body therefore NEVER reads Ctx.UnlessPay. It used to
// pose a second ask of its own; the gate had already consumed and cleared
// the answer by the time the body ran, so the body's ask was re-posed on
// every answered re-entry (the Flash / Slow Motion livelock the cardfuzz
// run found: decision_ask modes -> decision_made -> mode_chosen forever).
func effSacrificeAll(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	spec := sa.ParamStr(cards.PKValidCards)
	if spec == "" {
		spec = "Permanent"
	}
	remember := sa.ParamStr(cards.PKRememberSacrificed) != ""
	sacrifice := func(id state.ObjID) {
		if h.SacrificeBlocked(id, false) {
			// A CantSacrifice restriction (Call for Aid) or face static: the
			// permanent stays. Not remembered either — it was not sacrificed.
			return
		}
		o := g.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			return
		}
		if remember {
			c.Sacrificed = append(c.Sacrificed, SacrificedLKI(h, id))
			// Forge's RememberSacrificed$ also remembers the card (mirroring
			// effSacrifice's rememberLKICapture), which is what a following
			// ConditionDefined$ Remembered, Remembered$Amount or
			// RememberedCard reads, and event-backs it on the source so a
			// later, independently resolving ability sees the same list.
			c.Remembered = append(copyTargets(c.Remembered), state.Target{Obj: id})
			eventRemember(h, c, id)
		}
		h.Emit(events.Sacrifice(id))
	}
	// A SacrificeAll is one simultaneous event (CR 701.21, 603.10a, 614.6):
	// the batch window makes every member's replacements and look-back
	// read the pre-sacrifice board, so a Kalitas sacrificed alongside an
	// opponent's creatures still exiles them.
	if DefinedRefOf(sa).Raw != "" || TargetsOf(sa).Targeted() {
		h.BatchDepartures(nil)
		defer h.EndBatchDepartures()
		for _, t := range Defined(h, c, sa) {
			if !t.IsPlayer {
				// A Defined$-named object that is no longer on the battlefield
				// is skipped LOUDLY rather than silently: this is the shape
				// Mode$ Unattached's TriggeredObjectLKICopy referent reaches on
				// the bearer-left path (Grafted Exoskeleton's "sacrifice that
				// permanent"), where the named permanent has already left the
				// battlefield and nothing may be sacrificed in its place. The
				// same one-Note-per-object convention effRemoveCounter carries;
				// the sweep branch below stays quiet (it names no specific
				// object, so a skipped one is not a surprise).
				if o := g.Obj(t.Obj); o == nil {
					h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
						Text: fmt.Sprintf("SacrificeAll target %d no longer exists; skipped", t.Obj)})
					continue
				} else if o.Zone != state.ZBattlefield {
					h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
						Text: fmt.Sprintf("SacrificeAll target %d is not on the battlefield (zone %s); skipped", o.ID, o.Zone)})
					continue
				}
				sacrifice(t.Obj)
			}
		}
		return
	}
	// Victims are chosen against the pre-sacrifice board, then moved as one
	// batch.
	var victims []state.ObjID
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
				victims = append(victims, id)
			}
		}
	}
	if len(victims) == 0 {
		return
	}
	h.BatchDepartures(victims)
	defer h.EndBatchDepartures()
	for _, id := range victims {
		sacrifice(id)
	}
}
