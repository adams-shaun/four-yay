// MayPlay statics: an S:Mode$ Continuous ... MayPlay$ True line grants the
// affected cards a CR 118.3a permission to be PLAYED from a zone other than
// the hand ("You may play lands from your graveyard", Conduit of Worlds;
// "You may cast CARDNAME from your graveyard", a Card.Self self-grant;
// "You may play lands from the top of your library", Ka-Zar of the Savage
// Land). Playing such a card is the ordinary play action -- a land drop for
// a land, a cast paying the printed mana cost for a spell -- so the grant
// only opens the offer and the cost; every gate the ordinary hand walk
// applies (timing, restrictions, target availability, castable cost) applies
// here too. The grant lives in rules (not effects) because it is consumed by
// legalActions/beginCast, the same places the hand walk and its cast live.
package rules

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// mayPlayGrant reports whether the card id -- sitting in the acting player
// p's graveyard, exile or library -- is granted a "may play this card from
// that zone" permission, and whether the grant makes the play free
// (MayPlayWithoutManaCost$ True). Statics of two shapes grant it:
//
//   - a self-grant: the card's own S: static, evaluated while the card sits
//     in the affected zone ("You may cast CARDNAME from your graveyard",
//     Affected$ Card.Self + AffectedZone$ Graveyard). The static's source is
//     the card itself and its controller is the card's controller.
//   - a battlefield grant: a continuous static on a permanent another card
//     controls (Conduit of Worlds' Affected$ Land.YouOwn +
//     AffectedZone$ Graveyard). Only the static's controller benefits: the
//     Affected$ qualifiers (YouOwn, YouCtrl) resolve against that
//     controller, and a static worded for its controller must never hand the
//     permission to an opponent walking their own zones.
//   - an EFFECT-delivered grant: the ContinuousEffect a `DB$ Effect`
//     MayPlayWithoutManaCost$ static leaves behind (Dauthi Voidwalker,
//     Idol of Endurance). Its free-cast flag rides the MayPlayFree field
//     the effects-side registration set, and only the free shape joins
//     ok -- a plain effect-delivered grant is consumed by the spell-cast
//     walk's own effect arm (mayPlayEffectGrantsCast), never here (see
//     mayPlayGrant's effect-arm comment).
//
// ok and free are collected independently over the WHOLE scan, not from the
// first hit, because Forge splits the two roles across statics:
// MayPlayDontGrantZonePermissions$ True marks a static that only exempts the
// mana cost and does NOT open the zone permission -- so a granting static and
// a free-casting static must be allowed to cooperate (the permission from
// one, the exemption from the other), and a lone DontGrant static grants
// nothing.
//
// Gates the build cannot evaluate fail CLOSED (no grant) -- the conservative
// direction for a "may play" permission, the same discipline the filter
// matcher and MatchesPlayerSpec practise: an unimplemented condition must
// withhold the offer, not widen it. The full fail-closed list is in
// mayPlayStatic.
func (e *Engine) mayPlayGrant(p state.PlayerID, id state.ObjID) (free, ok bool) {
	return e.mayPlayGrantScoped(p, id, true)
}

// mayPlayBoardGrantsOpen reports whether either board-side source of
// mayPlayGrant can contribute for player p: a battlefield Continuous static
// p controls carrying MayPlay$ True (mayPlayStatic answers nothing for any
// other value), or an active effect that is a FREE may-play grant of p's
// (mayPlayEffectFree covers nothing else). When it is false, mayPlayGrant
// is exactly its self-grant half for every card, which a walk asking it per
// card (mayPlaySpellIds) can take through mayPlayGrantScoped.
func (e *Engine) mayPlayBoardGrantsOpen(p state.PlayerID) bool {
	for _, sv := range e.activeStatics("Continuous") {
		if sv.Controller == p && strings.TrimSpace(sv.ParamStr(cards.PKMayPlay)) == "True" {
			return true
		}
	}
	ces := e.active()
	for i := range ces {
		if ces[i].MayPlay && ces[i].MayPlayFree && ces[i].Controller == p {
			return true
		}
	}
	return false
}

// mayPlayGrantScoped is mayPlayGrant with the board-side sources (b) and (c)
// read only when board is true. board false is exact only while
// mayPlayBoardGrantsOpen(p) is false; verify mode (derivedMemoVerify)
// re-runs the full grant and panics on a difference.
func (e *Engine) mayPlayGrantScoped(p state.PlayerID, id state.ObjID, board bool) (free, ok bool) {
	if !board && derivedMemoVerify {
		defer func() {
			if wf, wo := e.mayPlayGrantScoped(p, id, true); wf != free || wo != ok {
				panic(fmt.Sprintf("rules: board-closed may-play grant of obj %d disagrees with the full read", id))
			}
		}()
	}
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return false, false
	}
	// (a) Self-grants: the card's own statics, read off its face wherever
	// the card currently sits (an S: line is part of the face, so a card in
	// the graveyard carries it exactly as it would on the battlefield).
	for _, st := range o.Face().Statics {
		if st.Mode != "Continuous" {
			continue
		}
		_, grants, staticFree, _, _, _ := e.mayPlayStatic(st.Params, id, o.Controller, id)
		if staticFree {
			free = true
		}
		if grants {
			ok = true
		}
	}
	// (a2) A Paradigm keyword is its own free-cast permission while the card
	// sits in exile after resolving (rules/paradigm.go): at its owner's first
	// main phase the may-play walk offers it as a free cast, and casting it
	// moves it back to the stack, whose own spellRestZone returns it to exile.
	// Board-independent, so it is read on both the scoped and full passes.
	if e.paradigmMayPlay(p, o) {
		free = true
		ok = true
	}
	if !board {
		return free, ok
	}
	// (b) Battlefield grants, in activeStatics' deterministic APNAP order.
	for _, sv := range e.activeStatics("Continuous") {
		if sv.Controller != p {
			continue
		}
		_, grants, staticFree, _, _, _ := e.mayPlayStatic(sv.Params, id, sv.Controller, sv.Source)
		if staticFree {
			free = true
		}
		if grants {
			ok = true
		}
	}
	// (c) Effect-delivered grants: the ContinuousEffect a `DB$ Effect`
	// "you may play that card this turn (without paying its mana cost)"
	// leaves behind. The FREE-cast shape (MayPlayWithoutManaCost$ True,
	// Dauthi Voidwalker, Idol of Endurance) rides the MayPlayFree field
	// the effects-side registration set; its free read joins the two
	// static sources above so the offer walk (legal.go's may-play spell
	// walk) and beginCast's may-play case agree on all three sources
	// through this ONE function. Only the free shape widens ok here: a
	// PLAIN effect-delivered grant is consumed by the spell-cast walk's
	// own effect arm (mayPlayEffectGrantsCast), and the zones it covers
	// are already enumerated there -- adding it to ok would widen the
	// land/library scans of mayPlayLandIds beyond the zone set those
	// walks deliberately cover.
	if effectFree, covered := e.mayPlayEffectFree(p, o); covered && effectFree {
		free = true
		ok = true
	}
	return free, ok
}

