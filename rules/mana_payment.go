package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// manaLetters is state.Mana's index order (MW, MU, MB, MR, MG, MC) spelled
// out as the WUBRGC symbols events.ManaAdd's Counter field expects.
var manaLetters = [...]string{"W", "U", "B", "R", "G", "C"}

// payMana spends cost from p's pool and reports whether it could. Every
// state mutation goes through events, so payment cannot be a direct field
// write to Players[p].Pool: it emits one ManaAdd event per colour bucket
// actually spent, with a negative Amount. That reuses the existing ManaAdd
// kind rather than adding a new one -- Apply's ManaAdd case is a plain "+=",
// so a negative Amount already subtracts correctly, the same trick Ruling F4
// uses for clearing damage.
//
// Ruling T19b-b: this used to be silent on failure (a no-op the caller could
// not observe), on the theory that legalActions always gates "cast" on
// CanPay first so failure here was unreachable. That theory held for the
// base cost but not for an alternative one: legalActions gates an alt-cost
// option on the ALTERNATIVE cost being payable, so a caller that (as
// castSpell used to) paid the base cost regardless of which option was
// chosen could genuinely hit this path with real, well-formed card data --
// and a silent no-op there is what let the spell go on the stack anyway,
// having paid nothing. Reporting failure explicitly is what lets castSpell
// abort the cast instead.
func (e *Engine) payMana(p state.PlayerID, cost Cost) bool {
	ok, _, _, _, _ := e.payManaDescriptorForSpent(p, paymentDescriptor{class: paymentOther, cost: &cost}, cost, nil, pipRider{})
	return ok
}

// payManaConv is payMana under a stat:ManaConvert conversion set (or nil,
// the plain exact-colour payment payMana always was). The conversion widens
// (and the <-C restriction narrows) what the pool's mana may pay, never what
// the cost demands.
func (e *Engine) payManaConv(p state.PlayerID, cost Cost, conv *manaConv) bool {
	ok, _, _, _, _ := e.payManaDescriptorForSpent(p, paymentDescriptor{class: paymentOther, cost: &cost}, cost, conv, pipRider{})
	return ok
}

func (e *Engine) payManaCumulative(p state.PlayerID, id state.ObjID, cost Cost, conv *manaConv) bool {
	ok, _, _, _, _ := e.payManaDescriptorForSpent(p, paymentDescriptor{id: id, class: paymentCumulativeUpkeep,
		cost: &cost, xAnnounced: cost.X > 0}, cost, conv, pipRider{})
	return ok
}

// payManaConvFor pays a specific spell or activated ability. RestrictValid$
// mana remains distinct from ordinary floating mana until this point: it is
// included only when its restriction admits this payment, then spent first
// and marked on the negative ManaAdd event so events.Apply can reconstruct
// the same provenance during replay.
func (e *Engine) payManaConvFor(p state.PlayerID, id state.ObjID, ability bool, cost Cost, conv *manaConv) bool {
	return e.payManaFor(p, id, ability, cost, conv, pipRider{})
}

// payManaFor is payManaConvFor with the may-play ignore-colour rider passed
// explicitly, so the payment sites that know the cast's recorded rider (a
// pendingCast's mayPlayIgnore, kept from the offer gate that proved it) keep
// the grant after the card has moved to the stack -- at payment time the
// card is no longer in the granted zone, so re-deriving from the zone would
// wrongly drop it.
func (e *Engine) payManaFor(p state.PlayerID, id state.ObjID, ability bool, cost Cost, conv *manaConv, rider pipRider) bool {
	ok, _, _, _, _ := e.payManaForSpent(p, id, ability, cost, conv, rider)
	return ok
}

// payManaForSpent is payManaFor with the payment's actually-spent mana
// returned: the per-colour delta the negative ManaAdd events record (zero on
// a failed payment). FOUR deltas come back: `spentPlain` is the split the
// payment emits (restricted batches already carved off by
// emitRestrictedManaSpend, the delta RememberCostMana$ notes today), `spentAll`
// is the FULL pool delta copied before that split -- the true "all mana spent
// to pay this cost", which converge (CR 107.4f-family) counts from via
// payManaCastSpent -- `spentSnow` is the per-colour count of the spent
// units that were SNOW units (CR 107.4h), the parallel-tally delta, and
// `spentTyped` is the per-tag per-colour count of the spent units that were
// TYPED units (task castfilter2: Treasure/Cave/Desert, the parallel
// Player.TypedMana tally), which the filtered Count$CastTotalManaSpent
// Treasure/Cave/Desert heads read. The RememberCostMana$ payment site
// (Jeweled Amulet) keeps its existing split-based note; every other caller
// keeps the bool-only payManaFor wrapper, so no other payment site changes
// shape.
func (e *Engine) payManaForSpent(p state.PlayerID, id state.ObjID, ability bool, cost Cost, conv *manaConv, rider pipRider) (bool, state.Mana, state.Mana, state.Mana, [7]state.Mana) {
	class := paymentSpell
	if ability {
		class = paymentActivated
	}
	return e.payManaDescriptorForSpent(p, paymentDescriptor{id: id, class: class, cost: &cost}, cost, conv, rider)
}

