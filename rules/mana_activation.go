package rules

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// chooseMana is the one-off pick among a source's distinct mana abilities.
// The existing chooseFor values occupy 0 through 5 (cast/target/miracle/etc.),
// 6 through 9 are this file's own mana windows, 110-14 the mainline
// opening/suspend/station flows, and the unless window sits above chooseUnlock.
const (
	chooseMana chooseFor = iota + 6
	chooseManaColor
	chooseManaDiscard
	chooseManaExile
)

// chooseManaSacrifice is the mana ability's sacrifice-cost pick. 31 is the
// next free value: 30 is chooseAttached (rules/cast.go) and 40 is
// chooseUntap; the numbers matter only inside this package's switch table.
const chooseManaSacrifice chooseFor = 31

// chooseManaTap is the mana ability's tapXType<N/Spec> tap pick, the tap
// analogue of chooseManaSacrifice. 48 is the next free value after the
// highest taken literal (47 is chooseOppPick, rules/stack.go).
const chooseManaTap chooseFor = 48

// chooseManaSubCounter is the mana ability's announced-SubCounter<X/...>
// election: the X announcement and, for a part anchored to another
// permanent, the removal target. 49 is the next free literal after
// chooseManaTap. chooseManaForage is the Forage cost's two-arm election and
// chooseManaUntap the untapYType<N/Spec> permanent pick (50/51).
const (
	chooseManaSubCounter chooseFor = 49
	chooseManaForage     chooseFor = 50
	chooseManaUntap      chooseFor = 51
)

// manaCostChoicePending reports whether the engine is parked on one of the
// mana ability's own cost sub-elections -- a discard, exile, sacrifice or
// tap pick -- or its Produced$ colour choice. Every caller that must not
// resolve an outer flow past an outstanding mana cost election reads this
// one helper, so a newly added sub-election (chooseManaTap is the most
// recent) cannot be forgotten at one guard and livelock a bot game.
func (e *Engine) manaCostChoicePending() bool {
	switch e.choosing {
	case chooseManaColor, chooseManaDiscard, chooseManaExile, chooseManaSacrifice, chooseManaTap,
		chooseManaSubCounter, chooseManaForage, chooseManaUntap:
		return true
	}
	return false
}

const (
	chooseManaUnless chooseFor = iota + 17
	chooseUnlessCost
	chooseUnlessMana
)

// manaActivation is the one outstanding choice among a permanent's distinct
// mana abilities. It is plain data so Clone can preserve a choice at an
// intent boundary. cast says the answer resumes CR 601.2g payment rather
// than the ordinary priority round.
type manaActivation struct {
	player    state.PlayerID
	source    state.ObjID
	abilities []*cards.SA
	// gained parallels abilities: the has-all-abilities-of identity of each
	// entry (zero for a printed or SVar-granted ability), captured when the
	// choice is posed so the answer resolves the same foreign ability even
	// after a Produced$ rewrite breaks the SA's pointer identity.
	gained     []gainedManaRef
	cast       bool
	cumulative bool
}

// gainedManaRef is the has-all-abilities-of identity of one mana ability
// activation (Forge's GainsAbilitiesOf$, rules/legal.go's grantedAbilities):
// the FOREIGN card the ability belongs to, its index in that card's face
// Abilities, and the face itself. A zero value (face == nil) is an ordinary
// printed or SVar-granted mana ability. It is threaded through the mana
// activation and every one of its choice continuations (colour, unless-pay,
// discard/exile cost) because the SA pointer does not survive them -- a
// Produced$ rewrite or an unless-gate strip resolves a COPY -- and the
// identity is what (a) resolves the body's SVars (Amount$ X) against the
// foreign face, not the recipient's, and (b) records the replayable
// ManaActivate marker (IDs[0] = foreign card, Amount = index) that the
// GainsAbilitiesLimitPerTurn$ cap counts (gainedActivationsThisTurn).
type gainedManaRef struct {
	from state.ObjID
	idx  int
	face *cards.Face
}

// svars returns the SVar table a gained mana ability resolves against, or
// fallback for an ordinary ability.
func (r gainedManaRef) svars(fallback map[string]string) map[string]string {
	if r.face != nil {
		return r.face.SVars
	}
	return fallback
}

// manaColorActivation holds an already-paid mana ability while its controller
// chooses the colour that Produced$ Any will add, or allocates every unit of a
// Produced$ Combo. triggers is the CR 605.3b triggered-mana batch still to
// resolve after the answer. When trigger is non-nil the choice belongs to that
// triggered mana ability instead (ability is then the Mana sub-ability in its
// chain, and player the player receiving the mana).
type manaColorActivation struct {
	player     state.PlayerID
	source     state.ObjID
	ability    *cards.SA
	cast       bool
	cumulative bool
	triggers   []pendingTrigger
	trigger    *pendingTrigger
	gained     gainedManaRef
	sacs       []state.ObjID
	allocation bool
	// nested is set when a colour choice was posed by effects.Ask from a
	// SubAbility$ Mana effect inside an off-stack mana resolution.
	nested *cards.SA
	// nestedResume carries any other ask from that off-stack chain. Its resume
	// object is the mana source (direct), never the unrelated stack top.
	nestedResume *resumePoint
}

// offStackManaFrame is the transient (never stored across a Submit, so never
// cloned) record of one off-stack mana resolution in progress: a mana
// ability's effect chain, or a CR 605.3b triggered mana ability, resolving
// synchronously without a stack object. Two things read it:
//
//   - Engine.Ask routes any ask posed inside it into the rules-owned
//     mana-activation continuation (act is the continuation template), because
//     a stack-oriented resume point would re-enter whatever object happens to
//     be on top of the stack (the resolving cumulative-upkeep trigger, or an
//     unrelated spell) and orphan the mana activation's own continuation --
//     the payment window then re-asked over it (the ask-overwrote panic).
//   - Engine.Suspended answers relative to the state the frame began in: a
//     payment window (cumulative, echo/triggered cost, unless payment) or a
//     suspended resolution that was ALREADY open when the mana ability was
//     activated inside it is not this chain's suspension, so its
//     SubAbility$ walk must continue (a mana ability's rider was silently
//     dropped inside every such window).
type offStackManaFrame struct {
	act             manaColorActivation
	asked           bool
	baseResume      *resumePoint
	baseUnless      bool
	baseCumulative  bool
	baseTriggerCost bool
}

// withOffStackMana runs one synchronous off-stack mana resolution under an
// offStackManaFrame and reports whether a routed ask suspended it.
func (e *Engine) withOffStackMana(act manaColorActivation, run func()) bool {
	saved := e.offStackMana
	// The frame lives in the engine's own slots while the nesting fits them
	// (it is never read after this call returns: askOffStackMana copies what
	// it keeps), else on the heap.
	var f *offStackManaFrame
	if d := e.offStackDepth; d < len(e.offStackSlots) {
		f = &e.offStackSlots[d]
	} else {
		f = new(offStackManaFrame)
	}
	e.offStackDepth++
	*f = offStackManaFrame{act: act, baseResume: e.resume, baseUnless: e.unlessPayment != nil,
		baseCumulative: e.cumulative != nil, baseTriggerCost: e.triggerCost != nil}
	e.offStackMana = f
	// A mana ability's own chain is not a contChain-draining pass: an ask it
	// posts must never be deferred onto the enclosing resolution's chain.
	savedOwners := e.contChainOwners
	e.contChainOwners = 0
	// CR 605: a mana ability resolves immediately and is never a stack
	// object, so inFlightCounterAdder's actionCause fallback cannot name it
	// -- it would read whatever spell (if any) happens to sit on the stack.
	// Publish the activating player as the counter adder for the whole
	// off-stack resolution so a SubAbility$ PutCounter (or a triggered mana
	// ability's own counter) is attributed to that ability's controller the
	// same whether it resolves at priority, inside a CR 601.2g payment
	// window, or in response to an unrelated spell.
	prevAdder := e.SetCounterAdder(act.player)
	run()
	e.SetCounterAdder(prevAdder)
	e.contChainOwners = savedOwners
	e.offStackMana = saved
	asked := f.asked
	*f = offStackManaFrame{}
	e.offStackDepth--
	return asked
}

// askOffStackMana routes every resumable ask from an off-stack mana chain
// through its own continuation. The synthetic resume point is anchored to the
// mana source as a direct resolution, not the current stack top.
func (e *Engine) askOffStackMana(d *decision.Decision) bool {
	f := e.offStackMana
	if f == nil || d.ResumeKind != "mana_color" || d.ResumeSA == nil {
		return false
	}
	act := f.act
	act.nested = d.ResumeSA
	act.nestedResume = nil
	act.allocation = d.Max > 1
	act.triggers = append([]pendingTrigger(nil), f.act.triggers...)
	e.manaColorActivation = &act
	f.asked = true
	if e.tape.InRun() {
		// Inside a tape run the colour choice is answered in place: the
		// activation's own continuation (answerManaColor) is what windowAsk
		// runs, and its holder (this frame) is open by design.
		e.tapeOffStackAsking = true
		windowAsk(e, d, chooseManaColor)
		e.tapeOffStackAsking = false
		return true
	}
	e.choosing = chooseManaColor
	e.ask(d)
	return true
}

// takeOffStackManaRider detaches the mana activation and its parked resume
// point. The decision-specific resolution handler installs that point itself:
// e.resume is owned by the resolution machinery, not this activation helper.
func (e *Engine) takeOffStackManaRider() (*manaColorActivation, *resumePoint) {
	ma := e.manaColorActivation
	if ma == nil || ma.nestedResume == nil {
		return nil, nil
	}
	e.manaColorActivation = nil
	e.choosing = chooseNone
	ma.nestedResume = cloneResume(ma.nestedResume)
	return ma, ma.nestedResume
}

// finishOffStackManaRider drains the remainder of a mana activation after its
// ordinary decision handler has re-entered the parked rider.
func (e *Engine) finishOffStackManaRider(ma *manaColorActivation, asked bool) {
	if !asked && e.pending == nil && e.resume == nil {
		e.resolveTriggeredManaAbilities(ma.triggers, ma.cast, ma.cumulative)
		e.continueManaPaymentWindow(ma.cumulative)
	}
}

// offStackSuspended is Suspended() inside an offStackManaFrame.
func (f *offStackManaFrame) suspended(e *Engine) bool {
	return f.asked || (e.resume != nil && e.resume != f.baseResume) ||
		(e.unlessPayment != nil && !f.baseUnless) ||
		(e.cumulative != nil && !f.baseCumulative) ||
		(e.triggerCost != nil && !f.baseTriggerCost)
}

// continueManaPaymentWindow re-opens a resolution-time payment window after a
// mana activation made from it has fully resolved. Nothing may be pending: a
// nested decision (a colour choice, a CR 616.1 replacement order) owns the
// continuation until it is answered.
func (e *Engine) continueManaPaymentWindow(cumulative bool) {
	if cumulative && e.choosing == chooseNone && e.pending == nil {
		e.paymentWindowAsk()
	}
}

// finishManaEffect resolves a paid mana ability's effect with produced as its
// Produced$, then its CR 605.3b triggered mana batch, then the payment
// window it was activated from -- unless the effect chain suspended on a
// routed colour ask, whose answer runs that same continuation.
func (e *Engine) finishManaEffect(p state.PlayerID, source state.ObjID, ma *cards.SA, produced string,
	gained gainedManaRef, sacs []state.ObjID, cast, cumulative bool, triggers []pendingTrigger) {
	act := manaColorActivation{player: p, source: source, ability: ma, cast: cast, cumulative: cumulative,
		triggers: triggers, gained: gained, sacs: append([]state.ObjID(nil), sacs...)}
	if e.withOffStackMana(act, func() { e.resolveManaEffectColor(p, source, ma, produced, gained, sacs) }) {
		return
	}
	e.resolveTriggeredManaAbilities(triggers, cast, cumulative)
	e.continueManaPaymentWindow(cumulative)
}

// answerNestedManaColor completes a routed SubAbility$ colour ask: the chain
// re-enters at the asking Mana effect with the answer, then continues exactly
// as finishManaEffect / resolveTriggeredManaAbilities would have.
func (e *Engine) answerNestedManaColor(ma *manaColorActivation, chosen []decision.Option) {
	var ctx effects.Ctx
	if ma.trigger != nil {
		ctx = ma.trigger.Ctx
	} else {
		ctx = effects.NewCtx(ma.source, ma.player, effects.CtxInit{})
		ctx.ResolvedThisTurn = e.resolvedAbilityTallyFor(ma.source, ma.ability)
		ctx.ActivationsThisTurn = e.activationsThisTurnFor(ma.source, ma.ability)
		if o := e.G.Obj(ma.source); o != nil && o.Face() != nil {
			effects.SetSVars(&ctx, ma.gained.svars(o.Face().SVars))
		}
	}
	for _, option := range chosen {
		colour := option.ManaSymbol
		if len(colour) != 1 || !strings.Contains("WUBRG", colour) {
			continue
		}
		if len(chosen) == 1 {
			ctx.ManaChoice = colour
		} else {
			ctx.ManaChoices = append(ctx.ManaChoices, colour)
		}
	}
	savedTap, savedProducer := e.manaFromTap, e.manaProducer
	if ma.trigger == nil && ma.ability != nil {
		e.manaFromTap = e.costRef(ma.ability.ParamStr(cards.PKCost)).Tap
		e.manaProducer = ma.source
	}
	template := *ma
	template.nested = nil
	asked := e.withOffStackMana(template, func() { effects.Resolve(e, &ctx, ma.nested) })
	e.manaFromTap, e.manaProducer = savedTap, savedProducer
	if asked {
		return
	}
	e.resolveTriggeredManaAbilities(ma.triggers, ma.cast, ma.cumulative)
	e.continueManaPaymentWindow(ma.cumulative)
}

// manaDiscardActivation holds a synchronous mana ability while its discard
// cost is chosen. It is separate from pendingCast so activating mana during a
// spell's CR 601.2g payment window never overwrites the outer cast flow.
type manaDiscardActivation struct {
	player     state.PlayerID
	source     state.ObjID
	ability    *cards.SA
	cost       Cost
	sacs       []state.ObjID
	sacPaid    int
	discards   []state.ObjID
	exiles     []state.ObjID
	taps       []state.ObjID
	sacPart    int
	part       int
	exilePart  int
	tapPart    int
	cast       bool
	cumulative bool
	gained     gainedManaRef
	// The announced SubCounter cost parts. subX is the announced X, set
	// once by manaSubCounterAsk's first decision (the cast path's pc.x) and
	// shared by every announced part; subCounterPays records the removal
	// picks exactly like the cast path's pc.subCounterPays (one entry per
	// counter unit for an "Any" part, one entry for a fixed-kind part).
	subX           int32
	subXAnnounced  bool
	subCounterPays []subCounterPay
	subPart        int
	// forageDone marks the Forage election already posed; forageFood is the
	// object sacrificed for it (zero when the exile-three arm paid, or for a
	// non-interactive caller that took the deterministic arm).
	forageDone bool
	forageFood state.ObjID
	foragePay  bool
	// untapPart walks the untapYType<N/Spec> parts and untaps the elected
	// permanents (the source's own {Q} untap is cost.Untap and stays
	// separate).
	untapPart int
	untaps    []state.ObjID
	// interactive records whether the caller could pose asks. A caller that
	// cannot (the attack-cost tap window, direct-resolve tests) settles the
	// announced SubCounter, Forage and untapYType parts with deterministic
	// R-9 picks instead of posing an election.
	interactive bool
}

// manaUnlessActivation parks an off-stack mana ability while its payer
// answers its UnlessCost$. The ordinary effects resume path needs a stack
// object, so this flow records the same decision and uses the same payment
// helper without pretending a mana ability is on the stack.
type manaUnlessActivation struct {
	player     state.PlayerID
	source     state.ObjID
	ability    *cards.SA
	cast       bool
	cumulative bool
	triggers   []pendingTrigger
	sacs       []state.ObjID
	payers     []state.PlayerID
	next       int
	gained     gainedManaRef
}

// availableManaAbilities returns exactly the individual mana abilities that
// p may activate from id now. Keeping the CantBeActivated gate here makes the
// priority action, payment window, and the eventual chosen activation share
// one member-by-member eligibility set.
// manaReflectedPresentHolds evaluates an activated ManaReflected ability's
// IsPresent$/PresentCompare$ activation gate. Most shapes use the shared
// deterministic battlefield count. hasAbility Activated.otherAbility is a
// property of the subject's face, so it is handled structurally here: a
// native mana ability excludes itself, while a static-granted SVar (Tazri)
// requires one printed activated ability on that creature.
func (e *Engine) manaReflectedPresentHolds(p state.PlayerID, source state.ObjID, ma *cards.SA) bool {
	mr := effects.ManaReflectedOf(ma)
	if !e.classBandGateHolds(mr.ClassBand, source) {
		return false
	}
	spec := mr.IsPresent
	if spec == "" {
		return true
	}
	if strings.Contains(spec, "hasAbility Activated.otherAbility") {
		o := e.G.Obj(source)
		if o == nil || o.Face() == nil || !e.matchesSpecFrom(
			strings.TrimSpace(strings.Split(spec, "+hasAbility Activated.otherAbility")[0]), source, p, source) {
			return false
		}
		n := 0
		for _, ab := range o.Face().Abilities {
			if ab.Kind == "AB" && ab != ma {
				n++
			}
		}
		if cmp := mr.PresentCompare; cmp != "" {
			return comparePresent(n, cmp)
		}
		return n > 0
	}
	n := e.countPresent(spec, source, p)
	if cmp := mr.PresentCompare; cmp != "" {
		return comparePresent(n, e.presentCompareFor(cmp, source, p))
	}
	return n > 0
}

func (e *Engine) availableManaAbilities(p state.PlayerID, id state.ObjID) []*cards.SA {
	return e.availableManaAbilitiesUsing(nil, p, id)
}

// A non-nil source belongs only to the caller's current legalActions pass.
// The ordinary wrapper deliberately supplies nil so payment windows and
// activation rechecks discover fresh static membership.
func (e *Engine) availableManaAbilitiesUsing(statics *actionStaticSource, p state.PlayerID, id state.ObjID) []*cards.SA {
	return e.appendAvailableManaAbilities(nil, statics, p, id)
}

