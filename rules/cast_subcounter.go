package rules

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// xMinAbilityParam is the ONE parse of an ability's XMin$ parameter (task
// cost:xmin-param): a positive integer is the SA's own announcement floor for
// the shared X; anything else (absent, non-numeric, zero, negative) binds
// nothing, exactly as the cost-embedded XMin<N> token fallback does not
// invent a floor. Both floor readers go through it -- xAsk for the option
// range of the ability actually being activated (pcAbility resolves the
// printed, granted or gained SA) and the offer gate for the cheapest legal
// announcement its feasibility composition prices (scope.ab, the same SA the
// offer walk scoped) -- so the offer and the ask can never disagree about
// what the parameter binds.
func xMinAbilityParam(ab *cards.SA) int32 {
	if ab == nil {
		return 0
	}
	n, err := strconv.ParseInt(ab.ParamStr(cards.PKXMin), 10, 32)
	if err != nil || n <= 0 {
		return 0
	}
	return int32(n)
}

// xAsk asks a value for {X} if pc.cost carries one, offering 0..max where
// max is the largest value the mana pool (crediting the best possible
// Delve) can still pay. Runs at most once (xDone).
func (e *Engine) xAsk() bool {
	pc := e.cast
	if pc.xDone {
		return false
	}
	pc.xDone = true
	// The announce-bearing alternative cost (the Shoal cycle's Announce$ X):
	// the announced X is bound by the exile settlement, not by mana — each
	// candidate value is a distinct mana value some exilable card still
	// matches at (altCostXCandidates walks the payer's hand, binding X to
	// each card's own mana value and keeping the matches). No pool bound
	// applies (the alt cost pays no mana X), and the pool-based payable walk
	// below is meaningless for it, so the arm returns straight from the
	// candidate set. An empty set — the hand changed under the offer gate —
	// aborts the cast (CR 733.1, nothing has moved).
	if pc.announceX != "" && len(pc.cost.Exile) > 0 {
		vals := e.altCostXCandidates(pc.player, pc.card, altCostView{
			cost: pc.cost, announce: pc.announceX, src: pc.card})
		if len(vals) == 0 {
			e.abortCast(pc, "announce cost no longer payable; cast aborted", true)
			return true
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: "Choose a value for X", Source: pc.card}
		for _, x := range vals {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "x",
				Label: fmt.Sprintf("X = %d", x), Amount: int(x)})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	// A PayEnergy<X> part announces the same X the cast pays with (CR
	// 107.3i's ability X), so its presence triggers this ask exactly like a
	// printed {X} mana symbol does. A Sac<X/Spec> part announces the count of
	// permanents to sacrifice the same way; announced ExileFromGrave<X/Spec>,
	// PayLife<X> and SubCounter<X/Kind> parts also announce this shared X.
	energyX := false
	for _, part := range pc.cost.Energy {
		if part.Spec == "X" {
			energyX = true
		}
	}
	sacX := false
	for _, part := range pc.cost.Sac {
		if part.Announced {
			sacX = true
		}
	}
	exileX := false
	for _, part := range pc.cost.Exile {
		if part.Announced {
			exileX = true
		}
	}
	subCounterX := false
	for _, part := range pc.cost.SubCounter {
		if part.Announced {
			subCounterX = true
		}
	}
	// A Blight<X> part announces its count as the cast's X exactly the way a
	// Sac<X/Spec> part does (CR 701.60: "blight X" is X -1/-1 counters on a
	// chosen creature, and both corpus carriers spell the announcement
	// `Announce$ X | XMax$ GrTo`). It carries no {X} mana symbol.
	blightX := false
	for _, part := range pc.cost.Blight {
		if part.Announced {
			blightX = true
		}
	}
	discardX := false
	for _, part := range pc.cost.Discard {
		if part.Announced {
			discardX = true
		}
	}
	lifeXCount := len(pc.cost.LifeX)
	if pc.cost.X <= 0 && !energyX && !sacX && !exileX && !subCounterX && !blightX && !discardX && lifeXCount == 0 {
		return false
	}
	min := int32(0)
	// The cost's own announced-X lower bound (XMin<N>, "X can't be 0"):
	// Thieving Skydiver's kicked {X} must be at least 1. Suspend keeps its
	// separate time-X bound; a cost carrying both takes the higher floor.
	if pc.cost.XMin > min {
		min = pc.cost.XMin
	}
	if pc.suspendTimeX && pc.suspendMinX > min {
		min = pc.suspendMinX
	}
	// The active ability's own XMin$ parameter (task cost:xmin-param): an
	// activated ability whose parameters announce a floor for the shared X
	// (Jetfire, Rasputin, Radiant Lotus, Corpseweft all carry XMin$ 1 beside
	// an announced X cost part). It folds by MAXIMUM with the cost-embedded
	// XMin<N> token and the suspend bound above -- three independent carriers
	// of the same CR 601.2b announcement floor -- and resolves through
	// pcAbility, so a granted or has-all-abilities activation reads the
	// ability actually being activated, not a face-wide parameter. A spell
	// cast (pcAbility nil) reads nothing here; a malformed value binds
	// nothing, exactly as the token fallback does not invent a floor.
	if n := xMinAbilityParam(e.pcAbility(pc)); n > min {
		min = n
	}
	pool := e.G.Players[pc.player].Pool
	gy := int32(len(e.G.Zone(state.ZGraveyard, pc.player)))
	// Bound: past this many mana no further X is ever payable. In addition to
	// the pool and possible Delve, the already-announced Convoke/Harmonize
	// payments can cover X's generic requirement. Their exact application
	// below handles coloured costs before generic reductions; this bound need
	// only be a safe finite ceiling.
	credit := int32(0)
	for _, pay := range pc.convoke {
		if pay.power > 0 {
			credit += pay.power
		} else {
			credit++
		}
	}
	bound := pool.Total() + gy + credit + 1
	// A named-announcement discount (the March cycle's "{2} less for each
	// card exiled this way") is a generic reduction the X can spend: the
	// ceiling rises by the composed reduction at the announced count, or an
	// X affordable only through it would never be offered.
	if costHasNamedCount(pc.cost) {
		for _, red := range e.manaToPayXMods(pc, 0).reduces {
			bound = addClampedGeneric(bound, int64(red.generic))
		}
	}
	// A PayEnergy<X> cost part pays the SAME announced X in energy counters
	// (Forge CostPayEnergy.getMaxAmountX bounds a dynamic PayEnergy by the
	// payer's energy total). When the energy part is the ONLY X the cost
	// carries, the mana bound is irrelevant and the bound is exactly that
	// total; when a printed {X} also exists, the energy total still caps it
	// from above -- an X beyond it could be announced but never paid, and
	// CR 601.2b's announcement must be one the payment can settle.
	for _, part := range pc.cost.Energy {
		if part.Spec == "X" {
			energy := e.G.Players[pc.player].Counter("ENERGY")
			if pc.cost.X == 0 {
				bound = energy
			} else if energy < bound {
				bound = energy
			}
		}
	}
	// Without a printed mana X or energy X, the mana-pool ceiling is not
	// relevant. The first announced-count part supplies the ceiling; every
	// subsequent part (including tapXType) min-clamps that same X.
	announcedOnly := pc.cost.X <= 0 && !energyX
	boundSet := false
	applyCap := func(cap int32) {
		if announcedOnly && !boundSet {
			bound, boundSet = cap, true
		} else if cap < bound {
			bound = cap
		}
	}
	// A Sac<X/Spec> part's bound is the number of matching permanents the
	// payer could sacrifice -- announcing a count beyond it could never be
	// settled (CR 601.2b's announcement must be one the payment can settle).
	// When the sac count is the ONLY announced X it IS the bound; when a
	// mana/energy X also exists the candidate count caps it from above.
	for _, part := range pc.cost.Sac {
		if part.Announced {
			avail := int32(len(e.sacrificeCostCandidates(pc.player, pc.card, part, pc.isAbility())))
			applyCap(avail)
		}
	}
	// An X-form tapXType part settles exactly the announced X the same way a
	// Sac<X/Spec> part's count does, so the announcement is bounded by the
	// untapped permanents matching its spec (Necron Overlord's "{X}, tap X
	// untapped artifacts": X beyond the artifact count could be announced but
	// never settled). When the tap part is the ONLY announced X it IS the
	// bound -- but that shape never reaches xAsk at all (the tap election
	// announces it at the tap stage, before xAsk's guard sees no other reason
	// and returns); this cap governs the composed shapes.
	for _, part := range pc.cost.TapPermanent {
		if part.Dyn != "X" {
			continue
		}
		avail := int32(0)
		for _, oid := range e.costCandidates(pc.player, pc.card, state.ZBattlefield, part.Spec, false, true) {
			// The {T} in the same cost claims the source (see tapPermanentCostAsk).
			if pc.cost.Tap && oid == pc.card {
				continue
			}
			avail++
		}
		applyCap(avail)
	}
	// An announced SubCounter<X/Kind> part's bound is the number of counters
	// of that kind the SOURCE actually has (Chandra, Awakened Inferno's
	// SubCounter<X/LOYALTY>: the loyalty the walker has to remove). A filtered
	// fixed-kind part is likewise capped by its largest candidate, but a
	// filtered Any-kind part may remove individual units across candidates, so
	// its cap is their aggregate available counters (Moxite Refinery). An
	// announced PayLife<X> part's bound is the payer's life total divided
	// across the parts (the payer cannot pay more life than they have;
	// paying exactly all of it is legal -- the SBA owns the zero-life
	// consequence). When an announced part is the ONLY X the cost carries it
	// IS the bound -- the pool-based mana ceiling is meaningless without a
	// mana X -- and when another announced X also exists each cap min-clamps
	// the shared X (CR 601.2b's announcement must be one the payment can
	// settle).
	// An announced Discard<X/Spec> (Aether Tide) discards exactly X matching
	// cards, so X is capped by the matching cards in hand.
	for _, part := range pc.cost.Discard {
		if part.Announced {
			applyCap(int32(len(e.discardCandidates(pc.player, pc.card, part, !pc.isAbility(), nil))))
		}
	}
	for _, part := range pc.cost.Exile {
		if !part.Announced {
			continue
		}
		// Use the same source exclusion and zone-order filter as exAsk: a
		// spell cast from this graveyard cannot exile itself as its cost.
		candidates := e.costCandidates(pc.player, pc.card, state.ZGraveyard, part.Spec, !pc.isAbility(), false)
		applyCap(int32(len(candidates)))
	}
	for _, part := range pc.cost.SubCounter {
		if !part.Announced {
			continue
		}
		have := int32(0)
		if subCounterTargetsSource(part.Target) {
			if o := e.G.Obj(pc.card); o != nil {
				have = subCounterAvailable(o, part.Spec)
			}
		} else {
			for _, oid := range e.subCounterRemovalCandidates(pc.player, pc.card, part, 1, nil) {
				if o := e.G.Obj(oid); o != nil {
					n := subCounterAvailable(o, part.Spec)
					if strings.EqualFold(part.Spec, "Any") {
						have += n
					} else if n > have {
						have = n
					}
				}
			}
		}
		applyCap(have)
	}
	if lifeXCount > 0 {
		life := e.G.Players[pc.player].Life
		if life < 0 {
			life = 0
		}
		applyCap(life / int32(lifeXCount))
	}
	if blightX {
		// Blight<X> alone must use the toughness cap as its bound, not the
		// pool/graveyard mana ceiling: blighting does not spend mana.
		// Blight<X>'s own cap: both carriers spell it `XMax$ GrTo` -- "X
		// can't be greater than the greatest toughness among creatures you
		// control" -- so bound the announcement by the greatest toughness
		// among exactly the candidates the payment will choose from. With no
		// matching creature the cap is 0 (the offer gate withholds the cast
		// anyway), and the announcement can never exceed what the blight pick
		// can settle (CR 601.2b).
		cap := int32(0)
		for _, oid := range e.costCandidates(pc.player, pc.card, state.ZBattlefield, "Creature.YouCtrl", false, false) {
			if t := e.Toughness(oid); t > cap {
				cap = t
			}
		}
		applyCap(cap)
	}
	var legal []int32
	maxOld := int32(0)
	// A cost whose announced X feeds a ReduceCost static (Dargo's Sac<X>
	// reading Count$xPaid) does NOT price monotonically in x: the total
	// falls as the reduction grows, so the first unpayable X may be
	// followed by a payable one. The offer gate's affordability sweep
	// accepts exactly such an announcement, so xAsk must offer it too --
	// breaking at the first unpayable x would withhold the only legal
	// announcement and wedge the fetched cast. Every other announced-X
	// cost keeps the early break (generic only grows with x, so nothing
	// past the first unpayable x can be payable).
	nonMonotonic := costAnnouncesPaidX(pc.cost)
	// The target-dependent retry's statics are collected lazily, on the first
	// candidate X the ordinary nil-target price cannot settle, and reused for
	// the rest of the loop.
	var xStatics costStaticViews
	xStaticsLoaded := false
	for x := min; x <= bound; x++ {
		// The offer sweep and the announcement must agree on whether the
		// SAME X can settle every Sac part without reusing an object.
		if sacX && !e.sacrificeCostAssignable(pc.player, pc.card, pc.cost.Sac, pc.isAbility(), x) {
			continue
		}
		var potentialMods costMods
		usedPotential := false
		wx := e.paymentManaX(pc, x)
		wx.Generic -= e.delveCredit(pc.player, pc.card, wx.Generic)
		// The descriptor carries the announced-X marker: WithX folded this
		// payment's X into Generic, and a CostContainsX batch must still see
		// an X payment here or every X announcement would be unpayable.
		payable := e.costPayableClass(pc.player, paymentForCast(pc, wx),
			pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType}, wx)
		if !payable {
			// A target-dependent reduction is absent from the nil-target
			// composition pc.mods carries, so an X affordable only under it
			// (Lullmage's Domination) would be withheld. Retry through the
			// same potential-target composition the offer gate uses; the
			// chosen target is repriced at CR 601.2c before payment, so this
			// only widens the menu to X some legal target can pay.
			if !xStaticsLoaded {
				xStatics = e.collectCostStatics()
				xStaticsLoaded = true
				// A target-dependent reduction does not price monotonically in
				// x: the candidate targets an X legalizes differ per X (a cmc-1
				// creature may qualify where a cmc-2 one does not, and vice
				// versa), so an unpayable X does not imply every larger X is
				// unpayable. Suppress the early break for the rest of the loop
				// exactly as costAnnouncesPaidX does for the Dargo shape.
				if xStatics.validTarget {
					nonMonotonic = true
				}
			}
			if m, ok := e.xTargetPotentialMods(pc, x, xStatics); ok {
				potentialMods = m
				wx = e.paymentManaXUsing(pc, x, m)
				wx.Generic -= e.delveCredit(pc.player, pc.card, wx.Generic)
				payable = true
				usedPotential = true
			}
		}
		if !payable {
			if !nonMonotonic {
				break
			}
			continue
		}
		maxOld = x
		// Every announced contribution must actually reduce this X's cost
		// (convokeAbsorbs against the PRE-contribution total manaToPayX --
		// paymentManaX has already applied them): an announcement
		// over-selected for the generic total cannot be made legal by
		// choosing a small X, so that X is not offered and the larger X
		// that absorbs every creature is. The payable check breaks at the
		// first unpayable X -- generic only grows with x, so everything
		// past it is unpayable too -- while a no-op X is skipped without
		// breaking: absorption improves monotonically with x. Without
		// announced contributions the absorb check is vacuously true, so
		// the offer is exactly the old payable range.
		convMana := e.manaToPayX(pc, x)
		if usedPotential {
			convMana = e.manaToPayXUsing(pc, x, potentialMods)
		}
		if !e.convokeAbsorbs(pc, convMana, pc.convoke, false) {
			continue
		}
		// Waterbend taps pay only the waterbend amount, which the fixed
		// Waterbend<N> parts plus every announced-X form (a RaiseCost
		// Waterbend<X> and/or the cost's own Waterbend<X> part) bound at this
		// candidate X. Reading the announced X here -- not skipping the check
		// whenever the cost carries an X-form part -- is what stops an over-
		// announced tap from being credited against an unrelated generic
		// component (CR 701.67a).
		if waterbendTaps(pc.convoke) > waterbendCap(pc.mods, x) {
			continue
		}
		legal = append(legal, x)
	}
	vals := legal
	if len(vals) == 0 {
		// Either the whole range was unpayable (a proposal the offer gate
		// would have priced differently, or one made directly -- the CR 733
		// audit does exactly that), or every payable X left an announced
		// contribution a no-op. In both, the OLD offer stands and payCast's
		// own payable check aborts as it always did (CR 733.2), rather
		// than this ask wedging or moving the abort site. Every restored X
		// must still satisfy the waterbend cap: an X the loop rejected
		// BECAUSE the announced taps exceed its waterbend amount cannot be
		// resurrected here -- payCast would settle it by letting a tap pay
		// a non-waterbend generic component, which CR 701.67a forbids.
		for x := min; x <= maxOld; x++ {
			if waterbendTaps(pc.convoke) > waterbendCap(pc.mods, x) {
				continue
			}
			vals = append(vals, x)
		}
	}
	if len(vals) == 0 {
		// A LOWER-BOUNDED announcement (Cost.XMin, from an XMin<N> leading
		// token or an X1+/... counter-removal argument) whose every cap sits
		// below the floor: the old offer is empty too (maxOld < min), and the
		// old min==0 paths always kept X=0, so only this new shape can arrive
		// here. No legal announcement exists (CR 601.2b's announcement must
		// be one the payment can settle), and posing a decision with no
		// options would wedge the seat -- so the cast aborts here, the CR
		// 733.2 fail-closed direction, the same site the other
		// no-longer-payable announcements use.
		e.abortCast(pc, "announced X has no payable value; cast aborted", true)
		return true
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Choose a value for X", Source: pc.card}
	for _, x := range vals {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "x",
			Label: fmt.Sprintf("X = %d", x), Amount: int(x)})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// xTargetPotentialMods prices candidate X through the potential-target retry