func (e *Engine) payManaDescriptorForSpent(p state.PlayerID, d paymentDescriptor, cost Cost, conv *manaConv, rider pipRider) (bool, state.Mana, state.Mana, state.Mana, [7]state.Mana) {
	av := e.manaAvailableFor(p, d)
	// The payment's persistence attribution: the visible pool's persistent
	// share (perVis) and its ordinary complement (perFresh). resolveMana is
	// persistence-blind — the units are interchangeable — so attributing the
	// spent units ordinary-first (the exception mana spent last) is a
	// bookkeeping choice the emitted events carry: the split below emits the
	// persistent remainder as a MARKED negative ManaAdd whose " pm" suffix is
	// what moves Player.PersistentMana in events.Apply. Carrying the
	// attribution on the events, rather than letting the fold derive it from
	// the RAW pool (whose fresh share disagrees with this visible pool
	// whenever a restricted batch is hidden from the payment, or the carve
	// consumed the persistent batch first), is what keeps the tally on the
	// units that actually survived a boundary.
	perVis := e.visiblePersistentMana(p, d)
	perFresh := state.Mana{}
	for i := range perFresh {
		perFresh[i] = av.pool[i] - perVis[i]
	}
	before := av.pool
	beforeSnow := e.G.Players[p].Snow
	beforeTyped := av.typed
	pay, ok := cost.resolveManaWith(before, beforeSnow, beforeTyped, e.G.Players[p].Life,
		e.payerGrantsPayLifeInsteadOfB(p), rider, conv)
	if !ok {
		return false, state.Mana{}, state.Mana{}, state.Mana{}, [7]state.Mana{}
	}
	after, afterSnow, afterTyped, lifeSpent := pay.pool, pay.snow, pay.typed, pay.lifeSpent
	spent := state.Mana{}
	spentSnow := state.Mana{}
	spentTyped := [7]state.Mana{}
	for i := range before {
		spent[i] = before[i] - after[i]
		// The parallel tallies' own deltas: how many of the units that left
		// slot i were snow / typed units. resolveManaWith consumes a plain
		// unit before a typed one and a typed one before snow wherever a
		// choice existed, so this is exactly what the payment search did and
		// each tally never exceeds spent[i].
		spentSnow[i] = beforeSnow[i] - afterSnow[i]
		for t := range spentTyped {
			spentTyped[t][i] = beforeTyped[t][i] - afterTyped[t][i]
		}
	}
	// Converge counts ALL mana spent, restricted batches included -- Boseiju's
	// {C} is not a colour, but a Tazri-restricted coloured unit IS the colour
	// it was paid as -- so copy the full delta before emitRestrictedManaSpend
	// carves the restricted batches out of `spent`.
	spentAll := spent
	// emitSnow/emitTyped are the emission split's copies of the tallies.
	// emitRestrictedManaSpend carves the restricted batches out of `spent`
	// and emits their tagged/snow form DIRECTLY, so the tallies driving the
	// remaining split must be carved alongside it -- otherwise a restricted
	// TAGGED unit would be emitted once by the carve and again by the loop
	// (double-decrementing TypedMana and the pool). The returned spentSnow /
	// spentTyped keep the FULL deltas the pay-time capture reads.
	emitSnow := spentSnow
	emitTyped := spentTyped
	e.emitRestrictedManaSpend(p, d, &spent, &emitSnow, &emitTyped, &perVis, &perFresh)
	for i, letter := range manaLetters {
		if spent[i] == 0 {
			continue
		}
		// A slot whose snow / typed units were spent (all or part) emits the
		// "S<colour>" / "<Tag><colour>" Counter forms so the parallel tallies
		// move with the pool through the same events the adds used.
		// resolveMana consumes a plain unit before a typed one and a typed
		// one before snow wherever a choice existed, so the split here is
		// exactly what the payment search did. The emission order (plain,
		// Treasure, Cave, Desert, snow) is fixed and deterministic.
		//
		// Every form is attributed ordinary-first against ONE running fresh
		// share for the slot (the exception mana is spent last): the part of
		// an emission past what is left of perFresh is the persistent
		// remainder and rides a MARKED event, so the fold moves the tally
		// with the units that were actually consumed. The carve above already
		// charged its own consumption against perVis/perFresh by the consumed
		// batch's flag. A TYPED or SNOW unit can be persistent too -- Tanuki
		// Transplanter is an artifact, so its PersistentMana$ G arrives as
		// "ArtifactG" -- and an unmarked typed spend of it left
		// PersistentMana above the emptied pool, which the next payment read
		// as a negative fresh share and over-charged into a negative pool
		// (cardfuzz fuzz-b13).
		fresh := perFresh[i]
		if fresh < 0 {
			fresh = 0
		}
		emitSpend := func(counter string, n int32) {
			if n <= 0 {
				return
			}
			ord := min(n, fresh)
			fresh -= ord
			if ord > 0 {
				e.emit(events.Event{Kind: events.ManaAdd, Player: p, Counter: counter, Amount: -ord})
			}
			if per := n - ord; per > 0 {
				e.emit(events.Event{Kind: events.ManaAdd, Player: p, Counter: counter,
					Amount: -per, Text: events.ManaPersistentText("")})
			}
		}
		snowSpent := emitSnow[i]
		typedSpent := int32(0)
		for t := range emitTyped {
			typedSpent += emitTyped[t][i]
		}
		emitSpend(letter, spent[i]-snowSpent-typedSpent)
		for t, tag := range state.ManaUnitTags {
			emitSpend(tag+letter, emitTyped[t][i])
		}
		emitSpend("S"+letter, snowSpent)
	}
	// Fixed life costs and any Phyrexian pips paid with life are deducted
	// through the ordinary LifeChange event so a replay learns them.
	if lifeSpent != 0 {
		e.emit(events.Event{Kind: events.LifeChange, Player: p, Amount: -lifeSpent})
	}
	return true, spentAll, spent, spentSnow, spentTyped
}

