package rules

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
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
func targetBounds(sa *cards.SA) (int, int) {
	min, max := 1, 1
	if v, ok := sa.Params["TargetMin"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			min = n
		}
	}
	if v, ok := sa.Params["TargetMax"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			max = n
		}
	}
	if min < 0 {
		min = 1
	}
	if max < 1 {
		max = 1
	}
	if max < min {
		max = min
	}
	return min, max
}

// isLiteralBound reports whether a raw TargetMin$/TargetMax$ token is a
// plain signed integer.
func isLiteralBound(v string) bool {
	_, err := strconv.Atoi(strings.TrimSpace(v))
	return err == nil
}

// targetBoundsDynamic reports whether either bound token is present and not
// a literal -- the only shape resolvedTargetBounds does extra work for, so a
// literal-only script never leaves the byte-identical fast path. (The two
// reads are spelled out rather than looped over the keys so the paramcensus
// rot guard sees two static Params keys.)
func targetBoundsDynamic(sa *cards.SA) bool {
	if v, ok := sa.Params["TargetMin"]; ok && !isLiteralBound(v) {
		return true
	}
	if v, ok := sa.Params["TargetMax"]; ok && !isLiteralBound(v) {
		return true
	}
	return false
}

// targetBoundCtx binds the effects numeric grammar to the asking player and
// the target declaration's source. The anchor follows what source IS: for a
// spell the stack object IS the card, so its face carries the SVar table
// (Kiora's Dismissal's SVar:X); for an ability or trigger wrapper
// (o.Card == nil, so Face() returns nil) the anchor is o.Source -- the
// source permanent every TriggerPush/AbilityPush stamps -- and ITS face's
// SVar table. No anchor (the object gone, or a sourceless wrapper) fails
// closed to the literal reader.
func (e *Engine) targetBoundCtx(p state.PlayerID, source state.ObjID) (*effects.Ctx, bool) {
	o := e.G.Obj(source)
	if o == nil {
		return nil, false
	}
	ctx := &effects.Ctx{Controller: p}
	// A trigger's dynamic target bound reading the causing event (Vitality
	// Hunter's `TargetMax$ MaxTgts` with `SVar:MaxTgts:TriggerCount$Amount`,
	// task agent-20260919T190014Z): the trigger context recorded for this
	// stack wrapper carries TriggerAmount, so the bound reads the mark/damage
	// magnitude instead of degrading to the clamp's 1. Measured corpus: the
	// ONLY two TargetMax$ TriggerCount$Amount shapes (one inline, one behind
	// the MaxTgts SVar name) are Vitality Hunter's; every other dynamic bound
	// names a Count$ body targetBoundCtx's SVar table already resolves.
	if tc, ok := e.triggerContexts[source]; ok {
		ctx.TriggerContext = tc
	}
	// The pending cast's own multikicker count (rules/cast.go's multikickAsk):
	// at the CR 601.2c announcement ask the pay-time CastInfo has not run
	// yet, so a TimesKicked bound (Comet Storm's TargetMin/Max$ TargetsNum)
	// would read 0 off the stack object. When the asking source IS the card
	// the pending cast is casting, seed the count the ask just settled --
	// exactly the `x` resolvedTargetBounds threads for a Count$xPaid bound.
	if pc := e.cast; pc != nil && pc.card == source {
		if pc.multikickSet {
			ctx.TimesKicked = pc.multikickTimes
		}
		// The CHOSEN cast mode's kicked bit (Tear Asunder's kicked main SA is
		// TargetMin$ X | TargetMax$ X over SVar:X:Count$Kicked.0.1): the same
		// pre-payment gap TimesKicked closes, for the FlagKicked half. The
		// mode was settled when the cast OPTION was picked, before this ask.
		ctx.PendingKicked = modeIsKicked(pc.mode)
	}
	if f := o.Face(); f != nil {
		ctx.Source = source
		effects.SetSVars(ctx, f.SVars)
		return ctx, true
	}
	src := e.G.Obj(o.Source)
	if src == nil {
		return nil, false
	}
	if f := src.Face(); f == nil {
		return nil, false
	}
	ctx.Source = o.Source
	// A mutated pile's under-card triggered ability (CR 702.140d) reads its
	// OWN face's SVar table, not the pile's top card's: Archipelagoe and
	// Nethroi, Apex of Death both bound their targeting with TargetMax$ X,
	// and the pile's top card can be any creature (with no X at all). The
	// owning face comes from the compiled trigger pointer; an ordinary
	// trigger's owning face is the top face, so nothing else moves. A
	// HAS-ALL-ABILITIES-OF wrapper (r3) is covered inside the recovery
	// functions themselves, so every caller shares the one read.
	if owned, ok := e.triggerLineSVars[source]; ok {
		effects.SetSVars(ctx, owned)
	} else if _, mf, ok := e.findTriggerForAbilityFace(o.Source, o.Ability); ok && mf != nil {
		effects.SetSVars(ctx, mf.SVars)
	} else if mf, ok := e.pileFaceForSA(o.Source, o.Ability); ok && mf != nil {
		// An activated ability of a MUTATED pile (CR 702.140d): the ask's SVar
		// bounds (TargetMin$/TargetMax$ X) resolve against the under-card's own
		// table, the same owning-face rule resolveTop's ability branch applies.
		effects.SetSVars(ctx, mf.SVars)
	} else {
		effects.SetSVars(ctx, src.Face().SVars)
	}
	return ctx, true
}

// resolvedTargetBounds is targetBounds extended to the dynamic bounds the
// corpus writes as TargetMax$ X / TargetMin$ X with an SVar body (212 raw
// TargetMax$ X lines / 209 files, 102 TargetMin$ X lines / 100 files -- the
// dominant shape is TargetMin$ 0 + TargetMax$ X, "return any number up to
// X"). A bound token that is a plain literal keeps targetBounds' reading
// byte-for-byte; a token that is PRESENT and not a literal resolves through
// the effects numeric grammar (NumResolved: an SVar name, an inline
// Count$/... expression, or the bare X bound to ctx.X), bound to the asking
// player and the source anchor targetBoundCtx builds. A present token the
// grammar cannot resolve (TargetMax$ Y, the MaxTgts family, a named SVar the
// face does not define) keeps today's semantics -- the parameter is dropped
// to the default 1 -- because Num's degrade-to-zero contract is correct for
// an effect amount ("the card did nothing") but wrong for a mandatory
// target MINIMUM (a TargetMin$ X degrading to 0 would let a mandatory spell
// resolve untargeted). x is the cast's settled {X} (pc.x at the CR 601.2c
// announcement ask, where the announce has already run) so a
// SVar:X:Count$xPaid bound reads the paid value; at a placement ask no X
// applies (a trigger was never paid an X) and 0 is correct there -- an
// xPaid body still finds the cast's value on the source permanent via
// count.go's provenance fallback. The clamp contract is targetBounds',
// applied AFTER resolution: min >= 0, max >= 1, max >= min.
func (e *Engine) resolvedTargetBounds(p state.PlayerID, source state.ObjID, sa *cards.SA, x int32) (int, int) {
	return e.resolvedTargetBoundsWithGift(p, source, sa, x, nil)
}

// resolvedTargetBoundsWithGift is the offer-gate variant: a non-nil promise
// override evaluates Count$PromisedGift as though that Gift election had been
// made, without changing the source object or the normal post-election reader.
func (e *Engine) resolvedTargetBoundsWithGift(p state.PlayerID, source state.ObjID, sa *cards.SA, x int32, promised *bool) (int, int) {
	min, max := targetBounds(sa)
	if !targetBoundsDynamic(sa) {
		return min, max
	}
	ctx, ok := e.targetBoundCtx(p, source)
	if !ok {
		return min, max
	}
	ctx.X = x
	ctx.PromisedGiftOverride = promised
	if v, ok := sa.Params["TargetMin"]; ok && !isLiteralBound(v) {
		if n, resolved := effects.NumResolvedStrict(e, ctx, sa, "TargetMin", 1); resolved {
			min = int(n)
		}
	}
	resolvedMax := false
	if v, ok := sa.Params["TargetMax"]; ok && !isLiteralBound(v) {
		if n, resolved := effects.NumResolvedStrict(e, ctx, sa, "TargetMax", 1); resolved {
			max = int(n)
			resolvedMax = true
		}
	}
	if min < 0 {
		min = 1
	}
	if max < 0 {
		max = 0
	}
	// A dynamic bound the grammar RESOLVED is honoured as written, zero
	// included. The "instead" idiom writes exactly that: Tear Asunder's
	// kicked main SA is TargetMin$ X | TargetMax$ X over
	// SVar:X:Count$Kicked.0.1, meaning "target nothing here, the chained sub
	// (Condition$ Kicked, SVar:Y:Count$Kicked.1.0) does the work". Clamping
	// that resolved 0 up to 1 asks for an artifact/enchantment the kicked
	// spell must not exile. Only an UNRESOLVED token -- and a literal, already
	// clamped by targetBounds -- keep the documented max >= 1 clamp; the
	// max < min clamp below still lifts a resolved 0 when a genuine minimum
	// is present (a bare TargetMax$ X announced 0, min defaulting to 1).
	if !resolvedMax && max < 1 {
		max = 1
	}
	if max < min {
		max = min
	}
	return min, max
}

// resolvedTargetMin is the Min half of resolvedTargetBounds, for resolveTop's
// N2 gate -- an ability or spell that MAY target zero things (a resolved
// TargetMin$ 0) and has none recorded resolves untargeted rather than
// fizzling. An unresolvable dynamic Min keeps the literal reader's default 1,
// so the N2 exemption never opens for a bound this build cannot price.
func (e *Engine) resolvedTargetMin(p state.PlayerID, source state.ObjID, sa *cards.SA, x int32) int {
	min, _ := e.resolvedTargetBounds(p, source, sa, x)
	return min
}

// targetZones resolves TgtZone$ (comma-separated) and TargetType$ into the
// zones to search for target options. TgtZone$ is the explicit zone
// declaration; TargetType$ (Forge) names the KIND of thing targeted, and
// when it names a stack object -- Spell, Instant, Sorcery, Activated,
// Triggered, SpellAbility -- the target lives on the stack. Mana Leak is
// scripted `TargetType$ Spell | ValidTgts$ Card` with NO TgtZone$ at all, so
// without reading TargetType$ the search defaulted to the battlefield and
// offered every permanent of every seat for a counterspell -- the bug this
// fixes (every counterspell in the corpus was inert, targeting a permanent
// so effCounter found o.Zone != state.ZStack and did nothing). The default
// remains the battlefield. An unknown TgtZone$ token is dropped (reviewer
// minor 4), but the battlefield default applies only when NEITHER source
// named a zone -- a typo'd TgtZone$ on a TargetType$ Spell card must not
// silently widen a stack target back to the battlefield.
//
// fb-20260916T024739Z-b89aea46: the remaining default is wrong for one more
// shape -- the Wrenn and Six ability (`AB$ ChangeZone | Origin$ Graveyard |
// Destination$ Hand | TargetMin$ 0 | TargetMax$ 1 | ValidTgts$ Land.YouOwn`,
// no TgtZone$). Its census searched the battlefield, offered a land already
// in play, and omitted the eligible graveyard card the prompt names -- and
// the offered battlefield land could never pass effChangeZone's own Origin$
// Graveyard resolution guard, so the ability could not do what it promises.
// For that one unambiguous shape the Origin$ implies the target zone; see
// originImpliedTargetZone for the four gates that admit it.
func targetZones(sa *cards.SA) []state.Zone {
	var zones []state.Zone
	for z := range strings.SplitSeq(sa.Params["TgtZone"], ",") {
		switch strings.TrimSpace(z) {
		case "Battlefield":
			zones = appendUniqueZone(zones, state.ZBattlefield)
		case "Graveyard":
			zones = appendUniqueZone(zones, state.ZGraveyard)
		case "Hand":
			zones = appendUniqueZone(zones, state.ZHand)
		case "Exile":
			zones = appendUniqueZone(zones, state.ZExile)
		case "Stack":
			zones = appendUniqueZone(zones, state.ZStack)
		}
	}
	// A stack-targeting TargetType$ adds the stack even when no TgtZone$ is
	// present (the counterspell shape) and even alongside a TgtZone$
	// Battlefield for a spell-or-permanent effect (TgtZone$ Stack,Battlefield).
	if targetsStackObjects(sa.Params["TargetType"]) {
		zones = appendUniqueZone(zones, state.ZStack)
	}
	if len(zones) == 0 {
		// The graveyard-enchant Aura family (Animate Dead, Dance of the Dead;
		// Spellweaver Volute for instants) casts its kw:Enchant attach spell
		// (`SP$ Attach | ValidTgts$ Creature.inZoneGraveyard`, no TgtZone$, API
		// Attach with no Origin$, so the Origin$ route below cannot fire) and
		// the default battlefield census offered no candidate and withheld the
		// cast entirely. The inZone<X> words in the comma-split ValidTgts$
		// alternatives name the census zones directly. Deliberately narrow, the
		// same shape as the fb-20260916 Origin$ precedent below: Attach-only,
		// inZone-words-only -- every other API keeps its existing zone
		// resolution, so the ~37 corpus specs carrying non-battlefield inZone<X>
		// outside this API are untouched. An explicit Origin$ outranks this
		// inference even when the two declarations disagree.
		if z, ok := originImpliedTargetZone(sa); ok {
			zones = []state.Zone{z}
		} else if sa.API == "Attach" {
			if zs, ok := attachValidTgtsZones(sa.Params["ValidTgts"]); ok {
				zones = zs
			}
		}
		if len(zones) == 0 {
			// Without an explicit zone, a ValidTgts$ stack-object kind
			// targets the stack; otherwise the battlefield remains the
			// default. An explicit TgtZone$ whose tokens were unknown must
			// not silently widen a target back to the stack.
			if sa.Params["TgtZone"] == "" && targetsStackObjects(sa.Params["ValidTgts"]) {
				zones = []state.Zone{state.ZStack}
			} else {
				zones = []state.Zone{state.ZBattlefield}
			}
		}
	}
	return zones
}

// attachValidTgtsZones is targetZones' Attach-scoped zone inference: the
// zones the comma-split ValidTgts$ alternatives' inZone<X> words name (the
// same word classifier the filter tier's wordInZone uses). It reports false
// when no alternative names a zone, leaving targetZones' existing fallbacks
// (stack kinds, then the battlefield default) in charge.
func attachValidTgtsZones(spec string) ([]state.Zone, bool) {
	var zones []state.Zone
	for alt := range strings.SplitSeq(spec, ",") {
		for word := range strings.SplitSeq(alt, ".") {
			z, has := strings.CutPrefix(strings.TrimSpace(word), "inZone")
			if !has {
				continue
			}
			if zn, ok := effects.ParseZoneWord(z); ok {
				zones = appendUniqueZone(zones, zn)
			}
		}
	}
	return zones, len(zones) > 0
}

// originImpliedTargetZone reports the implicit target zone for a ChangeZone
// or Attach with an explicit Origin$. For Attach it only arbitrates against
// an inZone<X> ValidTgts$ inference: a single concrete Origin$ wins over the
// conflicting inferred zone. ChangeZone remains limited to the established
// unambiguous public-graveyard object-targeted shape: extending it to
// Hand/Library/Exile needs hidden-information and mixed-zone semantics that
// this does not establish (Origin$ Hand's mixed multi-zone handling lives in
// effects/zone.go). It admits an SA when ALL of these hold:
//
//  1. it is API$ ChangeZone, or Attach with inZone<X> ValidTgts$;
//  2. it has no explicit TgtZone$ (explicit TgtZone$ stays authoritative;
//     this helper only runs from targetZones' empty fallback, but a TgtZone$
//     whose tokens were all unknown must not silently fall through to Origin$
//     either) and no stack-targeting TargetType$;
//  3. effects.ParseZones parses its Origin$ as exactly one concrete zone
//     (Graveyard only for ChangeZone) -- not Any/All, not an unknown token,
//     not a multi-zone origin (ParseZones' ok=false fails closed);
//  4. its ValidTgts$ is object-only under the existing targetsPlayers
//     classifier, so a player-targeted ChangeZone keeps its existing
//     player-target route untouched.
//
// The zone feeds both legalTargetCandidates (offer time) and legalTargets
// (the CR 608.2b resolution recheck) through their shared targetZones calls,
// and effChangeZone's own Origin$ guard -- unchanged -- then accepts the
// chosen graveyard object at resolution.
func originImpliedTargetZone(sa *cards.SA) (state.Zone, bool) {
	if sa.API != "ChangeZone" {
		if sa.API != "Attach" {
			return 0, false
		}
		if _, ok := attachValidTgtsZones(sa.Params["ValidTgts"]); !ok {
			return 0, false
		}
	}
	if sa.Params["TgtZone"] != "" || targetsStackObjects(sa.Params["TargetType"]) {
		return 0, false
	}
	if targetsPlayers(sa.Params["ValidTgts"]) {
		return 0, false
	}
	zones, all, ok := effects.ParseZones(sa.Params["Origin"])
	if !ok || all || len(zones) != 1 || (sa.API == "ChangeZone" && zones[0] != state.ZGraveyard) {
		return 0, false
	}
	return zones[0], true
}

// appendUniqueZone appends z to zones when it is not already present,
// preserving the deterministic TgtZone$/TargetType$ order both askTarget and
// legalTargets share (never a map, so no map iteration order reaches a wire
// decision).
func appendUniqueZone(zones []state.Zone, z state.Zone) []state.Zone {
	for _, existing := range zones {
		if existing == z {
			return zones
		}
	}
	return append(zones, z)
}

// targetsStackObjects reports whether a Forge TargetType$ or ValidTgts$
// value names a target that lives on the stack: a spell (Spell/Instant/
// Sorcery), or an activated/triggered/spell-ability object. The shared state
// parser keeps this census aligned with stack target-kind legality, including
// Forge's Ability alias.
func targetsStackObjects(spec string) bool {
	for token := range strings.SplitSeq(spec, ",") {
		if _, ok := state.StackKindTokenOf(strings.TrimSpace(token)); ok {
			return true
		}
	}
	return false
}

// stackObjKind classifies one stack object for TargetType$ legality. The
// classifier moved to state.StackKindOf -- effects' Defined$ ValidStack arm
// must admit exactly what this census admits and cannot import rules, so one
// shared classifier serves both (state.StackKindOf's doc). The type alias
// and the three kind constants keep every existing rules-side name valid.
type stackObjKind = state.StackObjKind

const (
	stackSpell     = state.StackKindSpell     // a card object (a Face) on the stack
	stackActivated = state.StackKindActivated // an ability object minted by AbilityPush
	stackTriggered = state.StackKindTriggered // an ability object minted by TriggerPush/DelayedPush
)

func (e *Engine) stackObjKind(o *state.Object) stackObjKind { return state.StackKindOf(e.G, o) }

// stackTargetOptionKind maps the engine's stack-object classifier to the
// public target-option kind. Keep this aligned with view.StackView.Kind so a
// stack target is not mislabeled as a battlefield permanent on the wire.
func stackTargetOptionKind(k stackObjKind) string {
	switch k {
	case stackSpell:
		return "spell"
	case stackTriggered:
		return "trigger"
	case stackActivated:
		return "ability"
	default:
		return "spell"
	}
}

// targetTypeToken is state.StackKindToken: one comma-separated TargetType$
// token -- which stack object kinds its base admits, the controller qualifier
// read off the qualifiers after the base ("YouCtrl" -- controller must be the
// chooser; "OppCtrl" -- controller must not be; e.g. Weaver of Harmony's
// `Activated.YouCtrl,Triggered.YouCtrl`, Kang Dynasty's `Spell.OppCtrl`), and
// the card-type restriction the base or a qualifier can impose -- the bases
// "Instant" and "Sorcery" (Spider Sense's `Instant,Sorcery,Triggered`) and the
// Spell qualifiers "Instant"/"Sorcery" (Sister of Silence's
// `Spell.Instant,Spell.Sorcery,Activated,Triggered`) restrict the Spell kind
// to instant/sorcery CARD objects -- a creature spell is never admitted.
// Target-count, controller and spell-characteristic qualifiers are parsed
// here as part of the same token. The rules-side matcher supplies the live
// characteristics and chosen-target count that the lower state package cannot
// derive.
type targetTypeToken = state.StackKindToken

// stackTargetKindTokens parses a TargetType$ value into its kind tokens.
// A parameter that is absent -- or whose tokens name no stack kind at all --
// defaults to Spell-only (today's behaviour, deliberately narrow: a spec
// that never said it wants abilities does not get them).
func stackTargetKindTokens(tt string) []targetTypeToken { return state.StackKindTokens(tt) }

// stackKindAdmits reports whether any TargetType$ token admits the stack
// object o (of kind k) controlled by controller, from chooser you's
// perspective. Token semantics are OR, matching ValidTgts$ alternatives:
// the object is offered when SOME token whose kind set contains k admits it
// under that token's own controller and card-type restriction. An
// instantOnly/sorceryOnly token checks the card object's Face, so a creature
// spell is never admitted by Spider Sense's `Instant,Sorcery,Triggered` or
// Sister of Silence's `Spell.Instant,Spell.Sorcery,...`; a Face-less object
// (never reachable for stackSpell, since StackObjKind only classifies a
// Face-bearing object as a spell) fails closed.
func (e *Engine) stackKindAdmits(toks []targetTypeToken, k stackObjKind, o *state.Object, controller, you state.PlayerID) bool {
	for _, tok := range toks {
		if !state.StackKindAdmits([]state.StackKindToken{tok}, k, o, controller, you) {
			continue
		}
		if tok.SingleTarget && len(o.Targets) != 1 {
			continue
		}
		if tok.NumTargetsOp != "" && !targetCountMatches(len(o.Targets), tok.NumTargetsOp, tok.NumTargets) {
			continue
		}
		if k == stackSpell {
			if tok.NonCreature && e.IsCreature(o.ID) {
				continue
			}
			if tok.Colorless && e.Colors(o.ID) != "" {
				continue
			}
			if tok.Legendary && !stackHasType(e.Derived(o.ID).Types, "Legendary") {
				continue
			}
		}
		return true
	}
	return false
}

func stackHasType(types []string, want string) bool {
	for _, typ := range types {
		if strings.EqualFold(typ, want) {
			return true
		}
	}
	return false
}

func targetCountMatches(count int, op string, want int) bool {
	switch op {
	case "EQ":
		return count == want
	case "NE":
		return count != want
	case "GE":
		return count >= want
	case "GT":
		return count > want
	case "LE":
		return count <= want
	case "LT":
		return count < want
	default:
		return false
	}
}

// targetName is the object's name for a target prompt, tolerating the ability
// stack object (no Face) a triggered ability's own target ask produces by
// falling back to its source permanent's name.
func (e *Engine) targetName(source state.ObjID) string {
	if o := e.G.Obj(source); o != nil {
		if f := o.Face(); f != nil && f.Name != "" {
			return f.Name
		}
		if s := e.G.Obj(o.Source); s != nil {
			if sf := s.Face(); sf != nil && sf.Name != "" {
				return sf.Name
			}
		}
	}
	// Falling back to the literal word "target" (reviewer minor 7) is
	// deliberate: every caller has already given the decision a usable
	// prompt, so this is only reached for an object with no name at all --
	// an unreadable name there is better than a fabricated one.
	return "target"
}

// targetOptionLabel renders one target option's label: the target's name
// followed by its controller's. The Face-less ability object a
// TargetType$ Activated/Triggered census now offers has no name of its own,
// so targetName's fallback (the source permanent's name) serves -- the two
// census consumers (cast.go's targetAsk and stack.go's askTarget) share this
// one helper so an ability object can never reach a nil-Face dereference in
// either.
func (e *Engine) targetOptionLabel(candidate targetCandidate) string {
	// The controller's name is seat-facing (the seat that answers sees it),
	// so it prefers the table's display name over the deck-identity slug.
	label := seatFacingName(e.G, candidate.player)
	if candidate.obj != 0 {
		label = e.targetName(candidate.obj) + " (" + label + ")"
	}
	return label
}

// protectionSource resolves the object whose characteristics decide whether
// "protection from X" filters out a candidate target or a Damage recipient
// (Task 15 fix round 1, Critical C2): for an ability stack object -- the
// top-of-stack wrapper events.Apply mints via AbilityPush/TriggerPush with no
// Face -- that is the Source permanent that carries the ability, because
// effects.ColorsOf and the Face-type reads in sourceHasQuality both return
// nothing for a Face-less object (CR 606.3: targeting checks the spell or
// ability's source, and for an activated or triggered ability that source is
// the permanent that granted it). For everything else -- a spell's own stack
// object, which IS the card and so carries a Face, or a plain permanent -- it
// is the object itself. This is the single definition both askTarget (which
// in the same round discovered it was passing the Face-less ability object)
// and legalTargets consult, so the two sites always agree on what "the
// source" is.
func (e *Engine) protectionSource(source state.ObjID) state.ObjID {
	if o := e.G.Obj(source); o != nil && o.Ability != nil && o.Source != 0 {
		return o.Source
	}
	return source
}

// describeTargetEffect is the context-aware target payload builder. The
// target ask is CR 601.2c's choice among legal targets, so a dynamic amount
// must be evaluated in the same announced/cast context the eventual effect
// will use -- never guessed from a zero-value Num read.
func (e *Engine) describeTargetEffect(p state.PlayerID, source state.ObjID, sa *cards.SA, x int32) *decision.TargetEffect {
	if sa == nil {
		return nil
	}
	out := &decision.TargetEffect{API: sa.API}
	if x == 0 {
		if o := e.G.Obj(source); o != nil {
			x = o.X
		}
	}
	if removal := targetRemoval(sa); removal != nil {
		out.Removal = removal
	}
	if sa.API == "Effect" {
		out.Statics = e.grantedStaticModes(p, source, sa)
	}
	switch sa.API {
	case "DealDamage", "DamageAll":
		out.Damage = &decision.DamageEffect{}
		// A missing amount remains null even though the effect implementation
		// has a defensive runtime default. A literal or a resolvable X/SVar is
		// the amount the client and bot can actually reason about at this ask.
		if amount, ok := e.targetDamageAmount(p, source, sa, x); ok && amount >= 0 {
			n := int(amount)
			out.Damage.Amount = &n
		}
	}
	return out
}

