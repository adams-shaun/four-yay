package pay

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// The mana ability's announced / anchored SubCounter stage (Haruspex,
// Petalmane Baku, Rasputin, Jetfire; lasagna spec §9.2, E7 flow slice 3):
// the shared X announcement (once) and each announced or anchored part's
// removal election, over the session's ManaCost. It runs between
// ManaCostTapStage and ManaCostChoiceStages and reports a ManaCostStep like
// them, so "asked" is what the stage itself did, never a read of the
// engine's pending-choice marker.

// SubCounterPhrase renders the amount/kind half of a SubCounter part's
// removal prompt ("P1P1 counters").
func SubCounterPhrase(part costvocab.CostPart, amt int32) string {
	unit := "counter"
	if amt != 1 && !part.Announced {
		unit = "counters"
	}
	return fmt.Sprintf("%s %s", strings.ToUpper(part.Spec), unit)
}

// manaSubCounterReservations keeps an earlier counter-cost part from spending
// the same permanent as the current part (the cast path's reservations' mana
// twin). Units of the current part are deliberately absent: the "Any" stage
// accounts for them by (object, kind), so a wildcard part may take its second
// unit from the permanent its first came from (reserving it dropped the
// payment on re-entry from the first unit's answer).
func manaSubCounterReservations(md *ManaCostActivation) map[state.ObjID]bool {
	reserved := map[state.ObjID]bool{}
	for _, s := range md.Sacs {
		reserved[s] = true
	}
	for _, p := range md.SubCounterPays {
		if p.Part != md.SubPart {
			reserved[p.Obj] = true
		}
	}
	return reserved
}

// ManaAbilityWithSubX rewrites the ability's Amount$ X to the announced
// SubCounter X, exactly as ManaAbilityWithPaidX does for the dynamic tap
// election. It is a no-op unless the Amount is the literal "X", so a cost
// that announces X for its counters but prices its output some other way is
// untouched. Copy-on-write keeps every other activation of the same card
// unchanged.
func ManaAbilityWithSubX(ma *cards.SA, x int32) *cards.SA {
	if ma == nil || !strings.EqualFold(effects.ManaOf(ma).AmountTrim, "x") {
		return ma
	}
	return ManaAbilityWithPaidX(ma, x)
}

// ManaCostSubCounterStage announces the shared X (once) and elects each
// announced or anchored SubCounter part's removal.
func ManaCostSubCounterStage(e Engine, md *ManaCostActivation) ManaCostStep {
	if !ManaSubCounterNeedsAsk(md.Cost) {
		return ManaCostNext
	}
	g := e.Game()
	if !md.SubXAnnounced {
		bound, _ := ManaSubCounterXBound(e, md.Player, g.Obj(md.Source), md.Source, md.Cost)
		min := md.Cost.XMin
		if min < 0 {
			min = 0
		}
		if bound < min {
			bound = min
		}
		if !md.Interactive || bound <= min {
			// No choice: a single legal value needs no zero-information ask
			// (the cast path's own convention), and a non-interactive caller
			// takes the maximum the board can settle so a silent activation
			// still produces all the mana it can. The removal-parts loop below
			// still records the mandatory payment.
			md.SubX = bound
			md.SubXAnnounced = true
			md.Ability = ManaAbilityWithSubX(md.Ability, md.SubX)
		} else {
			d := &decision.Decision{Player: md.Player, Kind: decision.KChoose, Min: 1, Max: 1,
				Prompt: "Choose a value for X", Source: md.Source}
			// Descending, maximum first: the deterministic driver's first answer
			// (the audit's drive, the bot's first-legal arm) then takes the whole
			// amount the board can settle, which is what a mana ability is for.
			// Every lower value down to the floor stays legal.
			for x := bound; x >= min; x-- {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "x",
					Label: fmt.Sprintf("X = %d", x), Amount: int(x)})
			}
			e.Ask(AskManaSubCounter, d)
			return ManaCostAsked
		}
	}
	for md.SubPart < len(md.Cost.SubCounter) {
		part := md.Cost.SubCounter[md.SubPart]
		if !part.Announced && costvocab.SubCounterTargetsSource(part.Target) {
			// A fixed source-anchored part stays with PayManaSourceParts.
			md.SubPart++
			continue
		}
		amt := part.N
		if part.Announced {
			amt = md.SubX
		}
		if amt <= 0 {
			md.SubPart++
			continue
		}
		if strings.EqualFold(part.Spec, "Any") {
			// The wildcard stage advances md.SubPart once the part's amount
			// is met; a completed part (its last unit forced, possibly on
			// re-entry from an earlier unit's answer) continues the walk.
			if st := manaSubCounterWildcardStage(e, md, part, amt); st != ManaCostNext {
				return st
			}
			continue
		}
		if costvocab.SubCounterTargetsSource(part.Target) {
			// Announced source-anchored: the settle emits on the source.
			md.SubCounterPays = append(md.SubCounterPays, SubCounterPay{Part: md.SubPart, Obj: md.Source})
			md.SubPart++
			continue
		}
		candidates := SubCounterRemovalCandidates(e, md.Player, md.Source, part, amt, manaSubCounterReservations(md))
		if len(candidates) == 0 {
			// The board changed under the offer: drop the payment rather than
			// ask an election no answer can satisfy.
			e.Session().ManaCost = nil
			return ManaCostDropped
		}
		if len(candidates) == 1 || !md.Interactive {
			md.SubCounterPays = append(md.SubCounterPays, SubCounterPay{Part: md.SubPart, Obj: candidates[0]})
			md.SubPart++
			continue
		}
		d := &decision.Decision{Player: md.Player, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: "Choose a permanent to remove " + SubCounterPhrase(part, amt) + " from", Source: md.Source}
		for _, oid := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "subcounter", Obj: oid,
				Label: g.Obj(oid).Face().Name})
		}
		e.Ask(AskManaSubCounter, d)
		return ManaCostAsked
	}
	return ManaCostNext
}