// appendAvailableManaAbilities is availableManaAbilitiesUsing appending into
// out (which must not alias anything the walk reads), so a caller that only
// inspects the list can reuse one buffer across objects. With out nil it
// returns exactly what availableManaAbilitiesUsing always returned.
func (e *Engine) appendAvailableManaAbilities(out []*cards.SA, statics *actionStaticSource, p state.PlayerID, id state.ObjID) []*cards.SA {
	return e.appendAvailableManaAbilitiesGate(out, statics, p, id, false)
}

// appendAvailableManaAbilitiesGate is appendAvailableManaAbilities with the
// live-pool payability gate made optional. ignorePayable is true ONLY for the
// cast-window probe's own walk (castWindowProbeUnits): a CR 601.2g window can
// fund a paid activation from mana it produced earlier in the SAME window, so
// the probe must see a source whose fee the CURRENT pool cannot yet cover and
// let its ordered reachability search prove the funding. Every other caller
// (the priority offer, the payment windows, the potential-action walk)
// keeps the live gate. The non-mana gates (zone, loyalty, activation
// condition, restriction, activation limit) are unchanged, so the probe's
// membership is still a subset of what the window can eventually offer.
func (e *Engine) appendAvailableManaAbilitiesGate(out []*cards.SA, statics *actionStaticSource, p state.PlayerID, id state.ObjID, ignorePayable bool) []*cards.SA {
	o := e.G.Obj(id)
	// CR 702.25b: a phased-out permanent is treated as though it does not
	// exist, so its mana abilities do not exist. PhasedOut is only ever set on
	// a battlefield permanent (events.Apply's PhaseOut fold is
	// battlefield-gated, the Move fold clears it), so testing the flag directly
	// avoids existsOnBattlefield's Zone == ZBattlefield requirement -- this
	// walk is shared by the battlefield, hand and graveyard offers, and a mana
	// ability may explicitly function from hand or graveyard (CR 605.2a,
	// Spirit Guides and Jack-o'-Lantern).
	if o == nil || o.PhasedOut || o.Face() == nil {
		return out
	}
	f := o.Face()
	// CR 708.8: a face-down permanent's printed mana abilities do not exist
	// while it is face down. Its ONE exception is CR 305.6: a permanent
	// whose set type includes a basic land subtype has that land's intrinsic
	// mana ability, so Yedora's face-down Forest land taps for {G}. The set
	// type comes from a ChangeZone FaceDownSetType$ (Object.FaceDownTypeWords);
	// a plain CR 708.5 face-down 2/2 has no basic-land set type and
	// contributes nothing.
	faceDown := e.faceDownPrintedHides(o)
	// CR 613.1f: a permanent that lost all abilities has no printed mana
	// ability; one another effect grants it survives only when the grant is
	// not older than the removal (abilityloss.go).
	var lossStamp uint32
	lost := false
	if !faceDown {
		lossStamp, lost = e.abilityLoss(o)
	}
	var manaAbilities []*cards.SA
	switch {
	case lost:
	case faceDown:
		for _, w := range o.FaceDownTypeWords() {
			if ab, ok := cards.IntrinsicManaAbility(w); ok {
				manaAbilities = append(manaAbilities, ab)
			}
		}
	case len(o.MergedCards) == 0:
		// The overwhelmingly common case: a permanent that is not a mutated
		// pile has exactly one face, so its own ManaAbilities slice IS the
		// walk -- no second collection, which is what keeps this walk at its
		// pre-mutate allocation cost (internal/searchprobe's Capture budget
		// runs legalActions over every object on every pass).
		manaAbilities = f.ManaAbilities()
	default:
		// CR 702.140d: a mutated pile's under-card mana abilities are live
		// too. Only the pile pays for the flattening.
		for i := 0; i < o.PileFaceCount(); i++ {
			pf, ok := o.PileFaceAt(i)
			if !ok {
				continue
			}
			manaAbilities = append(manaAbilities, pf.Face.ManaAbilities()...)
		}
	}
	// CR 305.6: basic land types granted in layer 4 carry their intrinsic
	// mana abilities too. Printed faces already contain their own intrinsics;
	// append only productions they do not already provide.
	// Whether any layer-4 effect is active is one board-wide fact per
	// active() build (activeSummaryOf), not a per-object list scan; the
	// ability is built only for a colour no listed ability already produces
	// (IntrinsicManaColor), so a basic land's own printed intrinsic costs no
	// allocation. A granted intrinsic (no ActivationZone$, no Activator$)
	// passes the loop's zone and Activator$ gates only on the battlefield and
	// only for its controller, so for any other object the block would add
	// nothing the loop keeps: it is skipped, with its layer read.
	if !faceDown && len(e.landTypeWords) > 0 && o.Zone == state.ZBattlefield && e.controllerOf(id) == p &&
		manaWalkHasLType(e, statics) {
		for _, typ := range e.derivedTypesOf(id) {
			color, ok := cards.IntrinsicManaColor(typ)
			if !ok || manaAbilitiesProduce(manaAbilities, color) {
				continue
			}
			if ma, ok := cards.IntrinsicManaAbility(typ); ok {
				manaAbilities = append(manaAbilities, ma)
			}
		}
	}
	// recipientCtx is minted on first use (a granted ManaReflected ability),
	// so an ordinary object's pass allocates nothing for it; both uses share
	// the one instance, exactly as before.
	var recipientCtx *effects.Ctx
	recipient := func() *effects.Ctx {
		if recipientCtx == nil {
			recipientCtx = effects.NewCtxPtr(id, p, effects.CtxInit{SVars: f.SVars})
		}
		return recipientCtx
	}
	abilityRestricted := func(ma *cards.SA) bool {
		if statics == nil {
			return e.abilityRestricted(p, id, ma)
		}
		return e.abilityRestrictedUsing(statics.get().cantActivate, p, id, ma)
	}
	for _, ma := range manaAbilities {
		// CR 605.1b: an activated ability is a mana ability only when it is
		// NOT a loyalty ability. A planeswalker's mana-producing loyalty
		// ability (Koth's [+1], Ugin, Eye of the Storms' [0]: Add {C}{C}{C},
		// 12+ corpus cards) must never enter this path: the mana path taps
		// nothing, poses no CR 606.3 gate, and a zero-loyalty cost is free --
		// the measured ulalek-eldrazi seed-1019 livelock re-tapped Ugin's
		// [0] once per intent, +3 colourless per activation, forever. The
		// ability offer (rules/legal.go) owns these abilities with the full
		// CR 606.3 gates (sorcery timing, once per permanent per turn).
		// (The zone gate runs first: it is the cheapest of these pure reads
		// and the one a hand/graveyard card's printed ability fails.)
		//
		// mf carries the configured ability's own-text verdicts
		// (mana_safacts.go): a gate its text leaves empty is answered from
		// it, every other gate runs its ordinary evaluator. A runtime-built
		// ability (mf nil) takes every gate the ordinary way.
		mf := e.manaFactsOf(ma)
		if mf != nil {
			if !mf.zoneOKFact(ma, o.Zone) || mf.loyalty {
				continue
			}
		} else if !abilityZoneOK(ma, o.Zone) || e.isLoyaltyAbility(ma) {
			continue
		}
		// Activation$ (Mox Opal's "Activate only if you control three or more
		// artifacts"): the same keyword-condition gate the printed-ability
		// offer loop in rules/legal.go applies, so the priority action, the
		// payment window and the chosen activation share one member set.
		// Activator$ (Mana Cache's "Any player may activate this ability but
		// only during their turn before the end step") is the same shared
		// selector every non-mana offer path applies: a mana ability is a
		// mana ability's own eligibility home, so without this read the source
		// controller could activate an ability whose Activator$ excluded them
		// and a permitted opponent could not.
		var cc *compiledCost
		if mf != nil {
			if mf.defaultActivator {
				// activatorAllows' blank-Activator$ arm.
				if e.controllerOf(id) != p {
					continue
				}
			} else if !e.activatorAllows(p, id, ma) {
				continue
			}
			if !mf.noActivation && !e.activationConditionOK(p, ma) {
				continue
			}
			if mf.noIsPresent {
				// manaActivationGateHolds without an IsPresent$ gate is its
				// activationPhasesOK read alone.
				if !mf.noPhaseGate && !e.activationPhasesOK(p, ma) {
					continue
				}
			} else if !e.manaActivationGateHolds(p, id, ma) {
				continue
			}
			cc = mf.cost
		} else {
			if !e.activatorAllows(p, id, ma) ||
				!e.activationConditionOK(p, ma) || !e.manaActivationGateHolds(p, id, ma) {
				continue
			}
			cc = e.compiledCostOf(ma.ParamStr(cards.PKCost))
		}
		// The cost is looked up once for the CR 302.6 tap-sick gate and the
		// payability gate (manaAbilityPayable's own tap-sick re-check is the
		// same pure read, so it is not repeated).
		if !e.tapFlagsSick(id, cc.Tap, cc.Untap) && !abilityRestricted(ma) && (ignorePayable || e.manaCostPayable(p, o, id, cc, nil)) &&
			// CheckSVar$/SVarCompare$ (Glistening Sphere's Corrupted "Activate
			// only if an opponent has three or more poison counters"): the same
			// intervening-if gate sVarGateOK applies to every non-mana
			// activation, so the priority offer, the payment windows and the V1
			// planner withhold the ability with its condition false.
			((mf != nil && mf.noCheckSVar) || e.manaSVarGateOK(o, p, id, ma)) {
			// ActivationLimit$ / GameActivationLimit$ (Vivi Ornitier's "only once
			// each turn", Stalking Leonin's "Activate only once"): the non-mana
			// ability offer loops in legal.go gate on these parameters, but this
			// walk is a mana ability's ONLY eligibility gate -- offer, payment
			// window and chosen activation all go through it -- so a mana ability
			// carrying either limit stayed repeatable without bound, and a
			// controller whose bot policy prefers activating mana over passing
			// looped on it forever (a zero-production source whose use never
			// advances any cast). Both limits are checked through the one shared
			// gate, with the printed identity (flat pile index, no SVar).
			if mf != nil && mf.noLimit {
				// Neither limit parameter: nothing to check.
			} else if _, limited := ma.Param(cards.PKActivationLimit); limited || ma.ParamStr(cards.PKGameActivationLimit) != "" {
				idx, merged, found := pileAbilityRefOf(o, ma)
				if found && e.activationLimitBlocked(p, id, ma, idx, "", merged) {
					continue
				}
			}
			out = append(out, ma)
		}
	}
	if faceDown {
		// Only the CR 305.6 intrinsics above exist on a face-down permanent:
		// its printed ManaReflected abilities and any Continuous AddAbility$
		// grant are hidden with the rest of its printed face (CR 708.8).
		return out
	}
	// CR 605.2a: a mana ability functions only while its source object is in
	// the zone its ActivationZone$ names -- the battlefield when printed
	// none is. The plain AB$ Mana branch above gates on abilityZoneOK; this
	// closure must agree, or a reflected land in hand/graveyard is offered
	// (and activatable) wherever an opponent's land exists -- Exotic Orchard
	// reporting an "Activate ... for mana" action for the card IN HAND.
	// considerReflected reports whether a ManaReflected ability is live; the
	// caller appends it (a closure appending to out itself would move out's
	// header to the heap on every call).
	// ctx is called only once every other gate has passed, so an ability
	// that fails one (an opponent's Exotic Orchard, a tapped source) never
	// mints its Ctx.
	considerReflected := func(ma *cards.SA, ctx func() *effects.Ctx) bool {
		if ma.Kind != "AB" || ma.API != "ManaReflected" || !abilityZoneOK(ma, o.Zone) || !e.activatorAllows(p, id, ma) || e.manaAbilityTapSick(id, ma) || abilityRestricted(ma) || !e.manaAbilityPayable(p, id, ma) || !e.manaReflectedPresentHolds(p, id, ma) {
			return false
		}
		// Face contexts are shared across abilities; never let one cost's
		// elected candidates leak into a sibling ManaReflected ability.
		c := *ctx()
		cost := e.parseCost(ma.ParamStr(cards.PKCost))
		if len(cost.UntapPermanent) > 0 {
			claimed := map[state.ObjID]bool{}
			if cost.Tap {
				claimed[id] = true
			}
			for _, part := range cost.UntapPermanent {
				candidates := e.manaUntapCandidates(p, id, part.Spec, claimed)
				if int32(len(candidates)) < part.N {
					return false
				}
				for i := int32(0); i < part.N; i++ {
					c.CostUntapped = append(c.CostUntapped, candidates[i])
					claimed[candidates[i]] = true
				}
			}
		}
		return len(effects.ManaReflectedCandidates(e, &c, ma)) > 0
	}
	// A ManaReflected ability may sit on the top face or any under-card; each
	// resolves its own face's table. A single face the walk facts show
	// carries none (walk_face_facts.go) has nothing for the scan to find.
	pileFaces := o.PileFaceCount()
	if lost {
		pileFaces = 0
	}
	if pileFaces == 1 && !walkSkipVerify {
		if ff := e.walkFaceFactsOf(f); ff != nil && !ff.manaReflected {
			pileFaces = 0
		}
	}
	for i := 0; i < pileFaces; i++ {
		pf, ok := o.PileFaceAt(i)
		if !ok {
			continue
		}
		// The per-face Ctx is minted only when one of the face's ManaReflected
		// abilities passes every other gate, and then shared by the face's
		// remaining ones, so an ordinary permanent's offer pass allocates
		// nothing here.
		var faceCtx *effects.Ctx
		var faceSVars map[string]string
		faceCtxFn := func() *effects.Ctx {
			if faceCtx == nil {
				faceCtx = effects.NewCtxPtr(id, p, effects.CtxInit{SVars: faceSVars})
			}
			return faceCtx
		}
		for _, ma := range pf.Face.Abilities {
			if ma.API != "ManaReflected" {
				continue
			}
			faceSVars = pf.Face.SVars
			if considerReflected(ma, faceCtxFn) {
				out = append(out, ma)
			}
		}
	}
	// A Continuous static may grant an activated ability through AddAbility$.
	// Resolve its named SVar from the static's source but activate it from id:
	// Tazri's ManaReflected reads the recipient creature's colours and its own
	// "another activated ability" condition, not Tazri's. This direct scan is
	// the mana path's membership AND ORDER source for printed Continuous
	// statics: it follows collectActionStatics' seat/zone/static walk (the
	// snapshot a legalActions pass shares), which
	// TestActionStaticMembershipPreservesOrderAndActiveFace pins. The
	// grantedAbilities loop below adds only grants this scan did not already
	// produce -- an Animate's Abilities$ member such as Wrenn and One's
	// "{T}: Add {G}" -- so a printed AddAbility$ is never offered twice (the
	// duplicate-offer trap: staticEffects now emits it into AddAbilities too).
	//
	// Both branches read ONE collector: a missing pass-scoped snapshot falls
	// back to collectActionStatics, not to the battlefield-only activeStatics,
	// so the offer walk and the handler/guard walk share a membership by
	// construction. They cannot disagree on a grantor (a phased-out one
	// included) that one walk sees and the other does not.
	var continuous []staticView
	if statics == nil {
		continuous = e.collectAddAbilityCarriers()
	} else {
		// The walk's snapshot pre-filtered to AddAbility$ carriers: every
		// other static fails the name test below, so the order and the
		// answer are those of the full list.
		continuous = statics.addAbilityContinuous()
	}
	var printed map[string]bool // allocated on the first printed grant
	for _, sv := range continuous {
		name := strings.TrimSpace(sv.ParamStr(cards.PKAddAbility))
		if name == "" || !e.matchesSpec(sv.ParamStr(cards.PKAffected), id, e.specCtx(sv.Source, sv.Controller)) {
			continue
		}
		source := e.G.Obj(sv.Source)
		if source == nil || source.Face() == nil {
			continue
		}
		if lost && lossStamp > source.Timestamp {
			continue
		}
		ma := cards.ResolveSVar(source.Face().SVars, name)
		if ma == nil || ma.Kind != "AB" {
			continue
		}
		if printed == nil {
			printed = make(map[string]bool)
		}
		printed[ma.Line] = true
		if ma.API == "ManaReflected" {
			if considerReflected(ma, recipient) {
				out = append(out, ma)
			}
			continue
		}
		if ma.API == "Mana" && !e.isLoyaltyAbility(ma) && abilityZoneOK(ma, o.Zone) && e.activatorAllows(p, id, ma) && !abilityRestricted(ma) && e.manaAbilityPayable(p, id, ma) &&
			e.manaActivationGateHolds(p, id, ma) && e.manaSVarGateOK(o, p, id, ma) {
			out = append(out, ma)
		}
	}
	// Granted mana abilities (CR 613.1f, rules/legal.go's grantedAbilities):
	// an AddAbilities grant's AB$ Mana/ManaReflected member -- a Saga
	// chapter's "gains '{T}: Add {C}'.", an Animate's Abilities$ member (Wrenn
	// and One) -- is a real mana ability with the same eligibility gates, so
	// the priority offer, the CR 601.2g payment window and the activation all
	// see exactly one member set. A member the printed scan above already
	// produced is skipped (it anchors the same activation); a granted
	// ManaReflected member goes through the same candidate/present gates as a
	// printed one. Activator$ applies to every one of these members exactly
	// as to a printed ability (legal.go's granted offer loop reads it too):
	// battlefieldManaSourceIDs walks every seat's permanents, so without the
	// gate an opponent's Mirran Safehouse -- which has the activated
	// abilities of the land cards in all graveyards -- was a mana source for
	// the non-controller's offer, payment window and planner (cardfuzz
	// explore seed 11656500164625753431: planfb source_changed).
	if statics != nil && statics.board.ready && !statics.board.hasGrants && !walkSkipVerify {
		// grantedAbilities is nil on a board without a grant
		// (activeSummary.hasGrants), read once per walk.
		return out
	}
	for _, ga := range e.grantedAbilities(p, id) {
		if printed[ga.sa.Line] {
			continue
		}
		if ga.sa.API == "ManaReflected" {
			if considerReflected(ga.sa, recipient) {
				out = append(out, ga.sa)
			}
			continue
		}
		if ga.sa.API != "Mana" || e.isLoyaltyAbility(ga.sa) {
			continue
		}
		if !abilityZoneOK(ga.sa, o.Zone) || !e.activatorAllows(p, id, ga.sa) || abilityRestricted(ga.sa) || !e.manaAbilityPayable(p, id, ga.sa) ||
			!e.manaActivationGateHolds(p, id, ga.sa) || !e.manaSVarGateOK(o, p, id, ga.sa) {
			continue
		}
		out = append(out, ga.sa)
	}
	return out
}

