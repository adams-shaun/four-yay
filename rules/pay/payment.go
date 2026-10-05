package pay

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// payment.go is mana payment (moved from package rules' mana_payment.go,
// E7): paying a cost from a seat's pool through events, the
// restriction-aware view of what the pool can pay for one payment
// descriptor, RestrictValid$ matching, and the CostPayable offer gates that
// run the same solver the payment does. The engine is reached only through
// the Engine interface.

// PayLifeProposalText marks a negative LifeChange emitted at a cost-payment
// boundary. rules consumes it synchronously before logging; it is never part
// of the replay log.
const PayLifeProposalText = "paylife proposal"

// manaTaggedLetters[t][i] is state.ManaUnitTags[t]+ManaLetters[i] and
// manaSnowLetters[i] is "S"+ManaLetters[i]: the spend split's Counter forms,
// built once instead of per payment.
var manaTaggedLetters, manaSnowLetters = func() (tagged [len(state.ManaUnitTags)][len(ManaLetters)]string, snow [len(ManaLetters)]string) {
	for i, letter := range ManaLetters {
		for t, tag := range state.ManaUnitTags {
			tagged[t][i] = tag + letter
		}
		snow[i] = "S" + letter
	}
	return tagged, snow
}()

// PayMana spends cost from p's pool and reports whether it could. Every
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
func PayMana(e Engine, p state.PlayerID, cost Cost) bool {
	ok, _, _, _, _ := PayManaDescriptorForSpent(e, p, Descriptor{Class: PurposeOther, Cost: &cost}, cost, nil, PipRider{})
	return ok
}

// PayManaConv is PayMana under a stat:ManaConvert conversion set (or nil,
// the plain exact-colour payment PayMana always was). The conversion widens
// (and the <-C restriction narrows) what the pool's mana may pay, never what
// the cost demands.
func PayManaConv(e Engine, p state.PlayerID, cost Cost, conv *Conv) bool {
	ok, _, _, _, _ := PayManaDescriptorForSpent(e, p, Descriptor{Class: PurposeOther, Cost: &cost}, cost, conv, PipRider{})
	return ok
}

// PayManaCumulative pays a cumulative-upkeep cost for id: the restriction
// class CumulativeUpkeep admits, with the cost's X announced when it has one.
func PayManaCumulative(e Engine, p state.PlayerID, id state.ObjID, cost Cost, conv *Conv) bool {
	ok, _, _, _, _ := PayManaDescriptorForSpent(e, p, Descriptor{ID: id, Class: PurposeCumulativeUpkeep,
		Cost: &cost, XAnnounced: cost.X > 0}, cost, conv, PipRider{})
	return ok
}

// PayManaConvFor pays a specific spell or activated ability. RestrictValid$
// mana remains distinct from ordinary floating mana until this point: it is
// included only when its restriction admits this payment, then spent first
// and marked on the negative ManaAdd event so events.Apply can reconstruct
// the same provenance during replay.
func PayManaConvFor(e Engine, p state.PlayerID, id state.ObjID, ability bool, cost Cost, conv *Conv) bool {
	return PayManaFor(e, p, id, ability, cost, conv, PipRider{})
}

// PayManaFor is PayManaConvFor with the may-play ignore-colour rider passed
// explicitly, so the payment sites that know the cast's recorded rider (a
// pendingCast's mayPlayIgnore, kept from the offer gate that proved it) keep
// the grant after the card has moved to the stack -- at payment time the
// card is no longer in the granted zone, so re-deriving from the zone would
// wrongly drop it.
func PayManaFor(e Engine, p state.PlayerID, id state.ObjID, ability bool, cost Cost, conv *Conv, rider PipRider) bool {
	ok, _, _, _, _ := PayManaForSpent(e, p, id, ability, cost, conv, rider)
	return ok
}

// PayManaForSpent is PayManaFor with the payment's actually-spent mana
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
// keeps the bool-only PayManaFor wrapper, so no other payment site changes
// shape.
func PayManaForSpent(e Engine, p state.PlayerID, id state.ObjID, ability bool, cost Cost, conv *Conv, rider PipRider) (bool, state.Mana, state.Mana, state.Mana, [7]state.Mana) {
	class := PurposeSpell
	if ability {
		class = PurposeActivated
	}
	return PayManaDescriptorForSpent(e, p, Descriptor{ID: id, Class: class, Cost: &cost}, cost, conv, rider)
}

