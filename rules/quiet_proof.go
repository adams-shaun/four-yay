package rules

import (
	"os"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// The quiet-seat proof (design
// docs/superpowers/specs/2026-10-08-quiet-seat-walk-skip-design.md, §2).
//
// seatQuiet(p) reports that the posed-priority legal-action walk would offer
// p nothing but the mana section's "activate" options (plus pass and concede,
// which are not part of the claim). It never computes an option; it only
// shows none can exist. false is always safe -- any window it cannot prove
// runs the walk exactly as today, so the offered option lists cannot change.
//
// The proof is a conjunction of blockers (§2.3): it returns the FIRST one it
// hits. Every blocker over-approximates a walk section's "may offer"; it
// never decides whether the offer is real. See quiet_stats.go for the
// counters and quiet_facts.go for the per-face facts it reads.

// quietBlockerID names the first blocker that fires for a window. qbNone
// means the seat is quiet.
type quietBlockerID uint8

const (
	qbNone               quietBlockerID = iota
	qbNoHost                            // no engine/game/log, or inside an active() build
	qbBoardGrant                        // hasGrants or an active AddAbility$ carrier
	qbBoardFlash                        // an active CastWithFlash static
	qbBoardMayPlay                      // an active may-play / mayhem-play / plot-zone grant
	qbBoardCostStatic                   // an active ReduceCost or SetCost static
	qbBoardCostGrant                    // an active granted/Effect-delivered cost static (bound hosts)
	qbBoardKeyword                      // an active AddKeyword head that grants an offer or Flash
	qbRestrictedMana                    // the seat holds restricted mana
	qbHandLand                          // a hand land play (or a keyword action) is open
	qbHandSpell                         // a hand card may be cast
	qbHandAbility                       // a hand card may be activated
	qbMayhemLand                        // a graveyard land may be played
	qbExileRoute                        // a card in exile may be recast
	qbCommand                           // a card in the command zone may be cast
	qbGraveRoute                        // a graveyard card may be recast
	qbBattlefieldAbility                // a printed/granted ability may be activated
	qbBattlefieldStack                  // a stack-zone ability hit
	qbBattlefieldFace                   // station / room unlock / face up / specialize may offer
	qbBadObject                         // fail-closed: facts absent, merged pile or face down
	qbCount
)

// quietBlockerNames are the blocker names for the -quietstats table.
var quietBlockerNames = [qbCount]string{
	qbNone:               "none",
	qbNoHost:             "no host / active build",
	qbBoardGrant:         "AddAbility$/grant static",
	qbBoardFlash:         "CastWithFlash static",
	qbBoardMayPlay:       "may-play / plot-zone grant",
	qbBoardCostStatic:    "ReduceCost/SetCost static",
	qbBoardCostGrant:     "granted cost static",
	qbBoardKeyword:       "AddKeyword grant",
	qbRestrictedMana:     "restricted mana",
	qbHandLand:           "hand land/keyword action",
	qbHandSpell:          "affordable hand spell",
	qbHandAbility:        "hand ability",
	qbMayhemLand:         "mayhem land play",
	qbExileRoute:         "exile recast route",
	qbCommand:            "command zone",
	qbGraveRoute:         "graveyard recast route",
	qbBattlefieldAbility: "battlefield ability",
	qbBattlefieldStack:   "stack-zone ability",
	qbBattlefieldFace:    "station/unlock/face-up/specialize",
	qbBadObject:          "unprovable object",
}

// quietCoveredSections names every call the walk's two section entry points
// make on the walk object, so TestQuietCoversWalkSections can fail when a new
// walk section is added without a corresponding blocker. Each entry is either
// a section whose "may offer" a §2.3 blocker over-approximates, or a helper
// that only narrows the same offer and is covered by the section's blocker.
var quietCoveredSections = map[string]bool{
	// legalActionsWalkWithWindow's section entry points (§1.2).
	"handWalk":           true,
	"mayPlayLandWalk":    true,
	"mayhemLandWalk":     true,
	"mayPlaySpellWalk":   true,
	"plotZoneWalk":       true,
	"commandZoneWalk":    true,
	"graveyardCastsWalk": true,
	"exileCastsWalk":     true,
	"battlefieldWalk":    true,
	// battlefieldWalk's helpers: the mana section is served (never a
	// blocker), the ability-class/mana gates narrow the ability loop, and the
	// reuse/verify helpers are records of the same walk.
	"abilityRestricted":         true,
	"boardFacts":                true,
	"manaLTypeBlockMay":         true,
	"manaLabel":                 true,
	"manaWalkEmpty":             true,
	"ownManaMembers":            true,
	"pileAbilitiesEmpty":        true,
	"recordManaSection":         true,
	"reuseAbilityBlock":         true,
	"reuseManaSection":          true,
	"verifyAbilityCold":         true,
	"verifyManaCold":            true,
	"offBattlefieldGrantedWalk": true,
	"grantedKeywordLines":       true,
	"offerCastable":             true,
	"recordAbilityBlock":        true,
	"maxSpeedBoastHolds":        true,
	"pricing":                   true,
	// add is the walk's local option-append closure (pass/concede and the
	// sections' own appends); it is covered by the sections that call it.
	"add": true,
}

// quietVerifyOn reports whether the proof runs alongside every walk and
// panics when it says quiet while the walk offered a non-mana option (§4.1).
//
// It is a function, not a package var, because the rules test binary turns
// verify mode on in rules/derivedmemo_verify_test.go's init() by setting the
// in-process derivedMemoVerify; a package-level var initializer runs BEFORE
// that init, so it would read the zero value and the rules test binary would
// never cross-check the proof. Reading derivedMemoVerify at each call keeps
// the test binary and enginebench-verify (derivedMemoVerifyFlag) both live.
//
// The GORGE_QUIET_VERIFY env read is cached at package init: quietStatsEnabled
// calls this on every posed window (the default build must pay one branch),
// and no test sets the env after init. derivedMemoVerify and
// derivedMemoVerifyFlag stay live per call.
func quietVerifyOn() bool {
	return derivedMemoVerify || derivedMemoVerifyFlag != "" || quietVerifyEnv
}

// quietVerifyEnv is the GORGE_QUIET_VERIFY switch, read once at init.
var quietVerifyEnv = os.Getenv("GORGE_QUIET_VERIFY") != ""

// seatQuiet is quietBlocker(p) == qbNone.
func (e *Engine) seatQuiet(p state.PlayerID) bool {
	return e.quietBlocker(p) == qbNone
}

// quietBlocker returns the first blocker §2.3 fires for the window, or
// qbNone. It is a pure read: no event, no mutation, no allocation.
func (e *Engine) quietBlocker(p state.PlayerID) quietBlockerID {
	if e == nil || e.G == nil || e.L == nil || e.compiledText == nil {
		return qbNoHost
	}
	// The board-flag reader needs active() outside a build; inside one the
	// summary is a handed list and the reader must not run. Fail closed.
	if e.activeDepth != 0 || e.G.Active >= state.PlayerID(len(e.G.Players)) || p >= state.PlayerID(len(e.G.Players)) {
		return qbNoHost
	}
	// The mana ceiling is read before the scan; a restricted unit makes it
	// unprovable.
	ceiling, unbounded, restricted := e.quietManaCeiling(p)
	if restricted {
		return qbRestrictedMana
	}
	sorceryOpen := e.G.Active == p && e.G.Step.IsMain() && len(e.G.Stack) == 0

	// Cheapest, highest-signal per-object scans first. The board-flag reader
	// is the most expensive part of the proof (several whole-board static
	// scans); it runs LAST, and only for a window every per-object section
	// left open. The conjunction is order-independent -- every blocker still
	// fires on the windows where it is the operative one -- so this only
	// changes which blocker the stats attribute a window to, never
	// seatQuiet's answer.
	if b := e.quietHandBlocker(p, ceiling, unbounded, sorceryOpen); b != qbNone {
		return b
	}
	if b := e.quietGraveBlocker(p); b != qbNone {
		return b
	}
	if b := e.quietCommandBlocker(p); b != qbNone {
		return b
	}
	if b := e.quietExileBlocker(p); b != qbNone {
		return b
	}
	if b := e.quietGrantedZoneBlocker(p); b != qbNone {
		return b
	}
	if b := e.quietBattlefieldBlocker(p, ceiling, unbounded, sorceryOpen); b != qbNone {
		return b
	}
	if b := e.quietBoardBlocker(p); b != qbNone {
		return b
	}
	return qbNone
}

// quietBoardBlocker reads the board-wide flags of §2.2 once per window.
//
// Deviation from the "no maps, no strings" proof-path rule, stated
// deliberately: two reads here touch a Params map or a string -- the
// MayPlay$ param scan over active Continuous statics and the AddKeyword head
// compare in quietActiveKWGrantBlocker. §2.2's board-flag reader REQUIRES
// them (there is no compiled S2 bit for an arbitrary MayPlay$/AddKeyword
// carrier in v1), and both run on the already-built active() summaries the
// walk shares, not on a raw script re-parse. They are the proof's most
// expensive part, so quietBlocker runs this reader LAST, only for a window
// every per-object section left open (see the ordering note in quietBlocker).
// The no-allocation half of the rule holds: both reads are value copies out
// of existing tables and allocate nothing.
func (e *Engine) quietBoardBlocker(p state.PlayerID) quietBlockerID {
	ces := e.active()
	sum := e.activeSummaryOf(ces)
	if sum.hasGrants || len(e.collectAddAbilityCarriers()) > 0 {
		return qbBoardGrant
	}
	if len(e.activeStatics("CastWithFlash")) > 0 {
		return qbBoardFlash
	}
	if len(e.activeStatics("PlotZone")) > 0 {
		return qbBoardMayPlay
	}
	// Any active may-play grant. mayPlaySpellIds covers the zone-permission
	// family; a Continuous static carrying MayPlay$ True (Omniscience,
	// Conspiracy Unraveler, Fires of Invention) and the effect-delivered
	// copies of the same grant are the "cast from hand for a substituted
	// cost" family mayPlayAltCosts serves. Reading the raw statics is the
	// coarse over-approximation: any such grant can open a hand cast.
	for _, sv := range e.activeStatics("Continuous") {
		if strings.EqualFold(strings.TrimSpace(sv.ParamStr(cards.PKMayPlay)), "True") {
			return qbBoardMayPlay
		}
	}
	for _, ce := range e.active() {
		if ce.MayPlay {
			return qbBoardMayPlay
		}
	}
	for _, h := range e.activeKWHeads {
		if quietActiveKWGrantBlocker(h) {
			return qbBoardKeyword
		}
	}
	if len(e.mayPlaySpellIds(p)) > 0 || len(e.mayPlayLandIds(p)) > 0 || len(e.mayhemLandPlayIds(p)) > 0 {
		return qbBoardMayPlay
	}
	// A self-carried may-play static on a card still in hand is reported by
	// the per-card castOpen classifier, but a board static that grants the
	// hand casts is caught here (above).
	// An active cost-minting ContinuousEffect (appendEffectCostStatics'
	// Effect-delivered, granted and AddKeyword$-Affinity-minted arms) blocks
	// on its own, before the printed-static scan: a granted or bound view's
	// Source is the BOUND HOST, so its ValidCard$ Card.Self does not scope it
	// to a printed face the per-card castOpen classifier reads, and the
	// selfOnly bit quietCostStaticBoardBlocker trusts is about printed faces
	// -- it says nothing about a grant that prices a hand card the classifier
	// never sees (Mycosynth Golem's affinity grant).
	for i := range ces {
		if continuousMintsCostStatic(&ces[i]) {
			return qbBoardCostGrant
		}
	}
	cs := e.collectCostStatics()
	if quietCostStaticBoardBlocker(&cs) {
		return qbBoardCostStatic
	}
	return qbNone
}

// quietCostStaticBoardBlocker reports whether an active reduce/set cost static
// can touch the pricing of SOME card. A self-only static (ValidCard$
// Card.Self) is inert for every object except its own source -- the case the
// per-card castOpen classifier handles -- so it is not a board blocker. A
// raise static only makes a cast dearer, so it cannot open an offer. An
// unmarked view (selfOnly false, e.g. a hand-built snapshot) is never inert,
// so it counts.
func quietCostStaticBoardBlocker(cs *costStaticViews) bool {
	for _, list := range [...][]staticView{cs.reduce, cs.set} {
		for i := range list {
			if !list[i].selfOnly {
				return true
			}
		}
	}
	return false
}

// continuousMintsCostStatic reports whether one active ContinuousEffect
// produces a cost-modifier static view through appendEffectCostStatics: an
// Effect-delivered or granted CostStaticMode (a CostStaticGranted grant
// included), or an AddKeyword$ Affinity grant whose Affected$ names hosts
// other than the grantor (affinityGrantCostStatics' mint shape, mirrored
// allocation-free). It is the coarse over-approximation the proof needs: any
// such effect can price a card the per-face classifier never reads, so the
// window cannot be proved quiet while it is live. It reads no map and
// allocates nothing.
func continuousMintsCostStatic(ce *ContinuousEffect) bool {
	if ce.CostStaticMode != "" {
		switch cards.StaticModeOf(ce.CostStaticMode) {
		case cards.StaticRaiseCost, cards.StaticReduceCost, cards.StaticSetCost:
			return true
		}
		return false
	}
	if len(ce.AddKeywords) == 0 {
		return false
	}
	// affinityGrantCostStatics' mint gate: an Affected$ that is absent or
	// Card.Self names the grantor itself and mints nothing; a non-blank
	// Affinity entry spec mints one bound static.
	if a := strings.TrimSpace(ce.Affects); a == "" || a == "Card.Self" {
		return false
	}
	for _, k := range ce.AddKeywords {
		head, param, _ := strings.Cut(k, ":")
		if !strings.EqualFold(head, "Affinity") {
			continue
		}
		if spec, _, _ := strings.Cut(param, ":"); strings.TrimSpace(spec) != "" {
			return true
		}
	}
	return false
}

// quietActiveKWGrantBlocker reports whether an active AddKeyword$ head can
// open an offer the proof's printed-face classifiers would miss: one of the
// four expanded keyword heads, Flash (which makes a non-instant castable at
// instant speed), or any head in quietCastOpenHeads -- a granted Convoke,
// Delve, Improvise, Offspring, ... reaches the cast spell through the walk's
// DERIVED reads (castOfferBase's hasCastConvoke/hasCastImprovise, the
// blitz/sneak/web-slinging/offspring cost readers), so the per-face castOpen
// classifier, which reads the printed face, would call the window quiet while
// the walk offers a cheaper or alternative-timing cast.
func quietActiveKWGrantBlocker(head string) bool {
	for _, hd := range grantedKWHeads {
		if strings.EqualFold(head, hd.S) {
			return true
		}
	}
	if strings.EqualFold(head, "Flash") {
		return true
	}
	for _, h := range quietCastOpenHeads {
		if strings.EqualFold(head, h) {
			return true
		}
	}
	return false
}

// quietManaCeiling returns the seat's mana ceiling, whether it is unbounded
// (a counted source's amount is indeterminate), and whether the seat holds
// restricted mana (which the proof fails closed on).
func (e *Engine) quietManaCeiling(p state.PlayerID) (int32, bool, bool) {
	if len(e.G.Players[p].RestrictedMana) != 0 {
		return 0, false, true
	}
	var total int32
	pool := &e.G.Players[p].Pool
	for i := range pool {
		total += pool[i]
	}
	unbounded := false
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o == nil || o.Controller != p || o.Tapped || o.PhasedOut || o.FaceDown {
			continue
		}
		if o.Face() == nil {
			// A face-less battlefield object (an ability object; tokens on
			// the battlefield always carry a face) is skipped by the mana
			// section too, so it contributes no ceiling. Not unbounded.
			continue
		}
		ff := e.walkFaceFactsOf(o.Face())
		if ff == nil {
			// A battlefield object with no facts could be a mana source; the
			// ceiling cannot be bounded. Fail closed (unbounded counts every
			// spell as affordable, which blocks).
			unbounded = true
			continue
		}
		qf := ff.quiet
		if qf.manaIndeterminate {
			unbounded = true
		}
		total += qf.manaMax
	}
	return total, unbounded, false
}