// manaSVarGateOK is the mana walks' CheckSVar$/SVarCompare$ activation gate:
// the same shared evaluator sVarGateOK (rules/legal.go) applies to every
// non-mana activation offer, so a gated ability (Glistening Sphere's
// Corrupted "Activate only if an opponent has three or more poison
// counters") reads one member set across the priority offer, the payment
// windows and the V1 payment planner. Only the merged face is walk-specific:
// pileAbilityRefOf resolves it for a printed SA (a mutated pile's under-card
// face reads its own SVar table); a granted or static-granted body is not a
// pile member and reads merged 0, the top face's table. sVarGateOK's
// fail-OPEN on an unevaluable body is the mana contract too -- an unreadable
// gate never silently removes a card's activation.
func (e *Engine) manaSVarGateOK(o *state.Object, p state.PlayerID, id state.ObjID, ma *cards.SA) bool {
	if _, ok := ma.Param(cards.PKCheckSVar); !ok {
		return true
	}
	merged := 0
	if _, m, found := pileAbilityRefOf(o, ma); found {
		merged = m
	}
	return e.sVarGateOK(p, id, ma, merged)
}

// manaActivationGateHolds evaluates a plain AB$ Mana ability's IsPresent$/
// PresentCompare$ existence gate (the same shape manaReflectedPresentHolds is
// for a reflected ability):
//
//   - IsPresent$ <spec> with PresentCompare$ <op><n>: the count of objects
//     matching <spec> (Shrine of the Forsaken Gods' "Activate only if you
//     control seven or more lands"). PresentCompare$ absent means GE1.
//
// An Activation$ <mechanic> rides rules/legal.go's shared
// activationConditionOK instead (main's vocabulary: Hellbent, Threshold,
// Metalcraft, Delirium), so the two gates compose rather than duplicate.
//
// A gate this build cannot price fails closed: the ability is withheld from
// the offer, the payment window and the activation alike, never widened.
func (e *Engine) manaActivationGateHolds(p state.PlayerID, id state.ObjID, ma *cards.SA) bool {
	// ActivationPhases$ and its rider qualifiers (PlayerTurn$,
	// OpponentTurn$, ActivationFirstCombat$, ActivationAfterBlockers$) are
	// the same offer-time window a non-mana ability is gated by. This walk
	// is a mana ability's ONLY eligibility gate, so reading the window here
	// is what keeps one AB$ Mana carrier (a charge-counter source whose
	// "any player may activate ... only during their turn before the end
	// step" line was previously offered outside its window) bound to it.
	if !e.activationPhasesOK(p, ma) {
		return false
	}
	if spec, ok := ma.Param(cards.PKIsPresent); ok && strings.TrimSpace(spec) != "" {
		n := e.countPresent(strings.TrimSpace(spec), id, p)
		if cmp := strings.TrimSpace(ma.ParamStr(cards.PKPresentCompare)); cmp != "" {
			if !comparePresent(n, e.presentCompareFor(cmp, id, p)) {
				return false
			}
		} else if n <= 0 {
			return false
		}
	}
	return true
}

// activateMana activates one of source's currently available mana abilities
// at PRIORITY -- the priority window's activate option (rules/legal.go),
// where the activating player holds priority. A singleton retains the old
// no-extra-decision path. Several abilities are distinct activated abilities
// sharing one tap cost, so their controller must choose one before the
// source is tapped.

func (e *Engine) activateMana(p state.PlayerID, source state.ObjID, cast bool) {
	e.activateManaFor(p, source, cast, false, true)
}

// activateManaPayment is the payment-window form: the payer is paying a cost
// (the CR 601.2g cast window, a ward payment) rather than acting on
// priority, so "Activate only as an instant" mana abilities are withheld.
func (e *Engine) activateManaPayment(p state.PlayerID, source state.ObjID, cast bool) {
	e.activateManaFor(p, source, cast, false, false)
}

// activatePaymentMana opens the mana-ability-only window used while a
// cumulative-upkeep or triggered-Untap cost is being paid.
func (e *Engine) activatePaymentMana(p state.PlayerID, source state.ObjID) {
	e.activateManaFor(p, source, false, true, false)
}

// instantSpeedOnly reports whether a mana ability's InstantSpeed$ True
// timing restriction is present ("Activate only as an instant", Lion's Eye
// Diamond): the ability is activatable exactly when its controller holds
// priority. The engine activates mana abilities in exactly two contexts: a
// priority window (the "activate for mana" action) and a payment window
// (paying for a spell, a ward or a cumulative-upkeep cost). CR 605.4 lets a
// player activate mana abilities while paying a cost only as far as the
// ability's own rules permit, and the card's text bars everything but a
// priority moment -- so the ability is activatable at priority and never
// inside a payment window.
func (e *Engine) instantSpeedOnly(ma *cards.SA) bool {
	return strings.EqualFold(strings.TrimSpace(ma.Params["InstantSpeed"]), "True")
}

// availableManaAbilitiesForWindow is the member set for one window: the
// ordinary priority set, or the payment-window set with the InstantSpeed$
// timing-restricted abilities withheld.
// priorityManaAbilityCount is len(availableManaAbilitiesForWindow(p, id,
// true)), counted in the engine's scratch list.
func (e *Engine) priorityManaAbilityCount(p state.PlayerID, id state.ObjID) int {
	buf := e.manaAbScratch
	e.manaAbScratch = nil
	all := e.appendAvailableManaAbilities(buf[:0], nil, p, id)
	n := len(all)
	clear(all)
	e.manaAbScratch = all[:0]
	return n
}

func (e *Engine) availableManaAbilitiesForWindow(p state.PlayerID, id state.ObjID, atPriority bool) []*cards.SA {
	all := e.availableManaAbilities(p, id)
	if atPriority {
		return all
	}
	out := make([]*cards.SA, 0, len(all))
	for _, ma := range all {
		if e.instantSpeedOnly(ma) {
			continue
		}
		out = append(out, ma)
	}
	return out
}

func (e *Engine) activateManaFor(p state.PlayerID, source state.ObjID, cast, cumulative, atPriority bool) {
	var abilities []*cards.SA
	if atPriority {
		// The priority member set is built in the engine's scratch list:
		// the common single-ability source resolves from it and keeps
		// nothing; a choice among several keeps an owned copy below.
		buf := e.manaAbScratch
		e.manaAbScratch = nil
		abilities = e.appendAvailableManaAbilities(buf[:0], nil, p, source)
		if len(abilities) <= 1 {
			var only *cards.SA
			if len(abilities) == 1 {
				only = abilities[0]
			}
			clear(abilities)
			e.manaAbScratch = abilities[:0]
			if only == nil {
				return
			}
			e.resolveManaAbilityInteractive(p, source, only, cast, cumulative)
			e.continueManaPaymentWindow(cumulative)
			return
		}
		owned := slices.Clone(abilities)
		clear(abilities)
		e.manaAbScratch = abilities[:0]
		abilities = owned
	} else {
		abilities = e.availableManaAbilitiesForWindow(p, source, atPriority)
	}
	if len(abilities) == 0 {
		return
	}
	if len(abilities) == 1 {
		e.resolveManaAbilityInteractive(p, source, abilities[0], cast, cumulative)
		e.continueManaPaymentWindow(cumulative)
		return
	}
	o := e.G.Obj(source)
	d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Choose a mana ability of " + o.Face().Name, Source: source}
	chosen := e.chosenProducedColour(source)
	for i, ma := range abilities {
		// An explicit multi-colour "Produced$ Combo <colours>" ability is
		// flattened into one option per colour (task fb-20260917T232800Z):
		// the player asked for per-colour pip bubbles, not the prose "Add B
		// or R" plus a second stage-2 colour ask. Each flattened option
		// carries the ability's own index, so the answer resolves that
		// ability with its Produced$ rewritten to the chosen colour and the
		// cost is paid once with no follow-up. Only plain AB$ Mana abilities
		// flatten -- a ManaReflected ability's colours come from what other
		// sources produce, never from its own Produced$ token. The colour
		// order is the ability's own token order, the same order askManaColor
		// offers today, so the two cannot disagree. Any / Combo Any / Chosen
		// keep the single option + stage-2 ask (the choice there is not
		// enumerable at option-build time).
		if cols, ok := manaAbilityComboColours(ma, chosen); ok {
			for _, col := range cols {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "mana", Obj: source,
					Ability: i, Label: manaAbilityCostPrefix(ma) + "Add " + manaAmountPips(ma, col), ManaSymbol: col})
			}
			continue
		}
		label := manaAbilityLabel(ma, chosen)
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "mana", Obj: source,
			Ability: i, Label: label})
	}
	gained := make([]gainedManaRef, len(abilities))
	for i, ma := range abilities {
		gained[i] = e.gainedManaRefFor(p, source, ma)
	}
	e.manaActivation = &manaActivation{player: p, source: source, abilities: abilities, gained: gained, cast: cast, cumulative: cumulative}
	windowAsk(e, d, chooseMana)
}

// manaAbilityLabel renders a mana ability's Produced$ value as the label the
// stage-1 "choose a mana ability" wheel shows (task fb-e079def5), with a
// recorded "Chosen" token already substituted (the combo-Chosen family), so
// the wheel shows "Add R", never the raw "Combo R Chosen" jargon. The raw
// script token ("Combo B R", "Any") is engine jargon a player cannot read,
// and it defeats the client's mana-pip styling for the whole wheel (the web
// tints an option list only when EVERY label is a single "Add <C>"), so a
// Talisman-of-Indulgence-shaped source rendered as plain grey text. The
// sibling ask sites (askManaColor, askTriggeredManaColor, the replacement
// colour ask in replacement.go) already emit resolved single colours -- this
// was the one label site that leaked the raw Produced string. Combo
// <colours> reads "Add B or R" (three or more: comma-separated, the last
// joined with "or"); Any/Combo Any read "Add any color" (the oracle's own
// wording, CR 107.4) unless they carry a literal Amount$ above one, which is
// then named ("Add three mana of any one color" for Any, "... in any
// combination of colors" for Combo Any) so a source whose abilities differ
// only in amount offers distinguishable wheel options (task
// mana-wheel-amount-labels); Produced$ Chosen reads "Add chosen color";
// anything else -- a plain single colour or C (the shape the wheel tints), a
// doubled "RR", a Special expression -- keeps the bare "Add <value>" shape.
// manaAbilityComboColours reports whether the ability's own Produced$ is an
// explicit MULTI-colour combo ("Combo B R") and returns the colour list in
// the ability's own token order -- the same order askManaColor offers, so the
// flattened stage-1 wheel and the stage-2 ask cannot disagree. A trailing
// "Chosen" token is first substituted from the recorded as-enters colour
// (the Thriving-lands/gate family, substituteChosenProduced). Both call
// sites guard on ma.API == "Mana" before calling (a ManaReflected ability's
// colours come from what other sources produce, never its own Produced$
// token), so this helper's Produced$ read is Mana-attributed
// (apiSpecificRulesSA) rather than joining the generic rules union.
func manaAbilityComboColours(ma *cards.SA, chosen string) ([]string, bool) {
	if ma == nil || ma.API != "Mana" {
		return nil, false
	}
	produced := substituteChosenProduced(effects.ManaOf(ma).Produced, chosen)
	cols, ok := effects.ComboColours(produced)
	if !ok || len(cols) <= 1 {
		return nil, false
	}
	return cols, true
}

// manaAbilityProduced reads a mana ability's Produced$ value. Every caller
// passes a mana ability (the intrinsic-append dedup inside
// availableManaAbilitiesUsing's CR 305.6 walk), so the param census
// attributes the read to api:Mana alone (apiSpecificRulesSA) -- left in the
// generic union it would mask every other API's unread Produced$ (measured:
// api:Sacrifice/api:DealDamage).
func manaAbilityProduced(ma *cards.SA) string {
	return effects.ManaOf(ma).ProducedRaw
}

// manaAbilitiesProduce reports whether some ability of mas has Produced$
// exactly color (the granted-intrinsic dedup key).
func manaAbilitiesProduce(mas []*cards.SA, color string) bool {
	for _, ma := range mas {
		if manaAbilityProduced(ma) == color {
			return true
		}
	}
	return false
}

// Two riders make a paid ability recognisable on the wheel (fb-877b8f8f,
// fb-bbe4fd8f: Phyrexian Tower's "{T}, Sacrifice a creature: Add {B}{B}"
// read "Add B", so the player saw no way to sacrifice): a cost beyond the
// shared {T} is named in front of the production (manaAbilityCostPrefix,
// "Sacrifice 1 creature: Add BB"), and a literal Amount$ repeats a single
// pip (manaAmountPips, "Add BB"). An ability with neither keeps the bare
// shape byte-for-byte.
func manaAbilityLabel(ma *cards.SA, chosen string) string {
	return manaAbilityCostPrefix(ma) + manaProducedLabel(ma, chosen)
}

func manaProducedLabel(ma *cards.SA, chosen string) string {
	mp := effects.ManaOf(ma)
	produced := substituteChosenProduced(mp.Produced, chosen)
	switch produced {
	case "Any", "Combo Any":
		// A literal amount above one is named so a source whose abilities
		// differ only in amount -- Sceptre of Eternal Glory's one-mana and
		// three-mana "any color" abilities -- offers two distinguishable
		// wheel options. Without it both read "Add any color", and a manual
		// payer could not choose the larger ability on purpose (task
		// mana-wheel-amount-labels). The two shapes keep the distinct
		// wording their stage-2 prompt uses (manaColourPrompt).
		if n, ok := literalManaAmount(mp); ok && n > 1 {
			if produced == "Combo Any" {
				return "Add " + manaNumberWord(n) + " mana in any combination of colors"
			}
			return "Add " + manaNumberWord(n) + " mana of any one color"
		}
		return "Add any color"
	case "Chosen":
		return "Add chosen color"
	}
	if cols, ok := effects.ComboColours(produced); ok {
		if len(cols) == 1 {
			return "Add " + manaAmountPips(ma, cols[0])
		}
		last := len(cols) - 1
		return "Add " + strings.Join(cols[:last], ", ") + " or " + cols[last]
	}
	return "Add " + manaAmountPips(ma, produced)
}

// manaAmountPips repeats a single-pip production by the ability's literal
// Amount$ ("B" with Amount$ 2 is "BB"). Only a strconv-literal positive
// amount counts -- the manaColourPrompt rule: an absent, non-literal (X, an
// SVar, Count$) or non-positive Amount$ keeps the single pip, because a
// wrong number on the wheel is worse than none -- and only a one-letter
// WUBRGC pip is repeated, so a "RR" token or a Special expression is left as
// written. An amount above manaPipRepeatLimit keeps the bare pip too: the
// repeated spelling is the only one that expands, so its length is bounded
// here rather than in the shared literal rule.
func manaAmountPips(ma *cards.SA, pip string) string {
	if len(pip) != 1 || !strings.Contains("WUBRGC", pip) {
		return pip
	}
	mp := effects.ManaOf(ma)
	if !mp.Amount.Present {
		return pip
	}
	n, ok := literalManaAmount(mp)
	if !ok || n <= 1 {
		return pip
	}
	if n > manaPipRepeatLimit {
		// A single-pip production repeated by an absurd Amount$ would make an
		// unreadable label (and a needlessly long string). The label spelling
		// is the only consumer that expands, so the safety limit lives here;
		// the any-colour wording below never repeats and has no such bound.
		return pip
	}
	return strings.Repeat(pip, n)
}

// manaPipRepeatLimit bounds how many times manaAmountPips expands a single
// pip. It is a label-size safety valve, not a correctness gate: a literal
// amount above it is a corpus oddity, and the bare pip is the fail-safe
// spelling. (The any-colour wording names an amount once, so it is not
// bounded here.)
const manaPipRepeatLimit = 20

// literalManaAmount parses a mana ability's Amount$ parameter as a positive
// integer literal, shared by manaAmountPips (which repeats a single pip) and
// the any-colour label (which names the amount). Only an absent, non-literal
// (X, an SVar, Count$) or non-positive amount fails: the rule is exactly the
// manaColourPrompt rule, which also accepts every positive literal -- a wheel
// label for "Add 21 mana of any one color" must be as faithful as its
// stage-2 prompt, not silently fall back to "Add any color". A wrong number
// on the wheel is worse than none, but a refused large number is worse still.
func literalManaAmount(mp *effects.ManaParams) (int, bool) {
	if !mp.AmountIsLit || mp.AmountLit <= 0 {
		return 0, false
	}
	return mp.AmountLit, true
}

