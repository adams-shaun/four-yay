package rules

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// validateCastContributions is the Submit-time gate for the cast flow's
// Convoke/Harmonize announcement decision (convokeAsk). The decision's
// static Validate sees only the offered option list -- two white creatures
// each carry a convoke_W option for a {W} spell, in distinct groups -- so
// an over-selection passes it; this gate rejects any answer containing a
// contribution that reduces nothing (convokeAbsorbs), before the intent is
// recorded and the pending decision is consumed, exactly like
// validateAttackers. The client then resubmits a legal subset. Any other
// KChoose decision, and an out-of-range choice (Validate's own error), is
// passed through untouched.
func (e *Engine) validateCastContributions(d *decision.Decision, in decision.Intent) error {
	pc := e.cast
	if pc == nil || len(in.Choices) == 0 {
		return nil
	}
	var pays []convokePayment
	for _, c := range in.Choices {
		if c < 0 || c >= len(d.Options) {
			return nil
		}
		o := d.Options[c]
		switch {
		case o.Kind == "harmonize":
			pays = append(pays, convokePayment{id: o.Obj, power: int32(o.Amount)})
		case o.Kind == "improvise_generic":
			pays = append(pays, convokePayment{id: o.Obj})
		case o.Kind == "waterbend_generic":
			pays = append(pays, convokePayment{id: o.Obj, waterbend: true})
		case strings.HasPrefix(o.Kind, "convoke_"):
			color := byte(0)
			if o.Kind != "convoke_generic" {
				color = o.Kind[len("convoke_")]
			}
			pays = append(pays, convokePayment{id: o.Obj, color: color, countsMana: true})
		default:
			return nil // a different cast-flow ask, not the convoke announcement
		}
	}
	all := append(append([]convokePayment(nil), pc.convoke...), pays...)
	if !e.convokeAbsorbs(pc, e.manaToPay(pc), all, pc.cost.X > 0) {
		return fmt.Errorf("announcement reduces nothing: the outstanding cost cannot absorb every chosen contribution")
	}
	// Waterbend taps pay only the waterbend amount (CR 701.67a). A fixed
	// Waterbend<N> part has a cap known here; a Waterbend<X> amount is the
	// announced X, which does not exist yet at this pre-X announcement gate,
	// so xAsk -- the one site that knows X -- enforces that cap through
	// waterbendCap, and its last-resort fallback never resurrects an X the
	// cap rejected.
	if !pc.mods.waterbendX && pc.mods.raiseX == 0 && waterbendTaps(all) > waterbendCap(pc.mods, 0) {
		return fmt.Errorf("more permanents tapped than the waterbend cost allows")
	}
	return nil
}

