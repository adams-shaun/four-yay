package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// station.go implements the two bespoke activated actions this ticket's
// keywords need, each with its own offer in legalActions, its own KChoose
// answer flow (handleChoose routes on the chooseFor below), and its own
// effects:
//
//   - kw:Station (CR 702.150, "Station: Tap another creature you control: Put
//     charge counters equal to its power on this Spacecraft. Station only as
//     a sorcery."): the offer is gated on the sorcery window (sorcerySpeed)
//     and on at least one legal candidate existing; the KChoose picks the
//     creature to tap; the answer emits the Tap (the cost) and the CHARGE
//     CounterChange (the effect, its amount read from the layer-derived
//     power at answer time).
//
//   - Room unlock (CR 309.5, rules/rooms.go): the offer is gated on the
//     sorcery window and on the locked half's mana cost being payable; the
//     answer pays that cost and emits the DoorUnlock event.
//
// Both follow the cleanup-discard precedent: the decision suspends nothing
// mid-resolution (no resolution is running), and the answer handler is the
// flow's whole continuation.

// chooseStation is the chooseFor for the Station tap pick. iota+13 is
// pairwise distinct from the shared package set as of the mass-turn/
// new-mechanics merge: cast=1/etb=2/miracle=3 (cast.go), cleanup=4/
// damageDivision=5 (combat.go), mana=6/manaColor=7/manaDiscard=8/
// manaExile=9 (mana_activation.go), opening=10 (opening_hand.go),
// suspendCast=11 (turn.go) -- the exact numbers only need to differ.
const chooseStation chooseFor = iota + 13

// chooseUnlock is a marker for the room-unlock flow (no second decision is
// ever asked -- the unlock pays its cost directly on the option answer) -- but
// keeping the name reserved here documents that handlePriority's "unlock"
// case never routes through handleChoose. The zero-value flow that reads it
// is the cast flow's; no handler exists, by design.
const chooseUnlock chooseFor = iota + 14

func hasCreatureType(types []string) bool {
	for _, typ := range types {
		if typ == "Creature" {
			return true
		}
	}
	return false
}

// stationCandidates lists the creatures p controls that may be tapped for
// Station: on the battlefield and untapped, not the spacecraft itself
// ("Tap ANOTHER creature you control"). Summoning sickness does NOT
// disqualify a creature: sickness (CR 302.6) bars a creature from ATTACKING
// and from activating its OWN tap-cost abilities -- tapping it to pay
// another permanent's activation cost is neither, exactly as crewing a
// Vehicle (CR 702.121b) is. The CR-correct shape is also the simpler one.
func (e *Engine) stationCandidates(p state.PlayerID, station state.ObjID) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if id == station {
			continue
		}
		o := e.G.Obj(id)
		if o == nil || o.Tapped || !hasCreatureType(e.Derived(id).Types) {
			continue
		}
		out = append(out, id)
	}
	return out
}

// askStation poses the Station tap pick for the spacecraft the "station"
// option named. A stale option (no spacecraft, it left play, no candidate
// anymore) degrades to a no-op: nothing is emitted, priority resumes
// untouched.
func (e *Engine) askStation(p state.PlayerID, opt decision.Option) {
	e.stationing = opt.Obj
	o := e.G.Obj(opt.Obj)
	if o == nil || o.Zone != state.ZBattlefield || !e.hasKeywordH(opt.Obj, kwhStation) {
		return
	}
	cands := e.stationCandidates(p, opt.Obj)
	if len(cands) == 0 {
		return
	}
	opts := make([]decision.Option, 0, len(cands))
	for _, id := range cands {
		name := "a creature"
		if co := e.G.Obj(id); co != nil && co.Face() != nil {
			name = co.Face().Name
		}
		opts = append(opts, decision.Option{Index: len(opts), Kind: "station",
			Label: fmt.Sprintf("Tap %s (power %d) to station", name, e.Power(id)),
			Obj:   id, Player: p})
	}
	e.choosing = chooseStation
	e.ask(&decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt:  "Station " + o.Face().Name + " — tap another creature you control",
		Options: opts})
}

// handleStation applies the answered Station pick: the Tap is the cost (one
// event), the CHARGE counters the effect, its amount the tapped creature's
// layer-derived power read at answer time. A stale answer (the creature left
// play, or is already tapped) degrades to a no-op rather than wedging.
func (e *Engine) handleStation(spacecraft state.ObjID, chosen []decision.Option) {
	e.stationing = 0
	if len(chosen) == 0 {
		return
	}
	id := chosen[0].Obj
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		return
	}
	// The stationing player puts the CHARGE counters, and this is a
	// turn-based special action with no stack cause, so publish the adder
	// explicitly (chosen[0].Player is the station ask's own player).
	prevAdder := e.SetCounterAdder(chosen[0].Player)
	defer e.SetCounterAdder(prevAdder)
	n := e.Power(id)
	e.emit(events.Event{Kind: events.Tap, Obj: id})
	if n > 0 {
		e.emit(events.Event{Kind: events.CounterChange, Obj: spacecraft,
			Counter: "CHARGE", Amount: n})
	}
}
