package rules

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Window diagnostics are an opt-in, observer-only sidecar on priority
// decisions. A table that opts in (Config.WindowDiagnostics) gets, on each
// priority Decision it is asked to answer, a list of the FIRST gate in the
// offer walk that withheld each of its OWN visible candidates. It is a pure
// read: the collector emits no event, draws no RNG and changes no option
// list, so a diagnostics-enabled game replays byte-identically to one with
// the switch off.
//
// Everything here is a closed vocabulary token. No card name, no opponent
// state, no payment-plan Detail string ever reaches a WindowReason; an
// unclassifiable refusal emits wrAbilityUnsupported rather than inventing a
// string. The token size and the entry count are bounded, and the entries are
// sorted, so a deterministic replay cannot vary the sidecar's bytes.
const windowReasonLimit = 24

// The WindowReason.Kind vocabulary. A "card" is a card the seat may cast
// from a playable zone; a "land" is a land play; an "activation" is an
// activated ability of a permanent. "ability" is reserved for a withheld
// non-activation ability; the current classifier has no such producer.
// Option kinds are deliberately NOT used to clear these entries: a single
// object may be offered through another route (e.g. a Morph land is a cast
// even after its land drop is spent), or through a new option kind.
const (
	windowKindCard       = "card"
	windowKindLand       = "land"
	windowKindAbility    = "ability"
	windowKindActivation = "activation"
)

// The closed reason vocabulary. These are the ONLY strings a WindowReason
// may carry; the initial set is the spec's, and a shape the walk cannot
// classify fails closed to wrAbilityUnsupported.
const (
	wrTimingNotMain         = "timing:not_main"
	wrTimingStackNotEmpty   = "timing:stack_not_empty"
	wrTimingNotActive       = "timing:not_active"
	wrLandDropExhausted     = "land:drop_exhausted"
	wrLandGrantMissing      = "land:play_grant_missing"
	wrCostInsufficientMana  = "cost:insufficient_mana"
	wrCostUnpayable         = "cost:unpayable"
	wrCostTap               = "cost:tap"
	wrCostSacrifice         = "cost:sacrifice"
	wrCostLife              = "cost:life"
	wrCostX                 = "cost:x"
	wrCostPhyrexian         = "cost:phyrexian"
	wrTargetNoLegalTarget   = "target:no_legal_target"
	wrTargetCrossConstraint = "target:cross_constraint"
	wrActivationLimit       = "activation:limit_reached"
	wrActivationCondition   = "activation:condition_failed"
	wrActivationGate        = "activation:gate_failed"
	wrActivationZone        = "activation:zone_wrong"
	wrActivationActivator   = "activation:activator_refused"
	wrAbilityUnsupported    = "ability:unsupported"
)

// windowReasonTokens is the set of legal tokens, used by the tests to prove
// the vocabulary is closed. It is only looked up, never iterated.
var windowReasonTokens = map[string]bool{
	wrTimingNotMain:         true,
	wrTimingStackNotEmpty:   true,
	wrTimingNotActive:       true,
	wrLandDropExhausted:     true,
	wrLandGrantMissing:      true,
	wrCostInsufficientMana:  true,
	wrCostUnpayable:         true,
	wrCostTap:               true,
	wrCostSacrifice:         true,
	wrCostLife:              true,
	wrCostX:                 true,
	wrCostPhyrexian:         true,
	wrTargetNoLegalTarget:   true,
	wrTargetCrossConstraint: true,
	wrActivationLimit:       true,
	wrActivationCondition:   true,
	wrActivationGate:        true,
	wrActivationZone:        true,
	wrActivationActivator:   true,
	wrAbilityUnsupported:    true,
}

// windowCollector accumulates one entry per (candidate, kind), keeping the
// FIRST reason recorded. It is a fresh value per offer walk and never reaches
// replay, an event or a view of another seat.
type windowCollector struct {
	asked   state.PlayerID
	entries []decision.WindowReason
	seen    map[windowEntryKey]bool
}

type windowEntryKey struct {
	obj  state.ObjID
	kind string
}