// payManaCastSpent is the spell-cost payment (the shared payManaFor core
// with the cast's recorded may-play ignore-colour rider, CR 401.5's "spend
// mana as though it were mana of any color to cast it") returning the FULL
// spent delta: the pre-restriction-split per-colour pool delta converge
// counts from (task converge1), the snow-unit delta the filtered
// Count$CastTotalManaSpent Snow head reads (task castfilter1), and the
// per-tag typed deltas the filtered Treasure/Cave/Desert heads read (task
// castfilter2). The spell arm's only ask stages have all completed by
// payment, so the deltas ride pendingCast plain data to the pay-time
// CastInfo exactly like replicateTimes does. The rider was proved by the
// offer gate while the card still sat in the granted zone; the payment
// keeps it via pc.mayPlayIgnore because after the push (CR 601.2a) the card
// is on the stack and a zone re-derivation would wrongly drop the grant.
func (e *Engine) payManaCastSpent(pc *pendingCast, cost Cost) (bool, state.Mana, state.Mana, [7]state.Mana) {
	ok, spentAll, _, spentSnow, spentTyped := e.payManaDescriptorForSpent(pc.player, paymentForCast(pc, cost), cost,
		e.paymentConv(pc.player, pc.card, false),
		pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType})
	return ok, spentAll, spentSnow, spentTyped
}

// payExtortPip charges the {W/B} hybrid pip (one mana of either W or B)
// from p's pool, emitting the ManaAdd events so a replay re-derives it. It
// returns false (and charges nothing) when the pool has neither colour, so
// an Extort payment a player genuinely cannot make is a decline rather than
// a free drain.
func (e *Engine) payExtortPip(p state.PlayerID) bool {
	// The pip is charged from the raw pool (the extort window carries no
	// payment id, so the restriction-aware view does not apply here — the
	// pre-existing, restriction-blind consumption is unchanged), but its
	// attribution follows the shared payment rule (ordinary units first): a
	// slot's persistent share is charged only past its non-persistent one,
	// and the persistent unit's event carries the " pm" marker so the fold
	// moves the tally with the unit actually consumed.
	pl := e.G.Players[p]
	fresh := pl.Pool
	per := pl.PersistentMana
	for i := range fresh {
		fresh[i] -= per[i]
	}
	for _, idx := range []int{state.MW, state.MB} {
		if fresh[idx] > 0 {
			e.emit(events.Event{Kind: events.ManaAdd, Player: p,
				Counter: manaLetters[idx], Amount: -1})
			return true
		}
	}
	for _, idx := range []int{state.MW, state.MB} {
		if per[idx] > 0 {
			e.emit(events.Event{Kind: events.ManaAdd, Player: p,
				Counter: manaLetters[idx], Amount: -1,
				Text: events.ManaPersistentText("")})
			return true
		}
	}
	return false
}

// availableMana is the restriction-aware view of a seat's floating mana:
// the pool minus every RestrictValid$ batch this payment cannot use, and the
// typed producer tallies with those same batches removed. A restricted TYPED
// unit (Echoing Cavern's Cave mana) must be invisible in BOTH: takeUnit
// partitions a slot by the typed tally, so a typed unit left visible under
// an unusable restriction could be consumed through that path although the
// filter already hid it from the pool.
type availableMana struct {
	pool  state.Mana
	typed [7]state.Mana
}

type paymentClass uint8

const (
	paymentSpell paymentClass = iota
	paymentActivated
	paymentCumulativeUpkeep
	// paymentOther is a real mana payment (ward, unless-pay, attack costs,
	// triggered costs, etc.) whose caller has no cast or activation
	// descriptor. It must not inherit paymentSpell: bare RestrictValid$ Spell
	// admits only a spell cast, and unknown/unclassified payments fail closed.
	paymentOther
)

type paymentDescriptor struct {
	id    state.ObjID
	class paymentClass
	cost  *Cost
	// xAnnounced records that the payment's cost carried an X the
	// announcement machinery has already folded (Cost.WithX clears Cost.X
	// once a value is chosen, so the normalized cost alone can no longer
	// identify an X payment for the CostContainsX restriction). Raw-cost
	// sites (offer gates, mana-ability activations) get it from paymentFor's
	// own derivation; the post-fold payment sites set it explicitly through
	// paymentForCast. Offer and payment must agree about an X cost, never
	// drift.
	xAnnounced bool
}

func paymentFor(id state.ObjID, ability bool, cost Cost) paymentDescriptor {
	class := paymentSpell
	if ability {
		class = paymentActivated
	}
	return paymentDescriptor{id: id, class: class, cost: &cost, xAnnounced: cost.X > 0}
}