// quietHandBlocker handles handWalk: land plays, keyword actions, casts and
// hand abilities. It scans every face of the card, an over-approximation of
// the walk's per-mode alternate-face probes, so a modal/adventure/room/split
// face cannot be missed.
func (e *Engine) quietHandBlocker(p state.PlayerID, ceiling int32, unbounded, sorceryOpen bool) quietBlockerID {
	landOpen := sorceryOpen && e.G.Players[p].LandsPlayed < int32(1+e.adjustLandPlays(p))
	for _, id := range e.G.Zone(state.ZHand, p) {
		o := e.G.Obj(id)
		if o == nil || o.Card == nil {
			return qbBadObject
		}
		if b := e.quietObjectFallback(o); b != qbNone {
			return b
		}
		// Keyword actions offered from hand regardless of the face's own
		// timing: Foretell ({2} on your turn), Plot (sorcery), Suspend
		// ({2} any step of your turn), MayFlashCost (instant timing when the
		// ordinary gate fails), Sneak (the caster's own declare-blockers
		// step, legal_walk_hand.go's above-the-gate offer, gated by the same
		// sneakTimingOK the offer runs) and Teamwork (the flash-timed
		// teamworkFlashOffer, gated by the same stackKeywordPossibleH +
		// activationPhasesOK pair the offer runs). Any of them is a blocker
		// while its own window is open.
		if quietHandKeywordAction(o, id, e, p, sorceryOpen) {
			return qbHandLand
		}
		for _, f := range o.Card.Faces {
			if f == nil {
				continue
			}
			ff := e.walkFaceFactsOf(f)
			if ff == nil {
				return qbBadObject
			}
			qf := ff.quiet
			if qf.isLand {
				if landOpen && quietFaceNotForbidden(e, p, id) {
					return qbHandLand
				}
				// A land printed with Morph/Megamorph/Disguise is also cast
				// face down for {3} (Zoetic Cavern), even after the land drop.
				if quietFaceHasDownCast(f) && quietAffordable(3, ceiling, unbounded) {
					return qbHandSpell
				}
				continue
			}
			timingOpen := qf.instantSpeed || sorceryOpen
			if !timingOpen {
				continue
			}
			if qf.castOpen || quietAffordable(qf.castFloor, ceiling, unbounded) {
				return qbHandSpell
			}
		}
		// Hand abilities: cycling/channel/ninjutsu are granted keyword
		// abilities (kwGranted) plus printed AB abilities with a Hand zone.
		// A printed ability the loop would offer is covered by the abQuiet
		// hand bucket; a granted-expansion head is covered below.
		if quietFaceGrantedHead(o) {
			return qbHandAbility
		}
		if abQuietBlocked(e.quietHandAbQuiet(o), o, ceiling, unbounded, sorceryOpen) {
			return qbHandAbility
		}
	}
	return qbNone
}

