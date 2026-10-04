package pay

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// Session is the payment layer's engine-owned state (lasagna spec §9.1-9.2):
// the ring's fields, moved off the Engine field list. Package rules embeds it
// in its Engine (unexported, as paySession), so rules code reads the fields
// as before (promoted) and the payment layer reaches them through
// Engine.Session. Every field carries its clone policy; rules' Clone
// generator flattens the embedded struct like any Engine cluster.
type Session struct {
	// UnlessPayment carries an in-progress non-mana unless-cost payment. It
	// keeps the enclosing resolution suspended while the payer chooses the
	// sacrifice/discard objects that pay it.
	UnlessPayment *UnlessPayment `clone:"deep"`

	// PaymentStats is the optional auto-pay diagnostics sink
	// (SetPaymentPlanStats, rules/payment_plan_stats.go). Like
	// ManaAbilityHook it is a harness-only observer: nil by default, it emits
	// nothing, never changes an offer, and Clone deliberately does not copy
	// it (spec §7: no pointer is shared across engines).
	PaymentStats *PlanStats `clone:"hook"`

	// PaymentPlanRelaxed is PotentialPaymentPlans' transient proof mode
	// (rules/potential_plan.go paymentPlanRelaxProof): relaxed, never
	// executed alternatives for the mana abilities the planner census does
	// not price, appended to every search while it is set. Pure per-query
	// scratch: Clone copies none of it.
	PaymentPlanRelaxed [][]Alt `clone:"reset"`

	// PaymentPlanRelaxedFee is the generic the relaxed proof charges on top
	// of every planned cost for the paid relaxed abilities it admits.
	PaymentPlanRelaxedFee int32 `clone:"reset"`

	// PaymentPlanPotentialPool marks a PotentialPaymentPlans query
	// (paymentPlanPoolAccepted). Pure per-query scratch: Clone copies none.
	PaymentPlanPotentialPool bool `clone:"reset"`

	// WalkRecDemand: the payment offer builder has run on this engine, so
	// its potential walk follows priority walks and they record
	// (walk_block_reuse.go). Clone copies none.
	WalkRecDemand bool `clone:"reset"`

	// ManaAbScratch is the priority mana member-set scratch list
	// (activateManaFor, priorityManaAbilityCount; taken for the call,
	// Clone leaves it nil).
	ManaAbScratch []*cards.SA `clone:"reset"`

	// CostProvenanceSeen is the transient capture of the last cost-modifier
	// pass (castprov3): true when that pass evaluated a cost static whose
	// ValidCard$ carries a cast-provenance token (Bilbo's
	// "!wasCastFromYourHand" ReduceCost) — such a static is unresolvable
	// pre-push, so the pass denied it and the pending cast's payment needs
	// the post-push re-price continueCast runs right after CR 601.2a's push.
	// Set inside costStaticApplies (inside the costModifiers attribution
	// roots, so the param census sees no new read), cleared at the top of
	// every costModifiersWithTargets[ X]Using pass. Like noCounterSpend it
	// is synchronous computation state: every read of it (the option-
	// selection sites and continueCast's post-push re-price) happens in the
	// same driven flow as the pass that set it, and no ask suspends between
	// the pass and the read. Like noCounterSpend, Clone copies nothing of
	// it.
	CostProvenanceSeen bool `clone:"reset"`

	// PaymentPlanCarriers memoises the objects whose faces carry a
	// Taps/TapsForMana trigger or a ProduceMana replacement -- the only
	// printed text the payment-plan source-interference check must run its
	// matchers over (rules/payment_plan_interference.go). The key is the
	// object-arena size plus the log head: a face or zone only changes through
	// an event or a new object. A pure derived memo, never copied by Clone.
	PaymentPlanCarriers       []state.ObjID `clone:"reset"`
	PaymentPlanCarriersObjs   int           `clone:"reset"`
	PaymentPlanCarriersEvents int           `clone:"reset"`
	PaymentPlanCarriersValid  bool          `clone:"reset"`

	// NoCounterSpend is the transient capture of emitRestrictedManaSpend: the
	// id of the SPELL whose payment just consumed a batch carrying
	// AddsNoCounter$ provenance (Cavern of Souls' "that spell can't be
	// countered"), zero when none. payManaCast's caller (payCast) reads it
	// once, synchronously, right after the payment — no ask can suspend
	// between the spend and the read (emitRestrictedManaSpend emits, never
	// asks) — and folds state.FlagNoCounter into the pay-time CastInfo, so
	// replay re-derives the flag from the recorded event exactly like every
	// other cast flag. Zero whenever no such spend is in flight, so Clone
	// copies nothing of it.
	NoCounterSpend state.ObjID `clone:"reset"`

	// ManaSpentSources is the transient capture of emitRestrictedManaSpend's
	// SPELL arm: the deduplicated Source of every restriction batch consumed
	// by the payment, in insertion order. payCast reads it once, synchronously,
	// right after the payment and queues each source's TriggersWhenSpent$
	// rider (Path of Ancestry's "when that mana is spent to cast ..."). Empty
	// Valid provenance batches -- the Boseiju shape effMana emits for a rider'd
	// mana ability -- are what make the attribution exact: emitRestrictedManaSpend
	// consumes batches before ordinary mana. Nothing can suspend between the
	// capture and the read (it emits, never asks), and Clone copies nothing of
	// it (like noCounterSpend), so a replay re-derives the same list from the
	// recorded ManaAdd events.
	ManaSpentSources []state.ObjID `clone:"reset"`

	// ManaSpentAddsCounters is the transient capture of emitRestrictedManaSpend's
	// SPELL/ACTIVATED arm for the AddsCounters$ rider: every consumed
	// restriction batch that carries a rider (state.ManaRestriction.AddsCounters,
	// the producing ability's snapshot) contributes its spent unit count as one
	// grant record, in insertion order. Unlike manaSpentSources this is NOT
	// deduplicated by source: two units from the same permanent's rider ability
	// are two grants, and two different abilities of the same permanent keep
	// their own rider snapshots. payCast reads it once, synchronously, right
	// after the payment. Nothing can suspend between the capture and the read
	// (it emits, never asks), and Clone copies nothing of it, so a replay
	// re-derives the same grants from the recorded ManaAdd/ManaRestriction
	// events.
	ManaSpentAddsCounters []state.ManaAddsCounterGrant `clone:"reset"`
}