func newWindowCollector(p state.PlayerID) *windowCollector {
	return &windowCollector{asked: p}
}

// record stores reason for (id, kind) once. A second record for the same key
// is ignored, so a candidate refused by an early gate keeps that gate's token
// even when a later gate in the same walk also refuses it -- gate order is
// the player-facing truth.
func (w *windowCollector) record(id state.ObjID, kind, reason string) {
	if w == nil || reason == "" {
		return
	}
	if !windowReasonTokens[reason] {
		reason = wrAbilityUnsupported
	}
	key := windowEntryKey{id, kind}
	if w.seen[key] {
		return
	}
	if w.seen == nil {
		w.seen = make(map[windowEntryKey]bool)
	}
	w.seen[key] = true
	w.entries = append(w.entries, decision.WindowReason{Obj: id, Kind: kind, Reason: reason})
}

// remove drops every entry for an object that produced ANY option, regardless
// of route or kind. Otherwise a land offered as a face-down spell could still
// report land:drop_exhausted, or a new special action could leave a stale
// activation reason. Obj 0 is not an object and cannot clear a candidate.
func (w *windowCollector) remove(id state.ObjID) {
	if w == nil || id == 0 {
		return
	}
	out := w.entries[:0]
	for _, entry := range w.entries {
		if entry.Obj == id {
			delete(w.seen, windowEntryKey{entry.Obj, entry.Kind})
		} else {
			out = append(out, entry)
		}
	}
	w.entries = out
}

// finish filters candidates that were actually offered, sorts by
// (Obj, Kind, Reason) and bounds the slice. Nil for a nil collector or an
// empty result, so an opted-out decision serialises byte-identically.
func (w *windowCollector) finish(opts []decision.Option) []decision.WindowReason {
	if w == nil {
		return nil
	}
	for _, o := range opts {
		w.remove(o.Obj)
	}
	if len(w.entries) == 0 {
		return nil
	}
	sort.Slice(w.entries, func(i, j int) bool {
		a, b := w.entries[i], w.entries[j]
		if a.Obj != b.Obj {
			return a.Obj < b.Obj
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Reason < b.Reason
	})
	if len(w.entries) > windowReasonLimit {
		w.entries = w.entries[:windowReasonLimit]
	}
	return w.entries
}

// sorcerySpeedReason decomposes the sorcery-speed timing gate into the ONE
// token naming why it refused: an inactive seat, a non-main step, or a
// non-empty stack. An empty string means the gate passed (the caller checks
// sorcerySpeed first).
func (e *Engine) sorcerySpeedReason(p state.PlayerID) string {
	if e.G.Active != p {
		return wrTimingNotActive
	}
	if !e.G.Step.IsMain() {
		return wrTimingNotMain
	}
	if len(e.G.Stack) != 0 {
		return wrTimingStackNotEmpty
	}
	return ""
}

// windowClassify records, for each of p's OWN visible candidates the walk did
// not offer, the FIRST gate in walk order that withheld it. It is a pure
// read: every predicate it calls is the same one the offer walk consults, so
// the two cannot disagree about whether a candidate is offered, and the gate
// order here mirrors the walk's.
//
// Redaction: only zones the asked seat can already see are scanned (its own
// hand, command zone, battlefield, graveyard and exile). An object owned by
// another seat is never classified, so an opponent's hidden candidate never
// produces an entry.
func (e *Engine) windowClassify(p state.PlayerID, opts []decision.Option, w *windowCollector) {
	if w == nil {
		return
	}
	e.classifyHand(p, w)
	e.classifyBattlefieldAbilities(p, w)
	e.classifyCommandZone(p, w)
	e.classifyGraveyardAndExile(p, w)
	// Every object offered by the walk clears all its entries in finish,
	// including an alternate route with a different Option kind.
}

// classifyHand covers the primary "why can't I cast this?" case: a card in
// the asked seat's hand. Gate order is the hand walk's own: land-drop, then
// CastSuppressed/CantBeCast, then timing, then targets, then cost.
func (e *Engine) classifyHand(p state.PlayerID, w *windowCollector) {
	sorcery := e.sorcerySpeed(p)
	dropOpen := e.G.Players[p].LandsPlayed < int32(1+e.adjustLandPlays(p))
	for _, id := range e.G.Zone(state.ZHand, p) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		f := o.Face()
		if f.IsLand() {
			// A hand land is offered only at sorcery speed with a drop open.
			if !dropOpen {
				w.record(id, windowKindLand, wrLandDropExhausted)
			} else if !sorcery {
				w.record(id, windowKindLand, nonEmpty(e.sorcerySpeedReason(p), wrAbilityUnsupported))
			} else {
				// A land that is neither playable nor face-down castable (no
				// Morph/Megamorph/Disguise) is withheld for a shape this build
				// does not model.
				w.record(id, windowKindLand, wrAbilityUnsupported)
			}
			continue
		}
		if e.castSuppressed(p, id) || e.castRestricted(p, id) {
			w.record(id, windowKindCard, wrAbilityUnsupported)
			continue
		}
		if !e.spellTimingOK(p, id, f, sorcery) {
			w.record(id, windowKindCard, nonEmpty(e.sorcerySpeedReason(p), wrAbilityUnsupported))
			continue
		}
		if sa := f.SpellAbility(); sa != nil && !e.castTargetsAvailable(p, id, sa) {
			w.record(id, windowKindCard, wrTargetNoLegalTarget)
			continue
		}
		// The plain cast's cost gate is the last conjunct the walk tests; a
		// face offered through any alternate route clears this entry in
		// finish.
		w.record(id, windowKindCard, wrCostInsufficientMana)
	}
}