// quietHandAbQuiet returns the Hand-zone ability summary for the object's
// live face, or the zero summary when it has none.
func (e *Engine) quietHandAbQuiet(o *state.Object) abQuietZone {
	ff := e.walkFaceFactsOf(o.Face())
	if ff == nil {
		return abQuietZone{}
	}
	return ff.quiet.abQuiet[3]
}

// quietFaceGrantedHead reports whether the object's face carries any of the
// four expanded keyword heads (Cycling, TypeCycling, Saddle, Crew) or any
// dynamic keyword state that could produce a granted ability offer.
func quietFaceGrantedHead(o *state.Object) bool {
	f := o.Face()
	if f == nil {
		return true
	}
	ff := walkFaceFactsOfFace(f)
	if ff != nil && ff.keywordsCurrent(f) {
		if ff.kwGranted {
			return true
		}
	} else if len(f.Keywords) > 0 {
		for _, hd := range grantedKWHeads {
			if f.KeywordLinesHaveHead(hd.S, hd.ID) {
				return true
			}
		}
	}
	return objectGrantedKWMaybe(o, f, ff)
}

// walkFaceFactsOfFace reads f's facts off the published slot only, without
// an engine: quietFaceGrantedHead is called from a per-object scan that may
// not have one. A face another configuration published with current
// keywords is served; else nil (the caller recomputes from the raw lists).
func walkFaceFactsOfFace(f *cards.Face) *walkFaceFacts {
	if f == nil {
		return nil
	}
	if p := f.ExtSlot().Load(); p != nil {
		if ff := (*walkFaceFacts)(p); ff.currentFor(f) {
			return ff
		}
	}
	return nil
}

