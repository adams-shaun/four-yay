package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// K:Split second (CR 702.62): "As long as this spell is on the stack,
// players can't cast spells or activate abilities that aren't mana
// abilities."
//
// The enforcement point is legalActionsPriced's priority-offer walk: a
// player in this engine can only act through an option that walk offers
// (Submit validates every intent against the pending decision's options),
// so withholding the blocked kinds from the offer IS the enforcement --
// there is no parallel intent path a client could take. Triggered
// abilities are unaffected (CR 702.62 stops only spells and activations),
// and the normal trigger queue needs no change.

// splitSecondHolds reports whether any spell currently on the stack has
// split second. The read is the derived keyword (HasKeyword), so both the
// printed keyword (Krosan Grip's K:Split second) and a granted one reach
// it through the same layer walk every other keyword uses: Molten
// Disaster's kicked-gated AddKeyword$ Split second CharacteristicDefining
// static (PresentZone$ Stack) grants the stack object exactly where the
// gate needs it.
func (e *Engine) splitSecondHolds() bool {
	for _, id := range e.G.Zone(state.ZStack, 0) {
		if o := e.G.Obj(id); o != nil && e.hasKeywordH(id, kwhSplitSecond) {
			return true
		}
	}
	return false
}

// filterSplitSecondActions removes the options CR 702.62 forbids from a
// priority option list and re-indexes in place (the walk's Index/position
// identity invariant, rules/engine.go's ask): every "cast" option (the hand,
// command-zone, may-play, flashback/aftermath/harmonize/warp/escape and
// exile-recast sources all append through the one walk) and every non-mana
// ability option. What survives: playing a land (not a spell cast), mana
// abilities (the "activate" Kind -- explicitly permitted by the rule), the
// Station/Room-unlock special actions, pass and concede. The two offers that
// ride the "cast" Kind but are special actions rather than spell casts --
// Suspend (CR 702.88a: exiling the card with time counters) and the Foretell
// {2} hand action (CR 702.126a) -- are kept: neither is casting a spell. A
// "granted" option is always a non-mana activated ability (a granted mana
// ability flows through availableManaAbilities into the "activate" Kind),
// so blocking the Kind wholesale is exact.
func (e *Engine) filterSplitSecondActions(out []decision.Option) []decision.Option {
	kept := out[:0]
	for _, o := range out {
		switch filterSplitSecondActions1201Codes.Code(string(o.Kind)) {
		case filterSplitSecondActions1201Cast:
			if o.Mode == "suspend" || o.Mode == "foretell" {
				kept = append(kept, o)
			}
		case filterSplitSecondActions1201Ability:
			// blocked outright
		default:
			kept = append(kept, o)
		}
	}
	for i := range kept {
		kept[i].Index = i
	}
	return kept
}

const (
	filterSplitSecondActions1201Cast    uint16 = 1 // "cast"
	filterSplitSecondActions1201Ability uint16 = 2 // "ability", "granted"
)

var filterSplitSecondActions1201Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "cast", Val: filterSplitSecondActions1201Cast},
	state.StrEntry[uint16]{Key: "ability", Val: filterSplitSecondActions1201Ability},
	state.StrEntry[uint16]{Key: "granted", Val: filterSplitSecondActions1201Ability},
)