// UnlessPayment is the continuation between accepting an UnlessCost$ and
// completing its non-mana Sac/Discard/Reveal components. Unlike ordinary
// activation costs, an unless cost is paid during a suspended resolution, so
// its choices must retain that resolution rather than silently taking the
// first card.
type UnlessPayment struct {
	Payer    state.PlayerID
	Cost     costvocab.Cost
	Ctx      effects.Ctx
	StackObj state.ObjID
	// Tape marks a payment the resolution kernel drives in line
	// (tapeUnlessComponents): its asks are served from the tape and its
	// settlement lands in the asking walk's live Ctx. A payment that is not
	// tape-driven belongs to an activated mana ability, whose continuation
	// remains in manaUnlessActivation.
	Tape     bool
	Part     int
	Sacs     []state.ObjID
	Discards []state.ObjID
	// Reveals holds the Reveal<N/Spec> picks (the hideaway-family ETB
	// lands, Xyru Specter's Challenge): unlike a discard the revealed cards
	// STAY in hand, so the dedup must be explicit — one card must not pay
	// two parts — and the settled picks are announced with one public Note
	// (the same event the cast flow's pay.EmitChoiceCosts emits).
	Reveals []state.ObjID
	// Beholds holds the Behold<N/Spec> picks (CR 702.176, Elven Passage's
	// "you may behold an Elf"): an object the payer controls on the
	// battlefield or a card revealed from their hand. The chosen object is
	// not moved (a plain Behold, unlike BeholdExile), so like a reveal it
	// needs the explicit dedup and one public Note.
	Beholds []state.ObjID
	// Returns holds the Return<N/Spec> picks (the cumulative-upkeep family's
	// "return a non-Lair land"): a permanent matching the spec returned to
	// its OWNER's hand (Forge CostReturn.moveToHand), one choice-bearing part
	// exactly like a sacrifice.
	Returns []state.ObjID
	// Exiles holds the Exile<N/Spec> picks (the Grip of Amnesia family's
	// "exile all cards from their graveyard"): cards exiled from the payer's
	// own hand or graveyard as the cost. A whole-zone part
	// (isWholeZoneExileSpec) takes EVERY candidate without asking; an
	// ordinary part is a choice exactly like a sacrifice.
	Exiles []state.ObjID
}

// PaymentPartCount is the flat count of the choice-bearing components
// (Sac, Discard, Reveal, Behold, Return, Exile) the continuation walks before
// it settles the synchronous ones.
func (u *UnlessPayment) PaymentPartCount() int {
	return len(u.Cost.Sac) + len(u.Cost.Discard) + len(u.Cost.Reveal) + len(u.Cost.Behold) + len(u.Cost.Return) + len(u.Cost.Exile)
}