// quietGrantedZoneBlocker covers mayPlayLandWalk, mayhemLandWalk and
// mayPlaySpellWalk beyond the board flag: the mayhem land play is a
// per-object live fact, and a may-play grant that only an uncapped/uncounted
// static opens is caught by the board flag. This runs the exact mayhem
// enumerator.
func (e *Engine) quietGrantedZoneBlocker(p state.PlayerID) quietBlockerID {
	if len(e.mayhemLandPlayIds(p)) > 0 {
		return qbMayhemLand
	}
	return qbNone
}

// quietExileBlocker covers plotZoneWalk's library half (blocked by the
// PlotZone static board flag), the prepared-copy cast, the adventure recast,
// the foretell/plot casts, the airbend recast and the warp recast.
func (e *Engine) quietExileBlocker(p state.PlayerID) quietBlockerID {
	for _, id := range e.G.Zone(state.ZExile, p) {
		o := e.G.Obj(id)
		if o == nil || o.Card == nil {
			return qbBadObject
		}
		if o.IsToken {
			continue
		}
		if b := e.quietObjectFallback(o); b != qbNone {
			return b
		}
		if o.IsCopy && o.PreparedSource != 0 {
			return qbExileRoute
		}
		if o.FaceIdx == 0 && adventureSpellFace(o) != nil {
			return qbExileRoute
		}
		if o.CastFlags&state.FlagForetold != 0 {
			return qbExileRoute
		}
		if o.PlottedTurn > 0 {
			return qbExileRoute
		}
		if o.SuspendGranted {
			return qbExileRoute
		}
		if e.airbendCastAvailable(id) {
			return qbExileRoute
		}
		if e.quietFaceExileRoute(o.Face()) || e.quietObjectDerivedRoute(id, quietExileDerivedHeads...) {
			return qbExileRoute
		}
	}
	return qbNone
}

