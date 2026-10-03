// resolution_ask.go holds the ask side of the mid-resolution mechanism: Engine.Ask, its resume-point capture (buildAskResume), the Cxt-scoped resolution state setters and the departing-target snapshots.

// Code moved verbatim out of rules/resolution.go (moving code only; the
// suspension/resumption mechanism is documented at the top of resolution.go).
package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Ask implements effects.Host.Ask. Every effect ask is answered through
// TapeAnswer (effects.AskTape) inside a tape run, so what reaches this method
// is an ask no run serves: a resolution the ask-free predicate wrongly
// exempted, or an engine driven with no run at all. It is refused with one
// Note (the R-9 no-host degrade: the asking primitive takes its deterministic
// default) and counted by the legacy-ask census. The one ask this engine
// still poses itself is an off-stack mana ability's colour choice, which is
// the activation's own flow (askOffStackMana).
func (e *Engine) Ask(d *decision.Decision) bool {
	if e.askOffStackMana(d) {
		return true
	}
	tapeLegacyAsked(e, d, false)
	e.emit(events.Event{Kind: events.Note, Player: d.Player,
		Text: "ask answered with its default: no resolution run serves it (" + string(d.Kind) + "/" + d.ResumeKind + ")"})
	return false
}

// AskCount implements effects' optional askSeam: the number of
// mid-resolution asks this engine has taken (posed or deferred). effects.
// Resolve's UnlessCost$ gate compares it across the gate, because a gate ask
// deferred behind an already-suspended resolution leaves Suspended()
// unchanged. Engine scratch, never logged.
func (e *Engine) AskCount() uint64 { return e.askCount }

// EventMark implements effects' optional eventMarker seam: the event log's
// current length, a mark StateChangedSince compares against.
func (e *Engine) EventMark() int { return len(e.L.Events) }

// StateChangedSince implements effects' optional eventMarker seam: whether
// any event other than a Note was logged after mark. A Note is the log's
// commentary and folds into no game state (events.Apply), so a span holding
// only Notes left the game exactly as it found it. A ZERO-amount Damage
// event is the same kind of no-op: CR 120.8 says 0 damage is never dealt,
// and the fold marks nothing for it (a DamageAll whose NumDmg$ counts an
// empty Remembered set -- Kindle the Carnage repeated over an empty hand
// logs one per creature per pass, and counting it as progress let a bot's
// "Repeat" answer loop forever, cardfuzz batch8 line 1). A ZERO-amount
// LifeChange likewise folds into nothing (events.Apply adds 0 to the life
// total; CR 119.9's "gains 0 life" is no life-gain event, and a 0 loss the
// same): Ad Nauseam repeated over an empty library loses life equal to the
// mana value of no card. AFLifeLost is always published, even for that zero
// loss: a StoreSVar only changes state when it replaces a different value.
func (e *Engine) StateChangedSince(mark int) bool {
	if mark < 0 {
		mark = 0
	}
	for i := mark; i < len(e.L.Events); i++ {
		ev := e.L.Events[i]
		if ev.Kind == events.Note || (ev.Kind == events.Damage && ev.Amount == 0) ||
			(ev.Kind == events.LifeChange && ev.Amount == 0) {
			continue
		}
		if ev.Kind == events.StoreSVar {
			// Read the value immediately before this event, not the final
			// object value: intermediate changes still count as progress.
			unchanged := false
			for j := i - 1; j >= 0; j-- {
				prev := e.L.Events[j]
				// CR 400.7 resets RuntimeSVars on a zone change: an
				// earlier write to that ObjID is no longer its old value.
				if prev.Obj == ev.Obj && (prev.Kind == events.MoveZone || prev.Kind == events.Draw || prev.Kind == events.PutOnStack) {
					break
				}
				if prev.Kind == events.StoreSVar && prev.Obj == ev.Obj && prev.Text == ev.Text {
					unchanged = prev.Amount == ev.Amount
					break
				}
			}
			if unchanged {
				continue
			}
		}
		return true
	}
	return false
}

// Suspended implements effects.Host.Suspended: the resolution is suspended
// when a mid-resolution ask set e.resume and the answer has not yet arrived
// to clear it. effects.Resolve checks this after every sub-ability so that a
// suspended ask stops the SubAbility chain instead of running what sits
// beneath it (B1). It is the rules-side half of the pairing with Ask: Ask
// sets e.resume, and handleModes clears it the moment the answer lands, so
// the resume pass re-enters the chain with nothing suspended and walks the
// rest of it exactly once.
//
// SetResolutionFlipMemory implements effects' flipMemoryHost (an optional
// seam, so the effects test doubles need no method): effects.Resolve publishes
// the resolving chain's shared coin-flip memory around the whole walk and
// restores the enclosing value on return, and effFlipCoin re-publishes when it
// lazily allocates that memory. Returns the previous value for the defer
// restore. Engine-transient scratch like resolvingTargetControllerLKI.
func (e *Engine) SetResolutionFlipMemory(m *effects.FlipMemory) *effects.FlipMemory {
	prev := e.resolvingFlipMemory
	e.resolvingFlipMemory = m
	return prev
}