// manaNumberWord is a literal mana amount in words, for the any-colour
// stage-1 label ("Add three mana of any one color"). Amounts past twenty
// fall back to digits ("Add 21 mana of any one color"): the wording stays
// faithful rather than inventing a bound the stage-2 prompt does not apply.
func manaNumberWord(n int) string {
	words := [...]string{"zero", "one", "two", "three", "four", "five", "six",
		"seven", "eight", "nine", "ten", "eleven", "twelve", "thirteen",
		"fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen", "twenty"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return strconv.Itoa(n)
}

// manaAbilityCostPrefix names a mana ability's activation cost BEYOND the
// {T} every wheel option shares, as "<Cost phrase>: " -- empty when the cost
// is the bare tap (or blank), so the common "Add C" label is unchanged. It
// phrases the parsed cost with manaAbilityCostPhrase: costPhrase with the tap
// clause cleared (every option on the wheel is tapping this source, so the
// tap is not what the player is choosing between), plus the article and
// origin-zone wording a wheel label wants.
func manaAbilityCostPhrase(c Cost) string {
	phrase := costPhrase(c)
	// costPhrase keeps the synthesized count as digits (its objectPhrase),
	// which reads as engine jargon on a mana wheel: "Sacrifice 1 creature".
	// Replace each object clause with its article form for a single object
	// ("a creature"), keyed on the parsed spec's own noun -- so an artifact
	// or enchantment sacrifice reads the same as a creature one, and the
	// next object noun the grammar learns needs no new string here. A count
	// other than one, an announced X count and an embedded description keep
	// objectPhrase's exact text, so N > 1 keeps its number.
	for _, part := range c.Sac {
		phrase = replaceObjectClause(phrase, part, "permanent")
	}
	for _, part := range c.Discard {
		if strings.EqualFold(part.Spec, "Hand") {
			// Forge's Discard<N/Hand> pays the WHOLE hand (N is the
			// discard-all marker, 0 on the corpus's Lion's Eye Diamond),
			// so objectPhrase's "0 card"/"1 card" is not the payment.
			phrase = strings.Replace(phrase, "discard "+objectPhrase(part, "card"), "discard your hand", 1)
			continue
		}
		phrase = replaceObjectClause(phrase, part, "card")
	}
	for _, part := range c.Exile {
		clause := objectPhrase(part, "card")
		repl := articleObjectClause(part, "card") + exileOriginPhrase(part.Zone)
		phrase = strings.Replace(phrase, "exile "+clause, "exile "+repl, 1)
	}
	return phrase
}

// replaceObjectClause swaps one cost part's rendered object clause
// (":sacrifice 1 creature") for its article form within an already-rendered
// phrase. It is a no-op when the two are identical.
func replaceObjectClause(phrase string, part CostPart, defNoun string) string {
	clause := objectPhrase(part, defNoun)
	repl := articleObjectClause(part, defNoun)
	if clause == repl {
		return phrase
	}
	return strings.Replace(phrase, clause, repl, 1)
}

// articleObjectClause renders one cost part's object the way the wheel
// reads: "a creature" for a single ordinary object, objectPhrase's own text
// for every other shape (a count, an announced X, or a corpus description
// that already names the object). specNoun supplies the noun, so the next
// object type the grammar learns is covered without a new string here.
func articleObjectClause(part CostPart, defNoun string) string {
	if !part.Announced && part.N == 1 && part.Desc == "" {
		return articleNoun(specNoun(part.Spec, defNoun))
	}
	return objectPhrase(part, defNoun)
}

// articleNoun returns the indefinite article plus noun ("a creature", "an
// artifact"), so a single object cost reads naturally.
func articleNoun(noun string) string {
	if noun == "" {
		return noun
	}
	switch noun[0] {
	case 'a', 'e', 'i', 'o', 'u', 'A', 'E', 'I', 'O', 'U':
		return "an " + noun
	}
	return "a " + noun
}

// exileOriginPhrase names the origin zone an exile cost pays from, so
// "Exile a card" reads as the oracle's "Exile a card from your hand". The
// zero zone is the hand default the mana continuation itself uses
// (manaExiles); a graveyard exile names its zone too.
func exileOriginPhrase(z state.Zone) string {
	switch z {
	case 0, state.ZHand:
		return " from your hand"
	case state.ZGraveyard:
		return " from your graveyard"
	default:
		return ""
	}
}

func manaAbilityCostPrefix(ma *cards.SA) string {
	raw := strings.TrimSpace(ma.ParamStr(cards.PKCost))
	if raw == "" {
		return ""
	}
	c := ParseCost(raw)
	c.Tap = false
	phrase := manaAbilityCostPhrase(c)
	if phrase == "" {
		return ""
	}
	return strings.ToUpper(phrase[:1]) + phrase[1:] + ": "
}

// manaAbilityPayable is the mana-ability equivalent of the cast cost gate.
// A source with a sacrifice cost is not offered unless this synchronous path
// can pay it without a chooser. Discard costs have their own continuation:
// ordinary discard asks, while random and discard-your-hand do not.
func (e *Engine) manaAbilityPayable(p state.PlayerID, source state.ObjID, ma *cards.SA) bool {
	return e.manaAbilityPayablePool(p, source, ma, nil)
}

// tapCostSick is CR 302.6's shared source-cost predicate for {T}/{Q}.
// Tapping another permanent to pay a cost is intentionally not checked here.
func (e *Engine) tapCostSick(source state.ObjID, cost *Cost) bool {
	return e.tapFlagsSick(source, cost.Tap, cost.Untap)
}

// tapFlagsSick is tapCostSick reading only the cost's {T}/{Q} flags, so a
// caller holding a shared compiled cost (costRef) passes two bools rather
// than copying the whole Cost.
func (e *Engine) tapFlagsSick(source state.ObjID, tap, untap bool) bool {
	o := e.G.Obj(source)
	if o == nil || (!tap && !untap) || o.Zone != state.ZBattlefield || !o.SummonSick {
		return false
	}
	return slices.Contains(e.derivedTypesOf(source), "Creature") && !e.hasKeywordH(source, kwhHaste)
}

func activationTapCostUnavailable(o *state.Object, cost *Cost) bool {
	return o == nil || (cost.Tap && o.Tapped) || (cost.Untap && !o.Tapped)
}

func (e *Engine) manaAbilityTapSick(source state.ObjID, ma *cards.SA) bool {
	if ma == nil {
		return false
	}
	c := e.costRef(ma.ParamStr(cards.PKCost))
	return e.tapFlagsSick(source, c.Tap, c.Untap)
}

// manaAbilityPayablePool is manaAbilityPayable with the mana part priced
// against an optional explicit pool: hyp nil keeps the ordinary real-pool
// gate (the restriction-adjusted manaAvailableFor the offer walk uses), hyp
// non-nil prices the activation against the potential-action walk's growing
// hypothetical bound (rules/potential.go PotentialMana), which is what lets
// a source's paid activation be reached after the seat floats mana from a
// cheaper source first. Every non-mana read -- tap state and the literal
// tapXType<N/Spec> candidates, sacrifice, discard and exile candidates, the
// announced-part refusals -- is real in both modes: hypothetical mana never
// satisfies a sacrifice or a tap.
func (e *Engine) manaAbilityPayablePool(p state.PlayerID, source state.ObjID, ma *cards.SA, hyp *state.Mana) bool {
	o := e.G.Obj(source)
	if o == nil || o.Face() == nil {
		return false
	}
	cc := e.compiledCostOf(ma.ParamStr(cards.PKCost))
	if e.tapFlagsSick(source, cc.Tap, cc.Untap) {
		return false
	}
	return e.manaCostPayable(p, o, source, cc, hyp)
}

// manaPayFastVerify makes manaCostPayable price every bare-{T} cost through
// the full walk too and panic on a disagreement. Set by the rules test
// binary.
var manaPayFastVerify = derivedMemoVerifyFlag != ""

// manaCostPayable is manaAbilityPayablePool after its source and CR 302.6
// gates: o is source's live object (non-nil, with a face) and the tap-sick
// check has already passed. A bare {T} cost -- no mana, no other component
// -- is payable exactly when the source is untapped and the priced pool's
// total is not negative (the empty requirement's only mana read:
// resolveManaWith's closing total check); every other read of the full walk
// (the life total, the sacrifice/discard/exile/tap candidate walks, the
// energy counter, which never goes below zero) is priced against an empty
// requirement. A restricted batch in the payer's pool reshapes the priced
// pool through manaAvailableFor's per-batch filter, so that case keeps the
// full walk. The test binary proves the equivalence on every call
// (manaPayFastVerify).
func (e *Engine) manaCostPayable(p state.PlayerID, o *state.Object, source state.ObjID, cc *compiledCost, hyp *state.Mana) bool {
	if cc.bareTap && (hyp != nil || len(e.G.Players[p].RestrictedMana) == 0) {
		pool := e.G.Players[p].Pool
		if hyp != nil {
			pool = *hyp
		}
		fast := !o.Tapped && pool.Total() >= 0
		if manaPayFastVerify {
			if slow := e.manaCostPayableFull(p, o, source, cc.Cost, hyp); slow != fast {
				panic(fmt.Sprintf("rules: bare-{T} mana payability fast path %v disagrees with the full walk %v (source %d)", fast, slow, source))
			}
		}
		return fast
	}
	return e.manaCostPayableFull(p, o, source, cc.Cost, hyp)
}

// manaCostPayableFull is the full cost walk of manaCostPayable.
func (e *Engine) manaCostPayableFull(p state.PlayerID, o *state.Object, source state.ObjID, cost Cost, hyp *state.Mana) bool {
	av := e.manaAvailableFor(p, paymentFor(source, true, cost))
	pool := av.pool
	typed := av.typed
	if hyp != nil {
		pool = *hyp
		// A hypothetical bound is a pure mana bound that may include
		// restricted units, so its typed partition is the raw tally (the
		// typed counts never affect payability anyway).
		typed = e.G.Players[p].ManaUnits()
	}
	if cost.X != 0 || len(cost.Reveal) > 0 || len(cost.RevealOrChoose) > 0 || len(cost.RevealChosen) > 0 || len(cost.Behold) > 0 ||
		len(cost.Blight) > 0 || activationTapCostUnavailable(o, &cost) || !e.costPayablePool(p, source, true, cost, pool, typed) {
		return false
	}
	// A Forage cost is payable when the payer's graveyard holds three cards OR
	// they control a Food; the settle poses the two-arm election (the cast
	// path's nonManaCastable read).
	if cost.Forage && !e.manaForagePayable(p, source) {
		return false
	}
	// untapYType<N/Spec> parts (Benthic Explorers): enough distinct TAPPED
	// matching permanents exist (an untap cost needs a tapped permanent).
	if !e.manaUntapPayable(p, source, cost) {
		return false
	}
	// The mana-activation path announces its own X for an announced
	// SubCounter<X/...> part and settles it off the continuation; a PayLife<X>
	// part still has no ask here, so the ability is not offered rather than
	// paid for free.
	if len(cost.LifeX) > 0 {
		return false
	}
	if !manaCostPartsSettleable(cost) {
		return false
	}
	// Announced SubCounter<X/...> parts: the mana path announces X (the cast
	// path's xAsk shape) and settles the removal, so the offer gate prices
	// the announcement instead of refusing it. XMin floors X (Rasputin and
	// Jetfire carry XMin$ 1), so a board that cannot settle at least XMin is
	// unpayable -- the fail-closed direction. A fixed part still needs its
	// counters: source-anchored from the source, anchored from any candidate.
	if bound, any := e.manaSubCounterXBound(p, o, source, cost); any && bound < cost.XMin {
		return false
	}
	for _, part := range cost.SubCounter {
		if part.Announced {
			continue
		}
		if subCounterTargetsSource(part.Target) {
			if o.Counter(part.Spec) < part.N {
				return false
			}
			continue
		}
		if len(e.subCounterRemovalCandidates(p, source, part, part.N, nil)) == 0 {
			return false
		}
	}
	// PayEnergy<N> (Aether Hub, Servant of the Conduit): the payer's energy
	// total must cover every fixed part (CR 107.14); the settle spends it
	// through chargeEnergyCost, the cast path's one energy-charging site.
	energy := int32(0)
	for _, part := range cost.Energy {
		energy += part.N
	}
	if energy > e.G.Players[p].Counter("ENERGY") {
		return false
	}
	// AddCounter<N/KIND> and Exert<1/CARDNAME> are source-anchored: the
	// source must still be the permanent that receives the counter or the
	// exert (a mana ability is activated from the battlefield).
	if (len(cost.AddCounter) > 0 || len(cost.Exert) > 0) && o.Zone != state.ZBattlefield {
		return false
	}
	// Return<N/Spec> parts: the mana path pays only the self-return
	// (Spec CARDNAME, Forge's payCostFromSource -- Grinning Ignus's
	// "{R}, Return this creature to its owner's hand"), which needs no
	// choice. Any other Return spec, or a Return beside a part the
	// discard/sacrifice continuation owns, is refused rather than activated
	// with the return silently unpaid.
	if !manaReturnCostSupported(cost) {
		return false
	}
	for range cost.Return {
		if o.Zone != state.ZBattlefield {
			return false
		}
	}
	sacs, ok := e.manaSacrifices(p, source, cost)
	if !ok {
		return false
	}
	discards, ok := e.manaDiscards(p, source, cost)
	if !ok {
		return false
	}
	exiles, ok := e.manaExiles(p, source, cost)
	if !ok {
		return false
	}
	// tapXType<N/Spec> parts (Springleaf Drum, Heritage Druid): the payer must
	// have enough untapped matching permanents, reserving the source when the
	// same cost also taps it and the sacrifice/discard/exile picks above (one
	// permanent cannot pay two parts of one cost). The candidates here are the
	// same battlefield-order walk the payment election uses, so the count that
	// offered the activation and the objects the payer may tap cannot
	// disagree.
	reserved := make(map[state.ObjID]bool, len(sacs)+len(discards)+len(exiles))
	for _, id := range sacs {
		reserved[id] = true
	}
	for _, id := range discards {
		reserved[id] = true
	}
	for _, id := range exiles {
		reserved[id] = true
	}
	if !e.manaTapsPayable(p, source, cost, reserved) {
		return false
	}
	return true
}

// manaTapsPayable reports whether a mana ability's literal tapXType<N/Spec>
// parts have enough untapped matching permanents, reserving the given set
// (the sacrifice/discard/exile picks already claimed), the source when the
// cost's own {T} taps it, and each earlier tap part's picked permanents. The
// X form (Forge's tapXType<X/Spec> head) is settled by the payment election in
// continueManaDiscard -- the count announced there is the ability's X -- so an X
// part with no eligible permanent is affordable at X=0 (the cast path's
// tapPermanentCostAsk does the same), while a non-X dynamic head still fails
// closed HERE: no "any number" election exists beside a mana ability yet.
func costHasDynamicXTap(cost *Cost) bool {
	for _, part := range cost.TapPermanent {
		if part.Dyn == "X" {
			return true
		}
	}
	return false
}

// manaAbilityWithPaidX binds the dynamic tap election to this activation's
// production amount. The compiled SA is immutable, and an explicit literal
// Amount$ survives the mana-colour continuation without inheriting another
// object's X: Hazel's `Amount$ X`/`SVar:X:Count$xPaid` is the elected tap
// count, which has no home on the off-stack mana path's Ctx (xPaid resolves
// the CAST's paid X, so reading it here would leak an enclosing spell's X).
// Copy-on-write keeps every other activation of the same card untouched.
func manaAbilityWithPaidX(ma *cards.SA, x int32) *cards.SA {
	cp := *ma
	cp.Params = make(map[string]string, len(ma.Params))
	for k, v := range ma.Params {
		cp.Params[k] = v
	}
	cp.Params["Amount"] = fmt.Sprint(x)
	return &cp
}

func (e *Engine) manaTapsPayable(p state.PlayerID, source state.ObjID, cost Cost, reserved map[state.ObjID]bool) bool {
	claimed := make(map[state.ObjID]bool, len(reserved)+1)
	for id := range reserved {
		claimed[id] = true
	}
	if cost.Tap {
		claimed[source] = true
	}
	for _, part := range cost.TapPermanent {
		if part.Dyn != "" && part.Dyn != "X" {
			return false
		}
		cands := e.manaTapCandidates(p, source, part.Spec, claimed)
		if part.Dyn == "X" {
			// The dynamic election itself announces X (0..len(cands)), and the
			// payment election in continueManaDiscard claims each elected
			// candidate, so the offer gate reserves nothing here: an X part is
			// affordable at X=0 whatever the candidate count.
			continue
		}
		if int32(len(cands)) < part.N {
			return false
		}
		for i := int32(0); i < part.N; i++ {
			claimed[cands[i]] = true
		}
	}
	return true
}

// manaTapCandidates returns, in battlefield order, the untapped permanents
// that can pay one literal tapXType<N/Spec> part and are not already claimed.
// It is the one candidate walk shared by the offer gate (manaTapsPayable)
// and the payment election (continueManaDiscard), so the count the offer is
// priced on and the objects the payer may tap cannot diverge.
func (e *Engine) manaTapCandidates(p state.PlayerID, source state.ObjID, spec string, claimed map[state.ObjID]bool) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.costCandidates(p, source, state.ZBattlefield, spec, false, true) {
		if !claimed[id] {
			out = append(out, id)
		}
	}
	return out
}

// manaTapsPicked is manaTapsPayable's deterministic first-eligible pick set:
// the R-9 no-ask stand-in for a caller that cannot pose the tap election
// (interactive == false). It walks the same manaTapCandidates order the
// election offers, claiming each part's first N, so a non-interactive
// activation taps exactly what the silent build did and a replay rebuilds
// the identical Tap events.
func (e *Engine) manaTapsPicked(p state.PlayerID, source state.ObjID, cost Cost, sacs []state.ObjID) []state.ObjID {
	claimed := make(map[state.ObjID]bool, len(sacs)+1)
	for _, id := range sacs {
		claimed[id] = true
	}
	if cost.Tap {
		claimed[source] = true
	}
	var taps []state.ObjID
	for _, part := range cost.TapPermanent {
		if part.Dyn != "" {
			// A caller without a choice surface must not silently announce X=0
			// for an ability whose output depends on the dynamic tap count; the
			// whole non-interactive activation already fails closed in
			// resolveManaAbilityRefOriginal, so this is belt-and-braces.
			return nil
		}
		cands := e.manaTapCandidates(p, source, part.Spec, claimed)
		n := int(part.N)
		if n > len(cands) {
			n = len(cands)
		}
		for i := 0; i < n; i++ {
			claimed[cands[i]] = true
			taps = append(taps, cands[i])
		}
	}
	return taps
}

// manaSacrifices finds enough candidates for each sacrifice cost part. The
// activation continuation chooses which candidates pay when there is a choice.
// Candidates are still returned in deterministic battlefield order for the
// no-choice path.
func (e *Engine) manaSacrifices(p state.PlayerID, source state.ObjID, cost Cost) ([]state.ObjID, bool) {
	candidates := make([][]state.ObjID, len(cost.Sac))
	needs := make([]int, len(cost.Sac))
	for i, part := range cost.Sac {
		needs[i] = int(part.N)
		if needs[i] <= 0 {
			return nil, false
		}
		candidates[i] = e.sacrificeCostCandidates(p, source, part, true)
	}
	used := make(map[state.ObjID]bool)
	var chosen []state.ObjID
	var assign func(int, int) bool
	assign = func(part, unit int) bool {
		for part < len(needs) && unit >= needs[part] {
			part++
			unit = 0
		}
		if part == len(needs) {
			return true
		}
		for _, id := range candidates[part] {
			if used[id] {
				continue
			}
			used[id] = true
			chosen = append(chosen, id)
			if assign(part, unit+1) {
				return true
			}
			chosen = chosen[:len(chosen)-1]
			delete(used, id)
		}
		return false
	}
	if !assign(0, 0) {
		return nil, false
	}
	return chosen, true
}