// the offer gate uses: a ReduceCost static that reads the chosen targets
// (ValidTarget$, a target-conditional ValidSpell$, or a target-relative
// Count$Compare Amount$) is absent from pc.mods, which was priced with no
// targets. Lullmage's Domination is the corpus carrier: its {3} reduction
// hinges on the controller of the creature it targets, so with only {U}{U}{U}
// available X=1 is payable only after a qualifying target is chosen. xAsk runs
// before CR 601.2c, so without this the X menu offered only X=0 and the
// reduction was unusable for nonzero X. The candidate X is bound while the
// potential targets are enumerated so an X-dependent ValidTgts$ (cmcEQX)
// resolves. ok=false means no potential-target composition made X payable, in
// which case the caller keeps the ordinary fail-closed break. The final
// selection is still repriced before payment (repriceForTargets and
// affordableTargetCandidates), so this only widens the MENU to X values some
// legal target choice can pay.
func (e *Engine) xTargetPotentialMods(pc *pendingCast, x int32, statics costStaticViews) (costMods, bool) {
	if !statics.validTarget {
		return costMods{}, false
	}
	scope, ok := e.pendingCastScope(pc)
	if !ok {
		return costMods{}, false
	}
	// Bind the candidate X for the target census: targetSpecContext reads
	// e.cast.x, so an X-bound ValidTgts$ (Creature.cmcEQX) enumerates exactly
	// the targets X would legalize. Restored immediately; pc.x is only
	// assigned the announcement after the decision is answered.
	prevX := pc.x
	pc.x = x
	targets := e.costPotentialTargets(pc.player, pc.card, scope)
	pc.x = prevX
	if len(targets) == 0 {
		return costMods{}, false
	}
	return e.potentialCostModsUsing(statics, pc.player, pc.card, scope, targets, x, func(m costMods) bool {
		w := e.paymentManaXUsing(pc, x, m)
		w.Generic -= e.delveCredit(pc.player, pc.card, w.Generic)
		return e.costPayableClass(pc.player, paymentForCast(pc, w),
			pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType}, w)
	})
}

