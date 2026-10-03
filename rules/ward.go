package rules

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

var wardSpecialCost = regexp.MustCompile(`^(AddCounterYou|Blight|CollectEvidence|Waterbend)<([0-9]+)(?:/[^>]*)?>$`)

// beginWardPayment handles the cost after the targeting object's controller
// selected "Pay". It either pays immediately or poses the one object-choice
// decision the corpus's non-mana Ward costs require.
func (e *Engine) beginWardPayment(rp *resumePoint, ctx *effects.Ctx) (paid, asked bool) {
	payer, ok := e.wardPayer(ctx)
	if !ok {
		return false, false
	}
	raw := strings.TrimSpace(rp.sa.ParamStr(cards.PKUnlessCost))

	// Titania's Forge spelling uses the suffix after ':' as an alternative:
	// discard one card OR pay {2}.
	if i := strings.LastIndex(raw, ">:"); i >= 0 {
		discardRaw, manaRaw := raw[:i+1], raw[i+2:]
		cost := e.parseCost(discardRaw)
		if len(cost.Discard) != 1 {
			return false, false
		}
		part := cost.Discard[0]
		candidates := e.discardCandidates(payer, ctx.Source, part, false, nil)
		d := &decision.Decision{Player: payer, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: "Choose how to pay ward", ResumeKind: "ward_alt", ResumeSA: rp.sa}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "ward_discard", Obj: id,
				Label: "Discard " + e.G.Obj(id).Face().Name})
		}
		mana := e.parseCost(manaRaw)
		if payable(mana, e.G.Players[payer].Pool, e.G.Players[payer].Snow, e.G.Players[payer].ManaUnits(), e.G.Players[payer].Life) ||
			(mana.HasManaPayment() && e.hasUntappedManaSource(payer)) {
			label := capitaliseFirst(costPhrase(mana))
			if label == "" {
				label = "Pay " + manaRaw
			}
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "ward_mana", Amount: int(mana.Generic), Label: label})
		}
		if len(d.Options) == 0 {
			return false, false
		}
		e.Ask(d)
		e.resume.outer = rp.outer
		return false, true
	}

	if m := wardSpecialCost.FindStringSubmatch(raw); m != nil {
		n, _ := strconv.Atoi(m[2])
		switch m[1] {
		case "AddCounterYou":
			// The ward cost's poison counters are put by the PAYER, not by
			// the ward permanent's controller (which is the triggering
			// ward ability's controller, so inFlightCounterAdder's
			// actionCause fallback would attribute them to the wrong
			// player). A cost is paid before any stack object owns it, so
			// publish the payer explicitly, the cost-site precedent.
			prevAdder := e.SetCounterAdder(payer)
			e.emit(events.Event{Kind: events.PlayerCounterChange, Player: payer, Counter: "POISON", Amount: int32(n)})
			e.SetCounterAdder(prevAdder)
			return true, false
		case "Blight":
			// Blight's cost is "a creature you control gets -N/-N"; unlike
			// Waterbend and a tap cost it does not require that creature to be
			// untapped (Auntie Ool, Cursewretch).
			return e.askWardObjects(rp, payer, "ward_blight", "Choose a creature to blight", 1, 1,
				e.wardPermanents(payer, ctx.Source, "Creature", false))
		case "CollectEvidence":
			ids := append([]state.ObjID(nil), e.G.Zone(state.ZGraveyard, payer)...)
			if wardManaValue(e.G, ids) < int32(n) {
				return false, false
			}
			return e.askWardObjects(rp, payer, "ward_evidence", "Exile evidence with total mana value "+strconv.Itoa(n), 1, len(ids), ids)
		case "Waterbend":
			need := n - int(e.G.Players[payer].Pool.Total())
			if need < 0 {
				need = 0
			}
			if need == 0 {
				return e.payMana(payer, Cost{Generic: int32(n)}), false
			}
			ids := e.wardPermanents(payer, ctx.Source, "Artifact,Creature", true)
			if len(ids) < need {
				return false, false
			}
			return e.askWardObjects(rp, payer, "ward_waterbend", "Tap permanents to waterbend", need, need, ids)
		}
	}

	cost := e.parseCost(raw)
	if strings.HasPrefix(raw, "PayLife<X/") {
		life := e.Power(ctx.Source)
		if life < 0 {
			life = 0
		}
		cost = Cost{Life: life}
	}
	if len(cost.Sac) > 1 || len(cost.Discard) > 1 || (len(cost.Sac) > 0 && len(cost.Discard) > 0) || cost.X != 0 {
		return false, false
	}
	if len(cost.Sac) == 1 {
		part := cost.Sac[0]
		ids := e.wardPermanents(payer, ctx.Source, sacrificeMatchSpec(part.Spec), false)
		// A CantSacrifice restriction (Call for Aid) or face static: the
		// permanent cannot pay the ward's sacrifice component. The ward cost
		// is demanded by the ward trigger (CR 702.22), so the cause is
		// costCauseTriggered -- a `ValidCause$ Spell,Activated | ForCost$ True`
		// carrier scopes PAST it, a `ValidCause$ Triggered` one admits it.
		var sacIDs []state.ObjID
		for _, id := range ids {
			if !e.sacrificeBlockedForCost(id, costCauseTriggered) {
				sacIDs = append(sacIDs, id)
			}
		}
		ids = sacIDs
		if int32(len(ids)) < part.N {
			return false, false
		}
		return e.askWardObjects(rp, payer, "ward_sac", "Choose permanents to sacrifice for ward", int(part.N), int(part.N), ids)
	}
	if len(cost.Discard) == 1 {
		part := cost.Discard[0]
		ids := e.discardCandidates(payer, ctx.Source, part, false, nil)
		if int32(len(ids)) < part.N {
			return false, false
		}
		if strings.EqualFold(part.Spec, "Random") {
			chosen := make([]state.ObjID, 0, part.N)
			for i := int32(0); i < part.N; i++ {
				pick := e.Rand(len(ids))
				chosen = append(chosen, ids[pick])
				ids = append(ids[:pick], ids[pick+1:]...)
			}
			if !e.payMana(payer, wardManaCost(cost)) {
				return false, false
			}
			for _, id := range chosen {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZGraveyard, Text: "discarded for ward"})
			}
			return true, false
		}
		return e.askWardObjects(rp, payer, "ward_discard", "Choose cards to discard for ward", int(part.N), int(part.N), ids)
	}
	if cost.Tap {
		ids := e.wardPermanents(payer, ctx.Source, "Artifact,Creature", true)
		if len(ids) == 0 {
			return false, false
		}
		return e.askWardObjects(rp, payer, "ward_tap", "Choose a permanent to tap for ward", 1, 1, ids)
	}
	if e.payMana(payer, cost) {
		return true, false
	}
	// CR 702.21a payment is a mana-payment window, not a check of only
	// floating mana. Give the payer the same chance to activate each currently
	// legal mana ability as during a cast before finally charging the Ward cost.
	if !cost.HasManaPayment() || !e.hasUntappedManaSource(payer) {
		return false, false
	}
	e.askWardMana(rp, &wardManaPayment{payer: payer, cost: cost, resumeKind: "ward_mana",
		prompt: "Activate mana abilities to pay Ward"})
	return false, true
}

