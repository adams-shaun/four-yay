package pay

import (
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The mana-ability cost election (lasagna spec §9.2, E7 flow slice 2): the
// choice-bearing parts of a mana ability's cost, elected one stage at a time
// over the session's ManaCost. Every ask goes through Engine.Ask; what follows
// a completed election (the commit, the mana effect, the window or cast it
// belongs to) is the engine's flow, so each stage reports a ManaCostStep.

// ManaCostStep is the outcome of a mana-cost election stage.
type ManaCostStep uint8

const (
	// ManaCostNext: the stage is complete; the election continues.
	ManaCostNext ManaCostStep = iota
	// ManaCostAsked: the stage posed an ask; its answer resumes the election.
	ManaCostAsked
	// ManaCostDropped: the board changed under the offer, so the payment was
	// dropped (the session's ManaCost is nil).
	ManaCostDropped
)

// ManaCostTapStage elects the tapXType<N/Spec> permanents of the mana
// ability's cost (md, the session's ManaCost). It runs first: the tap stage
// claims its permanents before the sacrifice stage's reserved map includes
// them, and the tap candidates reserve the sacrifice picks the offer walk
// already made (a permanent cannot pay two parts of one cost). A non-X Dyn
// part never reaches here -- manaTapsPayable refused the whole ability.
func ManaCostTapStage(e Engine, md *ManaCostActivation) ManaCostStep {
	for md.TapPart < len(md.Cost.TapPermanent) {
		part := md.Cost.TapPermanent[md.TapPart]
		claimed := make(map[state.ObjID]bool, len(md.Taps)+len(md.Sacs)+1)
		for _, id := range md.Taps {
			claimed[id] = true
		}
		for _, id := range md.Sacs {
			claimed[id] = true
		}
		if md.Cost.Tap {
			claimed[md.Source] = true
		}
		candidates := ManaTapCandidates(e, md.Player, md.Source, part.Spec, claimed)
		if part.Dyn == "X" {
			if len(candidates) == 0 {
				md.Ability = ManaAbilityWithPaidX(md.Ability, 0)
				md.TapPart++ // X=0 needs no empty decision.
				continue
			}
			d := &decision.Decision{Player: md.Player, Kind: decision.KChoose, Min: 0, Max: len(candidates),
				Prompt: "Choose any number of tokens to tap for the mana ability", Source: md.Source}
			for _, id := range candidates {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "tapcost", Obj: id, Label: TargetName(e.Game(), id)})
			}
			e.Ask(AskManaTap, d)
			return ManaCostAsked
		}
		// The offer gate agreed, so a shortfall is a board that changed under
		// the offer: drop the payment rather than ask an election no answer
		// can satisfy.
		if int32(len(candidates)) < part.N {
			e.Session().ManaCost = nil
			return ManaCostDropped
		}
		if int32(len(candidates)) == part.N {
			// Exactly N candidates makes the tap forced. Record them without a
			// zero-information ask, matching tapPermanentCostAsk.
			md.Taps = append(md.Taps, candidates[:int(part.N)]...)
			md.TapPart++
			continue
		}
		d := &decision.Decision{Player: md.Player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
			Prompt: "Choose permanents to tap for the mana ability", Source: md.Source}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "tapcost", Obj: id, Label: TargetName(e.Game(), id)})
		}
		e.Ask(AskManaTap, d)
		return ManaCostAsked
	}
	return ManaCostNext
}