// grantedStaticModes resolves an Effect SA's StaticAbilities$ SVar names
// against the source face's SVar table and returns each body's Mode$ value,
// in the order the names were written. It is the target-ask half of the
// Effect registration path (effects' effEffect walks the same names and
// resolves the same bodies via parseStaticLine), so the decision payload and
// the registered effect cannot disagree about what static an activation
// grants. A name that does not resolve, or whose body carries no Mode$, is
// silently skipped -- the payload publishes only what it can prove, exactly
// the way an unknown API stays uninterpreted.
func (e *Engine) grantedStaticModes(p state.PlayerID, source state.ObjID, sa *cards.SA) []string {
	names := strings.FieldsFunc(sa.Params["StaticAbilities"], func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	if len(names) == 0 {
		return nil
	}
	// The SVar table is whichever face targetBoundCtx binds -- the source
	// permanent for an ability object (Whirler Rogue's activation), the
	// spell itself for a spell. It is the same anchor the dynamic numeric
	// grammar uses, so one read serves both.
	ctx, ok := e.targetBoundCtx(p, source)
	if !ok || ctx.SVars == nil {
		return nil
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		body := strings.TrimSpace(ctx.SVars[name])
		if body == "" {
			continue
		}
		statics, ok := cards.ParseStaticLines(body)
		if !ok {
			continue
		}
		for _, st := range statics {
			if st.Mode != "" {
				out = append(out, st.Mode)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// targetDamageAmount evaluates NumDmg with a verdict. effects.NumResolved is
// intentionally a broad numeric reader for effect sites that degrade an
// unmodelled SVar to zero; a decision payload must not turn that degradation
// into a claimed damage amount, so SVar bodies use EvalCountOK here.
func (e *Engine) targetDamageAmount(p state.PlayerID, source state.ObjID, sa *cards.SA, x int32) (int32, bool) {
	ctx, ok := e.targetBoundCtx(p, source)
	if !ok {
		ctx = &effects.Ctx{Source: source, Controller: p}
		if o := e.G.Obj(source); o != nil && o.Face() != nil {
			effects.SetSVars(ctx, o.Face().SVars)
		}
	}
	ctx.X = x
	raw, present := sa.Params["NumDmg"]
	if !present {
		return 0, false
	}
	raw = strings.TrimSpace(raw)
	if n, err := strconv.ParseInt(raw, 10, 32); err == nil {
		return int32(n), true
	}
	sign := int32(1)
	if len(raw) > 1 && (raw[0] == '+' || raw[0] == '-') {
		if raw[0] == '-' {
			sign = -1
		}
		raw = raw[1:]
	}
	if ctx.SVars != nil {
		if body, found := ctx.SVars[raw]; found {
			// A body reading the target reference family has no value at this
			// ask: the unbound evaluation is the empty target set's sum, and
			// publishing it would claim a false zero (Kiku's Shadow's
			// SVar:X:Targeted$CardPower against a legal 5/5 deals 5, not 0).
			if e.amountDependsOnPendingTarget(ctx, p, source, sa, body) {
				return 0, false
			}
			n, resolved := effects.EvalCountOK(e, ctx, body)
			return sign * n, resolved
		}
	}
	// A direct helper/test ask without a source object has no announced or
	// resolving context in which a dynamic value could be known. Keep it null
	// rather than turning the evaluator's zero fallback into a claim.
	if source == 0 {
		return 0, false
	}
	if raw == "X" {
		return sign * ctx.X, true
	}
	// Inline Count$/ref-property bodies are valid direct numeric parameters.
	// The evaluator supplies the unknown verdict instead of collapsing them to
	// zero. This also covers published trigger/result values when their body is
	// supported by the effects count grammar. The same pending-target probe
	// guards this branch: an inline Targeted$ body is exactly as unvalued at
	// the ask as an SVar one.
	if e.amountDependsOnPendingTarget(ctx, p, source, sa, raw) {
		return 0, false
	}
	n, resolved := effects.EvalCountOK(e, ctx, raw)
	return sign * n, resolved
}

// amountDependsOnPendingTarget reports whether a NumDmg body's value moves
// with WHICH target the answering player is about to choose. The target ask
// is CR 601.2c's choice among legal candidates, so a body reading the target
// reference family (Targeted$CardPower, TargetedPlayer$Valid..., their
// Parent/This/All spellings) has NO value yet: its unbound evaluation is the
// empty target set's sum, and EvalCountOK rightly treats an empty set as a
// legitimate count -- which is precisely why the payload cannot take that 0
// as a nominal amount. The verdict is derived from evaluation, not from a
// hand-built token list, so a future target-reading head is covered without
// this site learning about it: bind each legal candidate as the body's ONLY
// target and compare against the unbound read. Any disagreement means the
// pending choice moves the amount, and no scalar may be published (null --
// "unknown" -- is the honest payload). A body that agrees with its unbound
// read under every candidate (Count$YourLifeTotal, Count$xPaid) is genuinely
// target-independent and stays publishable; a target-dependent sum over a
// multi-target ask also disagrees (any single binding differs from the empty
// sum whenever the value is nonzero), so a plural selection cannot smuggle a
// single-binding value through either. The census is the same
// legalTargetCandidates walk askTarget poses its options from, so the probe
// never sees a candidate the ask cannot offer. Cost: one extra census plus
// len(candidates) evaluations per posed damage ask -- decision posing, not a
// hot path.
func (e *Engine) amountDependsOnPendingTarget(ctx *effects.Ctx, p state.PlayerID, source state.ObjID, sa *cards.SA, body string) bool {
	base, _ := effects.EvalCountOK(e, ctx, body)
	for _, cand := range e.legalTargetCandidates(p, source, source, sa) {
		// Ctx is threaded by pointer through the evaluator; the probe binds
		// targets on a value copy and never touches the caller's context.
		probe := *ctx
		if cand.kind == "player" {
			probe.Targets = []state.Target{{Player: cand.player, IsPlayer: true}}
		} else {
			probe.Targets = []state.Target{{Obj: cand.obj}}
		}
		if v, _ := effects.EvalCountOK(e, &probe, body); v != base {
			return true
		}
	}
	return false
}

// targetRemoval classifies only APIs and destinations whose direct meaning is
// known. In particular, an unfamiliar API is never inferred to be removal
// from a label or parameter spelling.
func targetRemoval(sa *cards.SA) *decision.RemovalEffect {
	if sa == nil {
		return nil
	}
	switch sa.API {
	case "Destroy", "DestroyAll":
		return &decision.RemovalEffect{Kind: "destroy"}
	case "Sacrifice", "SacrificeAll":
		return &decision.RemovalEffect{Kind: "sacrifice"}
	case "ChangeZone", "ChangeZoneAll":
		destination := strings.ToLower(strings.TrimSpace(sa.Params["Destination"]))
		kind := destination
		switch destination {
		case "exile":
			kind = "exile"
		case "hand":
			kind = "bounce"
		case "graveyard":
			kind = "graveyard"
		case "library":
			kind = "library"
		case "command":
			kind = "command"
		default:
			return nil
		}
		return &decision.RemovalEffect{Kind: kind, Destination: destination}
	}
	return nil
}

type targetCandidate struct {
	kind   string
	obj    state.ObjID
	player state.PlayerID
}

// legalTargetCandidates is the pure target census shared by cast-option
// enumeration and the post-announcement target ask. It reads state in the
// same deterministic order as the old askTarget loops and emits no events.
//
// source is the object the spec's Self/Other predicates and the protection
// test are resolved against (for an ability, the Source permanent).
// excludeSelf is the object a prospective target may not equal -- the CR
// playerTargetSpecMatches judges one candidate seat q against a ValidTgts$
// player spec from the asker's (you) perspective. It is the ONE judge both
// target sites go through -- the offer (candidatesFor) and the resolution
// recheck (legalTargets) -- so the two cannot disagree (the one-definition
// rule). Every ordinary alternative is judged by the shared
// effects.MatchesPlayerSpecFrom grammar; an alternative whose qualifier names
// a trigger role the ask's own TriggerContext carries (pg2 event roles) is
// judged by triggerRolePlayerAlt below, because MatchesPlayerSpecFrom takes
// no trigger context and fails closed on the role names -- which turned The
// Lord of Pain's mandatory "choose another target player" trigger
// (ValidTgts$ Player.!TriggeredActivator) into an ask with no legal target
// and a silent fizzle. Forge's comma is OR: the spec matches when any one
// alternative matches.
func (e *Engine) playerTargetSpecMatches(sc effects.SpecContext, spec string, q, you state.PlayerID, source state.ObjID) bool {
	for alt := range strings.SplitSeq(spec, ",") {
		if matched, known := e.triggerRolePlayerAlt(sc, alt, q, you); known {
			if matched {
				return true
			}
			continue
		}
		if effects.MatchesPlayerSpecFrom(e.G, alt, q, you, source) {
			return true
		}
	}
	return false
}

// triggerRolePlayerAlt evaluates ONE comma-alternative of a ValidTgts$
// player spec whose qualifier names a trigger role the ask's own
// TriggerContext carries. The corpus writes exactly two such qualifiers on a
// player alternative, both negated: Player.!TriggeredActivator (The Lord of
// Pain) and Player.!TriggeredCardController (Lucy MacLean, Positively
// Armed); a positive form resolves the same way. The binding rides the ask's
// SpecContext (pg2); an absent binding matches NOBODY, including under !
// (the documented pg2 absent-binding contract), so the fail-closed direction
// is kept for every qualifier the context cannot answer. known=false when
// the alternative does not name a trigger role at all, leaving it to the
// shared grammar.
func (e *Engine) triggerRolePlayerAlt(sc effects.SpecContext, alt string, q, you state.PlayerID) (bool, bool) {
	base, qualifier, qualified := strings.Cut(strings.TrimSpace(alt), ".")
	if !qualified {
		return false, false
	}
	neg := strings.HasPrefix(qualifier, "!")
	var role state.PlayerID
	bound := false
	switch strings.TrimPrefix(qualifier, "!") {
	case "TriggeredActivator":
		role, bound = sc.TriggerContext.TriggerActivator.Player, sc.TriggerContext.TriggerActivator.IsPlayer
	case "TriggeredCardController":
		// effects.TriggeredCardController is the one resolver -- Defined$,
		// OptionalDecider$ and the targeting restriction all read it.
		if p, ok := effects.TriggeredCardController(e.G, sc.TriggerContext, sc.Remembered); ok {
			role, bound = p, true
		}
	default:
		return false, false
	}
	// The base still applies to the role alternative, exactly as
	// MatchesPlayerSpecFrom applies it to every other qualifier.
	switch base {
	case "Player", "Any":
	case "You":
		if q != you {
			return false, true
		}
	case "Opponent", "Other":
		if q == you {
			return false, true
		}
	default:
		// An object alternative (Creature.!TriggeredTarget, ...): not this
		// helper's business -- the object arm judges it.
		return false, false
	}
	return bound && ((q == role) != neg), true
}

// 115.5 self-targeting rule. It is separate from source because during a
// cast/activation proposal the two diverge: a spell on the stack may not
// target itself (excludeSelf == the card), while an activated ability CAN
// target its own Source permanent (excludeSelf == 0, since the Face-less
// ability object is not on the stack yet). Callers set excludeSelf == 0 to
// disable the rule.
func (e *Engine) legalTargetCandidates(p state.PlayerID, source, excludeSelf state.ObjID, sa *cards.SA) []targetCandidate {
	return e.candidatesFor(p, source, excludeSelf, sa, true)
}

// affectedCandidates is the Overload counterpart of legalTargetCandidates.
// It applies the script's object/player filter and zone/type restrictions but
// deliberately omits every rule that exists only because something is a
// target: protection, hexproof/CantTarget, and becomes-target bookkeeping.
func (e *Engine) affectedCandidates(p state.PlayerID, source, excludeSelf state.ObjID, sa *cards.SA) []targetCandidate {
	return e.candidatesFor(p, source, excludeSelf, sa, false)
}

func (e *Engine) candidatesFor(p state.PlayerID, source, excludeSelf state.ObjID, sa *cards.SA, targeting bool) []targetCandidate {
	return e.candidatesForLimit(p, source, excludeSelf, sa, targeting, 0)
}

// candidatesForLimit is candidatesFor that may stop enumerating once limit
// (> 0) candidates are collected. The early stop is taken only when neither
// post-filter (TargetsWithDefinedController$, TargetValidTargeting$) is
// present -- both can only DROP candidates, so without them the census is
// append-only and its first limit entries are exactly the full list's. The
// feasibility gate (targetSAAvailable) needs a count, never the list.
func (e *Engine) candidatesForLimit(p state.PlayerID, source, excludeSelf state.ObjID, sa *cards.SA, targeting bool, limit int) []targetCandidate {
	if limit > 0 && (strings.TrimSpace(sa.Params["TargetsWithDefinedController"]) != "" ||
		strings.TrimSpace(sa.Params["TargetValidTargeting"]) != "" ||
		strings.TrimSpace(sa.Params["TargetsWithControllerProperty"]) != "" ||
		// tpc1: TargetingPlayerControls$ True is also a DROPPING post-filter,
		// so the census must not stop at limit before the whole search space
		// (battlefield included) has been walked and filtered.
		strings.TrimSpace(sa.Params["TargetingPlayerControls"]) != "" ||
		sharedCardTypeRef(sa) != "") {
		limit = 0
	}
	spec := sa.Params["ValidTgts"]
	// The spec-relative source (Self/Other/CARDNAME/sameName predicates read
	// it) is the SOURCE PERMANENT when the ask belongs to a minted ability
	// object -- the same object resolution-time recheck (legalTargets) already
	// judges its specs against (rules/stack.go passes o.Source there), so the
	// offer and the recheck cannot disagree (Critical C2's one-definition
	// rule). The Face-less wrapper itself is never a creature/permanent, so
	// a spec like Flamerush Rider's `Creature.attacking+Other` judged the
	// wrapper id meant "every creature but nobody in particular" and offered
	// the ability's own source as its own copy target. excludeSelf stays the
	// object the CR 115.5 self-targeting rule keys on (the stack object, for
	// an ability -- an ability CAN legally target its own Source permanent),
	// and the trigger-context lookup stays keyed on the stack id.
	specSrc := source
	if o := e.G.Obj(source); o != nil && o.Face() == nil && o.Ability != nil && o.Source != 0 {
		specSrc = o.Source
	}
	sc := e.targetSpecContext(specSrc, excludeSelf, p)
	zones := targetZones(sa)
	var out []targetCandidate
	// Resolve the source ONCE for the whole census -- for an ability this is
	// the Source permanent, not the Face-less stack object. Every protection
	// test below is guarded on the candidate's zone, because a permanent's
	// static ability functions only on the battlefield (CR 604.3), so a
	// printed protection does not withhold a target sitting in the
	// Graveyard/Hand/Exile that a TgtZone$ spec is asking about. The PLAYER
	// candidates below read it too (hexproof from a quality resolves the
	// same source); the player-side shroud and hexproof grants carry no zone
	// gate -- a player is always in play.
	protSrc := e.protectionSource(source)
	// Players are offered only alongside the default battlefield search and
	// only when the spec actually names a seat. A spec that routes elsewhere
	// (TgtZone$ Graveyard/Hand/Exile) targets objects only -- never a player.
	// Each candidate seat is judged by the SAME shared player filter the
	// resolution recheck (legalTargets) applies, from the asker's perspective,
	// so offer and recheck cannot disagree (the one-definition rule): a
	// ValidTgts$ Opponent ask no longer offers the controller, ValidTgts$ You
	// no longer offers opponents, and a spec whose qualifier the filter cannot
	// evaluate fails closed to no seat (the AGENTS.md MatchesPlayerSpec
	// convention). Note MatchesPlayerSpecFrom splits on ',' and skips
	// alternatives whose base is not a player base, so a mixed
	// `Creature,Opponent` spec keeps the object half and matches only the
	// player half's seats.
	// CR 702.18 (player shroud), CR 702.11 (player hexproof) and CR 702.16c
	// (player protection): a seat a live `Affected$ You | AddKeyword$`
	// static grants those keywords is withheld here exactly as a permanent
	// carrying them is withheld in the object arm below -- the grant is read
	// off the same layer walk, through playerKeywords (rules/playerkeywords.go).
	// Protection is judged against the same census-wide protSrc the object
	// arm's protectedFrom uses, so the two arms resolve "the source"
	// identically (CR 702.16c). Only the targeting arm consults them; the
	// affected census (targeting=false) does not, the same split the
	// permanent shroud gate applies.
	if len(zones) == 1 && zones[0] == state.ZBattlefield {
		for _, q := range e.G.AliveFrom(0) {
			if e.playerTargetSpecMatches(sc, spec, q, p, specSrc) &&
				(!targeting || !e.playerShroudBlocksTarget(q)) &&
				(!targeting || !e.playerHexproofBlocksTarget(q, p, protSrc)) &&
				(!targeting || !e.playerProtectedFrom(q, protSrc)) {
				out = append(out, targetCandidate{kind: "player", player: q})
			}
		}
	}
	if limit > 0 && len(out) >= limit {
		return out
	}
zoneLoop:
	for _, z := range zones {
		if z == state.ZStack {
			// The stack is a single, shared sequence, not a per-seat zone, so
			// its objects are enumerated ONCE each -- labelled with the
			// object's own controller -- rather than once per alive seat,
			// which would offer the same spell N times in an N-seat game and
			// drift the option list. Which stack objects are targetable is
			// TargetType$'s job (CR 115.5 aside): Spell admits card objects,
			// Instant/Sorcery admit instant/sorcery card objects only (Spider
			// Sense, Sister of Silence), Activated/Triggered admit the ability
			// objects AbilityPush/TriggerPush mint, SpellAbility admits all
			// three, and a TargetType$ naming no stack kind falls back to
			// Spell -- the pre-fix behaviour, kept for every spec that never
			// said otherwise (the default stays the narrow one, never
			// widened).
			toks := stackTargetKindTokens(sa.Params["TargetType"])
			for _, oid := range e.G.Zone(state.ZStack, 0) {
				o := e.G.Obj(oid)
				// CR 115.5: a spell or ability on the stack is an illegal
				// target for itself. This census runs after PutOnStack or
				// AbilityPush, so source is already that object atop the
				// stack -- its own id must never be offered, or a
				// counterspell would counter itself. Only the source OBJECT
				// is excluded, never a different copy of the same card.
				if o == nil || (excludeSelf != 0 && oid == excludeSelf) {
					continue
				}
				if !e.stackKindAdmits(toks, e.stackObjKind(o), o, o.Controller, p) {
					continue
				}
				// The cast-provenance split (castprov1/castprov3/wascastfrom):
				// a stack-target spec carrying a wasCast* token — Wash Away's
				// `Card.!wasCastFromTheirHand` — evaluates the token against the
				// candidate's cast log here, before the ordinary filter; the
				// effects-side filter never strips the token, so without this the
				// spec fails closed to no candidate.
				tspec, ok := e.castProvenanceAdmits(targetSpecForZone(spec, z), oid, p)
				if !ok {
					continue
				}
				if e.matchesSpec(tspec, oid, sc) {
					out = append(out, targetCandidate{kind: stackTargetOptionKind(e.stackObjKind(o)), obj: oid, player: o.Controller})
					if limit > 0 && len(out) >= limit {
						break zoneLoop
					}
				}
			}
			continue
		}
		// Hand is the chooser's own hand only (CR 701.15a); the other
		// non-battlefield zones are public, so every seat's slice is offered.
		players := e.G.AliveFrom(0)
		if z == state.ZHand {
			players = []state.PlayerID{p}
		}
		for _, q := range players {
			for _, oid := range e.G.Zone(z, q) {
				o := e.G.Obj(oid)
				// CR 702.16c withholds a permanent protected from the
				// targeting source's qualities; a CantTarget restriction
				// (Vines of Vastwood) withholds one from the spoke player;
				// CR 702.14 shroud withholds one from EVERY targeting spell
				// or ability, its controller's included.
				// All function only on the battlefield (CR 604.3), the same
				// gate as protection above. CR 115.5 excludes the source.
				if o != nil && o.Face() != nil && !o.PhasedOut && (excludeSelf == 0 || oid != excludeSelf) {
					// The cast-provenance split at the non-battlefield target
					// zones too (wascastfrom): the token evaluates against the
					// candidate's cast log before the ordinary filter.
					tspec, ok := e.castProvenanceAdmits(targetSpecForZone(spec, z), oid, p)
					if !ok {
						continue
					}
					if e.matchesSpec(tspec, oid, sc) &&
						e.mentorAdmits(sa, specSrc, oid) &&
						(!targeting || !(o.Zone == state.ZBattlefield && e.protectedFrom(oid, protSrc))) &&
						(!targeting || !(o.Zone == state.ZBattlefield && e.shroudBlocksTarget(oid))) &&
						(!targeting || !(o.Zone == state.ZBattlefield && e.hexproofBlocksTarget(oid, p, protSrc))) &&
						(!targeting || !(o.Zone == state.ZBattlefield && e.restrictionBlocksTarget(oid, p))) {
						out = append(out, targetCandidate{kind: "permanent", obj: oid, player: q})
						if limit > 0 && len(out) >= limit {
							break zoneLoop
						}
					}
				}
			}
		}
	}
	out = e.filterTargetsWithDefinedController(out, sa, sc)
	out = e.filterTargetControllerProperty(out, sa)
	out = e.filterTargetsWithSharedCardType(out, sa, source, sc)
	out = e.filterTargetValidTargeting(out, sa, sc)
	if targeting {
		// tpc1: TargetingPlayerControls$ True -- the answering seat's
		// battlefield permanents only. Applied to the targeting census only;
		// the Overload affected sweep keeps its non-target semantics.
		out = e.filterTargetingPlayerControls(out, sa, p, source)
	}
	return out
}

// filterTargetValidTargeting implements TargetValidTargeting$ (Not of This
// World: "Counter target spell or ability that targets a permanent you
// control", TargetValidTargeting$ Permanent.YouCtrl+inRealZoneBattlefield):
// the candidate stack object QUALIFIES only when its own chosen targets
// include an object matching the spec, evaluated from the targeting
// ability's controller (the counter's "you" is its controller, not the
// countered spell's). Only permanent-kind candidates carry targets to
// check; a player candidate or a stack object without recorded targets --
// the ability-object shapes whose per-stack target bindings live in the
// trigger/activation roles this filter cannot see -- fails the filter, the
// narrower direction (Not of This World can still counter every spell that
// visibly targeted a matching permanent; an ability it cannot verify is
// never offered, never wrongly offered).
func (e *Engine) filterTargetValidTargeting(in []targetCandidate, sa *cards.SA, sc effects.SpecContext) []targetCandidate {
	spec := strings.TrimSpace(sa.Params["TargetValidTargeting"])
	if spec == "" {
		return in
	}
	out := make([]targetCandidate, 0, len(in))
	for _, cand := range in {
		if cand.kind != "permanent" {
			continue
		}
		o := e.G.Obj(cand.obj)
		if o == nil {
			continue
		}
		for _, t := range o.Targets {
			if t.IsPlayer || t.Obj == 0 {
				continue
			}
			if e.matchesSpec(spec, t.Obj, sc) {
				out = append(out, cand)
				break
			}
		}
	}
	return out
}

// filterTargetsWithDefinedController implements the common target restriction
// that says the chosen object must be controlled by an event-role player. It
// is deliberately applied once after every zone's candidates are collected,
// so battlefield, graveyard and stack target offers cannot drift apart.
//
// Only a selector this build binds narrows the offer. An unsupported selector
// (ParentTarget, ParentTargetedController, TriggeredCauser, ...) and a
// supported role the current trigger did not bind leave the candidates
// unchanged -- the offer every such ability had before this restriction was
// read -- so no existing ability silently loses its targets. The two roles
// only an attack-declaration trigger binds (TriggeredAttackingPlayer,
// TriggeredAttackedTarget: Karazikar, Firkraag, Seifer, Gornog, Whirlwind
// Killer) and NonTriggeredCardController (Confusion in the Ranks: "target
// permanent another player controls") fail closed when unbound instead, since
// offering every creature would widen "target creature that player controls"
// to any player's, or "another player's permanent" to your own.
func (e *Engine) filterTargetsWithDefinedController(in []targetCandidate, sa *cards.SA, sc effects.SpecContext) []targetCandidate {
	ref := strings.TrimSpace(sa.Params["TargetsWithDefinedController"])
	if ref == "" {
		return in
	}
	var player state.PlayerID
	var ok bool
	failClosed := false
	nonTriggeredController := false
	switch ref {
	case "NonTriggeredCardController":
		nonTriggeredController = true
		player, ok = effects.TriggeredCardController(e.G, sc.TriggerContext, sc.Remembered)
	case "TriggeredTarget":
		if sc.TriggerTarget.IsPlayer {
			player, ok = sc.TriggerTarget.Player, true
		} else if o := e.G.Obj(sc.TriggerTarget.Obj); o != nil {
			player, ok = o.Controller, true
		}
	case "TriggeredDefendingPlayer":
		if sc.DefendingPlayer.IsPlayer {
			player, ok = sc.DefendingPlayer.Player, true
		}
	case "TriggeredPlayer":
		if sc.TriggerPlayer.IsPlayer {
			player, ok = sc.TriggerPlayer.Player, true
		}
	case "TriggeredAttackingPlayer":
		failClosed = true
		if sc.AttackingPlayer.IsPlayer {
			player, ok = sc.AttackingPlayer.Player, true
		}
	case "TriggeredAttackedTarget":
		failClosed = true
		if sc.AttackedTarget.IsPlayer {
			player, ok = sc.AttackedTarget.Player, true
		}
	case "TriggeredCardController":
		player, ok = effects.TriggeredCardController(e.G, sc.TriggerContext, nil)
	}
	if !ok {
		if failClosed || nonTriggeredController {
			return nil
		}
		return in
	}
	out := in[:0]
	for _, candidate := range in {
		if candidate.kind != "permanent" {
			continue
		}
		o := e.G.Obj(candidate.obj)
		if o == nil {
			continue
		}
		if nonTriggeredController {
			if nonTriggeredControllerAdmits(o, player, ok) {
				out = append(out, candidate)
			}
			continue
		}
		if o.Controller == player {
			out = append(out, candidate)
		}
	}
	return out
}

// nonTriggeredControllerAdmits is the ONE definition of the
// TargetsWithDefinedController$ NonTriggeredCardController predicate ("target
// permanent another player controls", CR 109.5's complement of the triggering
// card's controller). It is shared by the offer post-filter
// (filterTargetsWithDefinedController) and the CR 608.2b resolution recheck
// (legalTargets) so the two cannot drift: both sites admit a candidate only
// when the triggering card's controller resolved (ok) and the candidate is a
// battlefield permanent controlled by a different player. The same-controller
// case is rejected, and an unresolvable controller fails closed, at both
// sites.
func nonTriggeredControllerAdmits(o *state.Object, controller state.PlayerID, ok bool) bool {
	return ok && o != nil && o.Zone == state.ZBattlefield && o.Controller != controller
}

// charmTargetSlots returns the selected DISTINCT target-bearing mode bodies.
// Repeated mode instances deliberately return nil: their later occurrences
// retain the established per-instance ask path.
func charmTargetSlots(svars map[string]string, root *cards.SA, modes []string) []string {
	if root == nil || root.API != "Charm" || len(modes) < 2 {
		return nil
	}
	seen := make(map[string]bool, len(modes))
	var slots []string
	for _, name := range modes {
		name = strings.TrimSpace(name)
		if seen[name] {
			return nil
		}
		seen[name] = true
		if sub := cards.ResolveSVar(svars, name); sub != nil && strings.TrimSpace(sub.Params["ValidTgts"]) != "" {
			slots = append(slots, name)
		}
	}
	if len(slots) < 2 {
		return nil
	}
	return slots
}

// askCharmModeTargets is the ordinary distinct-mode target ask (CR 601.2c).
// Every target-bearing chosen mode declares its own targets, so one option
// group per mode is allocated with each mode's OWN bounds. When every mode is
// exactly 1..1 the single combined decision is kept -- Min == Max == number of
// modes, so Decision.Validate requires exactly one target from every mode's
// own candidate set, and that wire form is pinned by
// charm_distinct_targets_test.go. A mode with bounds outside 1..1 (an "up to N"
// plural declaration, a mandatory minimum above one, differing maxes) cannot
// be expressed in that one decision: a Decision has ONE global Min/Max, one
// shared GroupLimit and no per-group minimum, so a plural mode could otherwise
// satisfy the count while a mandatory single-target mode contributes nothing
// (and, with the mode chosen first, steal its target). Those shapes are asked
// SEQUENTIALLY, one mode at a time in chosen-mode order, through the same
// binding machinery (each answer lands as one group in pc.charmTargets /
// e.charmTargets, which recheckCharmTargets and charmDistinctTargetRun already
// consume positionally). infeasible means a mandatory mode's minimum cannot be
// met: callers must not fall back to the first-mode ask.
func (e *Engine) askCharmModeTargets(p state.PlayerID, source state.ObjID, svars map[string]string, root *cards.SA, modes []string) (asked, infeasible bool) {
	slots := charmTargetSlots(svars, root, modes)
	if len(slots) < 2 {
		return false, false
	}
	choices := strings.Split(root.Params["Choices"], ",")
	if status, _ := effects.CharmCrossModeShape(svars, choices); status != effects.CharmUniqueNone {
		// The already-implemented TargetUnique family has a different wire
		// contract (one target per mode AND one different player per target).
		// Leave it to its dedicated ask path rather than weakening that
		// constraint.
		return false, false
	}
	type slot struct {
		name string
		sa   *cards.SA
		cs   []targetCandidate
	}
	var all []slot
	allSimple := true
	for _, name := range slots {
		sa := cards.ResolveSVar(svars, name)
		min, max := e.resolvedTargetBounds(p, source, sa, 0)
		if min != 1 || max != 1 {
			allSimple = false
		}
		cs := e.legalTargetCandidates(p, source, source, sa)
		if len(cs) < min {
			// A mandatory minimum the board cannot meet makes the whole
			// announcement impossible (CR 601.2c): abort rather than pose an
			// unanswerable ask or silently drop the mode.
			return false, true
		}
		all = append(all, slot{name: name, sa: sa, cs: cs})
	}
	if !allSimple {
		// A shape the one combined decision cannot express: pose each mode's
		// own ask in turn. Seed the binding storage -- the cast's scratch or
		// the triggered object's map, discriminated exactly as handleTarget's
		// answer arm does -- so a skipped optional mode's empty group is
		// recorded where resolution reads it.
		var groups [][]state.Target
		groups, asked, infeasible = e.askCharmSeqSlot(p, source, svars, root, slots, groups)
		if infeasible {
			return false, true
		}
		e.storeCharmSeqGroups(source, groups)
		return asked, false
	}
	d := &decision.Decision{Player: p, Kind: decision.KTarget, Min: len(all), Max: len(all),
		Prompt: fmt.Sprintf("Choose one target for each of %d modes", len(all)), Source: source,
		ResumeKind: "charm_targets", ResumeModes: append([]string(nil), slots...)}
	for i, s := range all {
		for _, candidate := range s.cs {
			o := decision.Option{Index: len(d.Options), Kind: candidate.kind,
				Label: e.targetOptionLabel(candidate), Obj: candidate.obj, Player: candidate.player,
				Group: fmt.Sprintf("charm-mode-%d", i), Controller: e.candidateControllerSeat(candidate)}
			d.Options = append(d.Options, o)
		}
	}
	e.ask(d)
	return true, false
}

// askCharmSeqSlot poses the next per-mode target ask of a sequential
// distinct-mode Charm. It walks slots from len(groups) forward: a mode whose
// mandatory minimum the board cannot meet reports infeasible; an optional mode
// with no legal candidates records an EMPTY group (keeping the positional
// binding aligned) and moves on; the first mode with candidates poses its own
// bounded KTarget and returns asked. It returns (groups,false,false) when
// every remaining slot was satisfied by an empty optional group, so the caller
// finishes the announcement. The accumulated groups are the same value the
// combined answer would have produced, in the same target-bearing-mode order,
// so charmTargetGroups/recheckCharmTargets/charmDistinctTargetRun bind them
// positionally without change.
func (e *Engine) askCharmSeqSlot(p state.PlayerID, source state.ObjID, svars map[string]string,
	root *cards.SA, slots []string, groups [][]state.Target) ([][]state.Target, bool, bool) {
	out := append([][]state.Target(nil), groups...)
	for idx := len(out); idx < len(slots); idx++ {
		sa := cards.ResolveSVar(svars, slots[idx])
		if sa == nil {
			out = append(out, nil)
			continue
		}
		min, max := e.resolvedTargetBounds(p, source, sa, 0)
		cs := e.legalTargetCandidates(p, source, source, sa)
		if len(cs) < min {
			return out, false, true
		}
		if len(cs) == 0 {
			out = append(out, nil)
			continue
		}
		if max > len(cs) {
			max = len(cs)
		}
		prompt := strings.TrimSpace(sa.Params["TgtPrompt"])
		if prompt == "" {
			prompt = "Choose a target"
		}
		d := &decision.Decision{Player: p, Kind: decision.KTarget, Min: min, Max: max,
			Prompt: prompt, Source: source, TargetEffect: e.describeTargetEffect(p, source, sa, 0),
			ResumeKind: "charm_mode_seq", ResumeSA: root,
			ResumeModes: append([]string(nil), slots...)}
		for _, candidate := range cs {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: candidate.kind, Label: e.targetOptionLabel(candidate), Obj: candidate.obj,
				Player: candidate.player, Controller: e.candidateControllerSeat(candidate)})
		}
		e.ask(d)
		return out, true, false
	}
	return out, false, false
}

// charmSeqGroups reads the sequential distinct-mode answer groups recorded so
// far, from the cast's scratch or the triggered object's map -- the same split
// storeCharmSeqGroups writes and handleTarget's combined arm discriminates.
// It is the ONE reader of that choice so the two halves cannot disagree about
// where a sequential group lives.
func (e *Engine) charmSeqGroups(source state.ObjID) [][]state.Target {
	if e.cast != nil {
		return e.cast.charmTargets
	}
	return e.charmTargets[source]
}

// storeCharmSeqGroups writes the sequential answer groups to the same place
// charmSeqGroups reads them and the combined answer's groups land: the cast's
// pc.charmTargets (committed to e.charmTargets by payCast) or the triggered
// stack object's e.charmTargets entry.
func (e *Engine) storeCharmSeqGroups(source state.ObjID, groups [][]state.Target) {
	if e.cast != nil {
		e.cast.charmTargets = groups
		return
	}
	if e.charmTargets == nil {
		e.charmTargets = make(map[state.ObjID][][]state.Target)
	}
	e.charmTargets[source] = groups
}

// charmSeqSVars resolves the SVars map and root Charm SA for a sequential ask's
// source object, so a re-asked slot resolves its mode body the same way the
// first pass did: a triggered ability reads its source permanent's face, a
// spell its own declared face.
func (e *Engine) charmSeqSVars(source state.ObjID) (map[string]string, *cards.SA, bool) {
	o := e.G.Obj(source)
	if o == nil {
		return nil, nil, false
	}
	if o.Ability != nil {
		src := e.G.Obj(o.Source)
		if src == nil || src.Face() == nil {
			return nil, nil, false
		}
		return src.Face().SVars, o.Ability, true
	}
	f := o.Face()
	if f == nil {
		return nil, nil, false
	}
	return f.SVars, f.SpellAbility(), true
}

// charmSeqAnswer records one answered per-mode group of a sequential
// distinct-mode Charm target declaration and either poses the next mode's ask
// or finalizes the announcement. It is the sequential twin of the
// charm_targets arm below: the group lands in the same positional storage
// (pc.charmTargets for a cast, e.charmTargets for a trigger), the flat target
// list and its TargetsChosen events are recorded in the same order, and the
// final group triggers the same payCast/trigger-drain tail -- so resolution's
// charmDistinctTargetRun and CR 608.2b's recheckCharmTargets need no change.
func (e *Engine) charmSeqAnswer(d *decision.Decision, in decision.Intent, chosen []decision.Option) {
	svars, root, ok := e.charmSeqSVars(d.Source)
	if !ok {
		return
	}
	thisGroup := targetOptions(chosen)
	prev := e.charmSeqGroups(d.Source)
	groups := append([][]state.Target(nil), prev...)
	groups = append(groups, thisGroup)
	appendFirst := len(prev) > 0
	if e.cast != nil {
		pc := e.cast
		pc.charmTargets = groups
		stageBase := len(pc.targets)
		pc.targets = append(pc.targets, thisGroup...)
		e.repriceForTargets(pc)
		if pc.stackObj != 0 {
			e.recordChosenTargets(pc.stackObj, chosen, stageBase > 0)
		}
	} else {
		e.storeCharmSeqGroups(d.Source, groups)
		e.recordChosenTargets(d.Source, chosen, appendFirst)
	}
	next, asked, infeasible := e.askCharmSeqSlot(d.Player, d.Source, svars, root, d.ResumeModes, groups)
	if infeasible {
		// The first pass pre-checked every slot's mandatory minimum, so this
		// cannot newly fail between asks; keep the callers' own infeasible
		// arms authoritative if it ever does.
		return
	}
	e.storeCharmSeqGroups(d.Source, next)
	if asked {
		return
	}
	if e.cast != nil {
		e.payCast()
		return
	}
	if e.drainAwaitsTarget {
		e.drainAwaitsTarget = false
		e.resumeTriggerDrain()
	} else if e.pending == nil {
		e.emit(events.Event{Kind: events.Priority, Player: in.Player, Amount: 0})
	}
}

// charmTargetGroups partitions a combined Charm target answer by its
// per-mode Option.Group labels. The returned order is target-bearing mode
// order, not the order in which a client happened to submit the options.
func charmTargetGroups(d *decision.Decision, chosen []decision.Option) ([][]state.Target, []decision.Option) {
	groups := make([][]state.Target, len(d.ResumeModes))
	bySlot := make([][]decision.Option, len(groups))
	for _, opt := range chosen {
		const prefix = "charm-mode-"
		if !strings.HasPrefix(opt.Group, prefix) {
			continue
		}
		i, err := strconv.Atoi(strings.TrimPrefix(opt.Group, prefix))
		if err != nil || i < 0 || i >= len(groups) {
			continue
		}
		if opt.Kind == "player" {
			groups[i] = append(groups[i], state.Target{Player: opt.Player, IsPlayer: true})
		} else {
			groups[i] = append(groups[i], state.Target{Obj: opt.Obj})
		}
		bySlot[i] = append(bySlot[i], opt)
	}
	var ordered []decision.Option
	for i := range bySlot {
		ordered = append(ordered, bySlot[i]...)
	}
	return groups, ordered
}

// askCrossModeCharmTargets poses the cross-mode TargetUnique family's ONE
// combined target ask: Min == Max == the number of chosen target-bearing
// modes, over the shared candidate pool of the modes' common ValidTgts$ spec,
// with every option's Group naming its player — Decision.Validate's
// mutual-exclusion rule (and botpolicy clamp's group discipline) enforce
// "each mode must target a different player" on the wire, so an intent that
// reuses a player is not merely wrong but impossible to submit. The answer
// records onto the stack object in choice order, which is the chosen-mode
// order, so the per-mode attribution is positional and replay-safe without
// any new event kind or field. Returns false (nothing asked) when the legal
// candidates are fewer than the modes that need them — the caller keeps the
// historical first-mode narrowing, whose own insufficiency handling governs.
func (e *Engine) askCrossModeCharmTargets(p state.PlayerID, source state.ObjID, tbms []*cards.SA) bool {
	k := len(tbms)
	sub := tbms[0]
	candidates := e.legalTargetCandidates(p, source, source, sub)
	// MaxTotalTargetPower$ over the cross-mode pool: the same shared cap read
	// every other target ask uses, so a Charm whose first target-bearing mode
	// carries the parameter prunes the census that can provably join no legal
	// selection and rides the running budget on the decision exactly as
	// askTarget and cast.go's targetAsk do. The family's modes target players
	// (CharmCrossModeShape requires a player-kind spec), whose Value is 0 and
	// which are never pruned, so a non-negative cap never rejects the required
	// one-per-mode answer; the read exists so the ask cannot silently ignore a
	// cap a caller put on it.
	candidates, powerCap, powerCapped := e.totalPowerCappedCandidates(candidates, p, source, sub, 0)
	if len(candidates) < k {
		return false
	}
	d := &decision.Decision{Player: p, Kind: decision.KTarget, Min: k, Max: k,
		Prompt: fmt.Sprintf("Choose %d targets: one for each mode, each a different player", k),
		Source: source, TargetEffect: e.describeTargetEffect(p, source, sub, 0)}
	for _, candidate := range candidates {
		o := decision.Option{Index: len(d.Options), Kind: candidate.kind,
			Label: e.targetOptionLabel(candidate), Obj: candidate.obj, Player: candidate.player}
		if candidate.kind == "player" {
			o.Group = "charm-mode-player-" + strconv.Itoa(int(candidate.player))
		}
		if powerCapped && candidate.kind != "player" {
			if co := e.G.Obj(candidate.obj); co != nil && co.Face() != nil {
				o.Value = int(e.Power(candidate.obj))
			}
		}
		d.Options = append(d.Options, o)
	}
	if powerCapped {
		d.MaxSum, d.Budgeted = powerCap, true
	}
	e.ask(d)
	return true
}

// candidateControllerSeat is the controller seat a TARGET CANDIDATE keys a
// per-controller selection constraint on: a player candidate is its own seat
// (player candidates carry an explicit seat), an object candidate is its
// controller (its owner for a card in a graveyard/hand/exile, which Move sets
// Controller to). Both oneEachTargetBounds' distinct-controller count and
// targetControllerGroup's group label read this one helper, so the bound and
// the wire exclusivity can never disagree about which candidates share a
// controller.
func (e *Engine) candidateControllerSeat(candidate targetCandidate) state.PlayerID {
	if candidate.kind == "player" {
		return candidate.player
	}
	if o := e.G.Obj(candidate.obj); o != nil {
		return o.Controller
	}
	return candidate.player
}

// oneEachTargetBounds applies Forge's per-controller target selection shapes:
// TargetsForEachPlayer$ (TargetRestrictions.setForEachPlayer) and
// TargetsWithDifferentControllers$ ("targets controlled by different
// players"). Both are the SAME set constraint -- no two chosen targets may
// share a controller -- and both are labelled on the wire by
// targetControllerGroup's Option.Group. askTarget (this file) and cast.go's
// targetAsk share it so the two ask sites cannot drift.
//
// It returns the (possibly rewritten) bounds, whether the constraint applies
// at all, and the DISTINCT-CONTROLLER count. The count is the real capacity of
// the constraint: a TargetMin$/TargetMax$ spelled OneEach asks for exactly
// that many, and BOTH shapes cap the effective maximum at it, because no legal
// answer can ever select more targets than there are distinct controllers.
// Without the cap (the pre-fix state) a TargetsWithDifferentControllers$ SA
// with a literal TargetMin$ 2 | TargetMax$ 2 asked for two picks while
// offering only one selectable group -- an unsatisfiable decision that no
// intent could answer (Run Away Together, Kitsune, Dragon's Daughter).
// Callers compare min against distinct (not the raw option count) to detect
// that no legal set exists.
// sameControllerTargetBounds applies the TargetsWithSameController$ set
// constraint. Unlike the one-per-controller family, this permits multiple
// picks from one group; its capacity is therefore the largest controller
// group, not the number of groups. The capacity is returned separately so
// callers can reject a mandatory ask without exposing an unsatisfiable
// decision.
func (e *Engine) sameControllerTargetBounds(sa *cards.SA, candidates []targetCandidate, min, max int) (int, int, int, bool) {
	if !strings.EqualFold(sa.Params["TargetsWithSameController"], "True") {
		return min, max, 0, false
	}
	counts := map[state.PlayerID]int{}
	for _, candidate := range candidates {
		counts[e.candidateControllerSeat(candidate)]++
	}
	capacity := 0
	for _, count := range counts {
		if count > capacity {
			capacity = count
		}
	}
	if capacity > 0 && max > capacity {
		max = capacity
	}
	return min, max, capacity, true
}

func (e *Engine) oneEachTargetBounds(sa *cards.SA, candidates []targetCandidate, min, max int) (int, int, bool, int) {
	if !targetControllerExclusive(sa) {
		return min, max, false, 0
	}
	// Option.Group makes the one-per-controller restriction part of the
	// generic decision contract, so every target API consumes the same
	// enforcement rather than each effect maintaining a picker.
	groups := map[state.PlayerID]bool{}
	for _, candidate := range candidates {
		groups[e.candidateControllerSeat(candidate)] = true
	}
	distinct := len(groups)
	// OneEach respells a bound as the distinct-controller count. It is the
	// TargetsForEachPlayer$ spelling in Forge's grammar, but the corpus also
	// writes it on a TargetsWithDifferentControllers$ SA (Mysterious
	// Stranger's "for each player" graveyard pick), where it means the same
	// thing -- so the respell is driven by the VALUE, not by which of the two
	// equivalent flags is present. Before this only the
	// TargetsForEachPlayer$ spelling was read and Mysterious Stranger asked
	// for ONE target (Min 1 / Max 1) instead of one per represented player.
	if strings.EqualFold(sa.Params["TargetMin"], "OneEach") {
		min = distinct
	}
	if strings.EqualFold(sa.Params["TargetMax"], "OneEach") {
		max = distinct
	}
	// Cap the maximum at the distinct-controller count for BOTH shapes: a
	// literal or dynamic TargetMax$ larger than the number of controllers
	// present could only invite an answer the exclusivity rule rejects, so
	// the offer must advertise the true capacity (Havoc Eater's TargetMax$ X,
	// Protector of the Wastes' TargetMax$ 2). distinct is 0 only when there
	// are no candidates at all, and the no-option paths handle that before a
	// decision is built, so the max >= 1 clamp contract is preserved.
	if distinct > 0 && max > distinct {
		max = distinct
	}
	return min, max, true, distinct
}

// targetControllerExclusive reports whether this targeting SA carries Forge's
// per-controller selection shape -- TargetsForEachPlayer$ True ("up to one
// target each player controls", the OneEach family) or
// TargetsWithDifferentControllers$ True ("targets controlled by different
// players", Protector of the Wastes and 7 more corpus carriers). Both are the
// SAME set constraint -- no two chosen targets may share a controller -- and
// both are expressed on the wire by Option.Group, so one predicate backs both
// and the resolution recheck reads it through the same helper.
func (e *Engine) narrowSameController(targets []state.Target) []state.Target {
	if len(targets) < 2 {
		return targets
	}
	controller := targets[0]
	seat, ok := e.targetControllerSeat(controller)
	if !ok {
		return targets
	}
	out := targets[:0]
	for _, target := range targets {
		if got, valid := e.targetControllerSeat(target); valid && got == seat {
			out = append(out, target)
		}
	}
	return out
}

func targetControllerExclusive(sa *cards.SA) bool {
	return strings.EqualFold(sa.Params["TargetsForEachPlayer"], "True") ||
		strings.EqualFold(sa.Params["TargetsWithDifferentControllers"], "True")
}

// targetControllerGroup is the Option.Group label binding one selection slot
// to its controller -- the same label both ask sites attach, so
// Decision.Validate's mutual-exclusion rule enforces one pick per controller
// on the wire. It applies whenever targetControllerExclusive holds, so a
// TargetsWithDifferentControllers$ ask restricts the SAME way a OneEach ask
// does: the exclusivity is the generic wire contract's job, not each effect's.
func (e *Engine) targetControllerGroup(sa *cards.SA, candidate targetCandidate) string {
	if !targetControllerExclusive(sa) {
		return ""
	}
	return "target-controller-" + strconv.Itoa(int(e.candidateControllerSeat(candidate)))
}

// targetControllerSeat is the controller seat a chosen target keys the
// per-controller constraint on, read at the resolution recheck: a player
// target is its own seat; an object target is its controller (its owner for a
// card in a graveyard/hand/exile, which apply.go's Move fold keeps equal to
// Controller). It is the recheck's twin of targetControllerGroup, which reads
// the same seat off a candidate at the offer (candidatesFor labels a
// non-battlefield candidate with the zone slice's owner q, and Move sets
// Controller = Owner there), so offer and recheck cannot disagree. ok is false
// for a target whose seat is unknown (a vanished object), which the caller
// keeps rather than duplicating a phantom controller.
func (e *Engine) targetControllerSeat(t state.Target) (state.PlayerID, bool) {
	if t.IsPlayer {
		if int(t.Player) < len(e.G.Players) && !e.G.Players[t.Player].Lost {
			return t.Player, true
		}
		return 0, false
	}
	if o := e.G.Obj(t.Obj); o != nil {
		return o.Controller, true
	}
	return 0, false
}

// narrowDifferentControllers is CR 608.2b's "does as much as possible" read of
// TargetsWithDifferentControllers$ at resolution: walk the still-legal targets
// in recorded order and keep the first of each controller, dropping a later
// target whose controller already appears. A controller change in response can
// make two targets share a controller, and a resolution must never act on a
// set the targeting requirement forbids; keeping the earliest chosen target is
// the deterministic, order-stable reading. It is applied only to the flag-bearing
// SA, after the ordinary per-target legality recheck, so it only ever REMOVES a
// target -- it can never widen a set the per-target filter already narrowed.
func (e *Engine) narrowDifferentControllers(targets []state.Target) []state.Target {
	seen := map[state.PlayerID]bool{}
	out := targets[:0:0]
	for _, t := range targets {
		seat, ok := e.targetControllerSeat(t)
		if ok && seen[seat] {
			continue
		}
		if ok {
			seen[seat] = true
		}
		out = append(out, t)
	}
	return out
}

// maxTotalTargetPower resolves a targeting subject's MaxTotalTargetPower$
// cap -- the running total-power bound over a multi-target selection
// ("Return any number of target creature cards with total power 10 or less",
// Reunion of the House and Nethroi, Apex of Death; 2 corpus files). A literal
// token reads directly (including a literal 0 or negative, both enforceable:
// the ask sites attach every present cap as the decision's budget with
// Decision.Budgeted set, because a MaxSum of 0 alone reads as NO budget on
// the wire); a dynamic token resolves through the
// effects numeric grammar, the same reader resolvedTargetBounds applies to a
// TargetMax$ X token, bound to the asking player and the source anchor. A
// token the grammar cannot resolve returns ok=false -- the cap is then
// simply not enforced (today's behaviour; measured, no corpus carrier
// reaches this arm unresolvable, both carriers are the literal 10).

func (e *Engine) maxTotalTargetPower(p state.PlayerID, source state.ObjID, sa *cards.SA, x int32) (int, bool) {
	v, ok := sa.Params["MaxTotalTargetPower"]
	if !ok {
		return 0, false
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		return n, true
	}
	ctx, okc := e.targetBoundCtx(p, source)
	if !okc {
		return 0, false
	}
	ctx.X = x
	if n, resolved := effects.NumResolved(e, ctx, sa, "MaxTotalTargetPower", 0); resolved {
		return int(n), true
	}
	return 0, false
}

// totalPowerCappedCandidates applies the MaxTotalTargetPower$ cap to a
// target census. The per-option half is a real filter, but ONLY where a
// candidate provably cannot join any legal selection: with every power
// non-negative, a candidate whose power ALONE exceeds the cap busts every
// set containing it and is pruned. A NEGATIVE-power candidate breaks that
// argument -- a CDA can be negative in a zone (Scourge of the Skyclaves's
// 20-minus-highest-life CDA is -1 at a 21-life opponent, and it applies in
// EVERY zone per CR 208.2), and 11 + (-1) = 10 is a legal compensated
// selection under a cap of 10 -- so when any candidate reads negative the
// over-cap candidate is pruned only when even the maximal offset cannot
// save it: no selection containing it can score under the cap unless it
// takes EVERY other negative candidate on offer, so the prune test is
// p + otherNeg > cap (otherNeg = the sum of the OTHER candidates' negative
// powers). TargetMax$ bounds the offset: a selection containing the
// candidate under test can take at most maxTargets-1 OTHER things (CR
// 601.2c), so the offset is the sum of the most negative maxTargets-1
// candidates, never the whole negative sum. Both corpus carriers' TargetMax$
// is X (every candidate), where the ceiling is unreachable and the offset is
// the whole negative sum exactly as before; a lone-target ask (the default
// TargetMax$ 1) has no offset at all. A survivor the TargetMax$-aware prune
// keeps is always selectable alongside the negatives it counted, and
// Decision.Validate still rejects any over-cap answer.
// The running half -- any combination whose summed power stays within the
// bound -- is NOT expressible as a per-candidate property, so it is not a
// filter here: the two ask sites (askTarget below and cast.go's targetAsk)
// attach it to the decision as the cumulative-budget wire contract --
// Decision.MaxSum over each object option's Value (the candidate's power,
// negatives included) -- which Decision.Validate enforces on every
// submitted answer and Clamp/decision.FitRequired mirror for the
// deterministic bot. That is the same mechanism a Dig's WithTotalCMC$
// budget uses, so what a client may submit and what the engine offered can
// never disagree. Player candidates carry Value 0 and are never pruned (a
// MaxTotalTargetPower$ ask names cards; a player's presence is free).
// Returns the pruned census, the cap and whether the parameter is present
// at all.
//
// The power read is the DERIVED power (Engine.Power), not the printed
// face: a characteristic-defining P/T applies in EVERY zone (CR 208.2 --
// Lord of Extinction counts the graveyards from its own graveyard, which
// derivedScalarFrom's CDA read covers), and the printed Face().Power()
// returns 0 for such a face -- the first cut of this read undercounted a
// CDA creature as a free target.
func (e *Engine) totalPowerCappedCandidates(candidates []targetCandidate, p state.PlayerID, source state.ObjID, sa *cards.SA, x int32) ([]targetCandidate, int, bool) {
	capPower, ok := e.maxTotalTargetPower(p, source, sa, x)
	if !ok {
		return candidates, 0, false
	}
	// First pass: every card candidate's DERIVED power, plus the census's
	// negative powers sorted most-negative first (the pool the offset draws
	// from). Player candidates carry no power.
	power := make([]int32, len(candidates))
	var negs []int32
	for i, c := range candidates {
		if c.kind == "player" {
			continue
		}
		o := e.G.Obj(c.obj)
		if o == nil || o.Face() == nil {
			continue
		}
		power[i] = e.Power(c.obj)
		if power[i] < 0 {
			negs = append(negs, power[i])
		}
	}
	sort.Slice(negs, func(a, b int) bool { return negs[a] < negs[b] })
	// A selection containing the candidate under test has at most this many
	// OTHER slots (CR 601.2c), so its offset may draw on at most that many
	// negative candidates. resolvedTargetBounds clamps max >= 1, so a
	// lone-target ask (the default) has otherSlots 0 and no offset at all.
	_, maxTargets := e.resolvedTargetBounds(p, source, sa, x)
	otherSlots := maxTargets - 1
	if otherSlots < 0 {
		otherSlots = 0
	}
	out := make([]targetCandidate, 0, len(candidates))
	for i, c := range candidates {
		if c.kind == "player" {
			out = append(out, c)
			continue
		}
		o := e.G.Obj(c.obj)
		if o == nil || o.Face() == nil {
			continue
		}
		// Prune only a candidate no legal selection can contain: its own
		// power plus the maximal offset the OTHER selectable candidates can
		// supply (the most negative maxTargets-1 of them) still busts the
		// cap. With no negatives that is the plain p > cap prune; a candidate
		// at or under the cap is never pruned by it when cap >= 0. The same
		// rule holds for a cap of zero or less (powers 2,-1,-1 under a cap of
		// 0 with maxTargets >= 3 total 0, so the 2 stays; under maxTargets 2
		// only one -1 fits, 2-1 = 1 > 0, so the 2 is pruned), and it can
		// prune the whole census. Whatever survives, taking the negatives the
		// offset counted alongside it fits within both the cap and Max, which
		// is what decision.FitRequired's negative top-up relies on to always
		// reach a valid answer.
		p := power[i]
		others := maxNegativeOffset(negs, otherSlots, p < 0, p)
		if p+others > int32(capPower) {
			continue
		}
		out = append(out, c)
	}
	return out, capPower, true
}

// maxNegativeOffset is the most a selection of at most `slots` candidates can
// claw back from `negs` (negative powers sorted most-negative first), skipping
// one occurrence of skipVal when the candidate under test is itself negative
// and so already contributes its own power to the sum. A missing skipVal or a
// non-positive slots yields the plain top-slots sum (0 when slots <= 0). The
// caller's census is small, so the linear scan per candidate is kept simple
// rather than prefix-summed.
func maxNegativeOffset(negs []int32, slots int, skip bool, skipVal int32) int32 {
	if slots <= 0 {
		return 0
	}
	sum := int32(0)
	taken := 0
	skipped := !skip
	for _, v := range negs {
		if !skipped && v == skipVal {
			skipped = true
			continue
		}
		if taken >= slots {
			break
		}
		sum += v
		taken++
	}
	return sum
}

// AskCopyTargets offers CR 707.10c's new-target choice for the copy on top
// of the stack. It is driven entirely by the copy's own
// CopyMayChooseTarget flag -- set by the StackCopy fold from the CREATING
// CopySpellAbility's MayChooseTarget$ parameter, so an external copier
// (Mirari, Cloven Casting, a Storm or Replicate copy) grants the election
// even though its SA is not part of the copied spell's text.
//
// The ask preserves the copied spell's WHOLE target requirement, not just
// one slot: MayChooseTarget$ True is not restricted to one-target spells, so
// the decision's bounds come from the copy's own declaration through the
// SAME resolvedTargetBounds / oneEachTargetBounds pair askTarget and cast.go's
// targetAsk use (a two-target spell therefore accepts two picks, and a
// per-controller declaration keeps its Option.Group exclusivity). Every
// inherited target is offered first as a keep-current option -- ALWAYS, even
// when it is no longer legal, because choosing new targets is optional and a
// player who keeps an illegal target simply lets the copy fizzle per CR
// 608.2b (forcing a new target here would retarget a copy the player declined
// to change) -- so selecting the leading keep-current options reproduces
// "choose nothing new". The remaining options use the same legal-target
// census as casting. The election is one-shot: the answer records targets
// through recordChosenTargets, whose TargetsChosen fold clears the flag, so
// the resolveTop re-entry does not ask again.
//
// A copy whose spell has MORE THAN ONE target DECLARATION -- the two halves
// of a Fuse cast, or each target-bearing mode of a modal spell -- is asked
// ONE DECISION PER DECLARATION, in the same
// order the cast asked them (castStageSA / castHasNextTargetStage), so each
// declaration is offered its own legal set, its own bounds and its own
// per-controller Option.Group. The earlier single-list shape flattened every
// declaration into one pool, so a half's own ValidTgts$ was never applied to
// its half's choices (a fused Wear // Tear offered only artifacts and never
// the enchantment the alternate half demands). A copy inherits the cast's
// announcement and target provenance; an illegal inherited target remains
// in its original declaration rather than migrating to a later stage. The
// in-progress stage is
// tracked in Engine.copyTargetStage keyed on the copy's stack object, so the
// second-stage ask survives the TargetsChosen fold that clears
// CopyMayChooseTarget after the first declaration.
func (e *Engine) AskCopyTargets() bool {
	n := len(e.G.Stack)
	if n == 0 {
		return false
	}
	o := e.G.Obj(e.G.Stack[n-1])
	if o == nil || !o.IsCopy {
		return false
	}
	stage := 0
	if e.copyTargetStage != nil {
		stage = e.copyTargetStage[o.ID]
	}
	// The one-shot election is pending while the flag is set OR while a
	// multi-declaration ask is mid-flight (the first answer's TargetsChosen
	// fold clears the flag, but later declarations still owe an ask).
	if !o.CopyMayChooseTarget && stage == 0 {
		return false
	}
	controller := o.Controller
	decls := e.copyTargetDeclarations(o)
	if stage >= len(decls) {
		return false
	}
	sa := decls[stage]
	if sa == nil || strings.TrimSpace(sa.Params["ValidTgts"]) == "" {
		return false
	}
	candidates := e.legalTargetCandidates(controller, o.ID, o.ID, sa)
	// MaxTotalTargetPower$ (Reunion of the House): re-run the cast ask's
	// per-candidate prune here, so a copy of a power-capped multi-target spell
	// offers the same census the cast did. The running budget rides the
	// decision as Decision.MaxSum over Option.Value (below), exactly as the
	// cast ask attaches it.
	candidates, powerCap, powerCapped := e.totalPowerCappedCandidates(candidates, controller, o.ID, sa, o.X)
	// Keep the original declaration's targets, including ones that have since
	// become illegal. The StackCopy emission inherits the cast's stage split;
	// when no split exists, assign flat targets by declaration position, never
	// by current legality.
	inherited := e.copyInheritedForDeclaration(o, decls, stage)
	ordered := make([]targetCandidate, 0, len(candidates)+len(inherited))
	for _, old := range inherited {
		matched := -1
		for i, candidate := range candidates {
			if targetCandidateEqual(old, candidate) {
				matched = i
				break
			}
		}
		if matched >= 0 {
			ordered = append(ordered, candidates[matched])
			candidates = append(candidates[:matched], candidates[matched+1:]...)
		} else {
			// Keep-current even though the target is no longer legal: the
			// player may decline new targets (CR 707.10c), and the copy
			// then fizzles at CR 608.2b.
			ordered = append(ordered, stateTargetCandidate(old))
		}
	}
	ordered = append(ordered, candidates...)
	if len(ordered) == 0 {
		return false
	}
	// The copy's OWN declaration supplies the required count (CR 707.10c
	// retargets a copy per the spell's target rules), through the same shared
	// readers the cast ask uses so the two sites cannot drift. oneEachTargetBounds
	// is fed the full selectable list -- keep-current slots included -- so the
	// per-controller capacity counts a kept target too.
	min, max := e.resolvedTargetBounds(controller, o.ID, sa, o.X)
	min, max, _, _ = e.oneEachTargetBounds(sa, ordered, min, max)
	// A mandatory minimum above the offered list would be an unanswerable
	// decision no seat could satisfy (a livelock). The inherited keep-current
	// entries are always offered even when illegal, so the list is non-empty;
	// clamping Min to it keeps totality whenever a dynamic bound outruns the
	// copy's inherited set (the copy then fizzles at CR 608.2b like any other
	// under-target resolution).
	if min > len(ordered) {
		min = len(ordered)
	}
	if max < min {
		max = min
	}
	// A declaration whose resolved bound admits NO target (Min 0 Max 0 -- a
	// dynamic TargetMax$ that evaluated to zero) leaves nothing to choose:
	// the only legal answer is the empty one, which Engine.ask refuses to
	// post. Resolve it silently -- the copy keeps its inherited (empty)
	// set for this declaration -- and move on to the next declaration, if
	// any, exactly as an answered ask would.
	if max == 0 {
		if e.copyTargetStage == nil {
			e.copyTargetStage = make(map[state.ObjID]int)
		}
		e.copyTargetStage[o.ID] = stage + 1
		return e.AskCopyTargets()
	}
	d := &decision.Decision{Player: controller, Kind: decision.KTarget, Min: min, Max: max,
		Prompt: "Choose a new target for the copy", Source: o.ID,
		ResumeKind: "copy_targets", ResumeSA: sa, TargetEffect: e.describeTargetEffect(controller, o.ID, sa, o.X)}
	for _, candidate := range ordered {
		opt := decision.Option{Index: len(d.Options), Kind: candidate.kind,
			Label: e.targetOptionLabel(candidate), Obj: candidate.obj, Player: candidate.player,
			Group: e.targetControllerGroup(sa, candidate)}
		opt.Controller = e.candidateControllerSeat(candidate)
		// Option.Value is read only under a budget (Decision.HasBudget), so a
		// budget-less copy ask keeps its wire payload byte-identical. The
		// value is the DERIVED power (Engine.Power), matching the prune read.
		if powerCapped && candidate.kind != "player" {
			if co := e.G.Obj(candidate.obj); co != nil && co.Face() != nil {
				opt.Value = int(e.Power(candidate.obj))
			}
		}
		d.Options = append(d.Options, opt)
	}
	if powerCapped {
		d.MaxSum, d.Budgeted = powerCap, true
	}
	// Record the stage BEFORE the ask is answered: the answer's TargetsChosen
	// clears the one-shot flag, so the resolveTop re-entry learns from this
	// scratch that the next declaration (if any) still owes an ask.
	if e.copyTargetStage == nil {
		e.copyTargetStage = make(map[state.ObjID]int)
	}
	e.copyTargetStage[o.ID] = stage + 1
	e.Ask(d)
	return true
}

// copyTargetDeclarations returns the target DECLARATIONS a copy must ask, in
// cast order. A Fuse copy has two: the front half's spell ability and the
// alternate half's (split.go's fusedSplitFaces), each with its own ValidTgts$
// and therefore its own legal set. For a chosen-mode Charm, each selected
// target-bearing mode is a separate declaration in chosen order. A declaration
// with no ValidTgts$ is dropped.
func (e *Engine) copyTargetDeclarations(o *state.Object) []*cards.SA {
	if o == nil {
		return nil
	}
	if o.Ability != nil {
		if src := e.G.Obj(o.Source); src != nil && src.Face() != nil {
			if modes := copyCharmModes(src.Face(), o.Ability, o.ChosenModes); len(modes) > 0 {
				return modes
			}
		}
		return []*cards.SA{o.Ability}
	}
	f := o.Face()
	if f == nil {
		return nil
	}
	if ff, fa := fusedSplitFaces(o); ff != nil && fa != nil {
		out := make([]*cards.SA, 0, 2)
		for _, hf := range []*cards.Face{ff, fa} {
			if modes := copyCharmModes(hf, hf.SpellAbility(), o.ChosenModes); len(modes) > 0 {
				out = append(out, modes...)
			} else if sa := hf.SpellAbility(); sa != nil && strings.TrimSpace(sa.Params["ValidTgts"]) != "" {
				out = append(out, sa)
			}
		}
		return out
	}
	if modes := copyCharmModes(f, f.SpellAbility(), o.ChosenModes); len(modes) > 0 {
		return modes
	}
	sa := modalTargetSA(f, f.SpellAbility(), o.ChosenModes)
	if sa == nil {
		return nil
	}
	return []*cards.SA{sa}
}

func copyCharmModes(f *cards.Face, sa *cards.SA, names []string) []*cards.SA {
	if f == nil || sa == nil || sa.API != "Charm" || len(names) == 0 || sa.Params["ValidTgts"] != "" {
		return nil
	}
	var out []*cards.SA
	for _, name := range names {
		if sub := cards.ResolveSVar(f.SVars, name); sub != nil && strings.TrimSpace(sub.Params["ValidTgts"]) != "" {
			out = append(out, sub)
		}
	}
	return out
}

// copyInheritedForDeclaration returns the targets owned by this declaration.
// Cast-time provenance survives StackCopy in fuseTargets. For copies without
// provenance, flat targets are divided by declaration bounds in cast order;
// legality must never be used to infer ownership after the board changes.
func (e *Engine) copyInheritedForDeclaration(o *state.Object, decls []*cards.SA, stage int) []state.Target {
	if o == nil || len(o.Targets) == 0 {
		return nil
	}
	if len(decls) <= 1 {
		return o.Targets
	}
	if stages, ok := e.fuseTargets[o.ID]; ok {
		index := stage
		if ff, _ := fusedSplitFaces(o); ff != nil {
			if sa := ff.SpellAbility(); sa == nil || strings.TrimSpace(sa.Params["ValidTgts"]) == "" {
				index++ // the first fused half has no target declaration
			}
		}
		if index < len(stages) {
			return stages[index]
		}
		return nil
	}
	start := 0
	for i, sa := range decls {
		end := len(o.Targets)
		if i < len(decls)-1 {
			_, max := e.resolvedTargetBounds(o.Controller, o.ID, sa, o.X)
			if max < 0 {
				max = 0
			}
			if end > start+max {
				end = start + max
			}
		}
		if i == stage {
			return o.Targets[start:end]
		}
		start = end
	}
	return nil
}

// stateTargetCandidate converts a recorded state.Target into the option shape
// the target census uses. The kind is only the wire label; a player target
// keeps "player" and every object target is offered as "permanent" (the
// label reads the object's own name, so a target that has left the
// battlefield still renders correctly).
func stateTargetCandidate(t state.Target) targetCandidate {
	if t.IsPlayer {
		return targetCandidate{kind: "player", player: t.Player}
	}
	return targetCandidate{kind: "permanent", obj: t.Obj}
}

func targetCandidateEqual(t state.Target, c targetCandidate) bool {
	if t.IsPlayer {
		return c.kind == "player" && c.player == t.Player
	}
	return c.kind != "player" && c.obj == t.Obj
}

// targetChooserCore is the ONE home for the TargetingPlayer$ redirect at the
// RULES-TIER ask sites: it reads the parameter off sa and resolves the
// referent against the stored trigger context (trigger-relative referents)
// or the living-seat table (the non-triggered Opponent form). Both the
// rules-tier ask sites (targetAskChooser) and the effects-tier mid-resolution
// asks (Engine.ChooserFor and OpponentPickAsk, which effects.Host calls)
// reach it, so the cast, activation, trigger, resolution-sub and
// mid-resolution ValidTgts$ (mvts1 "tgts" and ChangeZone "choice") paths
// cannot drift.
//
// Target LEGALITY is unaffected by every outcome: the caller keeps the
// ability controller as the reference for legalTargetCandidates /
// targetSpecContext, and only the decision's Player moves to the chooser.
//
// The third return (pick) is the multi-opponent Opponent form's selection
// signal: with two or more living opponents the CONTROLLER must first choose
// which of them answers, through the controller-facing "opp_pick" ask
// (poseOpponentPick for the rules tier, OpponentPickAsk for the effects
// tier). ok is then false and the caller poses that selection ask instead of
// the target ask; the answer re-poses the target ask to the chosen seat via
// the pin this function reads. A sole living opponent answers directly with
// no ask; a controller with no living opponent fails closed to the
// controller.
//
// Returns (controller, false, false) when sa names no chooser, or the spec
// is unknown, unbound or dead, so the ask stays with the controller.
func (e *Engine) targetChooserCore(controller state.PlayerID, remembered []state.Target, tc effects.TriggerContext, source state.ObjID, sa *cards.SA) (state.PlayerID, bool, bool) {
	if sa == nil {
		return controller, false, false
	}
	spec := strings.TrimSpace(sa.Params["TargetingPlayer"])
	if spec == "" {
		return controller, false, false
	}
	if spec == "Opponent" || spec == "Player.Opponent" {
		// The rules-tier selection pin (answerOppPick's answer, consumed by
		// the re-posed ask): the answered ask matched on the same source
		// object and SA line.
		if s := e.oppSel; s.done && s.line == sa.Line && s.source == source {
			e.oppSel = oppSelectState{}
			return s.player, true, false
		}
		return e.opponentPicker(controller)
	}
	who, ok := e.targetChooserFromSpec(spec, controller, remembered, tc)
	return who, ok, false
}

// opponentPicker resolves the Opponent form's seat from the controller's
// living-opponent table: no living opponent fails closed to the controller, a
// sole living opponent answers directly (an ask nobody could answer
// differently is never posed), and two or more owe the controller's
// which-opponent selection (pick=true).
func (e *Engine) opponentPicker(controller state.PlayerID) (state.PlayerID, bool, bool) {
	opponents := e.livingOpponents(controller)
	switch len(opponents) {
	case 0:
		return controller, false, false
	case 1:
		return opponents[0], true, false
	}
	return controller, false, true
}

// livingOpponents lists the controller's living opponents in AliveFrom(0)
// turn order -- the same order the former deterministic first-opponent
// contract scanned, so a deterministic answerer (the bot's first-option
// clamp) still names the seat the old contract named.
func (e *Engine) livingOpponents(controller state.PlayerID) []state.PlayerID {
	var out []state.PlayerID
	for _, p := range e.G.AliveFrom(0) {
		if p != controller {
			out = append(out, p)
		}
	}
	return out
}

// tpControlState is the TargetingPlayerControls$ restriction's resolution
// state (tpc1): Forge's `TargetingPlayerControls$ True` makes every target
// OBJECT of the SA a battlefield permanent controlled by the answering
// TargetingPlayer$ seat (Evangelize: "target creature of an opponent's
// choice they control"), without touching ValidTgts$'s own
// controller-relative reading (YouCtrl, hexproof, protection stay judged
// from the ability controller).
type tpControlState int

const (
	// tpNone: the SA carries no TargetingPlayerControls$ True -- candidates
	// and targets are judged exactly as before.
	tpNone tpControlState = iota
	// tpResolved: the answering seat is known -- the answered ask's record,
	// either tier's answered selection pin, a sole living opponent, a bound
	// trigger-relative referent, or the fail-closed controller.
	tpResolved
	// tpPending: the multi-opponent Opponent form's which-opponent selection
	// is still owed, so the answering seat is not known yet. The offer
	// census admits the union over the seats the selection may name (the
	// same set the selection ask offers), so the offer never lies about
	// feasibility; the eventual selection re-poses the ask with the exact
	// seat (the pin reads above) and an empty exact set falls to the
	// ordinary CR 608.2b/733.1 reversal machinery.
	tpPending
)

// tpCtlAnswer is one TargetingPlayerControls$ answered-ask record: the SA
// line whose ask was answered and the seat that answered it. Plain scalars,
// so Clone carries the store like the oppSel class.
type tpCtlAnswer struct {
	line   string
	player state.PlayerID
}

// targetControlsChooser resolves the TargetingPlayerControls$ restriction
// for a target census (candidatesForLimit's post-filter) or a CR 608.2b
// recheck (legalTargets). It shares the chooser derivation targetChooserCore
// owns -- the answered selection pins of both tiers, the sole-opponent and
// fail-closed shapes, the trigger-relative referent grammar -- plus the
// answered-ask record (tpCtlChooser), so the offer and the recheck cannot
// disagree about who "they" is (Critical C2's one-definition rule).
// p stays the ability CONTROLLER: target legality is judged from it exactly
// as the ValidTgts$ filter judges it; only the restriction's controller
// binding moves to the answering seat. The pending state is returned only
// for the Opponent form: every other TargetingPlayer$ referent resolves
// deterministically or fails closed.
func (e *Engine) targetControlsChooser(p state.PlayerID, source state.ObjID, sa *cards.SA) (state.PlayerID, tpControlState) {
	if sa == nil || !strings.EqualFold(strings.TrimSpace(sa.Params["TargetingPlayerControls"]), "True") {
		return 0, tpNone
	}
	// The answered ask's record first: once a target decision for this
	// (object, line) was answered, the answering seat is what every later
	// census and recheck reads -- the selection pins are consumed between
	// the ask and the recheck (the rules-tier one by the re-posed ask's own
	// targetChooserCore, the mid-tier one by OpponentPickAsk), so a pin
	// read must not be able to change the answer afterwards.
	if rec, ok := e.tpCtlChooser[source]; ok && (sa.Line == "" || rec.line == "" || rec.line == sa.Line) {
		return rec.player, tpResolved
	}
	spec := strings.TrimSpace(sa.Params["TargetingPlayer"])
	if spec == "Opponent" || spec == "Player.Opponent" {
		// The mid-tier answered selection (oppPicksMid, keyed by the SA's
		// line): present between the "opp_pick" resume arm and the
		// synchronous re-entry that poses the target ask -- exactly the
		// window this census runs in (chosenTargetsFor builds its candidates
		// before ChooserFor/OpponentPickAsk consume the entry).
		if sa.Line != "" {
			if who, ok := e.oppPicksMid[sa.Line]; ok {
				return who, tpResolved
			}
		}
		// The rules-tier selection pin, read WITHOUT consuming it:
		// targetChooserCore owns the consume, at the same re-posed ask this
		// census has just filtered for (candidates are built before
		// targetAskChooser runs, so the pin is still set here).
		if s := e.oppSel; s.done && s.line == sa.Line && s.source == source {
			return s.player, tpResolved
		}
		opponents := e.livingOpponents(p)
		switch len(opponents) {
		case 0:
			// targetChooserCore's fail-closed shape: no living opponent keeps
			// the ask with the controller, so "they" is the controller.
			return p, tpResolved
		case 1:
			return opponents[0], tpResolved
		}
		return 0, tpPending
	}
	// Trigger-relative referents (TriggeredTarget, TriggeredPlayer, ...):
	// the same referent grammar targetChooserCore resolves through, against
	// the trigger context stored for the asking object. An unknown, unbound
	// or dead referent fails closed to the controller, the same shape
	// targetChooserCore keeps the ask with the controller for.
	tc := effects.TriggerContext{}
	if ctx, ok := e.triggerContexts[source]; ok {
		tc = ctx
	}
	who, ok := e.targetChooserFromSpec(spec, p, nil, tc)
	if !ok {
		return p, tpResolved
	}
	return who, tpResolved
}

// tpControlsAdmits judges one OBJECT target against the restriction state.
// The object must be a battlefield permanent (control exists nowhere else)
// and, once the answering seat is resolved, controlled by that seat. While
// the multi-opponent selection is still owed the census admits a permanent
// of any seat the selection may name -- the union over the controller's
// living opponents, the same set the selection ask offers. A non-battlefield
// object (a stack spell, a graveyard card) is never a legal target of a
// TargetingPlayerControls$ ask.
func (e *Engine) tpControlsAdmits(o *state.Object, p, chooser state.PlayerID, st tpControlState) bool {
	if st == tpNone {
		return true
	}
	if o == nil || o.Zone != state.ZBattlefield {
		return false
	}
	if st == tpResolved {
		return o.Controller == chooser
	}
	for _, q := range e.livingOpponents(p) {
		if o.Controller == q {
			return true
		}
	}
	return false
}

// filterTargetingPlayerControls applies TargetingPlayerControls$ True to a
// target census (tpc1): every OBJECT candidate must be a battlefield
// permanent controlled by the answering TargetingPlayer$ seat. Player
// candidates are out of the parameter's scope and stay. It runs last in
// candidatesForLimit's post-filter chain and only on the targeting census
// (the Overload affected sweep keeps its non-target semantics).
func (e *Engine) filterTargetingPlayerControls(out []targetCandidate, sa *cards.SA, p state.PlayerID, source state.ObjID) []targetCandidate {
	if len(out) == 0 {
		return out
	}
	chooser, st := e.targetControlsChooser(p, source, sa)
	if st == tpNone {
		return out
	}
	filtered := out[:0]
	for _, c := range out {
		if c.kind == "player" || e.tpControlsAdmits(e.G.Obj(c.obj), p, chooser, st) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// recordTpControlsChooser pins the seat that ANSWERED a target ask whose SA
// carries TargetingPlayerControls$ True (tpc1), keyed by the RESOLVING
// stack object -- the spell object for a cast (pc.stackObj), the
// AbilityPush-minted object for an activation, the TriggerPush object for a
// placement ask. The CR 608.2b recheck (legalTargets) reads the same key
// through its self parameter, so the restriction is judged against exactly
// the seat the ask was answered by, never re-derived past the selection
// pins' short lifetimes. The entry lives from the answer to the object
// leaving the stack (the zone-change clear), so the next targeting cycle on
// a fresh object never sees a stale seat.
func (e *Engine) recordTpControlsChooser(obj state.ObjID, sa *cards.SA, chooser state.PlayerID) {
	if sa == nil || obj == 0 || !strings.EqualFold(strings.TrimSpace(sa.Params["TargetingPlayerControls"]), "True") {
		return
	}
	if e.tpCtlChooser == nil {
		e.tpCtlChooser = make(map[state.ObjID]tpCtlAnswer)
	}
	e.tpCtlChooser[obj] = tpCtlAnswer{line: sa.Line, player: chooser}
}

// targetAskChooser resolves who answers a target ask declared by sa at a
// rules-tier ask site. Forge's TargetingPlayer$ names another player as the
// chooser; the trigger-relative grammar in targetChooserFromSpec resolves
// those referents from the stored trigger context, while the non-triggered
// cast/activation form names an opponent (with the multi-opponent selection
// ask owed by the controller -- see targetChooserCore's pick return). It
// delegates to targetChooserCore, the shared home, and is called by askTarget
// (trigger placement and resolution sub-abilities, this file), targetAsk (CR
// 601.2c cast and activation targeting, rules/cast.go) and subTargetAsk
// (chained sub-abilities). The effects-tier mid-resolution asks reach the
// same core through Engine.ChooserFor and Engine.OpponentPickAsk.
//
// Returns (controller, false, false) when sa names no chooser, or the spec
// is unknown, unbound or dead, so the ask stays with the controller.
func (e *Engine) targetAskChooser(controller state.PlayerID, source state.ObjID, sa *cards.SA) (state.PlayerID, bool, bool) {
	tc := effects.TriggerContext{}
	if triggerContext, ok := e.triggerContexts[source]; ok {
		tc = triggerContext
	}
	return e.targetChooserCore(controller, nil, tc, source, sa)
}

// chooseOppPick is the chooseFor for the TargetingPlayer$ Opponent
// controller-selection ask (poseOpponentPick). Keep its value distinct from
// every other chooseFor flow; handleChoose dispatches on these values.
const chooseOppPick chooseFor = 47

// oppPickStage names which rules-tier flow posed a TargetingPlayer$
// Opponent selection ask, so answerOppPick re-poses the right target ask.
type oppPickStage string

const (
	// oppPickTarget: the askTarget site (trigger placement, resolution-sub
	// placement, handleModes' modal sub asks).
	oppPickTarget oppPickStage = "target"
	// oppPickCastRoot: the CR 601.2c root cast/activation ask (targetAsk).
	oppPickCastRoot oppPickStage = "cast_root"
	// oppPickCastSub: the chained sub-ability cast-time pre-ask
	// (subTargetAsk, ResumeKind "cast_sub").
	oppPickCastSub oppPickStage = "cast_sub"
)

// oppSelectState is the engine-side record of an outstanding or answered
// TargetingPlayer$ Opponent selection ask at a rules-tier site: set by
// poseOpponentPick when the controller's which-opponent ask is posted,
// flipped to done by answerOppPick when the controller names a seat, and
// consumed by targetChooserCore when the re-posed target ask reads it. Plain
// scalars only, so Clone carries it like the blockerRound class and a
// snapshot boundary mid-ask re-poses the same selection.
type oppSelectState struct {
	stage  oppPickStage
	ctl    state.PlayerID
	source state.ObjID
	line   string
	done   bool
	player state.PlayerID
}

// oppPicksMid carries the mid-resolution tier's answered selections, keyed by
// the asking SA's line: the "opp_pick" resume arm (rules/resolution.go)
// records the controller's chosen opponent there and the re-entered walk's
// ChooserFor consumes it when it re-derives the chooser. Lines are unique
// per SVar body, the pin only lives between the arm and the synchronous
// re-entry that reads it, and the read deletes it, so no entry can outlive
// the ask it belongs to.

// poseOpponentPick poses the controller's which-opponent selection ask for a
// TargetingPlayer$ Opponent target ask at a rules-tier site: one "player"
// option per living opponent, Min/Max 1, answered by the CONTROLLER (never
// by a target candidate). The caller has already established that the target
// ask itself would be posted (feasibility ran first), so the selection ask
// replaces it one-for-one and the target ask is re-posed by answerOppPick
// once the seat is named. The pending KChoose is routed through the
// chooseOppPick flow marker (handleChoose), NOT the mid-resolution resume
// machinery: a cast begun inside a suspended resolution (a Miracle cast in
// the trigger drain) parks the resolution's own resume point, and a resume
// dispatch would consume the selection answer as the resolution's ask
// answer.
func (e *Engine) poseOpponentPick(controller state.PlayerID, source state.ObjID, sa *cards.SA, stage oppPickStage) {
	d := &decision.Decision{Player: controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: source, ResumeKind: "opp_pick", ResumeSA: sa,
		Prompt: "Choose which opponent answers the target ask for " + e.targetName(source)}
	for _, p := range e.livingOpponents(controller) {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options),
			Kind: "player", Label: e.G.Players[p].Name, Player: p})
	}
	e.oppSel = oppSelectState{stage: stage, ctl: controller, source: source, line: sa.Line}
	e.choosing = chooseOppPick
	e.ask(d)
}

// answerOppPick applies the controller's answered selection ask: the chosen
// "player" option names the opponent who answers the re-posed target ask.
// The selection rides e.oppSel (done) back to the asking site's re-entry,
// which consumes it. An empty or malformed answer cannot legally arrive
// (Min 1 / Max 1, validated before handle), so the conservative fallback is
// the old deterministic contract -- the first living opponent in AliveFrom(0)
// turn order -- which can never strand the flow.
func (e *Engine) answerOppPick(d *decision.Decision, chosen []decision.Option) {
	st := e.oppSel
	e.oppSel = oppSelectState{}
	e.choosing = chooseNone
	if len(chosen) > 0 && chosen[0].Kind == "player" {
		st.player = chosen[0].Player
	} else {
		// Malformed answer (the ask is Min 1 / Max 1, so this is the
		// conservative read): fall back to the deterministic first living
		// opponent in AliveFrom(0) turn order.
		opp := e.livingOpponents(st.ctl)
		if len(opp) == 0 {
			return
		}
		st.player = opp[0]
	}
	st.done = true
	e.oppSel = st
	switch st.stage {
	case oppPickTarget:
		e.askTarget(st.ctl, st.source, d.ResumeSA)
	case oppPickCastRoot:
		e.continueCast()
	case oppPickCastSub:
		if pc := e.cast; pc != nil {
			e.postTargetAsks(pc)
		}
	}
}

// midChooserCore is the effects-tier arm of targetChooserCore: the
// mid-resolution ValidTgts$ asks (effects.chosenTargetsFor's "tgts" ask and
// effects.changeZoneChosenTargets' "choice" ask) consult it through
// ChooserFor/OpponentPickAsk. It reads the mid tier's answered selection
// (the "opp_pick" resume arm's pin, keyed by the SA's line) before the
// shared core, so a multi-opponent Opponent form resolves to the seat the
// controller named on the re-entered walk.
func (e *Engine) midChooserCore(c *effects.Ctx, sa *cards.SA) (state.PlayerID, bool, bool) {
	if sa != nil && sa.Line != "" {
		if p, ok := e.oppPicksMid[sa.Line]; ok {
			// Do not consume here: effects asks call ChooserFor first and
			// OpponentPickAsk second. The latter owns consumption after both
			// seams have observed the same selected seat.
			return p, true, false
		}
	}
	if c == nil {
		return 0, false, false
	}
	return e.targetChooserCore(c.Controller, c.Remembered, c.TriggerContext, c.Source, sa)
}

// ChooserFor implements effects.Host's chooser seam for the mid-resolution
// ValidTgts$ asks (effects.chosenTargetsFor and effects.changeZoneChosenTargets).
// Those asks run below the rules tier and cannot import it, so the resolver
// arrives through this hook instead; c carries the controller, source,
// remembered set and trigger context the effects-side ask already holds. An
// absent chooser (or an unknown/unbound/dead referent) keeps c.Controller,
// the same fail-closed default every rules-tier ask uses. The multi-opponent
// Opponent form's selection ask is NOT posed here -- this hook is a
// synchronous read inside a running walk; the asking site calls
// OpponentPickAsk first, which may suspend the walk on the controller's
// selection ask, and ChooserFor then only ever answers the already-selected
// (or sole-opponent) form.
func (e *Engine) ChooserFor(c *effects.Ctx, sa *cards.SA) state.PlayerID {
	if who, ok, pick := e.midChooserCore(c, sa); ok && !pick {
		return who
	}
	if c == nil {
		return 0
	}
	return c.Controller
}

// OpponentPickAsk is the effects.Host seam for the multi-opponent
// TargetingPlayer$ Opponent selection at a mid-resolution ask site
// (chosenTargetsFor / changeZoneChosenTargets, which are mid-walk and can
// suspend). With two or more living opponents and no answered selection it
// poses the controller's which-opponent ask through Engine.Ask -- the
// ordinary mid-resolution resume machinery, so the walk parks on it and the
// answer re-enters this very SA (the "opp_pick" resume arm records the pin
// midChooserCore reads) -- and reports posed=true; the caller returns a
// handled-nil set and stops before the body. Every other shape reports
// posed=false with the seat that answers the target ask: the pinned or sole
// living opponent, or c.Controller when the resolver fails closed (and for
// a host that has no resolver at all -- the effects test double, which never
// reaches this method -- the caller keeps plain ChooserFor).
func (e *Engine) OpponentPickAsk(c *effects.Ctx, sa *cards.SA) (state.PlayerID, bool) {
	who, ok, pick := e.midChooserCore(c, sa)
	if !pick {
		if sa != nil && sa.Line != "" {
			delete(e.oppPicksMid, sa.Line)
		}
		if ok {
			return who, false
		}
		if c == nil {
			return 0, false
		}
		return c.Controller, false
	}
	d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: c.Source, ResumeKind: "opp_pick", ResumeSA: sa,
		Prompt: "Choose which opponent answers the target ask for " + e.targetName(c.Source)}
	for _, p := range e.livingOpponents(c.Controller) {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options),
			Kind: "player", Label: e.G.Players[p].Name, Player: p})
	}
	e.Ask(d)
	return 0, true
}

