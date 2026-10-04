package rules

// The mana-activation path's three extra cost settles that need a real
// election or announcement: an announced SubCounter<X/Kind> part (Haruspex,
// Petalmane Baku, Rasputin, Jetfire), the Forage cost (Thornvault Forager)
// and the literal untapYType<N/Spec> part (Benthic Explorers). Each rides the
// manaDiscardActivation continuation beside the existing sacrifice / discard
// / exile / tap parts (see continueManaDiscard), so the offer gate
// (manaCostPayableFull), the X/removal election here and the commit
// (commitManaDiscard) read one shared candidate walk and cannot disagree.

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// manaSubCounterReservations keeps an earlier counter-cost part from spending
// the same permanent as the current part (pc.subCounterReservations' mana
// twin). Wildcard units of the current part are deliberately absent:
// the "Any" stage accounts for them by (object, kind).
func (e *Engine) manaSubCounterReservations(md *manaDiscardActivation) map[state.ObjID]bool {
	reserved := map[state.ObjID]bool{}
	for _, s := range md.Sacs {
		reserved[s] = true
	}
	for _, p := range md.SubCounterPays {
		reserved[p.Obj] = true
	}
	return reserved
}

// manaSubCounterStage announces the shared X (once) and elects each announced
// or anchored SubCounter part's removal. It returns true when it posed a
// decision (the activation resumes on the answer).
func (e *Engine) manaSubCounterStage(md *manaDiscardActivation) bool {
	if !pay.ManaSubCounterNeedsAsk(md.Cost) {
		return false
	}
	if !md.SubXAnnounced {
		bound, _ := pay.ManaSubCounterXBound(asPayer(e), md.Player, e.G.Obj(md.Source), md.Source, md.Cost)
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
			// still produces all the mana it can. Do not return here: the
			// removal-parts loop below must still record the mandatory payment.
			md.SubX = bound
			md.SubXAnnounced = true
			md.Ability = manaAbilityWithSubX(md.Ability, md.SubX)
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
			e.choosing = chooseManaSubCounter
			e.ask(d)
			return true
		}
	}
	for md.SubPart < len(md.Cost.SubCounter) {
		part := md.Cost.SubCounter[md.SubPart]
		if !part.Announced && subCounterTargetsSource(part.Target) {
			// A fixed source-anchored part stays with payManaSourceParts.
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
			e.manaSubCounterWildcardStage(md, part, amt)
			return e.choosing == chooseManaSubCounter
		}
		if subCounterTargetsSource(part.Target) {
			// Announced source-anchored: the settle emits on the source.
			md.SubCounterPays = append(md.SubCounterPays, subCounterPay{Part: md.SubPart, Obj: md.Source})
			md.SubPart++
			continue
		}
		candidates := pay.SubCounterRemovalCandidates(asPayer(e), md.Player, md.Source, part, amt, e.manaSubCounterReservations(md))
		if len(candidates) == 0 {
			// The board changed under the offer: drop the payment rather than
			// ask an election no answer can satisfy.
			e.ManaCost = nil
			e.choosing = chooseNone
			return false
		}
		if len(candidates) == 1 || !md.Interactive {
			md.SubCounterPays = append(md.SubCounterPays, subCounterPay{Part: md.SubPart, Obj: candidates[0]})
			md.SubPart++
			continue
		}
		d := &decision.Decision{Player: md.Player, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: "Choose a permanent to remove " + e.subCounterPhrase(part, amt) + " from", Source: md.Source}
		for _, oid := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "subcounter", Obj: oid,
				Label: e.G.Obj(oid).Face().Name})
		}
		e.choosing = chooseManaSubCounter
		e.ask(d)
		return true
	}
	return false
}

// manaAbilityWithSubX rewrites the ability's Amount$ X to the announced
// SubCounter X, exactly as manaAbilityWithPaidX does for the dynamic tap
// election. It is a no-op unless the Amount is the literal "X", so a cost
// that announces X for its counters but prices its output some other way is
// untouched. Copy-on-write keeps every other activation of the same card
// unchanged.
func manaAbilityWithSubX(ma *cards.SA, x int32) *cards.SA {
	if ma == nil || !strings.EqualFold(effects.ManaOf(ma).AmountTrim, "x") {
		return ma
	}
	return pay.ManaAbilityWithPaidX(ma, x)
}