// mayPlaySpellOffer is one cast offer a may-play permission produces for a
// card: which card and zone, the permission's identity (key/text -- empty
// for an untyped grant), and the permission's own free-cast / RaiseCost$
// riders (populated only for typed permissions; an untyped offer's riders
// are read by the caller's existing aggregate helpers).
type mayPlaySpellOffer struct {
	zone     state.Zone
	id       state.ObjID
	key      string
	text     string
	free     bool
	raise    Cost
	hasRaise bool
	priced   bool
}

// mayPlayPermissions returns every usable printed/self/board MayPlay static
// permission covering card id for player p, in deterministic order: the
// card's own face statics, then battlefield statics in activeStatics' APNAP
// order. board is mayPlayGrantScoped's board switch, so the verify-mode
// scoping stays consistent. An untyped grant collapses to one entry with an
// empty key (its limit is the historical per-card one); a MayPlayText$-typed
// grant is its own entry keyed by (source, label), so an artifact creature
// matching both Muldrotha's Artifact and Creature statics is offered once
// per still-unused permission and the cast can say which one it consumes.
// A static whose own limit is already reached reports applies=false inside
// mayPlayStatic, so it is absent here.
func (e *Engine) mayPlayPermissions(p state.PlayerID, id state.ObjID, board bool) []mayPlaySpellOffer {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return nil
	}
	var out []mayPlaySpellOffer
	typed := map[string]bool{}
	untyped := false
	// ok records one static's permission (or the untyped collapse) after
	// mayPlayStatic's gates. The callers below pass st.Params / sv.Params
	// DIRECTLY so the parameter census traces each argument to a Params map
	// (the same shape mayPlayRaiseCost uses).
	ok := func(text string, source state.ObjID, free bool, raise Cost, hasRaise, priced bool) {
		if text == "" {
			untyped = true
			return
		}
		key := mayPlayPermKey(source, text)
		if typed[key] {
			return
		}
		typed[key] = true
		out = append(out, mayPlaySpellOffer{id: id, key: key, text: text,
			free: free, raise: raise, hasRaise: hasRaise, priced: priced})
	}
	for _, st := range o.Face().Statics {
		if st.Mode != "Continuous" {
			continue
		}
		applies, grants, free, raise, hasRaise, priced := e.mayPlayStatic(st.Params, id, o.Controller, id)
		if !applies || !grants {
			continue
		}
		ok(strings.TrimSpace(st.Params["MayPlayText"]), id, free, raise, hasRaise, priced)
	}
	if board {
		for _, sv := range e.activeStatics("Continuous") {
			if sv.Controller != p {
				continue
			}
			applies, grants, free, raise, hasRaise, priced := e.mayPlayStatic(sv.Params, id, sv.Controller, sv.Source)
			if !applies || !grants {
				continue
			}
			ok(strings.TrimSpace(sv.Params["MayPlayText"]), sv.Source, free, raise, hasRaise, priced)
		}
	}
	if untyped {
		// The untyped entry carries no riders: the caller reads them through
		// the existing mayPlayGrant/mayPlayRaiseCost aggregate, exactly as it
		// did before typed permissions existed.
		out = append([]mayPlaySpellOffer{{id: id}}, out...)
	}
	return out
}

// mayPlayPermFreeRaise reports the free-cast and RaiseCost$ composition of a
// CAST through the may-play permission named by key. The empty key is the
// aggregate read every untyped grant uses (mayPlayGrant / mayPlayRaiseCost);
// a typed key is looked up among the card's matching permissions so the cast
// prices exactly the static it consumed. A stale or unmatched key falls back
// to the aggregate -- the fail-closed direction beginCast already takes for
// a grant that vanished between offer and payment.
func (e *Engine) mayPlayPermFreeRaise(p state.PlayerID, id state.ObjID, key string) (free bool, raise Cost, hasRaise, priced bool) {
	if key != "" {
		for _, off := range e.mayPlayPermissions(p, id, true) {
			if off.key == key {
				return off.free, off.raise, off.hasRaise, off.priced
			}
		}
	}
	free, granted := e.mayPlayGrant(p, id)
	free = free && granted
	raise, hasRaise, priced = e.mayPlayRaiseCost(p, id)
	return free, raise, hasRaise, priced
}

// mayPlayRaiseCost reports the composition of the RaiseCost$ surcharge the
// may-play permission over this card carries -- the cost the permission adds
// on top of the printed cost, in addition to the ordinary cast cost (CR
// 118.3a: Kotis, Sibsig Champion's "by exiling three other cards from your
// graveyard in addition to paying its other costs"). `hasRaise` says a
// granting static carries one at all; `raise` is the accumulated priced
// composition. `ok` is false only when a raise is present but unpriceable
// (the variable forms ParseCost cannot price), in which case the static is
// already withheld by mayPlayStatic and the caller must withhold the offer
// too -- never grant with an uncharged surcharge.
//
// The walk is the SAME two sources and the SAME gate chain the permission
// grant runs (mayPlayStatic), so a static that raises and a static that
// grants cannot drift: the raise and the grant are read through one code
// path. Order is deterministic -- battlefield statics in activeStatics'
// APNAP order, then the card's own face statics -- and every raise is
// accumulated with Cost.Plus.
//
// A LAND walk must withhold a static carrying ANY raise, priced or not
// (legal.go's mayPlayLandIds): a land play is free, so there is no cost site
// that could charge the surcharge, and the widening direction this file
// refuses forbids granting it uncharged. `hasRaise` is the test; `ok` is
// irrelevant there.
func (e *Engine) mayPlayRaiseCost(p state.PlayerID, id state.ObjID) (raise Cost, hasRaise, ok bool) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return Cost{}, false, true
	}
	ok = true
	// Battlefield statics first (activeStatics' deterministic APNAP order),
	// each read through the SAME mayPlayStatic gate chain the grant walk
	// runs. A static mayPlayStatic withheld (unpriceable raise, failed gate)
	// reports has=false or priced=false and is handled below; the direct
	// field-selector arguments keep the census's static-param attribution
	// (a bare local map variable would be unclassifiable).
	for _, sv := range e.activeStatics("Continuous") {
		if sv.Controller != p {
			continue
		}
		_, _, _, r, has, priced := e.mayPlayStatic(sv.Params, id, sv.Controller, sv.Source)
		if !has {
			continue
		}
		hasRaise = true
		if !priced {
			ok = false
			continue
		}
		raise = raise.Plus(r)
	}
	for _, st := range o.Face().Statics {
		if st.Mode != "Continuous" {
			continue
		}
		_, _, _, r, has, priced := e.mayPlayStatic(st.Params, id, o.Controller, id)
		if !has {
			continue
		}
		hasRaise = true
		if !priced {
			ok = false
			continue
		}
		raise = raise.Plus(r)
	}
	return raise, hasRaise, ok
}