// PayManaDescriptorForSpent is the one payment core every PayMana* form
// reaches: it resolves cost against the pool visible to d and emits the spend.
func PayManaDescriptorForSpent(e Engine, p state.PlayerID, d Descriptor, cost Cost, conv *Conv, rider PipRider) (bool, state.Mana, state.Mana, state.Mana, [7]state.Mana) {
	av := AvailableFor(e, p, d)
	// The payment's persistence attribution: the visible pool's persistent
	// share (perVis) and its ordinary complement (perFresh). ResolveMana is
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
	perVis := visiblePersistentMana(e, p, d)
	perFresh := state.Mana{}
	for i := range perFresh {
		perFresh[i] = av.Pool[i] - perVis[i]
	}
	before := av.Pool
	beforeSnow := e.Game().Players[p].Snow
	beforeTyped := av.Typed
	pay, ok := ResolveManaWith(cost, before, beforeSnow, beforeTyped, e.Game().Players[p].Life,
		e.Eval().PayLifeInsteadOfB(p), rider, conv)

	if !ok {
		return false, state.Mana{}, state.Mana{}, state.Mana{}, [7]state.Mana{}
	}
	after, afterSnow, afterTyped, lifeSpent := pay.Pool, pay.Snow, pay.Typed, pay.LifeSpent
	spent := state.Mana{}
	spentSnow := state.Mana{}
	spentTyped := [7]state.Mana{}
	for i := range before {
		spent[i] = before[i] - after[i]
		// The parallel tallies' own deltas: how many of the units that left
		// slot i were snow / typed units. ResolveManaWith consumes a plain
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
	emitRestrictedManaSpend(e, p, d, &spent, &emitSnow, &emitTyped, &perVis, &perFresh)
	for i, letter := range ManaLetters {
		if spent[i] == 0 {
			continue
		}
		// A slot whose snow / typed units were spent (all or part) emits the
		// "S<colour>" / "<Tag><colour>" Counter forms so the parallel tallies
		// move with the pool through the same events the adds used.
		// ResolveMana consumes a plain unit before a typed one and a typed
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
				e.Emit(events.Event{Kind: events.ManaAdd, Player: p, Counter: counter, Amount: -ord})
			}
			if per := n - ord; per > 0 {
				e.Emit(events.Event{Kind: events.ManaAdd, Player: p, Counter: counter,
					Amount: -per, Text: events.ManaPersistentText("")})
			}
		}
		snowSpent := emitSnow[i]
		typedSpent := int32(0)
		for t := range emitTyped {
			typedSpent += emitTyped[t][i]
		}
		emitSpend(letter, spent[i]-snowSpent-typedSpent)
		for t := range state.ManaUnitTags {
			if emitTyped[t][i] > 0 {
				emitSpend(manaTaggedLetters[t][i], emitTyped[t][i])
			}
		}
		if snowSpent > 0 {
			emitSpend(manaSnowLetters[i], snowSpent)
		}
	}
	// Fixed life costs and any Phyrexian pips paid with life are deducted
	// through the ordinary LifeChange event so a replay learns them.
	if lifeSpent != 0 {
		e.Emit(events.Event{Kind: events.LifeChange, Player: p, Amount: -lifeSpent, Text: PayLifeProposalText})
	}
	return true, spentAll, spent, spentSnow, spentTyped
}