// manaSubCounterWildcardStage advances one "Any" part by one counter unit at
// a time (wildcardCounterAsk's mana twin), pausing on an ask when more than
// one (object, kind) unit is legal. It advances md.subPart once the part's
// full amount is picked.
func (e *Engine) manaSubCounterWildcardStage(md *manaDiscardActivation, part CostPart, amt int32) {
	for md.SubCounterPicked(md.SubPart) < amt {
		candidates := pay.SubCounterRemovalCandidates(asPayer(e), md.Player, md.Source, part, 1, e.manaSubCounterReservations(md))
		if len(candidates) == 0 {
			e.ManaCost = nil
			e.choosing = chooseNone
			return
		}
		type unit struct {
			obj  state.ObjID
			kind string
		}
		var units []unit
		for _, oid := range candidates {
			o := e.G.Obj(oid)
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
			e.ManaCost = nil
			e.choosing = chooseNone
			return
		}
		if len(units) == 1 || !md.Interactive {
			md.SubCounterPays = append(md.SubCounterPays, subCounterPay{Part: md.SubPart, Obj: units[0].obj, Kind: units[0].kind})
			continue
		}
		d := &decision.Decision{Player: md.Player, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: "Choose a counter to remove", Source: md.Source}
		for _, u := range units {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "subcounter",
				Obj: u.obj, Counter: u.kind,
				Label: fmt.Sprintf("%s (%s counter)", e.G.Obj(u.obj).Face().Name, u.kind)})
		}
		e.choosing = chooseManaSubCounter
		e.ask(d)
		return
	}
	md.SubPart++
}

// answerManaSubCounter routes a chooseManaSubCounter answer to the X
// announcement or the removal-target election, whichever the stage is
// currently posing.
func (e *Engine) answerManaSubCounter(chosen []decision.Option) bool {
	md := e.ManaCost
	if md == nil {
		return false
	}
	if !md.SubXAnnounced {
		return e.answerManaSubCounterX(chosen)
	}
	return e.answerManaSubCounterTarget(chosen)
}

// answerManaSubCounterX records the announced X and resumes the payment.
func (e *Engine) answerManaSubCounterX(chosen []decision.Option) bool {
	md := e.ManaCost
	if md == nil {
		return false
	}
	if len(chosen) > 0 {
		md.SubX = int32(chosen[0].Amount)
	}
	md.SubXAnnounced = true
	// Re-clamp against the live board (counters may have moved): the announced
	// amount can never exceed what the settle can remove.
	if bound, _ := pay.ManaSubCounterXBound(asPayer(e), md.Player, e.G.Obj(md.Source), md.Source, md.Cost); md.SubX > bound {
		md.SubX = bound
	}
	if md.SubX < md.Cost.XMin {
		md.SubX = md.Cost.XMin
	}
	md.Ability = manaAbilityWithSubX(md.Ability, md.SubX)
	cast := md.Cast
	e.continueManaDiscard()
	return cast
}

// answerManaSubCounterTarget records one removal pick and resumes the payment.
// A wildcard "Any" part records one unit and stays on the part until its
// amount is met; every other part advances.
func (e *Engine) answerManaSubCounterTarget(chosen []decision.Option) bool {
	md := e.ManaCost
	if md == nil {
		return false
	}
	if md.SubPart < len(md.Cost.SubCounter) {
		part := md.Cost.SubCounter[md.SubPart]
		for _, opt := range chosen {
			md.SubCounterPays = append(md.SubCounterPays, subCounterPay{Part: md.SubPart, Obj: opt.Obj, Kind: opt.Counter})
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
	cast := md.Cast
	e.continueManaDiscard()
	return cast
}

// ---------------------------------------------------------------------------
// Forage

// ---------------------------------------------------------------------------
// untapYType