// classifyBattlefieldAbilities covers an activated ability of a permanent the
// asked seat controls. It walks the SAME flat pile-ability list the printed
// activation loop does, in the same order, so a printed ability and an
// under-card ability classify alike and the entry names the ability's source
// object (never a synthetic id).
func (e *Engine) classifyBattlefieldAbilities(p state.PlayerID, w *windowCollector) {
	sorcery := e.sorcerySpeed(p)
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil || e.printedAbilitiesGone(o) || !existsOnBattlefield(o) {
			continue
		}
		for i, n := 0, o.PileAbilityCount(); i < n; i++ {
			pa, ok := o.PileAbilityAt(i)
			if !ok || pa.SA == nil || pa.SA.Kind != "AB" {
				continue
			}
			ab := pa.SA
			if isManaAbilityAPI(ab.API) && !e.isLoyaltyAbility(ab) {
				continue
			}
			// Gate order mirrors the printed activation loop.
			if !abilityZoneOK(ab, state.ZBattlefield) {
				w.record(id, windowKindActivation, wrActivationZone)
				continue
			}
			if ab.ParamStr(cards.PKSorcerySpeed) == "True" && !sorcery {
				w.record(id, windowKindActivation, nonEmpty(e.sorcerySpeedReason(p), wrTimingNotMain))
				continue
			}
			if !e.activatorAllows(p, id, ab) {
				w.record(id, windowKindActivation, wrActivationActivator)
				continue
			}
			if e.isLoyaltyAbility(ab) {
				if !sorcery && !e.loyaltyAtInstantSpeed(p, id) {
					w.record(id, windowKindActivation, nonEmpty(e.sorcerySpeedReason(p), wrTimingNotMain))
					continue
				}
				if e.loyaltyActivationsThisTurn(id) >= e.loyaltyAbilityLimit(id) {
					w.record(id, windowKindActivation, wrActivationLimit)
					continue
				}
			}
			if e.castSuppressed(p, id) {
				w.record(id, windowKindActivation, wrAbilityUnsupported)
				continue
			}
			if !e.activationConditionOK(p, ab) {
				w.record(id, windowKindActivation, wrActivationCondition)
				continue
			}
			if e.activationLimitBlocked(p, id, ab, i, "", pa.Merged) {
				w.record(id, windowKindActivation, wrActivationLimit)
				continue
			}
			if strings.EqualFold(strings.TrimSpace(ab.ParamStr(cards.PKBoast)), "True") && !e.boastGateOK(id, i, "") {
				w.record(id, windowKindActivation, wrActivationGate)
				continue
			}
			if !e.sVarGateOK(p, id, ab, pa.Merged) {
				w.record(id, windowKindActivation, wrActivationGate)
				continue
			}
			if !e.abilityPresentHolds(p, id, ab) {
				w.record(id, windowKindActivation, wrActivationGate)
				continue
			}
			if !e.adaptGateOK(id, ab) {
				w.record(id, windowKindActivation, wrActivationGate)
				continue
			}
			if !e.monstrosityGateOK(id, ab) {
				w.record(id, windowKindActivation, wrActivationGate)
				continue
			}
			if !e.abilityTargetsAvailable(p, id, ab) {
				w.record(id, windowKindActivation, wrTargetNoLegalTarget)
				continue
			}
			w.record(id, windowKindActivation, wrCostUnpayable)
		}
	}
}

