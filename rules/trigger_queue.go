// trigger_queue.go owns the pending-trigger queue that checkTriggers
// (trigger_match.go) fills and putTriggersOnStack drains onto the stack in
// APNAP order, one group of same-controller triggers at a time. The
// ask/handle pairs below (askTriggerOrder/handleTriggerOrder,
// askTriggerOptional/handleTriggerOptional) are how a player orders or
// accepts/declines the triggers this file offers them; nothing here decides
// whether a trigger matches, only how an already-matched one gets resolved.
package rules

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// ringEmblemLabel names one of the Ring emblem's level abilities (CR
// 701.54c) for an ordering ask or a log line. The emblem has no card to read
// a name or TriggerDescription$ from, so the label is the rules text itself.
func ringEmblemLabel(level int) string {
	switch level {
	case ringEmblemLevelDraw:
		return "The Ring emblem: whenever your Ring-bearer attacks, draw a card"
	case ringEmblemLevelBlocked:
		return "The Ring emblem: whenever your Ring-bearer becomes blocked, discard a card; if you can't, sacrifice it"
	case ringEmblemLevelCombatHit:
		return "The Ring emblem: whenever your Ring-bearer deals combat damage to a player, sacrifice it"
	case ringEmblemLevelTempted:
		return "The Ring emblem: whenever the Ring tempts you, each opponent loses 1 life"
	}
	return "The Ring emblem ability"
}

// putTriggersOnStack drains pendingTriggers onto the stack, asking each
// controller who has two or more of them for the order (CR 603.3b, user
// requirement R1) and asking each optional trigger's decider whether it goes
// on the stack at all (Forge's OptionalDecider$, user requirement R2).
//
// It reports whether it asked a decision and so has NOT finished. A true
// return means e.pending is set: the caller must return immediately without
// granting priority, and the drain is resumed by handleTriggerOrder /
// handleTriggerOptional below, both of which end in resumeTriggerDrain
// (turn.go). It is entered from exactly one place, turn.go's priorityRound,
// and re-entered only through resumeTriggerDrain -- which is itself the
// continuation of that same priorityRound. That is what makes "finish the
// priority round" the one correct continuation for every decision it asks,
// and it is why the resumption goes to grantPriority rather than back through
// priorityRound -- see resumeTriggerDrain's own header (turn.go) for the
// reason that still holds: a second call on priorityRound's path would give
// a replacement-blocked state-based action a second attempt per step and
// move sba.go's measured firing counts (Ruling T22-p). (Ruling T28-b: this
// used to say re-entering priorityRound "would run the draw step's draw a
// second time" -- true before Task 28 moved the draw out of priorityRound
// entirely; the reason above is what actually survives that move.)
//
// ORDERING DIRECTION (Ruling U2, and the one thing here that is silent if it
// is backwards): the ordering decision's choice[0] is the trigger put on the
// stack FIRST, and therefore the trigger that resolves LAST. That is the same
// direction as the between-player rule either side of it -- APNAP puts the
// active player's triggers on the stack first and resolves them last -- so
// one sentence describes the whole placement. The Prompt says so in the
// player's own words, and TestTriggerOrderChoiceDecidesResolutionOrder
// asserts it by the order the two effects actually RUN, not by the order they
// were pushed.
//
// The between-player order is not the player's (Ruling U1): it is CR 603.3b's
// APNAP, kept here as the same sort.SliceStable by controller rank the
// pre-Task-27 version used. Ties within one controller keep checkTriggers'
// discovery order (seat, then zone, then zone position); that is the order the
// options are OFFERED in, and which of them actually goes first is then the
// controller's answer rather than the engine's.
//
// RESUMABILITY (Ruling U3). The queue this walks can grow underneath it.
// Submit runs handle, then checkStateBased, then Advance, and checkStateBased
// (sba.go) is a bounded fixed-point loop that emits PlayerLost, MoveZone and
// GameOver events -- every one of which runs checkTriggers. So a creature
// dying to a state-based action while a controller is being asked to order
// their triggers really does append to e.pendingTriggers between the ask and
// the answer. Three properties make that safe:
//
//   - Appends only ever land at the END of e.pendingTriggers, and a decision
//     is always about entries at the FRONT. Nothing but popFrontTrigger and
//     dropDepartedTriggers ever removes an entry.
//   - e.orderedTriggers counts the leading entries whose order is already
//     settled. While it is non-zero the queue is not re-sorted, so a trigger
//     arriving mid-drain can neither be shuffled into a group the controller
//     has already ordered nor make them order the same triggers twice.
//   - A trigger leaves the queue in the same call that pushes it (or declines
//     it), so nothing can be pushed twice or dropped on the floor.
//
// A trigger that arrives after the group it would have joined was settled
// simply forms the next group, and is still placed before any player receives
// priority -- which is all CR 117.5 asks of it.
//
// CR 800.4a: an ability controlled by a player who has left the game ceases to
// exist, so dropDepartedTriggers discards those rather than (as the
// pre-Task-27 version did) ranking them after every living seat and putting
// them on the stack regardless.
func (e *Engine) putTriggersOnStack() bool {
	for {
		if e.G.Over {
			e.pendingTriggers, e.orderedTriggers = nil, 0
			return false
		}
		e.dropDepartedTriggers()
		if len(e.pendingTriggers) == 0 {
			// Keep the drained queue's array for the next batch, zeroed so
			// the shifted-out tail pins nothing (removeTriggerAt shifts in
			// place; nothing holds a view of the queue across a drain). Only
			// the prefix a shrink can have left entries in needs zeroing
			// (trigQueueStale): an already-drained array is clean, and a
			// pendingTrigger carries a whole effects.Ctx, so re-zeroing the
			// whole capacity on every priority round was most of this call.
			if n := min(e.trigQueueStale, cap(e.pendingTriggers)); n > 0 {
				clear(e.pendingTriggers[:n])
			}
			e.trigQueueStale = 0
			e.pendingTriggers, e.orderedTriggers = e.pendingTriggers[:0], 0
			return false
		}
		if e.orderedTriggers == 0 {
			n := e.sortPendingTriggers()
			// Forge's OrderDuplicates$: duplicate instances of a flagged
			// trigger line are kept adjacent (their order among the copies
			// stable) before the ordering ask is built. Must run BEFORE the
			// ask so the decision's options and handleTriggerOrder's recheck
			// see the same grouped queue.
			e.groupOrderDuplicates(n)
			if n >= 2 {
				// R1. Exactly one trigger is never asked about: there is no
				// choice to make, and a decision with a single legal answer is
				// noise on the wire (definition of done, item 2).
				e.askTriggerOrder(e.pendingTriggers[0].Controller, n)
				return true
			}
			e.orderedTriggers = n
		}
		pt := e.pendingTriggers[0]
		// CR 603.5: an optional triggered ability goes on the stack
		// REGARDLESS of whether its controller wants to apply the effect; the
		// choice is made as it resolves (resolveTop's ability branch poses
		// askOptionalAtResolution). So an OptionalDecider$ trigger is pushed
		// unconditionally here, exactly like a mandatory one. Miracle is the
		// lone keyword cast offer decided at placement. Madness is mandatory
		// here: its cast choice happens only when its respondable keyword
		// ability resolves.
		if who, optional, askable := e.optionalDecider(pt); optional && pt.Miracle {
			if !askable {
				// The decider left the game between this trigger matching and
				// its turn to be placed. R2 forbids assuming the answer, so
				// the trigger is declined rather than silently placed.
				e.popFrontTrigger()
				continue
			}
			e.askTriggerOptional(who, pt)
			return true
		}
		e.popFrontTrigger()
		e.pushTrigger(pt)
		if e.Pending() != nil {
			// Task 7: the trigger asked for its targets right after its
			// TriggerPush. Return immediately without granting priority, exactly
			// as for the ordering and optional asks above; the drain resumes
			// through handleTarget -> resumeTriggerDrain.
			return true
		}
	}
}

// sortPendingTriggers puts the whole queue in CR 603.3b APNAP order by
// controller and reports how many leading entries share the first entry's
// controller -- the size of the group whose internal order is that
// controller's to choose.
//
// Called only when e.orderedTriggers is zero, i.e. when no group's order has
// been settled yet, so it can never re-order an answer a player already gave.
// dropDepartedTriggers runs first on every pass, so every controller left here
// is alive and therefore present in rank; the map is indexed by key and never
// ranged over, so no map iteration order can reach an event or the order of a
// decision's options.
func (e *Engine) sortPendingTriggers() int {
	seats := e.G.AliveFrom(e.G.Active)
	rank := make(map[state.PlayerID]int, len(seats))
	for i, p := range seats {
		rank[p] = i
	}
	sort.SliceStable(e.pendingTriggers, func(i, j int) bool {
		return rank[e.pendingTriggers[i].Controller] < rank[e.pendingTriggers[j].Controller]
	})
	first := e.pendingTriggers[0].Controller
	n := 0
	for _, pt := range e.pendingTriggers {
		if pt.Controller != first {
			break
		}
		n++
	}
	return n
}

// triggerOrdersDuplicates reports whether a trigger line carries Forge's
// OrderDuplicates$ True: the copies of that line, when several permanent
// instances of the same card trigger at once, must be ordered as a block so
// their relative order among themselves is stable (Forge forces its ordering
// prompt for such duplicates even when the trigger texts are identical --
// the shape Arcane Bombardment and Captured by the Consulate carry). A line
// that does not carry the flag keeps its natural discovery order.
func triggerOrdersDuplicates(t cards.Trigger) bool {
	return strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKOrderDuplicates)), "True")
}

// printed reports whether pt is an ordinary printed face trigger -- one whose
// TriggerPush re-derives from the source's active face at Idx. The keyword
// and synthetic shapes (Ward, Afflict, Conspire, Cascade, Exploit, Offspring,
// Mentor, the Ring emblem, Miracle, Madness, Evoke) carry no face trigger
// line, and a delayed, granted or merged entry's body is an Execute$ SVar
// rather than a face Triggers entry -- none of them can carry
// OrderDuplicates$, and reading face.Triggers[Idx] for one would hand back a
// different line entirely.
func (pt pendingTrigger) printed() bool {
	return !pt.Delayed && !pt.Granted && pt.Merged == 0 && !pt.Miracle && !pt.Madness &&
		!pt.Evoke && !pt.Gift && pt.Ward == "" && pt.Afflict == "" && !pt.Conspire && !pt.Casualty && !pt.Cascade &&
		!pt.Exploit && !pt.Offspring && !pt.Mentor && pt.RingEmblem == 0 && pt.Cipher == 0 && !pt.SpeedIncrease &&
		!pt.InitiativeVenture
}

// orderDuplicatesGroup returns the duplicate-group identity of pt's trigger
// line when that line carries OrderDuplicates$ True. Two pending triggers are
// duplicates exactly when they are the same trigger line of the same printed
// card on two permanent instances: the identity is the source's printed card
// name plus the line's index within that face (two copies of one card share
// the face, so the index identifies the line). A non-duplicate -- a line
// without the flag, or one this build cannot attribute to a printed face --
// returns ok false and is never grouped.
func (e *Engine) orderDuplicatesGroup(pt pendingTrigger) (string, bool) {
	if !pt.printed() {
		return "", false
	}
	t, ok := e.triggerOf(pt)
	if !ok || !triggerOrdersDuplicates(t) {
		return "", false
	}
	o := e.G.Obj(pt.Source)
	if o == nil {
		return "", false
	}
	f := o.Face()
	if f == nil {
		return "", false
	}
	return f.Name + "\x00" + strconv.Itoa(int(o.FaceIdx)) + "\x00" + strconv.Itoa(pt.Idx), true
}

