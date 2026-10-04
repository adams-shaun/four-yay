// pay_engine.go is package rules' side of the payment layer's seam (lasagna
// spec §9, E7): the pay.Engine implementation (payer, a pointer conversion
// of *Engine, so its methods are not Engine methods), the aliases rules keeps
// for the payment vocabulary, and the cast-flow helpers that read rules-only
// types (pendingCast).
package rules

import (
	"fmt"
	"strconv"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// manaLetters is state.Mana's index order (MW, MU, MB, MR, MG, MC) spelled
// out as the WUBRGC symbols events.ManaAdd's Counter field expects.
var manaLetters = pay.ManaLetters

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
	ok, spentAll, _, spentSnow, spentTyped := pay.PayManaDescriptorForSpent(asPayer(e), pc.player, paymentForCast(pc, cost), cost,
		asPayer(e).Conv(pc.player, pc.card, false),
		pipRider{AnyColor: pc.mayPlayIgnore, AnyType: pc.mayPlayIgnoreType})
	return ok, spentAll, spentSnow, spentTyped
}

// paymentForCast is paymentFor for a pendingCast's resolved payment cost: the
// X the announcement machinery folded into Generic is still an X component of
// this payment (CostContainsX), so the marker rides pc.cost — the folded cost
// itself has Cost.X == 0 and would read as X-less.
func paymentForCast(pc *pendingCast, cost Cost) paymentDescriptor {
	d := paymentFor(pc.card, pc.isAbility(), cost)
	d.XAnnounced = pc.cost.X > 0
	return d
}

// AddsCounterGrant (pay.Engine) resolves one consumed rider batch into the grant payCast
// records: the producing ABILITY's rider parsed into (filter, kind, amount),
// with a non-literal amount looked up in the producing source's SVar table
// NOW (cast-payment time), and used -- how many of the batch's units the
// payment spent -- as the grant's unit count. A malformed rider, an
// unresolvable SVar name or a spent count of zero yields ok=false, so the
// caller drops it (fail closed) instead of placing a guessed counter.
func (pe *payer) AddsCounterGrant(r state.ManaRestriction, used int32) (state.ManaAddsCounterGrant, bool) {
	e := (*Engine)(pe)
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

// Conv (pay.Engine; formerly paymentConv) is the conversion set for p paying id (ability selects the
// ValidSA$ Spell/Activated scoping), or nil when no ManaConvert static would
// change any pip match. Returning nil -- not a zero conv -- keeps the pure
// resolveMana path (and every game without a converter on the board)
// byte-identical.
func (pe *payer) Conv(p state.PlayerID, id state.ObjID, ability bool) *manaConv {
	e := (*Engine)(pe)
	// With no printed ManaConvert static on the board and no Effect-delivered
	// one in active()'s list (its build digest), manaConversionParts has no
	// source to apply, so both parts are empty: the nil answer, directly.
	if len(e.manaConvPrintedSources()) == 0 && !e.activeSummaryOf(e.active()).hasManaConvert {
		if walkCacheVerify {
			if m, o := e.manaConversionParts(p, id, ability); !m.Empty() || !o.Empty() {
				panic(fmt.Sprintf("rules: a board with no ManaConvert source converts mana for obj %d", id))
			}
		}
		return nil
	}
	mandatory, optional := e.manaConversionParts(p, id, ability)
	conv := mandatory
	// During an Optional$ ManaConvert cast, the offer-side path uses the union
	// until the election is answered. Thereafter the selected arm is the only
	// one allowed to widen payment; this keeps target affordability, the mana
	// window and the actual charge on one answer.
	if e.cast != nil && e.cast.card == id && e.cast.manaConvertDone {
		if e.cast.manaConvertUse {
			pay.MergeConv(&conv, optional)
		}
	} else {
		pay.MergeConv(&conv, optional)
	}
	if conv.Empty() {
		return nil
	}
	// Copy out only the non-empty conversion: returning &conv directly moved
	// conv to the heap on EVERY call, including the common nil return.
	out := conv
	return &out
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
		return costAnnouncesX(ParseCost(o.Ability.ParamStr(cards.PKCost)))
	}
	return false
}

// payer is the Engine under pay.Engine's method set: asPayer is a pointer
// conversion, so passing it to package pay neither allocates nor copies, and
// its methods are not Engine methods.
type payer Engine

var _ pay.Engine = (*payer)(nil)

// asPayer views e as the payment layer's pay.Engine.
func asPayer(e *Engine) *payer { return (*payer)(e) }

type (
	// paymentDescriptor is one payment's identity (pay.Descriptor).
	paymentDescriptor = pay.Descriptor
	// paymentClass is the payment's kind (pay.Purpose).
	paymentClass = pay.Purpose
	// availableMana is the restriction-aware pool view (pay.Available).
	availableMana = pay.Available
)

const (
	paymentSpell            = pay.PurposeSpell
	paymentActivated        = pay.PurposeActivated
	paymentCumulativeUpkeep = pay.PurposeCumulativeUpkeep
	paymentOther            = pay.PurposeOther
)

func paymentFor(id state.ObjID, ability bool, cost Cost) paymentDescriptor {
	return pay.DescriptorFor(id, ability, cost)
}

func (pe *payer) Game() *state.Game                 { return pe.G }
func (pe *payer) Emit(ev events.Event) events.Event { return (*Engine)(pe).emit(ev) }
func (pe *payer) Verify() bool                      { return walkCacheVerify }
func (pe *payer) Log() *events.Log                  { return pe.L }
func (pe *payer) MatchesSpecFrom(spec string, id state.ObjID, you state.PlayerID, source state.ObjID) bool {
	return (*Engine)(pe).matchesSpecFrom(spec, id, you, source)
}
func (pe *payer) CastProvenanceAdmitsPending(spec string, objID state.ObjID, you state.PlayerID) (string, bool) {
	return (*Engine)(pe).castProvenanceAdmitsPending(spec, objID, you)
}
func (pe *payer) CostBlocked(op pay.CostBlock, id state.ObjID, cause pay.CostCause) bool {
	if op == pay.BlockExile {
		return (*Engine)(pe).exileBlockedForCost(id, cause)
	}
	return (*Engine)(pe).sacrificeBlockedForCost(id, cause)
}
func (pe *payer) Capture() pay.Capture {
	return pay.Capture{NoCounter: &pe.noCounterSpend, Sources: &pe.manaSpentSources, AddsCounters: &pe.manaSpentAddsCounters}
}