func (e *Engine) askWardObjects(rp *resumePoint, payer state.PlayerID, kind, prompt string, min, max int, ids []state.ObjID) (bool, bool) {
	d := &decision.Decision{Player: payer, Kind: decision.KChoose, Min: min, Max: max,
		Prompt: prompt, ResumeKind: kind, ResumeSA: rp.sa}
	for _, id := range ids {
		o := e.G.Obj(id)
		label := fmt.Sprintf("object #%d", id)
		if o != nil && o.Face() != nil {
			label = o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: kind, Obj: id, Label: label})
	}
	e.Ask(d)
	e.resume.outer = rp.outer
	return false, true
}

func (e *Engine) wardPayer(ctx *effects.Ctx) (state.PlayerID, bool) {
	cause := e.G.Obj(ctx.TriggerStack)
	if cause == nil || cause.Zone != state.ZStack || int(cause.Controller) >= len(e.G.Players) {
		return 0, false
	}
	return cause.Controller, true
}

func (e *Engine) wardPermanents(p state.PlayerID, source state.ObjID, spec string, untapped bool) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o == nil || (untapped && o.Tapped) || !e.matchesSpecFrom(spec, id, p, source) {
			continue
		}
		out = append(out, id)
	}
	return out
}

func wardManaValue(g *state.Game, ids []state.ObjID) int32 {
	var n int32
	for _, id := range ids {
		if o := g.Obj(id); o != nil && o.Face() != nil {
			n += o.Face().Cmc()
		}
	}
	return n
}

