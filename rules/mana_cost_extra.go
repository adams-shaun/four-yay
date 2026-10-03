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
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// manaSubCounterNeedsAsk reports whether a cost carries a SubCounter part the
// mana path must ask about: an announced part (its X and, when anchored, its
// removal target) or a part anchored to a permanent other than the source.
// A fixed source-anchored part keeps its historic no-ask settle in
// payManaSourceParts.
func manaSubCounterNeedsAsk(cost Cost) bool {
	for _, part := range cost.SubCounter {
		if part.Announced || !subCounterTargetsSource(part.Target) {
			return true
		}
	}
	return false
}

// manaSubCounterReservations keeps an earlier counter-cost part from spending
// the same permanent as the current part (pc.subCounterReservations' mana
// twin). Wildcard units of the current part are deliberately absent:
// the "Any" stage accounts for them by (object, kind).
func (e *Engine) manaSubCounterReservations(md *manaDiscardActivation) map[state.ObjID]bool {
	reserved := map[state.ObjID]bool{}
	for _, s := range md.sacs {
		reserved[s] = true
	}
	for _, p := range md.subCounterPays {
		reserved[p.obj] = true
	}
	return reserved
}

// manaSubCounterStage announces the shared X (once) and elects each announced
// or anchored SubCounter part's removal. It returns true when it posed a
// decision (the activation resumes on the answer).
func (e *Engine) manaSubCounterStage(md *manaDiscardActivation) bool {
	if !manaSubCounterNeedsAsk(md.cost) {
		return false
	}
	if !md.subXAnnounced {
		bound, _ := e.manaSubCounterXBound(md.player, e.G.Obj(md.source), md.source, md.cost)
		min := md.cost.XMin
		if min < 0 {
			min = 0
		}
		if bound < min {
			bound = min
		}
		if !md.interactive || bound <= min {
			// No choice: a single legal value needs no zero-information ask
			// (the cast path's own convention), and a non-interactive caller
			// takes the maximum the board can settle so a silent activation
			// still produces all the mana it can. Do not return here: the
			// removal-parts loop below must still record the mandatory payment.
			md.subX = bound
			md.subXAnnounced = true
			md.ability = manaAbilityWithSubX(md.ability, md.subX)
		} else {
			d := &decision.Decision{Player: md.player, Kind: decision.KChoose, Min: 1, Max: 1,
				Prompt: "Choose a value for X", Source: md.source}
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
	for md.subPart < len(md.cost.SubCounter) {
		part := md.cost.SubCounter[md.subPart]
		if !part.Announced && subCounterTargetsSource(part.Target) {
			// A fixed source-anchored part stays with payManaSourceParts.
			md.subPart++
			continue
		}
		amt := part.N
		if part.Announced {
			amt = md.subX
		}
		if amt <= 0 {
			md.subPart++
			continue
		}
		if strings.EqualFold(part.Spec, "Any") {
			e.manaSubCounterWildcardStage(md, part, amt)
			return e.choosing == chooseManaSubCounter
		}
		if subCounterTargetsSource(part.Target) {
			// Announced source-anchored: the settle emits on the source.
			md.subCounterPays = append(md.subCounterPays, subCounterPay{part: md.subPart, obj: md.source})
			md.subPart++
			continue
		}
		candidates := e.subCounterRemovalCandidates(md.player, md.source, part, amt, e.manaSubCounterReservations(md))
		if len(candidates) == 0 {
			// The board changed under the offer: drop the payment rather than
			// ask an election no answer can satisfy.
			e.manaDiscardActivation = nil
			e.choosing = chooseNone
			return false
		}
		if len(candidates) == 1 || !md.interactive {
			md.subCounterPays = append(md.subCounterPays, subCounterPay{part: md.subPart, obj: candidates[0]})
			md.subPart++
			continue
		}
		d := &decision.Decision{Player: md.player, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: "Choose a permanent to remove " + e.subCounterPhrase(part, amt) + " from", Source: md.source}
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
	return manaAbilityWithPaidX(ma, x)
}

// manaSubCounterWildcardStage advances one "Any" part by one counter unit at
// a time (wildcardCounterAsk's mana twin), pausing on an ask when more than
// one (object, kind) unit is legal. It advances md.subPart once the part's
// full amount is picked.
func (e *Engine) manaSubCounterWildcardStage(md *manaDiscardActivation, part CostPart, amt int32) {
	for md.subCounterPicked(md.subPart) < amt {
		candidates := e.subCounterRemovalCandidates(md.player, md.source, part, 1, e.manaSubCounterReservations(md))
		if len(candidates) == 0 {
			e.manaDiscardActivation = nil
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
				if c.N <= 0 || md.subCounterKindUsed(md.subPart, oid, c.Kind) {
					continue
				}
				units = append(units, unit{oid, c.Kind})
			}
		}
		if len(units) == 0 {
			e.manaDiscardActivation = nil
			e.choosing = chooseNone
			return
		}
		if len(units) == 1 || !md.interactive {
			md.subCounterPays = append(md.subCounterPays, subCounterPay{part: md.subPart, obj: units[0].obj, kind: units[0].kind})
			continue
		}
		d := &decision.Decision{Player: md.player, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: "Choose a counter to remove", Source: md.source}
		for _, u := range units {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "subcounter",
				Obj: u.obj, Counter: u.kind,
				Label: fmt.Sprintf("%s (%s counter)", e.G.Obj(u.obj).Face().Name, u.kind)})
		}
		e.choosing = chooseManaSubCounter
		e.ask(d)
		return
	}
	md.subPart++
}