// askTarget offers every legal target for a spell or ability. It deliberately
// retains the post-push insufficient-target backstop: modal and dynamic target
// counts are not rejected by the earlier cast-offer census.
func (e *Engine) askTarget(p state.PlayerID, source state.ObjID, sa *cards.SA) {
	min, max := e.resolvedTargetBounds(p, source, sa, 0)
	if max == 0 {
		// A dynamic bound RESOLVED to zero: this stage takes no targets, so
		// pose no ask (the effects-side askTargets has the same max <= 0 arm).
		return
	}
	candidates := e.legalTargetCandidates(p, source, source, sa)
	// MaxTotalTargetPower$ (Reunion of the House): prune the candidates that
	// can provably join no legal selection (individually over the cap unless
	// a negative-power candidate could offset them) and carry the running
	// cap as the decision's cumulative budget. Read BEFORE the per-controller
	// bounds so `distinct` counts only candidates a legal selection can
	// still take.
	candidates, powerCap, powerCapped := e.totalPowerCappedCandidates(candidates, p, source, sa, 0)
	min, max, exclusive, distinct := e.oneEachTargetBounds(sa, candidates, min, max)
	min, max, sameCapacity, sameController := e.sameControllerTargetBounds(sa, candidates, min, max)
	min, max, setCapacity, setMode, setKind := e.setPropTargetBounds(sa, candidates, min, max)
	chooser := p
	pickOwed := false
	if who, ok, pick := e.targetAskChooser(p, source, sa); pick {
		pickOwed = true
	} else if ok {
		chooser = who
	}
	d := &decision.Decision{Player: chooser, Kind: decision.KTarget, Min: min, Max: max,
		Prompt: "Choose a target for " + e.targetName(source),
		Source: source, TargetEffect: e.describeTargetEffect(p, source, sa, 0),
		TargetsWithSameController: sameController, SetPropMode: setMode, ResumeSA: sa}
	for _, candidate := range candidates {
		// targetOptionLabel tolerates the Face-less ability object a
		// TargetType$ Activated/Triggered spec now offers: targetName falls
		// back to the source permanent's name.
		label := e.targetOptionLabel(candidate)
		o := decision.Option{Index: len(d.Options), Kind: candidate.kind,
			Label: label, Obj: candidate.obj, Player: candidate.player}
		o.Group = e.targetControllerGroup(sa, candidate)
		o.Controller = e.candidateControllerSeat(candidate)
		o.SetProps = e.setPropTokensFor(setKind, candidate)
		// Option.Value is omitempty and read only under a budget
		// (Decision.HasBudget), so a budget-less target ask keeps its wire
		// payload byte-identical. Every present cap -- zero and negative
		// included, via Decision.Budgeted -- rides the wire, so
		// Decision.Validate enforces the total on every submitted answer.
		if powerCapped && candidate.kind != "player" {
			if co := e.G.Obj(candidate.obj); co != nil && co.Face() != nil {
				o.Value = int(e.Power(candidate.obj))
			}
		}
		d.Options = append(d.Options, o)
	}
	if powerCapped {
		d.MaxSum, d.Budgeted = powerCap, true
	}
	if min == 0 {
		// Requirement N2 / totality: a target-hungry subject whose minimum
		// is zero resolves untargeted when NO legal target exists. When at
		// least one exists it is still offered (Min 0 lets the chooser take
		// none); a zero-option decision would strand the cast, never asked.
		if len(d.Options) == 0 {
			return
		}
	} else if len(d.Options) < min || (exclusive && min > distinct) || (sameController && min > sameCapacity) || (setMode != decision.SetPropNone && min > setCapacity) {
		// A target-hungry subject with fewer legal targets than Min -- or one
		// whose per-controller constraint admits fewer distinct controllers
		// than Min (exclusive && min > distinct: two mandatory targets, both
		// controlled by one player) -- uses CR
		// 608.2b's existing counter/fizzle exit: an immediate move to its
		// normal resting place (exile instead of the graveyard for a
		// Flashback cast, and for a triggered ability object -- which has no
		// graveyard -- exile per CR 608.2m, same as every ability fizzle in
		// resolveTop).
		rest := spellFizzleZone(e.G.Obj(source))
		if o := e.G.Obj(source); o != nil && o.Ability != nil {
			rest = state.ZExile
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: source,
			From: state.ZStack, To: rest, Text: "countered: no legal targets"})
		e.ensureLeftTheStack(source, rest, "a replacement fully discarded this "+
			"spell's 'countered: no legal targets' move without relocating it anywhere; sent "+
			"to its resting zone instead of re-resolving forever")
		// Ruling T14-e: p, the casting player, not e.G.Active -- CR 117.3c,
		// the caster keeps priority even when it fizzles. Only a SPELL keeps
		// priority this way: an ability fizzling is parked in exile (ceases
		// to exist) mid-drain, and handing out priority from inside the
		// trigger drain would double-emit against the drain's own tail.
		if o := e.G.Obj(source); o == nil || o.Ability == nil {
			e.emit(events.Event{Kind: events.Priority, Player: p, Amount: 0})
		}
		return
	}
	if pickOwed {
		// The multi-opponent Opponent form: the controller first names WHICH
		// opponent answers. The selection ask is posed only here, after the
		// feasibility census has established the target ask would be posted
		// (a fizzle above never owed a selection); answerOppPick re-poses
		// this very ask with the named seat.
		e.poseOpponentPick(p, source, sa, oppPickTarget)
		return
	}
	e.ask(d)
}

