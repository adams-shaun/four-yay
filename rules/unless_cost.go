package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// unless_cost.go is the UnlessCost$ payment entry (payUnlessCost): pricing an
// "unless [player] pays" cost and paying or posing it, plus the draw-cost
// payer read.

// payUnlessCost charges the non-choice subset of a mid-resolution
// UnlessCost$ to payer p. Sacrifice, discard, reveal, return and exile
// components are deliberately refused here: beginUnlessPayment owns every
// such component and gathers the payer's selected objects before it calls
// payMana. Keeping this guard makes a future caller unable to silently
// revive the old first-in-zone-order stand-in. Fixed mana/life, Mill,
// SubCounter and Draw
// components remain synchronous: a Draw<N/Spec> pays by drawing N cards for
// the player(s) the spec names (default the payer), resolved through the
// same Ctx roles the UnlessPayer$ grammar reads, and a Mill<N> mills from
// the top of the payer's own library through the shared payMillCost (CR
// 701.13a: every remaining card when fewer than N remain, so any library
// size is payable). The dynamic life folds
// (LifeTotalHalfUp, an announced PayLife<X>) and the energy parts (fixed and
// announced-X PayEnergy) charge here too, under the same offer gate's reads
// (unlessFoldDynamic / unlessEnergyAffordable), so the gate and the charge
// can never disagree.
func (e *Engine) payUnlessCost(p state.PlayerID, cost Cost, ctx *effects.Ctx, stackObj state.ObjID) bool {
	if len(cost.Sac) != 0 || len(cost.Discard) != 0 || len(cost.Reveal) != 0 || len(cost.Behold) != 0 || len(cost.RevealOrChoose) != 0 || len(cost.RevealChosen) != 0 || len(cost.Return) != 0 || len(cost.Exile) != 0 {
		return false
	}
	if int(p) < 0 || int(p) >= len(e.G.Players) {
		return false
	}
	folded, ok := e.unlessFoldDynamic(p, cost, ctx)
	if !ok {
		return false
	}
	cost = folded
	if !e.unlessEnergyAffordable(p, cost, ctx) {
		return false
	}
	g := e.G
	// The source the SubCounter parts drain is the activated ability's host
	// when this is an ability object, otherwise the resolving source.
	src := ctx.Source
	if o := g.Obj(stackObj); o != nil && o.Ability != nil {
		src = o.Source
	}
	type counterDrain struct {
		obj  state.ObjID
		kind string
		n    int32
	}
	var drains []counterDrain
	for _, part := range cost.SubCounter {
		o := g.Obj(src)
		if o == nil {
			return false
		}
		have := int32(0)
		for _, ct := range o.Counters {
			if ct.Kind == part.Spec {
				have += ct.N
			}
		}
		if have < part.N {
			return false
		}
		drains = append(drains, counterDrain{obj: o.ID, kind: part.Spec, n: part.N})
	}
	// Resolve every drawer before charging mana/life. A Draw component whose
	// role is unavailable makes the entire cost unpayable; validating first
	// avoids a partial payment followed by a silent omitted draw.
	drawers := make([][]state.PlayerID, len(cost.Draw))
	for i, part := range cost.Draw {
		players, ok := unlessDrawPlayers(ctx, p, part.Spec)
		if !ok {
			return false
		}
		for _, dp := range players {
			if int(dp) < 0 || int(dp) >= len(g.Players) {
				return false
			}
		}
		drawers[i] = players
	}
	// Everything is affordable: charge mana/life through ordinary events,
	// then apply the synchronous counter components, then the draws. The
	// resolving object is the payment subject, so its ManaConvert statics
	// (including EffectZone$ Command and Effect-delivered grants) apply here
	// under the same conversion read used by cast offers.
	if !e.payManaConv(p, cost, e.paymentConv(p, stackObj, false)) {
		return false
	}
	// The energy parts charge through the ONE shared site (CR 118.2d); the
	// offer gate proved the total affordable and the fold above proved every
	// dynamic part bound, so the charge cannot half-apply.
	x := int32(0)
	if ctx != nil && ctx.XAnnounced {
		x = ctx.X
	}
	e.chargeEnergyCost(p, cost, x)
	// Mill parts (Mill<N>) settle through the ONE shared mill site, after
	// every payability check above has passed and beside the other charges,
	// so the ordinary cast/activation cost and an unless cost cannot diverge.
	// CR 701.13a: the payer mills the SUM of the parts' requirements, taking
	// every remaining card when the library is short, so this never turns an
	// empty or short library into an unpayable cost.
	e.payMillCost(p, cost.Mill)
	for _, d := range drains {
		e.emit(events.Event{Kind: events.CounterChange, Obj: d.obj, Counter: d.kind, Amount: -d.n})
	}
	for i, part := range cost.Draw {
		for _, dp := range drawers[i] {
			for n := int32(0); n < part.N; n++ {
				effects.DrawFor(e, dp)
			}
		}
	}
	return true
}