// manaSubCounterWildcardStage advances one "Any" part by one counter unit at
// a time (the cast path's wildcard ask's mana twin), pausing on an ask when
// more than one (object, kind) unit is legal. It advances md.SubPart once
// the part's full amount is picked.
func manaSubCounterWildcardStage(e Engine, md *ManaCostActivation, part costvocab.CostPart, amt int32) ManaCostStep {
	g := e.Game()
	for md.SubCounterPicked(md.SubPart) < amt {
		candidates := SubCounterRemovalCandidates(e, md.Player, md.Source, part, 1, manaSubCounterReservations(md))
		if len(candidates) == 0 {
			e.Session().ManaCost = nil
			return ManaCostDropped
		}
		type unit struct {
			obj  state.ObjID
			kind string
		}
		var units []unit
		for _, oid := range candidates {
			o := g.Obj(oid)
			if o == nil {
				continue
			}
			for _, c := range o.Counters {
				if c.N <= 0 || md.SubCounterKindUsed(md.SubPart, oid, c.Kind) {
					continue
				}
				units = append(units, unit{oid, c.Kind})
			}
		}
		if len(units) == 0 {
			e.Session().ManaCost = nil
			return ManaCostDropped
		}
		if len(units) == 1 || !md.Interactive {
			md.SubCounterPays = append(md.SubCounterPays, SubCounterPay{Part: md.SubPart, Obj: units[0].obj, Kind: units[0].kind})
			continue
		}
		d := &decision.Decision{Player: md.Player, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: "Choose a counter to remove", Source: md.Source}
		for _, u := range units {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "subcounter",
				Obj: u.obj, Counter: u.kind,
				Label: fmt.Sprintf("%s (%s counter)", g.Obj(u.obj).Face().Name, u.kind)})
		}
		e.Ask(AskManaSubCounter, d)
		return ManaCostAsked
	}
	md.SubPart++
	return ManaCostNext
}

// recordManaSubCounterAnswer records an AskManaSubCounter answer: the X
// announcement (re-clamped against the live board) while X is unannounced,
// else one removal pick. A wildcard "Any" part records one unit and stays on
// the part until its amount is met; every other part advances.
func recordManaSubCounterAnswer(e Engine, md *ManaCostActivation, chosen []decision.Option) {
	if !md.SubXAnnounced {
		if len(chosen) > 0 {
			md.SubX = int32(chosen[0].Amount)
		}
		md.SubXAnnounced = true
		// Re-clamp against the live board (counters may have moved): the
		// announced amount can never exceed what the settle can remove.
		if bound, _ := ManaSubCounterXBound(e, md.Player, e.Game().Obj(md.Source), md.Source, md.Cost); md.SubX > bound {
			md.SubX = bound
		}
		if md.SubX < md.Cost.XMin {
			md.SubX = md.Cost.XMin
		}
		md.Ability = ManaAbilityWithSubX(md.Ability, md.SubX)
		return
	}
	if md.SubPart >= len(md.Cost.SubCounter) {
		return
	}
	part := md.Cost.SubCounter[md.SubPart]
	for _, opt := range chosen {
		md.SubCounterPays = append(md.SubCounterPays, SubCounterPay{Part: md.SubPart, Obj: opt.Obj, Kind: opt.Counter})
	}
	if strings.EqualFold(part.Spec, "Any") {
		amt := part.N
		if part.Announced {
			amt = md.SubX
		}
		if md.SubCounterPicked(md.SubPart) >= amt {
			md.SubPart++
		}
	} else {
		md.SubPart++
	}
}
