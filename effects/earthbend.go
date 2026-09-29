package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("Earthbend", effEarthbend)
}

// effEarthbend is Forge's EarthbendEffect: "target land you control becomes a
// 0/0 creature with haste that's still a land. Put N +1/+1 counters on it.
// When it dies or is exiled, return it to the battlefield tapped."
//
// It is a composite keyword action, not a primitive of its own: the animation
// is the Animate family's layer-4/6 grant (registerAnimateEffects, same
// per-object registration the DB$ Animate bodies use), the counters are the
// ordinary +1/+1 placement (putCounterSplit), and the return promise is a
// one-shot event-matched delayed trigger.
//
// The implicit target lands in the parser, not here: cards/normalizeImplicitTarget
// writes ValidTgts$ Land.YouCtrl into every Earthbend SA at parseSA (Forge's
// EarthbendEffect declares TargetLandYouControl), so Defined() here names the
// chosen land, never the resolving source. The Num$ count resolves the full
// grammar (literal, SVar name and Count$ body) through the ordinary Num
// reader, so a trigger-relative X (Beifong's TriggeredCard$CardPower) reads
// the resolution's own Ctx.SVars + TriggerContext.
func effEarthbend(h Host, c *Ctx, sa *cards.SA) {
	// The count is resolved ONCE, against the same Ctx every target shares.
	// A negative value clamps to zero: the placement helper treats <=0 as a
	// no-op, and a "0/0 with no counters" land is the CR 704.5f SBA's
	// problem, not this primitive's.
	n := Num(h, c, sa, "Num", 0)
	if n < 0 {
		n = 0
	}
	// The animation, built through the shared Animate grant so the layer
	// assignment, the lifetime and the type-ADD reading of "still a land"
	// can never drift from api:Animate. Power/Toughness are
	// SetPower/SetToughness at layer 7b (the 0/0 base), Haste is a layer-6
	// keyword, and Duration$ Permanent keeps the whole grant alive while
	// the land stays on the battlefield -- exactly the oracle's "that's
	// still a land", no end-of-turn reversion. The grant is NOT
	// Stalking-Stones-forever: endOnLeave ends it the moment the land
	// leaves the battlefield, because the card the return promise brings
	// back is a plain land (CR 122.2 already removed the counters; the
	// animation ended with the permanent instance that carried them).
	ag := animateGrant{
		pw:       0,
		tf:       0,
		hasPower: true, hasTough: true,
		types:      []string{"Creature"},
		kws:        []string{"Haste"},
		duration:   "Permanent",
		permanent:  true,
		endOnLeave: true,
	}
	bent := false
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		o := h.Game().Obj(t.Obj)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		registerAnimateEffects(h, c, o.ID, ag)
		if n > 0 {
			putCounterSplit(h, n, "P1P1", []state.Target{{Obj: o.ID}})
		}
		registerEarthbendReturn(h, c, o.ID)
		bent = true
	}
	if bent {
		h.Emit(events.Event{Kind: events.ElementalBend, Obj: c.Source, Player: c.Controller, Text: "earth"})
	}
}

// registerEarthbendReturn lays the land's "when it dies or is exiled, return
// it to the battlefield tapped" promise. It mints ONE one-shot delayed
// registration naming BOTH destinations in a comma-separated Destination$
// clause; the delayed-trigger path's ChangesZone arm splits that list
// (rules.zoneDelayedDestinationAdmits), so the promise is consumed exactly
// once at its first fire. Registering two single-zone siblings instead is
// the shape that looks safer but is wrong: the DelayedPush consumes only the
// registration whose ID it carries, so the un-fired sibling survives and
// returns the land a SECOND time when it later departs to the other zone.
// One registration cannot have a leftover.
//
// The registration's Source is the land itself, so the builtin body's
// Defined$ Self resolves to it; the registration's Counter names the builtin
// SVar body (cards/builtinSVars' __kwEarthbendReturn, resolved for an object
// with no SVar table of its own), and Text carries the mode and inline
// trigger body the events/apply.go decode splits back apart. The name
// deliberately avoids the __kwDash/__kwWarp/__kwAtEOT prefixes, so no
// incarnation tracking applies: the promise is consumed at its first fire
// and an object that later returns is not acted on again.
func registerEarthbendReturn(h Host, c *Ctx, id state.ObjID) {
	h.Emit(events.Event{
		Kind: events.DelayedRegister, Obj: id,
		Player: c.Controller, Step: h.Game().Step,
		Counter: "__kwEarthbendReturn",
		Text: "ChangesZone:Mode$ ChangesZone | Origin$ Battlefield | " +
			"Destination$ Graveyard,Exile | ValidCard$ Card.Self",
	})
}