// paymentForCast is paymentFor for a pendingCast's resolved payment cost: the
// X the announcement machinery folded into Generic is still an X component of
// this payment (CostContainsX), so the marker rides pc.cost — the folded cost
// itself has Cost.X == 0 and would read as X-less.
func paymentForCast(pc *pendingCast, cost Cost) paymentDescriptor {
	d := paymentFor(pc.card, pc.isAbility(), cost)
	d.xAnnounced = pc.cost.X > 0
	return d
}

// manaAvailableFor removes every restricted batch from the visible pool, then
// restores exactly the batches valid for this payment. This means a cast or a
// nonmatching activation can never borrow Tazri-style mana merely because it
// shares a colour bucket with unrestricted mana. The typed tallies are
// filtered by the same rule, so a typed restricted unit can never be spent
// through the typed consumption path either. The descriptor carries the real
// payment: a cost-blind descriptor misreads every cost-keyed dotless term
// (CostContainsX, CostContainsC, CantPayGenericCosts), so callers without a
// real cost must say so with Cost{} and stay on the class-only terms.
func (e *Engine) manaAvailableFor(p state.PlayerID, d paymentDescriptor) availableMana {
	pl := e.G.Players[p]
	available := availableMana{pool: pl.Pool, typed: pl.ManaUnits()}
	for _, r := range pl.RestrictedMana {
		idx := state.ManaSlot(r.Color)
		available.pool[idx] -= r.Amount
		// An empty Valid is an UNRESTRICTED batch that carries only its
		// AddsNoCounter$ provenance (Boseiju's plain {C}): it pays anything,
		// exactly like ordinary pool mana, so its units stay visible.
		if r.Valid == "" || e.restrictValidMatches(p, d, r.Valid, r.Source) {
			available.pool[idx] += r.Amount
			continue
		}
		// The batch is unusable here: hide its typed provenance too.
		if tag, slot, ok := state.TypedManaCounter(r.Color); ok {
			available.typed[tag][slot] -= r.Amount
		}
	}
	return available
}

// visiblePersistentMana is the persistent share of p's VISIBLE pool for this
// payment: the PersistentMana tally minus every persistent restriction batch
// this payment cannot use. manaAvailableFor hides an unusable batch's units
// from the payment, so those units cannot be what a spend here consumed and
// must not be attributed to it; the same hiding rule keeps this view and the
// visible pool from disagreeing. (An empty Valid is an unrestricted
// AddsNoCounter batch — spendable anywhere — so its units stay attributed.)
// Measured corpus: every PersistentMana carrier produces plain mana, so the
// persistent share never carries a snow/typed tag in practice.
func (e *Engine) visiblePersistentMana(p state.PlayerID, d paymentDescriptor) state.Mana {
	pl := e.G.Players[p]
	per := pl.PersistentMana
	for _, r := range pl.RestrictedMana {
		if !r.Persistent || (r.Valid != "" && e.restrictValidMatches(p, d, r.Valid, r.Source)) {
			continue
		}
		idx := state.ManaSlot(r.Color)
		if per[idx] < r.Amount {
			per[idx] = 0
		} else {
			per[idx] -= r.Amount
		}
	}
	return per
}