// ManaCostChoiceStages runs the mana ability's remaining cost elections in
// order -- Forage, sacrifice, discard, exile, untapYType -- each recording
// a forced pick without an ask, pausing on an ask, or dropping the payment
// when the board changed under the offer.
func ManaCostChoiceStages(e Engine, md *ManaCostActivation) ManaCostStep {
	if st := manaCostForageStage(e, md); st != ManaCostNext {
		return st
	}
	for md.SacPart < len(md.Cost.Sac) {
		part := md.Cost.Sac[md.SacPart]
		reserved := make(map[state.ObjID]bool, len(md.Sacs)+len(md.Taps))
		for _, id := range md.Sacs {
			reserved[id] = true
		}
		// A permanent elected to tap cannot also be sacrificed (one permanent
		// cannot pay two parts of one cost).
		for _, id := range md.Taps {
			reserved[id] = true
		}
		var candidates []state.ObjID
		for _, id := range SacrificeCostCandidates(e, md.Player, md.Source, part, true) {
			if !reserved[id] {
				candidates = append(candidates, id)
			}
		}
		n := int(part.N) - md.SacPaid
		// The same split sacAsk makes: only a part with a LATER Sac part is
		// paid one unit per decision under the feasibility filter (every
		// offered option leaves a complete distinct assignment for the later
		// parts, so Validate, Clamp and the bot cannot strand one). The LAST
		// Sac part keeps the historical exact-N shape -- forced when exactly n
		// candidates remain, else one Min == Max == n ask -- because nothing
		// downstream can be stranded by its answer.
		hasLaterSac := md.SacPart+1 < len(md.Cost.Sac)
		if hasLaterSac {
			pools := make([][]state.ObjID, len(md.Cost.Sac))
			needs := make([]int, len(md.Cost.Sac))
			for i, futurePart := range md.Cost.Sac {
				needs[i] = int(futurePart.N)
				if i == md.SacPart {
					needs[i] -= md.SacPaid
				}
				pools[i] = SacrificeCostCandidates(e, md.Player, md.Source, futurePart, true)
			}
			candidates = FeasibleSacrificeChoices(candidates, pools, needs, md.Sacs, md.SacPart)
		}
		if n <= 0 || n > len(candidates) {
			e.Session().ManaCost = nil
			return ManaCostDropped
		}
		var d *decision.Decision
		if hasLaterSac {
			// A singleton candidate is forced. Record it and continue one unit
			// at a time so each pick preserves a complete assignment for later
			// parts.
			if len(candidates) == 1 {
				md.Sacs = append(md.Sacs, candidates[0])
				md.SacPaid++
				if md.SacPaid >= int(part.N) {
					md.SacPart++
					md.SacPaid = 0
				}
				continue
			}
			d = &decision.Decision{Player: md.Player, Kind: decision.KChoose, Min: 1, Max: 1,
				Prompt: "Choose permanents to sacrifice for the mana ability", Source: md.Source}
		} else {
			// Exactly N candidates makes the sacrifice forced. Record that
			// deterministic battlefield-order set without a zero-information
			// ask; only a wider candidate set gives the player a choice.
			if len(candidates) == n {
				md.Sacs = append(md.Sacs, candidates...)
				md.SacPart++
				md.SacPaid = 0
				continue
			}
			d = &decision.Decision{Player: md.Player, Kind: decision.KChoose, Min: n, Max: n,
				Prompt: "Choose permanents to sacrifice for the mana ability", Source: md.Source}
		}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "sacrifice", Obj: id, Label: e.Game().Obj(id).Face().Name})
		}
		e.Ask(AskManaSacrifice, d)
		return ManaCostAsked
	}
	for md.Part < len(md.Cost.Discard) {
		part := md.Cost.Discard[md.Part]
		reserved := make(map[state.ObjID]bool, len(md.Discards))
		for _, id := range md.Discards {
			reserved[id] = true
		}
		candidates := DiscardCandidates(e, md.Player, md.Source, part, false, reserved)
		if strings.EqualFold(part.Spec, "Hand") {
			md.Discards = append(md.Discards, candidates...)
			md.Part++
			continue
		}
		n := int(part.N)
		if n <= 0 || n > len(candidates) {
			e.Session().ManaCost = nil
			return ManaCostDropped
		}
		if strings.EqualFold(part.Spec, "Random") {
			for i := 0; i < n; i++ {
				pick := e.Rand(len(candidates))
				md.Discards = append(md.Discards, candidates[pick])
				candidates = append(candidates[:pick], candidates[pick+1:]...)
			}
			md.Part++
			continue
		}
		d := &decision.Decision{Player: md.Player, Kind: decision.KChoose, Min: n, Max: n,
			Prompt: "Discard a card to pay the mana ability cost", Source: md.Source}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "mana_discard",
				Obj: id, Label: e.Game().Obj(id).Face().Name})
		}
		e.Ask(AskManaDiscard, d)
		return ManaCostAsked
	}
	for md.ExilePart < len(md.Cost.Exile) {
		part := md.Cost.Exile[md.ExilePart]
		zone := part.Zone
		if zone == 0 {
			zone = state.ZHand
		}
		reserved := make(map[state.ObjID]bool, len(md.Exiles))
		for _, id := range md.Exiles {
			reserved[id] = true
		}
		var candidates []state.ObjID
		for _, id := range e.Game().Zone(zone, md.Player) {
			if !reserved[id] && e.MatchesSpecFrom(part.Spec, id, md.Player, md.Source) {
				candidates = append(candidates, id)
			}
		}
		n := int(part.N)
		if n <= 0 || n > len(candidates) {
			e.Session().ManaCost = nil
			return ManaCostDropped
		}
		if n == 1 && len(candidates) == 1 && candidates[0] == md.Source &&
			strings.EqualFold(part.Spec, "CARDNAME") {
			md.Exiles = append(md.Exiles, md.Source)
			md.ExilePart++
			continue
		}
		d := &decision.Decision{Player: md.Player, Kind: decision.KChoose, Min: n, Max: n,
			Prompt: "Exile a card to pay the mana ability cost", Source: md.Source}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "mana_exile",
				Obj: id, Label: e.Game().Obj(id).Face().Name})
		}
		e.Ask(AskManaExile, d)
		return ManaCostAsked
	}
	return manaCostUntapStage(e, md)
}