// delveAsk offers exiling graveyard cards to pay for id's Delve, when id has
// Delve, the caster's graveyard is non-empty and the resolved cost still
// carries a generic requirement to reduce. Runs at most once (delveDone).
// Max is the SHORTFALL -- generic minus what the pool can already pay --
// not the whole generic requirement, so a caster with enough mana is not
// offered (and a bot does not take) exiles the cost does not actually need.
func (e *Engine) delveAsk() bool {
	pc := e.cast
	if pc.delveDone {
		return false
	}
	pc.delveDone = true
	if !e.hasKeywordH(pc.card, kwhDelve) {
		return false
	}
	gy := e.G.Zone(state.ZGraveyard, pc.player)
	cost := e.manaToPay(pc)
	generic := cost.Generic
	if len(gy) == 0 || generic <= 0 {
		return false
	}
	// Max is the SHORTFALL -- generic minus what the pool can already pay --
	// not the whole generic requirement, so a caster with enough mana is not
	// offered (and a bot does not take) exiles the cost does not actually
	// need. Delve only ever covers generic, so the colored requirement is
	// reserved out of the pool first; the rest of the pool can pay at most
	// its total as generic (Cost.Pay's WUBRG spending order never reduces
	// the total it can cover).
	rest := e.G.Players[pc.player].Pool
	for i, n := range cost.Colored {
		if rest[i] < n {
			// Colored unpayable: delve cannot help with it, so the whole
			// generic requirement is the shortfall (the commit stage's own
			// payMana will still fail honestly).
			rest = state.Mana{}
			break
		}
		rest[i] -= n
	}
	payable := generic
	if rest.Total() < payable {
		payable = rest.Total()
	}
	shortfall := generic - payable
	if shortfall <= 0 {
		return false
	}
	max := len(gy)
	if int32(max) > shortfall {
		max = int(shortfall)
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 0, Max: max,
		Prompt: "Delve: exile cards from your graveyard to help cast " + e.G.Obj(pc.card).Face().Name,
		Source: pc.card}
	for _, id := range gy {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "exile",
			Obj: id, Label: e.G.Obj(id).Face().Name})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// subCounterAvailable reports how many counters of the part's kind the object