// groupOrderDuplicates makes duplicate instances of an OrderDuplicates$
// trigger line contiguous within the n leading entries of e.pendingTriggers
// (one controller's group, just sorted by sortPendingTriggers). Instances of
// a flagged line move to that line's FIRST occurrence, in their existing
// relative order, so the copies' order among themselves is stable and none of
// them is interleaved with another trigger's resolution.
//
// The reorder is a STABLE sort by each entry's group anchor: an entry is
// anchored at the first occurrence of its flagged-duplicate signature when
// that signature occurs more than once, and at its own position otherwise.
// Anchors are unique per position, so the sort is deterministic and the
// relative order of distinct triggers is preserved. Called only when
// e.orderedTriggers is zero (right after sortPendingTriggers), so it can
// never disturb an order a player already gave.
func (e *Engine) groupOrderDuplicates(n int) {
	if n < 2 {
		return
	}
	group := e.pendingTriggers[:n]
	sig := make([]string, n)
	grouped := make([]bool, n)
	first := make(map[string]int, n)
	count := make(map[string]int, n)
	for i := range group {
		s, ok := e.orderDuplicatesGroup(group[i])
		if !ok {
			continue
		}
		sig[i], grouped[i] = s, true
		if _, seen := first[s]; !seen {
			first[s] = i
		}
		count[s]++
	}
	order := make([]int, n)
	duplicate := false
	for i := range group {
		order[i] = i
		if grouped[i] && count[sig[i]] >= 2 {
			order[i] = first[sig[i]]
			duplicate = true
		}
	}
	if !duplicate {
		return
	}
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return order[idx[a]] < order[idx[b]] })
	out := make([]pendingTrigger, n)
	for a, i := range idx {
		out[a] = group[i]
	}
	copy(group, out)
}

// dropDepartedTriggers discards every pending trigger controlled by a player
// who has left the game (CR 800.4a) and leaves e.orderedTriggers counting the
// same surviving entries it counted before.
func (e *Engine) dropDepartedTriggers() {
	departed := false
	for i := range e.pendingTriggers {
		if c := e.pendingTriggers[i].Controller; int(c) >= len(e.G.Players) || e.G.Players[c].Lost {
			departed = true
			break
		}
	}
	if !departed {
		// Nothing to drop: the filter below would copy every entry onto
		// itself, and count min(orderedTriggers, len) settled survivors.
		e.orderedTriggers = min(e.orderedTriggers, len(e.pendingTriggers))
		return
	}
	e.noteTrigShrink()
	kept := e.pendingTriggers[:0]
	ordered := 0
	for i, pt := range e.pendingTriggers {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			continue
		}
		if i < e.orderedTriggers {
			ordered++
		}
		kept = append(kept, pt)
	}
	e.pendingTriggers = kept
	e.orderedTriggers = ordered
}

// noteTrigShrink records the queue's length before a shrink, so the drain
// knows how much of the backing array may hold left-behind entries
// (trigQueueStale). Every site that shortens pendingTriggers in place calls
// it first.
func (e *Engine) noteTrigShrink() {
	if n := len(e.pendingTriggers); n > e.trigQueueStale {
		e.trigQueueStale = n
	}
}

// popFrontTrigger removes the entry every pending decision is about. Entries
// are shifted down rather than the slice re-sliced so that index 0 always
// means the same thing to every reader, including one that ran between an ask
// and its answer.
func (e *Engine) popFrontTrigger() {
	if len(e.pendingTriggers) == 0 {
		return
	}
	e.removeTriggerAt(0)
}

// removeTriggerAt takes one entry out of the queue, keeping orderedTriggers
// counting the same settled entries it counted before. Entries are shifted
// down rather than the slice re-sliced so index 0 always means the same thing
// to every reader, including one that ran between an ask and its answer.
func (e *Engine) removeTriggerAt(i int) pendingTrigger {
	pt := e.pendingTriggers[i]
	e.noteTrigShrink()
	e.pendingTriggers = append(e.pendingTriggers[:i], e.pendingTriggers[i+1:]...)
	if i < e.orderedTriggers {
		e.orderedTriggers--
	}
	return pt
}

// takeAnsweredTrigger removes and returns the pending trigger an optional
// decision was asked about.
//
// It is normally at the front: nothing on a reachable path removes from the
// front of the queue between an ask and its answer. Fix round 1, review
// finding F4: this used to be a bare front-only equality check whose failure
// branch did nothing at all, so the drain went straight on to ask the SAME
// trigger's question again -- the player's answer consumed, its Seq spent,
// and silently discarded. Searching forward instead honours the answer
// wherever the entry actually sits and never puts the same question twice,
// which is also how handleTriggerOrder's own defensive branch behaves (it
// forces progress rather than re-asking). A source carrying two pending
// triggers is unambiguous here because the search runs front-first, and the
// front one is the one that was asked about.
func (e *Engine) takeAnsweredTrigger(d *decision.Decision) (pendingTrigger, bool) {
	for i := range e.pendingTriggers {
		if e.pendingTriggers[i].Source == d.Source {
			return e.removeTriggerAt(i), true
		}
	}
	return pendingTrigger{}, false
}