// SetResolutionExchangeMemory implements effects' exchangeMemoryHost (an
// optional seam, so the effects test doubles need no method): effects.Resolve
// publishes the resolving chain's shared ExchangeLife rider memory around the
// whole walk and restores the enclosing value on return, and effExchangeLife
// re-publishes when it lazily allocates that memory. Returns the previous
// value for the defer restore. Engine-transient scratch like
// resolvingFlipMemory, never a writer of the resume state the archtest guards.
func (e *Engine) SetResolutionExchangeMemory(m *effects.ExchangeMemory) *effects.ExchangeMemory {
	prev := e.resolvingExchangeMemory
	e.resolvingExchangeMemory = m
	return prev
}

// SetResolutionTargetControllerLKI implements
// effects.Host.SetResolutionTargetControllerLKI: effects.Resolve publishes
// the target-controller snapshot of the chain it is about to walk, and
// restores the previous value on return, so the scratch holds exactly the
// innermost running chain's map. Engine.Ask consumes it onto the pending
// resumePoint. Writes engine scratch, never e.resume, so it is not a writer
// of the resume state the archtest guards.
func (e *Engine) SetResolutionTargetControllerLKI(m map[state.ObjID]state.PlayerID) map[state.ObjID]state.PlayerID {
	prev := e.resolvingTargetControllerLKI
	e.resolvingTargetControllerLKI = m
	return prev
}

// SetResolutionCtx implements effects.Host's optional resolutionCtxHost
// interface: effects.Resolve publishes the live Ctx of the chain it is about
// to walk, and restores the previous value on return, so the scratch holds
// exactly the innermost running chain's Ctx. Engine.Ask consumes it as the
// fallback source of the chain's TargetUnique$ accumulator. Writes engine
// scratch, never e.resume, so it is not a writer of the resume state the
// archtest guards.
func (e *Engine) SetResolutionCtx(c *effects.Ctx) *effects.Ctx {
	prev := e.resolutionCtx
	e.resolutionCtx = c
	return prev
}

// snapshotDepartingTargetCounters refreshes the resolving chain's target-
// counters look-back (Ctx.TargetCountersLKI) at the DEPARTURE boundary, the
// authoritative capture point for the CR 608.2b/h read: oid is about to leave
// the battlefield (its MoveZone is in flight, pre-Apply), so if it is an
// object target of the published chain Ctx its counters are captured NOW --
// immediately before events.Apply's Move fold clears them -- overwriting
// effects.Resolve's resolution-start snapshot. A chain that changed the
// target's counters earlier in the same resolution (Dismantle-style destroy
// preceded by a counter swing) must look back to the counters as they were at
// the zone change, not to the stale entry capture. An object that departs
// with no counters drops its entry, so the read fails closed to the live
// (already-cleared) zero. Non-targets are never captured: the look-back is
// read only through the chain's target groups.
func (e *Engine) snapshotDepartingTargetCounters(oid state.ObjID) {
	c := e.resolutionCtx
	if c == nil {
		return
	}
	targeted := false
	for _, t := range c.Targets {
		if !t.IsPlayer && t.Obj == oid {
			targeted = true
			break
		}
	}
	if !targeted && c.PickedTargets != nil {
		for _, t := range c.PickedTargets {
			if !t.IsPlayer && t.Obj == oid {
				targeted = true
				break
			}
		}
	}
	if !targeted {
		return
	}
	o := e.G.Obj(oid)
	if o == nil || o.Zone != state.ZBattlefield {
		return
	}
	// The P/T half (Ctx.TargetPTLKI): the layer-derived power and toughness
	// the target has at this last battlefield instant (CR 608.2h -- a
	// Giant Growth-pumped Condemn target's toughness, not the printed one).
	if o.Face() != nil {
		if c.Snap.TargetPT == nil {
			c.Snap.TargetPT = make(map[state.ObjID]effects.TargetPT)
		}
		c.Snap.TargetPT[oid] = effects.TargetPT{Power: e.Power(oid), Toughness: e.Toughness(oid)}
	}
	if len(o.Counters) == 0 {
		delete(c.Snap.TargetCounters, oid)
		return
	}
	if c.Snap.TargetCounters == nil {
		c.Snap.TargetCounters = make(map[state.ObjID][]state.Counter)
	}
	c.Snap.TargetCounters[oid] = append([]state.Counter(nil), o.Counters...)
}