// could give up: the kind's own count, or the object's TOTAL counter count
// for the "Any" kind (Forge's Any removes that many counters regardless of
// kind). Used by the offer gate, the X bound and the candidate walk.
func subCounterAvailable(o *state.Object, kind string) int32 {
	if strings.EqualFold(kind, "Any") {
		total := int32(0)
		for _, c := range o.Counters {
			if c.N > 0 {
				total += c.N
			}
		}
		return total
	}
	return o.Counter(kind)
}

// subCounterRemovalCandidates lists the permanents the payer could remove
// part's counters from. A source-anchored part (subCounterTargetsSource)
// offers just the source when it still carries enough counters; a filtered
// part offers every battlefield object the PAYER controls that matches the
// target spec (MatchesSpecFrom, the sacAsk machinery's read) and still
// carries enough counters, excluding ids already reserved by an earlier part
// of the same cost (sacs and earlier counter removals).
func (e *Engine) subCounterRemovalCandidates(p state.PlayerID, source state.ObjID, part CostPart, amt int32, reserved map[state.ObjID]bool) []state.ObjID {
	if subCounterTargetsSource(part.Target) {
		if o := e.G.Obj(source); o != nil && o.Zone == state.ZBattlefield &&
			subCounterAvailable(o, part.Spec) >= amt && (reserved == nil || !reserved[source]) {
			return []state.ObjID{source}
		}
		return nil
	}
	var out []state.ObjID
	for _, oid := range e.G.Zone(state.ZBattlefield, p) {
		if reserved != nil && reserved[oid] {
			continue
		}
		o := e.G.Obj(oid)
		if o == nil || subCounterAvailable(o, part.Spec) < amt {
			continue
		}
		if e.matchesSpecFrom(part.Target, oid, p, source) {
			out = append(out, oid)
		}
	}
	return out
}