// settleWardPayment validates and applies the object choice from a non-mana
// Ward payment. A malformed/stale answer pays nothing and therefore declines.
func (e *Engine) settleWardPayment(kind string, sa *cards.SA, ctx *effects.Ctx, chosen []decision.Option) bool {
	payer, ok := e.wardPayer(ctx)
	if !ok {
		return false
	}
	raw := strings.TrimSpace(sa.ParamStr(cards.PKUnlessCost))
	ids := make([]state.ObjID, 0, len(chosen))
	seen := map[state.ObjID]bool{}
	for _, opt := range chosen {
		if opt.Obj != 0 && !seen[opt.Obj] {
			seen[opt.Obj] = true
			ids = append(ids, opt.Obj)
		}
	}

	switch kind {
	case "ward_alt":
		if len(chosen) != 1 {
			return false
		}
		if chosen[0].Kind == "ward_mana" {
			_, manaRaw, _ := strings.Cut(raw, ">:")
			return e.payMana(payer, e.parseCost(manaRaw))
		}
		if chosen[0].Kind != "ward_discard" || len(ids) != 1 || !slices.Contains(e.G.Zone(state.ZHand, payer), ids[0]) {
			return false
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: ids[0], From: state.ZHand, To: state.ZGraveyard, Text: "discarded for ward"})
		return true
	case "ward_blight":
		if len(ids) != 1 || !slices.Contains(e.wardPermanents(payer, ctx.Source, "Creature", false), ids[0]) {
			return false
		}
		m := wardSpecialCost.FindStringSubmatch(raw)
		n, _ := strconv.Atoi(m[2])
		// The blight -1/-1 counters are put by the PAYER (a ward cost),
		// not by the ward permanent's controller, so publish the payer
		// explicitly rather than letting the actionCause fallback
		// attribute them to the wrong player.
		prevAdder := e.SetCounterAdder(payer)
		e.emit(events.Event{Kind: events.CounterChange, Obj: ids[0], Counter: "M1M1", Amount: int32(n)})
		e.SetCounterAdder(prevAdder)
		return true
	case "ward_evidence":
		for _, id := range ids {
			if !slices.Contains(e.G.Zone(state.ZGraveyard, payer), id) {
				return false
			}
		}
		m := wardSpecialCost.FindStringSubmatch(raw)
		n, _ := strconv.Atoi(m[2])
		if wardManaValue(e.G, ids) < int32(n) {
			return false
		}
		for _, id := range ids {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZExile, Text: "collected as evidence for ward"})
		}
		return true
	case "ward_waterbend":
		for _, id := range ids {
			if !slices.Contains(e.wardPermanents(payer, ctx.Source, "Artifact,Creature", true), id) {
				return false
			}
		}
		m := wardSpecialCost.FindStringSubmatch(raw)
		n, _ := strconv.Atoi(m[2])
		remaining := n - len(ids)
		if remaining < 0 || !e.payMana(payer, Cost{Generic: int32(remaining)}) {
			return false
		}
		for _, id := range ids {
			e.emit(events.Event{Kind: events.Tap, Obj: id})
		}
		return true
	case "ward_tap":
		if len(ids) != 1 || !slices.Contains(e.wardPermanents(payer, ctx.Source, "Artifact,Creature", true), ids[0]) {
			return false
		}
		e.emit(events.Event{Kind: events.Tap, Obj: ids[0]})
		return true
	case "ward_sac", "ward_discard":
		cost := e.parseCost(raw)
		var part CostPart
		var zone state.Zone
		if kind == "ward_sac" && len(cost.Sac) == 1 {
			part, zone = cost.Sac[0], state.ZBattlefield
		} else if kind == "ward_discard" && len(cost.Discard) == 1 {
			part, zone = cost.Discard[0], state.ZHand
		} else {
			return false
		}
		if len(ids) != int(part.N) {
			return false
		}
		for _, id := range ids {
			valid := slices.Contains(e.G.Zone(zone, payer), id)
			if kind == "ward_sac" {
				valid = valid && e.matchesSpecFrom(sacrificeMatchSpec(part.Spec), id, payer, ctx.Source)
			} else {
				valid = valid && slices.Contains(e.discardCandidates(payer, ctx.Source, part, false, nil), id)
			}
			if !valid {
				return false
			}
		}
		if !e.payMana(payer, wardManaCost(cost)) {
			return false
		}
		toText := "discarded for ward"
		if kind == "ward_sac" {
			toText = "sacrificed for ward"
		}
		for _, id := range ids {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: zone, To: state.ZGraveyard, Text: toText})
		}
		return true
	}
	return false
}