// quietFaceExileRoute reports whether a face carries an exile recast keyword.
func (e *Engine) quietFaceExileRoute(f *cards.Face) bool {
	ff := e.walkFaceFactsOf(f)
	if ff == nil {
		return true
	}
	return ff.quiet.exileCastKW
}

// quietCommandBlocker: any object in the command zone is a blocker (the
// command-zone walk only offers commanders in a Commander game, but the
// coarse "any object" is sound).
func (e *Engine) quietCommandBlocker(p state.PlayerID) quietBlockerID {
	if len(e.G.Zone(state.ZCommand, p)) > 0 {
		return qbCommand
	}
	return qbNone
}

// quietGraveBlocker covers graveyardCastsWalk: a graveyard card with any
// recast route.
func (e *Engine) quietGraveBlocker(p state.PlayerID) quietBlockerID {
	for _, id := range e.G.Zone(state.ZGraveyard, p) {
		o := e.G.Obj(id)
		if o == nil || o.Card == nil {
			return qbBadObject
		}
		if b := e.quietObjectFallback(o); b != qbNone {
			return b
		}
		if aftermathAlternateFace(o) != nil {
			return qbGraveRoute
		}
		if e.quietFaceGraveRoute(o.Face()) || e.quietObjectDerivedRoute(id, quietGraveDerivedHeads...) {
			return qbGraveRoute
		}
	}
	return qbNone
}