// manaDiscards performs the pure offer-side feasibility walk for a mana
// ability's discard cost. It reserves deterministic candidates but consumes
// no RNG; the payment continuation makes the actual choice.
func (e *Engine) manaExiles(p state.PlayerID, source state.ObjID, cost Cost) ([]state.ObjID, bool) {
	var exiles []state.ObjID
	reserved := map[state.ObjID]bool{}
	for _, part := range cost.Exile {
		zone := part.Zone
		if zone == 0 {
			zone = state.ZHand
		}
		var candidates []state.ObjID
		for _, id := range e.G.Zone(zone, p) {
			if !reserved[id] && e.matchesSpecFrom(part.Spec, id, p, source) {
				candidates = append(candidates, id)
			}
		}
		if part.N <= 0 || int(part.N) > len(candidates) {
			return nil, false
		}
		for i := 0; i < int(part.N); i++ {
			reserved[candidates[i]] = true
			exiles = append(exiles, candidates[i])
		}
	}
	return exiles, true
}

func (e *Engine) manaDiscards(p state.PlayerID, source state.ObjID, cost Cost) ([]state.ObjID, bool) {
	var discards []state.ObjID
	reserved := map[state.ObjID]bool{}
	for _, part := range cost.Discard {
		candidates := e.discardCandidates(p, source, part, false, reserved)
		if strings.EqualFold(part.Spec, "Hand") {
			for _, id := range candidates {
				reserved[id] = true
				discards = append(discards, id)
			}
			continue
		}
		n := int(part.N)
		if n <= 0 || n > len(candidates) {
			return nil, false
		}
		for i := 0; i < n; i++ {
			id := candidates[0]
			reserved[id] = true
			discards = append(discards, id)
			candidates = candidates[1:]
		}
	}
	return discards, true
}

// continueManaDiscard walks a mana ability's discard parts without putting
// the ability on the stack. Ordinary parts ask their controller; Random and
// Hand select internally using the same rules as discardAsk.
func (e *Engine) continueManaDiscard() {
	md := e.manaDiscardActivation
	if md == nil {
		return
	}
	// The tapXType<N/Spec> election runs first: the tap stage claims its
	// permanents before the sacrifice stage's reserved map includes them, and
	// the tap candidates reserve the sacrifice picks the offer walk already
	// made (a permanent cannot pay two parts of one cost). A non-X Dyn part
	// never reaches here -- manaTapsPayable refused the whole ability.
	for md.tapPart < len(md.cost.TapPermanent) {
		part := md.cost.TapPermanent[md.tapPart]
		claimed := make(map[state.ObjID]bool, len(md.taps)+len(md.sacs)+1)
		for _, id := range md.taps {
			claimed[id] = true
		}
		for _, id := range md.sacs {
			claimed[id] = true
		}
		if md.cost.Tap {
			claimed[md.source] = true
		}
		candidates := e.manaTapCandidates(md.player, md.source, part.Spec, claimed)
		if part.Dyn == "X" {
			if len(candidates) == 0 {
				md.ability = manaAbilityWithPaidX(md.ability, 0)
				md.tapPart++ // X=0 needs no empty decision.
				continue
			}
			d := &decision.Decision{Player: md.player, Kind: decision.KChoose, Min: 0, Max: len(candidates),
				Prompt: "Choose any number of tokens to tap for the mana ability", Source: md.source}
			for _, id := range candidates {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "tapcost", Obj: id, Label: e.targetName(id)})
			}
			e.choosing = chooseManaTap
			e.ask(d)
			return
		}
		// The offer gate agreed, so a shortfall is a board that changed under
		// the offer: drop the payment rather than ask an election no answer
		// can satisfy.
		if int32(len(candidates)) < part.N {
			e.manaDiscardActivation = nil
			e.choosing = chooseNone
			return
		}
		if int32(len(candidates)) == part.N {
			// Exactly N candidates makes the tap forced. Record them without a
			// zero-information ask, matching tapPermanentCostAsk.
			md.taps = append(md.taps, candidates[:int(part.N)]...)
			md.tapPart++
			continue
		}
		d := &decision.Decision{Player: md.player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
			Prompt: "Choose permanents to tap for the mana ability", Source: md.source}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "tapcost", Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseManaTap
		e.ask(d)
		return
	}
	// The announced-SubCounter X announcement and removal election, then the
	// Forage election, then the untapYType election. Each may pause on an ask
	// or drop the payment when the board changed under the offer.
	if e.manaSubCounterStage(md) {
		return
	}
	if e.manaDiscardActivation == nil {
		return
	}
	if e.manaForageStage(md) {
		return
	}
	if e.manaDiscardActivation == nil {
		return
	}
	for md.sacPart < len(md.cost.Sac) {
		part := md.cost.Sac[md.sacPart]
		reserved := make(map[state.ObjID]bool, len(md.sacs)+len(md.taps))
		for _, id := range md.sacs {
			reserved[id] = true
		}
		// A permanent elected to tap cannot also be sacrificed (one permanent
		// cannot pay two parts of one cost).
		for _, id := range md.taps {
			reserved[id] = true
		}
		var candidates []state.ObjID
		for _, id := range e.sacrificeCostCandidates(md.player, md.source, part, true) {
			if !reserved[id] {
				candidates = append(candidates, id)
			}
		}
		n := int(part.N) - md.sacPaid
		// The same split sacAsk makes: only a part with a LATER Sac part is
		// paid one unit per decision under the feasibility filter (every
		// offered option leaves a complete distinct assignment for the later
		// parts, so Validate, Clamp and the bot cannot strand one). The LAST
		// Sac part keeps the historical exact-N shape -- forced when exactly n
		// candidates remain, else one Min == Max == n ask -- because nothing
		// downstream can be stranded by its answer.
		hasLaterSac := md.sacPart+1 < len(md.cost.Sac)
		if hasLaterSac {
			pools := make([][]state.ObjID, len(md.cost.Sac))
			needs := make([]int, len(md.cost.Sac))
			for i, futurePart := range md.cost.Sac {
				needs[i] = int(futurePart.N)
				if i == md.sacPart {
					needs[i] -= md.sacPaid
				}
				pools[i] = e.sacrificeCostCandidates(md.player, md.source, futurePart, true)
			}
			candidates = feasibleSacrificeChoices(candidates, pools, needs, md.sacs, md.sacPart)
		}
		if n <= 0 || n > len(candidates) {
			e.manaDiscardActivation = nil
			e.choosing = chooseNone
			return
		}
		var d *decision.Decision
		if hasLaterSac {
			// A singleton candidate is forced. Record it and continue one unit
			// at a time so each pick preserves a complete assignment for later
			// parts.
			if len(candidates) == 1 {
				md.sacs = append(md.sacs, candidates[0])
				md.sacPaid++
				if md.sacPaid >= int(part.N) {
					md.sacPart++
					md.sacPaid = 0
				}
				continue
			}
			d = &decision.Decision{Player: md.player, Kind: decision.KChoose, Min: 1, Max: 1,
				Prompt: "Choose permanents to sacrifice for the mana ability", Source: md.source}
		} else {
			// Exactly N candidates makes the sacrifice forced. Record that
			// deterministic battlefield-order set without a zero-information
			// ask; only a wider candidate set gives the player a choice.
			if len(candidates) == n {
				md.sacs = append(md.sacs, candidates...)
				md.sacPart++
				md.sacPaid = 0
				continue
			}
			d = &decision.Decision{Player: md.player, Kind: decision.KChoose, Min: n, Max: n,
				Prompt: "Choose permanents to sacrifice for the mana ability", Source: md.source}
		}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "sacrifice", Obj: id, Label: e.G.Obj(id).Face().Name})
		}
		e.choosing = chooseManaSacrifice
		e.ask(d)
		return
	}
	for md.part < len(md.cost.Discard) {
		part := md.cost.Discard[md.part]
		reserved := make(map[state.ObjID]bool, len(md.discards))
		for _, id := range md.discards {
			reserved[id] = true
		}
		candidates := e.discardCandidates(md.player, md.source, part, false, reserved)
		if strings.EqualFold(part.Spec, "Hand") {
			md.discards = append(md.discards, candidates...)
			md.part++
			continue
		}
		n := int(part.N)
		if n <= 0 || n > len(candidates) {
			e.manaDiscardActivation = nil
			e.choosing = chooseNone
			return
		}
		if strings.EqualFold(part.Spec, "Random") {
			for i := 0; i < n; i++ {
				pick := e.Rand(len(candidates))
				md.discards = append(md.discards, candidates[pick])
				candidates = append(candidates[:pick], candidates[pick+1:]...)
			}
			md.part++
			continue
		}
		d := &decision.Decision{Player: md.player, Kind: decision.KChoose, Min: n, Max: n,
			Prompt: "Discard a card to pay the mana ability cost", Source: md.source}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "mana_discard",
				Obj: id, Label: e.G.Obj(id).Face().Name})
		}
		e.choosing = chooseManaDiscard
		e.ask(d)
		return
	}
	for md.exilePart < len(md.cost.Exile) {
		part := md.cost.Exile[md.exilePart]
		zone := part.Zone
		if zone == 0 {
			zone = state.ZHand
		}
		reserved := make(map[state.ObjID]bool, len(md.exiles))
		for _, id := range md.exiles {
			reserved[id] = true
		}
		var candidates []state.ObjID
		for _, id := range e.G.Zone(zone, md.player) {
			if !reserved[id] && e.matchesSpecFrom(part.Spec, id, md.player, md.source) {
				candidates = append(candidates, id)
			}
		}
		n := int(part.N)
		if n <= 0 || n > len(candidates) {
			e.manaDiscardActivation = nil
			e.choosing = chooseNone
			return
		}
		if n == 1 && len(candidates) == 1 && candidates[0] == md.source &&
			strings.EqualFold(part.Spec, "CARDNAME") {
			md.exiles = append(md.exiles, md.source)
			md.exilePart++
			continue
		}
		d := &decision.Decision{Player: md.player, Kind: decision.KChoose, Min: n, Max: n,
			Prompt: "Exile a card to pay the mana ability cost", Source: md.source}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "mana_exile",
				Obj: id, Label: e.G.Obj(id).Face().Name})
		}
		e.choosing = chooseManaExile
		e.ask(d)
		return
	}
	if e.manaUntapStage(md) {
		return
	}
	if e.manaDiscardActivation == nil {
		return
	}
	e.commitManaDiscard()
}

func (e *Engine) commitManaDiscard() {
	md := e.manaDiscardActivation
	if md == nil || !e.payManaConvFor(md.player, md.source, true, md.cost, e.paymentConv(md.player, md.source, true)) {
		e.manaDiscardActivation = nil
		e.choosing = chooseNone
		return
	}
	// Only a decision posed BY this payment defers the mana effect below.
	posedBefore := e.pending != nil
	e.payDiscardCost(md.discards, "")
	for _, id := range md.exiles {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone,
				To: state.ZExile, Text: "exiled as a mana ability cost"})
		}
	}
	e.payMillCost(md.player, md.cost.Mill)
	// The elected tapXType permanents are tapped as part of the cost, before
	// the source's own {T} (the cast path's emitChoiceCosts/payCast order), so
	// a TapsForMana trigger on one of them matches the same way in both
	// paths. The Tap events carry the same "tapped as a cost" text the cast
	// path uses, so a replay rebuilds the identical chain.
	for _, id := range md.taps {
		e.emit(events.Event{Kind: events.Tap, Obj: id, Text: "tapped as a cost"})
	}
	var manaTriggers []pendingTrigger
	if md.cost.Tap {
		manaTriggers = e.emitManaTap(md.player, md.source, md.ability)
	}
	if md.cost.Untap {
		e.emit(events.Event{Kind: events.Untap, Obj: md.source, Player: md.player, Text: "untapped as a cost"})
	}
	e.payManaSourceParts(md.player, md.source, md.cost)
	e.settleManaSubCounter(md)
	for _, id := range md.sacs {
		e.emit(events.Sacrifice(id))
	}
	e.settleManaForage(md)
	e.settleManaUntap(md)
	e.manaDiscardActivation = nil
	e.choosing = chooseNone
	if !posedBefore && e.pending != nil {
		// Paying the cost posed a decision: a sacrificed (or discarded,
		// or exiled) commander's CR 903.9 move is parked and its owner is
		// being asked. Resolving the mana effect now would pose its colour
		// choice ON TOP of that ask -- overwriting it, so the parked move
		// is never emitted, the commander stays on the battlefield and the
		// same cost can be "paid" again for free forever (the botbench
		// Phyrexian Altar + Rakdos, the Muscle livelock). The effect waits
		// for the answer instead; Submit resumes it (resumeManaAfterCost).
		e.manaAfterCost = &manaAfterCost{player: md.player, source: md.source, ability: md.ability,
			cast: md.cast, cumulative: md.cumulative, triggers: manaTriggers,
			sacs: append([]state.ObjID(nil), md.sacs...), gained: md.gained,
			untaps: append([]state.ObjID(nil), md.untaps...)}
		return
	}
	e.resolveManaEffect(md.player, md.source, md.ability, md.cast, md.cumulative, manaTriggers, md.sacs, md.gained, md.untaps)
	e.continueManaPaymentWindow(md.cumulative)
}

// manaAfterCost parks a paid mana ability's effect while a decision its cost
// payment posed is outstanding (see Engine.manaAfterCost). untaps carries the
// elected untapYType permanents through the park, so a ManaReflected
// "Defined.Untapped" selector still binds them at resume.
type manaAfterCost struct {
	player     state.PlayerID
	source     state.ObjID
	ability    *cards.SA
	cast       bool
	cumulative bool
	triggers   []pendingTrigger
	sacs       []state.ObjID
	gained     gainedManaRef
	untaps     []state.ObjID
}

// resumeManaAfterCost resolves the parked mana effect once the decision its
// cost payment posed has been answered, then continues whatever flow the
// activation belonged to -- the same tail the mana-cost choose arms run
// (handleChoose's chooseManaSacrifice case): Ward's payment window, an
// unless-cost payment, or the cast being paid for. A priority-window
// activation has no tail; Advance grants priority as usual.
func (e *Engine) resumeManaAfterCost() {
	r := e.manaAfterCost
	e.manaAfterCost = nil
	e.resolveManaEffect(r.player, r.source, r.ability, r.cast, r.cumulative, r.triggers, r.sacs, r.gained, r.untaps)
	if r.cumulative && e.choosing == chooseNone {
		e.paymentWindowAsk()
	}
	if e.pending != nil || e.manaCostChoicePending() {
		return
	}
	if e.wardMana != nil {
		e.continueWardMana()
	} else if e.unlessPayment != nil {
		e.advanceUnlessPayment()
	} else if r.cast {
		e.continueCast()
	}
}

// answerManaDiscard records one ordinary discard part and continues payment.
// It reports whether this mana activation belongs to an outer cast window.
func (e *Engine) answerManaSacrifice(chosen []decision.Option) bool {
	md := e.manaDiscardActivation
	if md == nil {
		return false
	}
	for _, opt := range chosen {
		md.sacs = append(md.sacs, opt.Obj)
		md.sacPaid++
	}
	part := md.cost.Sac[md.sacPart]
	if md.sacPaid >= int(part.N) {
		md.sacPart++
		md.sacPaid = 0
	}
	cast := md.cast
	e.continueManaDiscard()
	return cast
}

func (e *Engine) answerManaDiscard(chosen []decision.Option) bool {
	md := e.manaDiscardActivation
	if md == nil {
		return false
	}
	for _, opt := range chosen {
		md.discards = append(md.discards, opt.Obj)
	}
	md.part++
	cast := md.cast
	e.continueManaDiscard()
	return cast
}

func (e *Engine) answerManaExile(chosen []decision.Option) bool {
	md := e.manaDiscardActivation
	if md == nil {
		return false
	}
	for _, opt := range chosen {
		md.exiles = append(md.exiles, opt.Obj)
	}
	md.exilePart++
	cast := md.cast
	e.continueManaDiscard()
	return cast
}

// answerManaTap records one literal tapXType<N/Spec> election's picks and
// continues the payment. It mirrors answerManaSacrifice: the chosen objects
// are recorded on the activation and the same continuation resumes, so the
// tap stage's "answer then ask the next part" loop is identical to the
// sacrifice stage's.
func (e *Engine) answerManaTap(chosen []decision.Option) bool {
	md := e.manaDiscardActivation
	if md == nil {
		return false
	}
	part := md.cost.TapPermanent[md.tapPart]
	for _, opt := range chosen {
		md.taps = append(md.taps, opt.Obj)
	}
	if part.Dyn == "X" {
		// Hazel's Amount$ X is the elected count. Rewrite only this activation
		// copy so it survives the later colour decision without reading an
		// enclosing spell's X or mutating the compiled card ability.
		md.ability = manaAbilityWithPaidX(md.ability, int32(len(chosen)))
	}
	md.tapPart++
	cast := md.cast
	e.continueManaDiscard()
	return cast
}

// emitManaTap preserves "tapped for mana" as transient engine context while
// trigger matching runs. Tap's existing event payload stays unchanged, so a
// mana activation without a matching trigger keeps its historic event chain.
func (e *Engine) emitManaTap(p state.PlayerID, source state.ObjID, sa *cards.SA) []pendingTrigger {
	// A TapsForMana trigger can restrict the mana's Produced$ value. Keep the
	// activating SA alongside the synchronous tap marker so matching sees the
	// same output declaration that the activation will resolve. This context is
	// rebuilt by replay because replay takes the same activation path.
	produced := ""
	if sa != nil {
		produced = effects.ManaOf(sa).Produced
	}
	before := len(e.pendingTriggers)
	e.tappingForMana, e.tappingManaProduced = source, produced
	e.emitTap(source, p, false)
	e.tappingForMana, e.tappingManaProduced = 0, ""
	// The mana the activation is about to produce does not exist yet -- the
	// Tap event (and so this trigger match) precedes the mana effect -- so
	// record where the activation's ManaAdd batch will land. The batch is
	// read back in resolveTriggeredManaAbilities and bound onto each matched
	// trigger's context as TriggerMana (Mana Flare's ReflectProperty$
	// Produced). Scanning the log rather than reading sa.Params["Produced"]
	// makes the read the mana ACTUALLY produced, so a Produced$ Any colour
	// choice and a ProduceMana replacement are both reflected faithfully.
	// The pending boundary records WHICH triggers this tap queued for the
	// same stamp: only the CR 605.3b immediate batch is returned here, and
	// an ordinary TapsForMana trigger (C.A.M.P.'s targeted one) stays in
	// e.pendingTriggers for its later placement.
	e.manaTapMark, e.manaTapPendingFrom, e.manaTapSource = len(e.L.Events), before, source

	// CR 605.3b: a triggered mana ability resolves immediately after the mana
	// ability that caused it, without using the stack. Separate only newly
	// matched triggers from this Tap event; older pending triggers and ordinary
	// tap reactions such as Manabarbs remain in the normal APNAP queue.
	matched := e.pendingTriggers[before:]
	kept := e.pendingTriggers[:before]
	var immediate []pendingTrigger
	for _, pt := range matched {
		if e.isTriggeredManaAbility(pt) {
			immediate = append(immediate, pt)
			continue
		}
		kept = append(kept, pt)
	}
	e.noteTrigShrink()
	e.pendingTriggers = kept
	return immediate
}