// emitRestrictedManaSpend consumes matching restriction batches in insertion
// order before ordinary mana. Every matching unit is interchangeable for the
// current payment; using this fixed order keeps the log deterministic. When a
// consumed batch carries AddsNoCounter$ provenance and this is a SPELL cast
// payment (never an ability activation), the cast's id is captured in
// e.noCounterSpend for payCast to fold state.FlagNoCounter into the pay-time
// CastInfo — with the batch's own condition evaluated against the paying
// spell's face (Boseiju's !Permanent).
//
// The carve is capped by the units resolveMana's search ACTUALLY attributed to
// the batch's provenance, not merely by the slot's total delta: the search's
// takeUnit consumes a plain unit before a typed one and a typed one before
// snow, so a restricted TAGGED batch beside plain mana of the same colour can
// go entirely unspent even though the slot's delta exceeds its amount. Taking
// the full min(spent[idx], r.Amount) would decrement the tag's emission tally
// below what was spent, driving emitTyped negative; the split loop's
// `plain := spent - snow - typed` subtraction would then be inflated by the
// negative term and the pool would lose more units than the cost required.
// Capping at the tag's (or snow tally's, or the slot's remaining plain units')
// actual spend reconciles the carve's restricted-first attribution with the
// search's plain-first consumption and keeps every emission tally >= 0.
func (e *Engine) emitRestrictedManaSpend(p state.PlayerID, d paymentDescriptor, spent *state.Mana, emitSnow *state.Mana, emitTyped *[7]state.Mana, perVis *state.Mana, perFresh *state.Mana) {
	e.noCounterSpend = 0
	e.manaSpentSources = nil
	e.manaSpentAddsCounters = nil
	// Emit mutates RestrictedMana through events.Apply, so range a snapshot:
	// otherwise removing the first of two matching batches would make the
	// live slice shift under this loop and could skip or double-spend one.
	batches := append([]state.ManaRestriction(nil), e.G.Players[p].RestrictedMana...)
	for _, r := range batches {
		if r.Amount <= 0 || (r.Valid != "" && !e.restrictValidMatches(p, d, r.Valid, r.Source)) {
			continue
		}
		idx := state.ManaSlot(r.Color)
		used := spent[idx]
		if used > r.Amount {
			used = r.Amount
		}
		// Cap by the provenance the search actually spent in this slot. A
		// tagged or snow batch can only carve the units whose parallel tally
		// left the pool; a plain batch only the slot's remaining plain units
		// (spent minus every tally still attributed to this slot).
		if tag, slot, ok := state.TypedManaCounter(r.Color); ok {
			if emitTyped[tag][slot] < used {
				used = emitTyped[tag][slot]
			}
		} else if len(r.Color) == 2 && r.Color[0] == 'S' {
			if s := emitSnow[state.ManaIndex(r.Color[1])]; s < used {
				used = s
			}
		} else {
			plain := spent[idx]
			for t := range emitTyped {
				plain -= emitTyped[t][idx]
			}
			plain -= emitSnow[idx]
			if plain < used {
				used = plain
			}
		}
		if used <= 0 {
			continue
		}
		if r.NoCounter != "" && d.class == paymentSpell && e.noCounterSpend == 0 && addsNoCounterHolds(e.G, d.id, r.NoCounter) {
			e.noCounterSpend = d.id
		}
		// A consumed batch's producing source keys TriggersWhenSpent$.
		// Capture spell and activated-ability payments; paymentOther (including
		// unless-pay) and every other unclassified payment do not dispatch.
		// Dedup keeps one entry per source, in deterministic batch order.
		if (d.class == paymentSpell || d.class == paymentActivated) && r.Source != 0 && !containsObjID(e.manaSpentSources, r.Source) {
			e.manaSpentSources = append(e.manaSpentSources, r.Source)
		}
		// AddsCounters$ (task opalp): a consumed batch that carries the
		// producing ability's rider contributes THIS batch's spent unit count
		// as one grant. The rider is the batch's own snapshot, so a different
		// ability of the same permanent cannot import its rider, and the
		// count is per unit -- two units spent from one rider ability are two
		// grants, never one. The amount is resolved to the producing source's
		// SVar table HERE (cast-payment time), so the stored grant is the
		// cast-time value; a source that changes before the spell enters
		// cannot alter it. A malformed rider or an unresolvable amount is
		// dropped (fail closed), never guessed.
		if (d.class == paymentSpell || d.class == paymentActivated) && strings.TrimSpace(r.AddsCounters) != "" {
			if g, ok := e.addsCounterGrant(r, used); ok {
				e.manaSpentAddsCounters = append(e.manaSpentAddsCounters, g)
			}
		}
		e.emit(events.Event{Kind: events.ManaAdd, Player: p, Counter: r.Color, Amount: -used,
			Text: events.ManaRestrictionText(r.Valid, r.Source)})
		spent[idx] -= used
		// The consumed batch's own flag charges the persistence attribution:
		// a persistent batch's units were the slot's persistent share (the
		// fold moves Player.PersistentMana by the batch flags on this event),
		// an ordinary batch's were the ordinary share. events.Apply reduces
		// the SAME batches in the same insertion order, so the two views of
		// which units were consumed cannot disagree.
		if r.Persistent {
			if (*perVis)[idx] < used {
				(*perVis)[idx] = 0
			} else {
				(*perVis)[idx] -= used
			}
		} else {
			if (*perFresh)[idx] < used {
				(*perFresh)[idx] = 0
			} else {
				(*perFresh)[idx] -= used
			}
		}
		// Carve the consumed units out of the emission split's tallies too:
		// the carve emitted this batch's tagged/snow form directly, so the
		// split loop must not emit it a second time. The caps above guarantee
		// neither tally can go below zero.
		if tag, slot, ok := state.TypedManaCounter(r.Color); ok {
			emitTyped[tag][slot] -= used
		} else if len(r.Color) == 2 && r.Color[0] == 'S' {
			emitSnow[state.ManaIndex(r.Color[1])] -= used
		}
	}
}

// addsCounterGrant resolves one consumed rider batch into the grant payCast
// records: the producing ABILITY's rider parsed into (filter, kind, amount),
// with a non-literal amount looked up in the producing source's SVar table
// NOW (cast-payment time), and used -- how many of the batch's units the
// payment spent -- as the grant's unit count. A malformed rider, an
// unresolvable SVar name or a spent count of zero yields ok=false, so the
// caller drops it (fail closed) instead of placing a guessed counter.
func (e *Engine) addsCounterGrant(r state.ManaRestriction, used int32) (state.ManaAddsCounterGrant, bool) {
	if used <= 0 {
		return state.ManaAddsCounterGrant{}, false
	}
	filter, kind, amount, ok := parseAddsCounters(r.AddsCounters)
	if !ok {
		return state.ManaAddsCounterGrant{}, false
	}
	if _, err := strconv.Atoi(amount); err != nil {
		body := svarBodyForObject(e.G.Obj(r.Source), amount)
		if body == "" {
			return state.ManaAddsCounterGrant{}, false
		}
		amount = body
	}
	return state.ManaAddsCounterGrant{Filter: filter, Kind: kind, Amount: amount, Count: used}, true
}