// pushTrigger mints one triggered ability's stack object.
//
// Ruling T20-a: the object itself is created inside events.Apply's TriggerPush
// case, not here -- a direct, unlogged Game.AddObject call (as this used to
// be) names an ObjID a log-only replay never learns about, so its own
// zone-placement Move silently no-ops and the replayed stack permanently
// diverges from the live one. Player/Obj/Amount/IDs are all Apply needs to
// recreate the same object: the permanent (pt.Source), its Triggers index
// (pt.Idx, so Apply re-derives the *cards.SA rather than needing a logged
// pointer), the controller, and the Remembered object(s).
//
// Ruling U4: the ORDER of these events is the only place the ordering choice
// is recorded, and it is the whole of what a log-only replay needs. No event
// kind and no Event field was added for Task 27.
func (e *Engine) pushTrigger(pt pendingTrigger) {
	// Every push path announces the modes, the root target and (CR 603.3d)
	// the chain links' targets before priority through this one helper. A
	// gained trigger (drainAlways) marks the mode drain even when the mode
	// ask answered in place.
	announce := func(id state.ObjID, drainAlways bool) {
		handled := false
		if pt.SA.ParamStr(cards.PKChoices) != "" {
			handled = e.askTriggerModes(pt.Controller, id, pt.SA)
			if handled && (drainAlways || e.Pending() != nil) {
				e.drainAwaitsModes = true
			}
		}
		if !handled && effects.TargetsOf(pt.SA).Targeted() {
			e.askTarget(pt.Controller, id, pt.SA)
		}
		if !handled {
			e.startTriggerSubTargets(id, pt.Controller, pt.SA)
		}
	}
	if pt.SpeedIncrease {
		e.emit(events.Event{Kind: events.DelayedPush, Player: pt.Controller, Counter: speedTrigger})
		return
	}
	if pt.RadiationDrain {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		e.emit(events.Event{Kind: events.DelayedPush, Player: pt.Controller, Counter: "__radiation_drain"})
		return
	}
	if pt.MonarchDraw {
		// Use DelayedPush's event-sourced stack-object creation. Apply has a
		// dedicated synthetic body for this engine-owned trigger, so no card
		// SVar or direct state mutation is needed.
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		e.emit(events.Event{Kind: events.DelayedPush, Obj: pt.Source,
			Player: pt.Controller, Counter: "__monarch_draw"})
		return
	}
	if pt.InitiativeVenture {
		// CR 726.2's source-less "that player ventures into Undercity". Like
		// MonarchDraw it mints a real stack ability from a synthetic body
		// (events/apply.go's DelayedPush case), so the venture can be responded
		// to by the ordinary priority round.
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		e.emit(events.Event{Kind: events.DelayedPush,
			Player: pt.Controller, Counter: "__initiative_venture"})
		return
	}
	// Evoke and Madness are mandatory keyword-triggered abilities minted as
	// genuine stack objects. Madness's cast-or-graveyard choice is made when
	// that object resolves, not here, so either one may be responded to or
	// countered before doing anything.
	if pt.Madness {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "__kwMadnessCast", Text: "madness cast"})
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	if pt.Gift {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		if int(pt.GiftTo) >= len(e.G.Players) {
			return
		}
		// CR 702.168c: this is a real ETB trigger. GiveGift is recorded when
		// the trigger is put on the stack, not during spell resolution -- and
		// with the promised receiver snapshotted into the payload (IDs), the
		// gift it marks stays deliverable however the permanent moves from
		// here on, so the CR 702.168d reader fires for a gift that will be
		// given.
		e.emit(events.Event{Kind: events.GiveGift, Player: pt.Controller, Obj: pt.Source})
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "GiftAbility", Text: "gift ability",
			IDs: []state.ObjID{state.PlayerRef(pt.GiftTo)}})
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	if pt.Evoke {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "__kwEvokeSacrifice", Text: "evoke sacrifice"})
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	// A granted ward (CR 702.21a via a layer-6 AddKeyword$ Ward:<cost> -- the
	// printed K:Ward expansion is a face trigger and never lands here). The
	// ward ability is mandatory: its Counter payload is the cost text, which
	// events.Apply rebuilds into the same DB$ Ward ability a printed trigger
	// would have carried. The targeted spell is named by the ward Ctx the
	// walk captured (TriggeredStack roles), exactly as the printed path does.
	if pt.Ward != "" {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		stackLen := len(e.G.Stack)
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "__kwWard:" + pt.Ward, Text: "ward ability"})
		if len(e.G.Stack) > stackLen {
			id := e.G.Stack[len(e.G.Stack)-1]
			if e.triggerContexts == nil {
				e.triggerContexts = make(map[state.ObjID]effects.TriggerContext)
			}
			// effWard reads the targeting spell off TriggerStack at resolution,
			// the same role the printed ward's stack object carries.
			e.triggerContexts[id] = pt.Ctx.TriggerContext
		}
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	// A layer-6 Melee instance has no printed trigger index. Its captured
	// attacked-opponent player refs are logged in IDs, so replay and stack
	// copies read the same cards.MeleePumpCount as a printed instance.
	if pt.Melee {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		ids := make([]state.ObjID, 0, len(pt.Ctx.Remembered))
		for _, tgt := range pt.Ctx.Remembered {
			if tgt.IsPlayer {
				ids = append(ids, state.PlayerRef(tgt.Player))
			}
		}
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "__kwMeleeGranted", IDs: ids, Text: "melee ability"})
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	// A granted afflict (CR 702.130 via a layer-6 AddKeyword$ Afflict:<N> --
	// Lost Monarch of Ifnir's "Other Zombies you control have afflict 3"):
	// the Ward shape exactly. The trigger is mandatory; its Counter payload
	// is the life amount, which events.Apply rebuilds into the same
	// DB$ LoseLife body the printed K:Afflict expansion carries, and the
	// captured TriggerContext (the blocked attacker's roles, including the
	// defender Defined$ TriggeredDefendingPlayer reads) rides along.
	if pt.Afflict != "" {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		stackLen := len(e.G.Stack)
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "__kwAfflict:" + pt.Afflict, Text: "afflict ability"})
		if len(e.G.Stack) > stackLen {
			id := e.G.Stack[len(e.G.Stack)-1]
			if e.triggerContexts == nil {
				e.triggerContexts = make(map[state.ObjID]effects.TriggerContext)
			}
			e.triggerContexts[id] = pt.Ctx.TriggerContext
		}
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	// A granted Conspire (CR 702.78a's copy trigger via a layer-6 AddKeyword$
	// Conspire -- Wort, the Raidmother / Raiding Schemes): the Ward shape.
	// The trigger is mandatory; its Counter payload "__kwConspire:" is what
	// events.Apply rebuilds into the same DB$ CopySpellAbility body the
	// printed K:Conspire expansion carries, and the cast spell rides IDs as
	// Remembered because Defined$ TriggeredSpellAbility reads the triggering
	// spell off it (the same IDs encoding the ordinary TriggerPush path
	// uses, which is also what a replay needs). The trailing colon is
	// load-bearing: addKeywordTrigger mints a printed keyword's SVar as
	// "__kw"+line, so a bare K:Conspire line mints exactly "__kwConspire" --
	// the colon keeps the granted payload from aliasing that real SVar (the
	// Ward/Afflict payloads already carry one).
	if pt.Conspire {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		ids := make([]state.ObjID, 0, len(pt.Ctx.Remembered))
		for _, tgt := range pt.Ctx.Remembered {
			if tgt.IsPlayer {
				ids = append(ids, state.PlayerRef(tgt.Player))
				continue
			}
			ids = append(ids, tgt.Obj)
		}
		stackLen := len(e.G.Stack)
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "__kwConspire:", IDs: ids, Text: "conspire ability"})
		if len(e.G.Stack) > stackLen {
			id := e.G.Stack[len(e.G.Stack)-1]
			if e.triggerContexts == nil {
				e.triggerContexts = make(map[state.ObjID]effects.TriggerContext)
			}
			e.triggerContexts[id] = pt.Ctx.TriggerContext
		}
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	if pt.Cipher != 0 {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		ids := make([]state.ObjID, 0, 1)
		if o := e.G.Obj(pt.Cipher); o != nil && o.Zone == state.ZExile {
			ids = append(ids, pt.Cipher)
		}
		if len(ids) == 0 {
			// The encoded card left exile between the match and the push; the
			// trigger has nothing to copy.
			return
		}
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "__kwCipher:", IDs: ids, Text: "cipher copy trigger"})
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	if pt.Casualty {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		stackLen := len(e.G.Stack)
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "__kwCasualty:" + pt.CasPayload, IDs: []state.ObjID{pt.Source}, Text: "casualty ability"})
		if len(e.G.Stack) > stackLen {
			id := e.G.Stack[len(e.G.Stack)-1]
			if e.triggerContexts == nil {
				e.triggerContexts = make(map[state.ObjID]effects.TriggerContext)
			}
			e.triggerContexts[id] = pt.Ctx.TriggerContext
		}
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	// A granted Demonstrate (CR 702.152's copy trigger via a layer-6
	// AddKeyword$ Demonstrate -- Silverquill Lecturer and friends): the
	// Conspire shape. The trigger is mandatory (the "may copy" election is
	// the BODY's own ask at resolution, not a placement election); its
	// Counter payload "__kwDemonstrate:" is what events.Apply rebuilds into
	// the same DB$ Demonstrate body the printed K:Demonstrate expansion
	// carries, and the cast spell rides IDs as Remembered because Defined$
	// TriggeredSpellAbility reads the triggering spell off it. The trailing
	// colon keeps the payload from aliasing the "__kwDemonstrate" SVar a
	// printed bare K:Demonstrate line mints.
	if pt.Demonstrate {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		ids := make([]state.ObjID, 0, len(pt.Ctx.Remembered))
		for _, tgt := range pt.Ctx.Remembered {
			if tgt.IsPlayer {
				ids = append(ids, state.PlayerRef(tgt.Player))
				continue
			}
			ids = append(ids, tgt.Obj)
		}
		stackLen := len(e.G.Stack)
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "__kwDemonstrate:", IDs: ids, Text: "demonstrate ability"})
		if len(e.G.Stack) > stackLen {
			id := e.G.Stack[len(e.G.Stack)-1]
			if e.triggerContexts == nil {
				e.triggerContexts = make(map[state.ObjID]effects.TriggerContext)
			}
			e.triggerContexts[id] = pt.Ctx.TriggerContext
		}
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	// A printed-or-granted cascade (CR 702.85, task cascade1): the Ward
	// shape. The trigger is mandatory; its Counter payload "__kwCascade:" is
	// what events.Apply rebuilds into the DB$ Cascade body both the printed
	// K:Cascade line and every layer-6 AddKeyword$ Cascade grant share, and
	// the exile-until + may-cast sequence runs when the ability RESOLVES
	// (effects/cascade.go's effCascade), respondable like any trigger. The
	// trigger carries no target or mode placement ask. As with Conspire, the
	// trailing colon keeps the payload from aliasing the "__kwCascade" SVar a
	// printed bare K:Cascade line mints.
	if pt.Cascade {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		stackLen := len(e.G.Stack)
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "__kwCascade:", Text: "cascade ability"})
		if len(e.G.Stack) > stackLen {
			id := e.G.Stack[len(e.G.Stack)-1]
			if e.triggerContexts == nil {
				e.triggerContexts = make(map[state.ObjID]effects.TriggerContext)
			}
			e.triggerContexts[id] = pt.Ctx.TriggerContext
		}
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	// A granted flanking instance (CR 702.25a via a layer-6 AddKeyword$
	// Flanking -- Agility, Flanking Licid, Sidewinder Sliver, Cavalry
	// Master): the Ward/Conspire shape. A creature GRANTED flanking has no
	// printed K:Flanking trigger to carry the pump body, so the queue pushes
	// a KeywordTriggerPush whose __kwFlanking: payload events.Apply rebuilds
	// into the same DB$ Pump | Defined$ TriggeredBlockerLKICopy | NumAtt$ -1
	// | NumDef$ -1 body the printed expansion carries. The blocked creature
	// rides IDs as Remembered (the printed path's own slot, which
	// Defined$ TriggeredBlockerLKICopy reads), and the trailing colon keeps
	// the payload from aliasing the "__kwFlanking" SVar a printed bare
	// K:Flanking line mints.
	if pt.Flanking {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		ids := make([]state.ObjID, 0, len(pt.Ctx.Remembered))
		for _, tgt := range pt.Ctx.Remembered {
			if tgt.IsPlayer {
				ids = append(ids, state.PlayerRef(tgt.Player))
				continue
			}
			ids = append(ids, tgt.Obj)
		}
		stackLen := len(e.G.Stack)
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "__kwFlanking:", IDs: ids, Text: "flanking ability"})
		if len(e.G.Stack) > stackLen {
			id := e.G.Stack[len(e.G.Stack)-1]
			if e.triggerContexts == nil {
				e.triggerContexts = make(map[state.ObjID]effects.TriggerContext)
			}
			e.triggerContexts[id] = pt.Ctx.TriggerContext
		}
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	// A granted cumulative upkeep (CR 702.24a via a layer-6 AddKeyword$
	// Cumulative upkeep:<cost> or an A:AB$ Pump's KW$ Cumulative upkeep:<cost>
	// -- Breath of Dreams, Mana Chains, Decomposition, Balduvian Shaman,
	// Dreams of the Dead): the Ward/Afflict/Flanking shape. A permanent
	// granted the keyword has no printed K:Cumulative upkeep expansion
	// trigger, so the synthesized Phase trigger queues here and its Counter
	// payload __kwCumulativeUpkeepGranted:<cost> is what events.Apply
	// rebuilds into the same DB$ CumulativeUpkeep | Cost$ <cost> ability the
	// printed expansion carries. The trailing colon (and the "Granted"
	// suffix) keep the payload from aliasing the "__kwCumulativeupkeep:<cost>"
	// SVar a printed bare K:Cumulative upkeep line mints. The trigger has no
	// target roles, so no TriggerContext rides along.
	if pt.Cumulative != "" {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "__kwCumulativeUpkeepGranted:" + pt.Cumulative, Text: "cumulative upkeep ability"})
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	// A granted Mentor (CR 702.134 via a layer-6 AddKeyword$ Mentor -- Aegis
	// of the Legion's "Equipped creature has mentor", Nyxborn Unicorn's
	// Bestow aura): the Ward/Afflict shape, but the body is TARGETED. Its
	// Counter payload "__kwMentorGranted" is what events.Apply rebuilds into
	// the same DB$ PutCounter | ValidTgts$ Creature.attacking | Mentor$ True
	// targeted body the printed K:Mentor expansion carries
	// (cards/kw_mentor.go), and the target ask is posed from the walk's own
	// synthesized SA (the Gained/Granted branch shape) -- the offer and the
	// CR 608.2b recheck both read the Mentor$ marker through mentorAdmits
	// either way. The trigger is mandatory. The "Granted" suffix keeps the
	// payload from aliasing the "__kwMentor" SVar a printed bare K:Mentor
	// line mints (the Exploit/Offspring rule).
	if pt.Mentor {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		stackLen := len(e.G.Stack)
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "__kwMentorGranted", Text: "mentor ability"})
		if pt.SA != nil && len(e.G.Stack) > stackLen {
			id := e.G.Stack[len(e.G.Stack)-1]
			if e.triggerContexts == nil {
				e.triggerContexts = make(map[state.ObjID]effects.TriggerContext)
			}
			e.triggerContexts[id] = pt.Ctx.TriggerContext
			e.askTarget(pt.Controller, id, pt.SA)
			e.startTriggerSubTargets(id, pt.Controller, pt.SA)
		}
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	// A granted Firebending (CR 702.189a via a layer-6 AddKeyword$
	// Firebending:<N> -- Sozin's Comet's "Each creature you control gains
	// firebending 5 until end of turn", Fire Nation Palace's targeted grant):
	// the Ward/Afflict/Flanking shape. A creature granted firebending has no
	// printed K:Firebending trigger to carry the attack body, so the
	// synthesized Attacks trigger queues here and its Counter payload
	// __kwFirebendingGranted:<N> is what events.Apply rebuilds into the same
	// DB$ Mana | Produced$ R | Amount$ <N> | PersistentUntilEndOfCombat$
	// True body the printed K:Firebending expansion carries (cards/
	// kw_firebending.go). The trigger has no target roles, so no
	// TriggerContext rides along. The "Granted" suffix keeps the payload from
	// aliasing the "__kwFirebending:<N>" SVar a printed K:Firebending line
	// mints (the Exploit/Offspring rule).
	if pt.Firebending != "" {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "__kwFirebendingGranted:" + pt.Firebending, Text: "firebending ability"})
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	// A granted Exploit (CR 702.58a via a layer-6 AddKeyword$ Exploit --
	// Colonel Autumn's "Other legendary creatures you control have
	// exploit"): the Ward/Afflict shape. The trigger is optional in
	// EFFECT (its body poses the may-sacrifice election when it resolves),
	// not in the trigger itself, so it is pushed unconditionally; its
	// Counter payload "__kwExploitGranted" is what events.Apply rebuilds
	// into the same DB$ Sacrifice -> DB$ Exploit chain the printed K:Exploit
	// expansion carries. The trigger's Source is the granted creature that
	// just entered, which the marker half names as the exploiter.
	if pt.Exploit {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		stackLen := len(e.G.Stack)
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "__kwExploitGranted", Text: "exploit ability"})
		if len(e.G.Stack) > stackLen {
			id := e.G.Stack[len(e.G.Stack)-1]
			if e.triggerContexts == nil {
				e.triggerContexts = make(map[state.ObjID]effects.TriggerContext)
			}
			e.triggerContexts[id] = pt.Ctx.TriggerContext
		}
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	// A granted Offspring (CR 702.175a via a layer-6 AddKeyword$ Offspring --
	// Zinnia, Valley's Voice's "Creature spells you cast have offspring
	// {2}"): the Ward/Afflict/Exploit shape. The trigger's Counter payload
	// "__kwOffspringGranted" is what events.Apply rebuilds into the same
	// DB$ CopyPermanent | Defined$ Self | NumCopies$ Count$OffspringPaid |
	// SetPower$ 1 | SetToughness$ 1 body the printed K:Offspring expansion
	// carries, so live and replay mint identical objects from the event text
	// alone. The trigger's Source is the granted creature that just entered,
	// so the Count$OffspringPaid read resolves against its pay-time
	// provenance (0 for a plain cast, 1 for the paid additional cost).
	if pt.Offspring {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		stackLen := len(e.G.Stack)
		e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: "__kwOffspringGranted", Text: "offspring ability"})
		if len(e.G.Stack) > stackLen {
			id := e.G.Stack[len(e.G.Stack)-1]
			if e.triggerContexts == nil {
				e.triggerContexts = make(map[state.ObjID]effects.TriggerContext)
			}
			e.triggerContexts[id] = pt.Ctx.TriggerContext
		}
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	// One of the Ring emblem's four level abilities (CR 701.54c): the emblem
	// has no object in any zone and no face, so its stack object is minted by
	// a RingEmblemPush event whose "__ring:<level>" payload events.Apply
	// rebuilds the hand-built body from (the granted ward/afflict shape). The
	// ability is mandatory and targetless, so nothing here asks.
	if pt.RingEmblem > 0 {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		e.emit(events.Event{Kind: events.RingEmblemPush, Player: pt.Controller,
			Amount:  int32(pt.RingEmblem),
			Counter: "__ring:" + strconv.Itoa(pt.RingEmblem),
			Text:    ringEmblemLabel(pt.RingEmblem)})
		e.drainAwaitsTarget = e.Pending() != nil
		return
	}
	// Task 18: a Miracle offer is placed by casting the card for its miracle
	// cost, not by minting a triggered-ability stack object. castMiracle
	// verifies the card is still in the owner's hand, emits the reveal Note,
	// and begins the ordinary cast flow; it may pause on an X/target decision
	// of its own, which is exactly Task 7's drainAwaitsTarget continuation
	// (see castMiracle's doc). A Miracle trigger is NOT delayed, so the flag
	// order matters: a Miracle offer must reach castMiracle, never the
	// DelayedPush path below.
	if pt.Miracle {
		e.castMiracle(pt)
		return
	}
	// A static-grant's trigger (AddTrigger$ on a Mode$ Continuous static,
	// e.g. Hearthhull's "STATION 8+ Whenever you sacrifice a land"): its
	// stack object is minted through the GrantTriggerPush event, whose shape
	// is DelayedPush's minus the registration -- the fired event carries the
	// Execute$ SVar name (Counter) and the GRANTOR's object id (Amount; 0 for
	// the self-grant shape) for events.Apply to resolve from the grantor's
	// SVar table (the queue walk's replayability gate established that this
	// resolves to the exact body the granting face's table names), and the
	// ability receives the same CR 603.3c mode/target placement asks a
	// TriggerPush ability would.
	if pt.Gained {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		ids := make([]state.ObjID, 0, len(pt.Ctx.Remembered)+1)
		// IDs[0] is the foreign card (the event's own provenance slot); the
		// remembered targets follow. events.Apply resolves the ability from
		// IDs[0] and Amount, so the order is load-bearing.
		ids = append(ids, pt.GainedFrom)
		for _, tgt := range pt.Ctx.Remembered {
			if tgt.IsPlayer {
				ids = append(ids, state.PlayerRef(tgt.Player))
				continue
			}
			ids = append(ids, tgt.Obj)
		}
		stackLen := len(e.G.Stack)
		e.emit(events.Event{Kind: events.GainedTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Amount: int32(pt.Idx), Counter: pt.Execute,
			IDs: ids, Text: "gained trigger"})
		if pt.SA != nil && len(e.G.Stack) > stackLen {
			id := e.G.Stack[len(e.G.Stack)-1]
			if e.triggerContexts == nil {
				e.triggerContexts = make(map[state.ObjID]effects.TriggerContext)
			}
			e.triggerContexts[id] = pt.Ctx.TriggerContext
			e.recordTriggerLine(id, pt)
			announce(id, true)
		}
		e.drainAwaitsTarget = e.Pending() != nil && !e.drainAwaitsModes
		return
	}
	if pt.Granted {
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		ids := make([]state.ObjID, 0, len(pt.Ctx.Remembered))
		for _, tgt := range pt.Ctx.Remembered {
			if tgt.IsPlayer {
				ids = append(ids, state.PlayerRef(tgt.Player))
				continue
			}
			ids = append(ids, tgt.Obj)
		}
		stackLen := len(e.G.Stack)
		e.emit(events.Event{Kind: events.GrantTriggerPush, Player: pt.Controller,
			Obj: pt.Source, Counter: pt.Execute,
			Amount: int32(pt.Grantor), IDs: ids, Text: "granted trigger"})
		if pt.SA != nil && len(e.G.Stack) > stackLen {
			id := e.G.Stack[len(e.G.Stack)-1]
			if e.triggerContexts == nil {
				e.triggerContexts = make(map[state.ObjID]effects.TriggerContext)
			}
			e.triggerContexts[id] = pt.Ctx.TriggerContext
			e.recordTriggerLine(id, pt)
			announce(id, false)
		}
		e.drainAwaitsTarget = e.Pending() != nil && !e.drainAwaitsModes
		return
	}
	// A Mode$ Phase delayed trigger (CR 603.7): its stack object is minted by
	// a DelayedPush event rather than a TriggerPush. The difference is the
	// Ability: TriggerPush re-derives it from a face Triggers index, while a
	// delayed trigger's Effect is the Execute$ SVar-named sub-ability on the
	// source's face, so the fired event carries the Execute$ name (Counter)
	// for events.Apply to resolve. The ability has still been put on the
	// stack, however: it must receive the same CR 603.3c mode/target-placement
	// asks as a TriggerPush ability. Most Mode$ Phase delayed triggers do not
	// target (Flickerwisp re-derives its referent from
	// DelayTriggerRememberedLKI), but a Room's UnlockDoor trigger reaches its
	// alternate-face Execute$ SVar through this path and may target.
	// A mutated pile's under-card trigger (CR 702.140d) shares the delayed
	// shape -- the ability is minted inside events.Apply from data the log
	// carries -- but its event names the UNDER-CARD's pile index AND that
	// face's own Triggers index (packed into MergedTriggerPush's Amount), so
	// Apply mints the face's COMPILED trigger effect, the same pointer
	// f.Triggers[i].Effect an ordinary TriggerPush mints. A by-name SVar
	// resolution would be wrong twice over: the top face's same-named SVar
	// (Forge's canonical TrigToken) would steal the body -- the pile's top
	// card can be any non-Human creature, and Cubwarden under Everquill
	// Phoenix is the real collision pair -- and a freshly parsed SA has no
	// pointer identity with the compiled trigger, which is how
	// findTriggerForAbilityFace (and through it the OptionalDecider$ gate,
	// the intervening-if recheck, the ResolvedLimit$ count, the label and
	// the resolution-time SVar table) recovers the owning line.
	if pt.Delayed || pt.Merged > 0 {
		kind, amount, text := events.DelayedPush, int32(pt.DelayedID), "delayed trigger"
		if pt.Merged > 0 {
			kind, amount, text = events.MergedTriggerPush,
				events.MergedTriggerAmount(pt.Merged-1, pt.Idx), "merged trigger"
			if amount < 0 {
				return
			}
		}
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			return
		}
		ids := make([]state.ObjID, 0, len(pt.Ctx.Remembered))
		for _, tgt := range pt.Ctx.Remembered {
			if tgt.IsPlayer {
				ids = append(ids, state.PlayerRef(tgt.Player))
				continue
			}
			ids = append(ids, tgt.Obj)
		}
		stackLen := len(e.G.Stack)
		e.emit(events.Event{Kind: kind, Player: pt.Controller,
			Obj: pt.Source, Amount: amount, Counter: pt.Execute,
			IDs: ids, Text: text})
		if pt.SA != nil && len(e.G.Stack) > stackLen {
			id := e.G.Stack[len(e.G.Stack)-1]
			if e.triggerContexts == nil {
				e.triggerContexts = make(map[state.ObjID]effects.TriggerContext)
			}
			// An event-matched registration's fired ability carries the same
			// event provenance a face trigger's stack object does (Chancellor
			// of the Annex's counter reads the triggering spell's roles). A
			// Mode$ Phase registration's context is the zero value, so storing
			// it is behaviourally the absence every Phase delayed trigger
			// read before.
			e.triggerContexts[id] = pt.Ctx.TriggerContext
			// An Effect-created trigger body's source-scoped frame, so the
			// one-shot self-exile idiom it may run ends the Effect. Mirrors the
			// static fire arm, which resolves with the frame on its Ctx.
			if pt.Ctx.EffectFrame.Source != 0 {
				if e.triggerEffectFrames == nil {
					e.triggerEffectFrames = make(map[state.ObjID]effects.EffectFrame)
				}
				e.triggerEffectFrames[id] = pt.Ctx.EffectFrame
			}
			e.bindReflexiveContext(id, &pt)
			e.recordTriggerLine(id, pt)
			announce(id, false)
		}
		e.drainAwaitsTarget = e.Pending() != nil && !e.drainAwaitsModes
		return
	}
	// CR 800.4a / Ruling U6, fix round 2 (re-review N2): an ability
	// controlled by a player who has left the game ceases to exist, so it is
	// never minted. dropDepartedTriggers enforces this for the queue, but it
	// only runs inside putTriggersOnStack -- it cannot run while a decision
	// is pending, and an optional trigger's DECIDER may be a different,
	// living seat (OptionalDecider$ TriggeredCardController, 40 T: lines in
	// the corpus). So a decider could answer yes for a controller who had
	// been eliminated in the meantime and resurrect their ability. This is
	// the check placed at the one point a triggered ability's stack object is
	// created, rather than at each of the paths that reach it.
	if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
		return
	}
	// FL-41: Remembered can hold a player entry now (triggerRemembered's
	// DeclareAttackers case appends the defending player), but IDs is an
	// []ObjID -- there is no field here for a bare PlayerID. Encoding it
	// with state.PlayerRef instead of just writing tgt.Obj (always 0 for a
	// player target) is what lets events.Apply's TriggerPush case below
	// reconstruct {Player: p, IsPlayer: true} rather than {Obj: 0} -- which
	// playersOf (effects/context.go) would filter out, so Defined$
	// TriggeredDefendingPlayer would resolve to nothing and the effect
	// silently no-op (PlayerOf is never reached).
	ids := make([]state.ObjID, 0, len(pt.Ctx.Remembered))
	for _, tgt := range pt.Ctx.Remembered {
		if tgt.IsPlayer {
			ids = append(ids, state.PlayerRef(tgt.Player))
			continue
		}
		ids = append(ids, tgt.Obj)
	}
	stackLen := len(e.G.Stack)
	e.emit(events.Event{Kind: events.TriggerPush, Player: pt.Controller,
		Obj: pt.Source, Amount: int32(pt.Idx), IDs: ids, Text: "triggered ability"})
	if len(e.G.Stack) > stackLen {
		id := e.G.Stack[len(e.G.Stack)-1]
		if e.triggerContexts == nil {
			e.triggerContexts = make(map[state.ObjID]effects.TriggerContext)
		}
		e.triggerContexts[id] = pt.Ctx.TriggerContext
		if pt.Ctx.LKI != nil {
			if e.triggerLKI == nil {
				e.triggerLKI = make(map[state.ObjID]triggerObjectLKI)
			}
			lki := pt.Ctx.LKI.CloneDeep()
			e.triggerLKI[id] = triggerObjectLKI{object: &lki,
				power: pt.Ctx.Snap.Power, toughness: pt.Ctx.Snap.Toughness,
				ptValid: pt.Ctx.Snap.PTValid}
		}
		if pt.Ctx.Snap.SourceLifelinkValid {
			if e.sourceLifelinkLKI == nil {
				e.sourceLifelinkLKI = make(map[state.ObjID]bool)
			}
			e.sourceLifelinkLKI[id] = pt.Ctx.Snap.SourceLifelink
		}
		if pt.Ctx.Snap.SourceControllerValid {
			if e.sourceControllerLKI == nil {
				e.sourceControllerLKI = make(map[state.ObjID]state.PlayerID)
			}
			e.sourceControllerLKI[id] = pt.Ctx.Snap.SourceController
		}
		if pt.sourceCharLKIValid {
			e.setSourceCharLKI(id, pt.sourceCharLKI)
		}
		if pt.Ctx.Snap.DamageSource != nil {
			if e.damageSourceLKI == nil {
				e.damageSourceLKI = make(map[state.ObjID]map[state.ObjID]effects.DamageSourceLKI)
			}
			e.damageSourceLKI[id] = cloneDamageSourceLKI(pt.Ctx.Snap.DamageSource)
		}
		emitAbilityTriggered(e.G, e.triggerOf, e.emit, pt)
	}
	// Task 7: a trigger that declares ValidTgts$ asks its controller for
	// targets RIGHT AFTER its TriggerPush -- the ability object is now top of
	// stack, and the choice is asked before any player receives priority. The
	// drain records drainAwaitsTarget (Clone copies it) so handleTarget knows
	// to resume the drain through the SAME continuation handleTriggerOrder
	// uses rather than granting priority; putTriggersOnStack sees the new
	// pending decision and returns true. If the TriggerPush no-opped (Apply's
	// own guard), the stack never grew and there is no ability object to
	// target for -- skip the ask. askTarget itself may decline to ask (its
	// TargetMin$ 0 / fizzle paths), which is why the flag is derived from
	// e.Pending(), not from "we wanted to ask". SA is nil only for a
	// hand-seeded queue entry (clone_test's seeded fake), which never has
	// ValidTgts$ -- mirror the nil-tolerance the TriggerPush out-of-range
	// guard already provides. CR 603.3c: a modal triggered ability announces its mode choice when it
	// is put on the stack, not at resolution. That ask is posed here too,
	// in preference to the target ask for a trigger whose effect carries
	// both a Choices$ clause and a ValidTgts$ (the modal shape a Charm
	// commonly pairs with target selection INSIDE its modes, not on the
	// ability itself -- a top-level ValidTgts$ alongside Choices$ is not
	// exercised by the corpus). The answered modes are recorded on the
	// stack object (ChosenModes) and the drain resumes through handleModes,
	// the same continuation shape.
	if pt.SA != nil && len(e.G.Stack) > 0 &&
		e.G.Obj(e.G.Stack[len(e.G.Stack)-1]) != nil {
		id := e.G.Stack[len(e.G.Stack)-1]
		announce(id, false)
	}
	// Fix round 1 (reviewer minor, cheap): derive drainAwaitsTarget from
	// e.Pending() rather than clearing it first. The old form set it false
	// up front and recomputed it only inside the ask branch, so a flag an
	// OUTER caller had already set and not yet consumed was discarded the
	// moment pushTrigger reached here without asking a target of its own.
	// Deriving it from Pending() after the optional ask preserves a
	// pre-existing true and recomputes the one decision the drain was paused
	// on otherwise; askTarget may decline to ask (its TargetMin$ 0 / fizzle
	// paths), which is exactly why the flag must come from the resulting
	// pending state rather than from "we wanted to ask".
	e.drainAwaitsTarget = e.Pending() != nil && !e.drainAwaitsModes
}