// quietFaceGraveRoute reports whether a face can open a cast from the
// graveyard. A face with no facts fails closed.
func (e *Engine) quietFaceGraveRoute(f *cards.Face) bool {
	ff := e.walkFaceFactsOf(f)
	if ff == nil {
		return true
	}
	return ff.quiet.recastKW
}

// quietObjectDerivedRoute reports whether id could carry any of heads on its
// DERIVED keyword list (printed plus layer-6 granted). mayHaveDerivedKeywordAnyH
// is the cheap necessary condition hasKeywordH and derivedKeywordParamH
// themselves start with, so a false answer means the walk's own derived reads
// cannot see the head either -- exactly the over-approximation the proof
// needs. It covers the printed lines, intrinsic keywords, keyword counters,
// the status flags and any active AddKeyword$ grant, so a layer-6 grant the
// printed face lacks (Snapcaster Mage's Flashback, Underworld Breach's
// Escape, Dream Devourer's Foretell) cannot slip past a proof that read only
// the printed face.
func (e *Engine) quietObjectDerivedRoute(id state.ObjID, heads ...kwHead) bool {
	return e.mayHaveDerivedKeywordAnyH(id, heads...)
}

// quietBattlefieldBlocker covers battlefieldWalk: the mana section is never a
// blocker (§3.3 serves it), the ability loop and the special-action sections.
func (e *Engine) quietBattlefieldBlocker(p state.PlayerID, ceiling int32, unbounded, sorceryOpen bool) quietBlockerID {
	pn := len(e.G.Players)
	for _, z := range [...]state.Zone{state.ZBattlefield, state.ZStack, state.ZGraveyard, state.ZHand, state.ZExile} {
		zidx := quietZoneIndex(z)
		zonePlayers := pn
		if z == state.ZStack {
			zonePlayers = 1
		}
		for seat := 0; seat < zonePlayers; seat++ {
			q := state.PlayerID(seat)
			if z == state.ZStack {
				q = 0
			}
			for _, id := range e.G.Zone(z, q) {
				o := e.G.Obj(id)
				if o == nil {
					return qbBadObject
				}
				if o.Face() == nil {
					// The walk's ability loop skips an object with no face
					// (an ability object waiting on the stack, a ceased
					// token): it offers nothing for it, so there is nothing
					// to block (rules/legal_walk_battlefield.go's `f :=
					// o.Face(); if f == nil { continue }`).
					continue
				}
				if z == state.ZBattlefield && !existsOnBattlefieldQuiet(o) {
					// A phased-out permanent is treated as though it does not
					// exist (CR 702.25b).
					continue
				}
				if b := e.quietObjectFallback(o); b != qbNone {
					return b
				}
				ctl := e.controllerOf(id)
				owner := o.Owner
				// A granted/CastWithFlash board already blocked. An object
				// that can carry a dynamic granted keyword must block.
				if quietFaceGrantedHead(o) || len(o.IntrinsicKeywords) > 0 && quietDynamicKWMaybe(o) {
					if ctl == p || owner == p || z == state.ZStack {
						return qbBattlefieldAbility
					}
				}
				ff := e.walkFaceFactsOf(o.Face())
				if ff == nil {
					return qbBadObject
				}
				mask := ff.abZones
				if ctl != p {
					mask = ff.abZonesActivator
				}
				if !zoneBit(mask, z) {
					continue
				}
				if z == state.ZStack {
					return qbBattlefieldStack
				}
				if zidx >= 0 {
					aq := ff.quiet.abQuiet[zidx]
					if abQuietBlocked(aq, o, ceiling, unbounded, sorceryOpen) {
						return qbBattlefieldAbility
					}
				}
			}
		}
	}
	// Special-action sections.
	if b := e.quietBattlefieldFaceBlocker(p, sorceryOpen); b != qbNone {
		return b
	}
	return qbNone
}