// isTriggeredManaAbility recognises Forge's Static$ True marker for a
// TapsForMana trigger. Forge uses that marker for CR 605.1b triggered mana
// abilities; checking the real trigger by source/index avoids treating every
// TapsForMana reaction (notably Manabarbs) as immediate. A target anywhere in
// the linked effect chain keeps the trigger on the stack, as CR 605.1b
// requires a mana ability not to require a target.
func (e *Engine) isTriggeredManaAbility(pt pendingTrigger) bool {
	t, ok := e.triggerOf(pt)
	if !ok {
		return false
	}
	if t.Mode != "TapsForMana" || !strings.EqualFold(t.ParamStr(cards.PKStatic), "True") {
		return false
	}
	for sa := pt.SA; sa != nil; sa = sa.Sub {
		if effects.TargetsOf(sa).Targeted() {
			return false
		}
	}
	return pt.SA != nil
}

// resolveTriggeredManaAbilities executes the CR 605.3b batch directly after
// the activated mana ability has resolved. These abilities never mint stack
// objects; their ordinary effect events are enough for deterministic replay.
//
// A triggered mana ability whose mana is a colour choice -- Produced$ Any or
// Combo Any (Fertile Ground, Regal Behemoth, Market Festival) or a Combo
// colour list -- is not resolved as colourless: the rest of the batch is
// parked and the player receiving the mana chooses, exactly as the activated
// path's askManaColor asks. The answer resolves that ability with the chosen
// colour and continues the batch (answerManaColor). cast is the payment
// window flag the parked activation carries back to the caller.
func (e *Engine) resolveTriggeredManaAbilities(triggers []pendingTrigger, cast, cumulative bool) {
	e.stampTriggeredManaProduced(triggers)
	for i := range triggers {
		pt := triggers[i]
		if int(pt.Controller) >= len(e.G.Players) || e.G.Players[pt.Controller].Lost {
			continue
		}
		if src := e.G.Obj(pt.Source); src != nil {
			// CR 702.140d: a triggered mana ability's SVar table is the
			// table of the face that carries its SA, not the pile top. The
			// SA here is a Trigger.Effect body minted from a face's Triggers
			// list, which is NEVER in that face's Abilities, so the owning
			// face is recovered by findTriggerForAbilityFace (the trigger
			// scan) -- pileFaceForSA, which searches Abilities, can only ever
			// report ok=false for a trigger body. pileFaceForSA stays as the
			// second try for the rare SA that IS a printed activated ability
			// arriving through this queue; the pile-top fallback is last, the
			// same three-step order rules/stack.go's resolving-object SVar
			// resolution uses.
			if _, f, ok := e.findTriggerForAbilityFace(pt.Source, pt.SA); ok && f != nil {
				effects.SetSVars(&pt.Ctx, f.SVars)
			} else if f, ok := e.pileFaceForSA(pt.Source, pt.SA); ok {
				effects.SetSVars(&pt.Ctx, f.SVars)
			} else if src.Face() != nil {
				effects.SetSVars(&pt.Ctx, src.Face().SVars)
			}
		}
		if e.ManaAbilityHook != nil {
			e.ManaAbilityHook(pt.Controller, pt.Source, pt.SA)
		}
		pt = e.rewriteChosenMana(pt)
		if e.askTriggeredManaColor(pt, triggers[i+1:], cast, cumulative) {
			return
		}
		if e.resolveTriggeredManaOffStack(pt, triggers[i+1:], cast, cumulative) {
			return
		}
	}
}

// stampTriggeredManaProduced binds the produced-type set an activated mana
// ability's Tap actually made to each trigger of that activation's CR 605.3b
// batch. It runs once per batch, at the first resolveTriggeredManaAbilities
// entry: the batch's triggers were matched and queued in emitManaTap, before
// the mana effect ran, so their context's TriggerMana is still empty. The
// batch is the activated activation's own, and every call site of
// resolveTriggeredManaAbilities threads a slice emitManaTap returned, so the
// scan window is exactly that activation -- the triggered abilities
// themselves resolve after this stamp, and the mana a triggered ability adds
// only reaches those later abilities' own reflect reads, never this batch's
// set.
//
// The mark is cleared whether or not the window carried ManaAdd, so a
// productionless mana ability (a fail-closed Produced$ the effect refused)
// cannot leave a stale window for a later batch.
func (e *Engine) stampTriggeredManaProduced(triggers []pendingTrigger) {
	if e.manaTapMark <= 0 {
		return
	}
	produced := e.manaProducedSince(e.manaTapMark)
	tapped := e.manaTapSource
	from := e.manaTapPendingFrom
	e.manaTapMark, e.manaTapPendingFrom, e.manaTapSource = 0, 0, 0
	if produced == "" {
		return
	}
	for i := range triggers {
		triggers[i].Ctx.TriggerMana = produced
	}
	// The ordinary (non-immediate) TapsForMana triggers the same tap queued
	// stay in e.pendingTriggers for their later placement, outside the
	// immediate batch this call stamps above. Bind the produced set onto
	// exactly the ones THIS tap queued: from the boundary emitManaTap
	// recorded, a TapsForMana trigger whose fold bound TriggerCard to the
	// tapped permanent (rules/trigger_referents.go's TapsForMana roles). An
	// unrelated trigger the activation's other events queued (a sacrifice,
	// a cost tap's reaction) keeps an unbound referent -- sharesColorWith's
	// TriggeredProduced reads the EMPTY set as shares-nothing, resolved.
	if from > len(e.pendingTriggers) {
		from = len(e.pendingTriggers)
	}
	for i := from; i < len(e.pendingTriggers); i++ {
		pt := &e.pendingTriggers[i]
		if tapped == 0 || pt.Ctx.TriggerCard != tapped {
			continue
		}
		if t, ok := e.triggerOf(*pt); ok && t.Mode == "TapsForMana" {
			pt.Ctx.TriggerMana = produced
		}
	}
}

// manaProducedSince returns the fixed-order WUBRGC set of mana types carried
// by the positive-amount ManaAdd events at or after log index mark, in the
// same order effects/trigger_referents.go documents for TriggerMana. A
// counter's trailing rune is its type: state.TypedManaTags and the snow "S"
// prefix a producer tag, never the type. Amount <= 0 is a spend or a zeroed
// unit, not production. Returns "" when the window produced nothing.
func (e *Engine) manaProducedSince(mark int) string {
	if mark < 0 || mark > len(e.L.Events) {
		return ""
	}
	var set uint8
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind != events.ManaAdd || ev.Amount <= 0 || ev.Counter == "" {
			continue
		}
		r := ev.Counter[len(ev.Counter)-1]
		if i := strings.IndexByte("WUBRGC", r); i >= 0 {
			set |= 1 << uint(i)
		}
	}
	if set == 0 {
		return ""
	}
	var b strings.Builder
	for i := range "WUBRGC" {
		if set&(1<<uint(i)) != 0 {
			b.WriteByte("WUBRGC"[i])
		}
	}
	return b.String()
}

// resolveTriggeredManaOffStack resolves one triggered mana ability's chain
// under an offStackManaFrame and reports whether a routed colour ask
// suspended it (the rest of the batch is then carried by that ask).
func (e *Engine) resolveTriggeredManaOffStack(pt pendingTrigger, rest []pendingTrigger, cast, cumulative bool) bool {
	parked := clonePendingTriggers([]pendingTrigger{pt})[0]
	act := manaColorActivation{player: pt.Controller, source: pt.Source, cast: cast, cumulative: cumulative,
		triggers: rest, trigger: &parked}
	return e.withOffStackMana(act, func() { effects.Resolve(e, &pt.Ctx, pt.SA) })
}

// rewriteChosenMana resolves a triggered Mana sub-ability's Produced$ Chosen
// (Utopia Sprawl: "Whenever enchanted Forest is tapped for mana, its
// controller adds an additional one mana of the chosen color"): the colour
// was already chosen by the triggering source's as-enters ChooseColor choice
// (state.Object.ChosenColor), so it is a read, not a choice -- the chain is
// rewritten via withProduced so effMana sees a plain letter, and any later
// genuinely-choice-valued sub still asks through askTriggeredManaColor. With
// nothing recorded the chain is returned untouched and effMana keeps its
// loud fail-closed (never invent a colour). Measured on the corpus, the
// "Combo <letter> Chosen" family (Thriving Bluff, Citadel Gate) carries its
// Produced$ on AB$ activations only -- 0 triggered carriers -- so this
// rewrite keeps the exact-"Chosen" read and does not need the combo
// substitution (substituteChosenProduced covers it if a trigger ever
// carries one).
func (e *Engine) rewriteChosenMana(pt pendingTrigger) pendingTrigger {
	for sa, d := pt.SA, 0; sa != nil && d < 32; sa, d = sa.Sub, d+1 {
		if sa.API != "Mana" || effects.ManaOf(sa).Produced != "Chosen" {
			continue
		}
		o := e.G.Obj(pt.Source)
		if o == nil {
			return pt
		}
		col := strings.TrimSpace(o.ChosenColor)
		if len(col) != 1 || !strings.ContainsRune("WUBRG", rune(col[0])) {
			return pt
		}
		pt.SA = withProduced(pt.SA, sa, col)
		return pt
	}
	return pt
}

// askTriggeredManaColor poses the colour choice for the first colour-choice
// Mana sub-ability in pt's chain, parking pt and the rest of its batch, and
// reports whether it asked. The chooser is the first player the Mana
// sub-ability adds mana for (effects.ManaRecipients, which reads Defined$):
// Fertile Ground on an opponent's land asks that land's controller.
func (e *Engine) askTriggeredManaColor(pt pendingTrigger, rest []pendingTrigger, cast, cumulative bool) bool {
	mana, colours := triggeredManaColourChoice(pt.SA)
	if mana == nil {
		return false
	}
	chooser := pt.Controller
	if ps := effects.ManaRecipients(e, &pt.Ctx, mana); len(ps) > 0 {
		chooser = ps[0]
	}
	mp := effects.ManaOf(mana)
	amount := mp.AmountNum(e, &pt.Ctx, 1)
	allocation := strings.HasPrefix(mp.Produced, "Combo ") && amount > 1
	min, max := 1, 1
	if allocation {
		min, max = int(amount), int(amount)
	}
	d := &decision.Decision{Player: chooser, Kind: decision.KChoose, Min: min, Max: max,
		Prompt: manaColourPrompt(mana), Source: pt.Source}
	for unit := 0; unit < max; unit++ {
		for _, color := range colours {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "mana", Obj: pt.Source, Label: "Add " + color, ManaSymbol: color})
		}
	}
	parked := pt
	e.manaColorActivation = &manaColorActivation{player: chooser, source: pt.Source, ability: mana,
		cast: cast, cumulative: cumulative, triggers: rest, trigger: &parked, allocation: allocation}
	windowAsk(e, d, chooseManaColor)
	return true
}

// triggeredManaColourChoice finds the first Mana sub-ability in a triggered
// ability's chain whose Produced$ is a colour choice, with the colours it
// offers. nil when the chain adds only fixed mana (or none).
func triggeredManaColourChoice(sa *cards.SA) (*cards.SA, []string) {
	for d := 0; sa != nil && d < 32; d, sa = d+1, sa.Sub {
		if sa.API != "Mana" {
			continue
		}
		produced := effects.ManaOf(sa).Produced
		if produced == "Any" || produced == "Combo Any" {
			return sa, []string{"W", "U", "B", "R", "G"}
		}
		if colours, ok := effects.ComboColours(produced); ok {
			return sa, colours
		}
	}
	return nil, nil
}

// withProduced copies the chain from head down to target, with target's
// Produced$ rewritten to the chosen colour. Corpus SAs are shared immutable
// data, so the rewrite never touches them.
func withProduced(head, target *cards.SA, produced string) *cards.SA {
	return withManaProduction(head, target, produced, "")
}

// withManaAllocation records a resolved Combo allocation as one concrete
// symbol per selected unit and prevents effMana from multiplying that string
// by the original Amount$ again.
func withManaAllocation(head, target *cards.SA, produced string) *cards.SA {
	return withManaProduction(head, target, produced, "1")
}

func withManaProduction(head, target *cards.SA, produced, amount string) *cards.SA {
	if head == nil {
		return nil
	}
	cp := *head
	if head == target {
		cp.Params = make(map[string]string, len(head.Params)+1)
		for k, v := range head.Params {
			cp.Params[k] = v
		}
		cp.Params["Produced"] = produced
		if amount != "" {
			cp.Params["Amount"] = amount
		}
		return &cp
	}
	cp.Sub = withManaProduction(head.Sub, target, produced, amount)
	return &cp
}

// substituteChosenProduced replaces the literal "Chosen" token in a Mana
// ability's Produced$ value with the recorded as-enters chosen colour
// (state.Object.ChosenColor): the bare "Chosen" (Quirion Elves) and the
// "Combo <letter> Chosen" family (Thriving Bluff, Citadel Gate, the five
// thriving lands and five gates: "Add {R} or one mana of the chosen
// color"). It is a read, not a choice; with nothing valid recorded the
// value is returned unchanged and the caller keeps its loud fail-closed
// handling (never invent a colour). The classifier for the substituted
// value stays effects.ComboColours -- a pure string classifier shared with
// the tests and the mana projection, deliberately not taught about state --
// so the substitution happens here in rules, where ChosenColor is readable,
// and every caller (resolveManaEffect, manaAbilityComboColours,
// manaAbilityLabel) sees one consistent value. Measured on the corpus,
// every Chosen token is the value's LAST token ("Combo Chosen" has no fixed
// letter), so the rewrite only ever touches the tail.
func substituteChosenProduced(produced, chosen string) string {
	if chosen == "" {
		return produced
	}
	trimmed := strings.TrimSpace(produced)
	if trimmed == "Chosen" || trimmed == "ChosenColor" || trimmed == "ComboChosen" {
		return chosen
	}
	toks := strings.Fields(trimmed)
	if len(toks) >= 2 && toks[0] == "Combo" && (toks[len(toks)-1] == "Chosen" || toks[len(toks)-1] == "ChosenColor") {
		toks[len(toks)-1] = chosen
		// The recorded colour can equal a fixed letter ("Combo R Chosen" with
		// R recorded): dedup so the value stays "Combo R" -- a decision nobody
		// could answer differently resolves directly, and the duplicate
		// "Combo R R" would otherwise pose a two-option ask over one colour.
		out := toks[:1]
		for _, tok := range toks[1:] {
			seen := false
			for _, prev := range out {
				if prev == tok {
					seen = true
					break
				}
			}
			if !seen {
				out = append(out, tok)
			}
		}
		return "Combo " + strings.Join(out[1:], " ")
	}
	return produced
}

// chosenProducedColour reads the recorded as-enters chosen colour for
// source: a single WUBRG letter, else "" (nothing valid recorded). Shared
// by the activation-path Produced$ read sites.
func (e *Engine) chosenProducedColour(source state.ObjID) string {
	if o := e.G.Obj(source); o != nil {
		if col := strings.TrimSpace(o.ChosenColor); len(col) == 1 && strings.ContainsRune("WUBRG", rune(col[0])) {
			return col
		}
	}
	return ""
}

// resolveManaAbility pays this ability's actual activation cost, then resolves
// it outside the stack. In particular, Sac and Discard costs are emitted
// before the mana effect, and no phantom generic mana is charged.
func (e *Engine) resolveManaAbility(p state.PlayerID, source state.ObjID, ma *cards.SA, cast bool, cumulative ...bool) {
	payment := len(cumulative) > 0 && cumulative[0]
	e.resolveManaAbilityRef(p, source, ma, e.gainedManaRefFor(p, source, ma), cast, payment, false)
}

func (e *Engine) resolveManaAbilityInteractive(p state.PlayerID, source state.ObjID, ma *cards.SA, cast bool, cumulative ...bool) {
	payment := len(cumulative) > 0 && cumulative[0]
	e.resolveManaAbilityRef(p, source, ma, e.gainedManaRefFor(p, source, ma), cast, payment, true)
}

// gainedManaRefFor reports the has-all-abilities-of identity of mana ability
// sa activated from source, measured against sa's ORIGINAL compiled pointer
// (callers pass the ability before any Produced$ rewrite). A printed ability
// on source's own pile wins -- the ordinary case, answered without walking
// the grants -- so a recipient that happens to share the foreign card's
// compiled face keeps the printed identity. Otherwise the first live gained
// grant in grantedAbilities' deterministic order whose foreign SA is sa is
// the identity; grantedAbilities has already applied the grant's
// GainsValidAbilities$ filter and GainsAbilitiesLimitPerTurn$ cap, so a
// capped grant never supplies it.
func (e *Engine) gainedManaRefFor(p state.PlayerID, source state.ObjID, sa *cards.SA) gainedManaRef {
	o := e.G.Obj(source)
	if o == nil || sa == nil {
		return gainedManaRef{}
	}
	if _, _, printed := pileAbilityRefOf(o, sa); printed {
		return gainedManaRef{}
	}
	for _, ga := range e.grantedAbilities(p, source) {
		if ga.gained && ga.sa == sa && ga.gainedFace != nil {
			return gainedManaRef{from: ga.gainedFrom, idx: ga.gainedIdx, face: ga.gainedFace}
		}
	}
	return gainedManaRef{}
}

// resolveManaAbilityRef is resolveManaAbility with the gained identity
// already known (answerManaActivation captured it before rewriting the SA).
func (e *Engine) resolveManaAbilityRef(p state.PlayerID, source state.ObjID, ma *cards.SA, gained gainedManaRef, cast, payment, interactive bool) {
	e.resolveManaAbilityRefOriginal(p, source, ma, ma, gained, cast, payment, interactive)
}