// triggerOf re-reads the T: line a pending trigger came from, so nothing has
// to be cached on pendingTrigger for it.
//
// Fix round 1, review finding F5: the guarantee is narrower than the previous
// wording claimed. A parsed cards.Trigger is static text, but this reads
// Obj.Face(), which is NOT static -- it follows the object's ACTIVE face. A
// permanent whose face changed between the trigger matching and the drain
// would be read against the new face's Triggers slice, giving the wrong
// optionality flag and label. Nothing in M1 can reach that (effects.SetState
// is the only FlipFace source and no card in play uses it), but this is
// "correct because nothing flips faces", not "correct because the data cannot
// change". A source that has ceased to exist, or an index the current face is
// too short for, reports false rather than panicking.
func (e *Engine) triggerOf(pt pendingTrigger) (cards.Trigger, bool) {
	o := e.G.Obj(pt.Source)
	if o == nil {
		return cards.Trigger{}, false
	}
	// A mutated pile's under-card trigger is keyed to the UNDER-CARD's face:
	// pt.Idx indexes that face's Triggers, and reading the TOP face at the
	// same index would hand back a different trigger line entirely (both its
	// description and its OptionalDecider$) -- the same top-face steal the
	// MergedTriggerPush body resolution exists to prevent.
	var f *cards.Face
	if pt.Merged > 0 {
		f = o.MergedFaceAt(pt.Merged - 1)
	} else {
		f = o.Face()
	}
	if f == nil || pt.Idx < 0 || pt.Idx >= len(f.Triggers) {
		return cards.Trigger{}, false
	}
	return f.Triggers[pt.Idx], true
}