// mayPlayUnreadGates are the gating parameters a MayPlay$ static can carry
// that this build neither implements nor can safely ignore. Each one either
// further conditions the permission (the residual SVar condition family
// CheckSecondSVar$/CheckThirdSVar$/PresentCompare$, ValidSA$,
// ActivationZone$) -- and an unconditional gate is the widening this file
// refuses: an offer the engine cannot evaluate must not exist at all. Any of
// these present fails the static closed, so the card is simply not offered.
// CheckSVar$/SVarCompare$ LEFT this family (mayPlayConditionGateHolds now
// evaluates them through the shared statics evaluator, exactly as
// continuousGateHolds does for a generic Continuous static), which is what
// lets Verge Rangers' opponent-ahead gate open its top-of-library permission.
// MayPlayAltManaCost$ is NOT in this list: it is
// genuinely consumed, but on a different path -- mayPlayAltCosts delivers it
// for the ordinary cast walk (alternativeCosts), while the ZONE-permission
// grant below still refuses a cost-carrying static that would have the
// may-play cast pay the printed or free cost (the altCostWithholding check
// in mayPlayStatic). RaiseCost$ left this list: it is now genuinely CONSUMED
// -- mayPlayStatic parses it, mayPlayRaiseCost surfaces it, and both cast
// cost sites compose it -- so it is no longer a recognition but a read.
// Only a raise ParseCost cannot price still fails the static closed (the
// priceability check in mayPlayStatic), never an uncharged surcharge.
var mayPlayUnreadGates = [...]string{
	"CheckSecondSVar", "CheckThirdSVar",
	"PresentCompare", "ValidSA", "ActivationZone", "CharacteristicDefining",
}

// mayPlayGateRejected reports whether a MayPlay$ static carries one of the
// gates this build cannot evaluate -- the mayPlayUnreadGates family plus
// MayPlayPlayer$ (a beneficiary other than the static's controller:
// ActivePlayer, CardOwner, Exiler, Player). The static is withheld whole --
// withholding the offer is the conservative direction; both families are
// named in the AGENTS.md audit. CheckSVar$/SVarCompare$ are NOT recognised
// here any more: mayPlayConditionGateHolds evaluates them (fail closed on an
// unreadable body) on every path this function guards.
//
// The literal per-key reads keep the parameter scan classifiable, but they
// are a RECOGNITION, not a consumption: the static never grants, so the key
// stays unread in the parameter census's sense. The census scopes these
// reads away from every card-side label (apiSpecificRulesStat in
// paramcensus_test.go names the functions it excludes), so a MayPlay static
// carrying one of these keys keeps its unread-param label instead of the
// recognition masking it -- Evendo Brushrazer's CheckSVar$, for example, is
// still reported unread because its may-play is withheld entirely, while its
// Condition$ PlayerTurn (which mayPlayStatic genuinely evaluates on the
// family) is not.
//
// RaiseCost$ is NOT read here any more: it is a genuine consumption (see
// mayPlayRaiseCost), so reading it here would be the wrong classification.
// The function is shared with mayPlayAltCosts, which is safe because the
// removal only stops the shared gate from rejecting a RaiseCost$ static on
// the alt-cost path too -- measured 0 corpus statics carry both
// MayPlayAltManaCost$ and RaiseCost$, so no alternative cost moves.
func mayPlayGateRejected(params map[string]string) bool {
	// ValidSA$ shapes the walk can classify stay live: the mutate-cast
	// permission (Brokkos, Apex of Forever's `ValidSA$ Spell.Mutate` -- "You
	// may cast CARDNAME from your graveyard using its mutate ability", CR
	// 903.3d) is read by mayPlayKinds, which splits the permission into its
	// plain and mutate cast halves, and the blitz-cast permission (Sabin,
	// Master Monk's `ValidSA$ Spell.Blitz` -- "You may cast CARDNAME from its
	// graveyard using its blitz ability", CR 702.152a) is read as the blitz
	// half. The exact `Spell.Mutate` token grants ONLY the mutate cast --
	// mayPlayKinds' plain half runs spellMatchesValidSA, which fails closed on
	// the non-Self constraint -- and the exact `Spell.Blitz` token likewise
	// grants only the blitz half (the plain half's spellMatchesValidSA rejects
	// it too), so admitting them here widens nothing. Every other ValidSA$
	// value (Spell.Warp, Spell.Bestow, the bare Spell, ...) keeps the
	// unread-gate fail-closed behaviour measured on the corpus.
	if sa := strings.TrimSpace(params["ValidSA"]); sa != "" && !spellValidSAIsClassified(sa) {
		return true
	}
	return mayPlayGateRejectedOther(params)
}

// mayPlayGateRejectedOther is mayPlayGateRejected's remaining unread-gate
// family, with ValidSA$ handled by the caller (the mutate-cast token is the
// one shape the may-play walk classifies; see mayPlayKinds).
func mayPlayGateRejectedOther(params map[string]string) bool {
	if strings.TrimSpace(params["CheckSecondSVar"]) != "" ||
		strings.TrimSpace(params["CheckThirdSVar"]) != "" ||
		strings.TrimSpace(params["PresentCompare"]) != "" ||
		strings.TrimSpace(params["ActivationZone"]) != "" ||
		strings.TrimSpace(params["CharacteristicDefining"]) != "" {
		return true
	}
	return strings.TrimSpace(params["MayPlayPlayer"]) != ""
}

// mayPlayConditionGateHolds evaluates a MayPlay$ static's CheckSVar$ /
// SVarCompare$ intervening-if through the ONE shared statics evaluator
// (Engine.checkSVarHolds -> effects.CheckSVarHolds), the same machinery
// rules/layers.go's continuousGateHolds runs for every generic Mode$
// Continuous static. A static whose gate body this build cannot read fails
// CLOSED -- the statics convention -- so an unmodelled count head still
// withholds the permission rather than widening it.
//
// Every may-play path must run this before honouring a static: mayPlayStatic
// (the zone-permission/raise-cost grant) and mayPlayAltCosts (the priced
// alternative) both call it, which is why CheckSVar$/SVarCompare$ no longer
// belong in mayPlayGateRejected -- removing them there without evaluating
// them here would widen every CheckSVar-gated static on whichever path
// forgot.
func (e *Engine) mayPlayConditionGateHolds(params map[string]string, source state.ObjID, you state.PlayerID) bool {
	if !e.classBandGateHolds(params["ClassBand"], source) {
		return false
	}
	ck := strings.TrimSpace(params["CheckSVar"])
	cmpRaw := strings.TrimSpace(params["SVarCompare"])
	if ck == "" {
		// A bare SVarCompare$ with no CheckSVar$ is malformed (nothing to
		// compare): refuse it rather than let it through unconditioned.
		return cmpRaw == ""
	}
	return e.checkSVarHolds(staticView{Source: source, Controller: you, Params: params})
}