// handleTarget records the chosen target(s) via TargetsChosen events (Ruling
// T14-b) rather than writing state.Object.Targets directly: apply.go clears
// Targets on a zone change but nothing else ever set it, so a direct write
// here would leave a replayed game with no targets while the live game had
// them. N targets record as TargetMin$ asks for: the first option replaces
// with targets (TargetsChosen shape 0 for an object, 1 for a player), each
// further option appends one (shape 2 object, 3 player) -- see apply.go's
// TargetsChosen case and its test TestTargetsChosenAppendShapes.
func (e *Engine) handleTarget(d *decision.Decision, in decision.Intent) {
	chosen := d.Chosen(in)
	if d.ResumeKind == "copy_targets" {
		if e.resume != nil {
			e.resumeResolution(e.resume, chosen)
		}
		return
	}
	if d.ResumeKind == "charm_targets" {
		groups, ordered := charmTargetGroups(d, chosen)
		if e.cast != nil {
			pc := e.cast
			pc.charmTargets = groups
			stageBase := len(pc.targets)
			pc.targets = append(pc.targets, targetOptions(ordered)...)
			e.repriceForTargets(pc)
			if !pc.isAbility() {
				if pc.stackObj != 0 {
					e.recordChosenTargets(pc.stackObj, ordered, stageBase > 0)
				}
				e.payCast()
			} else {
				pc.rootOpts = append([]decision.Option(nil), ordered...)
				e.payCast()
				if pc.stackObj != 0 {
					e.recordChosenTargets(pc.stackObj, ordered, false)
					e.cast = pc
					e.fireManaSpentTriggers(pc.activationPushEvent(e), nil)
					e.cast = nil
				}
			}
		} else {
			if e.charmTargets == nil {
				e.charmTargets = make(map[state.ObjID][][]state.Target)
			}
			e.charmTargets[d.Source] = groups
			e.recordChosenTargets(d.Source, ordered, false)
		}
		if e.drainAwaitsTarget {
			e.drainAwaitsTarget = false
			e.resumeTriggerDrain()
		} else if e.pending == nil {
			e.emit(events.Event{Kind: events.Priority, Player: in.Player, Amount: 0})
		}
		return
	}
	if d.ResumeKind == "charm_mode_seq" {
		e.charmSeqAnswer(d, in, chosen)
		return
	}
	// A cast-flow target decision (CR 601.2c, asked by targetAsk after the
	// object was pushed by pushCast but BEFORE any cost is paid): completing
	// it means recording the chosen targets onto the stack object and then
	// committing the transaction -- paying the costs and firing the cast
	// trigger -- via payCast. For a spell the stack object is the card
	// itself, already on the stack (pushCast), so targets are recorded before
	// payment (601.2c before 601.2h); for an activated ability the stack
	// object is minted by payCast's AbilityPush, so targets are recorded
	// AFTER it. Targets are never written directly (they go through
	// TargetsChosen events) and always after the push, because a zone change
	// clears them.
	if e.cast != nil {
		pc := e.cast
		// A chained sub-ability's cast-time pre-ask answer (alltargeted1): the
		// KTarget decision posed by subTargetAsk. Record against the stage it
		// was asked for (subStage indexes both), re-price (the union grew, so
		// a target-dependent ReduceCost$ may now apply), then pose the next
		// outstanding post-target ask or finish the payment.
		if d.ResumeKind == "cast_sub" {
			e.answerCastSubTarget(pc, chosen)
			if e.postTargetAsks(pc) {
				return
			}
			e.finishTargetedCast(pc, pc.player)
			return
		}
		// A Fuse cast may ask targets twice (front, then alternate). Append
		// rather than replace so the stack object's flat target list carries
		// both halves, and stageBase records whether an earlier half's
		// targets are already recorded (its first target must then APPEND).
		stageBase := len(pc.targets)
		pc.targets = append(pc.targets, targetOptions(chosen)...)
		// A Fuse cast records the stage's own slice (review MAJOR 1): the flat
		// list on the stack object cannot express which half chose which
		// target, and re-deriving the split through each half's ValidTgts spec
		// mis-assigns every target that half's spec merely overlaps (Turn //
		// Burn's Creature vs Any). Indexed by pc.targetStage, so a targetless
		// stage the ask loop skipped never misaligns the slices.
		if pc.mode == "fuse" {
			for len(pc.stageTargets) <= pc.targetStage {
				pc.stageTargets = append(pc.stageTargets, nil)
			}
			pc.stageTargets[pc.targetStage] = append(pc.stageTargets[pc.targetStage], targetOptions(chosen)...)
		}
		e.repriceForTargets(pc)
		if !pc.isAbility() {
			if pc.stackObj != 0 {
				e.recordChosenTargets(pc.stackObj, chosen, stageBase > 0)
			}
			// CR 702.101b: after the front half's targets, ask the alternate
			// half's before payment. targetAsk skips a targetless half and the
			// cast pays once every target stage is settled.
			if e.castHasNextTargetStage(pc, e.G.Obj(pc.card)) {
				pc.targetStage++
				if e.targetAsk() {
					return
				}
			}
		} else {
			// The ability object does not exist until payCast's AbilityPush, so
			// the root answer's options ride pc.rootOpts for the payment tail's
			// recordChosenTargets; the post-target stages (alltargeted1's sub
			// pre-asks, the CollectEvidence ask) run BEFORE payment (CR 601.2c).
			pc.rootOpts = append([]decision.Option(nil), chosen...)
		}
		// alltargeted1: the post-target announcement stages -- the chain sub
		// pre-asks first, then the CollectEvidence ask whose amount reads the
		// union -- park the same way the root ask did, before any cost is paid.
		if e.postTargetAsks(pc) {
			return
		}
		e.finishTargetedCast(pc, pc.player)
		// tpc1: pin the answering seat against the resolving object. For a
		// spell pc.stackObj is already the pushed object; for an activation
		// payCast's AbilityPush has just minted it. The CR 608.2b recheck
		// (legalTargets) reads this record off the resolving object.
		if po := e.G.Obj(pc.card); po != nil {
			if tsa := e.castStageSA(pc, po, po.Face()); tsa != nil {
				e.recordTpControlsChooser(pc.stackObj, tsa, in.Player)
			}
		}
		return
	}
	e.recordTpControlsChooser(d.Source, d.ResumeSA, in.Player)
	e.recordChosenTargets(d.Source, chosen, false)
	// A target decision asked by a trigger drain (putTriggersOnStack's
	// pushTrigger, immediately after the trigger's TriggerPush -- Task 20's
	// checkTriggers never asked targets, so only a spell's cast-time ask
	// reached here before) does not hand out priority: the drain's own tail
	// does, through the SAME continuation handleTriggerOrder uses
	// (resumeTriggerDrain, turn.go), so a second, later trigger is still
	// placed before any player acts. A spell's cast-time target decision
	// keeps its caster's priority (CR 117.3c) via the emit below.
	if e.drainAwaitsTarget {
		e.drainAwaitsTarget = false
		e.resumeTriggerDrain()
		return
	}
	// Ruling T14-e: the submitting player, not e.G.Active -- CR 117.3c, the
	// player who chose the target (the caster) keeps priority.
	e.emit(events.Event{Kind: events.Priority, Player: in.Player, Amount: 0})
}