// findTriggerForAbility returns the face trigger on source whose Effect is
// exactly the given *cards.SA -- the T: line a triggered-ability stack object
// came from. A triggered ability's stack object carries the same compiled SA
// pointer its source's Triggers entry holds (events.Apply re-derives it from
// the TriggerPush's Idx), so pointer equality identifies the line. This is
// what lets resolution (resolveTop's ability branch) re-read the trigger's
// CR 603.4 intervening-if condition and optionality (CR 603.5) from the
// source's static text rather than caching them on the stack object.
//
// It returns false for an activated ability, whose o.Ability comes from the
// source's Abilities slice rather than a Triggers entry, and for any source
// whose face has since changed or gone -- so a caller must treat false as
// "this is not a face trigger, apply no trigger-only rule" rather than as an
// error. A source that has ceased to exist entirely (a token or copy gone
// from the board) degrades the same way.
// triggerPaidX implements CR 107.3m's X binding for a triggered ability that
// is already a stack object: the value of X in its text is the X chosen for
// the spell that became the permanent it is on (an ETB trigger of a cast
// creature -- Wan Shi Tong) or the X of the spell it triggered on (a cast
// trigger -- Hydroid Krasis, Genesis Hydra; a magecraft trigger on another
// permanent -- Zaxara's "put X +1/+1 counters" reads the triggering spell).
// The trigger object itself was never paid an X (events.Apply's TriggerPush
// records the trigger index in Amount, and commitCast emits no CastInfo for
// it), so its own o.X is 0. The causing event's card contributes the value
// captured in TriggerContext.TriggerPaidX when the trigger matched. It must
// not be read from the card at resolution: an ETB permanent can have died or
// been bounced in the meantime, and events.Move correctly clears its live X.
//
// An activated ability never falls back: CR 107.3i gives its X only from the
// {X} paid for the activation itself, recorded on the ability object by
// CastInfo -- findTriggerForAbility returns false for one, so the early exit
// below is what keeps a Walking Ballista's ability from inheriting its own
// cast-time X.
//
// The trigger context is engine-only (rules.pushTrigger, keyed by stack id),
// and a Mode$ Phase delayed trigger -- pushed via DelayedPush, with no
// context -- reads 0 here, its status quo.
func (e *Engine) triggerPaidX(stack state.ObjID, o *state.Object) int32 {
	if o == nil || o.Ability == nil {
		return 0
	}
	if tc, ok := e.triggerContexts[stack]; ok && tc.Reflexive {
		// A reflexive ability (CR 603.12) reads the X its SPAWNING ability
		// was paid ("you may pay {X}. When you do, it deals X damage"),
		// captured by QueueReflexiveTrigger (rules/reflexive.go).
		return tc.TriggerPaidX
	}
	if _, ok := e.triggerForAbilityObject(stack, o); !ok {
		return 0
	}
	tc, ok := e.triggerContexts[stack]
	if !ok {
		return 0
	}
	return tc.TriggerPaidX
}

func (e *Engine) findTriggerForAbility(source state.ObjID, sa *cards.SA) (cards.Trigger, bool) {
	t, _, ok := e.findTriggerForAbilityFace(source, sa)
	return t, ok
}

// findTriggerForAbilityFace is findTriggerForAbility plus the face that owns
// the matched trigger. A mutated pile's under-card trigger lives on a MERGED
// face (CR 702.140d), so the scan walks the top face first and then every
// merged face -- pointer equality identifies the line wherever it lives, and
// callers that label the ability (abilityLabel) need the owning face's name,
// never the pile's top face's.
func (e *Engine) findTriggerForAbilityFace(source state.ObjID, sa *cards.SA) (cards.Trigger, *cards.Face, bool) {
	if sa == nil {
		return cards.Trigger{}, nil, false
	}
	o := e.G.Obj(source)
	if o == nil {
		return cards.Trigger{}, nil, false
	}
	f := o.Face()
	if f == nil {
		return cards.Trigger{}, nil, false
	}
	for _, t := range f.Triggers {
		if t.Effect == sa {
			return t, f, true
		}
	}
	for i := range o.MergedCards {
		mf := o.MergedFaceAt(i)
		if mf == nil {
			continue
		}
		for _, t := range mf.Triggers {
			if t.Effect == sa {
				return t, mf, true
			}
		}
	}
	// A has-all-abilities-of GRANTED trigger (Forge's GainsTriggerAbsOf$): the
	// resolving body is a compiled trigger on a FOREIGN card's face, so the
	// owning face -- and therefore the SVar table, OptionalDecider$ gate,
	// intervening-if recheck and label every consumer reads -- is that foreign
	// face, not the recipient's. Measured against the minted provenance first
	// -- the resolving wrapper sits on the stack carrying the exact foreign
	// face events.Apply stamped (gainedOwnedFace), which survives the
	// granting static ending between the queue and the resolution, the case
	// the live-grant scan below cannot answer -- and only then against the
	// live grants (a grant that ended with its static is no owner), in
	// active()'s deterministic order. Any stack wrapper with this (source,
	// ability) pair carries the same provenance -- the foreign face is a
	// property of the compiled SA pointer, not of the instance -- so the scan
	// is deterministic and instance-independent. A gained ACTIVATED ability is
	// deliberately not matched here: it has no Trigger to return, and
	// pileFaceForSA is its recovery point.
	for _, wid := range e.G.Stack {
		wo := e.G.Obj(wid)
		if wo == nil || wo.Ability != sa || wo.Source != source {
			continue
		}
		if f := gainedOwnedFace(wo); f != nil {
			for i := range f.Triggers {
				if f.Triggers[i].Effect == sa {
					return f.Triggers[i], f, true
				}
			}
		}
	}
	for _, gf := range e.gainedFacesForSource(source) {
		if gf.Face == nil {
			continue
		}
		for _, t := range gf.Face.Triggers {
			if t.Effect == sa {
				return t, gf.Face, true
			}
		}
	}
	return cards.Trigger{}, nil, false
}

// recordTriggerLine stores the identity of a freshly minted triggered-ability
// stack object (see Engine.triggerLines). It is a no-op for a printed trigger,
// whose line the pointer scan already recovers, so every existing push is
// unchanged. A Saga chapter stores an empty trigger marker: its SVar body is a
// trigger, but it has no T: line. pushTrigger calls this from the Gained,
// Granted and Delayed arms -- the paths that mint an SVar body rather than a
// compiled Face.Triggers entry.
func (e *Engine) recordTriggerLine(id state.ObjID, pt pendingTrigger) {
	if pt.Trigger.Mode == "" && !pt.Chapter {
		return
	}
	if e.triggerLines == nil {
		e.triggerLines = make(map[state.ObjID]cards.Trigger)
	}
	e.triggerLines[id] = pt.Trigger
	if pt.TriggerSVars != nil {
		if e.triggerLineSVars == nil {
			e.triggerLineSVars = make(map[state.ObjID]map[string]string)
		}
		e.triggerLineSVars[id] = pt.TriggerSVars
	}
}