// mayPlayStatic evaluates one MayPlay$ static's parameters against the card
// id (in its current zone), with `you` the player whose YouOwn/YouCtrl the
// spec's qualifiers resolve against and `source` the static's source object.
// Three booleans are reported independently:
//
//   - applies: the static covers this card at all (its gates pass, its
//     Affected$/AffectedZone$ match). A DontGrant static that applies is
//     still only a cost exemption candidate.
//   - grants: applies AND the static opens the zone permission itself --
//     MayPlay$ True and not MayPlayDontGrantZonePermissions$ True.
//   - free: applies AND MayPlayWithoutManaCost$ True.
//
// The last three report the static's RaiseCost$ surcharge (the cost the
// permission adds on top of the printed cost, CR 118.3a: "by exiling three
// other cards ... in addition to paying its other costs"): `hasRaise` says
// the static carries one, `raise` is its parsed composition, and `priced`
// says ParseCost could price it. A static whose gates all pass but whose
// RaiseCost$ is unpriceable fails CLOSED -- applies stays false, the whole
// static is withheld rather than granted with an uncharged surcharge, the
// same discipline the unread gates practise. `priced` is true whenever
// !hasRaise, so a caller must test `hasRaise && !priced` to detect the
// withheld-whole case; a rejected static always reports priced=false.
func (e *Engine) mayPlayStatic(params map[string]string, id state.ObjID, you state.PlayerID, source state.ObjID) (applies, grants, free bool, raise Cost, hasRaise, priced bool) {
	if strings.TrimSpace(params["MayPlay"]) != "True" {
		// "MayPlay$ You" (1 corpus line) and any other value: this build
		// implements the plain permission, nothing else.
		return false, false, false, Cost{}, false, false
	}
	// Fail closed on gates this build cannot evaluate: the
	// mayPlayUnreadGates family plus MayPlayPlayer$ (see
	// mayPlayGateRejected's doc -- a recognition, not a consumption).
	if mayPlayGateRejected(params) {
		return false, false, false, Cost{}, false, false
	}
	// CheckSVar$/SVarCompare$ ("as long as an opponent controls more lands
	// than you", Verge Rangers): a condition the grant is gated on, evaluated
	// through the shared statics evaluator (fail closed on an unreadable
	// body). This is the may-play path's counterpart to continuousGateHolds.
	if !e.mayPlayConditionGateHolds(params, source, you) {
		return false, false, false, Cost{}, false, false
	}
	// ValidAfterStack describes the spell's characteristics at announcement.
	// The card is still in its origin zone while the permission is offered,
	// so evaluate it under the derived stack view without moving the object.
	if spec := strings.TrimSpace(params["ValidAfterStack"]); spec != "" {
		sc := e.specCtx(source, you)
		sc.AsStack = true
		if !e.matchesSpec(spec, id, sc) {
			return false, false, false, Cost{}, false, false
		}
	}
	// RaiseCost$ is a genuine consumption now (see mayPlayRaiseCost): parse
	// it here so an unpriceable raise fails the static closed at the ONE
	// place every caller's gate chain runs, never an uncharged surcharge.
	// ParseCost prices supported fixed costs, including RemoveAnyCounter as
	// SubCounter. Variable forms -- a bare X (Risen Executioner) or an
	// announced part (PayLife<X>) -- remain unpriceable here. costAnnouncesCastX
	// is the shared "carries any announced X part" test, so this cannot drift
	// from the announcement machinery.
	if raw := strings.TrimSpace(params["RaiseCost"]); raw != "" {
		hasRaise = true
		raise = ParseCost(raw)
		if len(raise.Unknown) > 0 || costAnnouncesCastX(raise) {
			return false, false, false, Cost{}, true, false
		}
		priced = true
	}
	// altCostWithholding: a static that prices the play itself
	// (MayPlayAltManaCost$) and still OPENS the zone permission would have
	// the may-play cast pay the printed or free cost -- the widening this
	// file refuses, because the may-play cast path cannot charge the
	// alternative. The cost's real delivery is mayPlayAltCosts, which
	// serves the ordinary cast walk (a hand cast pays the alt cost as its
	// own cast option); the zone permission stays closed until that cast
	// path can price it. A MayPlayDontGrantZonePermissions$ static (the
	// corpus's dominant carrier, Darksteel Monolith) never granted here
	// anyway, so this only formalises the boundary for a future
	// grant-and-reprice static.
	if _, alt := params["MayPlayAltManaCost"]; alt &&
		!strings.EqualFold(params["MayPlayDontGrantZonePermissions"], "True") {
		return false, false, false, Cost{}, hasRaise, priced
	}
	// Condition$ (PlayerTurn -- "during each of your turns", Kess, Dissident
	// Mage -- and the ability-word family: Threshold -- Null Summoner,
	// Delirium, Metalcraft, Hellbent, ...): evaluated for the static's
	// controller through the ONE shared Continuous-static evaluator
	// (continuousConditionHolds), which fails closed on any value it cannot
	// read -- the same gate the layer walk applies before it registers the
	// printed grant, so the two may-play sources cannot disagree.
	if strings.TrimSpace(params["Condition"]) != "" &&
		!e.continuousConditionHolds(staticView{Source: source, Controller: you, Params: params}) {
		return false, false, false, Cost{}, hasRaise, priced
	}
	// IsPresent$ ("as long as a Zombie is on the battlefield", Gravecrawler):
	// the static applies only while at least one object on the battlefield
	// matches the spec. A spec nothing on the battlefield satisfies --
	// including one whose predicate this build cannot evaluate (unknown
	// predicates fail closed) -- withholds the static, never widens it.
	if ip := strings.TrimSpace(params["IsPresent"]); ip != "" && !e.mayPlayIsPresent(ip, you, source) {
		return false, false, false, Cost{}, hasRaise, priced
	}
	spec := strings.TrimSpace(params["Affected"])
	if spec == "" {
		// A MayPlay$ static with no Affected$ grants nothing here: Forge's
		// default would be "everything", and granting everything is the
		// widening direction this file refuses.
		return false, false, false, Cost{}, hasRaise, priced
	}
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return false, false, false, Cost{}, hasRaise, priced
	}
	// AffectedZone$ (comma list of zone names): where the affected card must
	// sit. Absent grants every zone -- the corpus always spells the zone
	// out (181 S: MayPlay lines all carry it or default sensibly), so the
	// default only serves a script that omits it.
	if az := strings.TrimSpace(params["AffectedZone"]); az != "" {
		inZone := false
		for part := range strings.SplitSeq(az, ",") {
			if z, known := effects.ZoneFromString(strings.TrimSpace(part)); known && z == o.Zone {
				inZone = true
				break
			}
		}
		if !inZone {
			return false, false, false, Cost{}, hasRaise, priced
		}
	}
	// Affected$ describes a card in AffectedZone$, not necessarily a
	// battlefield permanent. Forge uses Permanent for permanent cards in
	// graveyards (Serra Paragon); keep the battlefield meaning elsewhere.
	if !e.matchesSpecFrom(targetSpecForZone(spec, o.Zone), id, you, source) {
		return false, false, false, Cost{}, hasRaise, priced
	}
	// MayPlayText$ names the permission this static grants (Muldrotha, the
	// Gravetide's six per-permanent-type grants: "you may ... cast a
	// permanent spell of each permanent type from your graveyard"). It is
	// both the option label and the permission's identity: a typed grant's
	// MayPlayLimit$ is enforced once per turn PER STATIC
	// (mayPlayTypedLimitReached), not per card, so Muldrotha allows one
	// creature AND one artifact in the same turn while a second creature is
	// withheld. An untyped grant keeps the historical per-card limit (Kess).
	permissionText := strings.TrimSpace(params["MayPlayText"])
	// MayPlayLimit$ (always the literal 1 in the corpus, 45 S: lines): the
	// granted play is once per turn. An unresolvable value stays unenforced,
	// the same convention activationLimitReached applies to a non-literal
	// ActivationLimit$.
	if raw := strings.TrimSpace(params["MayPlayLimit"]); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			if permissionText != "" {
				if e.mayPlayTypedLimitReached(source, permissionText, n) {
					return false, false, false, Cost{}, hasRaise, priced
				}
			} else if e.mayPlayLimitReached(id, n) {
				return false, false, false, Cost{}, hasRaise, priced
			}
		}
	}
	grants = !strings.EqualFold(params["MayPlayDontGrantZonePermissions"], "True")
	free = strings.EqualFold(params["MayPlayWithoutManaCost"], "True")
	return true, grants, free, raise, hasRaise, priced
}