// wardManaCost removes payment components already settled by a Ward choice.
// payMana deliberately rejects Sac/Discard/Tap, but a Ward cost can combine
// one with mana and must charge that mana after its object component.
func wardManaCost(c Cost) Cost {
	c.Tap = false
	c.Sac = nil
	c.Discard = nil
	return c
}

// wardManaPayment is the resumable CR 702.21a mana-payment window. It keeps
// the resolution frame attributes that Ask normally captures from an effects
// call, because this window is opened by rules after an answered Ward mode.
type wardManaPayment struct {
	payer       state.PlayerID
	cost        Cost
	obj         state.ObjID
	sa          *cards.SA
	outer       *resumePoint
	replacement bool
	replaced    state.ObjID
	before      *triggerSnapshot

	// resumeKind is the decision's own resume kind ("ward_mana" for a Ward
	// payment, "unless_mana" for a mid-resolution UnlessCost$). prompt is the
	// window's prompt. Both let the ONE mana-window owner below serve both
	// callers without duplicating the resume-state writes (ruling T21-e).
	resumeKind string
	prompt     string
	// unless marks the mid-resolution UnlessCost$ window: Done charges the
	// cost through payUnlessCost (with the resolving object's ManaConvert
	// conversion) and sets ctx.UnlessNext, rather than Ward's plain payMana.
	unless bool
	// target is the unless arm's payer cursor (rp.target), threaded so the
	// resumed arm attributes the answer to the same payer.
	target int
}