// unlessDrawPlayers resolves a Draw<N/Spec> cost component's drawer(s). The
// empty spec and "You" are the payer; every other spelling is one of the
// player roles the unless-payment context carries, and an unresolvable or
// unknown spec fails closed (the cost was not paid).
func unlessDrawPlayers(ctx *effects.Ctx, payer state.PlayerID, spec string) ([]state.PlayerID, bool) {
	one := func(t state.Target) ([]state.PlayerID, bool) {
		if t.IsPlayer {
			return []state.PlayerID{t.Player}, true
		}
		return nil, false
	}
	switch unlessDrawPlayers5231Codes.Code(string(spec)) {
	case unlessDrawPlayers5231Empty:
		return []state.PlayerID{payer}, true
	case unlessDrawPlayers5231PlayerTargetedBy:
		if len(ctx.Targets) == 0 {
			return nil, false
		}
		return one(ctx.Targets[0])
	case unlessDrawPlayers5231PlayerActivator:
		return one(ctx.TriggerActivator)
	case unlessDrawPlayers5231PlayerTriggeredPlayer:
		return one(ctx.TriggerPlayer)
	case unlessDrawPlayers5231PlayerTriggeredTarget:
		return one(ctx.TriggerTarget)
	}
	return nil, false
}

const (
	unlessDrawPlayers5231Empty                 uint16 = 1 // "", "You", "Player", "Self"
	unlessDrawPlayers5231PlayerTargetedBy      uint16 = 2 // "Player.targetedBy", "Targeted", "TargetedPlayer"
	unlessDrawPlayers5231PlayerActivator       uint16 = 3 // "Player.Activator", "TriggeredActivator"
	unlessDrawPlayers5231PlayerTriggeredPlayer uint16 = 4 // "Player.TriggeredPlayer", "TriggeredPlayer"
	unlessDrawPlayers5231PlayerTriggeredTarget uint16 = 5 // "Player.TriggeredTarget", "TriggeredTarget"
)

var unlessDrawPlayers5231Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "", Val: unlessDrawPlayers5231Empty},
	state.StrEntry[uint16]{Key: "You", Val: unlessDrawPlayers5231Empty},
	state.StrEntry[uint16]{Key: "Player", Val: unlessDrawPlayers5231Empty},
	state.StrEntry[uint16]{Key: "Self", Val: unlessDrawPlayers5231Empty},
	state.StrEntry[uint16]{Key: "Player.targetedBy", Val: unlessDrawPlayers5231PlayerTargetedBy},
	state.StrEntry[uint16]{Key: "Targeted", Val: unlessDrawPlayers5231PlayerTargetedBy},
	state.StrEntry[uint16]{Key: "TargetedPlayer", Val: unlessDrawPlayers5231PlayerTargetedBy},
	state.StrEntry[uint16]{Key: "Player.Activator", Val: unlessDrawPlayers5231PlayerActivator},
	state.StrEntry[uint16]{Key: "TriggeredActivator", Val: unlessDrawPlayers5231PlayerActivator},
	state.StrEntry[uint16]{Key: "Player.TriggeredPlayer", Val: unlessDrawPlayers5231PlayerTriggeredPlayer},
	state.StrEntry[uint16]{Key: "TriggeredPlayer", Val: unlessDrawPlayers5231PlayerTriggeredPlayer},
	state.StrEntry[uint16]{Key: "Player.TriggeredTarget", Val: unlessDrawPlayers5231PlayerTriggeredTarget},
	state.StrEntry[uint16]{Key: "TriggeredTarget", Val: unlessDrawPlayers5231PlayerTriggeredTarget},
)