// subCounterAsk offers the next unsettled SubCounter cost part whose
// removal-target field names something other than the source (walking
// pc.cost.SubCounter in order, pc.subCounterPart). A source-anchored
// fixed-kind part and a zero-count part record nothing: their settle emits on
// the source, the pre-existing behaviour. A wildcard "Any" part -- source-
// anchored or filtered -- records one counter unit per pick (see
// wildcardCounterAsk). The chosen removals are recorded into
// pc.subCounterPays and the counters actually leave the object at payCast,
// exactly like the Sac parts' flow; a part with no remaining candidate aborts
// the whole cast/activation cleanly (sacAsk's unpayable-cost rule -- a cost
// that cannot be fully paid is never committed half paid). A sole candidate
// is recorded without an ask: a decision nobody could answer differently is
// never posed, and the settlement event records the object for replay.
func (e *Engine) subCounterAsk() bool {
	pc := e.cast
	for pc.subCounterPart < len(pc.cost.SubCounter) {
		part := pc.cost.SubCounter[pc.subCounterPart]
		amt := part.N
		if part.Announced {
			amt = pc.x
		}
		if amt <= 0 {
			pc.subCounterPart++
			continue
		}
		reserved := pc.subCounterReservations()
		if strings.EqualFold(part.Spec, "Any") {
			if e.wildcardCounterAsk(pc, part, amt) {
				return true
			}
			continue
		}
		if subCounterTargetsSource(part.Target) {
			pc.subCounterPart++
			continue
		}
		candidates := e.subCounterRemovalCandidates(pc.player, pc.card, part, amt, reserved)
		if len(candidates) == 0 {
			e.abortCast(pc, "counter-removal cost no longer payable; cast/activation aborted", true)
			return true
		}
		if len(candidates) == 1 {
			pc.subCounterPays = append(pc.subCounterPays, subCounterPay{part: pc.subCounterPart, obj: candidates[0]})
			pc.subCounterPart++
			continue
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1, Prompt: "Choose a permanent to remove " + e.subCounterPhrase(part, amt) + " from", Source: pc.card}
		for _, oid := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "subcounter", Obj: oid, Label: e.G.Obj(oid).Face().Name})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