// targetOptions converts a target decision's selected options to the
// proposal-local target representation used while its cost is still being
// assembled. Events remain the source of truth once the stack object exists;
// this short-lived copy is only what lets an activated ability evaluate a
// ValidTarget$ cost modifier before its AbilityPush object is minted.
func targetOptions(chosen []decision.Option) []state.Target {
	out := make([]state.Target, 0, len(chosen))
	for _, opt := range chosen {
		if opt.Kind == "player" {
			out = append(out, state.Target{Player: opt.Player, IsPlayer: true})
		} else {
			out = append(out, state.Target{Obj: opt.Obj})
		}
	}
	return out
}

// recordChosenTargets emits the TargetsChosen events for a set of chosen
// target options onto the given object. Amount discriminates the target shape
// (Ruling T14-b, extended by Task 4): 0 replace-with-object, 1
// replace-with-player, 2 append-object, 3 append-player. It is the shared
// recording path for both a cast-flow target decision (onto the stack object,
// after the push) and a triggered ability's own post-TriggerPush ask.
// appendFirst makes even the first option an APPEND: a Fuse cast records the
// front half's targets first and must not have the alternate half's first
// target replace them on the stack object.
func (e *Engine) recordChosenTargets(targetObj state.ObjID, chosen []decision.Option, appendFirst bool) {
	// One target answer is one BATCH for Mode$ BecomesTargetOnce (Forge fires
	// TriggerBecomesTargetOnce once per targeting action, not once per
	// target): the latch is open for exactly the events this loop emits, so a
	// second matching target of the same answer is absorbed rather than
	// queueing a second instance. The bracket never spans a drain -- emit only
	// appends to pendingTriggers -- so the queue stays append-only across it.
	// Batch identity is this CALL, not the ability: a (hypothetical) ability
	// that records its targets in two separate recordChosenTargets calls opens
	// two batches and fires the watcher twice, where Forge fires once per
	// ability. No corpus card takes a multi-call path with a BecomesTargetOnce
	// watcher today; if one arrives the bracket must move up to the ability.
	e.openTargetBatch()
	defer e.closeTargetBatch()
	for i, opt := range chosen {
		ev := events.Event{Kind: events.TargetsChosen, Obj: targetObj}
		appendThis := i > 0 || appendFirst
		if opt.Kind == "player" {
			// shape 1 replace / shape 3 append a single player target.
			ev.Amount = 1
			if appendThis {
				ev.Amount = 3
			}
			ev.Player = opt.Player
		} else {
			// shape 0 replace / shape 2 append one object target.
			if appendThis {
				ev.Amount = 2
			}
			ev.IDs = []state.ObjID{opt.Obj}
		}
		e.emit(ev)
	}
}

// resolveTop resolves the object on top of the stack and moves it to
// wherever it goes next.
//
// CR 608.2b: before anything else runs, every target recorded when this
// spell or ability was put on the stack is rechecked against legalTargets
// below -- a target legal when chosen can stop being legal by the time its
// spell reaches the top of the stack, most commonly a creature that died to
// something else in the meantime. With no legal target left, this does not
// resolve at all: it leaves the stack (a spell to its owner's graveyard, an
// ability to exile per CR 608.2m just below) with no effect -- not even a
// SubAbility chained onto it that names no target of its own (Defined$ You
// and the like). That distinguishes this from the per-target nil-checks
// primitives like effDealDamage already had (Task 18, effects/damage.go):
// those already skip a target that individually vanished, but nothing
// before this stopped the OTHER, untargeted parts of the same spell's
// script from running anyway once every target it had was gone. With only
// some targets still legal, resolution proceeds against exactly that
// narrowed set -- CR 608.2b's "resolves, doing as much as possible".
func cloneCharmTargetGroups(in [][]state.Target) [][]state.Target {
	if in == nil {
		return nil
	}
	out := make([][]state.Target, len(in))
	for i, group := range in {
		out[i] = append([]state.Target(nil), group...)
	}
	return out
}

// recheckCharmTargets applies CR 608.2b independently to each selected
// distinct mode's target declaration. The flat target list remains available
// for legacy consumers, while the returned groups are what effCharm binds to
// each mode.
func (e *Engine) recheckCharmTargets(o *state.Object) ([][]state.Target, []state.Target, bool) {
	if o == nil || e.charmTargets == nil || e.charmTargets[o.ID] == nil || len(o.ChosenModes) == 0 {
		return nil, nil, false
	}
	var root *cards.SA
	var svars map[string]string
	if o.Ability != nil {
		src := e.G.Obj(o.Source)
		if src == nil || src.Face() == nil {
			return nil, nil, false
		}
		root, svars = o.Ability, src.Face().SVars
	} else {
		f := o.Face()
		if f == nil {
			return nil, nil, false
		}
		root, svars = f.SpellAbility(), f.SVars
	}
	slots := charmTargetSlots(svars, root, o.ChosenModes)
	groups := e.charmTargets[o.ID]
	if len(slots) != len(groups) {
		return nil, nil, false
	}
	checked := make([][]state.Target, len(groups))
	var flat []state.Target
	anyLegal := false
	source := o.ID
	if o.Ability != nil {
		source = o.Source
	}
	for i, name := range slots {
		sub := cards.ResolveSVar(svars, name)
		legal := e.legalTargets(groups[i], sub, targetZones(sub), o.Controller, source, o.ID)
		checked[i] = legal
		if len(legal) > 0 {
			anyLegal = true
		}
		flat = append(flat, legal...)
	}
	return checked, flat, anyLegal
}

// offeredTargetSA is the SA whose ValidTgts$ targeting the placement or
// announcement ask covered for this stack object, derived exactly as the
// TargetsOffered marker's derivation in resolveTop: the ability SA itself
// for a non-modal trigger or activated ability (pushTrigger's askTarget),
// the first target-bearing CHOSEN MODE's sub for a modal one (handleModes'
// placement branch asks the mode sub and skips the outer ask entirely), and
// the spell's target declaration (targetAsk's targetSA -- the announced
// mode's for a modal spell) for a spell. nil when none of those declares
// targets. Shared by resolveTop's two branches and resumeResolution so the
// generic ValidTgts$ pre-ask (effects' chosenTargetsFor) skips exactly the
// covered SA on the first pass AND on every resume re-entry -- an optional
// trigger's yes re-enters through resumeResolution, where the first pass's
// bool marker alone is not carried (task mvts1).
func offeredTargetSA(o *state.Object, svars map[string]string) *cards.SA {
	if o.Ability != nil {
		if len(o.ChosenModes) > 0 && strings.TrimSpace(o.Ability.Params["Choices"]) != "" {
			for _, name := range o.ChosenModes {
				if sub := cards.ResolveSVar(svars, name); sub != nil &&
					strings.TrimSpace(sub.Params["ValidTgts"]) != "" {
					return sub
				}
			}
			return nil
		}
		if strings.TrimSpace(o.Ability.Params["ValidTgts"]) != "" {
			return o.Ability
		}
		return nil
	}
	f := o.Face()
	if f == nil {
		return nil
	}
	sa := f.SpellAbility()
	if sa == nil {
		return nil
	}
	targetSA := modalTargetSA(f, sa, o.ChosenModes)
	if targetSA != nil && strings.TrimSpace(targetSA.Params["ValidTgts"]) != "" {
		return targetSA
	}
	return nil
}

// resolvedAbilityTally is the Count$ResolvedThisTurn read for a resolving
// stack object: the per-ability tally events.Apply folded on this resolution's
// Resolve event, keyed by (source permanent, root Ability$ body) content. It is
// the ONE home resolveTop's ability branch and resumeResolution's Ctx rebuild
// call, so a suspended-then-resumed chain cannot read a different ordinal than
// its first pass. Returns 0 for a spell (no Ability) and for a source that has
// already left, the modelled-head zero the effects case gives.
func (e *Engine) resolvedAbilityTally(o *state.Object) int32 {
	if o == nil {
		return 0
	}
	return e.resolvedAbilityTallyFor(o.Source, o.Ability)
}

// resolvedAbilityTallyFor is resolvedAbilityTally's core, shared with
// resolveAbility (which resolves an SA directly and so has no stack object to
// hand it). A nil SA is a spell or a synthetic resolution with no ability
// identity -- a zero.
func (e *Engine) resolvedAbilityTallyFor(source state.ObjID, sa *cards.SA) int32 {
	if sa == nil {
		return 0
	}
	return e.G.ResolvedThisTurn[events.ResolvedAbilityKey(source, sa)]
}

// activationsThisTurnFor is the ConditionActivationLimit$ read
// (Ctx.ActivationsThisTurn): how many times the activated ability sa on
// source has been activated this turn, INCLUDING the resolving one -- its
// AbilityPush (a stack ability) or ManaActivate marker (a mana ability,
// emitted for a chain carrying the gate) is already in the log. sa is located
// in the source's pile by identity, else by its script line (a mana
// ability's colour-pinned or Produced$-rewritten copy keeps Line). Zero --
// the unbound value the effects gate fails open on -- for a nil SA, a
// source that has left, or an SA that is not one of the source's own
// activated abilities (a trigger, a spell, a granted body).
func (e *Engine) activationsThisTurnFor(source state.ObjID, sa *cards.SA) int32 {
	if sa == nil {
		return 0
	}
	o := e.G.Obj(source)
	if o == nil {
		return 0
	}
	idx, _, found := pileAbilityRefOf(o, sa)
	if !found && sa.Line != "" {
		for i, n := 0, o.PileAbilityCount(); i < n; i++ {
			if pa, ok := o.PileAbilityAt(i); ok && pa.SA != nil && pa.SA.Line == sa.Line {
				idx, found = i, true
				break
			}
		}
	}
	if !found {
		return 0
	}
	return int32(e.activationUsedCount(source, idx, "", true))
}

