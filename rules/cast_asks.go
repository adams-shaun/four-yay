package rules

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// altAddAsk poses the AlternateAdditionalCost either-or choice: which ONE
// alternative additional cost this cast pays (CR 601.2h). It runs FIRST in
// continueCast -- before the {X} ask -- because the chosen part folds into
// cost and every later stage (Delve, sac, discard, the mana window and the
// final payment) must see the total it will actually charge. ALL parts are
// offered, the payable ones FIRST (deterministically: script order within
// each group), so an automated seat taking the first option always takes one
// it can pay; a seat that picks an unpayable part walks into a later stage's
// abort (CR 733.1 reversal). When NO part is payable the flow aborts (the
// offer gate already proved one was payable at offer time, so this is a
// board that changed under the flow).
func (e *Engine) altAddAsk() bool {
	pc := e.cast
	if pc == nil || pc.altAddDone || len(pc.altAddParts) == 0 {
		return false
	}
	pc.altAddDone = true
	if len(pc.altAddParts) == 1 {
		part := ParseCost(pc.altAddParts[0])
		if !e.castable(pc.player, pc.card, pc.cost.Plus(part), pc.isAbility()) {
			e.abortCast(pc, "additional cost no longer payable; cast aborted", true)
			return true
		}
		pc.cost = pc.cost.Plus(part)
		return false
	}
	payable := make([]int, 0, len(pc.altAddParts))
	unpayable := make([]int, 0, len(pc.altAddParts))
	for i, part := range pc.altAddParts {
		if e.castable(pc.player, pc.card, pc.cost.Plus(ParseCost(part)), pc.isAbility()) {
			payable = append(payable, i)
		} else {
			unpayable = append(unpayable, i)
		}
	}
	order := append(append([]int(nil), payable...), unpayable...)
	if len(payable) == 0 {
		e.abortCast(pc, "additional cost no longer payable; cast aborted", true)
		return true
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Choose an additional cost to cast " + e.targetName(pc.card), Source: pc.card}
	for _, i := range order {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "altaddcost",
			Label: capitaliseFirst(costPhrase(ParseCost(pc.altAddParts[i]))), Amount: i})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// giftAsk poses the CR 702.168 Gift election: "You may promise an opponent a
// gift as you cast this spell." The election is FREE -- no mana, no card --
// and is a single KChoose offering a decline plus one option per legal
// opponent (CR 702.168a: the caster chooses WHICH opponent), so a two-seat
// game offers exactly one opponent plus the decline. The answer goes nowhere
// here: it rides pc and pushCast folds it onto the stack object as an
// events.GiftPromise, the replay-derived home the target bound and the
// resolution read. Only a face printing K:Gift with a GiftAbility SVar poses
// the ask, so no unrelated cast gains a decision. The one-home answer rule
// is the generic KChoose contract (Min 1 / Max 1 over the offered options),
// which already drives Decision.Validate; the bot's own answer is proven
// against it in botpolicy's gift test, so no parallel rule can drift.
func (e *Engine) giftAsk() bool {
	pc := e.cast
	if pc == nil || pc.giftDone || pc.isAbility() {
		return false
	}
	o := e.G.Obj(pc.card)
	if o == nil || o.Face() == nil || !o.Face().HasKeyword("Gift") {
		return false
	}
	if _, ok := o.Face().SVars["GiftAbility"]; !ok {
		return false
	}
	pc.giftDone = true
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Promise a gift?", Source: pc.card}
	d.Options = append(d.Options, decision.Option{Index: 0, Kind: "gift_decline",
		Label: "Don't promise a gift"})
	for _, p := range e.G.AliveFrom(pc.player) {
		if p == pc.player {
			continue
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "gift_promise",
			Player: p, Label: "Promise " + pay.SeatName(e.G, p) + " a gift"})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

func (e *Engine) forageAsk() bool {
	pc := e.cast
	if pc == nil || !pc.cost.Forage || pc.forageDone {
		return false
	}
	pc.forageDone = true
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Choose how to forage", Source: pc.card}
	if len(e.G.Zone(state.ZGraveyard, pc.player)) >= 3 {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "forage_exile", Label: "Exile three cards from your graveyard"})
	}
	for _, id := range pay.CostCandidates(asPayer(e), pc.player, pc.card, state.ZBattlefield, "Food.YouCtrl", false, false) {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "forage_food", Obj: id,
			Label: "Sacrifice " + e.targetName(id)})
	}
	if len(d.Options) == 0 {
		e.abortCast(pc, "forage no longer payable; cast aborted", true)
		return true
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

func (e *Engine) revealCostAsk() bool {
	pc := e.cast
	for pc.RevealPart < len(pc.cost.Reveal) {
		part := pc.cost.Reveal[pc.RevealPart]
		// A whole-hand Reveal (Reveal<N/Hand>) settles without asking: it
		// reveals the payer's whole hand AS IT STANDS at payment. For a CAST
		// the spell being paid for has already moved to the stack (CR 601.2a)
		// and is no longer part of the hand; for an ABILITY activation the
		// source still sits in the hand and IS part of the hand it reveals.
		// Zero cards is a legal payment (CR 701.20a); pay.EmitChoiceCosts
		// announces it loudly via pc.revealedEmptyHand.
		if isWholeHandRevealSpec(part.Spec) {
			var hand []state.ObjID
			excludeSource := !pc.isAbility()
			for _, oid := range e.G.Zone(state.ZHand, pc.player) {
				if excludeSource && oid == pc.card {
					continue
				}
				hand = append(hand, oid)
			}
			pc.Reveals = append(pc.Reveals, hand...)
			for range hand {
				pc.RevealHandArm = append(pc.RevealHandArm, true)
			}
			if len(hand) == 0 {
				pc.RevealedEmptyHand = true
			}
			pc.RevealPart++
			continue
		}
		// Relational SameColor (Illuminated Folio): SameColor matches no card
		// as a filter, so the candidates are every eligible hand card and
		// the ask itself carries the constraint -- decision.SetPropShared
		// over each option's DERIVED colour tokens, so Decision.Validate
		// (seats) and botpolicy's Clamp (the bot) reject a non-sharing
		// answer through the same rule the offer gate measured. The exactly-N
		// auto-settle sits behind the capacity check, so it can never pay an
		// illegal reveal.
		if isSameColorRevealSpec(part.Spec) {
			cands, sets := e.sameColorRevealSets(pc.player, pc.card, !pc.isAbility())
			if decision.SetPropCapacity(decision.SetPropShared, sets) < int(part.N) {
				e.abortCast(pc, "reveal cost no longer payable; cast aborted", true)
				return true
			}
			if len(cands) == int(part.N) {
				pc.Reveals = append(pc.Reveals, cands...)
				for range cands {
					pc.RevealHandArm = append(pc.RevealHandArm, true)
				}
				pc.RevealPart++
				continue
			}
			d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
				Prompt: "Choose cards to reveal", Source: pc.card,
				SetPropMode: decision.SetPropShared}
			for _, id := range cands {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "revealcost", Obj: id,
					SetProps: e.setPropTokens("color", id), Label: e.targetName(id)})
			}
			e.choosing = chooseCast
			e.ask(d)
			return true
		}
		candidates := pay.CostCandidates(asPayer(e), pc.player, pc.card, state.ZHand, part.Spec, !pc.isAbility(), false)
		if len(candidates) < int(part.N) {
			e.abortCast(pc, "reveal cost no longer payable; cast aborted", true)
			return true
		}
		if len(candidates) == int(part.N) {
			pc.Reveals = append(pc.Reveals, candidates...)
			for range candidates {
				pc.RevealHandArm = append(pc.RevealHandArm, true)
			}
			pc.RevealPart++
			continue
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
			Prompt: "Choose cards to reveal", Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "revealcost", Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

// revealCostOrChooseAsk drives the either-or `RevealOrChoose<N/Spec>` parts,
// one at a time, through the same chooseCast suspend/resume shape
// revealCostAsk drives. It is explicit whenever BOTH arms can pay: the payer
// picks the arm (reveal a hand card OR choose a permanent they control) and
// the object, even when each arm has exactly one candidate -- an either-or
// additional cost is a real cast-time election and must not be silently
// resolved by the engine. When only ONE arm can pay, that arm is settled by
// the ordinary reveal rule (auto when exactly N candidates, else ask), and
// when NEITHER arm can pay the cast aborts, exactly as revealCostAsk does.
// The elected objects ride pc.reveals (the shared `Revealed$<Property>` paid
// list) with a parallel pc.revealHandArm marking which arm each came from.
func (e *Engine) revealCostOrChooseAsk() bool {
	pc := e.cast
	for pc.RevealOrChoosePart < len(pc.cost.RevealOrChoose) {
		part := pc.cost.RevealOrChoose[pc.RevealOrChoosePart]
		hand, battlefield := pay.RevealOrChooseCandidates(asPayer(e), pc.player, pc.card, part)
		handViable := len(hand) >= int(part.N)
		bfViable := len(battlefield) >= int(part.N)
		switch {
		case !handViable && !bfViable:
			e.abortCast(pc, "reveal-or-choose cost no longer payable; cast aborted", true)
			return true
		case handViable && bfViable:
			// Both arms can pay: pose the election. Options carry their arm as
			// the option kind so the answer records which branch paid.
			d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
				Prompt: "Reveal a card from hand or choose a permanent you control", Source: pc.card}
			for _, id := range hand {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "revealorchoose", Obj: id, Label: e.targetName(id)})
			}
			for _, id := range battlefield {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "choosecost", Obj: id, Label: e.targetName(id)})
			}
			e.choosing = chooseCast
			e.ask(d)
			return true
		case handViable:
			if len(hand) == int(part.N) {
				pc.Reveals = append(pc.Reveals, hand...)
				for range hand {
					pc.RevealHandArm = append(pc.RevealHandArm, true)
				}
				pc.RevealOrChoosePart++
				continue
			}
			d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
				Prompt: "Choose cards to reveal", Source: pc.card}
			for _, id := range hand {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "revealorchoose", Obj: id, Label: e.targetName(id)})
			}
			e.choosing = chooseCast
			e.ask(d)
			return true
		default: // battlefield only
			if len(battlefield) == int(part.N) {
				pc.Reveals = append(pc.Reveals, battlefield...)
				for range battlefield {
					pc.RevealHandArm = append(pc.RevealHandArm, false)
				}
				pc.RevealOrChoosePart++
				continue
			}
			d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
				Prompt: "Choose a permanent you control", Source: pc.card}
			for _, id := range battlefield {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "choosecost", Obj: id, Label: e.targetName(id)})
			}
			e.choosing = chooseCast
			e.ask(d)
			return true
		}
	}
	return false
}