// resolveManaAbilityRefOriginal resolves ma while retaining original's printed
// identity for activation-limit markers. Colour choices rewrite ma's Produced$
// on an immutable copy, but the limit census is keyed to the compiled ability
// in the source pile, not that copy.
func (e *Engine) resolveManaAbilityRefOriginal(p state.PlayerID, source state.ObjID, ma, original *cards.SA, gained gainedManaRef, cast, payment, interactive bool) {
	if !interactive && costHasDynamicXTap(e.costRef(ma.ParamStr(cards.PKCost))) {
		return
	}
	if !e.manaAbilityPayable(p, source, ma) {
		return
	}
	if e.ManaAbilityHook != nil {
		e.ManaAbilityHook(p, source, original)
	}
	// A GAINED mana ability records its activation identity: the same
	// ManaActivate marker, with IDs[0] naming the foreign card and Amount
	// its face-local index (never a flat pile index -- activationUsedCount
	// skips a marker carrying IDs), so the GainsAbilitiesLimitPerTurn$ cap's
	// log scan (gainedActivationsThisTurn) counts a mana activation exactly
	// as it counts a GainedAbilityPush, and a replay re-derives the count.
	// Emitted only for gained abilities, so no existing game's log changes.
	if gained.face != nil {
		e.emit(events.Event{Kind: events.ManaActivate, Player: p, Obj: source,
			IDs: []state.ObjID{gained.from}, Amount: int32(gained.idx)})
	}
	// The activation-limit scan marker: ManaAdd events carry no source
	// attribution, so an ability that carries EITHER limit records its
	// activation here (events.ManaActivate's own comment). Emitted only for a
	// limit-bearing ability so no existing game's log shape changes.
	if _, limited := original.Param(cards.PKActivationLimit); limited || original.ParamStr(cards.PKGameActivationLimit) != "" ||
		chainGatesOnActivationCount(original) {
		// The flat pile index (top face first, then under-cards) is the SAME
		// identity availableManaAbilitiesUsing's limit gate checks, so an
		// under-card mana ability's census cannot be counted against a
		// top-face ability. original keeps that identity when ma is a
		// colour-pinned immutable copy.
		if idx, _, found := pileAbilityRefOf(e.G.Obj(source), original); found {
			e.emit(events.Event{Kind: events.ManaActivate, Player: p, Obj: source, Amount: int32(idx)})
		}
	}
	cc := e.compiledCostOf(ma.ParamStr(cards.PKCost))
	cost := cc.Cost
	sacs, _ := e.manaSacrifices(p, source, cost)
	// The continuation owns EVERY non-mana cost part, so it must be entered
	// whenever one exists -- a caller that cannot ask (interactive == false:
	// the attack-cost tap window and the direct-resolve tests) still has to
	// pay the discard and exile parts. Only the sacrifice and tap ASKS are
	// gated: such a caller keeps the R-9 deterministic first-eligible set
	// manaSacrifices/manaTapsPicked picked and skips straight past those
	// parts.
	// Announced SubCounter parts need the X ask (and a filtered part its
	// removal-target election); a Forage cost needs its two-arm election; an
	// untapYType<N/Spec> part needs its tapped-permanent election. All three
	// ride the continuation beside the sacrifice/discard/exile/tap parts.
	needsContinuation := len(cost.Sac) > 0 || len(cost.Discard) > 0 || len(cost.Exile) > 0 ||
		len(cost.TapPermanent) > 0 || manaSubCounterNeedsAsk(cost) || cost.Forage || len(cost.UntapPermanent) > 0
	if needsContinuation {
		md := &manaDiscardActivation{player: p, source: source,
			ability: ma, cost: cost, cast: cast, cumulative: payment, gained: gained, interactive: interactive}
		if !interactive {
			md.sacs = sacs
			md.sacPart = len(cost.Sac)
			md.taps = e.manaTapsPicked(p, source, cost, sacs)
			md.tapPart = len(cost.TapPermanent)
		}
		e.manaDiscardActivation = md
		e.continueManaDiscard()
		return
	}
	if !e.payManaAbilityMana(p, source, cc) {
		return
	}
	e.payMillCost(p, cost.Mill)
	var manaTriggers []pendingTrigger
	if cost.Tap {
		manaTriggers = e.emitManaTap(p, source, ma)
	}
	if cost.Untap {
		e.emit(events.Event{Kind: events.Untap, Obj: source, Player: p, Text: "untapped as a cost"})
	}
	e.payManaSourceParts(p, source, cost)
	for _, id := range sacs {
		e.emit(events.Sacrifice(id))
	}
	// Return<1/CARDNAME>: the source goes to its OWNER's hand as part of the
	// payment (the cast path's Return settle, rules/cast.go), after the {T}
	// tap so a tap-and-return cost never taps a hand card.
	// manaAbilityPayablePool admitted only the self-return shape.
	for range cost.Return {
		if o := e.G.Obj(source); o != nil && o.Zone == state.ZBattlefield {
			e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: o.Zone, To: state.ZHand, Text: "returned to hand as a cost"})
		}
	}
	e.resolveManaEffect(p, source, ma, cast, payment, manaTriggers, sacs, gained, nil)
}

// manaReturnCostSupported reports whether a mana ability's Return<N/Spec>
// parts are the shape the off-stack mana path pays: none at all, or exactly
// the self-return Return<1/CARDNAME> with no sacrifice/discard/exile part
// beside it (those route through the manaDiscardActivation continuation,
// which does not settle a return).
func manaReturnCostSupported(cost Cost) bool {
	if len(cost.Return) == 0 {
		return true
	}
	if len(cost.Return) != 1 || len(cost.Sac) > 0 || len(cost.Discard) > 0 || len(cost.Exile) > 0 {
		return false
	}
	part := cost.Return[0]
	return part.N == 1 && strings.EqualFold(sacrificeMatchSpec(part.Spec), "CARDNAME")
}

// resolveManaEffect resolves the mana a paid ability produces. sacs carries
// the permanents the ability's Sac<...> cost sacrificed, so a ManaReflected
// Valid$ "Defined.Sacrificed" selector (Squandered Resources) can read them
// through the resolution context's Remembered list.
func (e *Engine) resolveManaEffect(p state.PlayerID, source state.ObjID, ma *cards.SA, cast, cumulative bool, triggers []pendingTrigger, sacs []state.ObjID, gained gainedManaRef, untaps []state.ObjID) {
	if strings.TrimSpace(ma.ParamStr(cards.PKUnlessCost)) != "" {
		e.askManaUnless(p, source, ma, cast, cumulative, triggers, sacs, gained)
		return
	}
	mp := effects.ManaOf(ma)
	produced := mp.Produced
	// A "Chosen" token (Quirion Elves' second activation: "Add one mana of
	// the chosen color"; the Thriving-lands/gate family: "Add {R} or one mana
	// of the chosen color") is a READ, not a choice: the colour was already
	// chosen by the source's as-enters ChooseColor choice
	// (state.Object.ChosenColor). With nothing recorded the local keeps the
	// raw value and the fall-through keeps effMana's loud fail-closed (never
	// invent a colour).
	if col := e.chosenProducedColour(source); col != "" {
		produced = substituteChosenProduced(produced, col)
	}
	if ma.API == "ManaReflected" {
		// CR 702.140d: resolve against the face that CARRIES this ability,
		// not the pile top -- an under-card mana ability's Reflected SVars
		// live on its own face. A granted/non-printed ability keeps the
		// top-face fallback (pileFaceForSA reports ok=false).
		svars := func() map[string]string {
			if gained.face != nil {
				return gained.face.SVars
			}
			if o := e.G.Obj(source); o != nil {
				if f, ok := e.pileFaceForSA(source, ma); ok {
					return f.SVars
				}
				if o.Face() != nil {
					return o.Face().SVars
				}
			}
			return nil
		}()
		ctx := effects.NewCtxPtr(source, p, effects.CtxInit{SVars: svars})
		ctx.CostUntapped = append([]state.ObjID(nil), untaps...)
		for _, id := range sacs {
			ctx.Remembered = append(ctx.Remembered, state.Target{Obj: id})
		}
		cols := effects.ManaReflectedCandidates(e, ctx, ma)
		switch len(cols) {
		case 0:
			e.emit(events.Event{Kind: events.Note, Obj: source, Text: "ManaReflected found no mana to reflect"})
		case 1:
			e.finishManaEffect(p, source, ma, cols[0], gained, sacs, cast, cumulative, triggers)
		default:
			e.askManaColor(p, source, ma, cast, cumulative, triggers, cols, gained, 1, sacs)
		}
		return
	}
	if costHasDynamicXTap(e.costRef(ma.ParamStr(cards.PKCost))) && mp.AmountTrim == "0" {
		// X=0 produces no mana and therefore has no meaningful colour
		// allocation decision.
		e.finishManaEffect(p, source, ma, produced, gained, sacs, cast, cumulative, triggers)
		return
	}
	if produced == "Any" || produced == "Combo Any" {
		e.askManaColor(p, source, ma, cast, cumulative, triggers, []string{"W", "U", "B", "R", "G"}, gained,
			e.manaEffectAmount(p, source, ma, sacs, gained), sacs)
		return
	}
	// A "Combo <colours>" shape is "add one of these", not "add each of
	// these": it asks, but restricted to exactly the colours it names --
	// "Combo R G" offers R and G, never the other three. The classifier
	// (effects.ComboColours) rejects "Combo Any" (kept on the five-colour
	// branch above) and every combo it cannot resolve to a plain colour list,
	// which then falls to resolveManaEffectColor and fails closed in effMana.
	// A SINGLE-colour list never asks -- a decision nobody could answer
	// differently is never posed (the same convention the ManaReflected and
	// ColorIdentity siblings apply): it resolves directly. Measured on the
	// corpus, no raw script carries "Produced$ Combo <one letter>"; the only
	// live carriers are the flattened stage-1 answers, whose ability arrives
	// with its Produced$ already rewritten to the chosen colour (task
	// fb-20260917T232800Z), so the shortcut skips the otherwise-posed
	// one-option stage-2 ask.
	if colours, ok := effects.ComboColours(produced); ok {
		if len(colours) == 1 {
			e.finishManaEffect(p, source, ma, colours[0], gained, sacs, cast, cumulative, triggers)
			return
		}
		e.askManaColor(p, source, ma, cast, cumulative, triggers, colours, gained,
			e.manaEffectAmount(p, source, ma, sacs, gained), sacs)
		return
	}
	// "ColorIdentity" (Command Tower, Arcane Signet: "Add one mana of any
	// color in your commander's color identity") is a colour choice scoped to
	// the activating player's commander colour identity (CR 903.4). Every
	// corpus occurrence is an AB$ Mana activation (6 files), so the branch
	// lives on the activation path only; the token is matched on the Produced
	// value itself (with or without the "Combo " prefix) rather than on the
	// API, so a future trigger carrying it cannot silently degrade. Colours
	// come in fixed WUBRG order: none (no commander, or a colourless one) keeps
	// today's fail-closed fall-through (CR 903.4's "any color" of an empty
	// identity is nothing, not colourless), one resolves directly (a decision
	// nobody could answer differently must not be posed), two or more ask.
	if isColourIdentityProduced(produced) {
		cols := e.commanderIdentityColours(p)
		switch len(cols) {
		case 1:
			e.finishManaEffect(p, source, ma, cols[0], gained, sacs, cast, cumulative, triggers)
			return
		default:
			if len(cols) > 1 {
				e.askManaColor(p, source, ma, cast, cumulative, triggers, cols, gained,
					e.manaEffectAmount(p, source, ma, sacs, gained), sacs)
				return
			}
			// 0 colours: fall through to the fail-closed resolve below.
		}
	}
	e.finishManaEffect(p, source, ma, produced, gained, sacs, cast, cumulative, triggers)
}

// askManaUnless handles the one off-stack instance of the shared UnlessCost$
// contract. Activated mana abilities cannot use effects.Host.Ask's stack
// resume point, but their payer still receives an ordinary KModes decision
// and rules charges exactly the same parsed cost on a "pay" answer.
func (e *Engine) askManaUnless(p state.PlayerID, source state.ObjID, ma *cards.SA, cast, cumulative bool, triggers []pendingTrigger, sacs []state.ObjID, gained gainedManaRef) {
	ctx := effects.NewCtxPtr(source, p, effects.CtxInit{})
	var payers []state.PlayerID
	for _, t := range effects.UnlessPayers(e, ctx, ma) {
		if t.IsPlayer {
			payers = append(payers, t.Player)
		}
	}
	if len(payers) == 0 {
		payers = []state.PlayerID{p}
	}
	e.manaUnlessActivation = &manaUnlessActivation{player: p, source: source, ability: ma,
		cast: cast, cumulative: cumulative, triggers: triggers, sacs: sacs, payers: payers, gained: gained}
	e.askManaUnlessDecision()
}

func (e *Engine) askManaUnlessDecision() {
	m := e.manaUnlessActivation
	if m == nil || m.next >= len(m.payers) {
		return
	}
	raw := strings.TrimSpace(m.ability.ParamStr(cards.PKUnlessCost))
	cost := capitaliseFirst(costPhrase(ParseCost(raw)))
	if cost == "" {
		// A cost costPhrase cannot render (a malformed or entirely
		// unmodelled token) keeps the raw text rather than emitting an
		// empty prompt -- the fallback is still better than "pay ?".
		cost = "Pay " + raw
	}
	d := &decision.Decision{Player: m.payers[m.next], Kind: decision.KModes, Min: 1, Max: 1,
		Source: m.source, ResumeKind: "mana_unless", Prompt: cost + ", or decline",
		Options: []decision.Option{
			{Index: 0, Kind: "mode", Obj: m.source, Player: m.payers[m.next], Label: cost},
			{Index: 1, Kind: "mode", Obj: m.source, Player: m.payers[m.next], Label: "Don't pay"},
		}}
	e.choosing = chooseManaUnless
	e.ask(d)
}

// answerManaUnless applies one payer's answer. Declines visit later named
// payers in turn; paying ends the sequence. A malformed cost is deliberately
// a decline, matching resumeResolution's ordinary unless-pay path.
func (e *Engine) answerManaUnless(chosen []decision.Option) bool {
	m := e.manaUnlessActivation
	if m == nil || m.next >= len(m.payers) {
		return false
	}
	payer := m.payers[m.next]
	paid := false
	if len(chosen) == 1 && chosen[0].Index == 0 {
		if cost, ok := ParseUnlessCost(m.ability.ParamStr(cards.PKUnlessCost)); ok {
			if len(cost.Sac) > 0 || len(cost.Discard) > 0 || len(cost.Reveal) > 0 || len(cost.Behold) > 0 || len(cost.RevealChosen) > 0 || len(cost.Return) > 0 {
				// Activated mana stays off stack, but a sacrifice/discard/return/
				// reveal/behold in its unless cost is still a real payer choice. The
				// payment continuation returns through finishManaUnlessPayment.
				e.beginUnlessPayment(payer, cost, effects.NewCtxPtr(m.source, m.player, effects.CtxInit{}), m.source, nil)
				return m.cast
			}
			paid = e.payUnlessCost(payer, cost, effects.NewCtxPtr(m.source, m.player, effects.CtxInit{}), m.source)
		}
	}
	e.finishManaUnlessPayment(paid)
	return m.cast
}

// finishManaUnlessPayment completes one payer's answer after either a direct
// payment or an asynchronous Sac/Discard choice.
func (e *Engine) finishManaUnlessPayment(paid bool) {
	m := e.manaUnlessActivation
	if m == nil {
		return
	}
	switched := strings.EqualFold(strings.TrimSpace(m.ability.ParamStr(cards.PKUnlessSwitched)), "True")
	if paid || m.next+1 == len(m.payers) {
		e.manaUnlessActivation = nil
		e.choosing = chooseNone
		if paid == switched {
			// Do not send this already-answered gate through effects.Resolve a
			// second time. Sub-abilities retain their own UnlessCost$ gates.
			cp := *m.ability
			cp.Params = make(map[string]string, len(m.ability.Params))
			for k, v := range m.ability.Params {
				cp.Params[k] = v
			}
			delete(cp.Params, "UnlessCost")
			delete(cp.Params, "UnlessPayer")
			delete(cp.Params, "UnlessSwitched")
			e.resolveManaEffect(m.player, m.source, &cp, m.cast, m.cumulative, m.triggers, m.sacs, m.gained, nil)
		} else {
			e.resolveTriggeredManaAbilities(m.triggers, m.cast, m.cumulative)
			e.continueManaPaymentWindow(m.cumulative)
		}
		return
	}
	m.next++
	e.askManaUnlessDecision()
}

// askManaColor poses the colour choice for a Produced value that names a
// fixed set. Any selects one colour for the whole Amount$; Combo allocates
// one option per mana unit, so Combo Any Amount 2 can select U then R.
func (e *Engine) askManaColor(p state.PlayerID, source state.ObjID, ma *cards.SA, cast, cumulative bool, triggers []pendingTrigger, colours []string, gained gainedManaRef, amount int32, sacs []state.ObjID) {
	allocation := strings.HasPrefix(effects.ManaOf(ma).Produced, "Combo ") && amount > 1
	min, max := 1, 1
	if allocation {
		min, max = int(amount), int(amount)
	}
	d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: min, Max: max,
		Prompt: manaColourPrompt(ma), Source: source}
	// The colour ask is the ONLY place a single-ability source's cost can
	// appear: activateManaFor resolves a one-ability source directly, with
	// no stage-1 wheel to name the cost (Mount Doom's "{T}, Pay 1 life: Add
	// {B} or {R}"). Name it on every colour option, so a paid activation
	// never offers a bare "Add B" that hides the life (fb-bbe4fd8f); a
	// multi-ability wheel's prefix is repeated here for the same reason.
	prefix := manaAbilityCostPrefix(ma)
	for unit := 0; unit < max; unit++ {
		for _, color := range colours {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "mana", Obj: source, Label: prefix + "Add " + color, ManaSymbol: color})
		}
	}
	e.manaColorActivation = &manaColorActivation{player: p, source: source, ability: ma, cast: cast, cumulative: cumulative, triggers: triggers, gained: gained, sacs: append([]state.ObjID(nil), sacs...), allocation: allocation}
	windowAsk(e, d, chooseManaColor)
}