// subCounterPicked counts the units already recorded for one part index.
func (md *manaDiscardActivation) subCounterPicked(partIdx int) int32 {
	n := int32(0)
	for _, p := range md.subCounterPays {
		if p.part == partIdx {
			n++
		}
	}
	return n
}

// subCounterKindUsed reports whether a wildcard part already removed a unit of
// this kind from this object, so the picker spreads units across the remaining
// kinds exactly as wildcardCounterAsk does.
func (md *manaDiscardActivation) subCounterKindUsed(partIdx int, obj state.ObjID, kind string) bool {
	for _, p := range md.subCounterPays {
		if p.part == partIdx && p.obj == obj && p.kind == kind {
			return true
		}
	}
	return false
}

// answerManaSubCounter routes a chooseManaSubCounter answer to the X
// announcement or the removal-target election, whichever the stage is
// currently posing.
func (e *Engine) answerManaSubCounter(chosen []decision.Option) bool {
	md := e.manaDiscardActivation
	if md == nil {
		return false
	}
	if !md.subXAnnounced {
		return e.answerManaSubCounterX(chosen)
	}
	return e.answerManaSubCounterTarget(chosen)
}

// answerManaSubCounterX records the announced X and resumes the payment.
func (e *Engine) answerManaSubCounterX(chosen []decision.Option) bool {
	md := e.manaDiscardActivation
	if md == nil {
		return false
	}
	if len(chosen) > 0 {
		md.subX = int32(chosen[0].Amount)
	}
	md.subXAnnounced = true
	// Re-clamp against the live board (counters may have moved): the announced
	// amount can never exceed what the settle can remove.
	if bound, _ := e.manaSubCounterXBound(md.player, e.G.Obj(md.source), md.source, md.cost); md.subX > bound {
		md.subX = bound
	}
	if md.subX < md.cost.XMin {
		md.subX = md.cost.XMin
	}
	md.ability = manaAbilityWithSubX(md.ability, md.subX)
	cast := md.cast
	e.continueManaDiscard()
	return cast
}

// answerManaSubCounterTarget records one removal pick and resumes the payment.
// A wildcard "Any" part records one unit and stays on the part until its
// amount is met; every other part advances.
func (e *Engine) answerManaSubCounterTarget(chosen []decision.Option) bool {
	md := e.manaDiscardActivation
	if md == nil {
		return false
	}
	if md.subPart < len(md.cost.SubCounter) {
		part := md.cost.SubCounter[md.subPart]
		for _, opt := range chosen {
			md.subCounterPays = append(md.subCounterPays, subCounterPay{part: md.subPart, obj: opt.Obj, kind: opt.Counter})
		}
		if strings.EqualFold(part.Spec, "Any") {
			amt := part.N
			if part.Announced {
				amt = md.subX
			}
			if md.subCounterPicked(md.subPart) >= amt {
				md.subPart++
			}
		} else {
			md.subPart++
		}
	}
	cast := md.cast
	e.continueManaDiscard()
	return cast
}