// convokeAsk announces every creature used for Convoke or Harmonize. It is
// deliberately before manaWindowAsk: tapping is part of paying, so a chosen
// creature cannot first be used as a mana source.
func (e *Engine) convokeAsk() bool {
	pc := e.cast
	if pc == nil || pc.convokeDone {
		return false
	}
	pc.convokeDone = true
	// A cast announces Convoke/Harmonize/Improvise; an activated ability
	// announces only its own waterbend contributions (Giant Koi's
	// `Cost$ Waterbend<3>`), so the cast-only keyword readers are skipped
	// for an ability.
	isAbility := pc.isAbility()
	isConvoke := !isAbility && e.hasCastConvoke(pc.card)
	isHarmonize := !isAbility && pc.mode == "harmonize"
	isImprovise := !isAbility && e.hasCastImprovise(pc.card)
	// A Waterbend<N>/<X> cost (a RaiseCost additional cost, or the ability's
	// or optional part's own Waterbend token folded into mods by
	// foldRaiseExtra): each untapped artifact or creature tapped while paying
	// it pays for {1} of the waterbend amount (CR 701.67a).
	isWaterbend := pc.mods.waterbend > 0 || pc.mods.waterbendX
	if !isConvoke && !isHarmonize && !isImprovise && !isWaterbend {
		return false
	}
	mana := e.manaToPay(pc)
	// Before X is announced, its generic requirement is not folded into
	// mana. It nevertheless makes every creature a possible generic payment;
	// the subsequent xAsk prices the selected contributions against the real
	// X total.
	hasX := pc.cost.X > 0
	if !mana.hasManaPayment() && !hasX {
		return false
	}
	name := e.G.Obj(pc.card).Face().Name
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 0, Source: pc.card}
	sawCreature, sawArtifact := false, false
	for _, id := range e.G.Zone(state.ZBattlefield, pc.player) {
		o := e.G.Obj(id)
		if o == nil || o.Tapped || o.Face() == nil || o.BestowedAttached() || o.ReconfiguredAttached() || e.convokeCommitted(pc, id) {
			continue
		}
		group := fmt.Sprintf("payment:%d", id)
		if isHarmonize && o.EffectiveIsCreature() && (mana.Generic > 0 || hasX) {
			// The reduction offered is the creature's ACTUAL power (CR
			// 702.46a), the same number harmonizePayment credits: a printed
			// 1/1 currently boosted to 4 funds four generic, and a printed
			// 4/4 reduced to 1 funds only one.
			if p := e.Derived(id).Power; p > 0 {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "harmonize", Obj: id,
					Group: group, Amount: int(p), Label: "Tap " + o.Face().Name + " (reduce by " + strconv.Itoa(int(p)) + ")"})
				sawCreature = true
			}
		}
		if isConvoke && o.EffectiveIsCreature() {
			for _, color := range []byte{'W', 'U', 'B', 'R', 'G'} {
				if mana.Colored[state.ManaIndex(color)] > 0 && strings.Contains(e.objColors(o), string(color)) {
					d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "convoke_" + string(color), Obj: id,
						Group: group, Label: "Tap " + o.Face().Name + " for " + string(color)})
					sawCreature = true
				}
			}
			if mana.Generic > 0 || hasX {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "convoke_generic", Obj: id,
					Group: group, Label: "Tap " + o.Face().Name + " for 1"})
				sawCreature = true
			}
		}
		// CR 702.66a: Improvise's artifacts -- artifact creatures included,
		// the card is an artifact independently of being a creature -- each
		// pay one generic. The shared payment group makes the object's
		// Convoke and Improvise options mutually exclusive, so one artifact
		// can never be committed to both payments.
		genericOffered := false
		if isImprovise && o.EffectiveIsArtifact() && (mana.Generic > 0 || hasX) {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "improvise_generic", Obj: id,
				Group: group, Label: "Tap " + o.Face().Name + " for 1"})
			sawArtifact = true
			genericOffered = true
		}
		if isConvoke && o.EffectiveIsCreature() && (mana.Generic > 0 || hasX) {
			genericOffered = true
		}
		// Waterbend: an artifact or creature not already offered a generic
		// payment by the spell's own Convoke/Improvise may tap for {1} of
		// the waterbend amount (the same payment group keeps one object to
		// one contribution).
		if isWaterbend && !genericOffered && (o.EffectiveIsArtifact() || o.EffectiveIsCreature()) && (mana.Generic > 0 || hasX) {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "waterbend_generic", Obj: id,
				Group: group, Label: "Tap " + o.Face().Name + " to waterbend for 1"})
			if o.EffectiveIsCreature() {
				sawCreature = true
			} else {
				sawArtifact = true
			}
		}
	}
	if len(d.Options) == 0 {
		return false
	}
	// The prompt names what is actually offered: a mixed Convoke/Improvise
	// spell offers both creatures and artifacts, an Improvise-only one only
	// artifacts, and the Convoke/Harmonize shapes only creatures.
	switch {
	case sawCreature && sawArtifact:
		d.Prompt = "Choose permanents to help pay for " + name
	case sawArtifact:
		d.Prompt = "Choose artifacts to help pay for " + name
	default:
		d.Prompt = "Choose creatures to help pay for " + name
	}
	// The announcement cannot tap more creatures than the cost can absorb:
	// each chosen contribution reduces exactly one outstanding slot (a
	// colour pip or one generic), so Max is the outstanding slot count.
	// With an unfixed {X} the generic requirement is not yet known (CR
	// 601.2b announces Convoke before X), so the bound is left open and
	// xAsk prices the announcement against every candidate X instead;
	// convokeAbsorbs's answer gate plus that pricing close the rest.
	d.Max = len(d.Options)
	if !hasX {
		if slots := int(mana.Colored.Total() + mana.Generic); slots < d.Max {
			d.Max = slots
		}
	}
	if isWaterbend && !isConvoke && !isHarmonize && !isImprovise && pc.mods.raiseX == 0 && !pc.mods.waterbendX && int(pc.mods.waterbend) < d.Max {
		// Only waterbend taps are offered: at most the waterbend amount. A
		// Waterbend<X> amount is not known until X is announced, so its cap
		// is left open (waterbendX).
		d.Max = int(pc.mods.waterbend)
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

func (e *Engine) manaToPayX(pc *pendingCast, x int32) Cost {
	return e.manaToPayXUsing(pc, x, e.manaToPayXMods(pc, x))
}

// manaToPayXMods is the modifier composition manaToPayX applies to a
// candidate X: pc.mods, except that a cost whose announced count feeds a
// ReduceCost static reading the paid X (Dargo's {2}-less-per-sacrifice) is
// re-priced with the X-bound targets, because the offer-time pc.mods snapshot
// was bound to X=0. xAsk reads it so the convoke-absorption check and the
// potential-target retry compose the SAME modifiers the payable check used.
func (e *Engine) manaToPayXMods(pc *pendingCast, x int32) costMods {
	if costAnnouncesPaidX(pc.cost) {
		if scope, ok := e.pendingCastScope(pc); ok {
			return e.costModifiersForTargetsX(pc.player, pc.card, scope, pc.targets, x)
		}
	}
	return pc.mods
}

// manaToPayXUsing is manaToPayX with an explicit modifier composition.
// xAsk's target-potential retry prices a candidate X under the reduction a
// potential target would give (mods is then the potential composition); every
// other caller passes manaToPayXMods. The commander tax is folded in the same
// place as manaToPayX, so an alternative composition charges the identical
// total.
func (e *Engine) manaToPayXUsing(pc *pendingCast, x int32, mods costMods) Cost {
	m := mods.apply(pc.resolvedManaX(x))
	m.Generic += pc.taxGeneric
	return m
}

// hasManaPayment reports whether a cost's mana component is non-empty, per CR
// 601.2g's "if the total cost includes a mana payment".
func (c Cost) hasManaPayment() bool {
	return c.Colored.Total() > 0 || c.Generic > 0
}

// dropAnnouncePrefix removes the first n announcement pips (in announcePip
// order: two-colour hybrids, then monocolour hybrids, then Phyrexian, then
// hybrid-Phyrexian) from the cost, leaving the rest as the cost's live
// choices. It is how feasibleAny's per-level walk consumes one announcement
// pip at a time after folding that pip's resolved face into the cost, so a
// leaf never sees a pip slot twice.
func (c Cost) dropAnnouncePrefix(n int) Cost {
	drop := n
	if drop < len(c.Hybrid) {
		c.Hybrid = c.Hybrid[drop:]
		drop = 0
	} else {
		drop -= len(c.Hybrid)
		c.Hybrid = nil
	}
	if drop > 0 {
		if drop < len(c.Twobrid) {
			c.Twobrid = c.Twobrid[drop:]
			drop = 0
		} else {
			drop -= len(c.Twobrid)
			c.Twobrid = nil
		}
	}
	if drop > 0 {
		if drop < len(c.Phyrexian) {
			c.Phyrexian = c.Phyrexian[drop:]
			drop = 0
		} else {
			drop -= len(c.Phyrexian)
			c.Phyrexian = nil
		}
	}
	if drop > 0 {
		if drop < len(c.HybridPhyrexian) {
			c.HybridPhyrexian = c.HybridPhyrexian[drop:]
		} else {
			c.HybridPhyrexian = nil
		}
	}
	return c
}

// announceCost is gone: its per-pip composition (mods applied to a cost that
// still carried the unannounced pips, so a Color$ reduction saw no W pip it
// could legally take and a floor priced an unresolved pip at its generic
// face) answered a different feasibility question than the offer gate and
// the charge, and could reject the only legal announcement (a reduction that
// is legally assigned to a LATER pip). announceFeasible below folds the
// already-announced pips into the cost as their final resolved faces and
// hands the remainder to the one shared primitive, costMods.feasibleAny.
// announceFeasible reports whether offering alternative alt for the pip the
// flow is announcing still leaves the whole cost payable: the pips already
// committed (payColor/payLife/payGeneric on pc) and the candidate alt are
// folded into the cost as their FINAL resolved faces, and the still-
// unannounced pips are enumerated by the shared primitive with the CR 601.2f
// modifiers composed onto each fully-resolved assignment, the CR 903.8
// commander tax added after and (for a spell) the Delve credit taken off the
// generic — exactly the composition manaToPay/payCast will charge once every
// pip is settled. It is the CR 601.2b legality question: an announced payment
// is offered only if SOME legal assignment of the remaining pips makes the
// total cost payable, so a player is never offered a payment that can only
// strand the cast in an unpayable remainder (and an abort at payCast).
func (e *Engine) announceFeasible(pc *pendingCast, alt pipAlt, pool, snow state.Mana, life int32) bool {
	c := pc.cost.WithX(pc.x)
	for i := range c.Colored {
		c.Colored[i] += pc.payColor[i]
	}
	c.Generic = addClampedGeneric(c.Generic, int64(pc.payGeneric))
	c.Life = addClampedGeneric(c.Life, int64(pc.payLife))
	switch {
	case alt.color != 0:
		c.Colored[state.ManaIndex(alt.color)]++
	case alt.generic > 0:
		c.Generic = addClampedGeneric(c.Generic, int64(alt.generic))
	case alt.life > 0:
		c.Life = addClampedGeneric(c.Life, int64(alt.life))
	}
	delve := int32(0)
	if !pc.isAbility() {
		delve = int32(len(pc.delve))
	}
	// The pips 0..payIdx have been announced (their faces are folded in
	// above), so their slots leave the cost; the pips after payIdx stay live
	// for the shared primitive to enumerate.
	c = c.dropAnnouncePrefix(pc.payIdx + 1)
	payment := paymentForCast(pc, c)
	rider := pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType}
	if e.manaFeasibleDescriptor(pc.player, payment, c, pc.mods, pc.taxGeneric, delve, rider) {
		return true
	}
	// CR 601.2b chooses a Phyrexian or hybrid face BEFORE the 601.2g mana
	// ability window.  Pricing a face only against floating mana therefore
	// withholds a coloured face whenever its source is still untapped: a
	// Gitaxian Probe with an Island offered only "Pay 2 life".  Probe the same
	// concrete source alternatives that manaWindowAsk can subsequently offer,
	// after composing exactly the modifiers, commander tax, and Delve credit
	// that payCast will charge.  This is an offer-side read only; the chosen
	// face still reaches the ordinary mana window and is paid there.
	charged := pc.mods.apply(c)
	charged.Generic = addClampedGeneric(charged.Generic, int64(pc.taxGeneric))
	charged.Generic -= delve
	if charged.Generic < 0 {
		charged.Generic = 0
	}
	if !charged.hasManaPayment() {
		return false
	}
	av := e.manaAvailableFor(pc.player, payment)
	return e.manaReachable(pc.player, charged, av.pool, e.G.Players[pc.player].Snow,
		av.typed, e.G.Players[pc.player].Life, rider,
		e.paymentConv(pc.player, payment.id, payment.class == paymentActivated), e.castWindowUnits(pc))
}

