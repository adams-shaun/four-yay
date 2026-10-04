package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// unlessPayment is the continuation between accepting an UnlessCost$ and
// completing its non-mana Sac/Discard/Reveal components. Unlike ordinary
// activation costs, an unless cost is paid during a suspended resolution, so
// its choices must retain that resolution rather than silently taking the
// first card.
type unlessPayment struct {
	payer    state.PlayerID
	cost     Cost
	ctx      effects.Ctx
	stackObj state.ObjID
	// tape marks a payment the resolution kernel drives in line
	// (tapeUnlessComponents): its asks are served from the tape and its
	// settlement lands in the asking walk's live Ctx. A payment that is not
	// tape-driven belongs to an activated mana ability, whose continuation
	// remains in manaUnlessActivation.
	tape     bool
	part     int
	sacs     []state.ObjID
	discards []state.ObjID
	// reveals holds the Reveal<N/Spec> picks (the hideaway-family ETB
	// lands, Xyru Specter's Challenge): unlike a discard the revealed cards
	// STAY in hand, so the dedup must be explicit — one card must not pay
	// two parts — and the settled picks are announced with one public Note
	// (the same event the cast flow's emitChoiceCosts emits).
	reveals []state.ObjID
	// beholds holds the Behold<N/Spec> picks (CR 702.176, Elven Passage's
	// "you may behold an Elf"): an object the payer controls on the
	// battlefield or a card revealed from their hand. The chosen object is
	// not moved (a plain Behold, unlike BeholdExile), so like a reveal it
	// needs the explicit dedup and one public Note.
	beholds []state.ObjID
	// returns holds the Return<N/Spec> picks (the cumulative-upkeep family's
	// "return a non-Lair land"): a permanent matching the spec returned to
	// its OWNER's hand (Forge CostReturn.moveToHand), one choice-bearing part
	// exactly like a sacrifice.
	returns []state.ObjID
	// exiles holds the Exile<N/Spec> picks (the Grip of Amnesia family's
	// "exile all cards from their graveyard"): cards exiled from the payer's
	// own hand or graveyard as the cost. A whole-zone part
	// (isWholeZoneExileSpec) takes EVERY candidate without asking; an
	// ordinary part is a choice exactly like a sacrifice.
	exiles []state.ObjID
}

func cloneUnlessCtx(in effects.Ctx) effects.Ctx {
	out := in
	out.Targets = append([]state.Target(nil), in.Targets...)
	out.Remembered = append([]state.Target(nil), in.Remembered...)
	out.Captured = append([]state.Target(nil), in.Captured...)
	out.Chosen = append([]state.Target(nil), in.Chosen...)
	if in.SVars != nil {
		out.SVars = make(map[string]string, len(in.SVars))
		for k, v := range in.SVars {
			out.SVars[k] = v
		}
	}
	return out
}

// unlessManaReachable reports whether pool plus one production alternative
// per window unit (or none of a unit) can satisfy cost's mana/life component.
// It is the exact affordability question the offer gate and the window's
// safety ordering both ask: a unit with several free abilities offers its
// alternatives as a CHOICE (the payer taps the permanent for one of them),
// never as their sum, so a dual land contributes {U} or {R} -- and the
// search proves a {U} tax payable from it. The search is bounded by
// manaPipCount (a minimal cover never needs more sources than the mana it
// supplies) and by a hard node budget; beyond that it fails closed, which is
// the conservative direction (never offer a Pay the window cannot complete).
func (e *Engine) unlessManaReachable(p state.PlayerID, cost Cost, pool, snow state.Mana, typed [7]state.Mana, life int32, conv *manaConv, units []windowManaUnit) bool {
	return e.manaReachable(p, cost, pool, snow, typed, life, pipRider{}, conv, units)
}

// manaReachable reports whether cost can be paid from pool plus at most one
// production alternative per mana source.  The cast announcement path uses a
// non-empty rider here: a MayPlayIgnoreColor grant must widen the same
// candidate face test as it widens the eventual payment.  The unless-payment
// callers deliberately retain their ordinary no-rider semantics through the
// wrapper above.
func (e *Engine) manaReachable(p state.PlayerID, cost Cost, pool, snow state.Mana, typed [7]state.Mana, life int32, rider pipRider, conv *manaConv, units []windowManaUnit) bool {
	payable := func(pool state.Mana, lifeNow int32) bool {
		_, ok := resolveManaWith(cost, pool, snow, typed, lifeNow,
			asPayer(e).PayLifeInsteadOfB(p), rider, conv)

		return ok
	}
	if payable(pool, life) {
		return true
	}
	budget := cost.ManaPipCount()
	if budget <= 0 {
		return false
	}
	nodes := 0
	var rec func(start, remaining int, acc state.Mana, lifeLeft int32) bool
	rec = func(start, remaining int, acc state.Mana, lifeLeft int32) bool {
		if payable(pay.ManaAdd(pool, acc), lifeLeft) {
			return true
		}
		if remaining <= 0 {
			return false
		}
		nodes++
		if nodes > 1<<18 {
			return false
		}
		for i := start; i < len(units); i++ {
			for _, a := range units[i].Alts {
				// A PayLife activation spends real life: it must be
				// present before the tap and is gone for the priced cost
				// afterwards. Every shared-window alt carries life 0, so
				// this is inert for the attack and unless windows.
				if a.Life > lifeLeft {
					continue
				}
				if rec(i+1, remaining-1, pay.ManaAdd(acc, a.Mana()), lifeLeft-a.Life) {
					return true
				}
			}
		}
		return false
	}
	return rec(0, budget, state.Mana{}, life)
}