// isPresent scans the battlefield (Forge's IsPresent$ default zone) for at
// least one object matching spec -- Gravecrawler's `IsPresent$ Zombie.YouCtrl`.
// The spec's controller-relative qualifiers resolve against `you`, Self and
// CARDNAME against `source`, exactly like the Affected$ spec. AliveFrom(0)
// keeps the scan deterministic and empty-safe.
func (e *Engine) mayPlayIsPresent(spec string, you state.PlayerID, source state.ObjID) bool {
	for _, p := range e.G.AliveFrom(0) {
		for _, oid := range e.G.Zone(state.ZBattlefield, p) {
			if e.matchesSpecFrom(spec, oid, you, source) {
				return true
			}
		}
	}
	return false
}

// mayPlayLimitReached reports whether the card has already been played
// `limit` times this turn -- a spell cast (PutOnStack) or moved to the
// battlefield from a non-hand, non-stack zone (a land played from the
// graveyard, exile or the top of the library). The scan mirrors
// activationLimitReached's: walk the log backwards to the last TurnChange.
// A land returned to the battlefield by a non-play effect (a reanimate) also
// matches the MoveZone shape, so the count can overcount for a card that is
// both reanimated and re-played -- narrower, never wider, and noted in the
// AGENTS.md audit.
func (e *Engine) mayPlayLimitReached(id state.ObjID, limit int) bool {
	used := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Obj != id {
			continue
		}
		if ev.Kind == events.PutOnStack ||
			(ev.Kind == events.MoveZone && ev.To == state.ZBattlefield && ev.From != state.ZHand && ev.From != state.ZStack) {
			used++
		}
	}
	return used >= limit
}

// mayPlayPermKey is a MayPlayText$-typed permission's stable identity: the
// granting static's source object plus its MayPlayText$ label. Muldrotha's
// six statics share one source and differ by label, so the pair names
// exactly one permission; two different Muldrothas (or a second card
// granting the same type name) never share a limit. Deterministic and
// replay-stable.
func mayPlayPermKey(source state.ObjID, text string) string {
	return strconv.FormatInt(int64(source), 10) + ":" + text
}

// mayPlayTypedLimitReached reports whether a MayPlayText$-typed permission
// has already been used `limit` times this turn. A cast through such a
// permission stamps a "perm=<key>" token on its pay-time CastInfo (payCast),
// so the scan mirrors mayPlayLimitReached's backward walk to the last
// TurnChange but keys on the permission token rather than the card id. A
// land played through a typed permission emits no CastInfo, so a typed LAND
// permission's count is not tracked here; the once-per-turn land drop
// (LandsPlayed) bounds it instead (Muldrotha's Land static).
func (e *Engine) mayPlayTypedLimitReached(source state.ObjID, text string, limit int) bool {
	token := "perm=" + mayPlayPermKey(source, text)
	used := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind != events.CastInfo || ev.Counter == "" {
			continue
		}
		if strings.Contains(ev.Counter, token) {
			used++
		}
	}
	return used >= limit
}

