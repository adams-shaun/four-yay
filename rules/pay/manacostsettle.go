package pay

import (
	"strings"

	"github.com/adams-shaun/gorge/events"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// ManaSubCounterNeedsAsk reports whether a cost carries a SubCounter part the
// mana path must ask about: an announced part (its X and, when anchored, its
// removal target) or a part anchored to a permanent other than the source.
// A fixed source-anchored part keeps its historic no-ask settle in
// payManaSourceParts.
func ManaSubCounterNeedsAsk(cost costvocab.Cost) bool {
	for _, part := range cost.SubCounter {
		if part.Announced || !costvocab.SubCounterTargetsSource(part.Target) {
			return true
		}
	}
	return false
}

// SettleManaSubCounter emits the announced and anchored SubCounter parts'
// counter removals (settleSubCounterParts' mana twin). A source-anchored part
// removes from the source; a filtered part removes from the object its pick
// carries; an "Any" part removes one unit per pick, grouped by (object, kind)
// so two units of one kind settle as a single CounterChange of -2.
func SettleManaSubCounter(e Engine, md *ManaCostActivation) {
	if !ManaSubCounterNeedsAsk(md.Cost) {
		return
	}
	for partIdx, part := range md.Cost.SubCounter {
		if !part.Announced && costvocab.SubCounterTargetsSource(part.Target) {
			continue
		}
		var pays []SubCounterPay
		for _, p := range md.SubCounterPays {
			if p.Part == partIdx {
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
				if p.Kind == "" {
					continue
				}
				k := payKey{p.Obj, p.Kind}
				if _, seen := sum[k]; !seen {
					order = append(order, k)
				}
				sum[k]++
			}
			for _, k := range order {
				e.Emit(events.Event{Kind: events.CounterChange, Obj: k.obj, Counter: k.kind, Amount: -sum[k]})
			}
			if len(order) == 0 {
				SettleCounterAnyFallback(e, pays[0].Obj, PartAnnouncedAmount(part, md.SubX))
			}
			continue
		}
		e.Emit(events.Event{Kind: events.CounterChange, Obj: pays[0].Obj, Counter: part.Spec,
			Amount: -PartAnnouncedAmount(part, md.SubX)})
	}
}

// SettleCounterAnyFallback removes up to amt counters from obj's counter list
// in object order, one CounterChange per kind (the legacy fallback
// settleSubCounterParts uses when no kind was recorded).
func SettleCounterAnyFallback(e Engine, obj state.ObjID, amt int32) {
	o := e.Game().Obj(obj)
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
		e.Emit(events.Event{Kind: events.CounterChange, Obj: obj, Counter: c.Kind, Amount: -n})
		left -= n
	}
}

// PartAnnouncedAmount returns a part's payment amount (its literal N or the
// announced X).
func PartAnnouncedAmount(part costvocab.CostPart, x int32) int32 {
	if part.Announced {
		return x
	}
	return part.N
}

// SettleManaForage pays an answered/deterministic Forage: sacrifice the
// elected Food, else exile the top three graveyard cards (the payer's
// graveyard order is deterministic).
func SettleManaForage(e Engine, md *ManaCostActivation) {
	if !md.ForagePay {
		return
	}
	if md.ForageFood != 0 {
		e.Emit(events.Sacrifice(md.ForageFood))
		return
	}
	n := 0
	for _, id := range e.Game().Zone(state.ZGraveyard, md.Player) {
		if n >= 3 {
			break
		}
		if o := e.Game().Obj(id); o != nil {
			e.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZExile,
				Text: "exiled as a forage cost"})
			n++
		}
	}
}

// SettleManaUntap emits the elected untapYType permanents' Untap events.
func SettleManaUntap(e Engine, md *ManaCostActivation) {
	for _, id := range md.Untaps {
		if o := e.Game().Obj(id); o != nil && o.Zone == state.ZBattlefield {
			e.Emit(events.Event{Kind: events.Untap, Obj: id, Player: md.Player, Text: "untapped as a cost"})
		}
	}
}