// UnlessCostPayable is the rules-side offer gate for the generic unless
// election (the ctx-less form used by tests and any mana-only caller).
func (e *Engine) UnlessCostPayable(p state.PlayerID, raw string) bool {
	return e.unlessCostPayable(p, raw, nil, 0)
}

// UnlessCostPayableFromCtx is the full-context offer gate the effects ask
// uses (poseUnlessAsk): it hands the gate the very resolution context, so
// source- and role-dependent components (RevealChosen's secret designation,
// a SubCounter drain, and a Draw<.../Player.targetedBy> or TriggeredPlayer
// drawer) are evaluated against the same bindings the pay path will use. It
// is the same gate the resolution arm runs, so the option list and the pay
// decision cannot disagree.
func (e *Engine) UnlessCostPayableFromCtx(p state.PlayerID, raw string, ctx *effects.Ctx) bool {
	var stackObj state.ObjID
	if ctx != nil {
		stackObj = ctx.Source
	}
	return e.unlessCostPayable(p, raw, ctx, stackObj)
}

// unlessCostPayable is the ctx-aware offer gate. A cost is offered when every
// supported component is satisfiable RIGHT NOW: the choice-bearing Sac/
// Discard/Reveal parts must have enough distinct candidates, SubCounter parts
// enough counters on the resolving source, RevealChosen parts their secret
// designation, Draw parts a resolvable role, and the mana/life component must
// be reachable from the floating pool plus the window's alternatives. An
// unpriceable cost is a hard decline: it receives only the decline option, so
// an answer can never select a Pay that the settlement path must reject. The
// Sacrifice arm's recognised DamageYou<N> payment is separate and never calls
// this generic gate. The pre-fix gate skipped the non-mana components and used
// a single-ability-per-
// source aggregate, so it offered Pay for a `Sac<1/Creature>` with no
// creatures and suppressed a {U} tax on an untapped dual land.
func (e *Engine) unlessCostPayable(p state.PlayerID, raw string, ctx *effects.Ctx, stackObj state.ObjID) bool {
	cost, ok := ParseUnlessCost(raw)
	if !ok {
		return false
	}
	cost, ok = e.unlessFoldDynamic(p, cost, ctx)
	if !ok {
		return false
	}
	if ctx == nil {
		ctx = effects.NewCtxPtr(0, p, effects.CtxInit{})
	}
	if !e.unlessComponentsPayable(p, cost, ctx, stackObj) {
		return false
	}
	if !e.unlessEnergyAffordable(p, cost, ctx) {
		// An energy part is never mana: resolveMana ignores it, so the
		// payable checks below would silently pass an energy the payer cannot
		// cover. Forge CostPayEnergy.canPay reads the same counter total.
		return false
	}
	player := e.G.Players[p]
	d := paymentDescriptor{ID: stackObj, Class: paymentOther, Cost: &cost}
	if pay.CostPayableClass(asPayer(e), p, d, pipRider{}, cost) {
		return true
	}
	if !cost.HasManaPayment() {
		// No mana to window. A life/snow component is what the pool check
		// just rejected and no source can supply it, so do not offer.
		if cost.Life > 0 || cost.Snow > 0 {
			return false
		}
		return true
	}
	return e.unlessManaReachable(p, cost, player.Pool, player.Snow, player.ManaUnits(), player.Life,
		asPayer(e).Conv(p, stackObj, false), e.windowManaUnits(p))
}

// unlessFoldDynamic folds the unless cost's DYNAMIC life tokens to concrete
// Life charges against the payer's CURRENT life total, so the offer gate and
// both charge sites (payUnlessCost, advanceUnlessPayment) price the same
// resolved cost. LifeTotalHalfUp (Temporal Extortion) is half the payer's
// life rounded up and unpayable at zero life; an announced PayLife<X> that
// the string resolver could not fold (no resolvable SVar:X body) cannot be
// priced here either — fail closed, the strict-parser convention.
func (e *Engine) unlessFoldDynamic(p state.PlayerID, cost Cost, ctx *effects.Ctx) (Cost, bool) {
	out := cost
	if cost.LifeHalfUp {
		life := e.G.Players[p].Life
		if life <= 0 {
			return cost, false
		}
		out.Life = addClampedGeneric(out.Life, int64((life+1)/2))
		out.LifeHalfUp = false
	}
	if len(cost.LifeX) > 0 {
		if ctx == nil || !ctx.XAnnounced {
			return cost, false
		}
		for range cost.LifeX {
			out.Life = addClampedGeneric(out.Life, int64(ctx.X))
		}
		out.LifeX = nil
	}
	return out, true
}