// PayExtortPip charges the {W/B} hybrid pip (one mana of either W or B)
// from p's pool, emitting the ManaAdd events so a replay re-derives it. It
// returns false (and charges nothing) when the pool has neither colour, so
// an Extort payment a player genuinely cannot make is a decline rather than
// a free drain.
func PayExtortPip(e Engine, p state.PlayerID) bool {
	// The pip is charged from the raw pool (the extort window carries no
	// payment id, so the restriction-aware view does not apply here — the
	// pre-existing, restriction-blind consumption is unchanged), but its
	// attribution follows the shared payment rule (ordinary units first): a
	// slot's persistent share is charged only past its non-persistent one,
	// and the persistent unit's event carries the " pm" marker so the fold
	// moves the tally with the unit actually consumed.
	pl := e.Game().Players[p]
	fresh := pl.Pool
	per := pl.PersistentMana
	for i := range fresh {
		fresh[i] -= per[i]
	}
	for _, idx := range []int{state.MW, state.MB} {
		if fresh[idx] > 0 {
			e.Emit(events.Event{Kind: events.ManaAdd, Player: p,
				Counter: ManaLetters[idx], Amount: -1})
			return true
		}
	}
	for _, idx := range []int{state.MW, state.MB} {
		if per[idx] > 0 {
			e.Emit(events.Event{Kind: events.ManaAdd, Player: p,
				Counter: ManaLetters[idx], Amount: -1,
				Text: events.ManaPersistentText("")})
			return true
		}
	}
	return false
}

// Available is the restriction-aware view of a seat's floating mana:
// the pool minus every RestrictValid$ batch this payment cannot use, and the
// typed producer tallies with those same batches removed. A restricted TYPED
// unit (Echoing Cavern's Cave mana) must be invisible in BOTH: takeUnit
// partitions a slot by the typed tally, so a typed unit left visible under
// an unusable restriction could be consumed through that path although the
// filter already hid it from the pool.
type Available struct {
	Pool  state.Mana
	Typed [7]state.Mana
}

// Purpose is what a payment pays for, which RestrictValid$ classes read.
type Purpose uint8

const (
	PurposeSpell Purpose = iota
	PurposeActivated
	PurposeCumulativeUpkeep
	// PurposeOther is a real mana payment (ward, unless-pay, attack costs,
	// triggered costs, etc.) whose caller has no cast or activation
	// descriptor. It must not inherit PurposeSpell: bare RestrictValid$ Spell
	// admits only a spell cast, and unknown/unclassified payments fail closed.
	PurposeOther
)

// Descriptor identifies one payment: the object paid for, its Purpose, the
// real cost, and whether an X was announced.
type Descriptor struct {
	ID    state.ObjID
	Class Purpose
	Cost  *Cost
	// XAnnounced records that the payment's cost carried an X the
	// announcement machinery has already folded (Cost.WithX clears Cost.X
	// once a value is chosen, so the normalized cost alone can no longer
	// identify an X payment for the CostContainsX restriction). Raw-cost
	// sites (offer gates, mana-ability activations) get it from DescriptorFor's
	// own derivation; the post-fold payment sites set it explicitly through
	// paymentForCast. Offer and payment must agree about an X cost, never
	// drift.
	XAnnounced bool
}

// DescriptorFor is the descriptor for paying cost for id (a spell, or an
// activated ability when ability is set), X derived from the raw cost.
func DescriptorFor(id state.ObjID, ability bool, cost Cost) Descriptor {
	class := PurposeSpell
	if ability {
		class = PurposeActivated
	}
	return Descriptor{ID: id, Class: class, Cost: &cost, XAnnounced: cost.X > 0}
}

// AvailableFor removes every restricted batch from the visible pool, then
// restores exactly the batches valid for this payment. This means a cast or a
// nonmatching activation can never borrow Tazri-style mana merely because it
// shares a colour bucket with unrestricted mana. The typed tallies are
// filtered by the same rule, so a typed restricted unit can never be spent
// through the typed consumption path either. The descriptor carries the real
// payment: a cost-blind descriptor misreads every cost-keyed dotless term
// (CostContainsX, CostContainsC, CantPayGenericCosts), so callers without a
// real cost must say so with Cost{} and stay on the class-only terms.
func AvailableFor(e Engine, p state.PlayerID, d Descriptor) Available {
	pl := e.Game().Players[p]
	available := Available{Pool: pl.Pool, Typed: pl.ManaUnits()}
	for _, r := range pl.RestrictedMana {
		idx := state.ManaSlot(r.Color)
		available.Pool[idx] -= r.Amount
		// An empty Valid is an UNRESTRICTED batch that carries only its
		// AddsNoCounter$ provenance (Boseiju's plain {C}): it pays anything,
		// exactly like ordinary pool mana, so its units stay visible.
		if r.Valid == "" || RestrictValidMatches(e, p, d, r.Valid, r.Source) {
			available.Pool[idx] += r.Amount
			continue
		}
		// The batch is unusable here: hide its typed provenance too.
		if tag, slot, ok := state.TypedManaCounter(r.Color); ok {
			available.Typed[tag][slot] -= r.Amount
		}
	}
	return available
}

