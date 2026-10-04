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
//   - impendingCost resolves the printed K:Impending parameter (the colon cut
//     is the TWO-field form N:cost; the count is the FIRST field, the cost
//     text the SECOND). It is the ONE cost reader the offer gate and the
//     charge call, the bestowCost convention: an unpriceable cost token
//     withholds the offer rather than charging a degraded generic.
//   - the offer (rules/legal_walk_hand.go and the command-zone half in
//     rules/legal_walk_alt.go) adds the "impending" cast mode; the spell is
//     an ordinary creature spell on the stack, so the offer gates on the
//     plain SpellAbility's targets LIKE the evoke/dash family.
//   - beginCast's "impending" arm charges the same resolved cost in place of
//     the printed mana cost.
//   - the pay-time CastInfo carries state.FlagImpending (rules/cast_targets.go's
//     modeFlags), the provenance the battlefield entry hook reads here and the
//     intrinsic type switch (state.Object.ImpendingDormant) reads while a time
//     counter remains. It is a CastProvenanceFlag, so a stack copy never
//     inherits it (CR 707.10).
//   - the entry rider (impendingEnter, called from altCostEnter for every
//     battlefield MoveZone) places the N time counters through a real
//     CounterChange and registers the recurring end-step removal as a
//     runtime-granted AddTrigger$ carrying the builtin __kwImpendingTick
//     body (the blitzEnter granted-trigger shape). The grant's
//     IsPresent$ Card.Self+counters_GE1_TIME gate (with PresentCompare$ GE1
//     and PresentDefined$ Self, the exact intervening-if
//     cards/kw_vanishing.go's upkeep trigger uses) stops it the moment the
//     last counter leaves and the permanent is a creature again.
//
// Deviation (not closed here): the N time counters are placed by a direct
// CounterChange emitted after the MoveZone, not by an entry replacement, so
// an "enters with an additional counter" replacement (Doubling Season,
// Hardened Scales) does not apply to them. An entry replacement cannot key on
// the alternative cost at the cards layer the way K:Vanishing's printed
// R:Event$ Moved can, because the rider is conditioned on which cost was
// PAID, which only exists once the cast is committed; the numbers ride the
// report.
//
// Every registration is a real event (DelayedRegister/AddContinuous) or a
// CounterChange the replay re-executes, so a replayed game re-derives the
// identical board.

package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
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

// impendingCost resolves the printed Impending keyword's alternative cost and
// count (CR 702.176a, Forge's K:Impending:<N>:<cost>). The count and cost are
// the TWO colon-separated fields after the head; only those two are read, so
// no trailing Forge metadata can leak into either. An unpriceable cost token
// (ParseCost's Unknown set) withholds the offer rather than charging a
// degraded generic, the bestowCost fail-closed convention.
func impendingCost(f *cards.Face) (Cost, bool) {
	s, ok := f.KeywordParam("Impending")
	if !ok {
		return Cost{}, false
	}
	count, costText, ok := strings.Cut(s, ":")
	if !ok || strings.TrimSpace(count) == "" {
		return Cost{}, false
	}
	c := ParseCost(strings.TrimSpace(costText))
	if len(c.Unknown) > 0 {
		return Cost{}, false
	}
	return c, true
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

// impendingEnter is the CR 702.176a entry rider: a permanent whose impending
// cost was paid enters with N time counters and is not a creature until the
// last is removed. It is called from altCostEnter (a battlefield MoveZone),
// so the registration's Source is the entering permanent and the
// CounterChange's Counter is the canonical "TIME" kind the intrinsic switch
// and the emitted-count guard in rules/emit.go both read.
//
// The counters are placed through a real CounterChange event (never a direct
// state write) so repl:AddCounter replacements and CounterAdded triggers see
// the placement exactly like a suspend card's. The recurring end-step removal
// is a runtime-granted trigger (the blitzEnter AddTrigger shape) whose
// IsPresent$ Card.Self+counters_GE1_TIME gate stops it the instant the last
// counter leaves.
func (e *Engine) impendingEnter(id state.ObjID, controller state.PlayerID) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return
	}
	n := impendingCount(o.Face())
	if n > 0 {
		e.emit(events.Event{Kind: events.CounterChange, Obj: id, Player: controller,
			Counter: "TIME", Amount: n, Text: "impending entry"})
	}
	// "At the beginning of your end step, remove a time counter from it."
	// A granted trigger scoped to the permanent itself (the blitzEnter
	// AddTrigger shape): Affects$ Card.Self pins it to this permanent, the
	// Phase/End-of-Turn gate plus ValidPlayer$ You makes it the controller's
	// own end step, and the counters_GE1_TIME intervening-if (the exact gate
	// cards/kw_vanishing.go's upkeep trigger uses, with PresentCompare$ GE1 and
	// PresentDefined$ Self) ends it with the last counter.
	if t, ok := cards.ParseTriggerLine(
		"Mode$ Phase | Phase$ End of Turn | ValidPlayer$ You | TriggerZones$ Battlefield | IsPresent$ Card.Self+counters_GE1_TIME | PresentCompare$ GE1 | PresentDefined$ Self | Execute$ __kwImpendingTick | TriggerDescription$ At the beginning of your end step, remove a time counter from it."); ok {
		e.AddContinuous(state.ContinuousEffect{
			Source: id, Affects: "Card.Self", Controller: controller,
			AddTrigger: &t,
		})
	}
}