func (e *Engine) beholdCostAsk() bool {
	pc := e.cast
	for pc.BeholdPart < len(pc.cost.Behold) {
		part := pc.cost.Behold[pc.BeholdPart]
		candidates := append(pay.CostCandidates(asPayer(e), pc.player, pc.card, state.ZBattlefield, part.Spec, false, false),
			pay.CostCandidates(asPayer(e), pc.player, pc.card, state.ZHand, part.Spec, true, false)...)
		if len(candidates) < int(part.N) {
			e.abortCast(pc, "behold cost no longer payable; cast aborted", true)
			return true
		}
		if len(candidates) == int(part.N) {
			pc.Beholds = append(pc.Beholds, candidates...)
			pc.BeholdPart++
			continue
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
			Prompt: "Choose permanents or cards to behold", Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "beholdcost", Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

func (e *Engine) tapPermanentCostAsk() bool {
	pc := e.cast
	// The activated-action kind this cost belongs to (Crew/Saddle, or "" for
	// a hand-written tapXType ability), read off the SA being activated, so
	// the affordability gate, the tap election's Option.Values and the
	// Decision.MinSum the client and bot enforce all read the ONE TapPowerValue
	// value (a Pilot's power+2, Giant Ox's toughness).
	tapKind := tapCostSAKind(e.pcAbility(pc))
	for pc.TapPart < len(pc.cost.TapPermanent) {
		part := pc.cost.TapPermanent[pc.TapPart]
		candidates := pay.TapCostCandidates(asPayer(e), pc.player, pc.card, part)
		// One permanent can never pay two parts of the same cost, so the
		// candidate filter claims everything an earlier stage already recorded:
		// an earlier tap part's choice (taps settle together at payCast, so the
		// state does not yet show it), a Convoke/Harmonize creature announced
		// for a spell whose dyn tap part deferred to xAsk (the settle runs
		// after the announcement), and -- for every part, literal and dynamic
		// alike -- the source itself when a {T} in the same cost will tap it at
		// payCast (Forge CostTap): the {T} and the tapXType can never spend one
		// permanent twice.
		if len(pc.Taps) > 0 || len(pc.Convoke) > 0 || pc.cost.Tap {
			taken := make(map[state.ObjID]bool, len(pc.Taps)+len(pc.Convoke)+1)
			for _, id := range pc.Taps {
				taken[id] = true
			}
			for _, pay := range pc.Convoke {
				taken[pay.ID] = true
			}
			if pc.cost.Tap {
				taken[pc.card] = true
			}
			kept := make([]state.ObjID, 0, len(candidates))
			for _, cid := range candidates {
				if !taken[cid] {
					kept = append(kept, cid)
				}
			}
			candidates = kept
		}
		if isNamedCountPart(part) {
			// A named-announcement count (Explosive Singularity's Announce$
			// Tapped): namedAnnounceAsk already fixed how many; tap exactly
			// that many. A shrunken board is the unpayable abort.
			n := int(pc.namedN)
			if n > len(candidates) {
				e.abortCast(pc, "tap cost no longer payable; cast aborted", true)
				return true
			}
			if n == 0 {
				pc.TapPart++
				continue
			}
			if n == len(candidates) {
				pc.Taps = append(pc.Taps, candidates...)
				pc.TapPart++
				continue
			}
			d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: n, Max: n,
				Prompt: "Choose permanents to tap", Source: pc.card}
			for _, id := range candidates {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "tapcost", Obj: id, Label: e.targetName(id)})
			}
			e.choosing = chooseCast
			e.ask(d)
			return true
		}
		if part.Dyn != "" {
			// The dynamic tapXType heads (dynTapCost's doc). An X-form part whose
			// cost carries another announce-bearing part defers: xAsk (later in
			// continueCast's stage order) announces the X the part settles
			// exactly, so the ask must not run before the announcement exists
			// (Necron Overlord's "{X}, tap X untapped artifacts"). xAsk's own
			// bound caps the announced value by these candidates.
			if part.Dyn == "X" && !pc.xDone && (costAnnouncesCastX(pc.cost) || pc.announceX != "") {
				return false
			}
			// An Any-form part pays only by tapping at least one: no eligible
			// permanent (the affordability gate agreed, so this is a board that
			// changed under the offer) aborts the whole cast/activation. The
			// same holds when the survivors can no longer reach a
			// withTotalPowerGE<N> group predicate's floor: posing an election
			// whose every answer Decision.Validate rejects would wedge the
			// game, so CR 733.1's clean reversal runs instead.
			if part.Dyn == "Any" && len(candidates) == 0 {
				e.abortCast(pc, "tap cost no longer payable; cast aborted", true)
				return true
			}
			if part.Dyn == "Any" && part.MinPower > 0 && pay.TapPowerSum(asPayer(e), candidates, tapKind) < part.MinPower {
				e.abortCast(pc, "tap cost no longer payable; cast aborted", true)
				return true
			}
			// An X-form election with no eligible permanent can only announce
			// X = 0 (CR 601.2b; the affordability gate agrees, so this is a
			// board that changed under the offer). A decision nobody could
			// answer differently is never emitted -- posting the Min 0/Max 0
			// empty ask panics rules/engine.go's ask -- so resolve it silently
			// with X = 0 and no taps, mirroring triggeredTapAsk's decline.
			if part.Dyn == "X" && !pc.xDone && len(candidates) == 0 {
				pc.x = 0
				pc.TapPart++
				continue
			}
			if part.Dyn == "X" && pc.xDone {
				// The announced X settles exactly: no choice beyond which
				// permanents, so a shortfall is the same unpayable abort.
				n := int(pc.x)
				if n > len(candidates) {
					e.abortCast(pc, "tap cost no longer payable; cast aborted", true)
					return true
				}
				if n > 0 {
					d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: n, Max: n,
						Prompt: "Choose permanents to tap", Source: pc.card}
					for _, id := range candidates {
						d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "tapcost", Obj: id, Label: e.targetName(id)})
					}
					e.choosing = chooseCast
					e.ask(d)
					return true
				}
				pc.TapPart++
				continue
			}
			// The election announces the count: Min 0 for the X form (X = 0 is
			// a legal announcement) and Min 1 for Any (a cost is not paid by
			// tapping nothing). The answer records the taps and, for the X form,
			// binds the cast's X to the chosen count (the "tapcost" answer arm).
			// A part carrying a withTotalPowerGE<N> group predicate publishes
			// the floor as Decision.MinSum and each candidate's power as its
			// Option.Value, so Validate -- the one legal-answer home a client
			// and the bot repair both read -- enforces the total-power clause
			// without learning what power is.
			min := int32(1)
			if part.Dyn == "X" {
				min = 0
			}
			d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: int(min), Max: len(candidates),
				Prompt: "Choose permanents to tap", Source: pc.card, MinSum: int(part.MinPower)}
			for _, id := range candidates {
				opt := decision.Option{Index: len(d.Options), Kind: "tapcost", Obj: id, Label: e.targetName(id)}
				if part.MinPower > 0 {
					opt.Value = int(e.tapPowerValue(id, tapKind))
				}
				d.Options = append(d.Options, opt)
			}
			e.choosing = chooseCast
			e.ask(d)
			return true
		}
		if len(candidates) < int(part.N) {
			e.abortCast(pc, "tap cost no longer payable; cast aborted", true)
			return true
		}
		// A literal part with a group predicate that the shrinking board can
		// no longer satisfy aborts like the Any form above -- an auto-tap of
		// the only N candidates (next branch) or an election with no legal
		// answer would pay a floor the state no longer reaches.
		if part.MinPower > 0 && pay.TapTopPowerSum(asPayer(e), candidates, int(part.N), tapKind) < part.MinPower {
			e.abortCast(pc, "tap cost no longer payable; cast aborted", true)
			return true
		}
		if len(candidates) == int(part.N) {
			pc.Taps = append(pc.Taps, candidates...)
			pc.TapPart++
			continue
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
			Prompt: "Choose permanents to tap", Source: pc.card, MinSum: int(part.MinPower)}
		for _, id := range candidates {
			opt := decision.Option{Index: len(d.Options), Kind: "tapcost", Obj: id, Label: e.targetName(id)}
			if part.MinPower > 0 {
				opt.Value = int(e.tapPowerValue(id, tapKind))
			}
			d.Options = append(d.Options, opt)
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

func (e *Engine) blightCostAsk() bool {
	pc := e.cast
	for pc.BlightPart < len(pc.cost.Blight) {
		// An announced Blight<X> part's count is the announced X, which does
		// not exist until xAsk has run; skip it here (returning false so the
		// later stages run) and let the post-xAsk call in continueCast settle
		// it. Fixed Blight<N> parts are unaffected.
		if pc.cost.Blight[pc.BlightPart].Announced && !pc.xDone {
			return false
		}
		candidates := pay.BlightCandidates(asPayer(e), pc.player, pc.card)
		if len(candidates) == 0 {
			e.abortCast(pc, "blight cost no longer payable; cast aborted", true)
			return true
		}
		if len(candidates) == 1 {
			pc.Blights = append(pc.Blights, candidates[0])
			pc.BlightPart++
			continue
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: "Choose a creature to blight", Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "blightcost", Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

// exAsk offers the next unsettled Exile cost part (ExileFromHand /
// ExileFromGrave), walking pc.cost.Exile in order (pc.exilePart) the way
// sacAsk walks pc.cost.Sac. Candidates come from the part's zone (the payer's
// hand, or their graveyard for the encore self-exile shape) and are filtered
// through MatchesSpecFrom so CARDNAME self-references resolve to the source
// object exactly as they do for sacrifice costs. A part with too few
// candidates aborts the whole cast (nothing has moved yet); a part whose sole
// candidate IS the source records it without a decision, mirroring
// sacAsk's CARDNAME singleton rule.

// exilePartZones returns the zones one Exile cost part pays from, in the
// deterministic order both the payment chooser (exAsk) and the announced-X
// ceiling (xAsk) scan: a ZoneSet (ExileCtrlOrGrave's battlefield|graveyard)
// expands to its member zones, a single Zone is itself, and the zero Zone
// (state.ZLibrary) means the hand -- the same reading exileCostZones gives
// on the pay side. One helper keeps the offer, the announcement cap and the
// payment from disagreeing about where a part's materials live.
func exilePartZones(part CostPart) []state.Zone {
	if part.ZoneSet != 0 {
		var zones []state.Zone
		for _, z := range []state.Zone{state.ZBattlefield, state.ZGraveyard} {
			if part.ZoneSet&(1<<z) != 0 {
				zones = append(zones, z)
			}
		}
		return zones
	}
	if part.Zone == 0 {
		return []state.Zone{state.ZHand}
	}
	return []state.Zone{part.Zone}
}

// exileCostBoard is the narrow slice of the engine announcedExileCandidates
// and announcedExileBounds read: the game state, the spec matcher and the
// ForCost$ cost-block static. Taking this interface (not *Engine) keeps those
// helpers off the engine's method surface.
type exileCostBoard interface {
	Game() *state.Game
	matchesSpecFrom(spec string, id state.ObjID, you state.PlayerID, source state.ObjID) bool
	exileBlockedForCost(id state.ObjID, cause pay.CostCause) bool
}

// announcedExileCandidates counts the objects that could pay one announced
// Exile cost part at the xAsk ceiling. It mirrors exAsk's candidate walk --
// every zone the part names, the cast-time self-exclusion, the battlefield
// cost-block -- so the announced X can never exceed what the pick can settle
// (CR 601.2b), and it reads every zone an ExileCtrlOrGrave part pays from
// rather than the graveyard alone. Establishment-time reservation against
// other parts is deliberately not applied: this is an upper bound for the
// announcement, not the payment itself, and xAsk's own payable sweep plus
// exAsk's `already` exclusion re-check distinctness per answer.
//
// It takes the narrow exileCostBoard slice rather than *Engine so it stays off
// the engine's method surface (the lasagna rule: engine logic belongs in a free
// function over a small interface).
func announcedExileCandidates(e exileCostBoard, pc *pendingCast, part CostPart) []state.ObjID {
	var out []state.ObjID
	for _, zone := range exilePartZones(part) {
		for _, oid := range pay.ExileCostCandidates(e.Game(), zone, pc.player, part) {
			if !pc.isAbility() && oid == pc.card {
				continue
			}
			if zone == state.ZBattlefield && e.exileBlockedForCost(oid, pay.CostCauseForAbility(pc.isAbility())) {
				continue
			}
			if e.matchesSpecFrom(part.Spec, oid, pc.player, pc.card) {
				out = append(out, oid)
			}
		}
	}
	return out
}

// announcedExileBounds min-clamps applyCap by every announced Exile cost part
// on the pending cast. It is xAsk's Exile arm: reading every zone a part pays
// from (not the graveyard alone) so an ExileCtrlOrGrave<X/...> part cannot cap
// the announcement below what its pick can settle. Extracted so xAsk stays
// within its frozen length.
func announcedExileBounds(e exileCostBoard, pc *pendingCast, applyCap func(int32)) {
	for _, part := range pc.cost.Exile {
		if !part.Announced {
			continue
		}
		applyCap(int32(len(announcedExileCandidates(e, pc, part))))
	}
}

func (e *Engine) exAsk() bool {
	pc := e.cast
	// ExileFromTop is settled after the mana window, immediately before payment:
	// a mana ability may draw or reorder the library, so locking IDs here would
	// pay a card that is no longer on top. There is no chooser for this cost.
	for pc.ExilePart < len(pc.cost.Exile) {
		part := pc.cost.Exile[pc.ExilePart]
		zones := exilePartZones(part)

		// The announce-bound filter (the Shoal cycle's cmcEQX): the announced
		// X binds the spec's non-literal RHS through SpecContext.Resolve — the
		// same closure mechanism a Chosen* predicate resolves through — so
		// the part's candidates are exactly the cards at the announced mana
		// value. pc.x is already settled (xAsk's announce arm ran first in
		// continueCast).
		var sc *effects.SpecContext
		if pc.announceX != "" {
			name := pc.announceX
			bound := e.withNames(effects.NewSpecContext(pc.player, pc.card))
			bound.Resolve = func(n string) (int32, bool) {
				if n == name {
					return pc.x, true
				}
				return 0, false
			}
			sc = &bound
		}
		var candidates []state.ObjID
		for _, zone := range zones {
			for _, oid := range pay.ExileCostCandidates(e.G, zone, pc.player, part) {
				// A CAST (pc.ability < 0) can never exile the card it is casting:
				// the card sits in this zone until pushCast runs (CR 601.2a pushes
				// AFTER the cost asks), so without this skip a `Card` spec would
				// offer the cast card as its own ExileFromGrave fodder -- the
				// payment side of the same self-exclusion nonManaCastable applies
				// to the affordability walk. An ability activation (pc.ability >=
				// 0) is untouched: encore's Cost$ ExileFromGrave<1/CARDNAME>
				// really does exile its own source.
				if !pc.isAbility() && oid == pc.card {
					continue
				}
				// A battlefield Exile cost candidate is withheld by a CantExile
				// static whose ForCost$ True restricts cost payments -- the
				// exAsk half of the same guard nonManaCastable's offer walk
				// applies, keeping the ask from offering an unpayable permanent
				// (which would abort the cast). ForCost$ False (The Master)
				// leaves the candidate offered.
				if zone == state.ZBattlefield && e.exileBlockedForCost(oid, pay.CostCauseForAbility(pc.isAbility())) {
					continue
				}
				wholeZone := isWholeZoneExileSpec(part.Spec)
				match := wholeZone || (part.Referent != 0 && oid == part.Referent) ||
					(part.Referent == 0 && e.matchesSpecFrom(part.Spec, oid, pc.player, pc.card))
				if sc != nil && !wholeZone && part.Referent == 0 {
					match = e.matchesSpec(part.Spec, oid, *sc)
				}
				if match {
					already := false
					for _, s := range pc.Exiles {
						if s == oid {
							already = true
							break
						}
					}
					if !already {
						candidates = append(candidates, oid)
					}
				}
			}
		}
		n := int(part.N)
		if part.Announced {
			n = int(pc.x)
		}
		named := isNamedCountPart(part)
		if named {
			n = int(pc.namedN)
		}
		if n < 0 || n > len(candidates) || (!part.Announced && !named && n == 0) {
			e.abortCast(pc, "exile cost no longer payable; cast/activation aborted", true)
			return true
		}
		if n == 0 {
			pc.ExilePart++
			continue
		}
		if isWholeZoneExileSpec(part.Spec) {
			// ExileFromHand<1/All> pays the WHOLE zone and never asks: every
			// candidate is the payment (the isWholeZoneExileSpec reading the
			// triggered window's arm takes). The count check above still
			// demands part.N cards. No cast/activation corpus carrier exists
			// today; the wiring keeps the paths from diverging.
			pc.Exiles = append(pc.Exiles, candidates...)
			pc.ExilePart++
			continue
		}
		// A singleton self-reference (encore's ExileFromGrave<1/CARDNAME>, the
		// sole candidate being the resolving card itself) has no player choice.
		if !part.Announced && part.N == 1 && len(candidates) == 1 && candidates[0] == pc.card &&
			strings.EqualFold(part.Spec, "CARDNAME") {
			pc.Exiles = append(pc.Exiles, pc.card)
			pc.ExilePart++
			continue
		}
		zoneName := "hand"
		if part.ZoneSet != 0 {
			zoneName = "battlefield or graveyard"
		} else if len(zones) != 0 && zones[0] == state.ZGraveyard {
			zoneName = "graveyard"
		} else if len(zones) != 0 && zones[0] == state.ZBattlefield {
			zoneName = "battlefield"
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: n, Max: n,
			Prompt: "Exile " + strconv.Itoa(n) + " card(s) from your " + zoneName +
				" to cast " + e.targetName(pc.card), Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "exilecost",
				Obj: id, Label: e.G.Obj(id).Face().Name})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

// returnAsk offers the next unsettled Return cost part (Return<N/Spec>:
// a permanent matching Spec returned to its OWNER's hand -- Forge
// CostReturn.doPayment's moveToHand), walking pc.cost.Return in order
// (pc.returnPart) the way exAsk walks pc.cost.Exile. A Spec of CARDNAME is
// Forge's payCostFromSource: the resolving permanent itself is the sole
// candidate (Chthonian Nightmare's "Return Chthonian Nightmare to its
// owner's hand"), so no decision is posed. Any other Spec walks the payer's
// battlefield. The settle is payCast's: the chosen objects move to their
// owner's hand beside the other cost payments, so an abort cannot leave a
// partially paid return on the board.
func (e *Engine) returnAsk() bool {
	pc := e.cast
	for pc.returnPart < len(pc.cost.Return) {
		part := pc.cost.Return[pc.returnPart]
		spec := pay.SacrificeMatchSpec(part.Spec)
		var candidates []state.ObjID
		if strings.EqualFold(spec, "CARDNAME") {
			if o := e.G.Obj(pc.card); o != nil && o.Zone == state.ZBattlefield {
				candidates = append(candidates, pc.card)
			}
		} else {
			candidates = pay.CostCandidates(asPayer(e), pc.player, pc.card, state.ZBattlefield, spec, false, false)
		}
		n := int(part.N)
		if n <= 0 || n > len(candidates) {
			e.abortCast(pc, "return cost no longer payable; cast/activation aborted", true)
			return true
		}
		// A singleton self-reference has no player choice, mirroring
		// sacAsk/exAsk's CARDNAME singleton rule.
		if part.N == 1 && len(candidates) == 1 && candidates[0] == pc.card &&
			strings.EqualFold(spec, "CARDNAME") {
			pc.returns = append(pc.returns, pc.card)
			pc.returnPart++
			continue
		}
		verb := "cast " + e.targetName(pc.card)
		if pc.isAbility() {
			verb = "activate"
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: n, Max: n,
			Prompt: "Return " + strconv.Itoa(n) + " permanent(s) to their owner's hand to " + verb,
			Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "returncost",
				Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

// putToLibAsk offers the next unsettled PutToLib cost part
// (PutCardToLibFrom<Zone><N/Pos/Spec>: cards matching Spec moved from the
// payer's Hand, Graveyard or Battlefield to the top or bottom of their OWNER's
// library -- Forge CostPutCardToLib.doPayment), walking pc.cost.PutToLib in
// order (pc.putToLibPart) the way returnAsk walks pc.cost.Return. A Spec of
// CARDNAME from the BATTLEFIELD is Forge's payCostFromSource: the resolving
// permanent itself is the sole candidate (Timestream Navigator's "{2}{U}{U},
// {T}, Put Timestream Navigator on the bottom of its owner's library"), so no
// decision is posed. Any other zone/spec walks the payer's own zone (a hand
// or graveyard cost may still name the source itself when the ability is
// activated from there -- no corpus carrier does, but costCandidates resolves
// it). The settle is payCast's: the chosen objects move to their owner's
// library beside the other cost payments, so an abort cannot leave a
// partially paid placement on the board.
func (e *Engine) putToLibAsk() bool {
	pc := e.cast
	for pc.putToLibPart < len(pc.cost.PutToLib) {
		part := pc.cost.PutToLib[pc.putToLibPart]
		spec := pay.SacrificeMatchSpec(part.Spec)
		var candidates []state.ObjID
		if part.Zone == state.ZBattlefield && strings.EqualFold(spec, "CARDNAME") {
			// The source itself is the sole candidate (Forge's
			// payCostFromSource) -- but only while the payer still controls it:
			// a control-changed source is not a cost the payer can pay, so
			// leaving it out sends the ability to the no-candidates abort
			// below instead of paying with a permanent the payer does not own
			// the choice over (0 corpus carriers; fail-closed).
			if o := e.G.Obj(pc.card); o != nil && o.Zone == state.ZBattlefield && o.Controller == pc.player {
				candidates = append(candidates, pc.card)
			}
		} else {
			candidates = pay.CostCandidates(asPayer(e), pc.player, pc.card, part.Zone, spec, false, false)
		}
		n := int(part.N)
		if n <= 0 || n > len(candidates) {
			e.abortCast(pc, "put-to-library cost no longer payable; cast/activation aborted", true)
			return true
		}
		// A singleton self-reference (CARDNAME from the battlefield) has no
		// player choice, mirroring sacAsk/exAsk/returnAsk's CARDNAME rule.
		if part.N == 1 && len(candidates) == 1 && candidates[0] == pc.card &&
			part.Zone == state.ZBattlefield && strings.EqualFold(spec, "CARDNAME") {
			pc.putToLibs = append(pc.putToLibs, pc.card)
			pc.putToLibPart++
			continue
		}
		verb := "cast " + e.targetName(pc.card)
		if pc.isAbility() {
			verb = "activate"
		}
		dest := "the top of their owner's library"
		if part.LibraryPos == -1 {
			dest = "the bottom of their owner's library"
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: n, Max: n,
			Prompt: "Put " + strconv.Itoa(n) + " card(s) from your " + putToLibZoneName(part.Zone) +
				" on " + dest + " to " + verb,
			Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "puttolibcost",
				Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

// putToLibZoneName names a PutToLib part's origin zone in a human-readable
// prompt. It is the cost-flow vocabulary, deliberately separate from
// handDestPhrase/destinationPhrase in effects.
func putToLibZoneName(z state.Zone) string {
	switch z {
	case state.ZHand:
		return "hand"
	case state.ZGraveyard:
		return "graveyard"
	default:
		return "battlefield"
	}
}

// moveGraveAsk offers the next unsettled ExiledMoveToGrave cost part, walking
// pc.cost.MoveToGrave in order (pc.moveGravePart) the way exAsk walks
// pc.cost.Exile. Candidates come from EVERY alive player's exile zone
// (moveToGraveCandidates above -- the origin is always exile, the owner's
// zone is where the card sits), filtered through MatchesSpecFrom so
// Card.ExiledWithSource resolves against the ability's source. A part with
// too few candidates aborts the whole cast (nothing has moved yet).
func (e *Engine) moveGraveAsk() bool {
	pc := e.cast
	for pc.moveGravePart < len(pc.cost.MoveToGrave) {
		part := pc.cost.MoveToGrave[pc.moveGravePart]
		seen := make(map[state.ObjID]bool, len(pc.moveGraves))
		for _, s := range pc.moveGraves {
			seen[s] = true
		}
		candidates := pay.MoveToGraveCandidates(asPayer(e), pc.player, pc.card, part.Spec, seen)
		n := int(part.N)
		if n <= 0 || n > len(candidates) {
			e.abortCast(pc, "move-to-graveyard cost no longer payable; cast/activation aborted", true)
			return true
		}
		verb := "cast " + e.targetName(pc.card)
		if pc.isAbility() {
			verb = "activate"
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: n, Max: n,
			Prompt: "Move " + strconv.Itoa(n) + " card(s) from exile to their owner's graveyard to " + verb,
			Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "movetogravecost",
				Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

// castModeAsk poses CR 601.2b's mode announcement for a modal spell. It uses
// KModes like the placement and resolution paths, but ResumeKind distinguishes
// this cast-transaction continuation from both: handleModes records the answer
// on the proposed spell and re-enters continueCast rather than resuming an
// effect or the trigger drain. Activated abilities retain their existing
// resolution-time behaviour; CR 603.3c triggered abilities remain owned by
// askTriggerModes.
func (e *Engine) castModeAsk() bool {
	pc := e.cast
	if pc == nil || pc.isAbility() || pc.modesDone {
		return false
	}
	pc.modesDone = true
	o := e.G.Obj(pc.card)
	if o == nil || o.Face() == nil {
		return false
	}
	f := o.Face()
	sa := f.SpellAbility()
	if sa == nil || sa.API != "Charm" || !effects.CharmOf(sa).HasChoices {
		return false
	}
	ctx := effects.NewCtxPtr(pc.card, pc.player, effects.CtxInit{})
	ctx.Kicker.PendingKicked = modeIsKicked(pc.mode)
	// CR 702.194b-c: the mode count is fixed by the declared Teamwork
	// election at 601.2b, before the optional tap payment is answered.
	ctx.Kicker.PendingTeamwork = castAnswerCodes.Code(pc.mode) == castAnswerTeamworkMode
	effects.SetSVars(ctx, f.SVars)
	if effects.CharmRandomChosen(e, ctx, sa) {
		// param:api:Charm.Random: a random Charm's mode announcement is not
		// asked (measured corpus-unreachable -- every Random$ Charm carrier,
		// 5 files, is a trigger body -- so this site is latent). Resolution's
		// effCharm picks the mode with the engine's rng (Random$ True, or
		// Random$ Compare while the comparison holds) or poses the ordinary
		// KModes ask there; modesDone is already set, so the announcement is
		// simply skipped and the per-mode legality filter above never
		// narrows the pool the rng would pick from.
		return false
	}
	choices := effects.CharmOf(sa).Modes
	// The potential pool (a pure read) is the colour-aware upper bound the
	// per-mode cost filter below prices against: at this point in the cast no
	// mana has been floated yet (the CR 601.2g window is in payCast), so the
	// floating pool alone would wrongly withhold every payable mode.
	pot := e.PotentialMana(pc.player)
	legal := make([]string, 0, len(choices))
	for _, name := range choices {
		name = strings.TrimSpace(name)
		sub := cards.ResolveSVar(f.SVars, name)
		if sub != nil && !modeTargetsAvailable(e, pc.player, pc.card, sub, pc.x, false) {
			continue
		}
		// CR 601.2b/702.171b: a Spree/Tiered mode's own ModeCost$ is an
		// additional cost charged per chosen mode. A mode whose cost cannot be
		// paid even after floating every untapped source is not a legal
		// announcement -- the same no-progress suppression the target-legality
		// filter above applies, and what makes a mode declined for cost ABSENT
		// rather than free. It is only a per-mode necessary condition: an
		// unaffordable COMBINATION of individually affordable modes still
		// aborts at payment (CR 733.1, the ordinary reversal), so no legal cast
		// is lost here and no unpayable cast is silently allowed.
		// modeCostFeasible prices the mode through the same modifier snapshot
		// the charge applies (pc.mods, with the potential-target retry), so a
		// ReduceCost/SetCost static that makes a mode payable is seen; the
		// price is against the potential pool (no mana floated yet at 601.2b).
		//
		// A ModeCost$ this build cannot price (ParseCost leaves an unknown
		// token) is withheld outright: an unparseable mandatory cost must never
		// degrade to a free mode.
		if modeCostUnparseable(f, name) {
			continue
		}
		if mc, present, ok := modeCost(f, name); present && ok {
			if !e.modeCostFeasible(pc, mc, pot) {
				continue
			}
		}
		legal = append(legal, name)
	}
	// ChoiceRestriction$: a Charm cast (no corpus carrier today, but the
	// class) still cannot announce a mode it already chose on the same source
	// under the scope. Filtered before the bounds clamp, exactly as the
	// triggered and mid-resolution asks do.
	legal = effects.CharmEligibleModes(e, pc.card, sa, legal)
	min, max, repeat := effects.CharmModeBounds(e, ctx, sa, len(legal))
	// Entwine (CR 702.42b): "If the entwine cost was paid, follow the
	// instructions of all its modes." So an entwined cast announces EVERY
	// eligible mode -- Min and Max both move to the filtered legal count, and
	// the answer handler, Decision.Validate and the bot arm all read those
	// bounds (the one-home rule), so no other site needs an entwine branch.
	// The cost was already folded into pc.cost by beginCast, so no
	// affordability clamp applies here: a cost that could not be paid never
	// offered, and an entwined cast pays it whether one or all modes are
	// legal. A repeatable Charm forces each distinct mode exactly once --
	// "follow the instructions of all its modes" is a distinct-mode pick, not
	// a licence to repeat one -- so the same distinct count is forced there
	// too (no corpus carrier is both repeatable and Entwine, but the
	// semantics are the same either way). When NO mode is legal the ordinary
	// min>len(legal) abort below still fires and the charge reverses through
	// the CR 733.1 path -- the card plus cost never strands.
	if pc.mode == "entwined" {
		if len(legal) == 0 {
			// An entwined cast must follow ALL modes, so with no eligible
			// mode the additional cost buys nothing and the cast is not a
			// legal announcement. Abort it loudly (the CR 733.1 reversal
			// path restores the board) rather than posing an empty ask or
			// silently resolving zero modes.
			e.abortCast(pc, "cast aborted: no legal modal choice", true)
			return true
		}
		min = len(legal)
		max = len(legal)
	}
	// Escalate (the modal additional cost): a cast choosing N modes pays the
	// escalate cost N-1 times, so a mode count the board cannot pay for is
	// not a legal announcement -- clamp Max to 1 + the largest number of
	// escalate payments the SAME affordability checker the payment window's
	// composed total faces (castable) still admits, the replicateAsk shape,
	// pool-only at ask time (the CR 601.2g window afterwards may still
	// produce mana). The loop is bounded by the bounds Max itself, so it
	// terminates. An unpriceable parameter cannot clamp (it also cannot
	// charge -- the answer handler emits a loud Note instead), so the
	// CharmNum$ bounds stay honest on the wire.
	if pc.escalateSet {
		if esc := ParseCost(pc.escalateParam); len(esc.Unknown) == 0 {
			maxEscalations := 0
			cand := pc.cost
			for 1+maxEscalations < max {
				if !e.castable(pc.player, pc.card, cand.Plus(esc), false) {
					break
				}
				cand = cand.Plus(esc)
				maxEscalations++
			}
			if clamped := 1 + maxEscalations; clamped < max {
				max = clamped
			}
		}
	}
	if min > len(legal) && !repeat {
		// No legal set of modes can complete its required target choices or
		// pay its per-mode costs. This is the modal counterpart of targetAsk's
		// no-legal-target reversal; use the no-progress suppression so an
		// automated seat cannot propose the same impossible cast forever. A
		// repeatable Charm can fill its slots by repeating an eligible mode, so
		// it never aborts here.
		e.abortCast(pc, "cast aborted: no legal modal choice", true)
		return true
	}
	// The defensive floor: a clamp below MinCharmNum$ cannot occur for the
	// corpus (every Escalate carrier's MinCharmNum$ is 1 and the clamp's
	// floor is 1 + 0 = 1), but a Min-2 future carrier on an unaffordable
	// board must abort loudly rather than pose an ask no legal answer
	// satisfies -- the same no-progress suppression as the no-legal-mode
	// abort above.
	if max < min {
		e.abortCast(pc, "cast aborted: no affordable modal choice", true)
		return true
	}
	d := modeDecisionForChoices(pc.player, pc.card, sa, f.SVars, legal, min, max, repeat)
	d.ResumeKind = "cast_modes"
	if effects.OnlyEmptyAnswer(d) {
		// "Choose up to N" (MinCharmNum$ 0) with no mode that has a legal
		// target or an affordable cost: the only legal announcement is zero
		// modes (Call Damage Control with an empty graveyard). Nobody could
		// answer differently, so record it without posting the decision --
		// the same silent resolution effects.Ask gives this shape, and what
		// Engine.ask requires of every asking site.
		e.applyCastModes(d, pc.player, nil)
		return true
	}
	e.ask(d)
	return true
}

// modalTargetSA returns the first target declaration of a modal spell for
// legacy single-mode and unsupported multi-target paths. Supported distinct
// modes instead use the per-mode grouped ask in targetAsk.
func modalTargetSA(f *cards.Face, sa *cards.SA, modes []string) *cards.SA {
	if sa == nil || effects.TargetsOf(sa).Targeted() || sa.API != "Charm" || f == nil {
		return sa
	}
	for _, name := range modes {
		if sub := cards.ResolveSVar(f.SVars, name); sub != nil && effects.TargetsOf(sub).Targeted() {
			return sub
		}
	}
	return sa
}

// replicateAsk poses CR 702.55a's replicate count question -- "you may pay
// [the replicate cost] any number of times as you cast this spell" -- once,
// before the Convoke/Harmonize and X stages, whose asks must see and bound
// against the composed total. The max is the largest N the current board can
// still pay, checked with the SAME affordability checker the payment window's
// composed total faces (castable: the conversion-aware mana gate plus every
// non-mana part), pool-only at ask time -- the 601.2g window afterwards may
// still produce mana for the composed total, exactly like a kicked cast. The
// answered count folds that many payments into cost (castAnswer); 0 declines:
// no flag, an exactly plain cast.
func (e *Engine) replicateAsk() bool {
	pc := e.cast
	if pc.replicateDone || !pc.replicateSet {
		return false
	}
	pc.replicateDone = true
	rc := ParseCost(pc.replicateParam)
	max := int32(0)
	cand := pc.cost
	for i := int32(0); i < 64; i++ {
		// The hard cap only exists so a degenerate future cost whose every
		// part prices against a non-reserving candidate count cannot loop;
		// every real replicate resource (mana, energy, life, tap/sac
		// candidates) is finite and breaks the loop naturally.
		next := cand.Plus(rc)
		if !e.countCandPayable(pc, next) {
			break
		}
		cand = next
		max++
	}
	if max == 0 {
		// The offer gate proved one payment payable; a board that changed
		// under the proposal (or a cost modifier that priced the OFFER but
		// not this loop's bare, unmodified cost -- the bound here is
		// deliberately conservative, never over-offering) degrades the
		// explicitly chosen "(replicated)" mode to the count-0 plain cast
		// (the conservative CR 733 direction) rather than wedging or
		// aborting. The degrade is loud: a silent downgrade would leave the
		// player's choice unrecorded.
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
			Text: "replicate no longer payable; casting without replicate"})
		return false
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Pay the replicate cost how many times?", Source: pc.card}
	for n := int32(0); n <= max; n++ {
		label := "No replicate"
		if n == 1 {
			label = "Pay replicate once"
		} else if n > 1 {
			label = fmt.Sprintf("Pay replicate %d times", n)
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "replicate",
			Label: label, Amount: int(n)})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// multikickAsk poses CR 702.43's multikicker count question -- "you may pay
// [the multikicker cost] any number of times as you cast this spell" -- once,
// the replicateAsk shape (the same cast announcement, CR 601.2b): the max is
// the largest N the current board can still pay, walked with the SAME
// affordability checker the payment window's composed total faces (castable),
// pool-only at ask time. The answered count folds that many payments into
// cost (castAnswer); 0 declines: no flag, an exactly plain cast.
func (e *Engine) multikickAsk() bool {
	pc := e.cast
	if pc.multikickDone || !pc.multikickSet {
		return false
	}
	pc.multikickDone = true
	mk := ParseCost(pc.multikickParam)
	max := int32(0)
	cand := pc.cost
	for i := int32(0); i < 64; i++ {
		// The hard cap only exists so a degenerate future cost whose every
		// part prices against a non-reserving candidate count cannot loop;
		// every real multikicker resource is finite and breaks the loop
		// naturally (the replicateAsk comment).
		next := cand.Plus(mk)
		if !e.countCandPayable(pc, next) {
			break
		}
		cand = next
		max++
	}
	if max == 0 {
		// The offer gate proved one payment payable; a board that changed
		// under the proposal degrades the explicitly chosen
		// "(multikicked)" mode to the count-0 plain cast (the replicateAsk
		// max==0 arm's conservative direction), never a wedge or abort.
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
			Text: "multikicker no longer payable; casting without multikick"})
		return false
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Pay the multikicker cost how many times?", Source: pc.card}
	for n := int32(0); n <= max; n++ {
		label := "No multikick"
		if n == 1 {
			label = "Pay multikicker once"
		} else if n > 1 {
			label = fmt.Sprintf("Pay multikicker %d times", n)
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "multikick",
			Label: label, Amount: int(n)})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// squadAsk poses CR 702.66's squad count question -- "you may pay [the squad
// cost] any number of times as you cast this spell" -- once, the
// replicateAsk/multikickAsk shape (the same cast announcement, CR 601.2b): the
// max is the largest N the current board can still pay, walked with the SAME
// affordability checker the payment window's composed total faces (castable),
// pool-only at ask time. The answered count folds that many payments into
// cost (castAnswer); 0 declines: no flag, an exactly plain cast.
func (e *Engine) squadAsk() bool {
	pc := e.cast
	if pc.squadDone || !pc.squadSet {
		return false
	}
	pc.squadDone = true
	sc := ParseCost(pc.squadParam)
	max := int32(0)
	cand := pc.cost
	for i := int32(0); i < 64; i++ {
		// The hard cap only exists so a degenerate future cost whose every
		// part prices against a non-reserving candidate count cannot loop;
		// every real squad resource (mana, energy, life, tap/sac candidates)
		// is finite and breaks the loop naturally (the replicateAsk
		// comment).
		next := cand.Plus(sc)
		if !e.countCandPayable(pc, next) {
			break
		}
		cand = next
		max++
	}
	if max == 0 {
		// The offer gate proved one payment payable; a board that changed
		// under the proposal degrades the explicitly chosen "(squadded)"
		// mode to the count-0 plain cast (the replicateAsk max==0 arm's
		// conservative direction), never a wedge or abort. The degrade is
		// loud: a silent downgrade would leave the player's choice
		// unrecorded.
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
			Text: "squad no longer payable; casting without squad"})
		return false
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Pay the squad cost how many times?", Source: pc.card}
	for n := int32(0); n <= max; n++ {
		label := "No squad"
		if n == 1 {
			label = "Pay squad once"
		} else if n > 1 {
			label = fmt.Sprintf("Pay squad %d times", n)
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "squad",
			Label: label, Amount: int(n)})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// conspireAsk poses CR 702.78a's tap election for a "conspired" cast: tap two
// untapped creatures you control that share a colour with the spell. A
// dedicated ask (not a costCandidates-driven TapPermanent part) because the
// eligibility is a COLOUR INTERSECTION with the spell itself, which no cost
// spec in the filter vocabulary can express today (measured: no
// sharesColorWith predicate). With exactly two eligible creatures the tap is
// forced and no decision is posed (the strict-supersets convention); with
// more, a Min==Max==2 KChoose is posed over exactly the eligible set. A board
// that changed under the proposal (fewer than two eligible) degrades the
// explicitly chosen "(conspired)" mode to the plain cast with a loud Note,
// the replicateAsk/multikickAsk max==0 direction.
func (e *Engine) conspireAsk() bool {
	pc := e.cast
	if pc.conspireDone || !pc.conspireSet {
		return false
	}
	pc.conspireDone = true
	candidates := e.conspireCandidates(pc.player, pc.card)
	if len(candidates) < 2 {
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
			Text: "conspire no longer payable; casting without conspire"})
		return false
	}
	if len(candidates) == 2 {
		pc.Taps = append(pc.Taps, candidates...)
		pc.conspirePaid = true
		return false
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 2, Max: 2,
		Prompt: "Choose two creatures to tap for conspire", Source: pc.card}
	for _, id := range candidates {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "conspire", Obj: id, Label: e.targetName(id)})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}