// triggerForAbilityObject resolves the trigger line for an ability stack
// object. A granted (AddTrigger$) or delayed (Effect Triggers$) body is an
// SVar-named *cards.SA and cards.ResolveSVar parses a FRESH pointer on every
// call, so pointer identity against Face.Triggers can never match one; the
// line pushTrigger recorded on Engine.triggerLines is the exact identity.
// Everything else -- a printed, merged or has-all-abilities-of trigger --
// falls through to the pointer scan, exactly as before.
//
// Every caller that then reads a trigger-only clause (OptionalDecider$,
// Cost$, ResolvedLimit$, Condition$, TriggerPaidX) must go through this so a
// granted trigger is not treated as a non-trigger and skipped.
func (e *Engine) triggerForAbilityObject(id state.ObjID, o *state.Object) (cards.Trigger, bool) {
	if o == nil || o.Ability == nil {
		return cards.Trigger{}, false
	}
	if t, ok := e.triggerLines[id]; ok {
		return t, true
	}
	return e.findTriggerForAbility(o.Source, o.Ability)
}

// faceOwningTrigger returns the face of source that carries t: its top face
// when t is an ordinary printed trigger, or the merged face beneath it when t
// belongs to a card stacked under a mutated pile's top card (CR 702.140d).
// The compiled Effect pointer is the identity -- cards.Link parses one *SA per
// T: line, so no two lines share it -- and a trigger with no compiled body has
// nothing to run and no face to name. nil means "not found"; callers keep
// whatever they read from the top face, which for every non-merged object is
// the same face this would return.
func (e *Engine) faceOwningTrigger(source state.ObjID, t cards.Trigger) *cards.Face {
	if t.Effect == nil {
		return nil
	}
	_, f, ok := e.findTriggerForAbilityFace(source, t.Effect)
	if !ok {
		return nil
	}
	return f
}

// optionalDecider reports whether pt is an optional trigger, which seat gets
// the yes/no question, and whether that seat can still answer it.
//
// The spelling is Forge's, established by grepping .cards/cardsfolder rather
// than guessed (the precedent for not guessing is Ruling T12-a). On a T: line
// optionality is spelled OptionalDecider$ and nothing else: 1496 T: lines
// carry it, and the bare Optional$ form -- 1143 occurrences (SVar 884, A 225,
// R 19, S 15) -- never once appears on a T: line, because it is a different
// thing (a "you may" inside an ability's own resolution, not a choice about
// whether the trigger is put on the stack). The parser needs no change to
// carry it: parseParams already collects every Key$ value on the line into
// Trigger.Params.
//
// "Bare" means the anchored pattern (^|[^A-Za-z])Optional$ and is load-
// bearing in the count: the unanchored substring gives 1199, because Forge
// also has RevealOptional$ (39), ChoiceOptional$ (10) and RepeatOptional$
// (7). Three separate counts were produced for this figure across two review
// rounds -- 1141, 1199 and 1143 -- before it was settled by anchoring the
// pattern. The load-bearing numbers above (1496, and zero on any T: line)
// were exact every time.
//
// The decider values that actually occur on T: lines, with counts:
//
//	You                        1441   the trigger's controller
//	TriggeredCardController      40   controller of the triggering card
//	TriggeredSourceController     5   controller of the triggering source
//	TriggeredPlayer               3   the player the event was about
//	EnchantedController           3   controller of the enchanted permanent
//	TriggeredAttackingPlayer      2   the attacking player
//	TriggeredActivator            2   who activated the triggering ability
//
// So the answer to "is the decider ever someone other than the controller" is
// yes, and it is honoured for the two forms this engine can actually resolve:
// TriggeredCardController and TriggeredSourceController both read the
// controller of the object the trigger remembered, which is the object the
// triggering event was about (triggerRemembered, above) -- the same object
// Forge means by both names in every T: line in the corpus that uses them.
//
// CAVEAT (Task 6, fix round 1): that equivalence assumes Remembered[0] is
// the object the FIRING trigger's own T: line is on, which triggerRemembered
// no longer guarantees for a DeclareAttackers-driven Attacks trigger --
// Remembered there is every attacker declared against one defending player
// (handleAttackers emits one event per defender since Task m34), in event
// order, so Remembered[0] is whichever creature attacking that defender was
// declared first, not necessarily pt.Source. No repo-deck T: line combines
// Mode$ Attacks with OptionalDecider$ TriggeredCardController/
// TriggeredSourceController today (measured against the same corpus the
// table above was), so this is a latent gap, not a live one -- but a future
// card that did would have this read the wrong creature's controller
// whenever it attacks alongside another creature that happened to be
// declared first.
//
// LIMITATION, stated rather than assumed: the remaining ten T: lines
// (TriggeredPlayer, EnchantedController, TriggeredAttackingPlayer,
// TriggeredActivator -- 0.7% of the corpus's optional triggers, and none of
// them in a mode this milestone implements) name a player this engine's
// pendingTrigger cannot derive, because events.Event carries no attacking
// player, no activator and no enchant link. Those fall back to asking the
// controller. That still satisfies R2 -- a human is asked, and no outcome is
// assumed -- but it can ask the wrong human, so it is a real gap and not a
// silent approximation. Closing it needs the trigger to capture the player at
// match time, which is a change to what checkTriggers records.
func (e *Engine) optionalDecider(pt pendingTrigger) (who state.PlayerID, optional, askable bool) {
	// Task 18: a Miracle offer is always optional and its decider is always the
	// owner (the controller of the drawn card). It has no T: line to read, so
	// this must be special-cased before triggerOf (which would fail for it).
	// Evoke and Madness are mandatory follow-ups with no placement question;
	// Madness asks whether to cast only at resolution.
	if pt.Miracle {
		who = pt.Controller
		if int(who) >= len(e.G.Players) || e.G.Players[who].Lost {
			return who, true, false
		}
		return who, true, true
	}
	// The Ring emblem's level abilities are mandatory "whenever" triggers
	// (CR 701.54c): there is no placement question and no resolution
	// question, so this returns not-optional before triggerOf, which would
	// fail for an entry that has no face.
	if pt.RingEmblem > 0 {
		return 0, false, false
	}
	if pt.Evoke || pt.Gift || pt.Madness {
		return 0, false, false
	}
	t, ok := e.triggerOf(pt)
	if !ok {
		return 0, false, false
	}
	spec := triggerOptionalSpec(t)
	if spec == "" {
		return 0, false, false
	}
	who, askable = e.deciderFromSpec(spec, pt.Controller, pt.Ctx.Remembered, pt.Ctx.TriggerContext)
	return who, true, askable
}

// deciderFromSpec resolves an OptionalDecider$ spec string to the seat that
// answers the yes/no, plus whether that seat can still answer it. It is the
// one place the spec grammar lives, shared by the placement path (which has
// a pendingTrigger) and the CR 603.5 resolution path (rules/stack.go's
// resolveTop, which has the stack object's own controller and Remembered
// rather than a pendingTrigger -- the two carry the same controller and the
// same Remembered objects, so the decider is re-derived identically).
// controller is the ability's controller and remembered the objects the
// trigger captured; the returned askable is false when the decider has left
// the game.
func (e *Engine) deciderFromSpec(spec string, controller state.PlayerID, remembered []state.Target, tc effects.TriggerContext) (who state.PlayerID, askable bool) {
	who = controller
	switch deciderFromSpecCodes.Code(string(spec)) {
	case deciderFromSpecYou:
		// The controller, which who already is.
	case deciderFromSpecTriggeredCardController:
		// The shared resolver: a card that left the battlefield is its
		// last-known controller's (Fecundity on a stolen creature's death).
		if p, ok := effects.TriggeredCardController(e.G, tc, remembered); ok {
			who = p
		}
	case deciderFromSpecTriggeredSourceController:
		if len(remembered) > 0 {
			if o := e.G.Obj(remembered[0].Obj); o != nil {
				who = o.Controller
			}
		}
	}
	if int(who) >= len(e.G.Players) || e.G.Players[who].Lost {
		return who, false
	}
	return who, true
}

// targetChooserFromSpec resolves a trigger-relative target chooser. Unknown or
// absent referents fail closed to the caller's controller. The Opponent /
// Player.Opponent form is NOT resolved here: it is the multi-opponent
// selection shape (targetChooserCore / opponentChooser in rules/stack.go),
// which consults the controller's which-opponent selection pin and counts
// living opponents before a target ask is re-posed.
func (e *Engine) targetChooserFromSpec(spec string, controller state.PlayerID, remembered []state.Target, tc effects.TriggerContext) (state.PlayerID, bool) {
	var target state.Target
	switch targetChooserFromSpecCodes.Code(string(strings.TrimSpace(spec))) {
	case targetChooserFromSpecTriggeredTarget:
		target = tc.TriggerTarget
	case targetChooserFromSpecTriggeredPlayer:
		target = tc.TriggerPlayer
	case targetChooserFromSpecTriggeredDefendingPlayer:
		target = tc.DefendingPlayer
	case targetChooserFromSpecTriggeredAttackingPlayer:
		target = tc.AttackingPlayer
	case targetChooserFromSpecTriggeredCardController:
		if p, ok := effects.TriggeredCardController(e.G, tc, remembered); ok {
			return p, e.targetChooserAlive(p)
		}
		return controller, false
	// The Opponent / Player.Opponent form is handled by targetChooserCore,
	// the one home for that shape: this resolver only resolves
	// trigger-relative referents. (The Opponent branch was here until the
	// controller-selection ask was added -- agent-20260925T085158Z-c861188d.)
	default:
		return controller, false
	}
	if !target.IsPlayer || !e.targetChooserAlive(target.Player) {
		return controller, false
	}
	return target.Player, true
}

func (e *Engine) targetChooserAlive(p state.PlayerID) bool {
	// PlayerID is uint8: negative referents cannot reach this function. A
	// converted negative integer becomes a large unsigned ID and fails the
	// upper bound before indexing Players.
	return int(p) < len(e.G.Players) && !e.G.Players[p].Lost
}

// PendingTriggers reports the triggers matched but not yet on the stack, in
// queue order (index 0 is placed first). Read-only; the slice is fresh, and
// so is each entry's own Label -- neither aliases e.pendingTriggers, so a
// caller (view.Project) mutating what it gets back cannot corrupt the drain.
//
// Ruling F2/R3: this is the whole of what lets view describe the trigger
// queue without importing rules -- state.PendingTrigger is the shared
// vocabulary, built here from the same triggerLabel and optionalDecider the
// drain itself (putTriggersOnStack, above) already uses, so a client's view
// of "what's about to hit the stack" can never disagree with what actually
// does.
func (e *Engine) PendingTriggers() []state.PendingTrigger {
	if len(e.pendingTriggers) == 0 {
		return nil
	}
	out := make([]state.PendingTrigger, 0, len(e.pendingTriggers))
	for _, pt := range e.pendingTriggers {
		who, optional, _ := e.optionalDecider(pt)
		out = append(out, state.PendingTrigger{
			Source:     pt.Source,
			Controller: pt.Controller,
			Label:      e.triggerLabel(pt),
			Optional:   optional,
			Decider:    who,
		})
	}
	return out
}