// visiblePersistentMana is the persistent share of p's VISIBLE pool for this
// payment: the PersistentMana tally minus every persistent restriction batch
// this payment cannot use. AvailableFor hides an unusable batch's units
// from the payment, so those units cannot be what a spend here consumed and
// must not be attributed to it; the same hiding rule keeps this view and the
// visible pool from disagreeing. (An empty Valid is an unrestricted
// AddsNoCounter batch — spendable anywhere — so its units stay attributed.)
// Measured corpus: every PersistentMana carrier produces plain mana, so the
// persistent share never carries a snow/typed tag in practice.
func visiblePersistentMana(e Engine, p state.PlayerID, d Descriptor) state.Mana {
	pl := e.Game().Players[p]
	per := pl.PersistentMana
	for _, r := range pl.RestrictedMana {
		if !r.Persistent || (r.Valid != "" && RestrictValidMatches(e, p, d, r.Valid, r.Source)) {
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
// the engine's Capture for payCast to fold state.FlagNoCounter into the pay-time
// CastInfo — with the batch's own condition evaluated against the paying
// spell's face (Boseiju's !Permanent).
//
// The carve is capped by the units ResolveMana's search ACTUALLY attributed to
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
func emitRestrictedManaSpend(e Engine, p state.PlayerID, d Descriptor, spent *state.Mana, emitSnow *state.Mana, emitTyped *[7]state.Mana, perVis *state.Mana, perFresh *state.Mana) {
	capt := e.Session()
	capt.NoCounterSpend = 0
	capt.ManaSpentSources = nil
	capt.ManaSpentAddsCounters = nil
	// Emit mutates RestrictedMana through events.Apply, so range a snapshot:
	// otherwise removing the first of two matching batches would make the
	// live slice shift under this loop and could skip or double-spend one.
	batches := append([]state.ManaRestriction(nil), e.Game().Players[p].RestrictedMana...)
	for _, r := range batches {
		if r.Amount <= 0 || (r.Valid != "" && !RestrictValidMatches(e, p, d, r.Valid, r.Source)) {
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
		if r.NoCounter != "" && d.Class == PurposeSpell && capt.NoCounterSpend == 0 && addsNoCounterHolds(e.Game(), d.ID, r.NoCounter) {
			capt.NoCounterSpend = d.ID
		}
		// A consumed batch's producing source keys TriggersWhenSpent$.
		// Capture spell and activated-ability payments; PurposeOther (including
		// unless-pay) and every other unclassified payment do not dispatch.
		// Dedup keeps one entry per source, in deterministic batch order.
		if (d.Class == PurposeSpell || d.Class == PurposeActivated) && r.Source != 0 && !slices.Contains(capt.ManaSpentSources, r.Source) {
			capt.ManaSpentSources = append(capt.ManaSpentSources, r.Source)
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
		if (d.Class == PurposeSpell || d.Class == PurposeActivated) && strings.TrimSpace(r.AddsCounters) != "" {
			if g, ok := e.AddsCounterGrant(r, used); ok {
				capt.ManaSpentAddsCounters = append(capt.ManaSpentAddsCounters, g)
			}
		}
		e.Emit(events.Event{Kind: events.ManaAdd, Player: p, Counter: r.Color, Amount: -used,
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

// addsNoCounterHolds evaluates a consumed batch's AddsNoCounter$ condition
// against the spell being paid for: "True" (or the empty default) always
// holds; "NotPermanent" (Forge's AddsNoCounter$ !Permanent, Boseiju's
// instant-or-sorcery mana) holds when the paying spell is not a permanent
// spell. An unrecognised condition fails closed — no protection.
func addsNoCounterHolds(g *state.Game, id state.ObjID, cond string) bool {
	switch addsNoCounterHoldsCodes.Code(string(cond)) {
	case addsNoCounterHoldsTrue:
		return true
	case addsNoCounterHoldsNotPermanent:
		o := g.Obj(id)
		return o != nil && o.Face() != nil && !o.Face().IsPermanent()
	default:
		return false
	}
}

// RestrictValidMatches evaluates RestrictValid$'s payment class. Forge spells
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
func RestrictValidMatches(e Engine, p state.PlayerID, d Descriptor, valid string, src state.ObjID) bool {
	for term := range strings.SplitSeq(strings.TrimSpace(valid), ",") {
		if restrictValidTermMatches(e, p, d, strings.TrimSpace(term), src) {
			return true
		}
	}
	return false
}

func restrictValidTermMatches(e Engine, p state.PlayerID, d Descriptor, term string, src state.ObjID) bool {
	kind, spec, dotted := strings.Cut(term, ".")
	if !dotted {
		switch restrictValidTermMatchesCodes.Code(string(term)) {
		case restrictValidTermMatchesSpell:
			return d.Class == PurposeSpell
		case restrictValidTermMatchesActivated:
			return d.Class == PurposeActivated
		case restrictValidTermMatchesCantCastNonArtifactSpells:
			o := e.Game().Obj(d.ID)
			return d.Class == PurposeSpell && o != nil && o.Face() != nil && o.Face().IsArtifact()
		case restrictValidTermMatchesCantCastSpellFromHand:
			_, ok := e.CastProvenanceAdmitsPending("Card.!wasCastFromYourHand", d.ID, p)
			return d.Class == PurposeSpell && ok
		case restrictValidTermMatchesCostContainsX:
			// An announced X was folded into Generic (Cost.WithX clears
			// Cost.X), so the descriptor's marker carries it — the cost the
			// payment actually commits still contains an X component.
			return d.Cost != nil && (d.Cost.X > 0 || d.XAnnounced)
		case restrictValidTermMatchesCostContainsC:
			return d.Cost != nil && d.Cost.Colored[state.ManaIndex('C')] > 0
		case restrictValidTermMatchesCantPayGenericCosts:
			// Read the actual resolved payment. Before its X and twobrid faces
			// have been announced, the offer stays open if a colour / X=0 face
			// can be selected; announceFeasible then rechecks the descriptor
			// after that face is folded. Thus {2/W} may use this mana as {W},
			// but not as {2}, and an X spell can choose only X=0 here.
			return d.Cost != nil && d.Cost.Generic == 0
		case restrictValidTermMatchesCumulativeUpkeep:
			return d.Class == PurposeCumulativeUpkeep
		default:
			return false
		}
	}
	ability := d.Class == PurposeActivated
	if kind == "Activated" && !ability {
		return false
	}
	if kind == "Spell" && d.Class != PurposeSpell {
		return false
	}
	if kind != "Activated" && kind != "Spell" {
		return false
	}
	needsBattlefield := strings.Contains(spec, "inZoneBattlefield")
	spec = strings.Trim(strings.ReplaceAll(spec, "+inZoneBattlefield", ""), "+")
	o := e.Game().Obj(d.ID)
	if o == nil || (needsBattlefield && o.Zone != state.ZBattlefield) {
		return false
	}
	if spec == "" {
		return true
	}
	srcID := d.ID
	if src != 0 {
		srcID = src
	}
	// The bare wasCastFromYourHand qualifier (castprov3, Mm'menon's
	// RestrictValid$ Spell.!wasCastFromYourHand — "spend this mana only to
	// cast a spell from anywhere other than your hand"): split the
	// provenance out before the filter match, through the pending-cast
	// variant — the offer-side affordability walk (castable → CostPayable)
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
	spec, provenanceOK = e.CastProvenanceAdmitsPending("Card."+spec, d.ID, p)
	if !provenanceOK {
		return false
	}
	if e.MatchesSpecFrom(spec, d.ID, p, srcID) {
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
	return e.MatchesSpecFrom("Card."+spec, d.ID, p, srcID)
}

// CostPayableGrant is CostPayable with the may-play ignore-colour rider
// passed explicitly, for the payment sites that know the cast's recorded
// rider and cannot re-derive it from the card's zone.
func CostPayableGrant(e Engine, p state.PlayerID, id state.ObjID, ability bool, cost Cost, rider PipRider) bool {
	// The descriptor carries the REAL cost: an offer gate priced against a
	// descriptor with an empty cost would hide every cost-keyed restricted
	// batch (CostContainsX, CostContainsC) even when the payment itself
	// admits it — the offer and the payment must read the same cost.
	av := AvailableFor(e, p, DescriptorFor(id, ability, cost))
	_, ok := ResolveManaWith(cost, av.Pool, e.Game().Players[p].Snow, av.Typed,
		e.Game().Players[p].Life, e.Eval().PayLifeInsteadOfB(p), rider, e.Eval().Conv(p, id, ability))

	return ok
}

// CostPayableClass is CostPayableClassLife with the payer's derived
// PayLifeInsteadOf:B grant.
func CostPayableClass(e Engine, p state.PlayerID, d Descriptor, rider PipRider, cost Cost) bool {
	return CostPayableClassLife(e, p, d, rider, cost, e.Eval().PayLifeInsteadOfB(p))
}

// CostPayableClassLife is CostPayableClass with the payer's
// PayLifeInsteadOf:B grant selected by the caller instead of always derived.
// The CR 601.2g mana-window gate (manaWindowAsk) passes false: that gate asks
// whether the POOL ALONE pays the cost, and a {B} pip the grant would settle
// with 2 life must not answer that question yes -- otherwise the window never
// opens, the payer never gets K'rrik's "may pay 2 life rather than pay that
// mana" choice, and the life is spent silently. Every other caller keeps the
// derived grant (passing true), because a cost only life can pay must still be
// offered and charged as such; the payment itself
// (PayManaDescriptorForSpent) also keeps the grant, so a payer who declines
// the window still spends the life.
func CostPayableClassLife(e Engine, p state.PlayerID, d Descriptor, rider PipRider, cost Cost, lifeGrant bool) bool {
	av := AvailableFor(e, p, d)
	_, ok := ResolveManaWith(cost, av.Pool, e.Game().Players[p].Snow, av.Typed,
		e.Game().Players[p].Life, lifeGrant, rider, e.Eval().Conv(p, d.ID, d.Class == PurposeActivated))

	return ok
}

// CostPayableOther is the offer-side partner of context-free PayMana and
// PayManaConv windows. A cost paid outside casting or activating an ability
// must not borrow spell- or activation-restricted mana merely because its
// source happens to be a card object.
func CostPayableOther(e Engine, p state.PlayerID, id state.ObjID, cost Cost) bool {
	return CostPayableClass(e, p, Descriptor{ID: id, Class: PurposeOther, Cost: &cost},
		e.Eval().MayPlayRider(p, id), cost)
}

// CostPayable is the conversion-aware equivalent of Cost.payable at the
// offering and window gates: the SAME ResolveMana PayMana will run, so an
// offered cost and the cost actually charged can never disagree about what
// the payer's converted mana may satisfy. The payer-side grants (a
// PayLifeInsteadOf:B static under its controller; a may-play grant's
// MayPlayIgnoreColor$ rider, derived from the card's current zone) are
// applied here too, so an offered cost and the charged cost agree about a
// K'rrik-shaped or may-play-shaped payment as well.
func CostPayable(e Engine, p state.PlayerID, id state.ObjID, ability bool, cost Cost) bool {
	return CostPayableGrant(e, p, id, ability, cost,
		e.Eval().MayPlayRider(p, id))
}

// CostPayablePool is CostPayable priced against an EXPLICIT pool instead of
// the seat's restriction-adjusted floating one: pool is the mana the cost
// must resolve against, whatever the seat is actually holding right now. The
// ordinary gates (CostPayable here, manaFeasible in statics.go) are exactly
// this with the real AvailableFor pool and never call it directly with
// the RAW pool -- offering a cast on mana its RestrictValid$ provenance would
// refuse at payment is the illegal direction (rv2c review: an earlier shape
// of castable did, and was reverted). The only caller is the potential-action
// walk (rules/legal.go legalActionsPriced via castablePriced), which passes
// the hypothetical bound the seat would hold after floating every untapped
// source. The payer grants and conversion shaping are the same reads in both
// modes, so a potential action and the payment it promises can never disagree
// about what the pool may satisfy.
func CostPayablePool(e Engine, p state.PlayerID, id state.ObjID, ability bool, cost Cost, pool state.Mana, typed [7]state.Mana) bool {
	if !cost.HasPips() {
		// The B-life grant, the may-play riders and the ManaConvert set only
		// ever widen or narrow a PIP's alternatives (costPips, pipAccepts);
		// a pip-free cost -- the bare {T} of nearly every mana ability --
		// resolves to exactly the life and generic totals whatever they
		// are, so the three whole-board reads are skipped, not changed.
		_, ok := ResolveManaWith(cost, pool, e.Game().Players[p].Snow, typed, e.Game().Players[p].Life, false, PipRider{}, nil)
		if e.Verify() {
			_, slow := ResolveManaWith(cost, pool, e.Game().Players[p].Snow, typed, e.Game().Players[p].Life,
				e.Eval().PayLifeInsteadOfB(p),
				e.Eval().MayPlayRider(p, id),
				e.Eval().Conv(p, id, ability))

			if slow != ok {
				panic("rules: pip-free CostPayablePool fast path disagrees with the full resolve")
			}
		}
		return ok
	}
	_, ok := ResolveManaWith(cost, pool, e.Game().Players[p].Snow, typed, e.Game().Players[p].Life,
		e.Eval().PayLifeInsteadOfB(p),
		e.Eval().MayPlayRider(p, id),
		e.Eval().Conv(p, id, ability))

	return ok
}

type addsNoCounterHoldsCode uint16

const (
	addsNoCounterHoldsTrue addsNoCounterHoldsCode = iota + 1
	addsNoCounterHoldsNotPermanent
)

var addsNoCounterHoldsCodes = state.NewStrCodes(
	state.StrEntry[addsNoCounterHoldsCode]{Key: "", Val: addsNoCounterHoldsTrue},
	state.StrEntry[addsNoCounterHoldsCode]{Key: "True", Val: addsNoCounterHoldsTrue},
	state.StrEntry[addsNoCounterHoldsCode]{Key: "NotPermanent", Val: addsNoCounterHoldsNotPermanent},
)

type restrictValidTermMatchesCode uint16

const (
	restrictValidTermMatchesSpell restrictValidTermMatchesCode = iota + 1
	restrictValidTermMatchesActivated
	restrictValidTermMatchesCantCastNonArtifactSpells
	restrictValidTermMatchesCantCastSpellFromHand
	restrictValidTermMatchesCostContainsX
	restrictValidTermMatchesCostContainsC
	restrictValidTermMatchesCantPayGenericCosts
	restrictValidTermMatchesCumulativeUpkeep
)

var restrictValidTermMatchesCodes = state.NewStrCodes(
	state.StrEntry[restrictValidTermMatchesCode]{Key: "Spell", Val: restrictValidTermMatchesSpell},
	state.StrEntry[restrictValidTermMatchesCode]{Key: "Activated", Val: restrictValidTermMatchesActivated},
	state.StrEntry[restrictValidTermMatchesCode]{Key: "nonSpell", Val: restrictValidTermMatchesActivated},
	state.StrEntry[restrictValidTermMatchesCode]{Key: "CantCastNonArtifactSpells", Val: restrictValidTermMatchesCantCastNonArtifactSpells},
	state.StrEntry[restrictValidTermMatchesCode]{Key: "CantCastSpellFromHand", Val: restrictValidTermMatchesCantCastSpellFromHand},
	state.StrEntry[restrictValidTermMatchesCode]{Key: "CostContainsX", Val: restrictValidTermMatchesCostContainsX},
	state.StrEntry[restrictValidTermMatchesCode]{Key: "CostContainsC", Val: restrictValidTermMatchesCostContainsC},
	state.StrEntry[restrictValidTermMatchesCode]{Key: "CantPayGenericCosts", Val: restrictValidTermMatchesCantPayGenericCosts},
	state.StrEntry[restrictValidTermMatchesCode]{Key: "CumulativeUpkeep", Val: restrictValidTermMatchesCumulativeUpkeep},
)