// subCounterReservations keeps an earlier counter-cost part from spending
// the same permanent as the current part. Wildcard units from the current
// part are deliberately absent: wildcardCounterAsk accounts for them by
// (object, kind), allowing the part itself to span objects and kinds.
func (pc *pendingCast) subCounterReservations() map[state.ObjID]bool {
	reserved := map[state.ObjID]bool{}
	for _, s := range pc.sacs {
		reserved[s] = true
	}
	for _, p := range pc.subCounterPays {
		if p.part != pc.subCounterPart {
			reserved[p.obj] = true
		}
	}
	return reserved
}

// wildcardCounterAsk advances a wildcard "Any" SubCounter part by one
// counter unit: it offers the remaining (permanent, counter-kind) units the
// payer may remove, asks when more than one is legal, and records the pick.
// It reports whether the ask loop must pause (a decision was posed); a false
// return means the part is fully paid or has advanced, and the caller's loop
// continues from the (possibly advanced) pc.subCounterPart. A wildcard part
// is settled one unit at a time so a single unit may come from a different
// kind or object than the next, exactly as "remove N counters from among ..."
// reads -- a kind's own count is NOT required to cover the whole amount.
func (e *Engine) wildcardCounterAsk(pc *pendingCast, part CostPart, amt int32) bool {
	picked := int32(0)
	used := map[state.ObjID]map[string]int32{}
	for _, p := range pc.subCounterPays {
		if p.part != pc.subCounterPart {
			continue
		}
		picked++
		if used[p.obj] == nil {
			used[p.obj] = map[string]int32{}
		}
		used[p.obj][p.kind]++
	}
	if picked >= amt {
		pc.subCounterPart++
		return false
	}
	candidates := e.subCounterRemovalCandidates(pc.player, pc.card, part, 1, pc.subCounterReservations())
	if len(candidates) == 0 {
		e.abortCast(pc, "counter-removal cost no longer payable; cast/activation aborted", true)
		return true
	}
	type choice struct {
		obj  state.ObjID
		kind string
	}
	var choices []choice
	for _, oid := range candidates {
		o := e.G.Obj(oid)
		if o == nil {
			continue
		}
		for _, c := range o.Counters {
			if c.N <= 0 {
				continue
			}
			if used[oid] != nil && used[oid][c.Kind] >= c.N {
				continue
			}
			choices = append(choices, choice{oid, c.Kind})
		}
	}
	if len(choices) == 0 {
		e.abortCast(pc, "counter-removal kind no longer payable; cast/activation aborted", true)
		return true
	}
	if len(choices) == 1 {
		pc.subCounterPays = append(pc.subCounterPays, subCounterPay{part: pc.subCounterPart, obj: choices[0].obj, kind: choices[0].kind})
		return false
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1, Prompt: "Choose a counter to remove", Source: pc.card}
	for _, c := range choices {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "subcounter", Obj: c.obj, Counter: c.kind, Label: fmt.Sprintf("%s (%s counter)", e.G.Obj(c.obj).Face().Name, c.kind)})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// subCounterPhrase renders the amount/kind half of a SubCounter part's
// removal for a decision prompt: "A +1/+1 counter", "CHARGE counters", "ANY
// counters". Display only.
func (e *Engine) subCounterPhrase(part CostPart, amt int32) string {
	unit := "counter"
	if amt != 1 && !part.Announced {
		unit = "counters"
	}
	return fmt.Sprintf("%s %s", strings.ToUpper(part.Spec), unit)
}