// unlessEnergyAffordable reports whether the payer's energy counter total
// covers the cost's energy parts (CR 118.2d, Forge CostPayEnergy.canPay): a
// fixed part spends its N, the dynamic X part the RESOLVING ability's
// announced X — an X part with no announced X is unaffordable, never free.
// resolveMana ignores energy parts, so this check is the ONLY thing keeping
// an energy cost from being offered (and charged) for free.
func (e *Engine) unlessEnergyAffordable(p state.PlayerID, cost Cost, ctx *effects.Ctx) bool {
	total := int32(0)
	for _, part := range cost.Energy {
		if part.Spec == "X" {
			if ctx == nil || !ctx.XAnnounced {
				return false
			}
			total += ctx.X
			continue
		}
		total += part.N
	}
	return total == 0 || e.G.Players[p].Counter("ENERGY") >= total
}

// unlessComponentsPayable checks every non-mana part of an unless cost for
// satisfiability, sharing the pay path's own candidate enumeration
// (unlessCandidatesFor) so the gate can never disagree with
// beginUnlessPayment about what exists. The choice-bearing Sac/Discard/Reveal
// parts must admit a system of distinct representatives (one card cannot pay
// two parts); the synchronous parts (SubCounter/RevealChosen/Draw) are
// checked directly.
func (e *Engine) unlessComponentsPayable(p state.PlayerID, cost Cost, ctx *effects.Ctx, stackObj state.ObjID) bool {
	return e.unlessChoiceComponentsPayable(p, cost, ctx) &&
		e.unlessCountersAffordableFor(cost, *ctx, stackObj) &&
		unlessRevealChosenDesignated(e.G, cost, *ctx) &&
		e.unlessDrawsResolvable(p, cost, ctx)
}

// unlessChoiceComponentsPayable builds the per-sub-slot candidate lists for
// the choice-bearing parts and runs a bipartite matching: feasible iff a
// system of distinct representatives of the required size exists. A
// whole-zone Exile part (isWholeZoneExileSpec) is not a pick at all -- the
// payment takes EVERY matching candidate -- so it consumes its zone's
// candidates before the ordinary parts are matched, exactly as
// advanceUnlessPayment settles it.
func (e *Engine) unlessChoiceComponentsPayable(p state.PlayerID, cost Cost, ctx *effects.Ctx) bool {
	type slot struct {
		zone state.Zone
		kind string
		part CostPart
	}
	var subs []slot
	add := func(zone state.Zone, kind string, parts []CostPart) {
		for _, part := range parts {
			for i := int32(0); i < part.N; i++ {
				subs = append(subs, slot{zone: zone, kind: kind, part: part})
			}
		}
	}
	add(state.ZBattlefield, "sacrifice", cost.Sac)
	add(state.ZHand, "discard", cost.Discard)
	add(state.ZHand, "revealcost", cost.Reveal)
	add(state.ZBattlefield, "beholdcost", cost.Behold)
	add(state.ZBattlefield, "returncost", cost.Return)
	// Whole-zone Exile parts take the entire zone, so they are validated and
	// marked used first; an ordinary Exile part then joins the bipartite
	// match like any other pick.
	var used []state.ObjID
	for _, part := range cost.Exile {
		if !isWholeZoneExileSpec(part.Spec) {
			continue
		}
		avail := e.unlessCandidatesFor(p, *ctx, unlessExileZone(part), "exilecost", part, used)
		if int32(len(avail)) < part.N {
			return false
		}
		used = append(used, avail...)
	}
	for _, part := range cost.Exile {
		if isWholeZoneExileSpec(part.Spec) {
			continue
		}
		for i := int32(0); i < part.N; i++ {
			subs = append(subs, slot{zone: unlessExileZone(part), kind: "exilecost", part: part})
		}
	}
	if len(subs) == 0 {
		return true
	}
	cands := make([][]state.ObjID, len(subs))
	for i, s := range subs {
		cands[i] = e.unlessCandidatesFor(p, *ctx, s.zone, s.kind, s.part, used)
	}
	return maxBipartiteMatch(cands) == len(subs)
}

// maxBipartiteMatch returns the maximum matching size between left slots and
// the distinct right-side candidates, in deterministic slot/candidate order.
func maxBipartiteMatch(cands [][]state.ObjID) int {
	matchOf := map[state.ObjID]int{} // candidate -> slot
	var try func(slot int, seen map[state.ObjID]bool) bool
	try = func(slot int, seen map[state.ObjID]bool) bool {
		for _, c := range cands[slot] {
			if seen[c] {
				continue
			}
			seen[c] = true
			if prev, ok := matchOf[c]; !ok || try(prev, seen) {
				matchOf[c] = slot
				return true
			}
		}
		return false
	}
	n := 0
	for i := range cands {
		if try(i, map[state.ObjID]bool{}) {
			n++
		}
	}
	return n
}

func (e *Engine) unlessDrawsResolvable(p state.PlayerID, cost Cost, ctx *effects.Ctx) bool {
	for _, part := range cost.Draw {
		if _, ok := unlessDrawPlayers(ctx, p, part.Spec); !ok {
			return false
		}
	}
	return true
}