// quietBattlefieldFaceBlocker covers Station (sorcery), Room unlock (sorcery),
// turn face up (any timing) and Specialize (sorcery).
func (e *Engine) quietBattlefieldFaceBlocker(p state.PlayerID, sorceryOpen bool) quietBlockerID {
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o == nil || o.Card == nil {
			// The walk's face sections skip an object with no card (the
			// specialize loop's own gate); there is nothing to block.
			continue
		}
		if !existsOnBattlefieldQuiet(o) {
			continue
		}
		if o.FaceDown {
			// Turn face up (CR 708.6) is offered any time its controller has
			// priority, so it is a blocker outside the sorcery block.
			return qbBattlefieldFace
		}
		if o.Card.AlternateMode == "Specialize" && o.FaceIdx == 0 {
			return qbBattlefieldFace
		}
		for _, f := range o.Card.Faces {
			if f == nil {
				continue
			}
			if f.IsRoom() {
				return qbBattlefieldFace
			}
			if f.KeywordLinesHaveHead(kwhStation.S, kwhStation.ID) {
				return qbBattlefieldFace
			}
		}
	}
	return qbNone
}

// existsOnBattlefieldQuiet is pay.ExistsOnBattlefield without importing pay
// here: a non-battlefield zone reads as existing, a battlefield object must
// not be phased out.
func existsOnBattlefieldQuiet(o *state.Object) bool {
	return o != nil && (o.Zone != state.ZBattlefield || !o.PhasedOut)
}

// quietObjectFallback is the fail-closed per-object test: a merged pile, a
// face-down object or a face with no facts blocks every section that would
// visit it.
func (e *Engine) quietObjectFallback(o *state.Object) quietBlockerID {
	if o == nil || o.Card == nil || o.Face() == nil || len(o.MergedCards) != 0 {
		return qbBadObject
	}
	if e.walkFaceFactsOf(o.Face()) == nil {
		return qbBadObject
	}
	return qbNone
}