// mayPlayAltCosts delivers a MayPlay static's MayPlayAltManaCost$ as an
// alternative cost for the ORDINARY cast walk (rules/statics.go's
// alternativeCosts appends these after its AlternativeCost statics): "Once
// each turn, you may pay {0} rather than pay the mana cost for a colorless
// spell that you cast from your hand" (Darksteel Monolith's
// MayPlayAltManaCost$ 0). The static's own gate chain decides whether the
// affected card is offered the alternative at all, exactly like the zone
// permission's gates decide whether the card may play from elsewhere --
// same gates, different delivery.
//
// Sources are the same two the permission grant walks: battlefield
// continuous statics (activeStatics, APNAP order) and the card's own face
// statics (a self-referential S: line -- activeStatics alone never sees a
// static on a card still in hand). Order is deterministic and STABLE
// between the offer walk (legal.go's alternativeCosts loop) and beginCast's
// re-resolution of the chosen AltCostIndex, because both call this function
// over the same board.
//
// Fail-closed discipline, the family's own:
//
//   - MayPlay$ True is required (the corpus pairs every MayPlayAltManaCost$
//     with MayPlay$ True, 27 of 27 carrier files at the corpus pin).
//   - the remaining mayPlayUnreadGates plus CheckSVar$/MayPlayPlayer$
//     withhold the static whole (mayPlayGateRejected).
//   - Condition$, IsPresent$, Affected$, AffectedZone$ and MayPlayLimit$
//     are evaluated exactly as mayPlayStatic evaluates them.
//   - the value is priced through altCostParse: the dynamic
//     ConvertedManaCost token (Valgavoth, Terror Eater's
//     MayPlayAltManaCost$ PayLife<ConvertedManaCost>, Bolass' Citadel and
//     the 11 sibling may-play statics) substitutes the cast card's mana
//     value, and a token the substitution leaves unpriceable still has
//     Cost.Unknown non-empty and the alternative is NOT offered -- an
//     unpriceable cost must never exist as an option.
//
// MayPlayLimit$ is enforced per AFFECTED CARD (mayPlayLimitReached's
// per-card log walk), the same reading the zone-permission family applies:
// a colorless spell already played this turn cannot use the discount again.
// The Monolith's printed "Once each turn" is a per-STATIC limit, which this
// per-card tracking under-enforces (two different colorless spells can each
// use it once in one turn) -- the same under-enforcement Forge's per-card
// MayPlayTurn carries; named in the deck import report's Issues.
func (e *Engine) mayPlayAltCosts(p state.PlayerID, id state.ObjID) []Cost {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return nil
	}
	var out []Cost
	// The two sources the permission grant walks, scanned in one
	// deterministic order (battlefield statics first, then the card's own
	// face statics). Each is ranged in place -- neither list is appended to
	// -- so no merged copy is built per call.
	bf := e.activeStatics("Continuous")
	face := o.Face().Statics
	for i, n := 0, len(bf)+len(face); i < n; i++ {
		var sv staticView
		if i < len(bf) {
			sv = bf[i]
		} else if st := face[i-len(bf)]; st.Mode == "Continuous" {
			sv = staticView{Params: st.Params, PS: st.ParamSetOf(), Source: id, Controller: o.Controller}
		} else {
			continue
		}
		raw := strings.TrimSpace(sv.ParamStr(cards.PKMayPlayAltManaCost))
		// MayPlayWithoutManaCost$ True over a card in HAND (Omniscience's
		// "You may cast spells from your hand without paying their mana
		// costs", Fires of Invention, Dracogenesis): casting without paying
		// the mana cost is itself an alternative cost (CR 118.9), and the
		// hand cast walk -- this function's only consumer zone -- is where
		// it is delivered, as a free alternative. The may-play zone walks
		// cover graveyard, exile and library and price their own free
		// shape (mayPlayGrant), so the hand gate keeps the two from ever
		// offering the same free cast twice.
		free := raw == "" && o.Zone == state.ZHand &&
			strings.EqualFold(strings.TrimSpace(sv.Params["MayPlayWithoutManaCost"]), "True")
		if (raw == "" && !free) || strings.TrimSpace(sv.Params["MayPlay"]) != "True" || mayPlayGateRejected(sv.Params) ||
			!e.mayPlayConditionGateHolds(sv.Params, sv.Source, sv.Controller) {
			continue
		}
		// A battlefield static's permission is its controller's alone (the
		// mayPlayGrant rule): MayPlayPlayer$ is rejected above, so no
		// static here grants another player anything, and an Affected$ with
		// no YouOwn/YouCtrl qualifier (Fires of Invention's
		// Card.nonLand+cmcLEX) must not reach an opponent's hand.
		if i < len(bf) && sv.Controller != p {
			continue
		}
		// Condition$ PlayerTurn ("during each of your turns"): the static's
		// controller's turn, the same switch mayPlayStatic runs. Any other
		// value is an unimplemented gate and fails closed.
		switch cond := strings.TrimSpace(sv.Params["Condition"]); cond {
		case "":
		case "PlayerTurn":
			if e.G.Active != sv.Controller {
				continue
			}
		default:
			continue
		}
		if ip := strings.TrimSpace(sv.Params["IsPresent"]); ip != "" && !e.mayPlayIsPresent(ip, sv.Controller, sv.Source) {
			continue
		}
		spec := strings.TrimSpace(sv.Params["Affected"])
		if spec == "" {
			continue
		}
		if az := strings.TrimSpace(sv.Params["AffectedZone"]); az != "" {
			inZone := false
			for part := range strings.SplitSeq(az, ",") {
				if z, known := effects.ZoneFromString(strings.TrimSpace(part)); known && z == o.Zone {
					inZone = true
					break
				}
			}
			if !inZone {
				continue
			}
		}
		if !e.matchesSpecFrom(spec, id, sv.Controller, sv.Source) {
			continue
		}
		if rawLimit := strings.TrimSpace(sv.Params["MayPlayLimit"]); rawLimit != "" {
			if n, err := strconv.Atoi(rawLimit); err == nil && n > 0 && e.mayPlayLimitReached(id, n) {
				continue
			}
		}
		if free {
			out = append(out, Cost{})
			continue
		}
		alt, ok := e.altCostParse(id, raw)
		if !ok {
			// An unpriceable alternative (a dynamic token the face read cannot
			// resolve, or a token this build does not model) is withheld,
			// never offered at a wrong price.
			continue
		}
		out = append(out, alt)
	}
	return out
}

// mayPlayKinds classifies the active may-play permissions over card id into the
// cast shapes a permission may name. `plain` is an ordinary cast permission
// (ValidSA$ empty, or a ValidSA$ the ordinary spell matcher accepts); `mutate`
// is Brokkos, Apex of Forever's shape -- `ValidSA$ Spell.Mutate`, "You may cast
// CARDNAME from your graveyard using its mutate ability", which permits ONLY
// the mutate cast; `blitz` is Sabin, Master Monk's shape -- `ValidSA$
// Spell.Blitz`, "You may cast CARDNAME from its graveyard using its blitz
// ability", which permits ONLY the blitz cast (CR 702.152a); `sneak` is Ninja
// Teen's level-3 shape -- `ValidSA$ Spell.Sneak`, "You may cast creature spells
// from your graveyard using their sneak abilities", which permits ONLY the
// sneak cast (CR 702.190a). A permission whose ValidSA$ the ordinary matcher
// fails closed on and names none of the classified tokens grants NEITHER of the
// cast shapes, so the may-play walk offers nothing for it (fail closed, the
// module's standing direction).
//
// The scan is the same two sources mayPlayGrant reads (the card's own face
// statics, then the battlefield Continuous statics its controller holds), so a
// permission cannot be discovered by one walk and not the other. The plain
// half keeps every existing may-play card's behaviour: an unrestricted
// permission yields plain=true, mutate=false, blitz=false, sneak=false.
func (e *Engine) mayPlayKinds(p state.PlayerID, id state.ObjID) (plain, mutate, blitz, sneak bool) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return false, false, false, false
	}
	// The scan is the same two sources mayPlayGrant reads (the card's own face
	// statics, then the battlefield Continuous statics its controller holds),
	// so a permission cannot be discovered by one walk and not the other. The
	// plain half keeps every existing may-play card's behaviour: an
	// unrestricted permission yields plain=true, mutate=false. The body is
	// inlined per source (no shared closure over a params parameter) so the
	// census sees each scan indexing the static's own Params map.
	for _, st := range o.Face().Statics {
		if st.Mode != "Continuous" {
			continue
		}
		if applies, grants, _, _, _, _ := e.mayPlayStatic(st.Params, id, o.Controller, id); applies && grants {
			pl, mu, bl, sn := e.mayPlayValidSAKinds(st.Params["ValidSA"], o.Face(), id, id, p)
			plain = plain || pl
			mutate = mutate || mu
			blitz = blitz || bl
			sneak = sneak || sn
		}
	}
	for _, sv := range e.activeStatics("Continuous") {
		if sv.Controller != p {
			continue
		}
		if applies, grants, _, _, _, _ := e.mayPlayStatic(sv.Params, id, sv.Controller, sv.Source); applies && grants {
			pl, mu, bl, sn := e.mayPlayValidSAKinds(sv.Params["ValidSA"], o.Face(), id, sv.Source, p)
			plain = plain || pl
			mutate = mutate || mu
			blitz = blitz || bl
			sneak = sneak || sn
		}
	}
	// The THIRD source mayPlaySpellIds reads: an EFFECT-delivered grant, the
	// ContinuousEffect a `DB$ Effect` "you may play that card this turn"
	// leaves behind (Atsushi's exile-and-play, Rakdos, Patron of Chaos'
	// MuscleSac, Rashmi's declined free cast). Those two walks must agree on
	// membership or this classifier VETOES a permission it cannot see: a
	// ContinuousEffect carries no ValidSA$ at all (state.ContinuousEffect has
	// no such field -- the grant is delivered as a matched card set, not as a
	// spell-shape predicate), so an effect grant is always the ORDINARY cast
	// permission and can never be the mutate-only one.
	if !plain && e.mayPlayEffectGrantsCast(p, o) {
		plain = true
	}
	// The Paradigm keyword is the FOURTH source mayPlaySpellIds reads
	// (rules/paradigm.go): its self-permission carries no ValidSA$, so it is
	// always the ordinary cast permission, exactly like the effect arm above.
	// Without this the classifier would VETO the Paradigm offer the grant
	// walk just produced.
	if !plain && e.paradigmMayPlay(p, o) {
		plain = true
	}
	return plain, mutate, blitz, sneak
}