// settleManaSubCounter emits the announced and anchored SubCounter parts'
// counter removals (settleSubCounterParts' mana twin). A source-anchored part
// removes from the source; a filtered part removes from the object its pick
// carries; an "Any" part removes one unit per pick, grouped by (object, kind)
// so two units of one kind settle as a single CounterChange of -2.
func (e *Engine) settleManaSubCounter(md *manaDiscardActivation) {
	if !manaSubCounterNeedsAsk(md.cost) {
		return
	}
	for partIdx, part := range md.cost.SubCounter {
		if !part.Announced && subCounterTargetsSource(part.Target) {
			continue
		}
		var pays []subCounterPay
		for _, p := range md.subCounterPays {
			if p.part == partIdx {
				pays = append(pays, p)
			}
		}
		if len(pays) == 0 {
			continue
		}
		if strings.EqualFold(part.Spec, "Any") {
			type payKey struct {
				obj  state.ObjID
				kind string
			}
			var order []payKey
			sum := map[payKey]int32{}
			for _, p := range pays {
				if p.kind == "" {
					continue
				}
				k := payKey{p.obj, p.kind}
				if _, seen := sum[k]; !seen {
					order = append(order, k)
				}
				sum[k]++
			}
			for _, k := range order {
				e.emit(events.Event{Kind: events.CounterChange, Obj: k.obj, Counter: k.kind, Amount: -sum[k]})
			}
			if len(order) == 0 {
				e.settleCounterAnyFallback(pays[0].obj, partAnnouncedAmount(part, md.subX))
			}
			continue
		}
		e.emit(events.Event{Kind: events.CounterChange, Obj: pays[0].obj, Counter: part.Spec,
			Amount: -partAnnouncedAmount(part, md.subX)})
	}
}

// settleCounterAnyFallback removes up to amt counters from obj's counter list
// in object order, one CounterChange per kind (the legacy fallback
// settleSubCounterParts uses when no kind was recorded).
func (e *Engine) settleCounterAnyFallback(obj state.ObjID, amt int32) {
	o := e.G.Obj(obj)
	if o == nil || amt <= 0 {
		return
	}
	left := amt
	for _, c := range o.Counters {
		if left <= 0 {
			break
		}
		if c.N <= 0 {
			continue
		}
		n := c.N
		if n > left {
			n = left
		}
		e.emit(events.Event{Kind: events.CounterChange, Obj: obj, Counter: c.Kind, Amount: -n})
		left -= n
	}
}

// partAnnouncedAmount returns a part's payment amount (its literal N or the
// announced X).
func partAnnouncedAmount(part CostPart, x int32) int32 {
	if part.Announced {
		return x
	}
	return part.N
}

// ---------------------------------------------------------------------------
// Forage

// manaForagePayable is the offer gate's read: Forage is payable when the
// payer's graveyard holds three cards OR a Food they control is on the
// battlefield (the cast path's nonManaCastable read; CR 701.16a).
func (e *Engine) manaForagePayable(p state.PlayerID, source state.ObjID) bool {
	if len(e.G.Zone(state.ZGraveyard, p)) >= 3 {
		return true
	}
	return len(e.costCandidates(p, source, state.ZBattlefield, "Food.YouCtrl", false, false)) > 0
}

// manaForageStage poses the Forage election (exile three graveyard cards OR
// sacrifice a Food), sharing the cast path's option kinds so the bot's
// existing arms answer it. A non-interactive caller takes the deterministic
// graveyard arm when it is payable, else the first Food.
func (e *Engine) manaForageStage(md *manaDiscardActivation) bool {
	if !md.cost.Forage || md.forageDone {
		return false
	}
	md.forageDone = true
	graveOK := len(e.G.Zone(state.ZGraveyard, md.player)) >= 3
	foods := e.costCandidates(md.player, md.source, state.ZBattlefield, "Food.YouCtrl", false, false)
	if !md.interactive {
		if graveOK {
			md.foragePay = true
			return false
		}
		if len(foods) > 0 {
			md.foragePay = true
			md.forageFood = foods[0]
		}
		return false
	}
	d := &decision.Decision{Player: md.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Choose how to forage", Source: md.source}
	if graveOK {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "forage_exile",
			Label: "Exile three cards from your graveyard"})
	}
	for _, id := range foods {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "forage_food", Obj: id,
			Label: "Sacrifice " + e.targetName(id)})
	}
	if len(d.Options) == 0 {
		e.manaDiscardActivation = nil
		e.choosing = chooseNone
		return false
	}
	if len(d.Options) == 1 {
		opt := d.Options[0]
		md.foragePay = true
		if opt.Kind == "forage_food" {
			md.forageFood = opt.Obj
		}
		return false
	}
	e.choosing = chooseManaForage
	e.ask(d)
	return true
}

// answerManaForage records the forage arm and resumes the payment.
func (e *Engine) answerManaForage(chosen []decision.Option) bool {
	md := e.manaDiscardActivation
	if md == nil {
		return false
	}
	md.forageDone = true
	if len(chosen) > 0 && chosen[0].Kind == "forage_food" {
		md.foragePay = true
		md.forageFood = chosen[0].Obj
	} else {
		// Exile three: only legal when the graveyard holds three cards (the
		// offer gate read it), so the arm is recorded and the settle takes the
		// top three.
		md.foragePay = true
	}
	cast := md.cast
	e.continueManaDiscard()
	return cast
}