// StackOptional reports whether a stack object is an optional triggered
// ability whose resolution-time yes/no (CR 603.5) has not yet been answered,
// and which seat answers it. Ruling VW-1: the optionality belongs on the
// stack entry, because under CR 603.5 the ability is already on the stack
// when the question is posed, so the queue is empty at that moment. It
// reuses the same OptionalDecider$ read and deciderFromSpec derivation as
// the placement and resolution paths rather than re-deriving the spec
// grammar — the view asks for the answer through view.Chars.
//
// It reports not-optional for anything that is not a face trigger (an
// activated ability, or a source whose face has since changed or gone), and
// for a trigger whose decider has left the game — a decider who is nobody
// has already ceased to exist (CR 800.4a), so such an object is never
// observed awaiting its question on the stack.
func (e *Engine) StackOptional(id state.ObjID) (optional bool, decider state.PlayerID) {
	o := e.G.Obj(id)
	if o == nil || o.Ability == nil {
		return false, 0
	}
	spec := ""
	if t, ok := e.findTriggerForAbility(o.Source, o.Ability); ok {
		spec = triggerOptionalSpec(t)
	} else {
		// An Effect-created delayed trigger: no face T: line, so its
		// OptionalDecider$ spec rides the registration's referent context
		// (the same fallback resolveTop's optional gate uses).
		spec = e.triggerContexts[id].OptionalSpec
	}
	if spec == "" {
		return false, 0
	}
	who, askable := e.deciderFromSpec(spec, o.Controller, o.Remembered, e.triggerContexts[id])
	if !askable {
		return false, 0
	}
	return true, who
}

// triggerLabel is what a client shows for one pending trigger. The card's own
// TriggerDescription$ is the text a real player would recognise; the source's
// name disambiguates two copies of the same card.
func (e *Engine) triggerLabel(pt pendingTrigger) string {
	// Task 18: a Miracle offer is named by the card and its miracle cost, not by
	// a TriggerDescription$ (there is no T: line). This is the label the brief's
	// interface spells -- "Miracle — reveal <name> and cast it for <cost>?" --
	// and it is what askTriggerOptional shows inside its offer prompt.
	// Madness and Evoke are mandatory keyword-triggered abilities; their labels
	// may appear in an ordering ask beside ordinary simultaneous triggers.
	// The emblem's own label, before triggerOf -- an emblem entry has no face
	// to read a TriggerDescription$ from.
	if pt.SpeedIncrease {
		return "Speed: whenever an opponent loses life during your turn"
	}
	if pt.RingEmblem > 0 {
		return ringEmblemLabel(pt.RingEmblem)
	}
	if pt.Ward != "" {
		name := "a permanent"
		if o := e.G.Obj(pt.Source); o != nil {
			if f := o.Face(); f != nil && f.Name != "" {
				name = f.Name
			}
		}
		return name + ": ward (" + pt.Ward + ")"
	}
	if pt.Cascade {
		name := "a spell"
		if o := e.G.Obj(pt.Source); o != nil {
			if f := o.Face(); f != nil && f.Name != "" {
				name = f.Name
			}
		}
		return name + ": cascade"
	}
	if pt.Casualty {
		return "casualty copy trigger"
	}
	if pt.Conspire {
		name := "a spell"
		if o := e.G.Obj(pt.Source); o != nil {
			if f := o.Face(); f != nil && f.Name != "" {
				name = f.Name
			}
		}
		return name + ": conspire copy trigger"
	}
	if pt.Demonstrate {
		name := "a spell"
		if o := e.G.Obj(pt.Source); o != nil {
			if f := o.Face(); f != nil && f.Name != "" {
				name = f.Name
			}
		}
		return name + ": demonstrate copy trigger"
	}
	if pt.Gift {
		name := "it"
		if o := e.G.Obj(pt.Source); o != nil && o.Face() != nil && o.Face().Name != "" {
			name = o.Face().Name
		}
		return name + ": gift"
	}
	if pt.Miracle || pt.Madness || pt.Evoke {
		name := "it"
		if o := e.G.Obj(pt.Source); o != nil {
			if f := o.Face(); f != nil && f.Name != "" {
				name = f.Name
			}
		}
		if pt.Evoke {
			return name + ": sacrifice it (evoked)"
		}
		if pt.Madness {
			return name + ": madness cast-or-graveyard trigger"
		}
		head, verb := "Miracle", "reveal "
		o := e.G.Obj(pt.Source)
		cost := ""
		if o != nil && o.Face() != nil {
			if c, ok := o.Face().KeywordParam(head); ok {
				cost = c
			}
		}
		return head + " — " + verb + name + " for " + cost + "?"
	}
	name := "Triggered ability"
	if o := e.G.Obj(pt.Source); o != nil {
		if f := o.Face(); f != nil && f.Name != "" {
			name = f.Name
		}
	}
	if t, ok := e.triggerOf(pt); ok {
		if d := t.ParamStr(cards.PKTriggerDescription); d != "" {
			return name + ": " + d
		}
	}
	return name
}

// abilityLabel is the resolution-side sibling of triggerLabel: the label a
// client sees for a triggered-ability stack OBJECT (which carries no
// pendingTrigger to read a queue entry off), built from the source's name
// and the trigger's own TriggerDescription$ -- the same text triggerLabel
// shows for the queued trigger it came from.
func (e *Engine) abilityLabel(o *state.Object, t cards.Trigger) string {
	name := "Triggered ability"
	if src := e.G.Obj(o.Source); src != nil {
		if f := src.Face(); f != nil && f.Name != "" {
			name = f.Name
		}
		// A mutated pile's under-card ability is labelled with the UNDER-CARD's
		// own name: the ability belongs to the card beneath the top card
		// (CR 702.140d), and the pile's top face can be any creature. A
		// HAS-ALL-ABILITIES-OF trigger (r3) is covered inside the recovery
		// function itself, so the foreign face's own name wins here too.
		if _, mf, ok := e.findTriggerForAbilityFace(o.Source, t.Effect); ok && mf != nil && mf.Name != "" {
			name = mf.Name
		}
	}
	if desc := t.ParamStr(cards.PKTriggerDescription); desc != "" {
		return name + ": " + desc
	}
	return name
}

// askTriggerModes is CR 603.3c: a modal triggered ability's controller
// announces the mode choice when putting the ability on the stack, not at
// resolution. It poses the same KModes decision effCharm would, but at
// placement, with ResumeKind/ResumeSA set so the answer's handler records
// the chosen SVar names onto the stack object (handleModes' placement
// branch) rather than re-entering a suspended resolution.
//
// The placement context has the triggering source, its SVar table, and the
// trigger controller, which is enough for effects.Num to resolve the same
// literal, SVar, and inline Count$ bounds as spell announcement and
// resolution. The option list mirrors effCharm's -- Choices$ order,
// SpellDescription$ as the label, resolved from the trigger's source SVar
// table -- so an index chosen here maps to the same SVar name modeChoiceNames
// produces at resolution.
//
// The ask exists for the MODAL family: its Choices$ values are SVar names
// that resolve to ability bodies (the Charm contract effCharm itself runs).
// Other APIs' Choices$ mean something else entirely -- a card spec
// (PutCounter's distribution pick, Clone's template, ChooseCard's filter), a
// player filter (ChoosePlayer), a ballot (Vote), a colour word (Protection)
// -- and their choice is asked at resolution through the primitive's own
// machinery. The structural discriminator is the contract itself: every
// Choices$ value must resolve, in the source face's own SVar table, to an
// ability body. Measured at the corpus pin, every Charm (240), Vote (11),
// GenericChoice (48) and VillainousChoice (5) trigger body resolves; every
// non-modal carrier (ChooseCard 93, ChoosePlayer 29, PutCounter 10,
// CopyPermanent 11, Clone 4, Attach, Manifest, and the rest) names at least
// one unresolvable spec, so the old unconditional ask recorded a bogus
// ChosenModes the primitive never read (Vastwood Hydra's death trigger asked
// the controller to "choose one mode" labelled Creature.YouCtrl before the
// distribution pick ever came). It returns whether the placement ask was
// posed, so the drain knows whether to wait for a modes answer
// (drainAwaitsModes) and whether to fall through to the target ask.
func (e *Engine) askTriggerModes(p state.PlayerID, obj state.ObjID, sa *cards.SA) bool {
	// VillainousChoice is not CR 603.3c modal placement: its Defined$
	// player chooses during resolution. Let the registered effect own the
	// ask so the victim, rather than the trigger controller, is the chooser
	// and the victim can be captured in Ctx.Remembered first.
	if sa.API == "VillainousChoice" {
		return false
	}
	// GenericChoice with a Defined$ other than the trigger controller is the
	// same non-modal shape: its chooser is chosen at RESOLUTION
	// (charmGenericPlayers asks each named player in turn and binds it as
	// Ctx.Remembered), so a CR 603.3c placement ask would pose the mode to
	// the wrong player and then run one body unbound before the per-chooser
	// asks. `Defined$ You` and an ABSENT Defined$ still resolve to the
	// controller alone, and charmGenericPlayers declines both -- the
	// placement ask must be retained for those. Returning false is this
	// function's "not modal: the primitive asks at resolution" verdict; the
	// drain falls through to the target ask exactly as it does for
	// VillainousChoice.
	if sa.API == "GenericChoice" {
		defined := effects.CharmOf(sa).Defined
		if (defined != "" && defined != "You") || effects.GenericChoicePlayerChoosers(sa) {
			return false
		}
		// param:api:GenericChoice.AtRandom: the engine picks at resolution
		// (effects.GenericChoiceAtRandom), never at placement, so the rng
		// draw stays in the replay-exact resolution path.
		if effects.GenericChoiceAtRandom(sa) {
			return false
		}
	}
	var source state.ObjID
	var svars map[string]string
	wr := e.G.Obj(obj)
	if wr != nil {
		source = wr.Source
	}
	if so := e.G.Obj(source); so != nil {
		// The modal SVar names resolve against the face that OWNS the trigger
		// (the compiled SA pointer identifies it): a mutated pile's under-card
		// modal trigger must not read the pile's top face's same-named SVar --
		// the same top-face steal the MergedTriggerPush body resolution
		// prevents. Ordinary triggers find their own face (the top one) and
		// behave exactly as before. A HAS-ALL-ABILITIES-OF wrapper (r3) is
		// covered inside the recovery function itself.
		if _, mf, ok := e.findTriggerForAbilityFace(source, sa); ok && mf != nil {
			svars = mf.SVars
		} else if sf := so.Face(); sf != nil {
			svars = sf.SVars
		}
	}
	if owned, ok := e.triggerLineSVars[obj]; ok {
		svars = owned
	}
	choices := effects.CharmOf(sa).Modes
	for i := range choices {
		if cards.ResolveSVar(svars, choices[i]) == nil {
			return false // not modal: the primitive asks at resolution
		}
	}
	ctx := effects.NewCtxPtr(source, p, effects.CtxInit{TriggerContext: e.triggerContexts[obj]})
	effects.SetSVars(ctx, svars)
	if sa.API == "Charm" && effects.CharmRandomChosen(e, ctx, sa) {
		// param:api:Charm.Random: a random Charm's mode is never asked at
		// placement. The Charm gate keeps this site's other modal families
		// (Vote, GenericChoice, VillainousChoice) untouched -- measured, no
		// corpus carrier of those carries `Random$` (GenericChoice's own
		// spelling is `AtRandom$`, a different unread parameter). Returning
		// false is this function's own "not modal: the primitive asks at
		// resolution" verdict -- resolution's effCharm then
		// picks the mode with the engine's rng (Random$ True, or Random$
		// Compare while the comparison holds) or poses the ordinary KModes
		// ask (a failed or unresolvable comparison). The rng draw must happen
		// at RESOLUTION, where a replay re-derives it byte-identically; a
		// placement-time pick would consume the stream before the trigger is
		// even on the stack.
		return false
	}
	// ChoiceRestriction$ ("choose one that hasn't been chosen this turn / this
	// game"): a later instance of the same triggered Charm on the same source
	// must not offer a mode an earlier instance already chose, so the eligible
	// list is filtered before the bounds clamp and the options are built. With
	// every mode exhausted, the min-over-modes guard below leaves the trigger
	// doing nothing -- the oracle's own outcome.
	eligible := effects.CharmEligibleModes(e, source, sa, choices)
	min, max, repeat := effects.CharmModeBounds(e, ctx, sa, len(eligible))
	if min > len(eligible) && !repeat {
		return true
	}
	e.ask(modeDecisionForChoices(p, source, sa, svars, eligible, min, max, repeat))
	return true
}