func (e *Engine) resolveTop() {
	id := e.G.Stack[len(e.G.Stack)-1]
	o := e.G.Obj(id)
	if o != nil && o.IsCopy && (o.CopyMayChooseTarget || (e.copyTargetStage != nil && e.copyTargetStage[id] > 0)) {
		if e.AskCopyTargets() {
			return
		}
	}
	savedResolving := e.resolvingObj
	e.resolvingObj = id
	defer func() { e.resolvingObj = savedResolving }()

	if o.Ability != nil {
		// A keyword trigger that refers to one particular permanent incarnation
		// (Evoke's "sacrifice it") loses track when that permanent changes
		// zones. The ability still resolves and leaves the stack, but does
		// nothing to the new object now sharing its stable ObjID (CR 400.7).
		if o.SourceIncarnation != 0 {
			src := e.G.Obj(o.Source)
			if src == nil || src.Incarnation != o.SourceIncarnation {
				e.emit(events.Event{Kind: events.Resolve, Obj: id})
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: state.ZExile})
				return
			}
		}
		// A triggered or activated ability with no printed card: Ruling
		// T14-c / F3 -- Face() returns nil for these, so this branch must
		// run before anything below touches it. Task 20 is what actually
		// puts objects like this on the stack.
		//
		// CR 603.4 (intervening-if): a triggered ability's condition is
		// checked both when it would trigger AND again as it resolves; if
		// it no longer holds, the ability is removed from the stack and does
		// nothing. The trigger was queued because triggerConditionHolds was
		// true at trigger time (triggerMatches), but Scute Mob's "whenever
		// you control five or more lands" can be false by the time the
		// ability resolves -- here a real instant response destroyed one of
		// those lands. findTriggerForAbility identifies the T: line from the
		// SA pointer: it is false for an activated ability (whose SA comes
		// from Abilities, not a Triggers entry) so this recheck never
		// applies to one, and false for a source whose face has changed or
		// gone so a trigger-only rule never fires on unknown provenance.
		// The fizzle move is the same exile rest the ability branch uses for
		// CR 608.2b's no-legal-targets case: an ability "ceases to exist"
		// (608.2m) rather than moving to a card zone, and this build parks
		// such objects in exile. Ordered first because it decides whether
		// the ability does anything at all.
		if t, ok := e.triggerForAbilityObject(id, o); ok {
			// NoResolvingCheck$ True (Ugin's Mastery, Werewolf Pack Leader,
			// Love on the Battlefield, ...): the condition was checked only
			// when the trigger fired, and the transient state it counted (a
			// bounced attacker, drained power) must not fizzle the ability
			// here (triggerResolvingCheckHolds in rules/trigger_condition.go).
			// The captured event roles travel with the ability; passing them
			// is what lets an event-relative clause (Condition$
			// AttackedPlayerWithMostLife) be re-checked with the defender the
			// trigger queued against, which no current state can re-derive.
			tc := e.triggerContexts[id]
			if !e.triggerResolvingCheckHolds(t, o.Source, o.Controller, &tc, e.triggerLineSVars[id]) {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id,
					From: state.ZStack, To: state.ZExile, Text: "fizzled: intervening-if no longer holds"})
				e.ensureLeftTheStack(id, state.ZExile, "a replacement fully discarded this "+
					"ability's 'intervening-if' move without relocating it anywhere; sent to exile "+
					"instead of re-resolving forever")
				return
			}
		}
		// No triggered or activated ability this build produces ever
		// actually populates Targets (only Remembered): Task 20's
		// checkTriggers never calls askTarget, which is the only place
		// TargetsChosen is ever emitted from. So this is unreachable in
		// practice today, but a stack object is a stack object, and CR
		// 608.2b's "spell or ability" covers this shape too if a later
		// task ever gives a triggered ability a player-chosen target.
		targets := o.Targets
		// alltargeted1 and the per-mode charm groups compose rather than
		// compete: collectSubTargetPreAsks excludes a modal (Charm) root
		// whole, so a charm-handled object has NO pre-asked sub answers and
		// recheckCastSubTargets returns (0, 0) for it; conversely a
		// non-modal chain never reaches recheckCharmTargets' groups.
		subChosen, subLegal := e.recheckCastSubTargets(id, o.Ability, o.Controller, o.Source)
		charmModeTargets, charmFlatTargets, charmHandled := e.recheckCharmTargets(o)
		if charmHandled {
			targets = charmFlatTargets
		}
		// Fix round 2 (re-review N1): the gate is `spec != ""` -- "this
		// ability declares a targeting requirement" -- not `len(targets) > 0`
		// -- "this ability happens to have targets right now". The old form
		// used the latter as a proxy for the former, so an ability that NEEDS
		// a target but has none recorded skipped CR 608.2b's recheck entirely
		// and resolved. Zero recorded targets is zero LEGAL targets, which is
		// exactly what 608.2b counters.
		// Requirement N2: an ability that MAY target zero things (TargetMin$ 0)
		// and has none recorded resolves untargeted rather than fizzling --
		// targetMin(o.Ability)==0 && len(targets)==0 is the exemption.
		if !charmHandled {
			if spec := o.Ability.Params["ValidTgts"]; spec != "" && !(e.resolvedTargetMin(o.Controller, id, o.Ability, 0) == 0 && len(targets) == 0) {
				legal := e.legalTargets(targets, o.Ability, targetZones(o.Ability), o.Controller, o.Source, id)
				// subLegal > 0 keeps a chain alive whose ROOT targets all
				// became illegal but whose pre-asked sub target did not
				// (alltargeted1); charmHandled cannot reach here, so the two
				// survival rules never overlap.
				if len(legal) == 0 && subLegal == 0 {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id,
						From: state.ZStack, To: state.ZExile, Text: "fizzled: no legal targets remain"})
					e.ensureLeftTheStack(id, state.ZExile, "a replacement fully discarded this "+
						"ability's 'fizzled: no legal targets' move without relocating it anywhere; "+
						"sent to exile instead of re-resolving forever")
					return
				}
				targets = legal
			}
		}
		if len(targets) == 0 && subChosen > 0 && subLegal == 0 {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id,
				From: state.ZStack, To: state.ZExile, Text: "fizzled: no legal targets remain"})
			e.ensureLeftTheStack(id, state.ZExile, "all cast-time sub targets became illegal")
			return
		}
		// CR 608.2m: a resolved ability just ceases to exist rather than
		// moving to a card zone. This build has no "ceases to exist" zone,
		// so it is parked in exile as the closest existing approximation.
		e.emit(events.Event{Kind: events.Resolve, Obj: id})
		// CR 702.35b: the mandatory, respondable madness trigger makes its
		// cast-or-graveyard choice only as it resolves. Stifle reaches this
		// object before this branch; if the exiled card has moved meanwhile,
		// the ability simply finishes with no choice.
		if o.Ability.API == "MadnessCast" {
			if e.askMadnessCast(o) {
				return
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: state.ZExile})
			return
		}
		// CR 603.5: an optional triggered ability goes on the stack regardless
		// (putTriggersOnStack pushes it unconditionally), and its controller
		// -- or whatever seat its OptionalDecider$ names -- chooses whether to
		// apply the effect as the ability resolves. With the Resolve event
		// already logged, pose that yes/no now and SPEND the resolution: a
		// yes re-enters it (resumeResolution runs the effect, exactly as the
		// tail below would have), a no lets the ability leave the stack
		// having done nothing. A decider who has left the game is nobody to
		// apply an effect to, so the ability ceases to exist (CR 800.4a) and
		// is parked in exile like the other ceased-to-exist rests. Activated
		// abilities and mandatory triggers (findTriggerForAbility returns
		// false for the former, or an OptionalDecider-less trigger for the
		// latter) fall straight through to their effect below.
		rt, triggered := e.triggerForAbilityObject(id, o)
		// resSpec is the OptionalDecider$ spec this ability must ask about.
		// A printed trigger's comes off its face T: line (findTriggerForAbility
		// recovered it). An Effect-created delayed trigger has no face T: line:
		// its Ability is an Execute$ SVar sub-ability, so findTriggerForAbility
		// reports false and the spec rides effects.TriggerContext.OptionalSpec
		// from the registration instead (effects/misc.go effEffect ->
		// state.DelayedTrigger.OptionalSpec -> checkDelayedTriggers /
		// checkEventDelayedTriggers -> this map). Without the fallback an Effect
		// trigger with OptionalDecider$ (Beck's "you may draw a card") would
		// resolve mandatorily, the opposite of the card text.
		resSpec := ""
		// An Effect registration's spec wins even when the trigger-line
		// provenance (abcopy) now recognizes the delayed body as triggered.
		if spec := e.triggerContexts[id].OptionalSpec; spec != "" {
			resSpec = spec
		} else if triggered {
			resSpec = rt.Params["OptionalDecider"]
		}
		if resSpec != "" {
			who, askable := e.deciderFromSpec(resSpec, o.Controller, o.Remembered, e.triggerContexts[id])
			if !askable {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id,
					From: state.ZStack, To: state.ZExile, Text: "ceased to exist: its optional decider left the game"})
				e.ensureLeftTheStack(id, state.ZExile, "the optional decider of this ability left the game, so the "+
					"ability ceased to exist (CR 800.4a) and was parked in exile")
				return
			}
			label := e.abilityLabel(o, cards.Trigger{})
			if triggered {
				label = e.abilityLabel(o, rt)
			}
			e.askOptionalAtResolution(who, o, o.Ability, label, e.triggerContexts[id].OptionalSpec != "")
			return
		}
		// ResolvedLimit$ ("Do this only once each turn."): a MANDATORY
		// trigger that reaches this point is one whose effect is about to run
		// (the optional gate above returned for every OptionalDecider$ shape),
		// so consume its per-turn resolution count now -- before the
		// CumulativeUpkeep/Echo/Cost$ dispatch below, which may open a
		// pay/decline window but is still this ability resolving. A freshly
		// accepted optional trigger is counted at resumeResolution's
		// "optional" arm instead. The eligibility check is on the RESOLVED
		// line's own param (rt), not on the source's other lines: a mixed-line
		// carrier (Cosmic Crucible's mandatory Main1 mana trigger, no
		// ResolvedLimit$) resolving first must not spend its sibling's limit.
		if triggered {
			if _, limited := resolvedLimitValue(rt); limited {
				e.noteTriggerResolved(o.Source)
			}
		}
		// Cumulative upkeep is an ordinary trigger through placement, but its
		// age/payment resolution needs rules' cost machinery. Mana Vault's
		// triggered Untap is the one ordinary effect shape authorized to use
		// that window; unrelated Cost$-bearing trigger effects retain their
		// established executor semantics. ImmediateTrigger joins Untap: its AB
		// shape is Forge's "you may pay <Cost$>. When you do, ..." idiom (Speed,
		// Young Avenger's TrigImmediateTrig -- the only repo-deck carrier), so
		// the ordinary triggered-cost window poses the pay/decline ask before
		// the body runs; a decline leaves the body unexecuted exactly as CR
		// 603.5's "when you do" promises. The DB shape's optional payment stays
		// the UnlessCost$ gate's (effects.unlessProceed); a plain Cost$ on a DB
		// ImmediateTrigger remains the established free-executor semantics.
		//
		// A trigger effect whose Cost$ carries a Draw component joins them: the
		// corpus's Draw<X/Spec> family (Champion of Wits' "you may draw cards
		// equal to its power. If you do, discard two cards" -- Cost$ Draw<X/You>)
		// is the same "you may pay; when you do" idiom, and without the window
		// the body ran for free and no draw happened. The window's pay arm
		// settles the draw, the decline arm leaves the body unexecuted.
		//
		// trigcost1: the gate is no longer an API allowlist. ANY Cost$-bearing
		// trigger body enters the window (Kalastria Highborn's `Cost$ B`,
		// Elenda and Azor's `Cost$ PayLife<4>`, ...): Forge's Cost$ on a trigger
		// body is the "you may pay; if you do" idiom, so a body that never asks
		// executes for free. The window keeps the split -- Priceable cost offers
		// a real pay, everything else lands decline-only. Mandatory-prefixed
		// costs and Mana/CopySpellAbility bodies are carved out inside
		// triggerBodyNeedsCostWindow.
		if o.Ability.API == "CumulativeUpkeep" {
			e.startCumulativeUpkeep(id, o.Source, o.Ability)
			return
		}
		// Echo (kw:Echo, CR 702.35a) is the same keyword-expansion shape: an
		// ordinary Phase trigger whose DB$ Echo body needs rules' payment
		// window and the pay-or-sacrifice election (rules/echo.go). The
		// intervening-if was already applied at trigger time (triggerMatches's
		// Echo$ branch), so everything reaching here is owed.
		if o.Ability.API == "Echo" {
			e.startEcho(id, o.Source, o.Ability)
			return
		}
		if _, triggered := e.triggerForAbilityObject(id, o); triggered &&
			e.triggerBodyNeedsCostWindow(o.Ability) {
			e.startTriggeredEffectCost(&resumePoint{kind: "effect_cost", obj: id, sa: o.Ability}, o.Source)
			return
		}
		// The ability-cast copy family (abcopy1): an AB$ CopySpellAbility
		// execute carrying a real Cost$ (Rings of Brighthearth {2}, Kurkesh
		// {R}, Battlemages' Bracers {1}, Chandra's Regulator {1}) must never
		// copy for free. When the firing trigger's context carries an event
		// role -- the activation role TriggerAbility, or the spell arm's
		// TriggerCard (a SpellCast fires on PutOnStack, whose Obj IS the spell;
		// no ability wrapper is minted) -- route the same pay/decline window
		// the Untap/ImmediateTrigger shapes use: a decline leaves the trigger
		// unexecuted, a pay charges the cost and then runs the copy. An
		// unpriceable cost (Verrak's PayLife<X>, Mica's Sac<1/Artifact>) poses
		// the ask but offers no answerable "pay" -- the ParseUnlessCost
		// hard-decline convention. A context-less synthetic push (no role)
		// keeps the free-executor semantics.
		tc := e.triggerContexts[id]
		if _, triggered := e.triggerForAbilityObject(id, o); triggered &&
			o.Ability.API == "CopySpellAbility" &&
			o.Ability.Params["Cost"] != "" &&
			(tc.TriggerAbility != 0 || tc.TriggerCard != 0) {
			e.startTriggeredEffectCost(&resumePoint{kind: "effect_cost", obj: id, sa: o.Ability}, o.Source)
			return
		}
		// The ability object itself has no Face, so its SVar table (needed
		// for Num's SVar indirection, e.g. Goblin Piledriver's "NumAtt$ +X")
		// comes from the permanent that granted it (o.Source) instead.
		// Printed face SVars are static card-script text, but a granted
		// trigger's owner may be a different card (or its grant may have ended).
		// For those wrappers the recorded line carries its owning SVar table.
		// A source that has since left the battlefield
		// (or ceased to exist) has nothing to read here and degrades to a
		// nil SVar table, same as before this ability object existed at
		// all, rather than panicking.
		//
		// A mutated pile (CR 702.140d) makes "the source's current Face" the
		// wrong table for an UNDER-card's ability: Face() on a pile is always
		// its TOP card, whose SVar table the under-card's body never meant --
		// Huntmaster Liger mutated under a Grizzly Bears read the Bears' (
		// empty) table for its own "NumAtt$ +X | SVar:X:Count$TimesMutated"
		// and pumped by 0. The owning face is the one that carries the
		// resolving trigger, which findTriggerForAbilityFace recovers by the
		// compiled trigger pointer -- the whole reason MergedTriggerPush mints
		// f.Triggers[i].Effect rather than a freshly parsed SVar body. An
		// ordinary trigger finds its own (top) face there, so its table is
		// unchanged, and an activated ability finds no trigger at all and
		// falls through to Face() exactly as before.
		var svars map[string]string
		if src := e.G.Obj(o.Source); src != nil {
			if _, mf, ok := e.findTriggerForAbilityFace(o.Source, o.Ability); ok && mf != nil {
				svars = mf.SVars
			} else if mf, ok := e.pileFaceForSA(o.Source, o.Ability); ok && mf != nil {
				// An activated ability of a MUTATED pile (CR 702.140d): o.Ability
				// is the under-card's SA, so the table its body reads is the
				// under-card's own -- Porcuparrot's `NumDmg$ X` resolves X from
				// the pile's Count$TimesMutated on ITS face, not the top card's.
				svars = mf.SVars
			} else if sf := src.Face(); sf != nil {
				svars = sf.SVars
			}
		}
		if owned, ok := e.triggerLineSVars[id]; ok {
			svars = owned
		}
		// Ruling T20-b: Source must be o.Source (the permanent that has this
		// ability), not id (the transient stack-object wrapper) -- Defined$
		// Self, the most common Defined$ value in real trigger scripts,
		// resolves to Ctx.Source, and a wrapper ID means "Self" refers to a
		// stack object with no Face() that leaves play the instant this
		// resolves, so the effect would silently apply to nothing. The SVar
		// lookup two lines above already gets this right by reading from
		// o.Source; this was a one-line inconsistency, not a second design.
		ctx := &effects.Ctx{Source: o.Source, Controller: o.Controller,
			Targets: targets, ModeTargets: charmModeTargets, Remembered: e.resolvingRemembered(o), Captured: o.Remembered, TriggerContext: e.triggerContexts[id],
			// Forge's Count$ResolvedThisTurn reads the per-ability tally the
			// Resolve event's Apply folded: the count INCLUDES this resolution,
			// because the Resolve event is emitted above before this Ctx is
			// built (the Sephiroth "if this is the fourth time" gate).
			ResolvedThisTurn:    e.resolvedAbilityTally(o),
			ActivationsThisTurn: e.activationsThisTurnFor(o.Source, o.Ability),
			// An Effect-created delayed trigger body resolves under the Effect's
			// source-scoped frame (queued by rules' delayed-trigger fire), so the
			// one-shot self-exile idiom it may run ends the Effect. Zero for every
			// ordinary printed trigger.
			EffectFrame: e.triggerEffectFrames[id],
			// The resolving stack-object wrapper: ValidStack's otherAbility
			// exclusion (Ulalek's sub-copy) anchors here, not on Source --
			// Source is the source permanent (Ruling T20-b), which is not on
			// the stack and would exclude nothing.
			ResolvingObj: id,
			// alltargeted1: the cast flow's pre-asked SubAbility$ target
			// answers, consumed line by line by chosenTargetsFor.
			SubPreAsk: e.castSubTargets[id]}
		// CR 702.49b: a K:Ninjutsu permanent enters attacking the same player
		// (planeswalker or battle) the returned creature was attacking. The
		// activator captured that defender when the Return cost was paid
		// (rules/cast.go's returncost arm) and it rides the AbilityPush event's
		// IDs, which events.Apply folded into o.Remembered as a player target.
		// Re-bind it here so effects/zone.go's Attacking$ True rider (which
		// reads Ctx.DefendingPlayer) places the permanent against the right
		// defender. Only a ninjutsu activation carries the tag, so no other
		// resolution's Remembered player is reinterpreted as a defending
		// player.
		if ab := o.Ability; ab != nil && saHasKeyword(ab, "Ninjutsu") {
			for _, rem := range o.Remembered {
				if rem.IsPlayer {
					ctx.DefendingPlayer = rem
					break
				}
			}
		}
		// The SA whose targeting the placement ask actually offered, not
		// blindly the resolving SA: for a non-modal ability that is the outer
		// SA's own ValidTgts$ (pushTrigger's askTarget), for a modal one it is
		// the first target-bearing CHOSEN MODE's sub -- handleModes' placement
		// branch asks the mode sub and skips the outer ask entirely (a Charm's
		// ValidTgts$ lives inside its modes, Kami of Restless Shadows'
		// RaiseScoundrel). Deriving the marker from the outer SA alone left
		// the modal shape unmarked, so a Min-0 mode target the chooser elected
		// ZERO of was re-posed by effChangeZone's mid-resolution ask at
		// resolution -- the exact duplicate-ask defect the marker exists to
		// stop.
		offeredSA := offeredTargetSA(o, svars)
		if offeredSA != nil {
			ctx.TargetsOffered = true
			ctx.OfferedSA = offeredSA
		}
		if lki, ok := e.triggerLKI[id]; ok {
			ctx.LKI = lki.object
			ctx.LKIPower, ctx.LKIToughness, ctx.LKIPTValid =
				lki.power, lki.toughness, lki.ptValid
		}
		// CR 107.3i: X is the value the activator chose for a Cost$ carrying
		// {X} (recorded on the ability stack object by commitCast's CastInfo,
		// emitted right after the AbilityPush). Zero for a trigger, which was
		// never paid an X -- and for a trigger CR 107.3m rebinds X to the
		// spell that became the permanent (an ETB trigger) or the spell the
		// trigger fired on (a cast/magecraft trigger), which triggerPaidX
		// reads off the causing event's card.
		ctx.X = o.X
		ctx.XAnnounced = stackXAnnounced(o) || ctx.X != 0
		if ctx.X == 0 {
			ctx.X = e.triggerPaidX(id, o)
		}
		// A cost-paid sacrifice carried its objects' LKI snapshot on the
		// engine (rules/cast.go commitCast), keyed by this stack object id;
		// load it so the ability's Sacrificed$<Property> heads resolve against
		// what it sacrificed. Mirror of triggerContexts: engine-only.
		ctx.Sacrificed = e.sacrificedLKI[id]
		ctx.Exiled = e.castExiled[id]
		ctx.Revealed = e.castRevealed[id]
		if link, ok := e.sourceLifelinkLKI[id]; ok {
			ctx.SourceLifelinkLKI = link
			ctx.SourceLifelinkLKIValid = true
		}
		if controller, ok := e.sourceControllerLKI[id]; ok {
			ctx.SourceControllerLKI = controller
			ctx.SourceControllerLKIValid = true
		}
		if lki := e.damageSourceLKI[id]; lki != nil {
			ctx.DamageSourceLKI = cloneDamageSourceLKI(lki)
		}
		effects.SetSVars(ctx, svars)
		// The Evolve keyword's own counter trigger (CR 702.99a) is identified
		// so its resolution can announce the completed evolve action (CR
		// 702.99b, trig:Evolved) once the counter lands below. Capturing the
		// pre-resolution +1/+1 count is what keeps a replaced or skipped
		// placement from firing the mode: only a real increase counts.
		var evolveWatch bool
		var evolveCountersBefore int32
		if triggered && rt.Params["Evolve"] != "" {
			if src := e.G.Obj(o.Source); src != nil {
				evolveWatch = true
				evolveCountersBefore = src.Counter("P1P1")
			}
		}
		// CR 603.3c: the mode choice was announced at placement (pushTrigger
		// asked KModes and handleModes recorded the answer into ChosenModes).
		// Pre-seeding Ctx.Modes makes effCharm take its re-entry branch and
		// run exactly the chosen modes rather than asking again at
		// resolution. Nil for a non-modal trigger, for an activated ability,
		// and for any trigger the placement ask never reached.
		ctx.Modes = o.ChosenModes
		e.damaging = o.Source
		e.contChain = e.contChain[:0]
		e.repeatReported = nil
		e.contChainOwners++
		effects.Resolve(e, ctx, o.Ability)
		e.contChainOwners--
		e.damaging = 0
		// CR 702.99b (task trig:Evolved): when this resolving ability is the
		// Evolve keyword's own counter trigger (its line carries Evolve$ True)
		// and it actually put the +1/+1 counter, announce the completed evolve
		// action so a sibling T:Mode$ Evolved ability on the same creature
		// fires. The marker is emitted AFTER the body so it names only an
		// evolve whose counter really landed: a source that left the
		// battlefield before resolution (effPutCounter skips a non-battlefield
		// recipient) places nothing and is not an evolve. A suspension
		// (e.resume != nil) is left to the resumed pass, which reaches this
		// same tail again. The counter read is the body's own P1P1 kind
		// (cards/kw_evolve.go's CounterType$ P1P1).
		if evolveWatch && e.resume == nil {
			if src := e.G.Obj(o.Source); src != nil && src.Counter("P1P1") > evolveCountersBefore {
				e.emit(events.Event{Kind: events.Evolved, Obj: o.Source, Player: o.Controller})
			}
		}
		if e.resume != nil {
			// A placement-announced modal ability can reach a nested ask during
			// this initial pass. Preserve every enclosing continuation exactly as
			// resumeResolution does for a nested ask reached on re-entry.
			e.resume.outer = e.buildContinuationChain(e.contChain, id, nil)
			e.contChain = e.contChain[:0]
			// A mid-resolution ask (M2d-2): the effect that asked has set a
			// decision pending and recorded a resume point. The object stays
			// on the stack waiting for the answer -- entering the exile exit
			// below would discard it mid-resolution. The answered decision
			// re-enters the suspended effect through resumeResolution
			// (rules/resolution.go), which runs the rest of this same tail.
			return
		}
		e.contChain = e.contChain[:0]
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: state.ZExile})
		e.ensureLeftTheStack(id, state.ZExile, "a replacement fully discarded this resolved "+
			"ability's own move off the stack without relocating it anywhere; sent to exile "+
			"instead of re-resolving forever")
		return
	}

	f := o.Face()
	sa := f.SpellAbility()
	// Bestow (CR 702.114a): a cast paid for with the bestow cost resolves
	// as the synthesized Aura attach spell -- the creature face itself has
	// no SP -- so the whole ordinary Aura tail below runs unchanged:
	// effAttach emits events.Attach while the spell is still on the stack,
	// and moveResolvedOffStack enters the permanent attached (the entry
	// keeps an Attach set on the stack). The flag is the pay-time CastInfo
	// provenance modeFlags("bestowed") rode.
	if o.CastFlags&state.FlagBestowed != 0 {
		sa = bestowedAttachSA()
	}
	// Morph / Megamorph / Disguise (CR 702.37a/702.168a/702.169a via
	// CR 708.4): a face-down cast resolves as a vanilla creature spell --
	// the face-down spell has no printed spell abilities and no targets.
	// The provenance flag is the pay-time CastInfo's modeFlags(fam) family
	// bit; moveResolvedOffStack re-carries the face-down entry marker so
	// the permanent enters face down (CR 708.5's 2/2 creature, the cloak
	// marker's ward {2} for Disguise). A stack copy inherits the flag (the
	// FlagFused way -- not a CastProvenanceFlags bit) and resolves the
	// same vanilla way.
	if o.CastFlags&(state.FlagMorphed|state.FlagMegamorphed|state.FlagDisguised) != 0 {
		sa = nil
	}
	// Mutate (CR 702.140d): a spell cast for its mutate cost does not become
	// an independent permanent. It merges into its target, so resolution is
	// diverted BEFORE the ordinary spell-block tail (which would move it to
	// the battlefield): resolveMutate emits the Mutate fold, which parks this
	// object off the stack. A mutate card carries no SP, so the spell block
	// below would resolve nothing anyway.
	if o.CastFlags&state.FlagMutated != 0 {
		e.resolveMutate(o, o.Targets)
		return
	}
	// Fuse (CR 702.101b): a fused split spell is one spell whose BOTH halves'
	// spell abilities resolve in sequence. The card stays at its front face;
	// resolveFused owns the per-half target recheck, the Resolve event, the
	// Ascend blessing and the off-stack move.
	if o.CastFlags&state.FlagFused != 0 {
		// A half that suspended on a mid-resolution ask hands back the
		// continuation (the rest of that half plus a fuse-rest frame for any
		// unrun half); link it onto the ask's fresh resume point here, the
		// resolution machinery's own write (ruling T21-e).
		if cont, suspended := e.resolveFused(o); suspended && e.resume != nil {
			e.resume.outer = cont
		}
		return
	}
	targets := o.Targets
	charmModeTargets, charmFlatTargets, charmHandled := e.recheckCharmTargets(o)
	if charmHandled {
		targets = charmFlatTargets
	}
	// targetSA is the SA whose ValidTgts$ the cast-flow target ask offered
	// (the modal declaration for a Charm, the SpellAbility itself otherwise);
	// hoisted so the resolution ctx can carry the TargetsOffered marker and
	// the mvts1 pre-ask's OfferedSA skip.
	targetSA := modalTargetSA(f, sa, o.ChosenModes)
	// An overloaded spell affects the matching set as it resolves, never as
	// targets chosen during announcement. This fresh non-target census means
	// protection/hexproof do not apply and objects entering or changing
	// controller in response are included correctly. Effect primitives keep
	// their generic Ctx.Targets recipient API; only the source of that list is
	// different.
	overloaded := o.CastFlags&state.FlagOverloaded != 0
	if overloaded && sa != nil {
		if targetSA != nil {
			for _, cand := range e.affectedCandidates(o.Controller, id, id, targetSA) {
				if cand.kind == "player" {
					targets = append(targets, state.Target{Player: cand.player, IsPlayer: true})
				} else {
					targets = append(targets, state.Target{Obj: cand.obj})
				}
			}
		}
	}
	// The spell branch's twin of the ability branch's composition: a modal
	// root carries no pre-asked sub answers, so this returns (0, 0) exactly
	// where charmHandled owns the recheck instead.
	subChosen, subLegal := e.recheckCastSubTargets(id, sa, o.Controller, id)
	if sa != nil && !overloaded && !charmHandled {
		// A modal spell's target declaration lives on its announced mode SVar,
		// not the outer Charm SA. Use the same selected declaration targetAsk
		// used during CR 601.2c, so its targets receive the ordinary CR 608.2b
		// legality recheck at resolution. (targetSA is hoisted above.)
		// Fix round 2 (re-review N1), the same correction as the ability
		// branch above, and the one that was actually reachable. Widening the
		// departed-player release hook in fix round 1 turned a stall into a
		// spell that RESOLVES with no targets at all: the caster is
		// eliminated while their target decision is outstanding, the hook
		// clears it so the match can continue, and the spell is left on the
		// stack with Targets nil. Under the old `len(targets) > 0` gate that
		// skipped the recheck and ran the whole script -- untargeted riders
		// included -- gaining the ELIMINATED caster 7 life in the re-review's
		// own reproduction. CR 608.2b counters a spell with no legal targets;
		// CR 800.4a says a departed player's spell ceases to exist. Neither
		// permits it to resolve.
		// Requirement N2, the same exemption as the ability branch: an
		// untargeted-with-Min-0 spell resolves rather than fizzling.
		if spec := targetSA.Params["ValidTgts"]; spec != "" && !(e.resolvedTargetMin(o.Controller, id, targetSA, 0) == 0 && len(targets) == 0) {
			legal := e.legalTargets(targets, targetSA, targetZones(targetSA), o.Controller, id, id)
			if len(legal) == 0 && subLegal == 0 {
				// CR 608.2b: every target became illegal. This spell does
				// not resolve -- no Resolve event, no script runs -- it goes
				// straight to its normal resting place, the same zone it
				// would reach after an ordinary resolution (an Aura instead
				// reaching the battlefield would be wrong here: CR 704.5m's
				// "nothing legal to attach to" is exactly this case for
				// that spell shape, and the resting zone is where it
				// belongs) -- exile instead of the graveyard for one cast
				// via Flashback (CR 702.32b).
				rest := spellFizzleZone(o)
				e.emit(events.Event{Kind: events.MoveZone, Obj: id,
					From: state.ZStack, To: rest, Text: "fizzled: no legal targets remain"})
				e.ensureLeftTheStack(id, rest, "a replacement fully discarded this "+
					"spell's 'fizzled: no legal targets' move without relocating it anywhere; "+
					"sent to its resting zone instead of re-resolving forever")
				return
			}
			targets = legal
		}
	}
	if len(targets) == 0 && subChosen > 0 && subLegal == 0 {
		rest := spellFizzleZone(o)
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: rest,
			Text: "fizzled: no legal targets remain"})
		e.ensureLeftTheStack(id, rest, "all cast-time sub targets became illegal")
		return
	}
	e.emit(events.Event{Kind: events.Resolve, Obj: id, Text: f.Name})
	// Ascend (CR 702.131a, the non-permanent case): an instant/sorcery with
	// K:Ascend grants its controller the blessing BEFORE the spell's own
	// body and condition checks read the latch (Forge's "do blessing there
	// before condition checks"; rules/ascend.go). Permanent faces are
	// excluded -- their grant is the emit-side continuous scan.
	e.grantSpellBlessing(o, f)
	// CR 702.168b: a promised gift on an INSTANT OR SORCERY resolves BEFORE
	// the spell's other effects (the gift's own "before its other effects").
	// The body is the face's GiftAbility SVar; it is spliced as the HEAD of
	// the spell's own chain so the ordinary suspension/continuation machinery
	// handles a mid-gift ask and then runs the rest of the spell, and it
	// shares the spell's Ctx so Defined$ Promised / TokenOwner$ Promised read
	// the promise. The events.GiveGift marker is emitted just before the gift
	// body runs. The splice is a SHALLOW COPY of the resolved gift SA, never
	// a mutation of the card's parsed table.
	//
	// A PERMANENT's promised gift is NOT spliced here: CR 702.168c makes it a
	// "when this permanent enters" triggered ability, queued after entry by
	// altCostEnter and pushed onto the stack by pushTrigger -- so it can be
	// responded to and ordered against the card's own printed ETB.
	resolveSA := sa
	if o.CastFlags&state.FlagPromisedGift != 0 && !f.IsPermanent() {
		if gift := cards.ResolveSVar(f.SVars, "GiftAbility"); gift != nil {
			e.emit(events.Event{Kind: events.GiveGift, Player: o.Controller, Obj: id})
			head := *gift
			tail := &head
			for tail.Sub != nil {
				tail = tail.Sub
			}
			tail.Sub = sa
			resolveSA = &head
		}
	}
	// Cipher (CR 702.99a): the encode is the resolving spell's own tail
	// instruction, not a triggered ability. rules/cipher.go appends it to a
	// copy of the chain so the ordinary suspension/continuation machinery runs
	// it after the spell's effects and before the move off the stack below. A
	// stack COPY (o.IsCopy) is not a card, so it cannot be encoded.
	if !o.IsCopy {
		if withCipher := cipherTailFor(f, resolveSA); withCipher != nil {
			resolveSA = withCipher
		}
	}
	if resolveSA != nil {
		e.damaging = id
		ctx := &effects.Ctx{Source: id, Controller: o.Controller, Targets: targets,
			ModeTargets: charmModeTargets, ResolvingObj: id,
			// alltargeted1: the cast flow's pre-asked SubAbility$ target
			// answers, consumed line by line by chosenTargetsFor. Disjoint
			// from ModeTargets: a modal root is never pre-asked.
			SubPreAsk: e.castSubTargets[id]}
		// Same marker as the ability branch: the cast-flow target ask
		// (targetAsk's targetSA) offered exactly this spell's targeting.
		if targetSA != nil && strings.TrimSpace(targetSA.Params["ValidTgts"]) != "" {
			ctx.TargetsOffered = true
			ctx.OfferedSA = targetSA
		}
		// CR 107.3i: X is the value the caster chose for the mana cost's {X},
		// recorded on the stack object by commitCast's CastInfo (the same
		// value the ETB/replacement path already reads as o.X). Without this
		// every numeric parameter that reads the paid X (TokenAmount$ X,
		// Amount$ X, SVar:X:Count$xPaid, ...) resolves to 0 and the spell's
		// body does nothing -- Entreat the Angels resolved to the graveyard
		// having created zero Angels.
		ctx.X = o.X
		ctx.XAnnounced = stackXAnnounced(o)
		// Same as the ability branch: carry the sacrifice LKI (engine-keyed)
		// onto resolution so Sacrificed$<Property> heads resolve against what
		// this spell sacrificed.
		ctx.Sacrificed = e.sacrificedLKI[id]
		ctx.Exiled = e.castExiled[id]
		ctx.Revealed = e.castRevealed[id]
		effects.SetSVars(ctx, f.SVars)
		// CR 601.2b: a modal spell's choice was recorded on its proposal before
		// targets and payment. Pre-seeding Modes makes effCharm execute exactly
		// that announcement instead of posing its old resolution-time ask.
		ctx.Modes = o.ChosenModes
		e.contChain = e.contChain[:0]
		e.repeatReported = nil
		e.contChainOwners++
		effects.Resolve(e, ctx, resolveSA)
		e.contChainOwners--
		e.damaging = 0
		if e.resume != nil {
			// The cast-announced outer mode may itself contain an asking effect.
			// This is an initial resolution pass rather than a resume re-entry,
			// but its enclosing SubAbility continuations have the same lifetime.
			e.resume.outer = e.buildContinuationChain(e.contChain, id, nil)
			e.contChain = e.contChain[:0]
			// A mid-resolution ask (M2d-2): same as the ability branch above
			// — the resolution is suspended with the object still on the
			// stack, and the answered decision re-enters it through
			// resumeResolution instead of this tail.
			return
		}
		e.contChain = e.contChain[:0]
	}
	e.moveResolvedOffStack(o)
}

// spellRestZone is where a spell goes AFTER IT RESOLVES. Buyback is a
// resolution replacement (CR 702.27a), not a replacement for being
// countered, so only this resolved-spell helper may return it to hand.
func spellRestZone(o *state.Object) state.Zone {
	if o != nil && (state.ExilesLeavingStack(o.CastFlags) ||
		o.IsCopy || o.CastFlags&state.FlagAdventure != 0 ||
		o.CastFlags&state.FlagReplaceGraveyard != 0 ||
		o.CastFlags&state.FlagRebound != 0) {
		return state.ZExile
	}
	if o != nil && o.CastFlags&state.FlagBuyback != 0 {
		return state.ZHand
	}
	return state.ZGraveyard
}

// spellFizzleZone is the resting place when a spell never resolved. Flashback,
// Harmonize, Aftermath, the ReplaceGraveyard$ Play rider and copies still use
// exile, but Buyback does not apply and the card reaches its owner's graveyard.
func spellFizzleZone(o *state.Object) state.Zone {
	if o != nil && (state.ExilesLeavingStack(o.CastFlags) ||
		o.IsCopy || o.CastFlags&state.FlagReplaceGraveyard != 0) {
		return state.ZExile
	}
	return state.ZGraveyard
}

// ensureLeftTheStack is CR 608.2m housekeeping, not a further game action:
// every one of resolveTop's five exits (the permanent-ETB Move above, the
// instant/sorcery Move above, and the three fizzle/ability-resolution Moves
// earlier in this function) emits a MoveZone meant to take id off the stack
// for good. If a ValidCard$-matching, ReplacementResult$-absent (this
// build's "Replaced") R:Event$ Moved replacement on some OTHER object
// intercepts that specific MoveZone and its own ReplaceWith$ does not itself
// relocate the card (e.g. it only gains life), nothing else ever removes id
// from e.G.Stack: the next priority round finds the same object on top and
// resolves it again, forever. Note this is NOT the shipped corpus's own
// Rest in Peace / Dryad Militant / Leyline of the Void shape: that shape's
// ReplaceWith$ names Defined$ ReplacedCard, which effects/context.go's
// Defined DOES model, and with ChangeZone's Origin$ All parsed as a real
// wildcard (effects/zone.go's ParseZones) it genuinely relocates the card
// to exile -- so a working Rest-in-Peace-shaped replacement takes the object
// off the stack itself and the guard never engages. The guard is for the
// replacements whose ReplaceWith$ truly leaves the card where it was.
//
// Originally added by Task 29 (Ruling T26-a) for the permanent-ETB exit
// alone; the final whole-branch review (Critical C1) measured the identical
// hazard, independently reachable, on the other four exits -- a two-card
// deck (an instant or an ability-granting permanent, plus a Rest-in-Peace-
// shaped enchantment already on the battlefield) reliably hung a real match
// at 100 000+ intents, never terminating, on turn 1. 24 corpus cards this
// build's own coverage gate already calls fully playable carry the shape
// (Rest in Peace, Dryad Militant, Leyline of the Void, ...), so this is
// reachable from ordinary deck-building, not adversarial card text. This
// helper is what makes every exit safe with one implementation instead of
// five copies of the same six lines.
//
// Held under applyingReplacement (saved and restored, not just set, in case
// a future caller ever reaches here already inside one) for the same reason
// the original ETB guard was (Task 29 review finding I-1): "ceases to
// exist" is engine bookkeeping, not a game event a card's own replacement
// gets to intercept a second time. why becomes the Note's Text, tailored by
// each call site to name which of the five moves was actually being
// guarded.
func (e *Engine) ensureLeftTheStack(id state.ObjID, to state.Zone, why string) {
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZStack {
		return
	}
	// fx44: a SUSPENDED replacement is legitimately still on the stack,
	// waiting for its answer — the ask it posed decides where the object goes
	// (Mox Diamond's ETB replacement parks the card exactly here). The guard
	// must distinguish "the replacement finished and moved nothing" (the
	// ordinary re-resolve hazard below, where e.resume is nil) from "the
	// replacement is waiting for an answer" (where the object must stay put
	// so resumeResolution can act on it). Deferring the park while suspended
	// leaves the object on the stack; the answered resume performs the real
	// move, and finishResumption's own o.Zone != state.ZStack check skips
	// this guard entirely once that move has happened.
	if e.Suspended() {
		return
	}
	saved := e.applyingReplacement
	e.applyingReplacement = true
	e.emit(events.Event{Kind: events.Note, Obj: id, Text: why})
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: to})
	e.applyingReplacement = saved
}

// legalTargets is CR 608.2b's recheck, applied at resolution: the subset of
// targets that are still legal right now. An object target must still be in
// a zone the spec legitimately targets -- zones, computed by the same
// targetZones the offering askTarget used, is what no-longer-hardcodes the
// battlefield -- because matchesBase's own bare-type predicates (effects/
// filter.go), e.g. "Creature", read printed types straight off the Face with
// no zone check of their own, so a creature that died and is sitting in a
// graveyard would otherwise still look like a match. The old form required
// o.Zone == state.ZBattlefield, which made every non-battlefield legal
// target offered by askTarget (a stack spell for a counterspell; a
// Graveyard card for the Snapcaster shape) fizzle at resolution. It still
// satisfies spec, via the same effects.MatchesSpec call askTarget used to
// offer it as an option in the first place: no self-relative source,
// matching askTarget's own simplification, so a spec that would filter on
// Self/Other is exactly as (im)precise here as it was at cast time. A player
// target is legal for as long as they are still in the game AND still match
// the spec's player filter (playerTargetSpecMatches -- the shared
// MatchesPlayerSpecFrom grammar plus the trigger-role alternatives the ask's
// own TriggerContext carries -- from the controller's perspective, against
// the same source the offer judged) -- the same judge the offer applies, so
// offer and recheck cannot disagree (the one-definition rule). A target
// whose qualifier the filter cannot evaluate was never offered and is
// rejected here too, fail closed.
// recheckCastSubTargets uses the same legality judge as the root target at
// resolution. Answers remain in the map through suspended re-entries, so a
// body resumed after an unrelated choice still uses its announced target.
// Preserve an answered-empty entry as a non-nil slice so the effects walk
// does not mistake it for an outstanding mid-resolution ask.
func (e *Engine) recheckCastSubTargets(id state.ObjID, root *cards.SA, controller state.PlayerID, source state.ObjID) (chosen, legal int) {
	answers := e.castSubTargets[id]
	if len(answers) == 0 {
		return 0, 0
	}
	for _, sa := range e.collectSubTargetPreAsks(root) {
		ts, ok := answers[sa.Line]
		if !ok {
			continue
		}
		chosen += len(ts)
		kept := e.legalTargets(ts, sa, targetZones(sa), controller, source, id)
		legal += len(kept)
		if kept == nil {
			kept = []state.Target{}
		}
		answers[sa.Line] = kept
	}
	return chosen, legal
}