// manaCostForageStage poses the Forage election (exile three graveyard cards OR
// sacrifice a Food), sharing the cast path's option kinds so the bot's
// existing arms answer it. A non-interactive caller takes the deterministic
// graveyard arm when it is payable, else the first Food.
func manaCostForageStage(e Engine, md *ManaCostActivation) ManaCostStep {
	if !md.Cost.Forage || md.ForageDone {
		return ManaCostNext
	}
	md.ForageDone = true
	graveOK := len(e.Game().Zone(state.ZGraveyard, md.Player)) >= 3
	foods := CostCandidates(e, md.Player, md.Source, state.ZBattlefield, "Food.YouCtrl", false, false)
	if !md.Interactive {
		if graveOK {
			md.ForagePay = true
			return ManaCostNext
		}
		if len(foods) > 0 {
			md.ForagePay = true
			md.ForageFood = foods[0]
		}
		return ManaCostNext
	}
	d := &decision.Decision{Player: md.Player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Choose how to forage", Source: md.Source}
	if graveOK {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "forage_exile",
			Label: "Exile three cards from your graveyard"})
	}
	for _, id := range foods {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "forage_food", Obj: id,
			Label: "Sacrifice " + TargetName(e.Game(), id)})
	}
	if len(d.Options) == 0 {
		e.Session().ManaCost = nil
		return ManaCostDropped
	}
	if len(d.Options) == 1 {
		opt := d.Options[0]
		md.ForagePay = true
		if opt.Kind == "forage_food" {
			md.ForageFood = opt.Obj
		}
		return ManaCostNext
	}
	e.Ask(AskManaForage, d)
	return ManaCostAsked
}

// manaCostUntapStage elects the untapYType permanents, mirroring the literal
// tapXType election (forced when exactly N candidates remain, else an ask).
func manaCostUntapStage(e Engine, md *ManaCostActivation) ManaCostStep {
	for md.UntapPart < len(md.Cost.UntapPermanent) {
		part := md.Cost.UntapPermanent[md.UntapPart]
		claimed := map[state.ObjID]bool{}
		for _, id := range md.Untaps {
			claimed[id] = true
		}
		if md.Cost.Tap {
			claimed[md.Source] = true
		}
		candidates := ManaUntapCandidates(e, md.Player, md.Source, part.Spec, claimed)
		if int32(len(candidates)) < part.N {
			e.Session().ManaCost = nil
			return ManaCostDropped
		}
		if int32(len(candidates)) == part.N || !md.Interactive {
			md.Untaps = append(md.Untaps, candidates[:int(part.N)]...)
			md.UntapPart++
			continue
		}
		d := &decision.Decision{Player: md.Player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
			Prompt: "Choose permanents to untap for the mana ability", Source: md.Source}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "untapcost",
				Obj: id, Label: TargetName(e.Game(), id)})
		}
		e.Ask(AskManaUntap, d)
		return ManaCostAsked
	}
	return ManaCostNext
}