// beginUnlessPayment begins a payer-selected payment. It owns all Sac,
// Discard and Reveal components, including the exact-candidate no-ask cases,
// so neither this path nor a future sibling can fall back to a
// first-in-zone-order pick.
func (e *Engine) beginUnlessPayment(payer state.PlayerID, cost Cost, ctx *effects.Ctx, stackObj state.ObjID) {
	// Fold the dynamic life tokens once, at continuation start: the offer
	// gate folded the same amounts against the same payer read, and the
	// charge below prices exactly this folded cost.
	cost, ok := e.unlessFoldDynamic(payer, cost, ctx)
	if !ok {
		e.unlessPayment = &unlessPayment{payer: payer, ctx: cloneUnlessCtx(*ctx), stackObj: stackObj}
		e.finishUnlessPayment(false)
		return
	}
	e.unlessPayment = &unlessPayment{payer: payer, cost: cost, ctx: cloneUnlessCtx(*ctx), stackObj: stackObj}
	e.advanceUnlessPayment()
}

// unlessPartAt returns the cost component at flat index i across the
// unlessPayment's Sac, Discard, Reveal, Behold, Return and Exile lists,
// together with the zone its candidates come from and the option kind the wire
// carries. The
// option kind "revealcost" is the cast flow's own reveal-cost string
// (rules/cast.go), and "exilecost" its exile-cost string, so a client sees
// the same vocabulary for both paths.
func unlessPartAt(cost Cost, i int) (CostPart, state.Zone, string) {
	nSac, nDisc, nRev, nBeh := len(cost.Sac), len(cost.Discard), len(cost.Reveal), len(cost.Behold)
	nRet := len(cost.Return)
	switch {
	case i < nSac:
		return cost.Sac[i], state.ZBattlefield, "sacrifice"
	case i < nSac+nDisc:
		return cost.Discard[i-nSac], state.ZHand, "discard"
	case i < nSac+nDisc+nRev:
		return cost.Reveal[i-nSac-nDisc], state.ZHand, "revealcost"
	case i < nSac+nDisc+nRev+nBeh:
		// The zone field is the primary (battlefield) scan; the candidate
		// enumeration for a beholdcost part deliberately scans BOTH the
		// battlefield and the hand (CR 702.176).
		return cost.Behold[i-nSac-nDisc-nRev], state.ZBattlefield, "beholdcost"
	case i < nSac+nDisc+nRev+nBeh+nRet:
		return cost.Return[i-nSac-nDisc-nRev-nBeh], state.ZBattlefield, "returncost"
	default:
		part := cost.Exile[i-nSac-nDisc-nRev-nBeh-nRet]
		return part, unlessExileZone(part), "exilecost"
	}
}

// unlessExileZone is the zone an Exile cost part draws from. CostPart.Zone's
// zero value means the hand (the same reading the cast-cost parser's
// FromHand arm leaves behind), so a FromGrave/AnyGrave part carries
// state.ZGraveyard and a FromHand part reads as state.ZHand.
func unlessExileZone(part CostPart) state.Zone {
	if part.Zone == 0 {
		return state.ZHand
	}
	return part.Zone
}

// paymentPartCount is the flat count of the choice-bearing components
// (Sac, Discard, Reveal, Behold, Return, Exile) the continuation walks before
// it settles the synchronous ones.
func (u *unlessPayment) paymentPartCount() int {
	return len(u.cost.Sac) + len(u.cost.Discard) + len(u.cost.Reveal) + len(u.cost.Behold) + len(u.cost.Return) + len(u.cost.Exile)
}

