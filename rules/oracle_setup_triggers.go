package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// setupPlacementDropsTrigger reports whether a trigger queued while an
// xmageFixture scenario's setup placed a permanent on the battlefield must be
// discarded before the drive loop answers it.
//
// XMage builds a generated scenario's starting position with
// addCard(Zone.BATTLEFIELD, ...), which puts the permanent onto the
// battlefield without "entering": no enters-the-battlefield trigger is put on
// the stack, so gorge must not resolve the ones its own MoveZone queued. The
// discard is deliberately narrow, because a single placement's emit also
// queues triggers the entry causes indirectly and that XMage DOES see fire:
//
//   - TriggerChangesZone / Destination$ Battlefield and TriggerChangesZoneAll
//     are the entry shapes ("when this enters, ..."; "whenever one or more
//     creatures enter"). Both are XMage-addCard-silent and are dropped.
//   - A Saga's synthesized chapter ability (pendingTrigger.Chapter) is dropped
//     only for a back-face placement whose ENTERING face is not itself a Saga
//     (backFaceChapter, computed by setupBackFaceDropsChapter): XMage's
//     transformed permanent then carries a face with no Saga ability, so no
//     lore counter and no chapter I (a transformed Saga's creature; the entry
//     fold queues nothing there, so the drop only guards it). A Saga placed
//     with addCard DOES enter with its lore counter and fire chapter I there,
//     so it stays queued -- a front-face Saga (Summon: Anima's life loss,
//     Summon: Titan's mill, Summon: Knights of Round's tokens, and Summon:
//     Leviathan bouncing Grizzly Bears before the setup checkpoint) and a
//     back-face Saga creature alike (Jecht's Braska's Final Aeon discarding
//     and drawing, Joshua's Phoenix, Warden of Fire dealing 2 and gaining 2:
//     XMage's setup snapshots hold lore 2 and the chapter I effect already
//     resolved).
//   - Everything else stays. In particular a planeswalker's entry loyalty
//     counters are emitted as CounterChanges inside the same entry fold, so a
//     CounterAdded / CounterAddedOnce / CounterAddedAll trigger on a permanent
//     already on the battlefield (Inspired Tethermage watching Ajani Goldmane
//     enter) is queued in the same window and MUST fire; the counted
//     compliance test TestGeneratedSetupKeepsCounterAddedTrigger pins it.
//
// A trigger whose line cannot be resolved is kept: the discard only ever
// removes a trigger it can positively classify as an entry shape, so an
// unrecognised synthetic trigger degrades toward firing rather than vanishing.
func setupPlacementDropsTrigger(t cards.Trigger, backFaceChapter bool) bool {
	if backFaceChapter {
		return true
	}
	switch t.ModeKind() {
	case cards.TriggerChangesZone:
		// Destination$ compiles once through the registered coder (the same
		// read the merge-base filter used), so this is a trip through the
		// compiled zone enum rather than a fresh string comparison.
		d, ok := t.ParamCode(cards.PKDestination)
		z, sole := effects.Destination(d).SoleZone()
		return ok && sole && z == state.ZBattlefield
	case cards.TriggerChangesZoneAll:
		return true
	}
	return false
}

// setupBackFaceDropsChapter reports whether the chapter trigger a back-face
// battlefield placement queued must be dropped: the card is placed on its back
// face and that entering face (o.Face(), read after the FlipFace that rides
// ahead of the MoveZone) is not a Saga. A back-face Saga creature keeps its
// chapter I -- XMage's addCard resolves it from the entry lore counter.
func setupBackFaceDropsChapter(o *state.Object, backFace bool) bool {
	if !backFace || o == nil {
		return false
	}
	n, _ := cards.SagaChapters(o.Face())
	return n == 0
}

// setupPlacedBackFace reports whether the seat's setup places the named
// battlefield card on its back face (oracleSeat.BackFace).
func setupPlacedBackFace(s oracleSeat, name string) bool {
	for _, back := range s.BackFace {
		if cards.NormalizeName(back) == cards.NormalizeName(name) {
			return true
		}
	}
	return false
}

// setupBackFaceLoyalty is the starting loyalty a back-face placement of a
// transforming planeswalker enters with when its ENTERING face prints none:
// Forge spells the transformed face of an Oko-cycle walker "Loyalty: 0", a
// carry-over marker for the counters the permanent already held, never a real
// zero-loyalty entry (that face is reachable only by transforming). The
// harness grants the front face's printed starting loyalty instead. 0 when the
// entering face has its own positive loyalty, when either face is not a
// planeswalker, or when a printed value does not parse (the same fail-closed
// read events.EntryCounterGrants takes).
func setupBackFaceLoyalty(o *state.Object) int32 {
	if o == nil || o.Card == nil || len(o.Card.Faces) < 2 {
		return 0
	}
	back := o.Face()
	if back == nil || !back.IsPlaneswalker() {
		return 0
	}
	if n, err := strconv.Atoi(strings.TrimSpace(back.Loyalty)); err == nil && n > 0 {
		return 0
	}
	front := o.Card.Faces[0]
	if front == nil || !front.IsPlaneswalker() {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(front.Loyalty))
	if err != nil || n <= 0 {
		return 0
	}
	return int32(n)
}
