package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// firstChosen is d.Chosen(in)[0] without materialising the chosen list (a
// heap copy of every chosen Option on every priority answer). It keeps
// Chosen's all-or-nothing contract: any out-of-range index, or no choice at
// all, is the same index-out-of-range panic the [0] of a nil list raised.
func firstChosen(d *decision.Decision, in decision.Intent) decision.Option {
	var none []decision.Option
	for _, c := range in.Choices {
		if c < 0 || c >= len(d.Options) {
			return none[0]
		}
	}
	if len(in.Choices) == 0 {
		return none[0]
	}
	return d.Options[in.Choices[0]]
}

func (e *Engine) handlePriority(d *decision.Decision, in decision.Intent) {
	opt := firstChosen(d, in)
	if opt.Kind != "pass" && opt.Kind != "concede" {
		// The inert backstop (rules/priority_guard.go): an action whose
		// handler emits nothing past the priority reset is recorded and
		// held out instead of being re-offered forever.
		mark := len(e.L.Events)
		defer e.inertPriorityBackstop(in.Player, opt, mark)
	}
	switch opt.Kind {
	case "pass":
		passes := e.G.Passes + 1
		if passes >= int32(e.G.AliveCount()) {
			if len(e.G.Stack) > 0 {
				e.resolveTop()
				// CR 117.5: nobody receives priority in the middle of a
				// resolution. A resolution that suspends on a mid-resolution
				// ask (a modal spell's KModes, an as-enters choose, an
				// unless-pay, a discard) is parked on that question: no player
				// has priority while the question is outstanding, so the log
				// must not record that priority returned to the active player
				// here. The one and only grant for that resolution happens when
				// it actually completes: the answering Submit re-enters
				// grantPriority (through resumeTriggerDrain / the step loop)
				// once e.resume is cleared, so an unsuspended resolution emits
				// the priority-returns-to-active marker below while a suspended
				// one defers it to its completion. Exactly one grant either
				// way; a resolution that suspends more than once (a nested ask)
				// still completes once and grants once.
				if e.Suspended() {
					return
				}
				// The pass count resets: priority returns to the active
				// player after a resolution, same as at the start of any
				// other step.
				e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
				return
			}
			// advanceStep's own emit carries the reset pass count; the count
			// this round reached is never itself a value anything observes.
			//
			// CR 514.3b (mayflashsac2, review round 3): an emptied cleanup-step
			// stack is NOT licence to advance. The rules require the cleanup
			// procedure to REPEAT, redoing its 514.1/514.2 actions, after a
			// trigger resolves or a player acts in this window -- an instant
			// cast here (Giant Growth) must have its 'until end of turn' effect
			// expire in the repeated cleanup, and a trigger that drew the
			// active player over the hand limit must face the repeat's
			// discard. advanceStep would instead begin the next turn outright,
			// skipping both. repeatCleanup runs the whole procedure once more
			// and itself reaches advanceStep only when nothing is waiting.
			if e.G.Step == state.StepCleanup {
				e.repeatCleanup()
				return
			}
			e.advanceStep()
			return
		}
		e.emit(events.Event{Kind: events.Priority, Player: e.G.NextAlive(e.G.Priority), Amount: passes})

	case "play_land":
		// A modal-land option is a face selection, not a generic land play.
		// Revalidate it against the current object before mutating state: the
		// priority option may have gone stale while another decision resolved.
		if opt.Mode == "modal_land" {
			if modalLandBack(e.G.Obj(opt.Obj)) == nil {
				return
			}
			e.emit(events.Event{Kind: events.FlipFace, Obj: opt.Obj, Amount: 1})
		}
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		// A land play uses the ordinary pending cast flow so payCast can put
		// LandPlayed after its MoveZone entry boundary. The MoveZone replacement
		// owns any "as this enters" choice, like every other entry path.
		//
		// The source zone is the object's CURRENT zone, not hardcoded to the
		// hand: since the MayPlay grants (rules/mayplay.go) the play_land
		// offer also comes from the graveyard, exile or the top of the
		// library, a hand land and a graveyard land must move From the zone
		// they were actually offered from (CR 118.3a -- the permission names
		// the zone it grants). A land that somehow left its zone between the
		// offer and the answer resolves from whatever zone it is in, and the
		// MoveZone/LandPlayed below still records a legal play.
		from := state.ZHand
		if o := e.G.Obj(opt.Obj); o != nil {
			from = o.Zone
		}
		e.cast = e.newCast(in.Player, opt.Obj, from, "land", -1)
		e.continueCast()

	case "activate":
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		e.activateMana(in.Player, opt.Obj, false)

	case "ability":
		// Task 10: an activated ability (non-mana AB$) was chosen. Reset the
		// pass count the same way every other non-pass action does, then drive
		// the same cost flow a cast drives (rules/activate.go's
		// beginActivation -> pendingCast -> continueCast -> commitCast).
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		e.beginActivation(in.Player, opt)

	case "concede":
		// M2d-3 (R-M3): choosing the concede option emits the existing
		// PlayerLost event with Text "conceded" (CR 104.3a) -- one event,
		// the same one a 0-life elimination emits. PlayerLost's Apply marks
		// the seat Lost; this checkStateBased then sweeps its permanents
		// (CR 800.4a) and ends the game with the last remaining seat the
		// winner (checkGameOver, CR 104.2a) -- a concession is just another
		// way to be Lost. With three or more seats still alive, Submit's own
		// tail Advance continues the match with the Lost seat skipped
		// everywhere (grantPriority, NextAlive, beginTurn).
		e.emit(events.Event{Kind: events.PlayerLost, Player: in.Player, Text: "conceded"})
		// CR 726.4 (task ds4-cr726.4): a concession is a way a player leaves
		// the game, so the initiative handoff runs here too -- the concede path
		// deliberately bypasses the playerLoses gate (CR 104.3a), which is why
		// this is its own call rather than one inside the gate.
		e.initiativeHandoffOnDeparture(in.Player)
		e.checkStateBased()

	case "station":
		// kw:Station (CR 702.150, rules/station.go): the spacecraft is
		// stationed by tapping another creature the KChoose below names. The
		// pass-count reset matches every other non-pass action.
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		e.askStation(in.Player, opt)

	case "unlock":
		// Room unlock (CR 309.5, rules/rooms.go): pay the locked half's mana
		// cost and emit the DoorUnlock event. The offer gated on castable,
		// so the payment here cannot disagree with the offer; a stale option
		// (the room left play or was unlocked between offer and answer -- the
		// same seat's answer, so the board cannot have moved) degrades to a
		// no-op through unlockRoomCost's nil face.
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		o := e.G.Obj(opt.Obj)
		cost, ok := e.unlockRoomCost(o)
		if !ok {
			return
		}
		mods, ok := e.unlockMods(in.Player, opt.Obj)
		if !ok {
			return
		}
		if !e.payMana(in.Player, mods.apply(cost)) {
			return
		}
		e.emit(events.Event{Kind: events.DoorUnlock, Obj: opt.Obj})

	case "granted":
		// kw:Start your engines (CR 702.179e, rules/speed.go): a max-speed
		// static's granted ability, activated through the ordinary cost
		// payment and the delayed-shape ability mint.
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		e.beginGrantedActivation(in.Player, opt)

	case "specialize":
		e.specialize(in.Player, opt)

	case "turn_face_up":
		// Morph-family turn face up (CR 708.6 / CR 116.2b): a special action
		// -- no stack, no target, no response window. rules/morph_turnup.go
		// owns the payment and the TurnFaceUp/megamorph-counter events.
		e.turnFaceUp(in.Player, opt)

	case "cast":
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		e.beginCast(in.Player, opt)
	}
}