// effectGrantMatches reports whether an Effect-delivered may-play grant ce
// covers the card id: the Affects spec (Affected$) plus, when the grant
// carries one, its ValidAfterStack$ derived-stack-view spell characteristic
// filter (Nahiri, Forged in Fury's "You may cast Equipment spells this way").
// It is the ONE per-card gate every effect-delivered grant walk shares -- the
// offer walks (legal.go's mayPlayLandIds/mayPlaySpellIds) and the single-card
// classification paths (mayPlayEffectFree/mayPlayEffectGrantsCast) -- so the
// qualifier is evaluated at exactly the places the Affects filter it extends
// already was, and a new caller cannot forget it. The card is still in its
// origin zone while the permission is offered, so the qualifier is matched
// with SpecContext.AsStack set: the derived override makes a `Spell.<...>`
// filter read the card's printed spell characteristics, the same way
// mayPlayStatic evaluates a printed S: grant's ValidAfterStack$. An
// unsupported value fails closed inside matchesSpec (no grant).
// effectGrantSpecContext is the match context an Effect-delivered may-play
// grant's Affects spec evaluates under: the effect's controller and source,
// its Remembered set and, when the grant snapshotted one at creation, the
// chosen-card set (state.ContinuousEffect.Chosen) a Card.ChosenCard spec
// reads (Strongbox Raider, Chandra, Flameshaper).
func (e *Engine) effectGrantSpecContext(ce *state.ContinuousEffect) effects.SpecContext {
	sc := e.withNames(effects.SpecContext{You: ce.Controller, Source: ce.Source,
		Remembered: rememberedTargets(ce.Remembered), Resolving: true})
	if ce.ChosenBound {
		sc.Chosen = rememberedTargets(ce.Chosen)
		sc.ChosenValid = true
	}
	return sc
}

func (e *Engine) effectGrantMatches(ce *state.ContinuousEffect, id state.ObjID) bool {
	sc := e.effectGrantSpecContext(ce)
	if !e.matchesSpec(ce.Affects, id, sc) {
		return false
	}
	if ce.MayPlayValidAfterStack != "" {
		sc.AsStack = true
		if !e.matchesSpec(ce.MayPlayValidAfterStack, id, sc) {
			return false
		}
	}
	return true
}

// mayPlayEffectFree reports whether an active EFFECT-delivered FREE-cast
// may-play grant (a ContinuousEffect with MayPlay and MayPlayFree set -- the
// MayPlayWithoutManaCost$ True shape) covers card o for player p right now.
// It is the free-cast sibling of mayPlayEffectGrantsCast and runs the SAME
// gates in the same order -- controller, the Condition$ PlayerTurn rider,
// the MayPlayLimit$ cap, the parsed AffectedZone$, and the Affects spec with
// the delivering effect's Remembered set loaded -- so a card the offer walk
// enumerated is never then classified as ungranted or un-free. Only the
// public zones that walk covers (graveyard, exile) can match; the library
// self-grant is a static, never an effect.
func (e *Engine) mayPlayEffectFree(p state.PlayerID, o *state.Object) (free, covered bool) {
	if o.Zone != state.ZGraveyard && o.Zone != state.ZExile {
		return false, false
	}
	limited := lazyMayPlays{e: e, p: p}
	ces := e.active()
	for i := range ces {
		ce := &ces[i]
		if !ce.MayPlay || !ce.MayPlayFree || ce.Controller != p {
			continue
		}
		if ce.MayPlayPlayerTurn && e.G.Active != p {
			continue
		}
		if ce.MayPlayLimit > 0 && int32(limited.count()) >= ce.MayPlayLimit {
			continue
		}
		zones, all, ok := effects.ParseZones(ce.AffectedZone)
		if !ok && !all {
			continue
		}
		if !all && !slices.Contains(zones, o.Zone) {
			continue
		}
		if !e.effectGrantMatches(ce, o.ID) {
			continue
		}
		return true, true
	}
	return false, false
}

// mayPlayEffectGrantsCast reports whether an active EFFECT-delivered may-play
// grant (a ContinuousEffect with MayPlay set) covers card o for player p
// right now. It is the single-card form of the `e.active()` walk at the foot
// of legal.go's mayPlaySpellIds and runs the SAME gates in the same order --
// controller, the Condition$ PlayerTurn rider, the MayPlayLimit$ cap, the
// parsed AffectedZone$, and the Affects spec with the delivering effect's
// Remembered set loaded -- so a card the offer walk enumerated is never then
// classified as ungranted. Only the public zones that walk covers
// (graveyard, exile) can match; the library self-grant is a static, never an
// effect.
func (e *Engine) mayPlayEffectGrantsCast(p state.PlayerID, o *state.Object) bool {
	if o.Zone != state.ZGraveyard && o.Zone != state.ZExile {
		return false
	}
	limited := lazyMayPlays{e: e, p: p}
	ces := e.active()
	for i := range ces {
		ce := &ces[i]
		if !ce.MayPlay || ce.Controller != p {
			continue
		}
		if ce.MayPlayPlayerTurn && e.G.Active != p {
			continue
		}
		if ce.MayPlayLimit > 0 && int32(limited.count()) >= ce.MayPlayLimit {
			continue
		}
		zones, all, ok := effects.ParseZones(ce.AffectedZone)
		if !ok && !all {
			continue
		}
		if !all && !slices.Contains(zones, o.Zone) {
			continue
		}
		if e.effectGrantMatches(ce, o.ID) {
			return true
		}
	}
	return false
}