// settleManaForage pays an answered/deterministic Forage: sacrifice the
// elected Food, else exile the top three graveyard cards (the payer's
// graveyard order is deterministic).
func (e *Engine) settleManaForage(md *manaDiscardActivation) {
	if !md.foragePay {
		return
	}
	if md.forageFood != 0 {
		e.emit(events.Sacrifice(md.forageFood))
		return
	}
	n := 0
	for _, id := range e.G.Zone(state.ZGraveyard, md.player) {
		if n >= 3 {
			break
		}
		if o := e.G.Obj(id); o != nil {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZExile,
				Text: "exiled as a forage cost"})
			n++
		}
	}
}

// ---------------------------------------------------------------------------
// untapYType

// manaUntapCandidates returns, in deterministic seat/zone order, the TAPPED
// permanents matching spec that can pay one untapYType<N/Spec> part. Unlike
// costCandidates (which walks only the payer's own zone), this walks every
// seat's battlefield because the head's spec can name an opponent's permanent
// (Benthic Explorers' Land.OppCtrl); the spec matcher owns the control
// relation.
func (e *Engine) manaUntapCandidates(p state.PlayerID, source state.ObjID, spec string, claimed map[state.ObjID]bool) []state.ObjID {
	var out []state.ObjID
	for _, q := range e.G.Players {
		for _, id := range e.G.Zone(state.ZBattlefield, q.ID) {
			if claimed != nil && claimed[id] {
				continue
			}
			o := e.G.Obj(id)
			if o == nil || !o.Tapped {
				continue
			}
			if e.matchesSpecFrom(spec, id, p, source) {
				out = append(out, id)
			}
		}
	}
	return out
}

// manaUntapPayable is the offer gate's read for the untapYType parts: enough
// distinct TAPPED matching permanents exist, reserving the source when the
// same cost also taps it.
func (e *Engine) manaUntapPayable(p state.PlayerID, source state.ObjID, cost Cost) bool {
	if len(cost.UntapPermanent) == 0 {
		return true
	}
	claimed := map[state.ObjID]bool{}
	if cost.Tap {
		claimed[source] = true
	}
	for _, part := range cost.UntapPermanent {
		cands := e.manaUntapCandidates(p, source, part.Spec, claimed)
		if int32(len(cands)) < part.N {
			return false
		}
		for i := int32(0); i < part.N; i++ {
			claimed[cands[i]] = true
		}
	}
	return true
}

// manaUntapStage elects the untapYType permanents, mirroring the literal
// tapXType election (forced when exactly N candidates remain, else an ask).
func (e *Engine) manaUntapStage(md *manaDiscardActivation) bool {
	for md.untapPart < len(md.cost.UntapPermanent) {
		part := md.cost.UntapPermanent[md.untapPart]
		claimed := map[state.ObjID]bool{}
		for _, id := range md.untaps {
			claimed[id] = true
		}
		if md.cost.Tap {
			claimed[md.source] = true
		}
		candidates := e.manaUntapCandidates(md.player, md.source, part.Spec, claimed)
		if int32(len(candidates)) < part.N {
			e.manaDiscardActivation = nil
			e.choosing = chooseNone
			return false
		}
		if int32(len(candidates)) == part.N || !md.interactive {
			md.untaps = append(md.untaps, candidates[:int(part.N)]...)
			md.untapPart++
			continue
		}
		d := &decision.Decision{Player: md.player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
			Prompt: "Choose permanents to untap for the mana ability", Source: md.source}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "untapcost",
				Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseManaUntap
		e.ask(d)
		return true
	}
	return false
}

// answerManaUntap records the untapYType election's picks and resumes.
func (e *Engine) answerManaUntap(chosen []decision.Option) bool {
	md := e.manaDiscardActivation
	if md == nil {
		return false
	}
	for _, opt := range chosen {
		md.untaps = append(md.untaps, opt.Obj)
	}
	md.untapPart++
	cast := md.cast
	e.continueManaDiscard()
	return cast
}

// settleManaUntap emits the elected untapYType permanents' Untap events.
func (e *Engine) settleManaUntap(md *manaDiscardActivation) {
	for _, id := range md.untaps {
		if o := e.G.Obj(id); o != nil && o.Zone == state.ZBattlefield {
			e.emit(events.Event{Kind: events.Untap, Obj: id, Player: md.player, Text: "untapped as a cost"})
		}
	}
}
