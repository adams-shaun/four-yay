package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Bargain (CR 702.166) — the optional additional cost "you may sacrifice an
// artifact, enchantment, or token as you cast this spell". Four pieces:
//
//   - possible reads the printed (or layer-6 granted) K:Bargain head through
//     the same derived-keyword read the other cast keywords share
//     (stackKeywordPossibleH + derivedWith with the stack-zone override), so
//     a grant reaching the cast spell is priced and stamped exactly like the
//     printed keyword.
//   - the offer (rules/legal_walk_hand.go) exposes a distinct
//     "cast (bargained)" option beside the plain cast. Bargain is an
//     ADDITIONAL cost -- it never replaces the mana cost -- so the offer
//     prices the same base the plain cast does and only the sacrifice is
//     added (the Casualty shape, not the alternative-cost family).
//   - ask poses the CR 601.2b sacrifice election mid-cast, the casualtyAsk
//     shape: the chosen permanent rides pc.Sacs so payCast charges the
//     sacrifice with every other cost part.
//   - modeFlags("bargained") stamps state.FlagBargained on the pay-time
//     CastInfo, the ONE home the Count$Bargained/Count$Bargain heads, the
//     bare Condition$ Bargain gate, the `bargained` filter predicate and the
//     Spell.Bargain cost-static constraint read.
//
// The eligibility filter is CR 702.166a's "an artifact, an enchantment, or a
// token" — a union of the three, matched through the ordinary filter grammar
// so a face-down token artifact and a granted-enchantment permanent read the
// same way every other spec does.
//
// bargain is a feature struct holding the one dependency these helpers need
// (the engine). Its methods deliberately do NOT live on *Engine: every
// *Engine method can call every other, and the engineMethodCount ratchet
// only ever shrinks, so new cast-side logic goes on a narrow receiver.
type bargain struct{ e *Engine }

// possible reports whether the cast spell carries Bargain right now --
// printed or layer-6 granted -- through the derived-keyword read that
// reaches the stack zone.
func (b bargain) possible(id state.ObjID) bool {
	return b.e.stackKeywordPossibleH(id, kwhBargain)
}

// candidates returns the permanents the caster may sacrifice for Bargain:
// every artifact, enchantment, or token they control (CR 702.166a), in
// stable battlefield order (the casualtyCandidates convention, so a replay
// derives the identical option list).
func (b bargain) candidates(p state.PlayerID, spell state.ObjID) []state.ObjID {
	var out []state.ObjID
	for _, id := range b.e.G.Zone(state.ZBattlefield, p) {
		if b.e.matchesSpecFrom("Artifact,Enchantment,token", id, p, spell) {
			out = append(out, id)
		}
	}
	return out
}

// ask announces the CR 702.166 Bargain sacrifice before payment, the
// casualtyAsk shape: the chosen permanent remains on the battlefield until
// payCast charges the sacrifice with the other cost parts. The offer only
// exists when at least one candidate is payable, so a stale option whose
// candidates have gone degrades to the plain cast (one replay-visible Note)
// rather than stranding the cast.
func (b bargain) ask() bool {
	e := b.e
	pc := e.cast
	if pc.mode != "bargained" || pc.bargainDone {
		return false
	}
	pc.bargainDone = true
	candidates := b.candidates(pc.player, pc.card)
	if len(candidates) == 0 {
		pc.mode = ""
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card, Text: "bargain no longer payable; casting without bargain"})
		return false
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Choose an artifact, enchantment, or token to sacrifice for bargain", Source: pc.card}
	for _, id := range candidates {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "bargain", Obj: id, Label: e.targetName(id)})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}