// manaAsk offers the player's payment choice for the next unsettled hybrid or
// Phyrexian pip of the cost (CR 601.2b), one decision per pip. EVERY face is
// gated on the ONE shared feasibility primitive (announceFeasible →
// costMods.feasibleAny): with the pips already announced and the candidate
// folded in as its final resolved face, some legal assignment of the
// remaining pips must make the composed total (modifiers on final faces,
// commander tax, Delve credit) payable. Any composed modifier can make a
// locally affordable face strand the final payment — a generic Thalia raise
// (Dismember's black faces), a Color$ reduction that is only legally
// assignable to a later pip ({W/U}{W/U} under Color$ W), a SetCost floor on
// an unresolved twobrid — and only the whole-cost search sees that, so a
// player is never offered a payment a complete assignment cannot pay. The
// valid options keep their announcePip order, so the deterministic bot
// fallback (index 0) always picks a legal payment and a no-answer host never
// wedges. The offer gate (offerCastable) proved at least one full assignment
// feasible over the same primitive, so the decision is never empty for a
// state the gate measured; an empty menu is still possible after the offer
// (the {X} choice or a repricing changed the composition) and the defensive
// arm below preserves the flow's behaviour for it. It returns true once it
// has asked (and therefore suspended); payCast applies the accumulated
// payColor / payLife / payGeneric when every pip is settled.
func (e *Engine) manaAsk() bool {
	pc := e.cast
	if pc == nil || pc.payIdx >= pc.cost.annPipCount() {
		return false
	}
	alts := pc.cost.announcePip(pc.payIdx)
	// announceFeasible receives the full pool and life total because the
	// commitments already made (and this candidate face) are folded into the
	// cost it evaluates; nothing has been paid yet. Do not pre-filter a colour
	// face merely because the current pool lacks that colour: a Color$
	// reduction can make the announced face free (for example {W/U} under
	// Color$ W).
	pool, snow := e.G.Players[pc.player].Pool, e.G.Players[pc.player].Snow
	fullLife := e.G.Players[pc.player].Life
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Choose how to pay a mana symbol of " + e.G.Obj(pc.card).Face().Name,
		Source: pc.card}
	addPip := func(alt pipAlt) {
		switch {
		case alt.color != 0:
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "pay_" + string(alt.color), Label: "Pay " + string(alt.color), Amount: 1})
		case alt.generic > 0:
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "pay_generic", Label: fmt.Sprintf("Pay %d generic", alt.generic), Amount: int(alt.generic)})
		case alt.life > 0:
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "pay_life", Label: "Pay 2 life", Amount: 2})
		}
	}
	seen := map[byte]bool{}
	seenGeneric := false
	for _, alt := range alts {
		switch {
		case alt.color != 0:
			if seen[alt.color] {
				continue
			}
			seen[alt.color] = true
			if e.announceFeasible(pc, alt, pool, snow, fullLife) {
				addPip(alt)
			}
		case alt.generic > 0:
			if seenGeneric {
				continue
			}
			seenGeneric = true
			if e.announceFeasible(pc, alt, pool, snow, fullLife) {
				addPip(alt)
			}
		case alt.life > 0:
			if e.announceFeasible(pc, alt, pool, snow, fullLife) {
				addPip(alt)
			}
		}
	}
	if len(d.Options) == 0 {
		// Defensive: the offer gate proved at least one pip alternative
		// completes the cost, so a feasible option is always present for a
		// gated cast measured at the gate; this arm only guards the state
		// having shifted since (the {X} choice, a repricing, a shorter pool).
		// Rather than offer an infeasible payment, offer the first alternative
		// (index 0, the deterministic best) so the decision is never empty.
		addPip(alts[0])
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// manaConvertAsk poses the Optional$ ManaConvert election once for this
// proposal. A mandatory conversion remains automatic; an optional conversion
// is offered even when the ordinary pool already pays, because declining is a
// meaningful player choice and the grant may matter to a later repricing.
func (e *Engine) manaConvertAsk() bool {
	pc := e.cast
	if pc == nil || pc.manaConvertDone {
		return false
	}
	_, optional := e.manaConversionParts(pc.player, pc.card, pc.isAbility())
	if optional.empty() {
		pc.manaConvertDone = true
		return false
	}
	pc.manaConvertDone = true
	e.choosing = chooseCast
	e.ask(&decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Use optional mana conversion?", Source: pc.card,
		Options: []decision.Option{
			{Index: 0, Kind: "manaconvert", Label: "Use mana conversion", Obj: pc.card, Player: pc.player},
			{Index: 1, Kind: "manaconvert", Label: "Don't use mana conversion", Obj: pc.card, Player: pc.player},
		}})
	return true
}