func (e *Engine) advanceUnlessPayment() {
	u := e.unlessPayment
	if u == nil {
		return
	}
	if int(u.payer) < 0 || int(u.payer) >= len(e.G.Players) ||
		!e.unlessCountersAffordable(u) ||
		!u.revealChosenDesignated(e) {
		e.finishUnlessPayment(false)
		return
	}
	// A mana component the pool cannot yet cover opens the one-source-at-a-
	// time CR 601.2g window (the ward/attack-prop discipline) rather than
	// declining: the payer taps until the pool covers the charge, then the
	// ordinary path below pays it. The window is only opened while a source
	// remains that the budget counted.
	d := paymentDescriptor{ID: u.stackObj, Class: paymentOther, Cost: &u.cost}
	// Open the window only when the POOL ALONE cannot pay (lifeGrant false:
	// a {B} pip K'rrik's grant could settle with 2 life must not close it)
	// AND a window source remains. The ordinary grant-bearing check below is
	// retained: when no source can help (or the payer answers Done with the
	// pool already covering the charge) the granted life still pays it.
	if !pay.CostPayableClassLife(asPayer(e), u.payer, d, pipRider{}, u.cost, false) &&
		u.cost.HasManaPayment() && len(e.windowManaUnits(u.payer)) > 0 {
		e.askUnlessMana()
		return
	}
	if !pay.CostPayableClass(asPayer(e), u.payer, d, pipRider{}, u.cost) {
		e.finishUnlessPayment(false)
		return
	}
	for u.part < u.paymentPartCount() {
		part, zone, kind := unlessPartAt(u.cost, u.part)
		eligible := e.unlessPaymentCandidates(u, zone, kind, part)
		if int32(len(eligible)) < part.N {
			e.finishUnlessPayment(false)
			return
		}
		if kind == "exilecost" && isWholeZoneExileSpec(part.Spec) {
			// ExileFromGrave<1/All> names the WHOLE zone: every matching
			// candidate is the payment, never a pick (the same
			// isWholeZoneExileSpec reading the cast gate and the
			// triggered-cost window take). The count check above still
			// demands part.N cards, so an empty zone declines.
			e.recordUnlessPaymentPick(u, kind, eligible)
			u.part++
			continue
		}
		if int32(len(eligible)) == part.N {
			e.recordUnlessPaymentPick(u, kind, eligible)
			u.part++
			continue
		}
		prompt := fmt.Sprintf("Choose %d card(s) to %s to pay the cost", part.N, kind)
		if kind == "revealcost" {
			prompt = fmt.Sprintf("Choose %d card(s) to reveal to pay the cost", part.N)
		}
		if kind == "returncost" {
			prompt = fmt.Sprintf("Return %d permanent(s) to their owner's hand to pay the cost", part.N)
		}
		if kind == "beholdcost" {
			prompt = fmt.Sprintf("Choose %d permanent(s) you control or card(s) in your hand to behold to pay the cost", part.N)
		}
		if kind == "exilecost" {
			zoneName := "hand"
			if zone == state.ZGraveyard {
				zoneName = "graveyard"
			}
			prompt = fmt.Sprintf("Exile %d card(s) from your %s to pay the cost", part.N, zoneName)
		}
		d := &decision.Decision{Player: u.payer, Kind: decision.KChoose,
			Min: int(part.N), Max: int(part.N), Source: u.ctx.Source,
			ResumeKind: "unless_cost",
			Prompt:     prompt}
		for _, id := range eligible {
			label := "a card"
			if o := e.G.Obj(id); o != nil && o.Face() != nil {
				label = o.Face().Name
			}
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: kind,
				Label: label, Obj: id, Player: u.payer})
		}
		windowAsk(e, d, chooseUnlessCost)
		return
	}
	// Resolve every drawer before charging any component. A Draw<N/Spec> may
	// name a role that this suspended resolution did not retain; that is an
	// unpayable cost, not a reason to spend mana/life and then silently omit
	// the draw. Keeping the resolved players also makes the selected-cost path
	// use the same binding rules as payUnlessCost's no-choice path.
	drawers := make([][]state.PlayerID, len(u.cost.Draw))
	for i, part := range u.cost.Draw {
		players, ok := unlessDrawPlayers(&u.ctx, u.payer, part.Spec)
		if !ok {
			e.finishUnlessPayment(false)
			return
		}
		for _, p := range players {
			if int(p) < 0 || int(p) >= len(e.G.Players) {
				e.finishUnlessPayment(false)
				return
			}
		}
		drawers[i] = players
	}
	if !pay.PayManaConv(asPayer(e), u.payer, u.cost, asPayer(e).Conv(u.payer, u.stackObj, false)) { // guarded above; retain totality if state changes.
		e.finishUnlessPayment(false)
		return
	}
	// Energy parts (PayEnergy<N>/<X>): the same shared charging site the cast
	// flow uses, one PlayerCounterChange per part. The offer gate proved the
	// total affordable; a state change since the choices is still guarded by
	// the same read.
	if !e.unlessEnergyAffordable(u.payer, u.cost, &u.ctx) {
		e.finishUnlessPayment(false)
		return
	}
	x := int32(0)
	if u.ctx.XAnnounced {
		x = u.ctx.X
	}
	e.chargeEnergyCost(u.payer, u.cost, x)
	// The settled reveal picks are announced exactly like the cast flow's
	// emitChoiceCosts announces them: one public Note carrying the revealed
	// ids (the cards STAY in hand), emitted only on the paid path. The
	// RevealChosen parts have no ids -- they make the source's secret
	// designation public with one Note too, the same call emitChoiceCosts
	// makes.
	if len(u.reveals) > 0 {
		names := make([]string, 0, len(u.reveals))
		for _, id := range u.reveals {
			names = append(names, e.targetName(id))
		}
		e.emit(events.Event{Kind: events.Note, Player: u.payer, Obj: u.ctx.Source,
			IDs:  append([]state.ObjID(nil), u.reveals...),
			Text: "revealed " + strings.Join(names, ", ") + " as a cost"})
	}
	// The settled Behold picks are announced exactly like the cast flow's
	// emitChoiceCosts announces them: one public Note carrying the beheld ids
	// (nothing is moved for a plain Behold; the ids let a `Remembered`/
	// observer read the choice).
	if len(u.beholds) > 0 {
		names := make([]string, 0, len(u.beholds))
		for _, id := range u.beholds {
			names = append(names, e.targetName(id))
		}
		e.emit(events.Event{Kind: events.Note, Player: u.payer, Obj: u.ctx.Source,
			IDs:  append([]state.ObjID(nil), u.beholds...),
			Text: "beheld " + strings.Join(names, ", ") + " as a cost"})
	}
	for _, part := range u.cost.RevealChosen {
		if text, ok := revealChosenText(e.G, e.G.Obj(u.ctx.Source), part.Spec); ok {
			e.emit(events.Event{Kind: events.Note, Player: u.payer, Obj: u.ctx.Source, Text: text})
		}
	}
	for _, id := range u.sacs {
		e.emit(events.Sacrifice(id))
	}
	// Bracket the settled Discard component's emissions as ONE discard action
	// (CR 701.8), exactly as the cast flow's payDiscardCost and effDiscard's
	// api:Discard do: a call to discard two cards is one discard action, so a
	// Mode$ DiscardedAll observer queues ONE trigger whose TriggerCount$Amount
	// is the number of cards, not one batch-of-one per event. Nothing here can
	// suspend (all choices were collected above), so the close runs immediately
	// after the last emitted discard, before the Return/Draw parts and
	// finishUnlessPayment resume resolution. Guarded so a payment with no
	// discard leaves no bracket open.
	if len(u.discards) > 0 {
		e.BeginDiscardBatch()
	}
	for _, id := range u.discards {
		e.emit(events.Discard(id, u.payer))
	}
	if len(u.discards) > 0 {
		e.EndDiscardBatch()
	}
	// Return parts: each chosen permanent moves to its OWNER's hand (Forge
	// CostReturn.moveToHand), the same event shape the cast flow's settle
	// emits for a Return cost part.
	for _, id := range u.returns {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.ReturnCost(id, o.Zone))
		}
	}
	// Exile parts: each chosen card moves from its own zone to exile, the
	// same event shape the cast flow's settle emits for an Exile cost part
	// (Forge CostExile.moveToExile).
	for _, id := range u.exiles {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZExile, Text: "exiled as a cost"})
		}
	}
	src := u.ctx.Source
	if o := e.G.Obj(u.stackObj); o != nil && o.Ability != nil {
		src = o.Source
	}
	for _, part := range u.cost.SubCounter {
		e.emit(events.Event{Kind: events.CounterChange, Obj: src, Counter: part.Spec, Amount: -part.N})
	}
	for i, part := range u.cost.Draw {
		for _, p := range drawers[i] {
			for n := int32(0); n < part.N; n++ {
				effects.DrawFor(e, p)
			}
		}
	}
	e.finishUnlessPayment(true)
}