// mayPlayGrantedBy reports whether a may-play permission HOSTED by host
// covers card id for player p right now -- Forge's MayPlaySource property
// (SpellAbilityProperty: sa.getMayPlay().getHostCard() equals the source),
// read for "spells you cast this way" (Urianger Augurelt's ValidSpell$
// Spell.MayPlaySource reduction, the CastSa Spell.MayPlaySource raises).
// It walks the same three sources mayPlayGrant reads, keeping only the
// permissions whose host is host, under the same gates:
//
//   - the card's own face statics (host == id, a self-grant);
//   - a battlefield Continuous static whose source is host (only its
//     controller benefits, mayPlayGrant's rule);
//   - an Effect-delivered grant (plain or free) whose Source is host, with
//     mayPlayEffectGrantsCast's controller / PlayerTurn / MayPlayLimit$ /
//     AffectedZone$ / Affects gates.
//
// A may-play cast's own record of these hosts (pendingCast.mayPlayHosts,
// captured at beginCast while the card still sat in the granted zone) is
// what the cost chain reads once the card has moved to the stack -- the
// permission's Affects$/ForgetOnMoved$ scope no longer covers it there.
func (e *Engine) mayPlayGrantedBy(p state.PlayerID, id, host state.ObjID) bool {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil || host == 0 {
		return false
	}
	if host == id {
		for _, st := range o.Face().Statics {
			if st.Mode != "Continuous" {
				continue
			}
			if _, grants, _, _, _, _ := e.mayPlayStatic(st.Params, id, o.Controller, id); grants {
				return true
			}
		}
	}
	for _, sv := range e.activeStatics("Continuous") {
		if sv.Controller != p || sv.Source != host {
			continue
		}
		if _, grants, _, _, _, _ := e.mayPlayStatic(sv.Params, id, sv.Controller, sv.Source); grants {
			return true
		}
	}
	if o.Zone != state.ZGraveyard && o.Zone != state.ZExile {
		return false
	}
	limited := lazyMayPlays{e: e, p: p}
	ces := e.active()
	for i := range ces {
		ce := &ces[i]
		if !ce.MayPlay || ce.Source != host || ce.Controller != p {
			continue
		}
		if ce.MayPlayPlayerTurn && e.G.Active != p {
			continue
		}
		if ce.MayPlayLimit > 0 && int32(limited.count()) >= ce.MayPlayLimit {
			continue
		}
		zones, all, ok := effects.ParseZones(ce.AffectedZone)
		if !ok && !all {
			continue
		}
		if !all && !slices.Contains(zones, o.Zone) {
			continue
		}
		if e.effectGrantMatches(ce, o.ID) {
			return true
		}
	}
	return false
}

// mayPlayHostsCovering returns, in activeStatics/active() order, the hosts of
// every may-play permission covering id for p (mayPlayGrantedBy's three
// sources). beginCast records it for a "mayplay" cast so the cost chain can
// still name the permission's host after CR 601.2a moves the card.
func (e *Engine) mayPlayHostsCovering(p state.PlayerID, id state.ObjID) []state.ObjID {
	var out []state.ObjID
	seen := func(h state.ObjID) bool { return slices.Contains(out, h) }
	if e.mayPlayGrantedBy(p, id, id) {
		out = append(out, id)
	}
	for _, sv := range e.activeStatics("Continuous") {
		if sv.Controller == p && !seen(sv.Source) && e.mayPlayGrantedBy(p, id, sv.Source) {
			out = append(out, sv.Source)
		}
	}
	ces := e.active()
	for i := range ces {
		if ces[i].MayPlay && !seen(ces[i].Source) && e.mayPlayGrantedBy(p, id, ces[i].Source) {
			out = append(out, ces[i].Source)
		}
	}
	return out
}

// castRidesMayPlayOf reports whether the cast being priced under scope is a
// may-play cast whose permission host is host (Forge's MayPlaySource). Only
// the "mayplay" cast mode rides a may-play permission. The pending cast of id
// answers from the hosts it recorded at beginCast; the pre-cast offer walk
// reads the live permissions.
func (e *Engine) castRidesMayPlayOf(p state.PlayerID, id, host state.ObjID, scope costScope) bool {
	if scope.kind != "Spell" || scope.mode != "mayplay" {
		return false
	}
	if pc := e.cast; pc != nil && pc.card == id && pc.mayPlayHostsSet {
		return slices.Contains(pc.mayPlayHosts, host)
	}
	return e.mayPlayGrantedBy(p, id, host)
}

// mayPlayValidSAKinds splits one may-play permission's ValidSA$ into its
// ordinary-cast, mutate-cast, blitz-cast and sneak-cast halves. An
// absent/empty value is an ordinary permission. The mutate, blitz and sneak
// tokens are matched case-insensitively as whole alternatives, never as
// substrings, so a future `Spell.Mutates`-style token cannot be misread as
// the cast permission.
func (e *Engine) mayPlayValidSAKinds(validSA string, f *cards.Face, id, source state.ObjID, you state.PlayerID) (plain, mutate, blitz, sneak bool) {
	raw := strings.TrimSpace(validSA)
	if raw == "" {
		return true, false, false, false
	}
	// A nil target list is deliberate: a may-play permission is evaluated
	// before any target is announced, so a target-conditional
	// `Spell.IsTargeting` alternative stays fail-closed here. A may-play
	// permission must not become an unconditional instant-speed grant merely
	// because the card has some legal target (the CastWithFlash offer path is
	// where prospective targets are read).
	if e.spellMatchesValidSA(f, raw, id, source, you, nil) {
		plain = true
	}
	for alt := range strings.SplitSeq(raw, ",") {
		switch {
		case strings.EqualFold(strings.TrimSpace(alt), "Spell.Mutate"):
			mutate = true
		case strings.EqualFold(strings.TrimSpace(alt), "Spell.Blitz"):
			blitz = true
		case strings.EqualFold(strings.TrimSpace(alt), "Spell.Sneak"):
			sneak = true
		}
	}
	return plain, mutate, blitz, sneak
}

// spellValidSAIsClassified reports whether a ValidSA$ value names ONLY the
// tokens mayPlayValidSAKinds classifies (Spell.Mutate, Spell.Blitz,
// Spell.Sneak). Any other token keeps mayPlayGateRejected's fail-closed
// behaviour: the static is withheld whole rather than offered a cast shape the
// walk cannot price. The whole-alternative match mirrors mayPlayValidSAKinds
// exactly, so the gate and the classifier cannot disagree about what they
// admit.
func spellValidSAIsClassified(validSA string) bool {
	for alt := range strings.SplitSeq(validSA, ",") {
		tok := strings.TrimSpace(alt)
		if tok == "" {
			continue
		}
		if !strings.EqualFold(tok, "Spell.Mutate") && !strings.EqualFold(tok, "Spell.Blitz") &&
			!strings.EqualFold(tok, "Spell.Sneak") {
			return false
		}
	}
	return true
}