func (e *Engine) legalTargets(targets []state.Target, sa *cards.SA, zones []state.Zone, you state.PlayerID, source state.ObjID, self state.ObjID) []state.Target {
	spec := ""
	if sa != nil {
		spec = sa.Params["ValidTgts"]
	}
	var legal []state.Target
	// The resolution recheck, unlike a target offer, has this stack object's
	// Targets available. Targeted* predicates may read precisely this binding;
	// setting it here keeps their self-reference unavailable at announcement.
	// The source rides in too, the same object askTarget's own offer filter
	// sees (candidatesFor's sc.Source): a source-reading predicate
	// (CanEnchantEquippedBy -- Mantle of the Ancients' recheck) judges the
	// chosen target at resolution exactly as the offer judged it at
	// placement, Critical C2's one-definition rule. Before this, the recheck
	// built its SpecContext with source 0 and every source-reading predicate
	// failed closed there -- a target the placement offer had just certified
	// fizzled at resolution.
	sc := e.targetSpecContext(source, self, you)
	sc.ResolutionTargets = targets
	sc.Resolving = true
	// TargetingPlayerControls$ (tpc1): the restriction's answering seat is
	// resolved through the same derivation the offer census used, with the
	// answered-ask record taking precedence -- it is keyed by self, the
	// RESOLVING stack object the answer was recorded on, so a selection pin
	// consumed since the ask cannot change the answer here. An SA whose
	// record is missing (a recheck that never had its own ask -- a copy's
	// inherited targets, a modal group) falls back to the derivation; with
	// the multi-opponent selection unrecoverable that is the union over the
	// controller's living opponents, the same shape the offer census used
	// while the selection was owed.
	tpChooser, tpState := e.targetControlsChooser(you, source, sa)
	if tpState != tpNone {
		if rec, ok := e.tpCtlChooser[self]; ok && (sa == nil || sa.Line == "" || rec.line == "" || rec.line == sa.Line) {
			tpChooser, tpState = rec.player, tpResolved
		}
	}
	controllerProp := ""
	nonTriggeredController := false
	triggeredCardController := state.PlayerID(0)
	triggeredCardControllerOK := false
	if sa != nil {
		controllerProp = strings.TrimSpace(sa.Params["TargetsWithControllerProperty"])
		if strings.TrimSpace(sa.Params["TargetsWithDefinedController"]) == "NonTriggeredCardController" {
			nonTriggeredController = true
			triggeredCardController, triggeredCardControllerOK = effects.TriggeredCardController(e.G, sc.TriggerContext, sc.Remembered)
		}
	}
	// TargetsWithSharedCardType$ is the reference-relative sibling of the
	// controller predicate: the SAME sharedCardTypeAdmits the census post-filter
	// (filterTargetsWithSharedCardType) applies is judged here, so a target the
	// mid-resolution offer certified cannot fizzle the recheck and vice versa.
	// The key present with an unresolved reference (sharedRef 0) fails every
	// candidate closed; the key absent admits everything.
	hasSharedRef := false
	sharedRef := state.ObjID(0)
	var sharedWhitelist []string
	if ref := sharedCardTypeRef(sa); ref != "" {
		hasSharedRef = true
		sharedRef = e.sharedCardTypeReference(ref, source, sc)
		sharedWhitelist = sharedTypesWhitelist(sa)
	}
	for _, t := range targets {
		if t.IsPlayer {
			// CR 702.18 / CR 702.11 / CR 702.16c for players: a target that
			// GAINED player shroud, hexproof or protection between placement
			// and resolution is dropped here, exactly as the object arm below
			// drops a permanent that gained them -- the same judge the offer
			// (candidatesFor's player loop) applies, so offer and recheck
			// cannot disagree (the one-definition rule). Players have no zone:
			// no CR 604.3 gate is consulted on the player arm.
			if int(t.Player) < len(e.G.Players) && !e.G.Players[t.Player].Lost &&
				e.playerTargetSpecMatches(sc, spec, t.Player, you, source) &&
				!e.playerShroudBlocksTarget(t.Player) &&
				!e.playerHexproofBlocksTarget(t.Player, you, e.protectionSource(source)) &&
				!e.playerProtectedFrom(t.Player, e.protectionSource(source)) {
				legal = append(legal, t)
			}
			continue
		}
		// CR 115.5: the resolving spell or ability is an illegal target for
		// itself. self is the stack object being resolved (not source, which
		// for an ability is the source PERMANENT and so is a legal target of
		// its own ability -- e.g. a creature's "target creature" ability on
		// itself). A target chosen at cast time for a different spell -- a
		// different copy of the same card, or a permanent -- is unaffected.
		// This is why the exclusion is keyed on the resolving object id, and
		// mirrors askTarget's own withholding so the two sites always agree.
		if t.Obj == self {
			continue
		}
		// CR 702.16c: a permanent that became protected from the resolving
		// source's qualities since the target was chosen is no longer a legal
		// target, exactly as askTarget withheld it at cast time -- so a
		// previously-offered target that gained matching protection mid-race
		// is dropped from resolution too (CR 608.2b). The source is resolved
		// through protectionSource so an ability fizzling here judges "the
		// source" as its Source permanent, the same object askTarget's own
		// filter has now been made to see (Critical C2 -- one definition).
		// CR 702.14 rides the same recheck: a target that GAINED shroud
		// between placement and resolution (Lightning Greaves equipping in
		// response) is dropped here, and a target with no other legal target
		// left fizzles the whole spell/ability through the existing fizzle
		// machinery upstream of this recheck.
		if o := e.G.Obj(t.Obj); o != nil && zoneIn(o.Zone, zones) {
			// The offer post-filter's own predicate, the SAME definition: a
			// target that was offered under NonTriggeredCardController cannot
			// be inconsistent with this CR 608.2b recheck, and one whose
			// controller changed to the triggering controller (or that left
			// the battlefield) is dropped here exactly as it was withheld at
			// announcement.
			if nonTriggeredController && !nonTriggeredControllerAdmits(o, triggeredCardController, triggeredCardControllerOK) {
				continue
			}
			if o.Zone == state.ZStack && (sa == nil || !e.stackKindAdmits(
				stackTargetKindTokens(sa.Params["TargetType"]), e.stackObjKind(o), o, o.Controller, you)) {
				continue
			}
			// The cast-provenance split at the resolution recheck too
			// (wascastfrom): the token evaluates against the target's cast
			// log before the ordinary filter, so offer and recheck cannot
			// disagree about a spec carrying one.
			tspec, ok := e.castProvenanceAdmits(targetSpecForZone(spec, o.Zone), t.Obj, you)
			if ok && e.matchesSpec(tspec, t.Obj, sc) &&
				e.mentorAdmits(sa, source, t.Obj) &&
				(controllerProp == "" || e.targetControllerPropertyAdmits(controllerProp, t.Obj)) &&
				(!hasSharedRef || e.sharedCardTypeAdmits(t.Obj, sharedRef, sharedWhitelist)) &&
				(tpState == tpNone || e.tpControlsAdmits(o, you, tpChooser, tpState)) &&
				!(o.Zone == state.ZBattlefield && e.restrictionBlocksTarget(t.Obj, you)) &&
				!(o.Zone == state.ZBattlefield && e.shroudBlocksTarget(t.Obj)) &&
				!(o.Zone == state.ZBattlefield && e.hexproofBlocksTarget(t.Obj, you, e.protectionSource(source))) &&
				!e.protectedFrom(t.Obj, e.protectionSource(source)) {
				legal = append(legal, t)
			}
		}
	}
	// CR 608.2b / CR 601.2c: the per-controller targeting requirement is
	// rechecked on the surviving set too -- see narrowDifferentControllers.
	if sa != nil && strings.EqualFold(sa.Params["TargetsWithDifferentControllers"], "True") {
		legal = e.narrowDifferentControllers(legal)
	}
	if sa != nil && strings.EqualFold(sa.Params["TargetsWithSameController"], "True") {
		legal = e.narrowSameController(legal)
	}
	legal = e.narrowSetProps(sa, legal)
	return legal
}

// targetSpecForZone preserves Forge's distinction between a battlefield
// permanent and a permanent card in another zone. Forge spells both with a
// `Permanent` base (Conduit of Worlds is a real `TgtZone$ Graveyard` example),
// while the general matcher correctly treats a bare Permanent as a battlefield
// object. Rewrite only the leading base token, retaining every qualifier, so
// all target offer and target-legality callers share this rule.
func targetSpecForZone(spec string, z state.Zone) string {
	if z == state.ZBattlefield || z == state.ZStack {
		return spec
	}
	if spec == "Permanent" {
		return "PermanentCard"
	}
	if len(spec) > len("Permanent") && spec[:len("Permanent")] == "Permanent" {
		next := spec[len("Permanent")]
		if next == '.' || next == '+' || next == ',' {
			return "PermanentCard" + spec[len("Permanent"):]
		}
	}
	return spec
}

// zoneIn reports whether z is one of the zones in the set.
func zoneIn(z state.Zone, zones []state.Zone) bool {
	for _, candidate := range zones {
		if candidate == z {
			return true
		}
	}
	return false
}

// resolveAbility walks an SA chain, running each API's implementation. svars
// is the resolving face's SVar table (nil for an ability-only stack object),
// which is what lets Num's SVar indirection and primitives like Charm and
// Repeat -- which run a sub-ability named by SVar rather than the
// auto-linked "SubAbility$" -- actually resolve something outside a test.
func (e *Engine) resolveAbility(source state.ObjID, controller state.PlayerID,
	targets []state.Target, sa *cards.SA, svars map[string]string) {
	ctx := &effects.Ctx{Source: source, Controller: controller, Targets: targets}
	// Forge's Count$ResolvedThisTurn: the same (source, root Ability$ body)
	// tally resolveTop's ability branch binds, so a DBTransform gated on the
	// fourth resolution of the turn reads it here too. Zero for a synthetic
	// direct resolution that never went through the stack (the map carries no
	// entry for it), the modelled-head zero the effects case gives.
	ctx.ResolvedThisTurn = e.resolvedAbilityTallyFor(source, sa)
	ctx.ActivationsThisTurn = e.activationsThisTurnFor(source, sa)
	// The caller supplies the chosen targets -- the announcement or placement
	// ask's answer -- so the generic ValidTgts$ pre-ask must not re-pose it
	// for an SA that declares targets (task mvts1).
	if sa != nil && strings.TrimSpace(sa.Params["ValidTgts"]) != "" {
		ctx.TargetsOffered = true
	}
	effects.SetSVars(ctx, svars)
	effects.Resolve(e, ctx, sa)
}

// Game, Emit, Rand and ShuffleLibrary satisfy effects.Host, which is how effects reach the
// engine without importing it. AddContinuous (layers.go), HasKeyword
// (layers.go) and Ask (resolution.go) round out the interface -- HasKeyword
// already existed for the layer system's own callers before effects.Host
// grew a method of the same name, and needed no change to satisfy it.
func (e *Engine) Game() *state.Game                   { return e.G }
func (e *Engine) ObjectColors(o *state.Object) string { return e.objColors(o) }

// ObjectText is effects.Host's derived-text read (CR 613.1d): the object's
// printed Oracle after every layer-3 text effect already registered on it, in
// timestamp order. It is the same render Engine.Text exposes, so
// api:ExchangeTextBox exchanges each object's text AS IT CURRENTLY READS
// rather than its printed face, carrying an earlier ChangeText substitution
// across the swap instead of discarding it.
func (e *Engine) ObjectText(o *state.Object) string {
	if o == nil {
		return ""
	}
	return e.Text(o.ID)
}

// ObjectKeywords is effects.Host's derived-keyword read (CR 613.1f): the
// object's current keyword list, printed and granted alike, copied so the
// caller never aliases Derived's scratch buffer (Engine.Keywords' own
// documented hazard). api:ExchangeTextBox carries the other box's keywords
// across a text-box exchange through this read, so the grant is the partner's
// AS-IT-READS list rather than its printed face.
func (e *Engine) ObjectKeywords(o *state.Object) []string {
	if o == nil {
		return nil
	}
	return append([]string(nil), e.Derived(o.ID).Keywords...)
}
func (e *Engine) Emit(ev events.Event) { e.emit(ev) }

// CastProhibited is effects' optional castProhibitedHost read (task
// play-prohibited-election): the same CantBeCast gate beginPlay enforces
// (rules/statics.go castRestricted), exposed so effPlay's Play election never
// OFFERS a cast that CR 601.3 would refuse. It is a pure read -- no event,
// no state change -- and the beginPlay recheck stays the enforcement site.
func (e *Engine) CastProhibited(p state.PlayerID, id state.ObjID) bool {
	return e.castRestricted(p, id)
}

// EmitScryRecord is effects.Host's completed-scry-record emit (task
// scrybottom): the stand-in completion in effects' effLookAndArrange goes
// through emitScryRecord, the exact site handleArrange uses, so a scry that
// finishes without an answerable KArrange (an empty library, ScryNum$ 0, a
// no-host run) still records its zero-card bottom pile outside the
// replacement pass.
func (e *Engine) EmitScryRecord(ev events.Event) { e.emitScryRecord(ev) }

// EmitTokenCreate emits a token-creation event (TokenCreate, Encore's
// CardToken copy, or the battlefield MoveZone of a CopyToken mint) and
// returns every token whose battlefield entry the emit actually completed --
// the ids publishTokenEntry (rules/token_rest.go) published, never a mint
// still parked behind an entry-counter order. A CreateToken replacement may rewrite one would-be token
// into several mints (Divine Visitation, Doubling Season, Xorn);
// effects/token.go consults this return so its per-token riders land on
// EVERY mint, not just the first. The sink is a stack: a nested token
// creation during this emit saves and restores it, so the outer call returns
// only its own plan's mints.
func (e *Engine) EmitTokenCreate(ev events.Event) []state.ObjID {
	var ids []state.ObjID
	saved := e.tokenMintSink
	e.tokenMintSink = &ids
	savedID := e.tokenMintSinkID
	e.tokenMintSinkID = 0 // the local buffer is never a named collector
	electionsBefore := [4]bool{e.etbMove != nil, e.riotMove != nil, e.unleashMove != nil, e.siegeMove != nil}
	queued, resumeBefore, choiceBefore := len(e.replChoices), e.resume, e.tokenChoice
	e.mintParkFrom = 0
	e.mintParkElection = false
	e.emit(ev)
	e.tokenMintSink, e.tokenMintSinkID = saved, savedID
	if e.resume != nil && e.resume != resumeBefore &&
		(len(e.replChoices) > queued || (e.tokenChoice != nil && e.tokenChoice != choiceBefore)) {
		// The mint (or a later mint of its plan) parked this resolution
		// behind a replacement-order ask or a CreateToken election: report
		// it so the caller can record its continuation (SuspendTokenRest).
		e.mintParkFrom = queued + 1
	}
	if e.resume != nil && e.resume != resumeBefore && e.etbElectionParked(electionsBefore) {
		// The mint's own entry parked on an as-enters election (a Riot,
		// Unleash, Siege or generic as-enter choice posed from inside the
		// emit): the election is the continuation the collector rides, not a
		// queued competition. Report it too, with the election marker
		// SuspendTokenRest consumes (it tags the election through
		// e.pendingMintSink instead of the queue).
		e.mintParkFrom = queued + 1
		e.mintParkElection = true
	}
	return ids
}

// etbElectionParked reports whether an as-enters election (etbMove, riotMove,
// unleashMove or siegeMove) parked during the emit just finished: its parked
// move went nil→non-nil, so the entry the emit was folding is held on the
// election's ask rather than folded.
func (e *Engine) etbElectionParked(before [4]bool) bool {
	return (e.etbMove != nil && !before[0]) || (e.riotMove != nil && !before[1]) ||
		(e.unleashMove != nil && !before[2]) || (e.siegeMove != nil && !before[3])
}

// EmitStackCopy emits a StackCopy event and returns the object it actually
// minted. The copy object is created inside events.Apply's StackCopy fold, so
// effects/copy.go's RememberCopies$ rider (Forge's card.addRemembered) cannot
// see its id from Emit; this method's sink records the pre-fold NextID the
// fold's AddObject assigns. The empty return is the fold's early breaks (no
// source, source already left the stack) -- a proposed copy that minted
// nothing, which must not be remembered. Same stack discipline as
// EmitTokenCreate, so a nested stack copy cannot leak its mint to the outer
// caller.
func (e *Engine) EmitStackCopy(ev events.Event) []state.ObjID {
	var ids []state.ObjID
	saved := e.stackCopyMintSink
	e.stackCopyMintSink = &ids
	e.emit(ev)
	e.stackCopyMintSink = saved
	return ids
}
func (e *Engine) EmitDamage(ev events.Event) events.Event { return e.emit(ev) }

// EmitLifeChange reports whether the exact proposed life change was applied.
// Kept for non-exchange callers; ExchangeLifeVariant uses the transactional
// method below so transformed and parked events can complete coherently.
func (e *Engine) EmitLifeChange(ev events.Event) bool {
	queued := len(e.replChoices)
	stored := e.emit(ev)
	return e.pending == nil && len(e.replChoices) == queued && stored.Kind == events.LifeChange && stored.Player == ev.Player && stored.Amount == ev.Amount
}

// ExchangeLifeVariant carries the exchange across a CR 616 choice. The life
// change is resolved first; the source's selected characteristic is set to
// its former total only if the player's life actually changed.
func (e *Engine) ExchangeLifeVariant(ev events.Event, source state.ObjID, controller state.PlayerID, oldLife int32, setPower, setToughness bool) {
	tx := &lifeExchangeTransaction{source: source, controller: controller, oldLife: oldLife,
		player: ev.Player, lifeBefore: e.G.Players[ev.Player].Life, setPower: setPower, setToughness: setToughness}
	e.lifeExchange = tx
	e.emit(ev)
	e.lifeExchange = nil
	if e.pending == nil && len(e.replChoices) == 0 {
		e.finishLifeExchange(tx)
	}
}

func (e *Engine) finishLifeExchange(tx *lifeExchangeTransaction) {
	if tx == nil {
		return
	}
	if int(tx.player) >= len(e.G.Players) || e.G.Players[tx.player].Life == tx.lifeBefore {
		e.emit(events.Event{Kind: events.Note, Obj: tx.source, Player: tx.player, Text: "ExchangeLifeVariant abandoned: life did not change"})
		return
	}
	ce := state.ContinuousEffect{Source: tx.source, Controller: tx.controller, Affects: "Card.Self",
		Layer: state.LPT, Sub: state.SubSet, HasSet: true, SetPower: tx.oldLife, SetToughness: tx.oldLife,
		SetPowerPresent: tx.setPower, SetToughnessPresent: tx.setToughness, StaticSet: true}
	e.AddContinuous(ce)
}
func (e *Engine) Rand(n int) int { return e.rng.IntN(n) }

// shufflePlanarDeck uses the match RNG; the emitted event, rather than this
// temporary order, is the replayable state change.
func (e *Engine) shufflePlanarDeck(player state.PlayerID, order []state.ObjID) []state.ObjID {
	out := append([]state.ObjID(nil), order...)
	e.rng.Shuffle(out)
	return out
}

// Planeswalk rotates this seat's current plane to the bottom and reveals the
// next plane (CR 901.8). With fewer than two cards there is no next plane to
// reveal. The event names the plane being walked away from (Obj) so the away
// trigger has a source; the walk itself is the PlanarWalk fold.
func (e *Engine) Planeswalk(player state.PlayerID) {
	if int(player) >= len(e.G.Players) || len(e.G.Zone(state.ZPlanarDeck, player)) == 0 {
		return
	}
	var away state.ObjID
	if p := e.currentPlane(player); p != nil {
		away = p.ID
	}
	e.emit(events.Event{Kind: events.PlanarWalk, Player: player, Obj: away})
}

// PlaneswalkTo walks this seat to the named destination plane(s) instead of
// rotating the deck (CR 901.8's Defined$ planeswalk; Norn's Seedcore,
// Spatial Merging). dontPlaneswalkAway suppresses the PlaneswalkedFrom
// ability of the plane being left. A game with no planar deck, or with no
// destination still in the zone, records nothing: there is no plane to walk
// to. The event's Obj names the departing plane and IDs the destinations, so
// the fold in events/apply.go moves exactly those planes to the current
// position.
func (e *Engine) PlaneswalkTo(player state.PlayerID, dests []state.ObjID, dontPlaneswalkAway bool) {
	if int(player) >= len(e.G.Players) || len(e.G.Zone(state.ZPlanarDeck, player)) == 0 {
		return
	}
	zone := e.G.Zone(state.ZPlanarDeck, player)
	kept := make([]state.ObjID, 0, len(dests))
	for _, d := range dests {
		for _, id := range zone {
			if id == d {
				kept = append(kept, d)
				break
			}
		}
	}
	if len(kept) == 0 {
		return
	}
	var away state.ObjID
	if p := e.currentPlane(player); p != nil {
		away = p.ID
	}
	var amount int32
	if dontPlaneswalkAway {
		amount = events.PlanarWalkDontPlaneswalkAway
	}
	e.emit(events.Event{Kind: events.PlanarWalk, Player: player, Obj: away, Amount: amount, IDs: kept})
}

// ShuffleLibrary is the single library-shuffle path used by rules and effects.
// State changes only when the caller emits the resulting Shuffle event.
func (e *Engine) ShuffleLibrary(player state.PlayerID, order []state.ObjID) []state.ObjID {
	out := append([]state.ObjID(nil), order...)
	s := e.rng.chance
	if s == nil || s.planner == nil {
		e.rng.Shuffle(out)
		return out
	}
	ordinal := s.shuffleOrdinals[player]
	s.shuffleOrdinals[player] = ordinal + 1
	ctx := ShuffleContext{Player: player, Ordinal: ordinal, Library: e.shuffleCards(out), Hand: e.shuffleCards(e.G.Zone(state.ZHand, player))}
	desired, err := s.planner(ctx)
	if err != nil {
		s.fail(fmt.Errorf("hypothetical shuffle planner: %w", err))
	}
	if desired == nil {
		e.rng.Shuffle(out)
		return out
	}
	if err := e.rng.forcePermutation(out, desired); err != nil {
		s.fail(err)
	}
	return out
}

// EmitTap satisfies effects.Host's EmitTap: see emitTap.
func (e *Engine) EmitTap(obj state.ObjID, tapper state.PlayerID, entering bool) {
	e.emitTap(obj, tapper, entering)
}

// emitTap emits the plain Tap event for obj while its provenance -- who tapped
// it, and whether it is only being given its entry state -- is visible to the
// Taps/TapsForMana matcher and to trigger referents. The event payload is the
// same one every Tap producer emitted before, so no chain head moves for a
// game without such a trigger. The previous context is restored rather than
// zeroed, so a Tap emitted from inside another Tap's trigger matching cannot
// clobber the outer one.
func (e *Engine) emitTap(obj state.ObjID, tapper state.PlayerID, entering bool) {
	savedObj, savedPlayer, savedEntering := e.tapObj, e.tapPlayer, e.tapEntering
	e.tapObj, e.tapPlayer, e.tapEntering = obj, tapper, entering
	e.emit(events.Event{Kind: events.Tap, Obj: obj})
	e.tapObj, e.tapPlayer, e.tapEntering = savedObj, savedPlayer, savedEntering
}

// LegalTargets satisfies effects.Host for target-changing effects. It exposes
// the same census used by cast and trigger target decisions, so a redirect
// cannot bypass protection, CantTarget, stack-kind, zone, or filter legality.
//
// The census runs with the RESOLVING stack object as both source and
// excludeSelf whenever one exists -- exactly the placement ask's own call
// (pushTrigger -> askTarget passes the stack object id): CR 115.5 withholds
// the ability on the stack from targeting itself, never its source permanent,
// so a trigger whose source is a legal target may target it (Kor Outfitter's
// Attach sub attaches to Kor Outfitter). A direct, off-stack resolution (no
// resolving object) keeps the caller's source.
func (e *Engine) LegalTargets(chooser state.PlayerID, source state.ObjID, sa *cards.SA) []state.Target {
	if e.resolvingObj != 0 {
		source = e.resolvingObj
	}
	cs := e.legalTargetCandidates(chooser, source, source, sa)
	out := make([]state.Target, 0, len(cs))
	for _, c := range cs {
		if c.kind == "player" {
			out = append(out, state.Target{Player: c.player, IsPlayer: true})
		} else {
			out = append(out, state.Target{Obj: c.obj})
		}
	}
	return out
}

// CastThisTurn satisfies effects.Host's CastThisTurn for Count$ThisTurnCast
// (Task 17/Storm): the spells cast this turn by ANY player, counted from
// the same event log spellsCastThisTurn reads, so a replay that rebuilds the
// game derives the identical number -- never a live-only engine counter.
// Storm subtracts one (Count$ThisTurnCast/Minus1) because the resolving
// spell's own PutOnStack is already in the log by the time its trigger
// effect runs, which would otherwise overcount by exactly one.
func (e *Engine) CastThisTurn() int {
	n := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.PutOnStack {
			n++
		}
	}
	return n
}

// SpellsCastThisTurnMatching satisfies effects.Host's
// SpellsCastThisTurnMatching for Count$ThisTurnCast_<spec> (the
// "first/second spell you cast" cost modifiers and triggers): spells put on
// the stack this turn whose object matches the Forge spec. When the spec
// carries a You* qualifier the count scopes to YOU's casts; otherwise it
// counts everyone's. Derived from the event log like CastThisTurn.
func (e *Engine) SpellsCastThisTurnMatching(you state.PlayerID, spec string) int {
	return len(e.spellsCastThisTurnMatching(you, spec, 0))
}

// SpellsCastThisTurnMatchingExcluding is SpellsCastThisTurnMatching with one
// object's own cast excluded -- the bare !CastSaSource qualifier's engine
// reading (the count's "other than the spell being cast" device; effects
// stripBareCastSaSource strips the token and routes here with the ctx
// source). Derived from the event log like CastThisTurn.
func (e *Engine) SpellsCastThisTurnMatchingExcluding(you state.PlayerID, spec string, exclude state.ObjID) int {
	return len(e.spellsCastThisTurnMatching(you, spec, exclude))
}

// EachSpellCastThisTurnMatching satisfies effects.Host's method of the same
// name: the matching casts' OBJECT IDS (the ARGUMENTED !CastSaSource$<Prop>
// aggregate forms' engine side; effects' aggregateCastProperty sums the
// property over them). Derived from the event log like the count forms.
func (e *Engine) EachSpellCastThisTurnMatching(you state.PlayerID, spec string, exclude state.ObjID) []state.ObjID {
	return e.spellsCastThisTurnMatching(you, spec, exclude)
}

func (e *Engine) spellsCastThisTurnMatching(you state.PlayerID, spec string, exclude state.ObjID) []state.ObjID {
	youScoped := strings.Contains(spec, "You")
	// The CastSa count specs (Rain of Riches' gate) are evaluated per cast
	// event against THAT cast's spend window, not the object's latest one —
	// a re-cast object's older cast must not inherit the newer cast's spend
	// — so the backward walk carries a per-caster spend bucket: a negative
	// ManaAdd (a spend event carries no Obj) belongs to the NEXT PutOnStack
	// the walk reaches for its player — the cast it sits above in the log —
	// exactly the window manaSpentForCast reads for the SA-level ValidSA$
	// family. Specs without a CastSa token take the unchanged per-event
	// chain call (their castProvenanceAdmits strip is event-local and
	// stateless).
	saTokens := castSaTokensIn(spec)
	// Flag tokens (CastSa Spell.Mayhem, Spell.MayPlaySource, Spell.Warp)
	// read the cast's pay-time CastInfo
	// flags rather than a spend bucket: the backward walk records each
	// object's most recent CastInfo flags (latest-first, first write wins)
	// and the push consumes its own cast's entry, so a re-cast object's
	// older cast never inherits the newer cast's flags — the per-event
	// mirror of castSaAdmits' latest-cast read. A plain cast emits no
	// pay-time CastInfo at all, so a missing entry reads as no flags.
	wantFlags := false
	for _, tok := range saTokens {
		if tok.flag != 0 {
			wantFlags = true
		}
	}
	var castFlags map[state.ObjID]uint64
	if wantFlags {
		castFlags = make(map[state.ObjID]uint64)
	}
	// The in-flight cast's own grant walk (queueCascadeTriggers' scratch,
	// rules/cascade.go) counts PRIOR casts only: the Affected$ half of the
	// same static evaluates the current cast's own qualification, and the
	// gate's EQ0 is Forge's "the first" idiom — the twelve AffectedZone$
	// Stack SVarCompare$ gates in the corpus are all EQ0. Outside the walk
	// the count is inclusive (Vengevine's "the second creature spell" EQ2
	// gate is evaluated with the triggering cast in the log and must count
	// it).
	var buckets [8]castSpendFacts
	useAcc := len(saTokens) > 0
	var out []state.ObjID
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		switch ev.Kind {
		case events.CastInfo:
			if wantFlags {
				if _, seen := castFlags[ev.Obj]; !seen {
					castFlags[ev.Obj] = events.FlagsFrom(ev.Counter)
				}
			}
			continue
		case events.ManaAdd:
			if useAcc && ev.Amount < 0 && int(ev.Player) < len(buckets) {
				buckets[ev.Player].spent += -ev.Amount
				if tag, _, ok := state.TypedManaCounter(ev.Counter); ok {
					base, artifact := state.ManaUnitTypes(tag)
					buckets[ev.Player].tagged[base] += -ev.Amount
					if artifact && base != state.TypedArtifact {
						buckets[ev.Player].tagged[state.TypedArtifact] += -ev.Amount
					}
				}
			}
			continue
		case events.PutOnStack:
		default:
			continue
		}
		// This push closes the spend window of the cast it announces: the
		// caster's bucket holds exactly the spends since the walk start,
		// which are this cast's own (plus the caster's own post-payment
		// floating — the manaSpentForCast convention). Take the facts and
		// reset, so an older cast of the same object (a hand cast before a
		// flashback) does not inherit them and the in-flight cast's window
		// belongs to no counted cast.
		var facts castSpendFacts
		if useAcc && int(ev.Player) < len(buckets) {
			facts = buckets[ev.Player]
			buckets[ev.Player] = castSpendFacts{}
		}
		// The push itself proves a cast exists: the window's ok read.
		facts.ok = true
		// This cast's own pay-time CastInfo flags (see wantFlags above): the
		// entry recorded at the CastInfo the backward walk already passed —
		// the payment runs after the push, so its CastInfo sits BELOW the
		// push in log order — is exactly this cast's.
		var evFlags uint64
		if wantFlags {
			evFlags = castFlags[ev.Obj]
			delete(castFlags, ev.Obj)
		}
		if (e.stackGrantCast != 0 && ev.Obj == e.stackGrantCast) ||
			(e.costCompositionEvent != 0 && i+1 == e.costCompositionEvent) {
			continue
		}
		if exclude != 0 && ev.Obj == exclude {
			continue
		}
		if youScoped && ev.Player != you {
			continue
		}
		matchSpec := spec
		alive := true
		for _, tok := range saTokens {
			var held bool
			if matchSpec, held = admitProvenanceAlternatives(matchSpec, tok.token, castSaTokenHolds(tok, facts, evFlags)); !held {
				alive = false
				break
			}
		}
		if !alive {
			continue
		}
		// The bare wasCastFromYourHandByYou qualifier (the 5 end-step "if you
		// haven't cast a spell from your hand this turn" carriers'
		// Count$ThisTurnCast_Card.wasCastFromYourHandByYou bodies) is
		// evaluated per cast event against the log (task castprov1); the
		// wasCastByYou sibling (task castprov2) rides the same combined read.
		matchSpec, ok := e.castProvenanceAdmits(matchSpec, ev.Obj, you)
		if !ok {
			continue
		}
		if e.matchesSpecFrom(matchSpec, ev.Obj, you, ev.Obj) {
			out = append(out, ev.Obj)
		}
	}
	return out
}

