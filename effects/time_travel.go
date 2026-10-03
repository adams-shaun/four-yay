package effects

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("TimeTravel", effTimeTravel) }

// effTimeTravel implements Doctor Who's Time Travel action. It runs the
// action Amount$ times (default 1, The Tenth Doctor's Amount$ 3): each
// repetition re-enumerates — in the game's deterministic order, owned
// suspended cards first then the controller's battlefield permanents — every
// affected object and asks that object one optional add/remove/skip election
// (CR: "for each suspended card you own and each permanent you control with a
// time counter on it, you may add or remove a time counter").
//
// A repetition's eligible set is captured ONCE as an immutable snapshot and
// rides the decision's ResumeObjects (rules stores it on the resume point and
// restores Ctx.TimeTravelObjects on re-entry), so an answer that drops an
// object's counter — or removes it from the battlefield — cannot shift the
// next object's cursor. The cursor is TimeTravelIndex into that snapshot and
// TimeTravelRound counts completed repetitions; an ask carries them as the
// decision's ResumeTarget and ResumeRound, two fields rather than one packed
// int, because an int is 32 bits wide on a 32-bit build and cannot hold both
// halves. Finishing a repetition copies a FRESH snapshot for the next
// one, which is what makes "then do it two more times" re-evaluate each
// object's current counter count.
func effTimeTravel(h Host, c *Ctx, sa *cards.SA) {
	choice := string("")

	done := false
	idx, round := int(0), int(0)

	objects := ([]state.ObjID)(nil)

	amount := int(Num(h, c, sa, "Amount", 1))
	if amount < 1 {
		return
	}
	if idx < 0 {
		idx = 0
	}
	if round < 0 {
		round = 0
	}
	if len(objects) == 0 {
		objects = timeTravelObjects(h.Game(), c.Controller)
	}

	// Apply the answer that suspended the previous pass, at the exact object
	// it named (the snapshot and cursor are the ones that asked).
	if done && idx < len(objects) {
		applyTimeTravel(h, objects[idx], choice)
		idx++
	}

	for {
		// A finished repetition: start the next one over a freshly
		// enumerated set, or stop once Amount repetitions have run.
		if idx >= len(objects) {
			round++
			if round >= amount {
				return
			}
			objects = timeTravelObjects(h.Game(), c.Controller)
			idx = 0
			if len(objects) == 0 {
				// Nothing is affected in this repetition; later repetitions
				// cannot gain objects without state changing, and the caller
				// itself is the only such change, so stop.
				return
			}
		}
		id := objects[idx]
		if !timeTravelEligible(h.Game(), id, c.Controller) {
			idx++
			continue
		}
		d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose,
			Min: 1, Max: 1, Source: c.Source,
			ResumeKind: "time_travel", ResumeSA: sa,
			// The index into the repetition's snapshot and the repetition
			// itself ride separate fields (an int is 32 bits on a 32-bit
			// build, so the two cannot share one). ResumeObjects carries the
			// snapshot itself, so a removal that shrinks the live eligible
			// set cannot shift this cursor.
			ResumeTarget:  idx,
			ResumeRound:   round,
			ResumeObjects: append([]state.ObjID(nil), objects...),
			Prompt:        "Time travel: add or remove a time counter?"}
		d.Options = append(d.Options,
			decision.Option{Index: 0, Kind: "time_travel_skip", Label: "Skip", Obj: id},
			decision.Option{Index: 1, Kind: "time_travel_add", Label: "Add a time counter", Obj: id},
			decision.Option{Index: 2, Kind: "time_travel_remove", Label: "Remove a time counter", Obj: id})
		if ans, ok := AskTape(h, d); ok {
			// The "time_travel" answer in hand (a malformed empty one
			// skips, as the arm reads it): applied at this object, then the
			// walk goes on.
			choice := "time_travel_skip"
			if len(ans) > 0 {
				choice = ans[0].Kind
			}
			applyTimeTravel(h, id, choice)
			idx++
			continue
		}
		if Ask(h, d) == AskAsked {
			return
		}
		// R-9/no-host fallback: decline the optional election. This still
		// traverses every affected object and never emits an unimplemented note.
		applyTimeTravel(h, id, "time_travel_skip")
		idx++
	}
}

func timeTravelObjects(g *state.Game, controller state.PlayerID) []state.ObjID {
	out := make([]state.ObjID, 0)
	// Exiled suspended cards are ordered by the owner's exile zone order. The
	// provenance flag prevents ordinary exiled TIME-counter cards from being
	// treated as suspended.
	for _, id := range g.Zone(state.ZExile, controller) {
		o := g.Obj(id)
		if o != nil && o.Owner == controller && o.CastFlags&state.FlagSuspend != 0 {
			out = append(out, id)
		}
	}
	for _, id := range g.Zone(state.ZBattlefield, controller) {
		o := g.Obj(id)
		if o != nil && o.Controller == controller && o.Counter("TIME") > 0 {
			out = append(out, id)
		}
	}
	return out
}

func timeTravelEligible(g *state.Game, id state.ObjID, controller state.PlayerID) bool {
	o := g.Obj(id)
	if o == nil {
		return false
	}
	if o.Zone == state.ZExile {
		return o.Owner == controller && o.CastFlags&state.FlagSuspend != 0
	}
	return o.Zone == state.ZBattlefield && o.Controller == controller && o.Counter("TIME") > 0
}

func applyTimeTravel(h Host, id state.ObjID, choice string) {
	o := h.Game().Obj(id)
	if o == nil {
		return
	}
	// A suspended card may receive its first TIME counter. Battlefield
	// permanents, by contrast, are eligible only while they already have one.
	if o.Zone == state.ZBattlefield && o.Counter("TIME") <= 0 {
		return
	}
	if o.Zone != state.ZBattlefield && (o.Zone != state.ZExile || o.CastFlags&state.FlagSuspend == 0) {
		return
	}
	switch strings.TrimSpace(choice) {
	case "time_travel_add":
		h.Emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "TIME", Amount: 1})
	case "time_travel_remove":
		h.Emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "TIME", Amount: -1})
	case "time_travel_skip", "":
	default:
		h.Emit(events.Event{Kind: events.Note, Obj: id,
			Text: fmt.Sprintf("TimeTravel: unknown choice %q; skipped", choice)})
	}
}