// askUnlessMana opens one tap ask over the payer's remaining window-eligible
// sources. Every production alternative of a source is its own option (a dual
// land offers "Tap Volcanic Island for {U}" and "... for {R}"), so the tap
// resolves the chosen ability with no nested colour ask. Options that keep
// the charge reachable come first, and the bot's first-activate pick is then
// always a non-stranding tap; options that would strand remain offered (a
// player may make a losing tap) but after every safe one. "Done" is always
// legal: it settles the payment (the ordinary path declines if the pool still
// cannot cover the charge) and is the R-9 no-host-compatible completion.
func (e *Engine) askUnlessMana() {
	u := e.unlessPayment
	if u == nil {
		return
	}
	units := e.windowManaUnits(u.payer)
	pool := e.G.Players[u.payer].Pool
	snow := e.G.Players[u.payer].Snow
	typed := e.G.Players[u.payer].ManaUnits()
	life := e.G.Players[u.payer].Life
	d := &decision.Decision{Player: u.payer, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Activate mana abilities to pay the unless cost", ResumeKind: "unless_mana"}
	// Safe alternatives first: tapping this one leaves the charge reachable
	// from the remaining sources, so the first-activate bot arm cannot strand.
	for _, safe := range []bool{true, false} {
		for si, src := range units {
			rest := make([]windowManaUnit, 0, len(units)-1)
			rest = append(rest, units[:si]...)
			rest = append(rest, units[si+1:]...)
			for ai, a := range src.Alts {
				if safe != (e.unlessManaReachable(u.payer, u.cost, pay.ManaAdd(pool, a.Mana()), snow, typed, life,
					asPayer(e).Conv(u.payer, u.stackObj, false), rest)) {
					continue
				}
				name := "a mana source"
				if o := e.G.Obj(src.ID); o != nil && o.Face() != nil {
					name = o.Face().Name
				}
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "activate",
					Obj: src.ID, Ability: ai, Label: "Tap " + name + pay.AltLabel(a)})
			}
		}
	}
	d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "done", Label: "Done"})
	windowAsk(e, d, chooseUnlessMana)
}

// answerUnlessMana applies one window answer. "Done" re-enters the ordinary
// payment path (which declines if the pool is still short); an activation
// resolves the recorded alternative's exact *cards.SA -- the membership walk
// captured it, so no chooseMana sub-ask is posed -- and then either re-enters
// advanceUnlessPayment (a sub-askless activation) or waits for
// handleChoose's continuation.
func (e *Engine) answerUnlessMana(chosen []decision.Option) {
	u := e.unlessPayment
	e.choosing = chooseNone
	if u == nil || len(chosen) != 1 {
		return
	}
	if chosen[0].Kind == "done" {
		e.advanceUnlessPayment()
		return
	}
	if chosen[0].Kind != "activate" {
		e.finishUnlessPayment(false)
		return
	}
	for _, src := range e.windowManaUnits(u.payer) {
		if src.ID != chosen[0].Obj {
			continue
		}
		ai := chosen[0].Ability
		if ai < 0 || ai >= len(src.Alts) {
			break
		}
		e.resolveManaAbility(u.payer, src.ID, src.Alts[ai].Ma, false)
		if e.Pending() == nil {
			e.advanceUnlessPayment()
		}
		return
	}
	e.finishUnlessPayment(false)
}

