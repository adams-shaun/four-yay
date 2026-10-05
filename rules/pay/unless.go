package pay

import (
	"strings"

	"github.com/adams-shaun/gorge/effects"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// HasRevealChosenDesignation reports whether o still carries the
// secretly-chosen designation a RevealChosen<Spec> part names: a player entry
// in Object.Chosen for RevealChosen<Player> (the Secretly$ True ChoosePlayer
// answer), a non-empty Object.ChosenType for RevealChosen<Type/...> (the
// Secretly$ True ChooseType answer). A RevealChosen part has no alternative
// payment -- no hand card is picked -- so an unset designation makes the whole
// cost unpayable and the ability is not offered at all.
func HasRevealChosenDesignation(o *state.Object, spec string) bool {
	if o == nil {
		return false
	}
	if strings.EqualFold(spec, "Player") {
		for _, t := range o.Chosen {
			if t.IsPlayer {
				return true
			}
		}
		return false
	}
	return o.ChosenType != ""
}

// CostAnnouncesSacX reports whether the cost carries a Sac<X/Spec> part whose
// count the cast announces (the Dargo shape).
func CostAnnouncesSacX(c costvocab.Cost) bool {
	for _, part := range c.Sac {
		if part.Announced {
			return true
		}
	}
	return false
}

// FixLifeXCost resolves an announced PayLife<X> cost part whose source face
// defines SVar:X with a body that is NOT Count$xPaid. Forge's SVar:X is the
// definition that body gives the cost's X, and exactly two shapes exist in
// the corpus:
//
//   - Count$xPaid (Toxic Deluge, Krumar Initiate's "Cost$ X B T PayLife<X>",
//     every SP face carrying the token): "the announced X" -- the payer
//     announces X freely (bounded by life, xAsk) and the life part settles at
//     the same value the printed {X} folds at. The cost passes through
//     unchanged.
//   - any other resolvable body (Murderous Betrayal's
//     SVar:X:Count$YourLifeTotal/HalfUp, Tornado's
//     SVar:X:Count$CardCounters.VELOCITY/Times.3): the value is FIXED -- the
//     payer announces nothing, and the settle pays exactly the evaluated
//     amount. Each such part is folded into Cost.Life, the fixed additional
//     cost both the payable gates (resolveManaWith's life check) and the
//     settle (payMana's LifeChange) already price and charge -- the same
//     place a RaiseCost's fixed life raise lands, so CR 601.2f's ordering
//     treats it as an additional cost that no reduction ever touches.
//
// The verdict is three-way, and ok=false is WITHHOLD: the SVar:X body is
// present, not Count$xPaid, and either unresolvable (War Room's commander
// colour identity) or negative -- the ability is not offered at all (the
// fail-closed direction ParseUnlessCost and manaAbilityPayable already take)
// rather than offered with an arbitrary announcement the payer cannot be
// held to. A cost pairing the non-xPaid body with a printed {X} or another
// announced part sharing the X (Sac<X/Spec>, PayEnergy<X>,
// SubCounter<X/Kind>) is also withheld: the corpus carries no such face, and
// what the fixed body would mean for a shared announcement is undefined
// here. The idempotence contract matters: offerCastable converts at the
// gate, the payment sites convert again, and a converted cost (no LifeX
// left) early-returns unchanged.
func FixLifeXCost(e Engine, p state.PlayerID, id state.ObjID, c costvocab.Cost) (costvocab.Cost, bool) {
	if len(c.LifeX) == 0 {
		return c, true
	}
	o := e.Game().Obj(id)
	if o == nil || o.Face() == nil {
		return c, true
	}
	body, present := o.Face().SVars["X"]
	if !present || strings.EqualFold(strings.TrimSpace(body), "Count$xPaid") {
		return c, true
	}
	if c.X > 0 || CostAnnouncesSacX(c) {
		return c, false
	}
	for _, part := range c.Energy {
		if part.Spec == "X" {
			return c, false
		}
	}
	for _, part := range c.SubCounter {
		if part.Announced {
			return c, false
		}
	}
	ctx := effects.NewCtxPtr(id, p, effects.CtxInit{SVars: o.Face().SVars})
	n, resolvable := e.Eval().EvalCount(ctx, body)
	if !resolvable || n < 0 {
		return c, false
	}
	out := c
	out.LifeX = nil
	for range len(c.LifeX) {
		out.Life = costvocab.AddClampedGeneric(out.Life, int64(n))
	}
	return out, true
}

// DrawCostCount resolves one Draw cost part's count at payment time. A
// literal part (Dyn == "") is simply N. A dynamic part (Forge's
// Draw<X/Spec>, Champion of Wits' "draw cards equal to its power") reads the
// source face's SVar table: the body named by part.Dyn (SVar:X for
// Draw<X/...>) is evaluated exactly the way fixLifeXCost evaluates its
// PayLife<X> body, with the source object bound as the count context so
// Count$CardPower reads the drawing permanent's own power. ok=false means
// the source face, the SVar, or the body is unavailable -- the cost is
// unpayable (the fail-closed direction), never a silent zero draw.
func DrawCostCount(e Engine, id state.ObjID, you state.PlayerID, part costvocab.CostPart) (int32, bool) {
	return DrawCostCountTrig(e, id, you, part, nil, nil)
}

// DrawCostCountTrig is drawCostCount with an optional fire-time trigger
// context seeded into the evaluation: the triggered-cost window's dynamic
// Draw<X/Spec> part (Hordewing Skaab's "draw cards equal to the number of
// opponents dealt damage this way", SVar:X:TriggeredPlayersTargets$Amount)
// reads the DAMAGE BATCH the triggering event captured, which the bare
// cast-flow context carries nothing of. A nil context is the ordinary
// cast/activation read, unchanged.
//
// remembered is the trigger's own capture (the parked stack object's
// Remembered, as rules.resolvingRemembered derives it). Ctx.TriggeredCard$
// referents resolve through Ctx.Remembered (refTargets' refTargetsTriggeredCard
// arm), not Ctx.TriggerCard, so a triggered cost whose SVar names
// TriggeredCard$CastTotalManaSpent (Uncover the Moon-Letters' Cost$ Draw<X/You>)
// saw an empty set and counted zero. The window passes the same set the body
// reads after the cost settles; the ordinary cast/activation callers pass nil,
// which leaves the cast flow's own Remembered untouched.
func DrawCostCountTrig(e Engine, id state.ObjID, you state.PlayerID, part costvocab.CostPart, tcx *effects.TriggerContext, remembered []state.Target) (int32, bool) {
	if part.Dyn == "" {
		return part.N, true
	}
	o := e.Game().Obj(id)
	if o == nil || o.Face() == nil {
		return 0, false
	}
	body, present := o.Face().SVars[part.Dyn]
	if !present {
		return 0, false
	}
	ctx := effects.NewCtxPtr(id, you, effects.CtxInit{SVars: o.Face().SVars})
	if tcx != nil {
		ctx.TriggerContext = *tcx
	}
	if len(remembered) > 0 {
		ctx.Remembered = remembered
	}
	n, resolvable := e.Eval().EvalCount(ctx, body)
	if !resolvable || n < 0 {
		return 0, false
	}
	return n, true
}

// UnlessDrawPlayers resolves a Draw<N/Spec> cost component's drawer(s). The
// empty spec and "You" are the payer; every other spelling is one of the
// player roles the unless-payment context carries, and an unresolvable or
// unknown spec fails closed (the cost was not paid).
func UnlessDrawPlayers(ctx *effects.Ctx, payer state.PlayerID, spec string) ([]state.PlayerID, bool) {
	one := func(t state.Target) ([]state.PlayerID, bool) {
		if t.IsPlayer {
			return []state.PlayerID{t.Player}, true
		}
		return nil, false
	}
	switch unlessDrawPlayersCodes.Code(string(spec)) {
	case unlessDrawPlayersPayer:
		return []state.PlayerID{payer}, true
	case unlessDrawPlayersPlayerTargetedBy:
		if len(ctx.Targets) == 0 {
			return nil, false
		}
		return one(ctx.Targets[0])
	case unlessDrawPlayersPlayerActivator:
		return one(ctx.TriggerActivator)
	case unlessDrawPlayersPlayerTriggeredPlayer:
		return one(ctx.TriggerPlayer)
	case unlessDrawPlayersPlayerTriggeredTarget:
		return one(ctx.TriggerTarget)
	}
	return nil, false
}

type unlessDrawPlayersCode uint16

const (
	unlessDrawPlayersPayer unlessDrawPlayersCode = iota + 1
	unlessDrawPlayersPlayerTargetedBy
	unlessDrawPlayersPlayerActivator
	unlessDrawPlayersPlayerTriggeredPlayer
	unlessDrawPlayersPlayerTriggeredTarget
)

var unlessDrawPlayersCodes = state.NewStrCodes(
	state.StrEntry[unlessDrawPlayersCode]{Key: "", Val: unlessDrawPlayersPayer},
	state.StrEntry[unlessDrawPlayersCode]{Key: "You", Val: unlessDrawPlayersPayer},
	state.StrEntry[unlessDrawPlayersCode]{Key: "Player", Val: unlessDrawPlayersPayer},
	state.StrEntry[unlessDrawPlayersCode]{Key: "Self", Val: unlessDrawPlayersPayer},
	state.StrEntry[unlessDrawPlayersCode]{Key: "Player.targetedBy", Val: unlessDrawPlayersPlayerTargetedBy},
	state.StrEntry[unlessDrawPlayersCode]{Key: "Targeted", Val: unlessDrawPlayersPlayerTargetedBy},
	state.StrEntry[unlessDrawPlayersCode]{Key: "TargetedPlayer", Val: unlessDrawPlayersPlayerTargetedBy},
	state.StrEntry[unlessDrawPlayersCode]{Key: "Player.Activator", Val: unlessDrawPlayersPlayerActivator},
	state.StrEntry[unlessDrawPlayersCode]{Key: "TriggeredActivator", Val: unlessDrawPlayersPlayerActivator},
	state.StrEntry[unlessDrawPlayersCode]{Key: "Player.TriggeredPlayer", Val: unlessDrawPlayersPlayerTriggeredPlayer},
	state.StrEntry[unlessDrawPlayersCode]{Key: "TriggeredPlayer", Val: unlessDrawPlayersPlayerTriggeredPlayer},
	state.StrEntry[unlessDrawPlayersCode]{Key: "Player.TriggeredTarget", Val: unlessDrawPlayersPlayerTriggeredTarget},
	state.StrEntry[unlessDrawPlayersCode]{Key: "TriggeredTarget", Val: unlessDrawPlayersPlayerTriggeredTarget},
)

func CloneUnlessCtx(in effects.Ctx) effects.Ctx {
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

// UnlessCostPayable is the ctx-aware offer gate. A cost is offered when every
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
func UnlessCostPayable(e Engine, p state.PlayerID, raw string, ctx *effects.Ctx, stackObj state.ObjID) bool {
	cost, ok := costvocab.ParseUnlessCost(raw)
	if !ok {
		return false
	}
	cost, ok = UnlessFoldDynamic(e, p, cost, ctx)
	if !ok {
		return false
	}
	if ctx == nil {
		ctx = effects.NewCtxPtr(0, p, effects.CtxInit{})
	}
	if !unlessComponentsPayable(e, p, cost, ctx, stackObj) {
		return false
	}
	if !UnlessEnergyAffordable(e, p, cost, ctx) {
		// An energy part is never mana: resolveMana ignores it, so the
		// payable checks below would silently pass an energy the payer cannot
		// cover. Forge CostPayEnergy.canPay reads the same counter total.
		return false
	}
	player := e.Game().Players[p]
	d := Descriptor{ID: stackObj, Class: PurposeOther, Cost: &cost}
	if CostPayableClass(e, p, d, PipRider{}, cost) {
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
	return UnlessManaReachable(e, p, cost, player.Pool, player.Snow, player.ManaUnits(), player.Life,
		e.Eval().Conv(p, stackObj, false), e.Eval().WindowUnits(p))
}

// UnlessFoldDynamic folds the unless cost's DYNAMIC life tokens to concrete
// Life charges against the payer's CURRENT life total, so the offer gate and
// both charge sites (payUnlessCost, advanceUnlessPayment) price the same
// resolved cost. LifeTotalHalfUp (Temporal Extortion) is half the payer's
// life rounded up and unpayable at zero life; an announced PayLife<X> that
// the string resolver could not fold (no resolvable SVar:X body) cannot be
// priced here either — fail closed, the strict-parser convention.
func UnlessFoldDynamic(e Engine, p state.PlayerID, cost costvocab.Cost, ctx *effects.Ctx) (costvocab.Cost, bool) {
	out := cost
	if cost.LifeHalfUp {
		life := e.Game().Players[p].Life
		if life <= 0 {
			return cost, false
		}
		out.Life = costvocab.AddClampedGeneric(out.Life, int64((life+1)/2))
		out.LifeHalfUp = false
	}
	if len(cost.LifeX) > 0 {
		if ctx == nil || !ctx.XAnnounced {
			return cost, false
		}
		for range cost.LifeX {
			out.Life = costvocab.AddClampedGeneric(out.Life, int64(ctx.X))
		}
		out.LifeX = nil
	}
	return out, true
}

// UnlessEnergyAffordable reports whether the payer's energy counter total
// covers the cost's energy parts (CR 118.2d, Forge CostPayEnergy.canPay): a
// fixed part spends its N, the dynamic X part the RESOLVING ability's
// announced X — an X part with no announced X is unaffordable, never free.
// resolveMana ignores energy parts, so this check is the ONLY thing keeping
// an energy cost from being offered (and charged) for free.
func UnlessEnergyAffordable(e Engine, p state.PlayerID, cost costvocab.Cost, ctx *effects.Ctx) bool {
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
	return total == 0 || e.Game().Players[p].Counter("ENERGY") >= total
}

// unlessComponentsPayable checks every non-mana part of an unless cost for
// satisfiability, sharing the pay path's own candidate enumeration
// (unlessCandidatesFor) so the gate can never disagree with
// beginUnlessPayment about what exists. The choice-bearing Sac/Discard/Reveal
// parts must admit a system of distinct representatives (one card cannot pay
// two parts); the synchronous parts (SubCounter/RevealChosen/Draw) are
// checked directly.
func unlessComponentsPayable(e Engine, p state.PlayerID, cost costvocab.Cost, ctx *effects.Ctx, stackObj state.ObjID) bool {
	return unlessChoiceComponentsPayable(e, p, cost, ctx) &&
		UnlessCountersAffordableFor(e, cost, *ctx, stackObj) &&
		UnlessRevealChosenDesignated(e.Game(), cost, *ctx) &&
		unlessDrawsResolvable(e, p, cost, ctx)
}

// unlessChoiceComponentsPayable builds the per-sub-slot candidate lists for
// the choice-bearing parts and runs a bipartite matching: feasible iff a
// system of distinct representatives of the required size exists. A
// whole-zone Exile part (isWholeZoneExileSpec) is not a pick at all -- the
// payment takes EVERY matching candidate -- so it consumes its zone's
// candidates before the ordinary parts are matched, exactly as
// advanceUnlessPayment settles it.
func unlessChoiceComponentsPayable(e Engine, p state.PlayerID, cost costvocab.Cost, ctx *effects.Ctx) bool {
	type slot struct {
		zone state.Zone
		kind string
		part costvocab.CostPart
	}
	var subs []slot
	add := func(zone state.Zone, kind string, parts []costvocab.CostPart) {
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
		if !costvocab.IsWholeZoneExileSpec(part.Spec) {
			continue
		}
		avail := UnlessCandidatesFor(e, p, *ctx, UnlessExileZone(part), "exilecost", part, used)
		if int32(len(avail)) < part.N {
			return false
		}
		used = append(used, avail...)
	}
	for _, part := range cost.Exile {
		if costvocab.IsWholeZoneExileSpec(part.Spec) {
			continue
		}
		for i := int32(0); i < part.N; i++ {
			subs = append(subs, slot{zone: UnlessExileZone(part), kind: "exilecost", part: part})
		}
	}
	if len(subs) == 0 {
		return true
	}
	cands := make([][]state.ObjID, len(subs))
	for i, s := range subs {
		cands[i] = UnlessCandidatesFor(e, p, *ctx, s.zone, s.kind, s.part, used)
	}
	return MaxBipartiteMatch(cands) == len(subs)
}

func unlessDrawsResolvable(e Engine, p state.PlayerID, cost costvocab.Cost, ctx *effects.Ctx) bool {
	for _, part := range cost.Draw {
		if _, ok := UnlessDrawPlayers(ctx, p, part.Spec); !ok {
			return false
		}
	}
	return true
}

// UnlessCandidatesFor is the shared candidate enumeration the offer gate
// (unlessChoiceComponentsPayable) and the payment continuation
// (unlessPaymentCandidates) both use, so the two can never disagree about
// what exists. used lists already-committed picks that must not be offered
// twice; the gate passes the empty list.
func UnlessCandidatesFor(e Engine, payer state.PlayerID, ctx effects.Ctx, zone state.Zone, kind string, part costvocab.CostPart, used []state.ObjID) []state.ObjID {
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
			for _, id := range e.Game().Zone(z, payer) {
				if seen[id] {
					continue
				}
				if z == state.ZBattlefield {
					if o := e.Game().Obj(id); o == nil || !ExistsOnBattlefield(o) {
						continue
					}
				}
				if e.Eval().MatchesSpec(part.Spec, id, sc) {
					out = append(out, id)
				}
			}
		}
		return out
	}
	var out []state.ObjID
	for _, id := range e.Game().Zone(zone, payer) {
		if kind == "sacrifice" && e.CostBlocked(BlockSacrifice, id, CostCauseResolution) {
			// A CantSacrifice restriction (Call for Aid) or face static: the
			// permanent cannot pay a sacrifice component. An unless payment
			// is a resolution-election payment, never a cast/activation cost,
			// so the cause is costCauseResolution -- inadmissible by every
			// readable ValidCause$ base, which scopes the line closed here
			// (the permissive direction) while a bare ForCost$ True and an
			// Effect-registered CantSacrifice still apply.
			continue
		}
		if kind == "exilecost" && zone == state.ZBattlefield && e.CostBlocked(BlockExile, id, CostCauseResolution) {
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
		if kind == "exilecost" && costvocab.IsWholeZoneExileSpec(part.Spec) {
			if !seen[id] {
				out = append(out, id)
			}
			continue
		}
		if !seen[id] && e.Eval().MatchesSpec(part.Spec, id, sc) {
			out = append(out, id)
		}
	}
	return out
}

// UnlessRevealChosenDesignated is the shared RevealChosen check: the offer
// gate (unlessComponentsPayable) and the payment continuation
// (revealChosenDesignated) both require every part's secret designation to be
// present on the resolving source.
func UnlessRevealChosenDesignated(g *state.Game, cost costvocab.Cost, ctx effects.Ctx) bool {
	if len(cost.RevealChosen) == 0 {
		return true
	}
	src := g.Obj(ctx.Source)
	for _, part := range cost.RevealChosen {
		if !HasRevealChosenDesignation(src, part.Spec) {
			return false
		}
	}
	return true
}

// UnlessCountersAffordableFor is the shared SubCounter feasibility check: the
// offer gate and the payment continuation both resolve the draining source the
// same way (an activated ability's host, else the resolving source -- exactly
// payUnlessCost's rule) and require enough counters of each named kind.
func UnlessCountersAffordableFor(e Engine, cost costvocab.Cost, ctx effects.Ctx, stackObj state.ObjID) bool {
	if len(cost.SubCounter) == 0 {
		return true
	}
	src := ctx.Source
	if o := e.Game().Obj(stackObj); o != nil && o.Ability != nil {
		src = o.Source
	}
	o := e.Game().Obj(src)
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