// castAnswer records a chooseCast answer into the flow, keyed off which
// stage asked it (every option in one decision shares a Kind). A mana-window
// decision (CR 601.2g) is the exception: it offers both "activate" and
// "done" options, so the CHOSEN option's kind, not the stage's, identifies
// the answer.
func (e *Engine) castAnswer(d *decision.Decision, chosen []decision.Option) {
	pc := e.cast
	if pc == nil || len(d.Options) == 0 {
		return
	}
	// The decision's CHOSEN option identifies the answer, never the first
	// offered option. Most chooseCast decisions are single-kind (an {X} value,
	// a Delve exile, a sacrifice) so Options[0].Kind would coincidentally be
	// right, but a hybrid/Phyrexian/twobrid pip decision offers MIXED kinds
	// (pay_W, pay_generic, pay_life) and the mana window offers activate/done,
	// so dispatching on Options[0].Kind would mis-route a non-first choice
	// (picking a twobrid generic face from a decision whose first option is
	// pay_W fell into the pay_W branch and minted a colourless pip).
	kind := d.Options[0].Kind
	if len(chosen) > 0 {
		kind = chosen[0].Kind
	}
	switch kind {
	case "manaconvert":
		// Option 0 is the affirmative election. The decision is deliberately
		// positional rather than label-based so a translated label cannot alter
		// the payment semantics.
		pc.manaConvertUse = len(chosen) > 0 && chosen[0].Index == 0
	case "named_announce":
		if len(chosen) > 0 {
			pc.namedN = int32(chosen[0].Amount)
		}
	case "x":
		if len(chosen) > 0 {
			// The value rides on Option.Amount, not Option.Index: xAsk is the
			// first stage and appends 0..max into an empty option list, so
			// Index happens to equal the value today, but a later task that
			// prepends an option (a "cancel", Task 10's ability variants)
			// would silently corrupt an Index-derived value.
			pc.x = int32(chosen[0].Amount)
		}
	case "replicate":
		// CR 702.55a: the answered payment count folds that many replicate
		// payments into cost, so every later stage (Convoke, X, the payment
		// window, payCast) charges the composed total. The count rides on
		// Option.Amount, not Index, for the same reason xAsk's value does.
		if len(chosen) > 0 && pc.replicateSet {
			n := int32(chosen[0].Amount)
			rc := ParseCost(pc.replicateParam)
			for i := int32(0); i < n; i++ {
				pc.cost = pc.cost.Plus(rc)
			}
			pc.replicateTimes = n
		}
	case "multikick":
		// CR 702.43: the answered payment count folds that many multikicker
		// payments into cost -- the replicate arm's exact shape.
		if len(chosen) > 0 && pc.multikickSet {
			n := int32(chosen[0].Amount)
			mk := ParseCost(pc.multikickParam)
			for i := int32(0); i < n; i++ {
				pc.cost = pc.cost.Plus(mk)
			}
			pc.multikickTimes = n
		}
	case "squad":
		// CR 702.66: the answered payment count folds that many squad
		// payments into cost -- the replicate/multikicker arm's exact shape.
		if len(chosen) > 0 && pc.squadSet {
			n := int32(chosen[0].Amount)
			sc := ParseCost(pc.squadParam)
			for i := int32(0); i < n; i++ {
				pc.cost = pc.cost.Plus(sc)
			}
			pc.squadTimes = n
		}
	case "mutate_place":
		// CR 702.140b: the answered over/under placement. Option.Amount is 1
		// for "on top", 0 for "under", so the answer is read positionally.
		if len(chosen) > 0 {
			pc.mutateTop = chosen[0].Amount == 1
		}
	case "casualty":
		if len(chosen) == 1 {
			pc.sacs = append(pc.sacs, chosen[0].Obj)
			pc.casualtyPaid = true
			// Casualty:X: the chosen creature's power names the amount; it is
			// read live at payment (payCast), the rules time the sacrifice
			// settles.
			pc.casualtySac = chosen[0].Obj
		}
	case "gift_decline":
		// CR 702.168: a declined gift is the plain cast -- no promise, and
		// pushCast emits only the Amount-0 record. The byte-identical shape
		// for every non-Gift carrier, which never reaches this ask at all.
		pc.giftPromise = false
	case "gift_promise":
		// The promised opponent rides Option.Player, the field the
		// protector/player elections share. An answer naming no live seat
		// (only reachable from a hand-built decision) degrades to a decline
		// rather than silently promising seat 0.
		pc.giftPromise = false
		if len(chosen) > 0 && chosen[0].Player != pc.player &&
			int(chosen[0].Player) < len(e.G.Players) && !e.G.Players[chosen[0].Player].Lost {
			pc.giftPromise = true
			pc.giftTo = chosen[0].Player
		}
	case "conspire":
		// CR 702.78a: the two chosen creatures are the tap the conspired cast
		// pays. They settle through pc.taps (payCast taps them) and
		// conspirePaid records that the provenance flag is owed. A Min==Max==2
		// KChoose, so a well-formed answer is exactly two options.
		for _, o := range chosen {
			pc.taps = append(pc.taps, o.Obj)
		}
		if len(chosen) >= 2 {
			pc.conspirePaid = true
		}
	case "exile":
		for _, o := range chosen {
			pc.delve = append(pc.delve, o.Obj)
		}
	case "sacrifice":
		if pc.emerge && pc.sacPart == 0 && len(chosen) == 1 {
			pc.emergeSac = chosen[0].Obj
		}
		for _, o := range chosen {
			pc.sacs = append(pc.sacs, o.Obj)
			pc.sacPaid++
		}
		part := pc.cost.Sac[pc.sacPart]
		total := int(part.N)
		if part.Announced {
			total = int(pc.x)
		}
		if pc.sacPaid >= total {
			pc.sacPart++
			pc.sacPaid = 0
		}
	case "subcounter":
		// The chosen counter-removal pick of a SubCounter cost part: a
		// wildcard "Any" part records one counter unit per answer (the ask
		// loop keeps asking until the part is fully paid), a fixed-kind part
		// records its one object and advances.
		wildcard := pc.subCounterPart < len(pc.cost.SubCounter) && strings.EqualFold(pc.cost.SubCounter[pc.subCounterPart].Spec, "Any")
		for _, o := range chosen {
			pc.subCounterPays = append(pc.subCounterPays, subCounterPay{part: pc.subCounterPart, obj: o.Obj, kind: o.Counter})
		}
		if !wildcard {
			pc.subCounterPart++
		}
	case "discard":
		for _, o := range chosen {
			pc.discards = append(pc.discards, o.Obj)
		}
		pc.discardPart++
	case "altaddcost":
		// The either-or additional cost (AlternateAdditionalCost): the chosen
		// part's cost folds into pc.cost (Plus), so the ordinary stages settle
		// it and payCast charges it. The chosen part rides on Option.Amount
		// (the index into pc.altAddParts), not the option Index, for the same
		// reason xAsk's value does.
		if len(chosen) > 0 && chosen[0].Amount >= 0 && int(chosen[0].Amount) < len(pc.altAddParts) {
			pc.cost = pc.cost.Plus(ParseCost(pc.altAddParts[chosen[0].Amount]))
		}
	case "exilecost":
		for _, o := range chosen {
			pc.exiles = append(pc.exiles, o.Obj)
		}
		pc.exilePart++
	case "evidence":
		// The CollectEvidence payment's chosen graveyard cards (alltargeted1).
		// The SETTLE validation (total mana value at least the resolved
		// amount, every card still the payer's) runs in evidenceAsk on the
		// continueCast re-entry, which re-poses the ask when the answer falls
		// short -- so a malformed answer never pays a short evidence.
		for _, o := range chosen {
			pc.evidence = append(pc.evidence, o.Obj)
		}
	case "movetogravecost":
		for _, o := range chosen {
			pc.moveGraves = append(pc.moveGraves, o.Obj)
		}
		pc.moveGravePart++
	case "revealcost":
		for _, o := range chosen {
			pc.reveals = append(pc.reveals, o.Obj)
			pc.revealHandArm = append(pc.revealHandArm, true)
		}
		pc.revealPart++
	case "revealorchoose":
		// Either-or cost, REVEAL arm: the elected hand cards are a real public
		// reveal (revealHandArm true; emitChoiceCosts announces them).
		for _, o := range chosen {
			pc.reveals = append(pc.reveals, o.Obj)
			pc.revealHandArm = append(pc.revealHandArm, true)
		}
		pc.revealOrChoosePart++
	case "choosecost":
		// Either-or cost, CHOOSE arm: a permanent the payer controls, elected
		// at cast time. It rides the same paid list the `Revealed$<Property>`
		// refs read (Forge's CostReveal owns both arms) but revealHandArm is
		// false, so emitChoiceCosts announces a choice, never a reveal.
		for _, o := range chosen {
			pc.reveals = append(pc.reveals, o.Obj)
			pc.revealHandArm = append(pc.revealHandArm, false)
		}
		pc.revealOrChoosePart++
	case "beholdcost":
		for _, o := range chosen {
			pc.beholds = append(pc.beholds, o.Obj)
		}
		pc.beholdPart++
	case "tapcost":
		part := CostPart{}
		if pc.tapPart < len(pc.cost.TapPermanent) {
			part = pc.cost.TapPermanent[pc.tapPart]
		}
		for _, o := range chosen {
			pc.taps = append(pc.taps, o.Obj)
		}
		pc.tapPart++
		// A dynamic X-form part whose tap election ANNOUNCED the count (no other
		// announce-bearing part ran xAsk first -- see tapPermanentCostAsk): the
		// chosen count is the cast's {X} (CR 601.2b), which the pay-time
		// CastInfo then carries to resolution and Count$xPaid reads. A part
		// whose cost pre-announced the X (pc.xDone) settles exactly that value
		// and must not overwrite it.
		if part.Dyn == "X" && !pc.xDone {
			pc.x = int32(len(chosen))
		}
	case "blightcost":
		for _, o := range chosen {
			pc.blights = append(pc.blights, o.Obj)
		}
		pc.blightPart++
	case "returncost":
		for _, o := range chosen {
			// K:Ninjutsu (CR 702.49b): the permanent the activated ability puts
			// onto the battlefield attacks the SAME defender the returned
			// creature was attacking. Capture that defender here, while the
			// chosen attacker is still a battlefield object (payCast moves it to
			// hand at settlement, which clears its Attacking field), and carry it
			// on the AbilityPush event so resolution can bind it.
			if e.activationIsNinjutsu(pc) {
				if o := e.G.Obj(o.Obj); o != nil {
					pc.ninjutsuDefender = o.Attacking
					pc.ninjutsuDefenderObject = o.AttackingBattle
					pc.ninjutsuHasDefender = true
				}
			}
			// K:Sneak (CR 702.190b): the permanent the sneak cast puts onto
			// the battlefield attacks the SAME defender the returned creature
			// was attacking. Capture it while the chosen attacker is still a
			// battlefield object; pushCast then folds it onto the spell's
			// Remembered so the entry hook can bind it.
			if pc.mode == "sneak" {
				if o := e.G.Obj(o.Obj); o != nil {
					pc.sneakDefender = o.Attacking
					pc.sneakDefenderObject = o.AttackingBattle
					pc.sneakHasDefender = true
				}
			}
			pc.returns = append(pc.returns, o.Obj)
		}
		pc.returnPart++
	case "puttolibcost":
		for _, o := range chosen {
			pc.putToLibs = append(pc.putToLibs, o.Obj)
		}
		pc.putToLibPart++
	case "forage_exile":
		pc.cost.Exile = append(pc.cost.Exile, CostPart{N: 3, Spec: "Card", Zone: state.ZGraveyard})
	case "forage_food":
		if len(chosen) > 0 {
			pc.sacs = append(pc.sacs, chosen[0].Obj)
		}
	case "pay_W", "pay_U", "pay_B", "pay_R", "pay_G", "pay_C":
		// A hybrid or Phyrexian pip paid with pool mana: record which colour.
		if len(chosen) > 0 {
			pc.payColor[state.ManaIndex(chosen[0].Kind[4])]++
		}
		pc.payIdx++
	case "pay_life":
		// A Phyrexian face (plain or hybrid) paid with two life.
		pc.payLife += 2
		pc.payIdx++
	case "pay_generic":
		// A monocolour hybrid pip paid with its generic face.
		if len(chosen) > 0 {
			pc.payGeneric += int32(chosen[0].Amount)
		}
		pc.payIdx++
	case "convoke_W", "convoke_U", "convoke_B", "convoke_R", "convoke_G", "convoke_generic", "improvise_generic", "waterbend_generic":
		for _, choice := range chosen {
			color := byte(0)
			if strings.HasPrefix(choice.Kind, "convoke_") && choice.Kind != "convoke_generic" {
				color = choice.Kind[len("convoke_")]
			}
			pc.convoke = append(pc.convoke, convokePayment{id: choice.Obj, color: color,
				countsMana: strings.HasPrefix(choice.Kind, "convoke_"), waterbend: choice.Kind == "waterbend_generic"})
		}
	case "harmonize":
		for _, choice := range chosen {
			pc.convoke = append(pc.convoke, convokePayment{id: choice.Obj, power: int32(choice.Amount)})
		}
	case "activate":
		// CR 601.2g: a source's mana abilities are distinct activations that
		// share its tap cost. activateManaPayment resolves a singleton
		// immediately or asks the caster to choose one before re-entering
		// this payment window -- the payment-window form, so an
		// InstantSpeed$ True mana ability (Lion's Eye Diamond, "Activate
		// only as an instant") is withheld: paying a cost is no priority
		// moment.
		if len(chosen) > 0 {
			e.activateManaPayment(pc.player, chosen[0].Obj, true)
		}
	case "done":
		// CR 601.2g: the player declines further mana abilities; pay the cost.
		pc.windowDone = true
	case "mana":
		// The announced window's per-ability option (announce_pay.go).
		if pc.announced && len(chosen) > 0 {
			e.announcedActivate(pc, chosen[0])
		}
	case decision.OptAutoFill:
		// Auto-fill: the planner's activations for what is still owed, run by
		// the ordinary plan executor on re-entry (spec §4.4). The plan is
		// re-derived at the unchanged state the ask priced.
		if pc.announced {
			if plan := e.announcedAutoFillPlan(pc, e.castPaymentMana(pc)); plan != nil {
				pc.payment = &plannedCastPayment{actionID: decision.OptAutoFill, plan: *plan}
				pc.paymentNext = 0
				pc.paymentFallback = nil
			}
		}
	case decision.OptUndoTap:
		if pc.announced {
			if _, ok := e.undoableWindowTap(pc); ok {
				e.undoWindowTap(pc)
			}
		}
	case decision.OptCancelCast:
		if pc.announced {
			e.cancelAnnouncedCast(pc)
		}
	}
}