// settleSubCounterParts emits the SubCounter cost parts' counter removals,
// shared by the payment branches so every path settles the same shape. A
// source-anchored part (subCounterTargetsSource) removes from the paying
// source -- the pre-existing behaviour -- and a filtered part removes from
// the object its recorded subCounterPay carries. The "Any" kind removes one
// counter per recorded pick (each pick carries its chosen kind and object); a
// pending cast with no recorded pick falls back to the object's counter-list
// order, one CounterChange per kind until the amount is met.
func (e *Engine) settleSubCounterParts(pc *pendingCast) {
	for partIdx, part := range pc.cost.SubCounter {
		amt := part.N
		if part.Announced {
			amt = pc.x
		}
		if amt == 0 {
			// A zero removal emits nothing: a CounterChange of 0 would be a
			// no-op folded into state but a spurious log entry.
			continue
		}
		// The picks recorded for THIS part, by explicit part index. A
		// wildcard part has one entry per counter unit (each carrying its
		// chosen kind); a fixed-kind filtered part has one, carrying the
		// object it removes the whole amount from.
		var pays []subCounterPay
		for _, p := range pc.subCounterPays {
			if p.part == partIdx {
				pays = append(pays, p)
			}
		}
		if strings.EqualFold(part.Spec, "Any") {
			if len(pays) == 0 {
				// Legacy fallback for a hand-built/old pending cast with no
				// recorded pick: remove across kinds deterministically from the
				// source. The ordinary flow always records one entry per unit.
				pays = []subCounterPay{{part: partIdx, obj: pc.card}}
			}
			// Group the per-unit picks by (object, kind), preserving first-seen
			// order, so two units of the same kind settle as ONE CounterChange
			// of -2 (the pre-existing shape) while units of different kinds
			// settle as one event each.
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
			if len(order) > 0 {
				continue
			}
			// No kind was recorded (the legacy/fallback entry): remove across
			// kinds in counter-list order.
			o := e.G.Obj(pays[0].obj)
			if o == nil {
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
				take := c.N
				if take > left {
					take = left
				}
				e.emit(events.Event{Kind: events.CounterChange, Obj: pays[0].obj, Counter: c.Kind, Amount: -take})
				left -= take
			}
			continue
		}
		target := pc.card
		if !subCounterTargetsSource(part.Target) {
			if len(pays) == 0 {
				// The ask stage guarantees an entry for every filtered part
				// this settle reaches; a missing one is a flow bug, not a
				// payment to silently skip.
				return
			}
			target = pays[0].obj
		}
		e.emit(events.Event{Kind: events.CounterChange, Obj: target, Counter: part.Spec, Amount: -amt})
	}
}