// manaColourPrompt names the amount of mana the ability adds when the script
// carries an EXPLICIT, positive literal Amount$, so a player choosing the
// colour of "Add three mana of any one color" (Lion's Eye Diamond), or every
// unit of a Combo allocation, sees the whole deal instead of a bare "Choose a
// colour of mana" that reads like the card only offered one colour of mana.
// Every other shape keeps the generic prompt: an absent Amount$ (the prompt
// must not invent "1" for the pool that effMana will actually resolve), a
// non-literal amount (X, Y, an SVar or inline Count$ expression) the ask site
// cannot price, and a non-positive literal. The wording distinguishes
// Produced$ Any (one colour covers every unit) from Combo Any (the selected
// units may be split); a restricted "Combo <colours>" shape is not "any"
// colour, so its prompt only names the amount; a ColorIdentity shape names
// the commander-identity restriction.
func manaColourPrompt(ma *cards.SA) string {
	generic := "Choose a colour of mana"
	mp := effects.ManaOf(ma)
	shape := mp.Produced
	identity := isColourIdentityProduced(shape)
	if identity {
		generic = "Choose a colour in your commander's color identity"
	}
	if !mp.Amount.Present {
		// No Amount$ param: stay generic — the prompt must not invent an
		// amount the script never stated.
		return generic
	}
	n := mp.AmountLit
	if !mp.AmountIsLit || n <= 0 {
		// A non-literal amount (X, Y, an SVar or inline Count$ expression)
		// or a non-positive literal: the ask site cannot price it, and a
		// wrong number in the prompt is worse than no number.
		return generic
	}
	switch {
	case identity:
		return fmt.Sprintf("Add %d mana of any color in your commander's color identity — choose the colour", n)
	case shape == "Any":
		return fmt.Sprintf("Add %d mana of any one color — choose the colour", n)
	case shape == "Combo Any":
		return fmt.Sprintf("Add %d mana in any combination of colors — choose the colours", n)
	case strings.HasPrefix(shape, "Combo "):
		return fmt.Sprintf("Add %d mana — choose the colours", n)
	default:
		return fmt.Sprintf("Add %d mana — choose the colour", n)
	}
}

// manaEffectAmount resolves the production amount in the same source and
// sacrifice context effMana will receive. Combo's allocation must use this
// value before posing its decision, including an SVar such as Burnt Offering.
func (e *Engine) manaEffectAmount(p state.PlayerID, source state.ObjID, ma *cards.SA, sacs []state.ObjID, gained gainedManaRef) int32 {
	o := e.G.Obj(source)
	if o == nil || o.Face() == nil {
		return 0
	}
	ctx := e.manaAmountCtx(p, source)
	for _, id := range sacs {
		ctx.Sacrificed = append(ctx.Sacrificed, effects.SacrificedLKI(e, id))
	}
	effects.SetSVars(ctx, gained.svars(o.Face().SVars))
	amount := effects.ManaOf(ma).AmountNum(e, ctx, 1)
	if amount < 0 {
		return 0
	}
	return amount
}

func (e *Engine) resolveManaEffectColor(p state.PlayerID, source state.ObjID, ma *cards.SA, produced string, gained gainedManaRef, sacs []state.ObjID) {
	o := e.G.Obj(source)
	if o == nil || o.Face() == nil {
		return
	}
	// A plain printed "{T}: Add {G}" emits its one ManaAdd directly
	// (rules/mana_plain.go); the verify build checks it against the general
	// path below.
	plain, plainTap, isPlain := e.plainManaAdd(p, source, ma, produced, gained, sacs)
	if isPlain && !manaPlainVerify {
		savedTap, savedProducer := e.manaFromTap, e.manaProducer
		e.manaFromTap, e.manaProducer = plainTap, source
		e.emit(plain)
		e.manaFromTap, e.manaProducer = savedTap, savedProducer
		return
	}
	n0 := len(e.L.Events)
	copy := *ma
	copy.Params = make(map[string]string, len(ma.Params)+1)
	for k, v := range ma.Params {
		copy.Params[k] = v
	}
	copy.Params["Produced"] = produced
	// ProduceMana replacements need the ability's source and whether its
	// paid cost included T. Preserve both only for effMana's synchronous emit:
	// producer attribution is replacement matching context, not a durable
	// property of the resulting ManaAdd (putting it in Event.Obj moved every
	// replay chain head). A sacrifice-only KCI activation therefore identifies
	// its source but is not tap-produced.
	savedTap, savedProducer := e.manaFromTap, e.manaProducer
	e.manaFromTap = e.costRef(ma.ParamStr(cards.PKCost)).Tap
	e.manaProducer = source
	// A gained mana ability's body resolves its SVars (Amount$ X) against
	// the FOREIGN face it was compiled on, never the recipient's.
	e.resolveAbilitySacrificing(source, p, nil, &copy, gained.svars(o.Face().SVars), sacs)
	e.manaFromTap, e.manaProducer = savedTap, savedProducer
	if isPlain {
		e.verifyPlainMana(plain, n0)
	}
}

// answerManaColor completes a Produced$ Any choice after the activation cost
// has already been paid. It returns whether the suspended activation belonged
// to a cast-time mana window.
func (e *Engine) answerManaColor(chosen []decision.Option) bool {
	ma := e.manaColorActivation
	e.manaColorActivation = nil
	e.choosing = chooseNone
	if ma == nil {
		return false
	}
	if ma.nestedResume != nil {
		// Keep the mana frame active during re-entry too: the resumed rider
		// may itself ask again, and each such ask belongs to this activation,
		// not to whichever unrelated object is still atop the stack.
		template := *ma
		template.nestedResume = nil
		asked := e.withOffStackMana(template, func() {
			e.resumeResolution(ma.nestedResume, chosen)
		})
		if asked {
			return ma.cast
		}
		if e.pending == nil && e.resume == nil {
			e.resolveTriggeredManaAbilities(ma.triggers, ma.cast, ma.cumulative)
			e.continueManaPaymentWindow(ma.cumulative)
		}
		return ma.cast
	}
	if len(chosen) == 0 || (!ma.allocation && len(chosen) != 1) {
		return false
	}
	var symbols strings.Builder
	for _, option := range chosen {
		color := option.ManaSymbol
		if len(color) != 1 || !strings.Contains("WUBRGC", color) {
			return ma.cast
		}
		symbols.WriteString(color)
	}
	produced := symbols.String()
	if ma.nested != nil {
		e.answerNestedManaColor(ma, chosen)
		return ma.cast
	}
	if ma.trigger != nil {
		pt := *ma.trigger
		if ma.allocation {
			pt.SA = withManaAllocation(pt.SA, ma.ability, produced)
		} else {
			pt.SA = withProduced(pt.SA, ma.ability, produced)
		}
		// A later colour-choice Mana sub-ability in the same chain asks in
		// turn; otherwise the ability resolves and the batch continues.
		if e.askTriggeredManaColor(pt, ma.triggers, ma.cast, ma.cumulative) {
			return ma.cast
		}
		if e.resolveTriggeredManaOffStack(pt, ma.triggers, ma.cast, ma.cumulative) {
			return ma.cast
		}
		e.resolveTriggeredManaAbilities(ma.triggers, ma.cast, ma.cumulative)
		e.continueManaPaymentWindow(ma.cumulative)
		return ma.cast
	}
	ability := ma.ability
	if ma.allocation {
		ability = withManaAllocation(ma.ability, ma.ability, produced)
	}
	e.finishManaEffect(ma.player, ma.source, ability, produced, ma.gained, ma.sacs, ma.cast, ma.cumulative, ma.triggers)
	return ma.cast
}

// answerManaActivation completes a multi-ability choice and returns whether
// it came from the cast-time mana window. A flattened combo choice (the
// stage-1 wheel offered one option per colour of an explicit "Produced$ Combo
// <colours>" ability, task fb-20260917T232800Z) resolves the ability with its
// Produced$ rewritten to the chosen colour: the cost is paid exactly once by
// resolveManaAbility and the stage-2 colour ask never opens, because
// resolveManaEffect sees a single fixed colour. The rewrite guards on the
// ability's own Produced$ being a MULTI-colour combo and the label being one
// "Add <C>" pip, so a plain single-colour ability that happens to share the
// label keeps its unrewritten resolution.
func (e *Engine) answerManaActivation(chosen []decision.Option) bool {
	ma := e.manaActivation
	e.manaActivation = nil
	e.choosing = chooseNone
	if ma == nil || len(chosen) != 1 {
		return false
	}
	idx := chosen[0].Ability
	if idx >= 0 && idx < len(ma.abilities) {
		ab := ma.abilities[idx]
		var gained gainedManaRef
		if idx < len(ma.gained) {
			gained = ma.gained[idx]
		}
		if _, ok := manaAbilityComboColours(ab, e.chosenProducedColour(ma.source)); ok {
			// WUBRGC matches the set the removed label parser accepted, so a
			// combo colour is admitted exactly as it was before the field.
			color := chosen[0].ManaSymbol
			if len(color) == 1 && strings.Contains("WUBRGC", color) {
				// abilities entries are chain heads (printed faces list
				// top-level abilities; granted and static-granted ones
				// come from ResolveSVar bodies), so head == target copies
				// the whole Sub chain with Produced$ rewritten.
				e.resolveManaAbilityRefOriginal(ma.player, ma.source, withProduced(ab, ab, color), ab, gained, ma.cast, ma.cumulative, true)
				return ma.cast
			}
		}
		e.resolveManaAbilityRef(ma.player, ma.source, ab, gained, ma.cast, ma.cumulative, true)
	}
	return ma.cast
}

// isColourIdentityProduced reports whether a Produced$ value is the
// commander-identity choice shape — the literal "ColorIdentity" or the combo
// form "Combo ColorIdentity" (Command Tower, Arcane Signet, Commander's
// Sphere, Hidden Hideout, Opal Palace, Path of Ancestry). Shared by the
// activation-path branch in resolveManaEffect and manaColourPrompt's
// restricted wording so the two cannot disagree.
func isColourIdentityProduced(produced string) bool {
	return produced == "ColorIdentity" || produced == "Combo ColorIdentity"
}

// commanderIdentityColours expands seat p's commander colour identity to the
// colours it names, in fixed WUBRG order (the order cards.Face's colour bits
// are declared in, and the order deck/deck.go's identity checks read — no map
// range, so the offer order is deterministic). The identity is the bitwise
// union of every commander's full-card identity (cards.Card.ColourIdentity
// unions over faces), read off the live command-zone objects
// (state.Player.Commanders, populated at genesis and stable across zone
// moves). A seat with no commanders — or commanders whose identity is empty
// (colourless, CR 903.4) — yields a nil slice: "any color" of an empty
// identity is no colour at all, so the caller keeps its fail-closed
// behaviour.
func (e *Engine) commanderIdentityColours(p state.PlayerID) []string {
	if int(p) >= len(e.G.Players) {
		return nil
	}
	var m uint8
	for _, cid := range e.G.Players[p].Commanders {
		o := e.G.Obj(cid)
		if o == nil || o.Card == nil {
			continue
		}
		m |= o.Card.ColourIdentity()
		// CR 903.4b: a commander whose printed CDA says "choose a color before
		// the game begins" derives its identity from the recorded choice. Gate
		// on the CDA static, never the bare ChosenColor field: a commander with
		// an ordinary "as this enters" colour choice must not leak its
		// battlefield choice into its identity, and before the pregame answer
		// (or for a non-commander) ChosenColor is empty anyway.
		if o.Card.Faces[0] != nil && o.Card.Faces[0].CommanderColourChoiceCDA() {
			if cols, ok := resolveChosenColors("ChosenColor", o); ok {
				for _, l := range cols {
					if len(l) == 0 {
						continue
					}
					if i := strings.IndexByte("WUBRG", l[0]); i >= 0 {
						m |= 1 << uint(i)
					}
				}
			}
		}
	}
	var cols []string
	for i, sym := range []string{"W", "U", "B", "R", "G"} {
		if m&(1<<uint(i)) != 0 {
			cols = append(cols, sym)
		}
	}
	return cols
}

// CommanderIdentityColourCount is the effects.Host read over
// commanderIdentityColours: how many colours seat p's commander colour
// identity names. This is Count$ColorsColorIdentity's backing (War Room's
// fixed "Pay life equal to the number of colors in your commanders' color
// identity"); it reads the same genesis bookkeeping the replay rebuilds in
// Config order, so a replay derives the identical count.
func (e *Engine) CommanderIdentityColourCount(p state.PlayerID) int {
	return len(e.commanderIdentityColours(p))
}

// chainGatesOnActivationCount reports whether sa's SubAbility$ chain carries
// a ConditionActivationLimit$ gate ("if this ability has been activated four
// or more times this turn" -- Farrelite Priest, Initiates of the Ebon Hand).
// A mana ability has no AbilityPush, so such a chain needs the ManaActivate
// census marker for Ctx.ActivationsThisTurn to count it; the marker is
// emitted only for these carriers so no other game's log changes.
func chainGatesOnActivationCount(sa *cards.SA) bool {
	for sub, n := sa, 0; sub != nil && n < 32; sub, n = sub.Sub, n+1 {
		if strings.TrimSpace(sub.ParamStr(cards.PKConditionActivationLimit)) != "" {
			return true
		}
	}
	return false
}

// manaCostPartsSettleable is the mana path's fail-closed whitelist: a mana
// ability is activated off the stack by resolveManaAbilityRefOriginal and
// the manaDiscardActivation continuation, which settle exactly mana/life
// (payManaConvFor), {T}, Mill, SubCounter on the source, PayEnergy<N>,
// AddCounter on the source, Exert<1/CARDNAME>, Sac, Discard, Exile, the
// literal tapXType<N/Spec> tap (its election rides the same continuation)
// and the self-Return. Every other part -- an unmodelled token (Cost.Unknown:
// Pili-Pala's {Q}, Benthic Explorers' untapYType, both of which used to be
// priced as one phantom generic), CollectEvidence (Cryptex), a
// Draw/DamageYou/PutToLib/MoveToGrave/RollDice part, a dynamic PayEnergy<X>
// or a SubCounter anchored to another permanent (Jetfire's
// RemoveAnyCounter) -- has no settle here, so the ability is refused rather
// than activated with that part silently free. The remaining refusals (X,
// Reveal, Behold, a DYNAMIC tapXType<X/...>/<Any/...> part, Blight, Forage,
// LifeX, an unsupported Return) live beside the call site.
func manaCostPartsSettleable(cost Cost) bool {
	if len(cost.Unknown) > 0 || len(cost.Evidence) > 0 || len(cost.Draw) > 0 ||
		len(cost.DamageYou) > 0 || len(cost.GainLife) > 0 || len(cost.PutToLib) > 0 || len(cost.MoveToGrave) > 0 ||
		len(cost.RollDice) > 0 || cost.LifeHalfUp {
		return false
	}
	for _, part := range cost.Energy {
		if part.Spec == "X" {
			return false
		}
	}
	return true
}

// manaSubCounterXBound returns the largest X a cost's announced
// SubCounter<X/Kind> parts can settle, and whether the cost carries any. It
// mirrors the cast path's xAsk SubCounter bound exactly (the largest
// candidate's counter count for a filtered part, the source's count for a
// source-anchored part, the aggregate for the "Any" kind), and takes the
// MIN across parts so a composed cost announces only what every part can
// settle. The offer gate and the X ask both read it, so the count an
// activation is priced on and the count the payer may announce cannot
// diverge.
func (e *Engine) manaSubCounterXBound(p state.PlayerID, o *state.Object, source state.ObjID, cost Cost) (int32, bool) {
	bound := int32(0)
	any := false
	for _, part := range cost.SubCounter {
		if !part.Announced {
			continue
		}
		have := int32(0)
		if subCounterTargetsSource(part.Target) {
			if o != nil {
				have = subCounterAvailable(o, part.Spec)
			}
		} else {
			for _, oid := range e.subCounterRemovalCandidates(p, source, part, 1, nil) {
				co := e.G.Obj(oid)
				if co == nil {
					continue
				}
				n := subCounterAvailable(co, part.Spec)
				if strings.EqualFold(part.Spec, "Any") {
					have += n
				} else if n > have {
					have = n
				}
			}
		}
		if !any || have < bound {
			bound = have
		}
		any = true
	}
	return bound, any
}

// payManaSourceParts settles a mana ability's source-anchored non-mana cost
// parts, after the {T} tap and before any sacrifice (so a counter or exert
// lands on the still-present source): SubCounter removals, the PayEnergy<N>
// spend (chargeEnergyCost, the cast path's one energy-charging site), the
// AddCounter<N/KIND> placement (Wall of Roots' -0/-1 counter; the same
// CounterChange the activation settle emits) and the Exert<1/CARDNAME>
// exert (Oasis Ritualist; the same events.Exert the attack election emits).
// manaAbilityPayablePool gated each one, so every part here is payable.
func (e *Engine) payManaSourceParts(p state.PlayerID, source state.ObjID, cost Cost) {
	for _, part := range cost.SubCounter {
		if part.Announced || !subCounterTargetsSource(part.Target) {
			// An announced part (its amount is the announced X) or a part
			// anchored to another permanent is settled by
			// settleManaSubCounter off the continuation's picks, never here.
			continue
		}
		e.emit(events.Event{Kind: events.CounterChange, Obj: source, Counter: part.Spec, Amount: -part.N})
	}
	e.chargeEnergyCost(p, cost, 0)
	for _, part := range cost.AddCounter {
		if part.N != 0 {
			e.emit(events.Event{Kind: events.CounterChange, Obj: source, Counter: part.Spec, Amount: part.N})
		}
	}
	for range cost.Exert {
		if o := e.G.Obj(source); o != nil && o.Zone == state.ZBattlefield {
			e.emit(events.Event{Kind: events.Exert, Obj: source, Player: p})
		}
	}
}

// manaWalkHasLType is activeSummaryOf(active()).hasLType, answered from the
// offer walk's once-per-walk board facts when the caller is that walk.
func manaWalkHasLType(e *Engine, statics *actionStaticSource) bool {
	if statics != nil && statics.board.ready {
		if walkSkipVerify {
			if fresh := e.activeSummaryOf(e.active()).hasLType; fresh != statics.board.hasLType {
				panic(fmt.Sprintf("rules: walk board hasLType %v disagrees with a fresh read %v", statics.board.hasLType, fresh))
			}
		}
		return statics.board.hasLType
	}
	return e.activeSummaryOf(e.active()).hasLType
}