// containsObjID reports whether id is already in ids (a small linear scan;
// the list holds at most a handful of mana-production sources per cast).
func containsObjID(ids []state.ObjID, id state.ObjID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// addsNoCounterHolds evaluates a consumed batch's AddsNoCounter$ condition
// against the spell being paid for: "True" (or the empty default) always
// holds; "NotPermanent" (Forge's AddsNoCounter$ !Permanent, Boseiju's
// instant-or-sorcery mana) holds when the paying spell is not a permanent
// spell. An unrecognised condition fails closed — no protection.
func addsNoCounterHolds(g *state.Game, id state.ObjID, cond string) bool {
	switch cond {
	case "", "True":
		return true
	case "NotPermanent":
		o := g.Obj(id)
		return o != nil && o.Face() != nil && !o.Face().IsPermanent()
	default:
		return false
	}
}

// restrictValidMatches evaluates RestrictValid$'s payment class. Forge spells
// each term as <SA-kind>.<object filter> and a Valid$ value may name several
// comma-separated terms with OR semantics (Eldrazi Temple's
// "Spell.Eldrazi+Colorless,Activated.Eldrazi+Colorless+inZoneBattlefield",
// Master of Dark Rites' "Spell.Demon,Spell.Cleric,Spell.Vampire"). The zone
// predicate is checked here because it describes the ability's source, not
// the mana source that made the restriction. Unknown classes fail closed so
// restricted mana is never spent illegally. src is the producing permanent's
// id when the batch's event recorded one, so source-relative filter
// predicates (Cavern of Souls' ChosenType) resolve against the mana source;
// source-less batches keep the historical paid-card reading.
func (e *Engine) restrictValidMatches(p state.PlayerID, d paymentDescriptor, valid string, src state.ObjID) bool {
	for term := range strings.SplitSeq(strings.TrimSpace(valid), ",") {
		if e.restrictValidTermMatches(p, d, strings.TrimSpace(term), src) {
			return true
		}
	}
	return false
}

func (e *Engine) restrictValidTermMatches(p state.PlayerID, d paymentDescriptor, term string, src state.ObjID) bool {
	kind, spec, dotted := strings.Cut(term, ".")
	if !dotted {
		switch term {
		case "Spell":
			return d.class == paymentSpell
		case "Activated", "nonSpell":
			return d.class == paymentActivated
		case "CantCastNonArtifactSpells":
			o := e.G.Obj(d.id)
			return d.class == paymentSpell && o != nil && o.Face() != nil && o.Face().IsArtifact()
		case "CantCastSpellFromHand":
			_, ok := e.castProvenanceAdmitsPending("Card.!wasCastFromYourHand", d.id, p)
			return d.class == paymentSpell && ok
		case "CostContainsX":
			// An announced X was folded into Generic (Cost.WithX clears
			// Cost.X), so the descriptor's marker carries it — the cost the
			// payment actually commits still contains an X component.
			return d.cost != nil && (d.cost.X > 0 || d.xAnnounced)
		case "CostContainsC":
			return d.cost != nil && d.cost.Colored[state.ManaIndex('C')] > 0
		case "CantPayGenericCosts":
			// Read the actual resolved payment. Before its X and twobrid faces
			// have been announced, the offer stays open if a colour / X=0 face
			// can be selected; announceFeasible then rechecks the descriptor
			// after that face is folded. Thus {2/W} may use this mana as {W},
			// but not as {2}, and an X spell can choose only X=0 here.
			return d.cost != nil && d.cost.Generic == 0
		case "CumulativeUpkeep":
			return d.class == paymentCumulativeUpkeep
		default:
			return false
		}
	}
	ability := d.class == paymentActivated
	if kind == "Activated" && !ability {
		return false
	}
	if kind == "Spell" && d.class != paymentSpell {
		return false
	}
	if kind != "Activated" && kind != "Spell" {
		return false
	}
	needsBattlefield := strings.Contains(spec, "inZoneBattlefield")
	spec = strings.Trim(strings.ReplaceAll(spec, "+inZoneBattlefield", ""), "+")
	o := e.G.Obj(d.id)
	if o == nil || (needsBattlefield && o.Zone != state.ZBattlefield) {
		return false
	}
	if spec == "" {
		return true
	}
	srcID := d.id
	if src != 0 {
		srcID = src
	}
	// The bare wasCastFromYourHand qualifier (castprov3, Mm'menon's
	// RestrictValid$ Spell.!wasCastFromYourHand — "spend this mana only to
	// cast a spell from anywhere other than your hand"): split the
	// provenance out before the filter match, through the pending-cast
	// variant — the offer-side affordability walk (castable → costPayable)
	// evaluates this read PRE-push, where the object has no cast in the log
	// and the negated spelling would wrongly hold, offering a hand cast as
	// payable on mana the payment then refuses. Off the stack the spec
	// denies (this function's own fail-closed convention: restricted mana is
	// never spent illegally — here it is never even counted); at the payment
	// the spell is on the stack and the read is honest. The term's spec is
	// BASE-LESS here (the "Spell." class was already cut off) and the strip
	// helpers rejoin onto a base, so evaluate the Card.-prefixed form; a
	// surviving alternative whose only predicate was the provenance token
	// rejoins to bare "Card", which MatchesSpecFrom matches like any card.
	var provenanceOK bool
	spec, provenanceOK = e.castProvenanceAdmitsPending("Card."+spec, d.id, p)
	if !provenanceOK {
		return false
	}
	if e.matchesSpecFrom(spec, d.id, p, srcID) {
		return true
	}
	// Forge's object-filter grammar defaults the base to Card, so a bare
	// predicate-only spec -- "Spell.Colorless" (Shrine of the Forsaken
	// Gods), "Spell.MultiColor", the compound "Spell.Eldrazi+Colorless"
	// (Eldrazi Temple) -- means Card.<spec>: a bare type word ("Creature",
	// "Artifact") already evaluates as its own base, but a word the matcher
	// only knows as a QUALIFIER fails closed with no base. Retry with the
	// explicit base before denying the batch: the retry can only turn a
	// "never spendable" batch into the correct evaluation, never widen a
	// spec that already evaluated (the first attempt ran unchanged).
	return e.matchesSpecFrom("Card."+spec, d.id, p, srcID)
}

// paymentConv is the conversion set for p paying id (ability selects the
// ValidSA$ Spell/Activated scoping), or nil when no ManaConvert static would
// change any pip match. Returning nil -- not a zero conv -- keeps the pure
// resolveMana path (and every game without a converter on the board)
// byte-identical.
func (e *Engine) paymentConv(p state.PlayerID, id state.ObjID, ability bool) *manaConv {
	mandatory, optional := e.manaConversionParts(p, id, ability)
	conv := mandatory
	// During an Optional$ ManaConvert cast, the offer-side path uses the union
	// until the election is answered. Thereafter the selected arm is the only
	// one allowed to widen payment; this keeps target affordability, the mana
	// window and the actual charge on one answer.
	if e.cast != nil && e.cast.card == id && e.cast.manaConvertDone {
		if e.cast.manaConvertUse {
			mergeManaConv(&conv, optional)
		}
	} else {
		mergeManaConv(&conv, optional)
	}
	if conv.empty() {
		return nil
	}
	// Copy out only the non-empty conversion: returning &conv directly moved
	// conv to the heap on EVERY call, including the common nil return.
	out := conv
	return &out
}

// costPayableGrant is costPayable with the may-play ignore-colour rider
// passed explicitly, for the payment sites that know the cast's recorded
// rider and cannot re-derive it from the card's zone.
func (e *Engine) costPayableGrant(p state.PlayerID, id state.ObjID, ability bool, cost Cost, rider pipRider) bool {
	// The descriptor carries the REAL cost: an offer gate priced against a
	// descriptor with an empty cost would hide every cost-keyed restricted
	// batch (CostContainsX, CostContainsC) even when the payment itself
	// admits it — the offer and the payment must read the same cost.
	av := e.manaAvailableFor(p, paymentFor(id, ability, cost))
	_, ok := cost.resolveManaWith(av.pool, e.G.Players[p].Snow, av.typed,
		e.G.Players[p].Life, e.payerGrantsPayLifeInsteadOfB(p), rider, e.paymentConv(p, id, ability))
	return ok
}

func (e *Engine) costPayableClass(p state.PlayerID, d paymentDescriptor, rider pipRider, cost Cost) bool {
	return e.costPayableClassLife(p, d, rider, cost, e.payerGrantsPayLifeInsteadOfB(p))
}

// costPayableClassLife is costPayableClass with the payer's
// PayLifeInsteadOf:B grant selected by the caller instead of always derived.
// The CR 601.2g mana-window gate (manaWindowAsk) passes false: that gate asks
// whether the POOL ALONE pays the cost, and a {B} pip the grant would settle
// with 2 life must not answer that question yes -- otherwise the window never
// opens, the payer never gets K'rrik's "may pay 2 life rather than pay that
// mana" choice, and the life is spent silently. Every other caller keeps the
// derived grant (passing true), because a cost only life can pay must still be
// offered and charged as such; the payment itself
// (payManaDescriptorForSpent) also keeps the grant, so a payer who declines
// the window still spends the life.
func (e *Engine) costPayableClassLife(p state.PlayerID, d paymentDescriptor, rider pipRider, cost Cost, lifeGrant bool) bool {
	av := e.manaAvailableFor(p, d)
	_, ok := cost.resolveManaWith(av.pool, e.G.Players[p].Snow, av.typed,
		e.G.Players[p].Life, lifeGrant, rider, e.paymentConv(p, d.id, d.class == paymentActivated))
	return ok
}

// stackXAnnounced reports whether the stack object's cast or activation
// genuinely announced an X (CR 601.2b/107.3i), possibly zero: a nonzero
// recorded value, or the face/ability cost carrying an announce-bearing X
// part (the shared costAnnouncesX census: a printed {X}, an announced
// PayLife<X> or SubCounter<X/Kind>, a dynamic PayEnergy<X> or tapXType<X>).
// A trigger that was never paid an X is NOT announced, even though
// triggerPaidX rebinds a nonzero value for its own readers -- an UnlessCost$
// X on such a body stays unbound, which the conservative direction is.
func stackXAnnounced(o *state.Object) bool {
	if o == nil {
		return false
	}
	if o.X != 0 {
		return true
	}
	if o.Face() != nil && costAnnouncesX(ParseCost(o.Face().ManaCost)) {
		return true
	}
	if o.Ability != nil {
		return costAnnouncesX(ParseCost(o.Ability.Params["Cost"]))
	}
	return false
}

// costPayableOther is the offer-side partner of context-free payMana and
// payManaConv windows. A cost paid outside casting or activating an ability
// must not borrow spell- or activation-restricted mana merely because its
// source happens to be a card object.
func (e *Engine) costPayableOther(p state.PlayerID, id state.ObjID, cost Cost) bool {
	return e.costPayableClass(p, paymentDescriptor{id: id, class: paymentOther, cost: &cost},
		pipRider{anyColor: e.payerGrantsIgnoreColor(p, id), anyType: e.payerGrantsIgnoreType(p, id)}, cost)
}

// costPayable is the conversion-aware equivalent of Cost.payable at the
// offering and window gates: the SAME resolveMana payMana will run, so an
// offered cost and the cost actually charged can never disagree about what
// the payer's converted mana may satisfy. The payer-side grants (a
// PayLifeInsteadOf:B static under its controller; a may-play grant's
// MayPlayIgnoreColor$ rider, derived from the card's current zone) are
// applied here too, so an offered cost and the charged cost agree about a
// K'rrik-shaped or may-play-shaped payment as well.
func (e *Engine) costPayable(p state.PlayerID, id state.ObjID, ability bool, cost Cost) bool {
	return e.costPayableGrant(p, id, ability, cost,
		pipRider{anyColor: e.payerGrantsIgnoreColor(p, id), anyType: e.payerGrantsIgnoreType(p, id)})
}

// costPayablePool is costPayable priced against an EXPLICIT pool instead of
// the seat's restriction-adjusted floating one: pool is the mana the cost
// must resolve against, whatever the seat is actually holding right now. The
// ordinary gates (costPayable here, manaFeasible in statics.go) are exactly
// this with the real manaAvailableFor pool and never call it directly with
// the RAW pool -- offering a cast on mana its RestrictValid$ provenance would
// refuse at payment is the illegal direction (rv2c review: an earlier shape
// of castable did, and was reverted). The only caller is the potential-action
// walk (rules/legal.go legalActionsPriced via castablePriced), which passes
// the hypothetical bound the seat would hold after floating every untapped
// source. The payer grants and conversion shaping are the same reads in both
// modes, so a potential action and the payment it promises can never disagree
// about what the pool may satisfy.

func (e *Engine) costPayablePool(p state.PlayerID, id state.ObjID, ability bool, cost Cost, pool state.Mana, typed [7]state.Mana) bool {
	if !cost.hasPips() {
		// The B-life grant, the may-play riders and the ManaConvert set only
		// ever widen or narrow a PIP's alternatives (costPips, pipAccepts);
		// a pip-free cost -- the bare {T} of nearly every mana ability --
		// resolves to exactly the life and generic totals whatever they
		// are, so the three whole-board reads are skipped, not changed.
		_, ok := cost.resolveManaWith(pool, e.G.Players[p].Snow, typed, e.G.Players[p].Life, false, pipRider{}, nil)
		if walkCacheVerify {
			_, slow := cost.resolveManaWith(pool, e.G.Players[p].Snow, typed, e.G.Players[p].Life,
				e.payerGrantsPayLifeInsteadOfB(p),
				pipRider{anyColor: e.payerGrantsIgnoreColor(p, id), anyType: e.payerGrantsIgnoreType(p, id)},
				e.paymentConv(p, id, ability))
			if slow != ok {
				panic("rules: pip-free costPayablePool fast path disagrees with the full resolve")
			}
		}
		return ok
	}
	_, ok := cost.resolveManaWith(pool, e.G.Players[p].Snow, typed, e.G.Players[p].Life,
		e.payerGrantsPayLifeInsteadOfB(p),
		pipRider{anyColor: e.payerGrantsIgnoreColor(p, id), anyType: e.payerGrantsIgnoreType(p, id)},
		e.paymentConv(p, id, ability))
	return ok
}

// targetBounds resolves a targeting subject's TargetMin$/TargetMax$ to the
// decision's Min/Max. Missing bounds default to 1 (the M1 single-target
// contract: a spell or ability that targets at all targets one thing).
// TargetMin$ 0 is legal -- requirement N2: a spell that MAY target zero
// things resolves untargeted when no legal target exists -- while a negative
// Min or a Max below Min is clamped back to a sane bound (never below 1, so
// a truncated 0 max still asks for at least one). The rule is clamp, not
// ignore: TargetMax$ is parsed whatever it reads (TargetMax$ 0 and even a
// negative TargetMax$ are both taken seriously) and then clamped so
// max >= 1 and max >= min. A discarded parameter and a clamped one both
// resolve to the same number when used alone, but they differ the moment
// TargetMin$ is also present -- this says which one the engine means.
//
// This is the LITERAL reader only. The dynamic forms the corpus writes as
// TargetMax$ X / TargetMin$ X (with SVar:X:Count$...) resolve through
// resolvedTargetBounds below; a token that is not a literal is silently
// dropped here, which is today's (and the unresolvable-fallback's) semantics.