// askWardMana offers every usable mana source plus Done. Unlike an ordinary
// cast window, Done remains useful even after the last source is tapped: it
// lets the player pay the now-floating mana or decline. It is the ONE owner
// of the mid-resolution mana-window resume-state writes (ruling T21-e); the
// unless-cost window (rules/unless_mana.go) delegates here rather than
// writing e.resume itself.
func (e *Engine) askWardMana(rp *resumePoint, wm *wardManaPayment) {
	wm.obj, wm.sa = rp.obj, rp.sa
	wm.outer, wm.replacement, wm.replaced, wm.before = rp.outer, rp.replacement, rp.replaced, rp.before
	e.wardMana = wm
	d := &decision.Decision{Player: wm.payer, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: wm.prompt, ResumeKind: wm.resumeKind, ResumeSA: rp.sa, ResumeTarget: wm.target}
	// Sources are offered only while the pool cannot yet pay the charge --
	// the same predicate that opened the window (unlessManaWindowNeeded) and
	// the one the cast and cumulative windows close on. Once the pool covers
	// it only Done remains: tapping further can add nothing to this payment,
	// and a window that kept offering its sources let an answerer that taps
	// "while a source is offered" tap every land and then activate a
	// non-tapping pool converter (Farrelite Priest's "{1}: Add {W}") forever
	// (cardfuzz batch3 lines 9/11, Peacekeeper's upkeep unless-cost).
	// The pool-only predicate (lifeGrant false) matches the gate that opened
	// the window (unlessManaWindowNeeded / advanceUnlessPayment): a {B} pip
	// K'rrik's grant could settle with life must NOT mark the cost payable
	// here, or the window would offer only Done and hide the untapped source.
	payable := wm.cost.Priceable() && e.costPayableClassLife(wm.payer,
		paymentDescriptor{id: rp.obj, class: paymentOther, cost: &wm.cost}, pipRider{}, wm.cost, false)
	if !payable {
		for _, id := range e.G.Zone(state.ZBattlefield, wm.payer) {
			if e.untappedManaSource(wm.payer, id) {
				opt := decision.Option{Index: len(d.Options), Kind: "activate", Obj: id,
					Label: "Tap " + e.G.Obj(id).Face().Name + " for mana"}
				// The beyond-tap cost marker every other "activate" offer
				// carries (legal.go's priority window, cast.go's payment
				// window), so an answerer can tell a non-tapping pool
				// converter from a tap source here too.
				opt.Cost = e.manaActivationCostMarker(e.availableManaAbilities(wm.payer, id))
				d.Options = append(d.Options, opt)
			}
		}
	}
	d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "done", Label: "Done"})
	e.Ask(d)
	// Ask sees the correct stack object but this payment window began while a
	// prior frame was resuming, so preserve that frame's continuation and
	// replacement snapshot explicitly.
	e.resume.outer = wm.outer
	e.resume.replacement = wm.replacement
	e.resume.replaced = wm.replaced
	e.resume.before = wm.before
	e.resume.target = wm.target
}

// continueWardMana reopens the payment window after one mana ability has
// resolved, including abilities that required their own ability/colour/discard
// decision. It serves both the Ward and the unless-cost windows, because both
// store their state on e.wardMana.
func (e *Engine) continueWardMana() {
	wm := e.wardMana
	if wm == nil {
		return
	}
	cp := *wm
	e.askWardMana(&resumePoint{obj: wm.obj, sa: wm.sa, outer: wm.outer,
		replacement: wm.replacement, replaced: wm.replaced, before: wm.before}, &cp)
}

// answerWardMana applies Done or activates the chosen source. It returns true
// when a new decision was installed and the current resolution must remain
// suspended; otherwise it sets ctx.UnlessPay for the asking SA's normal
// re-entry (effWard for Ward, the unless gate for an UnlessCost$).
func (e *Engine) answerWardMana(rp *resumePoint, chosen []decision.Option, ctx *effects.Ctx) bool {
	wm := e.wardMana
	if wm == nil || wm.obj != rp.obj || wm.sa != rp.sa || len(chosen) != 1 {
		ctx.UnlessPay = "decline"
		return false
	}
	if chosen[0].Kind == "done" {
		e.wardMana = nil
		var paid bool
		if wm.unless {
			paid = e.payUnlessCost(wm.payer, wm.cost, ctx, wm.obj)
			ctx.UnlessNext = rp.target
		} else {
			paid = e.payMana(wm.payer, wm.cost)
		}
		if paid {
			ctx.UnlessPay = "pay"
		} else {
			ctx.UnlessPay = "decline"
		}
		return false
	}
	if chosen[0].Kind != "activate" || !e.untappedManaSource(wm.payer, chosen[0].Obj) {
		ctx.UnlessPay = "decline"
		e.wardMana = nil
		return false
	}
	e.activateManaPayment(wm.payer, chosen[0].Obj, false)
	if e.Pending() == nil {
		e.continueWardMana()
	}
	return true
}