// classifyCommandZone covers a commander whose cast is withheld (CR 903.8).
func (e *Engine) classifyCommandZone(p state.PlayerID, w *windowCollector) {
	sorcery := e.sorcerySpeed(p)
	for _, id := range e.G.Zone(state.ZCommand, p) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil || o.Face().IsLand() {
			continue
		}
		f := o.Face()
		if e.castSuppressed(p, id) || e.castRestricted(p, id) {
			w.record(id, windowKindCard, wrAbilityUnsupported)
			continue
		}
		if !e.spellTimingOK(p, id, f, sorcery) {
			w.record(id, windowKindCard, nonEmpty(e.sorcerySpeedReason(p), wrAbilityUnsupported))
			continue
		}
		if sa := f.SpellAbility(); sa != nil && !e.castTargetsAvailable(p, id, sa) {
			w.record(id, windowKindCard, wrTargetNoLegalTarget)
			continue
		}
		w.record(id, windowKindCard, wrCostInsufficientMana)
	}
}

// classifyGraveyardAndExile covers a card the asked seat owns in a public
// zone. It classifies only cards whose controller is p (an opponent's
// graveyard card is a hidden owned candidate and never produces an entry)
// and only a card an active may-play grant could offer; an ungranted card
// fails closed.
func (e *Engine) classifyGraveyardAndExile(p state.PlayerID, w *windowCollector) {
	for _, z := range []state.Zone{state.ZGraveyard, state.ZExile} {
		for _, id := range e.G.Zone(z, p) {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil || o.Controller != p {
				continue
			}
			if _, ok := e.mayPlayGrantScoped(p, id, e.mayPlayBoardGrantsOpen(p)); !ok {
				continue
			}
			// The card is offered through the may-play walk; classify the
			// timing/target/cost gates it shares with the hand walk.
			e.classifyMayPlayCard(p, id, o, w)
		}
	}
}

func (e *Engine) classifyMayPlayCard(p state.PlayerID, id state.ObjID, o *state.Object, w *windowCollector) {
	f := o.Face()
	if f.IsLand() {
		if e.G.Players[p].LandsPlayed >= int32(1+e.adjustLandPlays(p)) {
			w.record(id, windowKindLand, wrLandDropExhausted)
		} else if !e.sorcerySpeed(p) {
			w.record(id, windowKindLand, nonEmpty(e.sorcerySpeedReason(p), wrAbilityUnsupported))
		} else {
			w.record(id, windowKindLand, wrLandGrantMissing)
		}
		return
	}
	if e.castRestricted(p, id) || e.castSuppressed(p, id) {
		w.record(id, windowKindCard, wrAbilityUnsupported)
		return
	}
	if !e.spellTimingOK(p, id, f, e.sorcerySpeed(p)) {
		w.record(id, windowKindCard, nonEmpty(e.sorcerySpeedReason(p), wrAbilityUnsupported))
		return
	}
	if sa := f.SpellAbility(); sa != nil && !e.castTargetsAvailable(p, id, sa) {
		w.record(id, windowKindCard, wrTargetNoLegalTarget)
		return
	}
	w.record(id, windowKindCard, wrCostInsufficientMana)
}

// nonEmpty returns s, or fallback when s is empty.
func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