// sacAsk offers the next unsettled Sac cost part, walking pc.cost.Sac in
// order (pc.sacPart). The chosen sacrifices are excluded from each later
// part's candidates so one permanent can never pay two Sac parts of the
// same cost. castable already required a distinct-candidate assignment
// before this option was ever offered (the fix-round-1 gate), so a part
// with too few candidates here is a board that changed under the flow --
// most directly, an earlier part of the SAME cost consumed the remaining
// matching permanents (an `Sac` part earlier in the same cost, or a board
// that changed under a hand-built intent) that the later part now needs.
// The totality rule is that a cost that cannot be fully paid must not be
// committed with only part of it paid, so rather than skip the part and
// let commitCast validate mana only, such a part aborts the whole cast/
// activation cleanly, exactly as if it was never offered. No sacrifice has
// actually moved yet -- sacAsk only records the choices into pc.sacs; the
// MoveZone events are emitted by commitCast -- so clearing e.cast restores
// the pre-offer board and the Note leaves nothing behind.
func (e *Engine) sacAsk() bool {
	pc := e.cast
	for pc.sacPart < len(pc.cost.Sac) {
		part := pc.cost.Sac[pc.sacPart]
		var candidates []state.ObjID
		for _, oid := range e.sacrificeCostCandidates(pc.player, pc.card, part, pc.isAbility()) {
			already := false
			for _, s := range pc.sacs {
				if s == oid {
					already = true
					break
				}
			}
			if !already && (!pc.emerge || pc.sacPart != 0 || e.emergeSacPayable(pc, oid)) {
				candidates = append(candidates, oid)
			}
		}
		n := int(part.N)
		if part.Announced {
			// Sac<X/Spec>: the announced count (0 = sacrifice nothing -- Dargo's
			// "you MAY sacrifice any number"), already bounded by xAsk to the
			// candidates available then; no priority passes mid-flow, so the
			// board cannot shrink between announcement and this settle.
			n = int(pc.x)
			if n == 0 {
				pc.sacPart++
				pc.sacPaid = 0
				continue
			}
		}
		n -= pc.sacPaid
		if n <= 0 {
			pc.sacPart++
			pc.sacPaid = 0
			continue
		}
		// Continuation feasibility only matters when a LATER Sac part exists:
		// each unit of this part is then asked one at a time, and only the
		// candidates that leave a complete distinct assignment for this part's
		// remaining units and every later part are offered (the one home of
		// the legal-answer rule -- the offered options themselves carry it, so
		// Validate, Clamp and the bot cannot pick a stranding answer). The
		// LAST Sac part has nothing downstream to strand, so it keeps the
		// historical exact-N ask (Min == Max == n) with no feasibility walk --
		// the wire shape TestSacrificedAmountCountsSacrificedObjects,
		// announceSacX and the Sac<X> reduction offers pin (one N-of decision,
		// not N one-of asks).
		hasLaterSac := pc.sacPart+1 < len(pc.cost.Sac)
		if hasLaterSac {
			pools := make([][]state.ObjID, len(pc.cost.Sac))
			needs := make([]int, len(pc.cost.Sac))
			for i, futurePart := range pc.cost.Sac {
				needs[i] = int(futurePart.N)
				if futurePart.Announced {
					needs[i] = int(pc.x)
				}
				if i == pc.sacPart {
					needs[i] -= pc.sacPaid
				}
				pools[i] = e.sacrificeCostCandidates(pc.player, pc.card, futurePart, pc.isAbility())
			}
			candidates = feasibleSacrificeChoices(candidates, pools, needs, pc.sacs, pc.sacPart)
		}
		if n <= 0 || n > len(candidates) {
			// A cost that can no longer be fully paid must not commit half
			// paid (fix round 1, reviewer Important 1). Abort the whole
			// thing; nothing has moved yet.
			//
			// This site used to hand-roll the teardown (clear e.cast, emit the
			// Note) on the reasoning that it was "unreachable from a
			// well-formed offer after the castable gate". It was reachable,
			// and hand-rolling it is what made that reachability unbounded
			// rather than merely wasteful: abortCast is where a no-progress
			// abort holds the option out of the rest of the priority window
			// (suppress=true), and skipping it meant the identical board
			// re-offered the identical doomed cast forever. A live 4-player
			// game sat on turn 3 doing that until it was killed.
			//
			// Route through abortCast like every other unpayable-cost abort,
			// so this path gets the same liveness guarantee the Delve decline
			// has (see cast_liveness_test.go): the suppression lifts on the
			// first state-changing event, which is exactly when a retry could
			// succeed.
			e.abortCast(pc, "sacrifice cost no longer payable; cast/activation aborted", true)
			return true
		}
		// CARDNAME and NICKNAME are bare source-object references in Forge
		// sacrifice costs. When the source is their sole candidate, this exact
		// one-object payment has no player choice: record it and settle the next
		// cost part instead of posing a KChoose the player can only answer one
		// way. The candidate check above deliberately stays first, so a source
		// that has left the battlefield still takes the ordinary unpayable-cost
		// abort path.
		if part.N == 1 && len(candidates) == 1 && candidates[0] == pc.card &&
			(strings.EqualFold(part.Spec, "CARDNAME") || strings.EqualFold(part.Spec, "NICKNAME")) {
			pc.sacs = append(pc.sacs, pc.card)
			pc.sacPart++
			pc.sacPaid = 0
			continue
		}
		verb := "cast"
		if pc.isAbility() {
			verb = "activate"
		}
		// The wire shape: a part with a later Sac part is paid one unit per
		// decision (Min == Max == 1, every option individually feasibility-
		// filtered); the last Sac part is one exact-N decision, as before the
		// continuation work -- nothing downstream can be stranded by its
		// answer, so no validator rule is needed for it.
		dmin, dmax := n, n
		if hasLaterSac {
			dmin, dmax = 1, 1
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: dmin, Max: dmax,
			Prompt: "Sacrifice a permanent to " + verb + " " + e.G.Obj(pc.card).Face().Name,
			Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "sacrifice",
				Obj: id, Label: e.G.Obj(id).Face().Name})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	// Every Sac part is settled: an Emerge cast (CR 702.118a) now folds the
	// chosen sacrifice's mana value out of pc.cost, before the mana window and
	// payment read it. Idempotent through pc.emergeDone, so the re-entries a
	// suspended mana window makes cannot subtract twice.
	e.applyEmergeReduction(pc)
	return false
}

// discardAsk settles Discard cost parts from the payer's hand. Ordinary
// specs pose the same exact-N KChoose used by sacrifice costs. Random parts
// consume the engine's seeded RNG and never ask the player; a Hand spec records
// every remaining hand card without asking. Nothing moves until commitCast,
// so an abort cannot leave a partially paid cost on the board.
func (e *Engine) discardAsk() bool {
	pc := e.cast
	for pc.discardPart < len(pc.cost.Discard) {
		part := pc.cost.Discard[pc.discardPart]
		reserved := make(map[state.ObjID]bool, len(pc.discards))
		for _, id := range pc.discards {
			reserved[id] = true
		}
		candidates := e.discardCandidates(pc.player, pc.card, part, !pc.isAbility(), reserved)

		if strings.EqualFold(part.Spec, "Hand") {
			pc.discards = append(pc.discards, candidates...)
			pc.discardPart++
			continue
		}
		n := int(part.N)
		if part.Announced {
			// The announced Discard<X/Spec>: exactly the announced X, and
			// X = 0 discards nothing.
			n = int(pc.x)
			if n == 0 {
				pc.discardPart++
				continue
			}
		}
		if n <= 0 || n > len(candidates) {
			e.abortCast(pc, "discard cost no longer payable; cast/activation aborted", true)
			return true
		}
		if strings.EqualFold(part.Spec, "Random") {
			for i := 0; i < n; i++ {
				pick := e.Rand(len(candidates))
				pc.discards = append(pc.discards, candidates[pick])
				candidates = append(candidates[:pick], candidates[pick+1:]...)
			}
			pc.discardPart++
			continue
		}

		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: n, Max: n,
			Prompt: "Discard a card to pay the cost of " + e.G.Obj(pc.card).Face().Name,
			Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "discard",
				Obj: id, Label: e.G.Obj(id).Face().Name})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}
