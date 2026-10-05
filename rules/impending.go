// impending.go implements the Impending alternative cast (CR 702.176a):
// "Impending N—[cost] (If you cast this spell for its impending cost, it
// enters with N time counters and isn't a creature until the last is
// removed. At the beginning of your end step, remove a time counter from
// it.)" Forge prints the whole mechanic on ONE line, K:Impending:<N>:<cost>
// (measured over the corpus: 6 carriers, all the printed form; there are no
// layer-6 AddKeyword$ Impending grants), so unlike Sneak's script-line riders
// the end-step removal is INTRINSIC to the keyword and this file owns it.
//
// The pieces:
//
//   - keywordAltCost resolves the printed K:Impending parameter (the colon cut
//     is the TWO-field form N:cost; the count is the FIRST field, the cost
//     text the SECOND). It is the ONE cost reader the offer gate, the charge
//     and the potential-plan pricing call, shared with evoke/dash/overload.
//   - the offer (rules/legal_walk_hand.go and the command-zone half in
//     rules/legal_walk_alt.go) adds the "impended" cast mode (castModeImpended); the spell is
//     an ordinary creature spell on the stack, so the offer gates on the
//     plain SpellAbility's targets LIKE the evoke/dash family.
//   - beginCast's alternative-cost keyword arm charges the same resolved cost
//     in place of the printed mana cost.
//   - the pay-time CastInfo carries state.FlagImpending (rules/cast_targets.go's
//     modeFlags), the provenance the battlefield entry hook reads here and the
//     intrinsic type switch (state.Object.ImpendingDormant) reads while a time
//     counter remains. It is a CastProvenanceFlag, so a stack copy never
//     inherits it (CR 707.10).
//   - the entry rider (impendingTickGrant, registered by altCostEnter for every
//     battlefield MoveZone) places the N time counters through a real
//     CounterChange and registers the recurring end-step removal as a
//     runtime-granted AddTrigger$ carrying the builtin __kwImpendingTick
//     body (the blitzEnter granted-trigger shape). The grant's
//     IsPresent$ Card.Self+counters_GE1_TIME gate (with PresentCompare$ GE1
//     and PresentDefined$ Self, the exact intervening-if
//     cards/kw_vanishing.go's upkeep trigger uses) stops it the moment the
//     last counter leaves and the permanent is a creature again.
//
// The paid-cost provenance remains on the stack object until its MoveZone
// folds. rules/entry_counters.go reads that bit into the common entry-counter
// plan, so AddCounter replacements settle the time-counter grant before the
// permanent enters, alongside other entry-characteristic counters.
//
// Every registration is a real event (AddContinuous) or a staged entry grant
// folded from the logged MoveZone and replacement-adjusted counter notice, so
// replay re-derives the identical board.

package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	// The coverage census: kw:Impending is implemented as a cast option read
	// directly off the K: line plus the intrinsic entry/time-counter and type
	// switch machinery this file and state/object.go own (see this file's
	// doc), registered here exactly as blitz.go and webSlinging register
	// theirs. Proof: the real-corpus tests in rules/impending_test.go.
	effects.RegisterNonAPI("kw:Impending")
}

// impendingCount reads the N of a printed K:Impending:N:cost line. It is the
// entry hook's own reader (impendingCost owns the CAST's price, this owns the
// COUNTERS), kept separate so neither depends on the other's parse shape. A
// missing, empty or non-numeric count yields 0, which places no counters --
// the fail-closed direction for a malformed script (there is no printed
// carrier today, and the census ratchet pins the set).
func impendingCount(f *cards.Face) int32 {
	s, ok := f.KeywordParam("Impending")
	if !ok {
		return 0
	}
	count, _, _ := strings.Cut(s, ":")
	n := int32(0)
	for _, r := range strings.TrimSpace(count) {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int32(r-'0')
	}
	return n
}

// impendingTickGrant is the CR 702.176a end-step trigger rider. Entry time
// counters are placed by the shared staged entry-counter path before MoveZone
// folds (rules/entry_counters.go), allowing the AddCounter replacement class
// to modify them. This builds the recurring end-step removal as a
// runtime-granted trigger (the blitzEnter AddTrigger shape) that
// altCostEnter registers: Affects$ Card.Self pins it to this permanent, the
// Phase/End-of-Turn gate plus ValidPlayer$ You makes it the controller's own
// end step, and the counters_GE1_TIME intervening-if (the exact gate
// cards/kw_vanishing.go's upkeep trigger uses, with PresentCompare$ GE1 and
// PresentDefined$ Self) ends it with the last counter.
func impendingTickGrant(o *state.Object, controller state.PlayerID) (state.ContinuousEffect, bool) {
	if o == nil || o.Face() == nil {
		return state.ContinuousEffect{}, false
	}
	t, ok := cards.ParseTriggerLine(
		"Mode$ Phase | Phase$ End of Turn | ValidPlayer$ You | TriggerZones$ Battlefield | IsPresent$ Card.Self+counters_GE1_TIME | PresentCompare$ GE1 | PresentDefined$ Self | Execute$ __kwImpendingTick | TriggerDescription$ At the beginning of your end step, remove a time counter from it.")
	if !ok {
		return state.ContinuousEffect{}, false
	}
	return state.ContinuousEffect{Source: o.ID, Affects: "Card.Self", Controller: controller, AddTrigger: &t}, true
}
