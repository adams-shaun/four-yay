package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// The plain mana ability fast path.
//
// Nearly every mana activation in a game is a printed "{T}: Add {G}" -- an
// AB$ Mana whose whole parameter set is Cost$, one Produced$ symbol, an
// optional literal Amount$ and presentation text. resolveManaEffectColor
// resolves it through the general machinery: a Params copy with Produced$
// rewritten, a resolution Ctx with the per-turn tallies, and effects.Resolve
// (its frame publications, target-LKI maps, condition gate, unless gate and
// imprint tail) around effMana. For the plain shape every one of those steps
// is a no-op, and effMana emits exactly one event: ManaAdd{Player: the
// activator, Counter: the producer tag (ManaProducerTag) + the symbol,
// Amount: the literal}. The fast path emits that event directly, under the
// same manaFromTap/manaProducer replacement context.
//
// Two conditions keep it exact beyond the shape:
//
//   - no ProduceMana replacement could see the event (none registered by an
//     Effect, no replacement-source face carrying one): a matching
//     replacement could pose a CR 616.1 order or colour ask, and an ask
//     captures the resolution frames effects.Resolve publishes. With no
//     candidate the dispatch returns the event untouched;
//   - the ability is the configured printed one (its facts carry the shape),
//     produced unchanged, no gained identity and no sacrificed permanents.
//
// In the rules test binary (manaPlainVerify) the fast path instead runs the
// general resolution and panics unless the first event it logged is exactly
// the event the fast path would have emitted.

// manaPlainVerify: see derivedMemoVerify. Set by the rules test binary.
var manaPlainVerify = derivedMemoVerifyFlag != ""

// plainManaShape reports the symbol and amount of a plain AB$ Mana ability:
// no sub-ability, a single W/U/B/R/G/C Produced$, an absent or positive
// literal Amount$, and no parameter beyond those, Cost$ and the
// presentation/AI keys (the parameter half is compiled once, mp's
// PlainSym/PlainAmt). It returns 0 for any other ability.
func plainManaShape(ab *cards.SA, mp *effects.ManaParams) (byte, int32) {
	if ab == nil || ab.API != "Mana" || ab.Sub != nil {
		return 0, 0
	}
	return mp.PlainSym, mp.PlainAmt
}

// plainManaAdd returns the one event a plain mana ability's resolution
// emits, and the replacement context's tap flag, when the fast path applies.
func (e *Engine) plainManaAdd(p state.PlayerID, source state.ObjID, ma *cards.SA, produced string,
	gained pay.GainedManaRef, sacs []state.ObjID) (events.Event, bool, bool) {
	if gained.Face != nil || len(sacs) != 0 || len(produced) != 1 {
		return events.Event{}, false, false
	}
	f := e.manaFactsOf(ma)
	if f == nil || f.plainSym == 0 || produced[0] != f.plainSym {
		return events.Event{}, false, false
	}
	ev := events.Event{Kind: events.ManaAdd, Player: p, Amount: f.plainAmt}
	if e.produceManaReplacementPossible(ev) {
		return events.Event{}, false, false
	}
	tag, snow := effects.ManaProducerTag(e, source)
	switch {
	case tag != "":
		ev.Counter = tag + produced
	case snow:
		ev.Counter = "S" + produced
	default:
		ev.Counter = produced
	}
	return ev, f.cost.Tap, true
}

// produceManaReplacementPossible reports whether any ProduceMana replacement
// could be offered ev: an Effect-registered one in the active list, or an R:
// line on a face the replacement dispatch would visit for the event's name.
func (e *Engine) produceManaReplacementPossible(ev events.Event) bool {
	act := e.active()
	for i := range act {
		if act[i].ReplacementEvent == "ProduceMana" {
			return true
		}
	}
	found := false
	e.forEachReplacementSourceFor(replEventBits[cards.ReplProduceMana], func(id state.ObjID) {
		if found {
			return
		}
		f := e.replacementFace(id, ev)
		if f == nil {
			return
		}
		for i := range f.Repls {
			if replacementEventNameMatches(f.Repls[i].Event, "ProduceMana") {
				found = true
				return
			}
		}
	})
	return found
}

// verifyPlainMana checks the fast path's event against the general
// resolution's first logged event (from log index n0).
func (e *Engine) verifyPlainMana(want events.Event, n0 int) {
	if len(e.L.Events) <= n0 {
		panic(fmt.Sprintf("rules: plain mana fast path would emit %+v; the general path emitted nothing", want))
	}
	got := e.L.Events[n0]
	if got.Kind != want.Kind || got.Player != want.Player || got.Obj != want.Obj || got.From != want.From ||
		got.To != want.To || got.Amount != want.Amount || got.Step != want.Step || got.Counter != want.Counter ||
		got.Text != want.Text || len(got.IDs) != 0 || len(got.Pairs) != 0 || got.Secret {
		panic(fmt.Sprintf("rules: plain mana fast path would emit %+v; the general path emitted %+v", want, got))
	}
}

// payManaAbilityMana pays the mana part of a mana ability's cost. A bare
// {T} cost (compiledCost.bareTap: every component but Tap zero) has no mana
// or life to pay: the general payment resolves the empty cost against the
// pool, spends nothing and emits nothing, and its one lasting effect is
// emitRestrictedManaSpend's reset of the per-payment spend capture -- which
// is all the fast path does. Verify mode runs the general payment instead and
// panics if it emitted or failed.
func (e *Engine) payManaAbilityMana(p state.PlayerID, source state.ObjID, cc *compiledCost) bool {
	if cc.BareTap && !manaPlainVerify {
		e.NoCounterSpend = 0
		e.ManaSpentSources = nil
		e.ManaSpentAddsCounters = nil
		return true
	}
	n0 := len(e.L.Events)
	ok := pay.PayManaConvFor(asPayer(e), p, source, true, cc.Cost, asPayer(e).Conv(p, source, true))
	if cc.BareTap && (!ok || len(e.L.Events) != n0 || e.NoCounterSpend != 0 || e.ManaSpentSources != nil || e.ManaSpentAddsCounters != nil) {
		panic(fmt.Sprintf("rules: bare-tap mana payment for %d was not a no-op (ok=%v, %d events)", source, ok, len(e.L.Events)-n0))
	}
	return ok
}