// unlessCandidatesFor is the shared candidate enumeration the offer gate
// (unlessChoiceComponentsPayable) and the payment continuation
// (unlessPaymentCandidates) both use, so the two can never disagree about
// what exists. used lists already-committed picks that must not be offered
// twice; the gate passes the empty list.
func (e *Engine) unlessCandidatesFor(payer state.PlayerID, ctx effects.Ctx, zone state.Zone, kind string, part CostPart, used []state.ObjID) []state.ObjID {
	seen := make(map[state.ObjID]bool, len(used))
	for _, id := range used {
		seen[id] = true
	}
	sc := ctx.SpecContext(payer)
	// A Behold<N/Spec> part (CR 702.176) draws its candidates from TWO zones:
	// permanents the payer controls on the battlefield, then cards in their
	// hand. This is exactly the enumeration the cast flow's beholdCostAsk
	// builds (battlefield first, then hand, source not excluded), so an
	// unless-cost Behold and a cast-cost Behold offer the same objects in
	// the same order.
	if kind == "beholdcost" {
		var out []state.ObjID
		for _, z := range [...]state.Zone{state.ZBattlefield, state.ZHand} {
			for _, id := range e.G.Zone(z, payer) {
				if seen[id] {
					continue
				}
				if z == state.ZBattlefield {
					if o := e.G.Obj(id); o == nil || !existsOnBattlefield(o) {
						continue
					}
				}
				if e.matchesSpec(part.Spec, id, sc) {
					out = append(out, id)
				}
			}
		}
		return out
	}
	var out []state.ObjID
	for _, id := range e.G.Zone(zone, payer) {
		if kind == "sacrifice" && e.sacrificeBlockedForCost(id, costCauseResolution) {
			// A CantSacrifice restriction (Call for Aid) or face static: the
			// permanent cannot pay a sacrifice component. An unless payment
			// is a resolution-election payment, never a cast/activation cost,
			// so the cause is costCauseResolution -- inadmissible by every
			// readable ValidCause$ base, which scopes the line closed here
			// (the permissive direction) while a bare ForCost$ True and an
			// Effect-registered CantSacrifice still apply.
			continue
		}
		if kind == "exilecost" && zone == state.ZBattlefield && e.exileBlockedForCost(id, costCauseResolution) {
			// The exile candidate guard the cast/activation gate applies,
			// restricted to a battlefield source: a CantExile static whose
			// ForCost$ True restricts cost payments withholds the candidate.
			// A hand/graveyard ExileFromHand/FromGrave/AnyGrave part reads no
			// zone restriction here, matching costCandidates' walk.
			continue
		}
		// A whole-zone Exile part (ExileFromGrave<1/All>) names the WHOLE
		// zone, not a filter: "All" matches no card through matchesSpec, so
		// every still-available card in the zone is a candidate -- the same
		// isWholeZoneExileSpec reading the cast gate and the triggered window
		// take.
		if kind == "exilecost" && isWholeZoneExileSpec(part.Spec) {
			if !seen[id] {
				out = append(out, id)
			}
			continue
		}
		if !seen[id] && e.matchesSpec(part.Spec, id, sc) {
			out = append(out, id)
		}
	}
	return out
}

func (e *Engine) unlessPaymentCandidates(u *unlessPayment, zone state.Zone, kind string, part CostPart) []state.ObjID {
	// The dedup is over the union of every component's picks, not just this
	// one's list: one card must not pay two parts, and a card already
	// sacrificed has left its zone anyway, so the wider union only ever
	// removes an already-impossible candidate. Revealed and beheld cards are
	// NOT removed from the hand, which is exactly why they need the explicit
	// exclusion.
	used := make([]state.ObjID, 0, len(u.sacs)+len(u.discards)+len(u.reveals)+len(u.beholds)+len(u.returns)+len(u.exiles))
	used = append(used, u.sacs...)
	used = append(used, u.discards...)
	used = append(used, u.reveals...)
	used = append(used, u.beholds...)
	used = append(used, u.returns...)
	used = append(used, u.exiles...)
	return e.unlessCandidatesFor(u.payer, u.ctx, zone, kind, part, used)
}

func (e *Engine) recordUnlessPaymentPick(u *unlessPayment, kind string, ids []state.ObjID) {
	switch recordUnlessPaymentPickCodes.Code(string(kind)) {
	case recordUnlessPaymentPickSacrifice:
		u.sacs = append(u.sacs, ids...)
	case recordUnlessPaymentPickRevealcost:
		u.reveals = append(u.reveals, ids...)
	case recordUnlessPaymentPickBeholdcost:
		u.beholds = append(u.beholds, ids...)
	case recordUnlessPaymentPickReturncost:
		u.returns = append(u.returns, ids...)
	case recordUnlessPaymentPickExilecost:
		u.exiles = append(u.exiles, ids...)
	default:
		u.discards = append(u.discards, ids...)
	}
}