// askTriggerOrder is R1: the controller of two or more simultaneous triggers
// chooses the order. Min == Max == n over exactly that controller's own n
// pending triggers, so Decision.Validate's existing rules (n choices, all
// distinct, all in range) already mean "a permutation of these n" and Ruling
// U2 needs no wire-format change at all.
//
// Option i is e.pendingTriggers[i]; that positional correspondence is the only
// binding between the decision and the queue, and handleTriggerOrder rechecks
// it before acting on the answer.
func (e *Engine) askTriggerOrder(p state.PlayerID, n int) {
	d := &decision.Decision{Player: p, Kind: decision.KTriggerOrder, Min: n, Max: n,
		Prompt: "Order your simultaneous triggered abilities: the one you choose " +
			"first is put on the stack first, and so resolves last"}
	for i := 0; i < n; i++ {
		pt := e.pendingTriggers[i]
		d.Options = append(d.Options, decision.Option{Index: i, Kind: "trigger",
			Label: e.triggerLabel(pt), Obj: pt.Source, Player: pt.Controller})
	}
	for i := 0; i < n; i++ {
		if e.pendingTriggers[i].Casualty {
			// Ashad's EQ0 stack grant has expired on this cast. Invalidate
			// the pre-payment layer snapshot before the ordering decision.
			e.invalidateScratchLayerLists()
			break
		}
	}
	e.ask(d)
}

// handleTriggerOrder applies an answered ordering decision, then resumes the
// drain. in.Choices is exactly a permutation of [0, n) -- Validate enforced
// that -- so the copy below is total: every entry of the group is written
// exactly once and none is lost or duplicated.
func (e *Engine) handleTriggerOrder(d *decision.Decision, in decision.Intent) {
	if e.frontIsTheOfferedGroup(d) {
		n := len(d.Options)
		permuteTriggersInPlace(e.pendingTriggers[:n], in.Choices)
		e.orderedTriggers = n
	} else if len(e.pendingTriggers) > 0 {
		// Defensive, and believed unreachable: nothing between ask and answer
		// removes from the front of the queue, so the group is still there.
		// If it somehow is not, settle exactly one entry so the drain makes
		// forward progress in the queue's existing order rather than re-asking
		// a decision whose answer it cannot apply.
		e.orderedTriggers = 1
	}
	e.resumeTriggerDrain()
}

// permuteTriggersInPlace rewrites q so that q[k] is the entry formerly at
// q[choices[k]], for choices a permutation of [0, len(q)) -- what copying
// q[choices[0]], q[choices[1]], ... into a fresh slice and back did, without
// the fresh slice (a pendingTrigger carries a whole effects.Ctx by value).
// Each cycle of the permutation is rotated through one held entry.
func permuteTriggersInPlace(q []pendingTrigger, choices []int) {
	var doneBuf [64]bool
	done := doneBuf[:0]
	if len(q) <= len(doneBuf) {
		done = doneBuf[:len(q)]
	} else {
		done = make([]bool, len(q))
	}
	for k := range q {
		if done[k] || choices[k] == k {
			done[k] = true
			continue
		}
		held := q[k]
		j := k
		for {
			done[j] = true
			src := choices[j]
			if src == k {
				q[j] = held
				break
			}
			q[j] = q[src]
			j = src
		}
	}
}

// frontIsTheOfferedGroup rechecks that the queue still starts with exactly the
// triggers d offered, in the order it offered them. Ruling U3's "cannot be
// reordered by that path", verified rather than assumed.
func (e *Engine) frontIsTheOfferedGroup(d *decision.Decision) bool {
	n := len(d.Options)
	if n == 0 || n > len(e.pendingTriggers) || e.orderedTriggers != 0 {
		return false
	}
	for i := 0; i < n; i++ {
		if e.pendingTriggers[i].Controller != d.Player ||
			e.pendingTriggers[i].Source != d.Options[i].Obj {
			return false
		}
	}
	return true
}

// askTriggerOptional is R2: an optional trigger reaches the stack only on an
// explicit yes. Min == Max == 1 over two options, "yes" first.
func (e *Engine) askTriggerOptional(who state.PlayerID, pt pendingTrigger) {
	label := e.triggerLabel(pt)
	d := &decision.Decision{Player: who, Kind: decision.KTriggerOptional, Min: 1, Max: 1,
		Prompt: "Put this optional triggered ability on the stack? — " + label,
		Source: pt.Source,
		Options: []decision.Option{
			{Index: 0, Kind: "yes", Label: "Yes — " + label, Obj: pt.Source, Player: pt.Controller},
			{Index: 1, Kind: "no", Label: "No", Obj: pt.Source, Player: pt.Controller},
		}}
	e.ask(d)
}

// askOptionalAtResolution is CR 603.5's question at resolution time: the
// optional triggered ability is already ON the stack (putTriggersOnStack
// pushes it unconditionally), and its decider chooses whether to apply the
// effect as it resolves. It poses a KTriggerOptional decision with a resume
// point whose kind is "optional", so handleTriggerOptional can route the
// answer: a yes re-enters the suspended resolution (resumeResolution runs
// the ability's effect), a no lets the ability leave the stack having done
// nothing (finishResumption). The decider is derived from the stack object's
// own controller + Remembered (deciderFromSpec), not from a pendingTrigger,
// because the queued trigger has already been consumed by the drain.
func (e *Engine) askOptionalAtResolution(who state.PlayerID, o *state.Object, sa *cards.SA, label string, effectOptional bool) {
	d := optionalAtResolutionDecision(who, o, sa, label, effectOptional)
	// The resolution kernel's answer in hand: run exactly the continuation
	// for the answer -- resumeResolution's "optional" arm on a yes,
	// finishResumption and the CR 117.3b reset on a no -- now, in line. Both
	// complete the resolution and log its priority grant, so handlePriority
	// must not log a second one (tapeGranted).
	in := tapeServe(e, d)
	rp := &resumePoint{kind: "optional", obj: o.ID, sa: sa}
	chosen := d.Chosen(in)
	if len(chosen) == 1 && chosen[0].Kind == "yes" {
		// ResolvedLimit$: an ACCEPTED optional trigger is one the effect runs
		// for, so it consumes the per-turn resolution count (a decline never
		// does). The check is on the resolved line's own param, never the
		// source's other lines (Tidus's non-RL BeginCombat line).
		if t, ok := e.triggerForAbilityObject(o.ID, o); ok {
			if _, limited := resolvedLimitValue(t); limited {
				e.noteTriggerResolved(o.Source)
			}
		}
		e.resumeResolution(rp, chosen)
	} else {
		e.finishResumption(rp.obj)
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
	}
	if !e.Suspended() {
		e.tapeGranted = true
	}
}

// optionalAtResolutionDecision is CR 603.5's yes/no at resolution.
func optionalAtResolutionDecision(who state.PlayerID, o *state.Object, sa *cards.SA, label string, effectOptional bool) *decision.Decision {
	return &decision.Decision{Player: who, Kind: decision.KTriggerOptional, Min: 1, Max: 1,
		ResumeKind: "optional", ResumeSA: sa, Source: o.Source, EffectOptional: effectOptional,
		Prompt: "Apply this triggered ability's effect? — " + label,
		Options: []decision.Option{
			{Index: 0, Kind: "yes", Label: "Yes — " + label, Obj: o.Source, Player: o.Controller},
			{Index: 1, Kind: "no", Label: "No", Obj: o.Source, Player: o.Controller},
		}}
}

// handleTriggerOptional applies an answered optional-trigger decision. There
// are two shapes, told apart by whether a resolution is suspended:
//
//   - The CR 603.5 resolution ask (e.resume set with kind "optional"): the
//     ability is already on the stack. A yes re-enters the suspended
//     resolution through resumeResolution (which runs the ability's effect,
//     exactly as resolveTop's own tail would have, and then moves it off the
//     stack); a no lets the ability leave the stack having done nothing via
//     finishResumption. Either way the resolution completes and, mirroring
//     resumeResolution's own tail, priority returns to the active player.
//   - The placement ask (a Miracle offer, the one place an optional decision
//     is still handed out before the ability is on the stack): a yes places
//     the trigger (for a Miracle, that begins the cast flow), a no discards
//     it, then the drain resumes.
//
// A declined optional trigger on the resolution path still emits its
// TriggerPush (the ability did go on the stack); only a placement no emits
// nothing beyond every decision's own DecisionAsk/DecisionMade.
func (e *Engine) handleTriggerOptional(d *decision.Decision, in decision.Intent) {
	yes := false
	if opts := d.Chosen(in); len(opts) == 1 {
		yes = opts[0].Kind == "yes"
	}
	if pt, ok := e.takeAnsweredTrigger(d); ok {
		if yes {
			e.pushTrigger(pt)
		}
	}
	e.resumeTriggerDrain()
}

type deciderFromSpecCode uint16

const (
	deciderFromSpecYou deciderFromSpecCode = iota + 1
	deciderFromSpecTriggeredCardController
	deciderFromSpecTriggeredSourceController
)

var deciderFromSpecCodes = state.NewStrCodes(
	state.StrEntry[deciderFromSpecCode]{Key: "You", Val: deciderFromSpecYou},
	state.StrEntry[deciderFromSpecCode]{Key: "TriggeredCardController", Val: deciderFromSpecTriggeredCardController},
	state.StrEntry[deciderFromSpecCode]{Key: "TriggeredSourceController", Val: deciderFromSpecTriggeredSourceController},
)

type targetChooserFromSpecCode uint16

const (
	targetChooserFromSpecTriggeredTarget targetChooserFromSpecCode = iota + 1
	targetChooserFromSpecTriggeredPlayer
	targetChooserFromSpecTriggeredDefendingPlayer
	targetChooserFromSpecTriggeredAttackingPlayer
	targetChooserFromSpecTriggeredCardController
)

var targetChooserFromSpecCodes = state.NewStrCodes(
	state.StrEntry[targetChooserFromSpecCode]{Key: "TriggeredTarget", Val: targetChooserFromSpecTriggeredTarget},
	state.StrEntry[targetChooserFromSpecCode]{Key: "TriggeredPlayer", Val: targetChooserFromSpecTriggeredPlayer},
	state.StrEntry[targetChooserFromSpecCode]{Key: "TriggeredDefendingPlayer", Val: targetChooserFromSpecTriggeredDefendingPlayer},
	state.StrEntry[targetChooserFromSpecCode]{Key: "DefendingPlayer", Val: targetChooserFromSpecTriggeredDefendingPlayer},
	state.StrEntry[targetChooserFromSpecCode]{Key: "TriggeredAttackingPlayer", Val: targetChooserFromSpecTriggeredAttackingPlayer},
	state.StrEntry[targetChooserFromSpecCode]{Key: "TriggeredCardController", Val: targetChooserFromSpecTriggeredCardController},
)