// abQuietBlocked applies the §2.4 ability test to one object's zone summary.
func abQuietBlocked(aq abQuietZone, o *state.Object, ceiling int32, unbounded, sorceryOpen bool) bool {
	if !aq.any {
		return false
	}
	if aq.nonMana {
		return true
	}
	if aq.hasAny && (unbounded || aq.floorAny <= ceiling) && (!aq.sorcAny || sorceryOpen) {
		return true
	}
	if aq.hasTap && !o.Tapped && (unbounded || aq.floorTap <= ceiling) && (!aq.sorcTap || sorceryOpen) {
		return true
	}
	return false
}

// quietAffordable reports floor <= ceiling, with an unbounded ceiling
// affordable for every floor.
func quietAffordable(floor, ceiling int32, unbounded bool) bool {
	return unbounded || floor <= ceiling
}

// quietFaceHasDownCast reports a Morph/Megamorph/Disguise face-down cast.
func quietFaceHasDownCast(f *cards.Face) bool {
	return f.HasKeyword("Morph") || f.HasKeyword("Megamorph") || f.HasKeyword("Disguise")
}

// quietHandKeywordAction reports a hand keyword action offered regardless of
// the face's own cast timing. Sneak and Teamwork mirror the exact timing
// gates their walk offers run (sneakTimingOK; stackKeywordPossibleH +
// activationPhasesOK), so a window where the walk offers neither stays
// provable.
func quietHandKeywordAction(o *state.Object, id state.ObjID, e *Engine, p state.PlayerID, sorceryOpen bool) bool {
	if e.sneakTimingOK(p) && e.stackKeywordPossibleH(id, kwhSneak) {
		return true
	}
	// The action keywords are read through the DERIVED precheck, the same
	// necessary condition the walk's own reads start with: Dream Devourer's
	// layer-6 AddKeyword$ Foretell grant reaches a plain hand card and the
	// walk offers the {2} action, so a printed-only proof would call the
	// window quiet. The Foretell turn gate mirrors the walk's own
	// (e.G.Active == p || playerForetellsAnyTurn), so a grant that only widens
	// the timing (Cosmos Charger) still blocks.
	if e.mayHaveDerivedKeywordH(id, kwhForetell) &&
		(e.G.Active == p || e.playerForetellsAnyTurn(p)) {
		return true
	}
	if e.mayHaveDerivedKeywordH(id, kwhSuspend) && e.G.Active == p {
		return true
	}
	if e.mayHaveDerivedKeywordH(id, kwhPlot) && sorceryOpen {
		return true
	}
	if e.mayHaveDerivedKeywordH(id, kwhMayFlashCost) {
		return true
	}
	for _, f := range o.Card.Faces {
		if f == nil {
			continue
		}
		if e.stackKeywordPossibleH(id, kwhTeamwork) && e.activationPhasesOK(p, f.SpellAbility()) {
			return true
		}
	}
	return false
}

// quietFaceNotForbidden is the land-play restriction gate. A restriction can
// only withhold a play, so ignoring it is sound; it is kept as a hook for the
// test fixtures that exercise adjustLandPlays.
func quietFaceNotForbidden(e *Engine, p state.PlayerID, id state.ObjID) bool {
	return !playLandForbidden(e, p, state.ZHand, id)
}

// quietDynamicKWMaybe reports an object whose intrinsic keywords or keyword
// counters could carry Flash or one of the expanded granted heads.
func quietDynamicKWMaybe(o *state.Object) bool {
	for _, k := range o.IntrinsicKeywords {
		if quietKWHeadBlocks(cards.KeywordHead(k)) {
			return true
		}
	}
	for _, c := range o.Counters {
		if c.N <= 0 {
			continue
		}
		if kwName, ok := cards.CounterKeyword(c.Kind); ok && quietKWHeadBlocks(kwName) {
			return true
		}
	}
	return false
}

func quietKWHeadBlocks(head string) bool {
	if strings.EqualFold(head, "Flash") {
		return true
	}
	for _, hd := range grantedKWHeads {
		if strings.EqualFold(head, hd.S) {
			return true
		}
	}
	return false
}
