package rules

import (
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
//   - A Saga's synthesized chapter ability (pendingTrigger.Chapter) is dropped
//     only for a back-face placement (backFaceChapter): XMage's transformed
//     permanent carries the back face, which has no Saga ability, so no lore
//     counter and no chapter I. A front-face Saga placed with addCard DOES
//     enter with its lore counter and fire chapter I there (Summon: Anima's
//     life loss, Summon: Titan's mill, Summon: Knights of Round's tokens, and
//     Summon: Leviathan bouncing Grizzly Bears before the setup checkpoint),
//     so it stays queued.
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