// WasCastFromHandByYou satisfies effects.Host's WasCastFromHandByYou for the
// Count$wasCastFromYourHandByYou branch head (the Myojin cycle's etbCounter
// CheckSVar$ gate) and the Card.wasCastFromYourHandByYou filter predicate:
// obj's latest PutOnStack event names the cast that put it on the stack —
// From is the zone the cast came from, Player the caster. Provenance is
// game-long, so the scan is not bounded by the turn; if the card was later
// cast again from another zone, the latest cast wins. Derived from the event
// log like SpellsCastThisTurnMatching, so a replay derives the same answer.
func (e *Engine) WasCastFromHandByYou(obj state.ObjID, p state.PlayerID) bool {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.PutOnStack && ev.Obj == obj {
			return ev.From == state.ZHand && ev.Player == p
		}
	}
	return false
}

// WasCastFromHand satisfies effects.Host's WasCastFromHand for the BARE
// wasCastFromYourHand filter family (task castprov3 — the "from anywhere
// other than your hand" carriers whose scripts spell the predicate without
// the ByYou suffix: Vega the Watcher, Bilbo Thief in the Night, Mm'menon's
// RestrictValid$): obj's latest PutOnStack event names the cast that put it
// on the stack, and From is the zone that cast came from — ANY caster. Every
// carrier that needs player scoping supplies it elsewhere (measured over the
// 46 raw carrier files: ValidActivatingPlayer$ You on the trigger lines,
// YouCtrl or wasCastByYou in the same Affected$/Count spec). A copy was
// never cast (the rules-side split, castProvenanceAdmits, applies the same
// IsCopy guard; this read answers the log question alone); a card never put
// on the stack (cheated into play) reads false. Derived from the event log
// like WasCastFromHandByYou, so a replay derives the same answer;
// latest-cast-wins.
func (e *Engine) WasCastFromHand(obj state.ObjID) bool {
	from, _, ok := e.latestCastOrigin(obj)
	return ok && from == state.ZHand
}

// WasCastFromExile satisfies effects.Host's WasCastFromExile for the
// Count$wasCastFromExile branch head (task wascastfrom): obj's LATEST
// PutOnStack cast came from EXILE (foretell, warp, may-play — no CastFlags
// bit carries an exile origin). Derived from the event log the way
// WasCastFromHand is, so a replay derives the same answer; a copy was never
// cast (the rules-side split applies that guard, this read answers the log
// question alone); a card never put on the stack reads false.
func (e *Engine) WasCastFromExile(obj state.ObjID) bool {
	from, _, ok := e.latestCastOrigin(obj)
	return ok && from == state.ZExile
}

// DiscardedInWindow satisfies effects.Host's DiscardedInWindow for the
// ConditionDefined$ Discarded group's cost-discard channel (task
// mordorparams1, Moria Scavenger's "If the discarded card was a creature
// card"): the events.DiscardCost records of obj's own activation. The walk
// itself (and the activation-window boundary rule) is costMovesInWindow's.
func (e *Engine) DiscardedInWindow(obj state.ObjID) []state.ObjID {
	return e.costMovesInWindow(obj, events.IsDiscardCost)
}

// ReturnedInWindow satisfies effects.Host's ReturnedInWindow: the
// Return<N/Spec> cost parts obj's own activation paid (events.IsReturnCost),
// enumerated over the same activation window DiscardedInWindow scans. It is
// the ONE other user of costMovesInWindow, so a third cost-provenance window
// (a new cost action marker) reuses the walk rather than copying it.
func (e *Engine) ReturnedInWindow(obj state.ObjID) []state.ObjID {
	return e.costMovesInWindow(obj, events.IsReturnCost)
}

// costMovesInWindow walks obj's activation window backward over the event log
// and returns (in log order) every MoveZone event match admits — the cost
// acts obj's OWN activation paid, which is what the ConditionDefined$
// Discarded/Returned groups enumerate. The scan starts at obj's resolving
// wrapper and stops at the first unrelated stack push, step/turn change, pool
// clear or player loss — while crossing obj's OWN push events, because the
// two cost orderings share the one rule: an ability's cost parts are paid
// BEFORE its AbilityPush mints the wrapper (rules/cast.go's activation
// branch), a spell's AFTER its PutOnStack (the spell branch), and no other
// wrapper's push can sit between a cost payment and the resolution that
// follows it. Priority passes are deliberately NOT a boundary: an activated
// ability can sit on the stack across any number of passes before it
// resolves, and the cost it paid belongs to exactly that resolution. Derived
// from the log the way WasCastFromHandByYou is, so a replay derives the same
// answer.
func (e *Engine) costMovesInWindow(obj state.ObjID, match func(events.Event) bool) []state.ObjID {
	if obj == 0 {
		return nil
	}
	// An ability wrapper's AbilityPush carries the SOURCE permanent's id
	// (events.Apply mints the wrapper; Event.Obj names its source), so the
	// scan crosses its own push by wrapper id or source id alike.
	var src state.ObjID
	if o := e.G.Obj(obj); o != nil {
		src = o.Source
	}
	var out []state.ObjID
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		switch ev.Kind {
		case events.MoveZone:
			if match(ev) {
				out = append(out, ev.Obj)
				continue
			}
			// An ordinary move inside the window is not a boundary — an
			// ability's payment can move several cards (exile parts, tapped
			// entries) between its cost payment and its push.
			continue
		case events.PutOnStack, events.AbilityPush, events.TriggerPush,
			events.DelayedPush, events.GrantTriggerPush:
			if ev.Obj == obj || (src != 0 && ev.Obj == src) {
				continue // the resolving object's own push: cross it
			}
			return reverseIDs(out)
		case events.StepChange, events.TurnChange, events.ManaClear, events.PlayerLost:
			return reverseIDs(out)
		}
	}
	return reverseIDs(out)
}

// reverseIDs restores log order to a backward scan's collection.
func reverseIDs(in []state.ObjID) []state.ObjID {
	for i, j := 0, len(in)-1; i < j; i, j = i+1, j-1 {
		in[i], in[j] = in[j], in[i]
	}
	return in
}

// WasCast satisfies effects.Host's WasCast (Forge Card.wasCast():
// castFrom != null), the Count$IfCastInOwnMainPhase third conjunct (task
// ifcastmain1). The pending CR 601.2c announcement ask is a cast in progress:
// pushCast runs AFTER targetAsk, so the log scan alone would misread Return
// to Dust's own TargetMax$ X bound as uncast; the live pending cast closes
// that window (Forge sets castFrom before setupTargets). !e.cast.isAbility()
// excludes an ACTIVATED-ABILITY activation (printed or granted, task
// grantcost1), which Forge never treats as a cast. A copy was never cast
// (IsCopy), and a card never put on the stack (cheated into play) reads
// false. Derived from the event log plus the live pending cast, so a replay
// derives the same answer.
func (e *Engine) WasCast(obj state.ObjID) bool {
	if e.cast != nil && e.cast.card == obj && !e.cast.isAbility() {
		return true
	}
	if o := e.G.Obj(obj); o == nil || o.IsCopy {
		return false
	}
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.PutOnStack && ev.Obj == obj {
			return true
		}
	}
	return false
}

// WasCastByYou reports whether card obj was CAST AT ALL by player p — the
// bare wasCastByYou qualifier's engine read (task castprov2: the "When
// CARDNAME enters, if you cast it" ETB family — Zacama, Marina Vendrell's
// Grimoire — and Nine-Lives Familiar's etbCounter gate field): the LATEST
// PutOnStack event for this object names you as caster, whatever zone the
// cast came from (a normal hand cast, a flashback, any origin — the oracle's
// "if you cast it" does not care where from). LATEST-cast, not exists-anywhere:
// the battlefield entry this gate answers for followed the latest cast, so
// that cast is the provenance the oracle means; the corner this leaves is
// you cast it, it left the battlefield again, and an OPPONENT later cast the
// same object — the gate then reads false even though you did cast it
// (measured: no corpus carrier exercises the corner; an exists-scan would
// instead answer true for a card whose latest cast was an opponent's, the
// wider wrong). Copies were never cast; the rules-side split
// (castProvenanceAdmits) applies that guard, this read answers the log
// question alone. Derived from the event log like WasCastFromHandByYou, so
// a replay derives the same answer; a card never put on the stack (cheated
// into play) reads false. Shared approximation with the hand read: the scan
// cannot distinguish a cast from a later un-cast re-entry's provenance.
func (e *Engine) WasCastByYou(obj state.ObjID, p state.PlayerID) bool {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.PutOnStack && ev.Obj == obj {
			return ev.Player == p
		}
	}
	return false
}

// combatHit snapshots one landed combat-damage-to-player instance for the
// per-turn ledger. The dealing object is read through g.Obj at damage time
// (it is still on the battlefield then); its *cards.Card face pointer and
// controller are copied into the hit so a later reader can match the spec
// after the source has died, left the battlefield or been turned face down.
func (e *Engine) combatHit(player state.PlayerID, source state.ObjID, amount int32) effects.CombatDamageHit {
	hit := effects.CombatDamageHit{Player: player, Source: source, Amount: amount}
	if o := e.G.Obj(source); o != nil {
		hit.Card = o.Card
		hit.FaceIdx = o.FaceIdx
		hit.Controller = o.Controller
	}
	return hit
}

// CombatDamageToPlayersThisTurn satisfies effects.Host's
// CombatDamageToPlayersThisTurn: every combat-damage instance dealt to a
// player so far this turn, in assignment order, as captured at the combat
// damage site (runCombatAssignments). Engine-side, NO-EVENT state that every
// rebuild re-derives; emit clears it on TurnChange.
func (e *Engine) CombatDamageToPlayersThisTurn() []effects.CombatDamageHit {
	return e.combatHitsThisTurn
}

// LifeLostThisTurn satisfies effects.Host's LifeLostThisTurn for
// Count$LifeOppsLostThisTurn (Rakdos, Lord of Riots' cost reduction): the
// total life p lost this turn, summed from every LifeChange below zero since
// the last TurnChange. Derived from the event log like CastThisTurn, so a
// replay that rebuilds the game arrives at the same number. Life GAINED is
// not folded in — "lost life" is a loss even if the player ended the turn
// higher than they started (CR 118.3's distinction, and the reading Forge's
// own head takes).
func (e *Engine) LifeLostThisTurn(p state.PlayerID) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.LifeChange && ev.Player == p && ev.Amount < 0 {
			n += -ev.Amount
		}
	}
	return n
}

// CountersRemovedThisTurn satisfies effects.Host's CountersRemovedThisTurn
// for Count$CountersRemovedThisTurn <KIND> <player> (Blaster Hulk's per-{E}
// cast discount and Izzet Generatorium's paid-or-lost-four-or-more {E}
// activation gate): the TOTAL of player counters of kind p paid or lost this
// turn, summed from every negative-Amount PlayerCounterChange naming the kind
// (case-insensitively — the grants and the pays write the same kind text a
// card's script uses, e.g. "ENERGY") since the last TurnChange. Derived from
// the event log like LifeLostThisTurn, so a replay derives the same number.
// A payment and a loss are the same event shape — rules/mana.go's PayEnergy
// settle emits exactly this fold's input — and an object-counter removal
// (Kind CounterChange, a permanent losing counters) is deliberately NOT
// folded: the head's player form counts the PLAYER's pool only.
func (e *Engine) CountersRemovedThisTurn(p state.PlayerID, kind string) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.PlayerCounterChange && ev.Player == p && ev.Amount < 0 &&
			strings.EqualFold(ev.Counter, kind) {
			n += -ev.Amount
		}
	}
	return n
}

// CountersAddedThisTurn is the rules-side backing for the three-part
// Count$CountersAddedThisTurn head. It deliberately uses the pre-event
// snapshot retained by emit rather than the live object.
func (e *Engine) CountersAddedThisTurn(kind, actorSpec, objectSpec string, sc effects.SpecContext) int32 {
	var n int32
	for _, add := range e.counterAddsThisTurn {
		if !strings.EqualFold(kind, "Any") && !strings.EqualFold(add.kind, kind) ||
			!effects.MatchesPlayerSpec(e.G, actorSpec, add.actor, sc.You) ||
			!effects.MatchesObjectCtx(e.G, objectSpec, &add.object, sc) {
			continue
		}
		n += add.amount
	}
	return n
}

// DamageTakenThisTurn satisfies effects.Host's DamageTakenThisTurn for the
// TargetedPlayer$DamageThisTurn count head (Knollspine Dragon's "draw cards
// equal to the damage dealt to target opponent this turn"): the total damage
// p was dealt this turn, summed from every player-targeted Damage event
// since the last TurnChange. A player hit is Kind Damage with Player set
// and Obj 0 — an object hit sets Obj and leaves Player 0 (seat 0 is a real
// player, so the discriminator is Obj == 0, never Player != 0); a
// replacement-rewritten Note never reaches this fold, and a redirect that
// moved a hit onto a permanent reads there instead. Derived from the event
// log like LifeLostThisTurn, so a replay derives the same number.
func (e *Engine) DamageTakenThisTurn(p state.PlayerID) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.Damage && ev.Obj == 0 && ev.Player == p && ev.Amount > 0 {
			n += ev.Amount
		}
	}
	return n
}

// LifeGainedThisTurn satisfies effects.Host's LifeGainedThisTurn for
// Count$LifeYouGainedThisTurn (the "At the beginning of each end step, if you
// gained 4 or more life this turn" family's CheckSVar$ gate — Angelic Accord,
// Resplendent Angel, Valkyrie Harbinger): the total life p gained this turn,
// summed from every LifeChange above zero since the last TurnChange. Derived
// from the event log like LifeLostThisTurn, so a replay that rebuilds the
// game arrives at the same number. Life LOST is not folded in — "gained
// life" counts only positive LifeChanges (CR 118.3's distinction, the same
// one-sided read LifeLostThisTurn takes in the other direction).
func (e *Engine) LifeGainedThisTurn(p state.PlayerID) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.LifeChange && ev.Player == p && ev.Amount > 0 {
			n += ev.Amount
		}
	}
	return n
}

// CardsDiscardedThisTurn satisfies effects.Host's CardsDiscardedThisTurn for
// PlayerCountPropertyYou$CardsDiscardedThisTurn (Ambergris Citadel Agent's
// "X = cards you discarded this turn"): every events.IsDiscard move since
// the last TurnChange naming p — the ordinary Discard form by its Player
// field, the cost form (events.DiscardCost, which carries no Player — every
// emitter constructs it without one, so the Player field is seat 0 regardless
// of who paid) by the discarded object's owner alone, since a cost discard is
// paid from the payer's own hand (CR 118.2a). Classifying by the marker and
// not by "Player == 0 as a fallback" is what keeps seat 0's tally from
// counting every other seat's cost discard. Derived from the event log like
// LifeLostThisTurn, so a replay derives the same number.
func (e *Engine) CardsDiscardedThisTurn(p state.PlayerID) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if !events.IsDiscard(ev) {
			continue
		}
		if events.IsDiscardCost(ev) {
			if o := e.G.Obj(ev.Obj); o != nil && o.Owner == p {
				n++
			}
			continue
		}
		if ev.Player == p {
			n++
		}
	}
	return n
}

// CardsDrawnThisTurn satisfies effects.Host's CardsDrawnThisTurn for the
// PlayerCount<group>$Condition<N> CardsDrawn property (Smuggler's Share's
// "draw a card for each opponent who drew two or more cards this turn"):
// every events.Draw naming p since the last TurnChange. Derived from the
// event log like CardsDiscardedThisTurn, so a replay derives the same
// number. Every draw emitter — the draw step, an effect's Draw and the
// opening hand — emits the same event kind with Player set, so the fold
// counts them all, exactly as Forge's per-turn cardsDrawn list does.
func (e *Engine) CardsDrawnThisTurn(p state.PlayerID) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.Draw && ev.Player == p {
			n++
		}
	}
	return n
}

// SpellsCastThisTurnBy satisfies effects.Host's SpellsCastThisTurnBy for the
// PlayerCount<group>$Condition<N> SpellsCastThisTurn property (Ertai's
// Scorn / Mindbreak Trap / Whiplash Trap: "for each opponent who cast two
// or more spells this turn"): every PutOnStack naming p since the last
// TurnChange. Derived from the event log like CastThisTurn, so a replay
// derives the same number.
func (e *Engine) SpellsCastThisTurnBy(p state.PlayerID) int {
	n := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.PutOnStack && ev.Player == p {
			n++
		}
	}
	return n
}

// StartingLife satisfies effects.Host's StartingLife: the opening life total
// genesis resolved (Config.StartingLife's 0-means-20 convention already
// applied in newWithRNG). Captured at construction and copied by Clone, so a
// replay derives the same value.
func (e *Engine) StartingLife() int32 { return e.startingLife }

// TurnsTaken satisfies effects.Host's TurnsTaken for Count$YourTurns (Serra
// Avenger's "your first, second, or third turns of the game"): the number of
// turns that have BEGUN with p as the active player, current turn included.
// Derived from the event log like LifeLostThisTurn — every turn p begins
// emits exactly one TurnChange naming p (events.Apply's TurnChange case),
// extra turns included, so a replay that rebuilds the log arrives at the
// same count. The whole-log walk (not a TurnChange-bounded scan) is the
// point: the count spans the game, not one turn.
// CommanderCastsFromCommandZone satisfies effects.Host's method of the
// same name for Count$TotalCommanderCastFromCommandZone: how many times
// player p has cast one of THEIR OWN commanders from the command zone this
// game. It walks the whole log for PutOnStack events whose caster is p,
// origin is the command zone and object is one of p's commanders — the
// exact criteria recordCmdCast (rules/cast.go) applies when it maintains
// the parallel CmdCasts slice, and commitCast's PutOnStack emit is the ONE
// site that can produce such an event, so this head and the CR 903.8 tax
// can never disagree. Whole-game scope like TurnsTaken (the whole-log walk
// is the point); derived from the event log, so a replay derives the same
// number. A non-Commander seat carries an empty Commanders list, so the
// count is 0 there by construction.
func (e *Engine) CommanderCastsFromCommandZone(p state.PlayerID) int32 {
	if p < 0 || int(p) >= len(e.G.Players) {
		return 0
	}
	var n int32
	for _, ev := range e.L.Events {
		if ev.Kind != events.PutOnStack || ev.Player != p || ev.From != state.ZCommand {
			continue
		}
		for _, cid := range e.G.Players[p].Commanders {
			if cid == ev.Obj {
				n++
				break
			}
		}
	}
	return n
}

func (e *Engine) TurnsTaken(p state.PlayerID) int32 {
	if int(p) >= len(e.G.Players) {
		return 0
	}
	if len(e.turnsTaken) != len(e.G.Players) || e.turnsTakenEpoch != len(e.L.Events) {
		e.turnsTaken = make([]int32, len(e.G.Players))
		for _, ev := range e.L.Events {
			if ev.Kind == events.TurnChange && int(ev.Player) < len(e.turnsTaken) {
				e.turnsTaken[ev.Player]++
			}
		}
		e.turnsTakenEpoch = len(e.L.Events)
	}
	return e.turnsTaken[p]
}

// AttackersThisTurn satisfies effects.Host's AttackersThisTurn for
// Count$AttackersDeclared (the Raid family's "attacked this turn" read): the
// number of attackers declared this turn, summed from every DeclareAttackers
// event's attacker list since the last TurnChange. Derived from the event log
// like CastThisTurn, so a replay that rebuilds the game arrives at the same
// number. A DeclareAttackers event carries its declared attackers in IDs (one
// event per defender); an event with no IDs contributes nothing.
// AttackersDeclaredThisTurn satisfies effects.Host's method of the same
// name: this turn's DeclareAttackers attacker ids, de-duplicated, oldest
// first. Derived from the event log like AttackersThisTurn.
func (e *Engine) AttackersDeclaredThisTurn() []state.ObjID {
	start := len(e.L.Events)
	for start > 0 && e.L.Events[start-1].Kind != events.TurnChange {
		start--
	}
	var out []state.ObjID
	seen := map[state.ObjID]bool{}
	for _, ev := range e.L.Events[start:] {
		if ev.Kind != events.DeclareAttackers {
			continue
		}
		for _, id := range ev.IDs {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// LifeLostLastTurn satisfies effects.Host's LifeLostLastTurn: the negative
// LifeChanges naming p between the second-to-last and the last TurnChange
// of the log -- the previous turn's window, the LifeLostThisTurn fold one
// turn back.
func (e *Engine) LifeLostLastTurn(p state.PlayerID) int32 {
	var n int32
	boundaries := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			boundaries++
			if boundaries == 2 {
				break
			}
			continue
		}
		if boundaries == 1 && ev.Kind == events.LifeChange && ev.Player == p && ev.Amount < 0 {
			n += -ev.Amount
		}
	}
	if boundaries < 2 {
		// The window before the first TurnChange is the pregame, not a turn.
		return 0
	}
	return n
}

// AttackedDuringLastTurn satisfies effects.Host's method of the same name.
// The walk runs backwards over the log: the window after the LAST
// TurnChange is the turn in progress and is skipped; the first earlier
// TurnChange naming q opens q's most recent completed turn, whose events
// run up to the next TurnChange. A DeclareAttackers inside it whose
// Player (the defending seat) is defender and whose attacker list is
// non-empty answers true.
func (e *Engine) AttackedDuringLastTurn(q, defender state.PlayerID) bool {
	ev := e.L.Events
	end := len(ev)
	// Skip the turn in progress.
	for end > 0 && ev[end-1].Kind != events.TurnChange {
		end--
	}
	if end == 0 {
		return false
	}
	end-- // the current turn's TurnChange itself
	for end > 0 {
		start := end
		for start > 0 && ev[start-1].Kind != events.TurnChange {
			start--
		}
		if start == 0 {
			return false // the pregame window, before any turn
		}
		if ev[start-1].Player == q {
			for _, x := range ev[start:end] {
				if x.Kind == events.DeclareAttackers && x.Player == defender && len(x.IDs) > 0 {
					return true
				}
			}
			return false
		}
		end = start - 1
	}
	return false
}

func (e *Engine) AttackersThisTurn() int {
	n := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.DeclareAttackers {
			n += len(ev.IDs)
		}
	}
	return n
}

// targetsPlayers and targetsPermanents read the coarse shape of a ValidTgts
// spec. The per-object predicate work is effects.MatchesSpec.

// targetsPlayers reports whether a spec can name a player as a target, by
// looking at each alternative's BASE type (the part before any "."), never
// a substring scan: "Any" targets either an object or a player, "Player"/
// "Opponent"/"You" a player, while a predicate like "Creature.YouCtrl" IS an
// object filter (its ".YouCtrl" clause scopes the creature's controller,
// not the target being a player). The old substring form matched the "You"
// inside "YouCtrl" and wrongly offered players as Equip targets (Task 14
// found it: an Equip onto Creature.YouCtrl offered both players as options,
// which effAttach then had to refuse).
func targetsPlayers(spec string) bool {
	for alt := range strings.SplitSeq(spec, ",") {
		switch base, _, _ := strings.Cut(strings.TrimSpace(alt), "."); base {
		case "Player", "Any", "Opponent", "You":
			return true
		}
	}
	return false
}

func targetsPermanents(spec string) bool {
	for _, t := range [...]string{"Creature", "Any", "Permanent", "Artifact",
		"Enchantment", "Land", "Planeswalker", "Card"} {
		if strings.Contains(spec, t) {
			return true
		}
	}
	return false
}

// payUnlessCost charges the non-choice subset of a mid-resolution
// UnlessCost$ to payer p. Sacrifice, discard, reveal and return components
// are deliberately refused here: beginUnlessPayment owns every such component
// and gathers the payer's selected objects before it calls payMana. Keeping
// this guard makes a future caller unable to silently revive the old
// first-in-zone-order stand-in. Fixed mana/life, Mill, SubCounter and Draw
// components remain synchronous: a Draw<N/Spec> pays by drawing N cards for
// the player(s) the spec names (default the payer), resolved through the
// same Ctx roles the UnlessPayer$ grammar reads, and a Mill<N> mills from
// the top of the payer's own library through the shared payMillCost (CR
// 701.13a: every remaining card when fewer than N remain, so any library
// size is payable). The dynamic life folds
// (LifeTotalHalfUp, an announced PayLife<X>) and the energy parts (fixed and
// announced-X PayEnergy) charge here too, under the same offer gate's reads
// (unlessFoldDynamic / unlessEnergyAffordable), so the gate and the charge
// can never disagree.
func (e *Engine) payUnlessCost(p state.PlayerID, cost Cost, ctx *effects.Ctx, stackObj state.ObjID) bool {
	if len(cost.Sac) != 0 || len(cost.Discard) != 0 || len(cost.Reveal) != 0 || len(cost.RevealOrChoose) != 0 || len(cost.RevealChosen) != 0 || len(cost.Return) != 0 {
		return false
	}
	if int(p) < 0 || int(p) >= len(e.G.Players) {
		return false
	}
	folded, ok := e.unlessFoldDynamic(p, cost, ctx)
	if !ok {
		return false
	}
	cost = folded
	if !e.unlessEnergyAffordable(p, cost, ctx) {
		return false
	}
	g := e.G
	// The source the SubCounter parts drain is the activated ability's host
	// when this is an ability object, otherwise the resolving source.
	src := ctx.Source
	if o := g.Obj(stackObj); o != nil && o.Ability != nil {
		src = o.Source
	}
	type counterDrain struct {
		obj  state.ObjID
		kind string
		n    int32
	}
	var drains []counterDrain
	for _, part := range cost.SubCounter {
		o := g.Obj(src)
		if o == nil {
			return false
		}
		have := int32(0)
		for _, ct := range o.Counters {
			if ct.Kind == part.Spec {
				have += ct.N
			}
		}
		if have < part.N {
			return false
		}
		drains = append(drains, counterDrain{obj: o.ID, kind: part.Spec, n: part.N})
	}
	// Resolve every drawer before charging mana/life. A Draw component whose
	// role is unavailable makes the entire cost unpayable; validating first
	// avoids a partial payment followed by a silent omitted draw.
	drawers := make([][]state.PlayerID, len(cost.Draw))
	for i, part := range cost.Draw {
		players, ok := unlessDrawPlayers(ctx, p, part.Spec)
		if !ok {
			return false
		}
		for _, dp := range players {
			if int(dp) < 0 || int(dp) >= len(g.Players) {
				return false
			}
		}
		drawers[i] = players
	}
	// Everything is affordable: charge mana/life through ordinary events,
	// then apply the synchronous counter components, then the draws. The
	// resolving object is the payment subject, so its ManaConvert statics
	// (including EffectZone$ Command and Effect-delivered grants) apply here
	// under the same conversion read used by cast offers.
	if !e.payManaConv(p, cost, e.paymentConv(p, stackObj, false)) {
		return false
	}
	// The energy parts charge through the ONE shared site (CR 118.2d); the
	// offer gate proved the total affordable and the fold above proved every
	// dynamic part bound, so the charge cannot half-apply.
	x := int32(0)
	if ctx != nil && ctx.XAnnounced {
		x = ctx.X
	}
	e.chargeEnergyCost(p, cost, x)
	// Mill parts (Mill<N>) settle through the ONE shared mill site, after
	// every payability check above has passed and beside the other charges,
	// so the ordinary cast/activation cost and an unless cost cannot diverge.
	// CR 701.13a: the payer mills the SUM of the parts' requirements, taking
	// every remaining card when the library is short, so this never turns an
	// empty or short library into an unpayable cost.
	e.payMillCost(p, cost.Mill)
	for _, d := range drains {
		e.emit(events.Event{Kind: events.CounterChange, Obj: d.obj, Counter: d.kind, Amount: -d.n})
	}
	for i, part := range cost.Draw {
		for _, dp := range drawers[i] {
			for n := int32(0); n < part.N; n++ {
				effects.DrawFor(e, dp)
			}
		}
	}
	return true
}

// unlessDrawPlayers resolves a Draw<N/Spec> cost component's drawer(s). The
// empty spec and "You" are the payer; every other spelling is one of the
// player roles the unless-payment context carries, and an unresolvable or
// unknown spec fails closed (the cost was not paid).
func unlessDrawPlayers(ctx *effects.Ctx, payer state.PlayerID, spec string) ([]state.PlayerID, bool) {
	one := func(t state.Target) ([]state.PlayerID, bool) {
		if t.IsPlayer {
			return []state.PlayerID{t.Player}, true
		}
		return nil, false
	}
	switch spec {
	case "", "You", "Player", "Self":
		return []state.PlayerID{payer}, true
	case "Player.targetedBy", "Targeted", "TargetedPlayer":
		if len(ctx.Targets) == 0 {
			return nil, false
		}
		return one(ctx.Targets[0])
	case "Player.Activator", "TriggeredActivator":
		return one(ctx.TriggerActivator)
	case "Player.TriggeredPlayer", "TriggeredPlayer":
		return one(ctx.TriggerPlayer)
	case "Player.TriggeredTarget", "TriggeredTarget":
		return one(ctx.TriggerTarget)
	}
	return nil, false
}