// revealChosenDesignated reports whether every RevealChosen<Spec> part of the
// pending payment still has its secret designation on the source. A part with
// no designation is unpayable, so the whole payment declines.
func (u *unlessPayment) revealChosenDesignated(e *Engine) bool {
	return unlessRevealChosenDesignated(e.G, u.cost, u.ctx)
}

// unlessRevealChosenDesignated is the shared RevealChosen check: the offer
// gate (unlessComponentsPayable) and the payment continuation
// (revealChosenDesignated) both require every part's secret designation to be
// present on the resolving source.
func unlessRevealChosenDesignated(g *state.Game, cost Cost, ctx effects.Ctx) bool {
	if len(cost.RevealChosen) == 0 {
		return true
	}
	src := g.Obj(ctx.Source)
	for _, part := range cost.RevealChosen {
		if !hasRevealChosenDesignation(src, part.Spec) {
			return false
		}
	}
	return true
}

func (e *Engine) unlessCountersAffordable(u *unlessPayment) bool {
	return e.unlessCountersAffordableFor(u.cost, u.ctx, u.stackObj)
}

// unlessCountersAffordableFor is the shared SubCounter feasibility check: the
// offer gate and the payment continuation both resolve the draining source the
// same way (an activated ability's host, else the resolving source -- exactly
// payUnlessCost's rule) and require enough counters of each named kind.
func (e *Engine) unlessCountersAffordableFor(cost Cost, ctx effects.Ctx, stackObj state.ObjID) bool {
	if len(cost.SubCounter) == 0 {
		return true
	}
	src := ctx.Source
	if o := e.G.Obj(stackObj); o != nil && o.Ability != nil {
		src = o.Source
	}
	o := e.G.Obj(src)
	if o == nil {
		return false
	}
	need := make(map[string]int32, len(cost.SubCounter))
	for _, part := range cost.SubCounter {
		need[part.Spec] += part.N
	}
	for kind, n := range need {
		have := int32(0)
		for _, c := range o.Counters {
			if c.Kind == kind {
				have += c.N
			}
		}
		if have < n {
			return false
		}
	}
	return true
}

func (e *Engine) answerUnlessPayment(chosen []decision.Option) {
	u := e.unlessPayment
	if u == nil || u.part >= u.paymentPartCount() {
		return
	}
	part, zone, kind := unlessPartAt(u.cost, u.part)
	ids := make([]state.ObjID, 0, len(chosen))
	for _, o := range chosen {
		if o.Obj != 0 {
			ids = append(ids, o.Obj)
		}
	}
	// Decision.Validate guaranteed the count and offered identity. Recheck the
	// zone/filter against the current game before any event is emitted so a
	// malformed resumed state declines rather than paying an illegal cost.
	eligible := e.unlessPaymentCandidates(u, zone, kind, part)
	allowed := make(map[state.ObjID]bool, len(eligible))
	for _, id := range eligible {
		allowed[id] = true
	}
	if int32(len(ids)) != part.N {
		e.finishUnlessPayment(false)
		return
	}
	for _, id := range ids {
		if !allowed[id] {
			e.finishUnlessPayment(false)
			return
		}
	}
	e.recordUnlessPaymentPick(u, kind, ids)
	u.part++
	e.choosing = chooseNone
	e.advanceUnlessPayment()
}

func (e *Engine) finishUnlessPayment(paid bool) {
	u := e.unlessPayment
	if u == nil {
		return
	}
	e.unlessPayment = nil
	e.choosing = chooseNone
	if u.tape {
		tapeUnlessSettled(e, u, paid)
		return
	}
	e.finishManaUnlessPayment(paid)
}

type recordUnlessPaymentPickCode uint16

const (
	recordUnlessPaymentPickSacrifice recordUnlessPaymentPickCode = iota + 1
	recordUnlessPaymentPickRevealcost
	recordUnlessPaymentPickBeholdcost
	recordUnlessPaymentPickReturncost
	recordUnlessPaymentPickExilecost
)

var recordUnlessPaymentPickCodes = state.NewStrCodes(
	state.StrEntry[recordUnlessPaymentPickCode]{Key: "sacrifice", Val: recordUnlessPaymentPickSacrifice},
	state.StrEntry[recordUnlessPaymentPickCode]{Key: "revealcost", Val: recordUnlessPaymentPickRevealcost},
	state.StrEntry[recordUnlessPaymentPickCode]{Key: "beholdcost", Val: recordUnlessPaymentPickBeholdcost},
	state.StrEntry[recordUnlessPaymentPickCode]{Key: "returncost", Val: recordUnlessPaymentPickReturncost},
	state.StrEntry[recordUnlessPaymentPickCode]{Key: "exilecost", Val: recordUnlessPaymentPickExilecost},
)
