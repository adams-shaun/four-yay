package rules

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// modalLandBack identifies the only Modal DFC face that may be selected by
// the hand-zone modal-land action. Keeping the shape check here makes offer
// and consumption use the same validation and prevents non-land modal backs
// from becoming castable through this path.
func modalLandBack(o *state.Object) *cards.Face {
	if o == nil || o.Card == nil || o.FaceIdx != 0 ||
		o.Card.AlternateMode != "Modal" || len(o.Card.Faces) != 2 ||
		o.Card.Faces[0] == nil || o.Card.Faces[1] == nil ||
		o.Zone != state.ZHand {
		return nil
	}
	if !o.Card.Faces[1].IsLand() {
		return nil
	}
	return o.Card.Faces[1]
}

// modalSpellBack identifies a Modal DFC's nonland back face when it is in hand.
func modalSpellBack(o *state.Object) *cards.Face {
	if o == nil || o.Card == nil || o.FaceIdx != 0 ||
		o.Card.AlternateMode != "Modal" || len(o.Card.Faces) != 2 ||
		o.Card.Faces[0] == nil || o.Card.Faces[1] == nil ||
		o.Zone != state.ZHand || o.Card.Faces[1].IsLand() {
		return nil
	}
	return o.Card.Faces[1]
}

// sorcerySpeed reports whether p may take a sorcery-speed action right now.
func (e *Engine) sorcerySpeed(p state.PlayerID) bool {
	return e.G.Active == p && e.G.Step.IsMain() && len(e.G.Stack) == 0
}

// mayPlayLandIds returns the ids of lands in player p's zones that an active
// may-play-from-zone grant (a S:Mode$ Continuous static carrying MayPlay$ True
// and an AffectedZone$, e.g. Conduit of Worlds' "You may play lands from your
// graveyard") lets p play this turn, in deterministic order. The result is
// empty unless p is at sorcery speed with a land drop remaining -- the same
// once-per-turn gate the hand walk applies, so a graveyard land and a hand
// land share one land drop per turn. A land already in p's hand never appears
// here (the ordinary hand walk offers it), and neither does a land in a hidden
// zone. Each candidate must match the granting effect's Affects filter (the
// Affected$ spec) through the same spec matcher the layer system uses, so a
// Land.YouOwn grant never offers an opponent's land or a non-land permanent.
//
// Order comes from e.active() (a sorted slice), then each effect's parsed
// AffectedZone order, then the zone slice order -- never a map -- so the
// resulting option list is reproducible run to run. Zones are deduplicated
// per (zone, id) so two grants naming the same zone never offer the same land
// twice. The zones walked are the graveyard and exile, plus the top card of
// the library (the kw-mayplay fallback below): the hand is covered by the
// normal walk, and a library card BELOW the top is hidden and cannot be
// meaningfully named.
func (e *Engine) mayPlayLandIds(p state.PlayerID) []state.ObjID {
	if !e.sorcerySpeed(p) || e.G.Players[p].LandsPlayed >= int32(1+e.adjustLandPlays(p)) {
		return nil
	}
	type offered struct {
		zone state.Zone
		id   state.ObjID
	}
	var out []state.ObjID
	var seen []offered
	limited := lazyMayPlays{e: e, p: p}
	for _, ce := range e.active() {
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
		consider := func(z state.Zone) {
			if z != state.ZGraveyard && z != state.ZExile {
				return
			}
			// Exile and graveyard are public zones keyed by the card's OWNER,
			// and a grant's cards can sit in another seat's slice (Opposition
			// Agent exiles a card from an OPPONENT's searching library, then
			// lets its controller play it), so every seat's slice is walked in
			// deterministic seat order -- the same shape mayPlaySpellIds'
			// walk already is. The Affects match decides ownership claims;
			// walking the slices only enumerates candidates.
			for _, q := range e.G.AliveFrom(0) {
				for _, id := range e.G.Zone(z, q) {
					o := e.G.Obj(id)
					if o == nil || o.Face() == nil || !o.Face().IsLand() {
						continue
					}
					if !e.effectGrantMatches(ce, id) {
						continue
					}
					dup := false
					for _, s := range seen {
						if s.zone == z && s.id == id {
							dup = true
							break
						}
					}
					if dup {
						continue
					}
					seen = append(seen, offered{z, id})
					out = append(out, id)
				}
			}
		}
		if all {
			consider(state.ZGraveyard)
			consider(state.ZExile)
		} else {
			for _, z := range zones {
				consider(z)
			}
		}
	}
	// kw-mayplay: the card's OWN static (or a battlefield static naming it)
	// can also grant the play -- the same predicate beginCast's "mayplay"
	// cost case consults, so offer and charge agree. It covers the zones the
	// ce walk above cannot reach: a self-grant on a card sitting in a
	// graveyard or exile (staticEffects never reads a non-battlefield
	// source) and a Library-zone grant (Ka-Zar of the Savage Land), offered
	// for the TOP CARD ONLY so the hidden library never leaks a deeper
	// identity. The membership check keeps a card the ce walk already
	// offered from being offered twice.
	contains := func(id state.ObjID) bool {
		for _, got := range out {
			if got == id {
				return true
			}
		}
		return false
	}
	// landGranted is the land walk's grant gate: the may-play permission AND
	// no RaiseCost$ surcharge. A land play is FREE -- there is no cost site
	// that could charge a surcharge -- so a granting static carrying ANY
	// RaiseCost$ (priced or not) is withheld whole rather than granted
	// uncharged, the widening direction this file refuses. Measured: no
	// corpus RaiseCost$ carrier is a land, so this is a guard against the
	// next one, not a live behaviour change.
	landGranted := func(id state.ObjID) bool {
		if _, ok := e.mayPlayGrant(p, id); !ok {
			return false
		}
		if _, hasRaise, _ := e.mayPlayRaiseCost(p, id); hasRaise {
			return false
		}
		return true
	}
	for _, z := range []state.Zone{state.ZGraveyard, state.ZExile} {
		for _, id := range e.G.Zone(z, p) {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil || !o.Face().IsLand() || o.Controller != p {
				continue
			}
			if landGranted(id) && !contains(id) {
				out = append(out, id)
			}
		}
	}
	if lib := e.G.Zone(state.ZLibrary, p); len(lib) > 0 {
		if o := e.G.Obj(lib[0]); o != nil && o.Face() != nil && o.Face().IsLand() && o.Controller == p {
			if landGranted(lib[0]) && !contains(lib[0]) {
				out = append(out, lib[0])
			}
		}
	}
	return out
}

// mayhemLandPlayIds returns the ids of lands in player p's graveyard that the
// bare, parameterless K:Mayhem permission makes playable this turn -- the
// "you may play this card from your graveyard if you discarded it this turn"
// land shape (Oscorp Industries is the sole corpus carrier). A land play is
// NOT a cast, so this is deliberately separate from mayhemCastCost, which
// withholds an empty parameter; the two cannot drift because a card's K:Mayhem
// is either a priced cast or the bare play permission, never both. The
// once-per-turn land-drop gate is the same sorcerySpeed &&
// LandsPlayed < 1+adjustLandPlays condition the hand walk and mayPlayLandIds
// apply, so a hand land, a granted graveyard land and a bare-Mayhem graveyard
// land all share one land drop. Provenance comes from mayhemDiscardedThisTurn,
// the same log-derived this-turn discard window the mayhem cast walk uses -- a
// mill move, another player's discard or a last-turn discard opens nothing.
// Graveyard zone slices are keyed by owner, so Zone(ZGraveyard, p) is p's own
// graveyard.
func (e *Engine) mayhemLandPlayIds(p state.PlayerID) []state.ObjID {
	if !e.sorcerySpeed(p) || e.G.Players[p].LandsPlayed >= int32(1+e.adjustLandPlays(p)) {
		return nil
	}
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZGraveyard, p) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil || !o.Face().IsLand() {
			continue
		}
		raw, ok := e.derivedKeywordParamH(id, kwhMayhem)
		if !ok || strings.TrimSpace(raw) != "" {
			continue
		}
		if !e.mayhemDiscardedThisTurn(p, id) {
			continue
		}
		out = append(out, id)
	}
	return out
}

// mayPlaysThisTurn counts the card plays this turn that went through a
// may-play-from-zone grant, in deterministic log order since the last
// TurnChange: CastInfo events carrying the mayplay flag (attributed by the
// card's OWNER -- CastInfo predates a Player field on the kind and setting
// one now would re-shape every replayed log's encoding, so the owner, which
// never changes, stands in; a control-steal corner may mis-attribute and
// then the cap can only undercount, never wedge), plus land plays whose
// MoveZone came from a granted (never hand) zone. Only a MayPlayLimit$ cap
// consults it; an unlimited grant ignores the count.
func (e *Engine) mayPlaysThisTurn(p state.PlayerID) int {
	// A log scan back to the turn's start, asked per candidate card: served
	// from the walk cache inside a legal-actions walk (rules/walkcache.go).
	return e.mayPlaysThisTurnCached(p)
}

// lazyMayPlays defers mayPlaysThisTurn's count to the first grant that
// carries a MayPlayLimit$ cap -- the count's only reader in every may-play
// walk -- so a walk over uncapped grants (or none) never runs the log scan.
// The walks are pure reads, so a late count equals an eager one.
type lazyMayPlays struct {
	e    *Engine
	p    state.PlayerID
	n    int
	done bool
}

func (l *lazyMayPlays) count() int {
	if !l.done {
		l.n, l.done = l.e.mayPlaysThisTurn(l.p), true
	}
	return l.n
}

func (e *Engine) scanMayPlaysThisTurn(p state.PlayerID) int {
	n := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		switch ev.Kind {
		case events.CastInfo:
			if !strings.Contains(ev.Counter, "mayplay") {
				continue
			}
			if o := e.G.Obj(ev.Obj); o != nil && o.Owner == p {
				n++
			}
		case events.MoveZone:
			if ev.From != state.ZGraveyard && ev.From != state.ZExile {
				continue
			}
			if ev.To != state.ZBattlefield {
				continue
			}
			if o := e.G.Obj(ev.Obj); o != nil && o.Owner == p && o.Face() != nil && o.Face().IsLand() {
				n++
			}
		}
	}
	return n
}

// mayPlaySpellIds returns the ids of NON-LAND cards in player p's zones that
// an active may-play-from-zone grant lets p CAST this turn, in deterministic
// order (e.active(), then each effect's parsed AffectedZone order, then the
// zone slice order). A card is offered once even when several grants cover
// it (per (zone,id) dedupe). Only zones a spell can meaningfully be cast
// from (graveyard, exile) are walked; hand is the ordinary walk and library
// is hidden. Each candidate must match the granting effect's Affects filter
// through MatchesSpecCtx with the grant's Remembered set loaded as the
// SpecContext's Remembered, so the dominant Affected$ Card.IsRemembered
// grant (227 corpus files) selects exactly the cards its delivering Effect
// captured -- the same direct-list reading restrictionApplies uses for
// Effect-delivered CantTarget/CantRegenerate. A MayPlayLimit$ grant whose
// cap is already reached does not offer through itself; another grant
// covering the same card still may.
func (e *Engine) mayPlaySpellIds(p state.PlayerID) []mayPlaySpellOffer {
	type offered struct {
		zone state.Zone
		id   state.ObjID
		key  string
	}
	var out []mayPlaySpellOffer
	var seen []offered
	limited := lazyMayPlays{e: e, p: p}
	consider := func(z state.Zone, id state.ObjID, key string) bool {
		for _, s := range seen {
			if s.zone == z && s.id == id && s.key == key {
				return false
			}
		}
		seen = append(seen, offered{z, id, key})
		out = append(out, mayPlaySpellOffer{zone: z, id: id, key: key})
		return true
	}
	// A card's OWN S: static can grant its cast from a public zone it sits
	// in -- Misthollow Griffin / Eternal Scourge's exile self-grant,
	// Gravecrawler's "as long as you control a Zombie" graveyard cast -- or
	// from the library's top card (Korlessa, Scale Singer). staticEffects
	// never reads a non-battlefield source, so this scan covers exactly the
	// self-grant shapes; the same mayPlayGrant predicate beginCast's
	// "mayplay" cost case consults evaluates the card's statics (its gates
	// -- Condition$ PlayerTurn, IsPresent$, the unread-gate family -- fail
	// closed inside) AND every battlefield static naming the card, so the
	// ce walk below and this scan agree on every grant either discovers.
	// A Library-zone grant is offered for the TOP CARD ONLY, so the hidden
	// library never leaks a deeper card identity into the option list. The
	// dedupe keeps a card the ce walk already offered from being offered
	// twice.
	//
	// Whether any board-side grant source exists for p is one fact for the
	// whole scan (mayPlayBoardGrantsOpen), so a board with none asks each
	// card only its own statics.
	//
	// A MayPlayText$-typed permission (Muldrotha) is enumerated per static:
	// mayPlayPermissions lists each still-unused typed permission, so an
	// artifact creature is offered once per matching permission and the
	// option carries the key the cast consumes. An untyped grant keeps its
	// single historical offer, whose riders the caller reads through the
	// aggregate mayPlayGrant/mayPlayRaiseCost helpers.
	board := e.mayPlayBoardGrantsOpen(p)
	addCard := func(z state.Zone, id state.ObjID) {
		perms := e.mayPlayPermissions(p, id, board)
		hasUntyped := false
		for _, off := range perms {
			if off.key == "" {
				hasUntyped = true
			}
		}
		// The untyped offer is the historical single offer. It is emitted
		// when a printed/self/board untyped grant covers the card; when only
		// typed permissions cover it, the typed offers below replace it (an
		// untyped offer would bypass every permission's own limit). When no
		// static covers it at all, mayPlayGrantScoped may still find a free
		// effect-delivered grant (its ce walk arm below), preserving the
		// historical effect-grant enumeration.
		if hasUntyped || len(perms) == 0 {
			if _, ok := e.mayPlayGrantScoped(p, id, board); ok {
				consider(z, id, "")
			}
		}
		for _, off := range perms {
			if off.key == "" {
				continue
			}
			if consider(z, id, off.key) {
				off.zone = z
				out[len(out)-1] = off
			}
		}
	}
	for _, z := range []state.Zone{state.ZGraveyard, state.ZExile} {
		for _, q := range e.G.AliveFrom(0) {
			for _, id := range e.G.Zone(z, q) {
				o := e.G.Obj(id)
				if o == nil || o.Controller != p || o.Face() == nil || o.Face().IsLand() {
					continue
				}
				addCard(z, id)
			}
		}
	}
	if lib := e.G.Zone(state.ZLibrary, p); len(lib) > 0 {
		if o := e.G.Obj(lib[0]); o != nil && o.Face() != nil && !o.Face().IsLand() && o.Controller == p {
			addCard(state.ZLibrary, lib[0])
		}
	}
	for _, ce := range e.active() {
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
		// Exile and graveyard are public zones keyed by the card's OWNER, and
		// a grant's cards can sit in another seat's slice (Intellect
		// Devourer exiles an OPPONENT's hand card, then lets its controller
		// play it), so every seat's slice is walked in deterministic seat
		// order -- never a map. The Affects match decides ownership claims;
		// walking the slices only enumerates candidates.
		for _, q := range e.G.AliveFrom(0) {
			for _, z := range func() []state.Zone {
				if all {
					return []state.Zone{state.ZGraveyard, state.ZExile}
				}
				return zones
			}() {
				if z != state.ZGraveyard && z != state.ZExile {
					continue
				}
				for _, id := range e.G.Zone(z, q) {
					o := e.G.Obj(id)
					if o == nil || o.Face() == nil || o.Face().IsLand() {
						continue
					}
					if !e.effectGrantMatches(ce, id) {
						continue
					}
					consider(z, id, "")
				}
			}
		}
	}
	return out
}

// coreCardTypes is CardType.CoreType (forge-card's CardType.java) -- the
// distinct-type census Delirium's gate counts. Kindred is included: modern
// type lines spell it as a word and the engine's printed Types carry it.
var coreCardTypes = []string{"Artifact", "Battle", "Creature", "Enchantment",
	"Instant", "Kindred", "Land", "Planeswalker", "Sorcery"}

// activationConditionOK evaluates an ability's Activation$ activation
// condition -- the "Hellbent —", "Threshold —", "Metalcraft —",
// "Delirium —" cost-prompt family (Sea Gate Wreckage's draw, Mox Opal's
// mana). Forge's SpellAbilityCondition.areMet keyword half, on the
// ACTIVATOR (the controller asking to activate), evaluated at OFFER time
// like the CheckSVar$ gate below: an ability whose condition fails is not
// offered, so a paid no-op activation is never reachable:
//
//   - Hellbent: the activator's hand is empty (Player.hasHellbent);
//   - Threshold: the activator's graveyard holds 7+ cards;
//   - Metalcraft: the activator controls 3+ artifacts;
//   - Delirium: the activator's graveyard holds 4+ distinct core card types
//     (AbilityUtils.countCardTypesFromList's non-permanent form);
//   - Blessing: the activator holds CR 702.131's city's blessing (the
//     one-way state.Player.Blessing latch rules/ascend.go's Ascend scan and
//     events.Apply's BlessingChange fold maintain). This is the gate half of
//     the city's-blessing family; Count$Blessing.<yes>.<no> (effects/count.go)
//     and the Condition$ Blessing gate read the same bit.
//
// Solved (the Case permanents' solved flag) names state this build does not
// track, so that gate FAILS CLOSED -- the conservative direction for an
// "only if" condition whose meeting cannot be verified. Blessing is the
// city's-blessing latch in state.Player and is read by the same offer-time
// gate as the other conditions. No repo-deck card carries Solved or Blessing
// (measured at the current corpus pin: 3 raw lines each, none in the decks).
func (e *Engine) activationConditionOK(p state.PlayerID, ab *cards.SA) bool {
	raw, ok := ab.Params["Activation"]
	if !ok || strings.TrimSpace(raw) == "" {
		return true
	}
	switch strings.TrimSpace(raw) {
	case "Hellbent":
		return len(e.G.Zone(state.ZHand, p)) == 0
	case "Threshold":
		return len(e.G.Zone(state.ZGraveyard, p)) >= 7
	case "Metalcraft":
		n := 0
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			if o := e.G.Obj(id); o != nil && slices.Contains(e.Derived(id).Types, "Artifact") {
				n++
			}
		}
		return n >= 3
	case "Blessing":
		// CR 702.131: the city's blessing, read off the same one-way latch
		// the Condition$ Blessing gate and the Count$Blessing branch head
		// read. An out-of-range activator denies -- the fail-closed
		// direction a blessing gate that cannot name its seat must take.
		return int(p) < len(e.G.Players) && !e.G.Players[p].Lost && e.G.Players[p].Blessing
	case "Delirium":
		seen := map[string]bool{}
		for _, id := range e.G.Zone(state.ZGraveyard, p) {
			if o := e.G.Obj(id); o != nil {
				for _, ty := range o.Face().Types {
					seen[ty] = seen[ty] || slices.Contains(coreCardTypes, ty)
				}
			}
		}
		n := 0
		for _, ty := range coreCardTypes {
			if seen[ty] {
				n++
			}
		}
		return n >= 4
	}
	return false
}

// activationGameTypesOK evaluates an activated ability's ActivationGameTypes$
// format list at offer time (War Room's "Activate only in a game of
// Commander, Brawl, Tiny Leaders, or Oathbreaker", the corpus's only
// carrier). The value is a comma list of the game formats the ability exists
// in; gorge models exactly two formats -- FormatCommander and
// FormatConstructed -- and only the "Commander" token maps to one. Brawl,
// TinyLeaders and Oathbreaker are not modelled and match nothing, so the
// list never admits a Constructed game: a format-gated ability is withheld
// before it could ever be announced (CR 602.1/601.2c: an illegal activation
// is not offered). Deterministic pure read -- no map range, tokens trimmed.
func activationGameTypesOK(f Format, raw string) bool {
	for tok := range strings.SplitSeq(raw, ",") {
		switch strings.TrimSpace(tok) {
		case "Commander":
			if f == FormatCommander {
				return true
			}
		}
	}
	return false
}

// abilityZoneOK reports whether ability ab may be activated while the
// source cardinal is in zone z (CR 602.1b): the printed ActivationZone$
// when present, the battlefield by default. Battlefield, Hand, Graveyard,
// Exile and Stack are enumerated by the legal-action walks (Exile since
// fuzz-cov3: Greater Gargadon's suspended sacrifice outlet; Stack since the
// Activator$ offer gate, for Lightning Storm's "Any player may activate this
// ability but only if CARDNAME is on the stack"); Command is not and
// therefore never offers an option.
// abilityZoneMask is abilityZoneOK(ab, z) for every z < 32, as bit z, from
// a single ActivationZone$ read (buildManaSAFacts' zone mask: one map lookup
// instead of one per zone).
func abilityZoneMask(ab *cards.SA) uint32 {
	az, ok := ab.Params["ActivationZone"]
	if !ok {
		return 1 << state.ZBattlefield
	}
	switch strings.TrimSpace(az) {
	case "Battlefield":
		return 1 << state.ZBattlefield
	case "Graveyard":
		return 1 << state.ZGraveyard
	case "Hand":
		return 1 << state.ZHand
	case "Exile":
		return 1 << state.ZExile
	case "Stack":
		return 1 << state.ZStack
	}
	return 0
}

func abilityZoneOK(ab *cards.SA, z state.Zone) bool {
	az, ok := ab.Param(cards.PKActivationZone)
	if !ok {
		return z == state.ZBattlefield
	}
	switch strings.TrimSpace(az) {
	case "Battlefield":
		return z == state.ZBattlefield
	case "Graveyard":
		return z == state.ZGraveyard
	case "Hand":
		return z == state.ZHand
	case "Exile":
		return z == state.ZExile
	case "Stack":
		return z == state.ZStack
	}
	return false
}

// activatorAllows reports whether player p may activate ability ab, per the
// ability's Activator$ parameter (Forge's PlayerProperty on an AB$/SP$ line,
// e.g. Oft-Nabbed Goat's "Only your opponents may activate this ability" ->
// Player.Opponent, Mana Cache's "Any player may activate this ability" ->
// Player). An absent Activator$ means the source's controller and only them:
// the CR 602.2a default. A present spec is resolved source-relative -- You is
// the source's current controller, and source-dependent selectors
// (Player.EnchantedController, Player.IsRemembered) bind their attachment and
// choice state to the source permanent -- through effects.MatchesPlayerSpecFrom,
// so every rule has one home and an unknown or unread selector fails closed
// rather than admitting extra activators. Every offer path (the printed AB walk,
// the granted/gained-ability walk and the mana-ability membership walk) routes
// through this helper, so the paths cannot drift.
func (e *Engine) activatorAllows(p state.PlayerID, id state.ObjID, ab *cards.SA) bool {
	spec := strings.TrimSpace(ab.Params["Activator"])
	if spec == "" {
		return e.controllerOf(id) == p
	}
	return effects.MatchesPlayerSpecFrom(e.G, spec, p, e.controllerOf(id), id)
}

// abilityPresentHolds evaluates an activated ability's IsPresent$ /
// PresentCompare$ activation gate at OFFER time -- Mistveil Plains' "Activate
// only if you control two or more white permanents" (IsPresent$
// Permanent.White+YouCtrl, PresentCompare$ GE2). The same deterministic
// battlefield count the statics' presentGate (rules/statics.go) and the
// reflected-mana offer's manaReflectedPresentHolds (rules/mana_activation.go)
// evaluate; PresentCompare$ defaults to "at least one" when it is absent, the
// shared reading everywhere else the gate appears. The gate is offer-time
// only, exactly like SorcerySpeed$/PlayerTurn$/CheckSVar$: no state can move
// between the offer and the answer inside one priority window, and the
// resolution does not re-gate.
//
// Because the gate applies to every non-mana activation (the offer loop
// skips isManaAbilityAPI abilities -- a plain AB$ Mana ability's own
// IsPresent$ gate is the mana path's business), its reads are excluded from
// the census's generic rules-side SA union for Mana/ManaReflected: see
// genericSAExcludes in paramcensus_test.go.
func (e *Engine) abilityPresentHolds(p state.PlayerID, id state.ObjID, ab *cards.SA) bool {
	if !e.classBandGateHolds(ab.Params, id) {
		return false
	}
	spec := strings.TrimSpace(ab.Params["IsPresent"])
	if spec == "" {
		return true
	}
	n := 0
	if pz := strings.TrimSpace(ab.Params["PresentZone"]); pz != "" {
		// PresentZone$ (Greater Gargadon's "Activate only if this is
		// suspended": IsPresent$ Card.Self+suspended | PresentZone$ Exile)
		// counts the named zone in every living seat, the trigger clause's
		// presentZoneCount; an unknown zone word fails closed.
		zone, known := effects.ParseZoneWord(pz)
		if !known {
			return false
		}
		n = e.presentZoneCount(zone, spec, id, p)
	} else {
		n = e.countPresent(spec, id, p)
	}
	if cmp := strings.TrimSpace(ab.Params["PresentCompare"]); cmp != "" {
		return comparePresent(n, e.presentCompareFor(cmp, id, p))
	}
	return n > 0
}

// adaptGateOK evaluates AB$ PutCounter's Adapt$ activation gate (CR 702.35a:
// "Activate only if this creature has no +1/+1 counters on it"). Adapt$ is
// an ability PARAMETER, not a K: keyword line, so the ability offer loop has
// to read it directly -- the same shape Boast$ takes (rules/boast_test.go's
// header). Measured corpus: 24 `A:AB$ PutCounter ... Adapt$` carriers
// (Pteramander, Incubation Druid, ... every value a literal 1-4) plus one
// chained `DB$ PutCounter | Adapt$ 3` body (Jetfire), which is governed by
// the effect's own if-condition at resolution (effects/counters.go), not by
// this activation gate. Offer-time only, exactly like the IsPresent$ /
// CheckSVar$/Boast$ gates it sits beside: no state can move between the
// offer and the answer inside one priority window.
func (e *Engine) adaptGateOK(id state.ObjID, ab *cards.SA) bool {
	if strings.TrimSpace(ab.Params["Adapt"]) == "" {
		return true
	}
	o := e.G.Obj(id)
	return o != nil && o.Counter("P1P1") == 0
}

// monstrosityGateOK evaluates AB$ PutCounter's Monstrosity$ once-only gate
// (CR 701.31b: "Activate only if this creature isn't already monstrous",
// Giggling Skitterspike's `{5}: Monstrosity 5`). Monstrosity$ is an ability
// PARAMETER like Adapt$, so the offer loop reads it directly -- the same
// shape the Adapt$ gate takes. Offer-time only, exactly like the Adapt$ /
// IsPresent$ / CheckSVar$ gates it sits beside: no state can move between
// the offer and the answer inside one priority window. effects/counters.go
// keeps a resolve-time already-monstrous skip as defense-in-depth (no
// corpus shape reaches the resolution through any other door -- no granted
// or copied route for these abilities).
func (e *Engine) monstrosityGateOK(id state.ObjID, ab *cards.SA) bool {
	if strings.TrimSpace(ab.Params["Monstrosity"]) == "" {
		return true
	}
	o := e.G.Obj(id)
	return o != nil && !o.Monstrous
}

// sVarGateOK evaluates the ability's CheckSVar$/SVarCompare$ intervening-if
// at OFFER time: Bloodsoaked Champion's Raid ("Activate only if you attacked
// this turn", CheckSVar$ RaidTest = Count$AttackersDeclared) and Ojer
// Axonil's transformed Temple of Power ("Activate only if red sources you
// controlled dealt 4 or more noncombat damage this turn" — a count head this
// build does not model, so its gate reads 0 and the transform stays
// unoffered, the documented degrade-to-zero direction). The same evaluator
// conditionMet (ConditionCheckSVar$, effects/conditions.go) and the statics'
// checkSVarHolds delegate to: effects.CheckSVarHolds. Because the gate
// applies to every non-mana activation, its reads are the census's generic
// rules-side SA set, not any one api's.
func (e *Engine) sVarGateOK(p state.PlayerID, id state.ObjID, ab *cards.SA, merged int) bool {
	check, ok := ab.Params["CheckSVar"]
	if !ok {
		return true
	}
	o := e.G.Obj(id)
	if o == nil || o.PileFaceFor(merged) == nil {
		return false
	}
	svars := e.pileSVars(id, merged)
	ctx := &effects.Ctx{Source: id, Controller: p, SVars: svars}
	holds, evaluated := effects.CheckSVarHolds(e, ctx, check, ab.Params["SVarCompare"])
	if !evaluated {
		// The gate's count body is not one the evaluator models: fail OPEN —
		// the ability is still offered. A gate you cannot read must not
		// silently remove a card's activation (Ojer Axonil's transformed
		// Temple of Power counts noncombat damage by source this build does
		// not track; suppressing the transform on that account would brick
		// the card's mechanic on an unreadable gate).
		return true
	}
	return holds
}

// ownReduceCost evaluates an ability's own ReduceCost$ parameter (Otawara,
// Soaring City's Channel: "This ability costs {1} less to activate for each
// legendary creature you control" — ReduceCost$ X over
// SVar:X:Count$Valid Creature.Legendary+YouCtrl): the generic reduction the
// value resolves to. A literal is the value; a name (X) resolves through the
// source face's SVar table via effects.EvalCountOK — the same resolver
// fixLifeXCost uses — and an unresolvable body degrades to zero (a
// reduction this build cannot compute is never silently over-applied; the
// full-cost ability stays legal, just never discounted). The CR 601.2f
// composition: folded into the offer gate's cost AND beginActivation's
// stored cost, so the two can never disagree. Because the read applies to
// every non-mana activation, it joins the census's generic rules-side SA
// set, not one api's.
//
// targets are the chosen ROOT targets, carried on the Ctx so a target-
// dependent body (Raft Security Officer's AllTargeted$Valid
// Creature.powerLE3) can resolve; allTargets is the whole-chain union
// (alltargeted1) bound as Ctx.AllTargets, so an AllTargeted$ body reads
// Forge's union over the root/sub-ability chain (Wayta, Trainer Prodigy's
// fight) rather than the root's list alone. The offer/projection sites pass
// nil for both — targets do not exist yet at offer time, so a target-
// dependent reduction reads 0 there (full price, fail closed) — and
// repriceForTargets re-runs the evaluation with the answered targets (and
// the pre-asked sub answers) at CR 601.2c, before CR 601.2h pays.
func (e *Engine) ownReduceCost(p state.PlayerID, id state.ObjID, ab *cards.SA, targets, allTargets []state.Target, merged int) int32 {
	v := strings.TrimSpace(ab.Params["ReduceCost"])
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n < 0 {
			return 0
		}
		return int32(n)
	}
	o := e.G.Obj(id)
	if o == nil || o.PileFaceFor(merged) == nil {
		return 0
	}
	svars := e.pileSVars(id, merged)
	body := v
	if b, ok := svars[v]; ok {
		body = b
	}
	ctx := &effects.Ctx{Source: id, Controller: p, SVars: svars, Targets: targets, AllTargets: allTargets}
	if n, ok := effects.EvalCountOK(e, ctx, body); ok && n > 0 {
		return n
	}
	return 0
}

// ownReduceCostOffer is ownReduceCost's offer-time reading for a body that
// reads a ROOT target ref (Targeted$CardPower, CR 702.6's equip target). The
// ability's own targets do not exist until CR 601.2c, so a plain nil-target
// read is 0 and the offer gate would withhold the ability at FULL price even
// when a legal target reduces it into reach: Belt of Giant Strength's
// `Equip {10}` against a 4-power creature costs {6}, and a {6} pool must
// offer it. This evaluates the body once per legal root target candidate and
// returns the LARGEST reduction, so the option is offered iff SOME legal
// CR 601.2c announcement is payable; beginActivation folds the same amount
// (so the pre-target payability check in continueCast passes) and
// repriceForTargets then charges the exact amount for the target actually
// chosen (CR 601.2f, idempotent net-adjust). A body that reads no root target
// ref keeps the plain nil-target read, unchanged; the AllTargeted$ union is
// deliberately not consulted here (alltargeted1's sub-ability shape). A
// property the <Ref>$<Property> family does not model (dragonfire_blade's
// Targeted$CardNumColors) still evaluates to 0 here, so that card's offer
// price is unchanged -- only the Ctx binding is in this helper's scope.
func (e *Engine) ownReduceCostOffer(p state.PlayerID, id state.ObjID, ab *cards.SA, merged int) int32 {
	best := e.ownReduceCost(p, id, ab, nil, nil, merged)
	if ab == nil {
		return best
	}
	v := strings.TrimSpace(ab.Params["ReduceCost"])
	if v == "" {
		return best
	}
	if _, err := strconv.Atoi(v); err == nil {
		return best
	}
	svars := e.pileSVars(id, merged)
	body := v
	if b, ok := svars[v]; ok {
		body = b
	}
	if !bodyReadsRootTarget(body, svars, 0) {
		return best
	}
	for _, cand := range e.costPotentialTargets(p, id, abilityScope(ab)) {
		if n := e.ownReduceCost(p, id, ab, []state.Target{cand}, nil, merged); n > best {
			best = n
		}
	}
	return best
}

// isLoyaltyAbility reports whether ab is a planeswalker loyalty ability
// (CR 606): it carries the Planeswalker$ parameter (case-insensitive -- three
// corpus lines spell it "true"), or its parsed Cost$ contains an
// AddCounter/SubCounter part of the LOYALTY kind. The OR is load-bearing on
// the cost side: the dynamic [-X] form SubCounter<X/LOYALTY> (20 raw lines)
// parses into an announced SubCounter part of the LOYALTY kind (see
// rules/mana.go's subCounterCost), so the cost alone still identifies it even
// if a hypothetical carrier omitted the parameter; the param additionally
// covers every fixed [+N]/[-N] shape (978 raw lines, 977 carrying the
// parameter) as well as the 20 dynamic [-X] lines, every one of which carries
// it.
func isLoyaltyAbility(ab *cards.SA) bool {
	return isLoyaltyAbilityCost(ab, ParseCost(ab.Params["Cost"]))
}

// isLoyaltyAbility is the engine-owned form of the loyalty classifier. Its
// card-script cost is configured text, so use the immutable parser sidecar.
func (e *Engine) isLoyaltyAbility(ab *cards.SA) bool {
	raw := ab.Params["Cost"]
	if !containsLoyaltyFold(raw) {
		// No AddCounter/SubCounter part of raw can carry a LOYALTY spec (each
		// part's Spec is a substring of the raw text, and no non-ASCII rune
		// case-folds onto l/o/y/a/t), so only the Planeswalker$ marker can
		// classify it: skip the parsed-cost lookup and its large Cost copy
		// (isLoyaltyMarked is isLoyaltyAbilityCost's marker half, which is
		// its whole answer for a cost with no counter parts).
		return isLoyaltyMarked(ab)
	}
	return isLoyaltyAbilityRef(ab, e.costRef(raw))
}

// containsLoyaltyFold reports whether s contains "loyalty" in any ASCII case.
func containsLoyaltyFold(s string) bool {
	const w = "loyalty"
	for i := 0; i+len(w) <= len(s); i++ {
		if s[i]|0x20 != 'l' {
			continue
		}
		j := 1
		for ; j < len(w); j++ {
			if s[i+j]|0x20 != w[j] {
				break
			}
		}
		if j == len(w) {
			return true
		}
	}
	return false
}

func isLoyaltyAbilityCost(ab *cards.SA, c Cost) bool {
	return isLoyaltyAbilityRef(ab, &c)
}

// isLoyaltyAbilityRef is isLoyaltyAbilityCost over a read-only cost pointer
// (a shared compiled cost from costRef is never copied or written).
func isLoyaltyAbilityRef(ab *cards.SA, c *Cost) bool {
	if isLoyaltyMarked(ab) {
		return true
	}
	for _, part := range c.AddCounter {
		if strings.EqualFold(part.Spec, "LOYALTY") {
			return true
		}
	}
	for _, part := range c.SubCounter {
		if strings.EqualFold(part.Spec, "LOYALTY") {
			return true
		}
	}
	return false
}

// isLoyaltyMarked reports whether ab carries the Planeswalker$ True marker,
// the half of isLoyaltyAbilityCost that reads no cost.
func isLoyaltyMarked(ab *cards.SA) bool {
	if v, ok := ab.Params["Planeswalker"]; ok && strings.EqualFold(strings.TrimSpace(v), "True") {
		// Ultimate$ (Ugin, Eye of the Storms' [-X]: AB$ ChangeZone ...
		// Ultimate$ True) marks the planeswalker's ultimate for Forge's
		// deck-tooling and the client's loyalty-UI presentation; the rules
		// meaning -- a loyalty ability, once per permanent per turn (CR
		// 606.3) -- is already covered by the Planeswalker$ marker this gate
		// reads. The recognition keeps the parameter census honest (its value
		// is discarded, so it is read only on this branch rather than on every
		// ability the offer walk classifies); the presentation half is named
		// in the deck import report's Issues.
		_ = ab.Params["Ultimate"]
		return true
	}
	return false
}

// loyaltyActivationsThisTurn counts how many loyalty abilities of the
// permanent id have been activated in its current battlefield stint this turn.
// It folds the whole replayable log forward because both facts an AbilityPush
// needs are historical: the source's active face at that event, and whether a
// MoveZone ACTUALLY crossed the battlefield boundary. Event.From is advisory
// (events.Move deliberately uses the object's recorded zone instead), so it
// must not decide a stint boundary.
//
// Every genesis object starts outside the battlefield and on face zero. The
// fold keeps just those two facts for id. MoveZone's To is authoritative after
// Apply succeeds, so a same-zone re-append leaves onBattlefield unchanged even
// when a malformed From claims otherwise. FlipFace is applied after any push
// on its old face, exactly as events.Apply does. TurnChange resets the count
// but deliberately retains the folded zone and face for the next turn.
func (e *Engine) loyaltyActivationsThisTurn(id state.ObjID) int {
	o := e.G.Obj(id)
	if o == nil || o.Card == nil || len(o.Card.Faces) == 0 {
		return 0
	}

	used := 0
	onBattlefield := false
	faceIdx := 0
	for _, ev := range e.L.Events {
		switch ev.Kind {
		case events.TurnChange:
			// CR 606.3's window is a turn, not a player's own turn.
			// Mirror events.Apply's validity gate: an invalid player leaves
			// Game.Turn unchanged, so its rejected event cannot open a new
			// loyalty-activation window in this historical fold either.
			if int(ev.Player) < len(e.G.Players) {
				used = 0
			}

		case events.MoveZone:
			if ev.Obj != id || !ev.To.Valid() {
				continue
			}
			nextOnBattlefield := ev.To == state.ZBattlefield
			if onBattlefield != nextOnBattlefield {
				// CR 400.7: every real departure or entry starts a new
				// permanent stint. This is derived from the folded zone,
				// never the caller-controlled Event.From.
				used = 0
			}
			onBattlefield = nextOnBattlefield

		case events.AbilityPush:
			if ev.Obj != id || !onBattlefield || int(ev.Player) >= len(e.G.Players) ||
				ev.Amount < 0 || faceIdx >= len(o.Card.Faces) {
				continue
			}
			// Amount indexes the active face's ability list. Check the exact
			// face and bounds that events.Apply used at push time.
			f := o.Card.Faces[faceIdx]
			if f != nil && int(ev.Amount) < len(f.Abilities) && e.isLoyaltyAbility(f.Abilities[int(ev.Amount)]) {
				used++
			}

		case events.GainedAbilityPush:
			// A GAINED loyalty activation (GainsAbilitiesOf$, Nicol Bolas
			// Dragon-God's class) counts toward the same CR 606.3
			// once-per-permanent window: the foreign face's ability at index
			// Amount is the loyalty ability that was activated FROM id, so the
			// printed and gained activations share one per-permanent tally.
			if ev.Obj != id || !onBattlefield || len(ev.IDs) == 0 {
				continue
			}
			foreign := e.G.Obj(ev.IDs[0])
			if foreign == nil || foreign.Face() == nil {
				continue
			}
			abilities := foreign.Face().Abilities
			if ev.Amount < 0 || int(ev.Amount) >= len(abilities) {
				continue
			}
			if e.isLoyaltyAbility(abilities[int(ev.Amount)]) {
				used++
			}

		case events.GrantAbilityPush:
			// An SVar-anchored GRANTED loyalty ability (an AddAbility$ static
			// whose body carries Planeswalker$ True -- Rowan's Talent's
			// "Enchanted planeswalker has [+1]: ...") is a loyalty ability of
			// THIS permanent (the recipient) and shares its CR 606.3 tally.
			// Counter names the body on the grantor (IDs[0]); a grantor that
			// has since left resolves through grantedSAFrom's recipient
			// fallback or not at all, and an unresolvable body is not counted.
			if ev.Obj != id || !onBattlefield || len(ev.IDs) == 0 || ev.Counter == "" {
				continue
			}
			if body := e.grantedSAFrom(ev.IDs[0], id, ev.Counter); body != nil && e.isLoyaltyAbility(body) {
				used++
			}

		case events.FlipFace:
			if ev.Obj == id && ev.Amount >= 0 && int(ev.Amount) < len(o.Card.Faces) {
				faceIdx = int(ev.Amount)
			}
		}
	}
	return used
}

// loyaltyAbilityLimit resolves how many loyalty abilities the permanent id
// may have activated this turn: 1 (CR 606.3) raised by every live
// S:Mode$ NumLoyaltyAct static whose ValidCard$ matches the permanent --
// Oath of Teferi's "twice each turn rather than only once" (Twice$ True,
// ValidCard$ Planeswalker.YouCtrl) and Urza, Lord Protector's self-scoped
// same (ValidCard$ Card.Self). Any matching Twice$ True raises the base limit
// to 2 (max, so two stacked Twice statics do not compound); every matching
// Additional$ N then adds N -- Forge's two parameters are accumulated
// separately so their result cannot depend on battlefield scan order. An
// additional-activation grant (The Chain Veil's "as though none of its loyalty
// abilities had been activated") therefore stacks on top of a twice grant
// rather than being absorbed by it. Statics that match neither parameter leave
// the limit alone. The ValidCard$ match resolves against the static's own source
// and controller (e.specCtx), exactly as castRestricted and abilityRestricted
// resolve theirs.
//
// Both delivery routes are read (task pw-numloyaltyact): a printed S: line
// (activeStatics' APNAP walk) and an Effect-delivered one -- Kaito, Dancing
// Shadow's PWTwice, Comet, Stellar Pup's LoyaltyAbs and Urza Assembles the
// Titans' PWTwice register a NumLoyaltyAct registry entry (effects' effEffect),
// read here in registry order. A granted line's own Remembered set is bound
// for the `Card.IsRemembered` spelling, exactly as staticGoadLines binds it
// for Goad. Both routes share one accumulator so neither can drift from the
// other's Twice/Additional combination rule.
func (e *Engine) loyaltyAbilityLimit(id state.ObjID) int {
	twice := false
	additional := 0
	// apply folds one live NumLoyaltyAct line into the accumulator. The
	// ValidCard$ spec is matched against the permanent with the static's own
	// source, controller and remembered set bound (the same binding
	// restrictionApplies and goadLineMatches use).
	apply := func(params map[string]string, source state.ObjID, controller state.PlayerID, remembered []state.ObjID) {
		sc := e.specCtx(source, controller)
		for _, r := range remembered {
			sc.Remembered = append(sc.Remembered, state.Target{Obj: r})
		}
		if !e.matchesSpec(params["ValidCard"], id, sc) {
			return
		}
		if params["Twice"] == "True" {
			twice = true
		}
		if raw, ok := params["Additional"]; ok {
			additional += int(parseAmount(raw, 0))
		}
	}
	for _, sv := range e.activeStatics("NumLoyaltyAct") {
		apply(sv.Params, sv.Source, sv.Controller, nil)
	}
	for _, ce := range e.active() {
		if ce.Restriction != "NumLoyaltyAct" {
			continue
		}
		apply(ce.RestrictParams, ce.Source, ce.Controller, ce.Remembered)
	}
	base := 1
	if twice {
		base = 2
	}
	return base + additional
}

// activationUsedCount counts this object's activations of one ability over
// the replayable event log. The identity has two shapes, exactly as
// boastGateOK reads them: a PRINTED or mana activation mints an AbilityPush /
// ManaActivate whose Amount is the ability's FLAT pile index (CR 702.140d,
// top face first then under-cards); a GRANTED activation (AddAbility$ /
// Animate) mints a DelayedPush or GrantAbilityPush whose Amount is -1 and
// whose Counter names the granting SVar instead. ability < 0 selects the
// granted shape, svar == "" the printed shape -- so one scanner serves both
// ActivationLimit$ (thisTurn) and GameActivationLimit$ (the whole game), and
// a third caller cannot forget one of them.
//
// thisTurn bounds the walk at the latest TurnChange: the per-turn limit's
// window (CR 606.3's "this turn", and Forge's ActivationLimit$, which
// ActivationTable resets each turn). The per-game walk deliberately does NOT
// break there, and deliberately does not reset on a zone change either:
// Forge keys the count on the host CARD (Card.numberGameActivations, read
// through SpellAbility.getActivationsThisGame), which survives a battlefield
// departure, so a permanent that leaves and returns has still spent its
// once-per-game activation.
func (e *Engine) activationUsedCount(id state.ObjID, ability int, svar string, thisTurn bool) int {
	used := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if thisTurn && ev.Kind == events.TurnChange {
			break
		}
		if ev.Obj != id {
			continue
		}
		switch ev.Kind {
		case events.AbilityPush, events.ManaActivate:
			// A ManaActivate carrying IDs is a GAINED mana activation
			// (gainedManaRef): its Amount indexes the foreign face, not
			// this object's pile, so it is never a printed activation.
			if ev.Kind == events.ManaActivate && len(ev.IDs) > 0 {
				continue
			}
			if ability >= 0 && ev.Amount == int32(ability) {
				used++
			}
		case events.DelayedPush, events.GrantAbilityPush:
			if svar != "" && ev.Counter == svar {
				used++
			}
		}
	}
	return used
}

// activationLimitReachedAt reports whether this object has already activated
// the ability at the FLAT pile index ability as many times as its
// ActivationLimit permits this turn. merged selects the face whose SVar table
// a computed limit resolves against (0 = the top face). The limit itself is
// resolved by resolveActivationLimitAt: a literal integer is used directly,
// and a computed expression (an SVar name or an inline Count$...) is
// evaluated through the effects count path, so a limit such as Withering
// Wisps' "number of snow Swamps you control" is enforced rather than silently
// ignored. A limit that resolves to zero or to fewer activations than have
// already been used withholds the offer. An expression that genuinely cannot
// be resolved stays unenforced (today's behaviour): resolveActivationLimitAt
// reports ok=false.
func (e *Engine) activationLimitReachedAt(id state.ObjID, p state.PlayerID, ability int, raw string, merged int) bool {
	limit, ok := e.resolveActivationLimitAt(id, p, raw, merged)
	if !ok || limit < 0 {
		return false
	}
	return e.activationUsedCount(id, ability, "", true) >= limit
}

// additionalActivationLimit returns the largest finite MinLimit$ supplied by
// an applicable Activations static, or baseline when none applies. Forge's
// MinLimit is an absolute ceiling (e.g. 2 means twice, not baseline + 2).
// Negative/unparseable limits are deliberately ignored: they represent a
// different unbounded/conditional rule and must not disable a finite cap.
func (e *Engine) additionalActivationLimit(id state.ObjID, actor state.PlayerID, ab *cards.SA, baseline int) int {
	limit := baseline
	for _, sv := range e.activeStatics("Activations") {
		validSA := strings.TrimSpace(sv.Params["ValidSA"])
		matched := false
		for alt := range strings.SplitSeq(validSA, ",") {
			parts := strings.SplitN(strings.TrimSpace(alt), ".", 2)
			if len(parts) != 2 || parts[0] != "Activated" {
				continue
			}
			switch parts[1] {
			case "Exhaust":
				matched = strings.EqualFold(strings.TrimSpace(ab.Params["Exhaust"]), "True")
			case "PowerUp":
				matched = strings.EqualFold(strings.TrimSpace(ab.Params["PowerUp"]), "True")
			}
			if matched {
				break
			}
		}
		if !matched {
			continue
		}
		if spec := strings.TrimSpace(sv.Params["ValidCard"]); spec != "" &&
			!e.matchesSpec(spec, id, e.staticSpecCtx(sv)) {
			continue
		}
		if !e.actorMatches(sv, "ValidPlayer", actor) {
			continue
		}
		if turn := strings.TrimSpace(sv.Params["PlayerTurn"]); turn != "" {
			if turn != "You" || e.G.Active != actor {
				continue
			}
		}
		if !e.checkSVarHolds(sv) {
			continue
		}
		min, err := strconv.Atoi(strings.TrimSpace(sv.Params["MinLimit"]))
		if err == nil && min > limit {
			limit = min
		}
	}
	return limit
}

// activationLimitBlocked is the ONE gate every activation offer site calls
// for ActivationLimit$ (this turn), GameActivationLimit$ (the whole game),
// Exhaust$ True, and PowerUp$ True (once per host card per game). All are
// read here so a new offer
// site cannot miss one -- the printed and granted loops in this file and the
// mana walk in mana_activation.go all funnel through it. svar is the
// granted-ability identity ("" for a printed ability); merged selects the face
// a computed limit resolves against. The per-GAME count is scanned with the
// same identity shapes and no turn or stint boundary (see activationUsedCount).
//
// A limit that resolves to zero or to fewer activations than already used
// withholds; an unresolvable expression stays unenforced, exactly as the
// per-turn gate already documented.
func (e *Engine) activationLimitBlocked(p state.PlayerID, id state.ObjID, sa *cards.SA, ability int, svar string, merged int) bool {
	if sa == nil {
		return false
	}
	if raw, ok := sa.Params["ActivationLimit"]; ok {
		if limit, ok := e.resolveActivationLimitAt(id, p, raw, merged); ok && limit >= 0 &&
			e.activationUsedCount(id, ability, svar, true) >= limit {
			return true
		}
	}
	if raw, ok := sa.Params["GameActivationLimit"]; ok {
		if limit, ok := e.resolveActivationLimitAt(id, p, raw, merged); ok && limit >= 0 {
			limit = e.additionalActivationLimit(id, p, sa, limit)
			if e.activationUsedCount(id, ability, svar, false) >= limit {
				return true
			}
		}
	}
	// Exhaust$ True and PowerUp$ True use the same host-card, per-game
	// counter as GameActivationLimit$: leaving and returning does not re-arm
	// either restriction. Activations statics raise this finite ceiling; they
	// never make the ability unlimited unless a supported static explicitly
	// has a negative MinLimit; those conditional/unbounded statics are not modeled.
	if strings.EqualFold(strings.TrimSpace(sa.Params["Exhaust"]), "True") ||
		strings.EqualFold(strings.TrimSpace(sa.Params["PowerUp"]), "True") {
		limit := e.additionalActivationLimit(id, p, sa, 1)
		if limit >= 0 && e.activationUsedCount(id, ability, svar, false) >= limit {
			return true
		}
	}
	return false
}

// boastGateOK implements CR 702.142's Boast activation restriction for an
// ability whose SA carries Boast$ True: the ability may be activated only if
// its source creature attacked this turn, and only once each turn. Both
// halves are read from replay-derivable state: "attacked this turn" is the
// event-folded Object.AttacksThisTurn (events.Apply's DeclareAttackers case,
// reset in TurnChange's per-object loop -- the same fact the Raid gate's
// Count$AttackersDeclared and the FirstAttack$ trigger gate read), and
// "already used this turn" is the activation-event scan the ActivationLimit$
// gate uses, with one extension.
//
// The extension is the granted-ability identity. A PRINTED AB$ mints an
// AbilityPush whose Amount is the ability's face index (events.Apply's
// AbilityPush case); a GRANTED AB$ (Besieged Viking Village's AddAbility$
// ABBoast) goes through beginGrantedActivation, which mints a DelayedPush
// whose Amount is -1 and whose Counter names the granting SVar instead
// (rules/speed.go). A scan that only looked at AbilityPush/Amount --
// activationLimitReached's shape -- could not see a granted Boast at all and
// would re-offer it every window. Matching either identity closes that: the
// printed form matches on index, the granted form on the SVar name.
func (e *Engine) boastGateOK(id state.ObjID, ability int, svar string) bool {
	o := e.G.Obj(id)
	if o == nil || o.AttacksThisTurn == 0 {
		return false
	}
	used := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Obj != id {
			continue
		}
		switch ev.Kind {
		case events.AbilityPush:
			if svar == "" && ev.Amount == int32(ability) {
				used++
			}
		case events.DelayedPush, events.GrantAbilityPush:
			// A granted activation's identity is its SVar name. A self-grant
			// mints through DelayedPush (Counter = the name); a CROSS-object
			// grant -- a printed Continuous AddAbility$ static such as
			// Besieged Viking Village's "All creatures have 'Boast -- {1}: ...'"
			// -- mints through GrantAbilityPush, whose Counter is the same
			// name. Reading only DelayedPush would leave the granted Boast
			// re-offered in every priority window of the turn it was used.
			if svar != "" && ev.Counter == svar {
				used++
			}
		}
	}
	return used == 0
}

// resolveActivationLimitAt interprets an ActivationLimit$ value. A literal
// integer is used directly. A non-literal value is resolved through the
// Count$/SVar evaluator the rest of the tree uses (effects.EvalCount), bound
// to the source object and the SVar table of the face at pile position merged
// (0 = the top face), so a computed limit is enforced rather than silently
// ignored. ok reports whether the limit was resolvable at all: false keeps the
// pre-fix behaviour of leaving the limit unenforced, which is also what a
// value that is neither a literal nor an SVar reference (such as a
// description-suffixed literal from a keyword template) gets.
func (e *Engine) resolveActivationLimitAt(id state.ObjID, p state.PlayerID, raw string, merged int) (int, bool) {
	raw = strings.TrimSpace(raw)
	if n, err := strconv.Atoi(raw); err == nil {
		return n, true
	}
	o := e.G.Obj(id)
	if o == nil || o.PileFaceFor(merged) == nil {
		return 0, false
	}
	svars := e.pileSVars(id, merged)
	ctx := &effects.Ctx{Source: id, Controller: p, SVars: svars}
	if strings.HasPrefix(raw, "Count$") {
		return int(effects.EvalCount(e, ctx, raw)), true
	}
	if body, ok := svars[raw]; ok {
		return int(effects.EvalCount(e, ctx, body)), true
	}
	return 0, false
}

// targetBoundReadsPromisedGift limits the alternate-branch offer check to
// target bounds whose SVar table can read Count$PromisedGift. It is a
// conservative over-approximation: a SVar that reads PromisedGift for a
// non-bound purpose also trips it. That is the safe direction for an
// OFFER-feasibility union (it can only widen the offered set, never narrow
// it, and the post-election ask still reads the elected branch), so the
// false-positive cost is an offered cast whose only feasible branch the
// player then does not elect -- the ordinary CR 601.2c reversal.
func targetBoundReadsPromisedGift(o *state.Object, sa *cards.SA) bool {
	if o == nil || o.Face() == nil || sa == nil {
		return false
	}
	if strings.Contains(sa.Params["TargetMin"], "Count$PromisedGift") ||
		strings.Contains(sa.Params["TargetMax"], "Count$PromisedGift") {
		return true
	}
	for _, body := range o.Face().SVars {
		if strings.Contains(body, "Count$PromisedGift") {
			return true
		}
	}
	return false
}

// targetSAAvailable reports whether a target declaration admits at least one
// legal announcement at offer time: enough legal candidates for its resolved
// mandatory minimum AND the cross-target set constraints the post-push ask
// enforces (per-controller exclusivity/OneEach, same-controller group capacity
// and set-property capacity). It is still not a full cast/payment check:
// target-dependent cost modifiers and the final target announcement belong to
// the post-push ask. CR 601.2c is the reason the offer must not admit a
// declaration the ask will reverse (CR 733.1) the instant it is submitted.
func (e *Engine) targetSAAvailable(p state.PlayerID, id, excludeSelf state.ObjID, sa *cards.SA, x int32, xPending bool) bool {
	if sa == nil || strings.TrimSpace(sa.Params["ValidTgts"]) == "" {
		return true
	}
	// A pending {X} is announced before targets, so a bare X bound cannot be
	// judged at offer time. Keep that shape offerable and let targetAsk use the
	// settled value. SVar-backed bounds remain readable now and are checked.
	// Read by literal key: the param census's rot guard rejects a dynamic
	// Params key that is not a function parameter.
	if xPending && (strings.EqualFold(strings.TrimSpace(sa.Params["TargetMin"]), "X") ||
		strings.EqualFold(strings.TrimSpace(sa.Params["TargetMax"]), "X")) {
		return true
	}
	min, _ := e.resolvedTargetBounds(p, id, sa, x)
	// CR 702.168a: the Gift election has not been made yet when this census
	// runs, so a bound that reads Count$PromisedGift is feasible if EITHER
	// branch of the promise is satisfiable. The union is a min of the two
	// branches' minimums: the census compares candidates against min, and a
	// smaller min is satisfiable exactly when at least one branch is. The
	// post-election ask (and every other reader) still resolves the ELECTED
	// branch through the nil-override resolvedTargetBounds, so promising
	// remains what switches the actual bound.
	if targetBoundReadsPromisedGift(e.G.Obj(id), sa) {
		promised := true
		if pmin, _ := e.resolvedTargetBoundsWithGift(p, id, sa, x, &promised); pmin < min {
			min = pmin
		}
	}
	// The census is a pure read; the two answers that never look at the
	// population return before walking it.
	if min <= 0 {
		return true
	}
	if xPending && specNamesXBound(sa.Params["ValidTgts"]) {
		return true
	}
	if !targetCrossConstrained(sa) {
		// No cross-target constraint: targetChoiceFeasible reduces to
		// len(candidates) >= min, which the limited census answers exactly
		// (candidatesForLimit stops at min only when no post-filter can drop
		// a candidate) without matching the rest of the population -- an
		// any-target spell stops at the player seats.
		ok := len(e.candidatesForLimit(p, id, excludeSelf, sa, true, min)) >= min
		if walkCacheVerify && ok != e.targetChoiceFeasible(sa, e.legalTargetCandidates(p, id, excludeSelf, sa), min) {
			panic("rules: limited target census disagrees with the full census")
		}
		return ok
	}
	candidates := e.legalTargetCandidates(p, id, excludeSelf, sa)
	return e.targetChoiceFeasible(sa, candidates, min)
}

// targetCrossConstrained reports whether sa carries a cross-target
// constraint targetChoiceFeasible reads beyond the candidate count: per-
// controller exclusivity (oneEachTargetBounds), same-controller grouping
// (sameControllerTargetBounds) or a set-property constraint
// (setPropTargetBounds). Each of those helpers is the identity on min when
// its predicate here is false.
func targetCrossConstrained(sa *cards.SA) bool {
	if targetControllerExclusive(sa) || strings.EqualFold(sa.Params["TargetsWithSameController"], "True") {
		return true
	}
	mode, _ := targetSetPropMode(sa)
	return mode != decision.SetPropNone
}

// targetChoiceFeasible is the ONE home for whether a target declaration with
// the given candidate population admits at least one legal selection under
// every cross-target constraint the ask enforces: the resolved mandatory
// minimum (with TargetMin$/TargetMax$ OneEach respelled to the
// distinct-controller count), TargetsForEachPlayer$/TargetsWithDifferentControllers$
// exclusivity, TargetsWithSameController$ group capacity, and the
// set-property capacity. Both the offer census (targetSAAvailable) and the
// post-push cast ask (cast.go's targetAsk) call it against their OWN candidate
// list, so an offered cast can always announce a legal target and the two sites
// cannot drift. Cost-driven candidate pruning stays the ask's alone: only it
// can re-price a target-dependent cost modifier.
func (e *Engine) targetChoiceFeasible(sa *cards.SA, candidates []targetCandidate, min int) bool {
	if min <= 0 {
		return true
	}
	min, _, exclusive, distinct := e.oneEachTargetBounds(sa, candidates, min, 0)
	min, _, sameCapacity, sameController := e.sameControllerTargetBounds(sa, candidates, min, 0)
	min, _, setCapacity, setMode, _ := e.setPropTargetBounds(sa, candidates, min, 0)
	if len(candidates) < min {
		return false
	}
	if exclusive && min > distinct {
		return false
	}
	if sameController && min > sameCapacity {
		return false
	}
	if setMode != decision.SetPropNone && min > setCapacity {
		return false
	}
	return true
}

// charmTargetsAvailable evaluates the possible CR 601.2b mode announcement
// before a cast is offered. Target-bearing modes with an unsatisfiable
// mandatory minimum are removed from the possible announcement set, using
// the same target census castModeAsk uses after the spell reaches the stack.
// A mode whose target count depends on an announcement remains offerable.
func (e *Engine) charmTargetsAvailable(p state.PlayerID, id state.ObjID, sa *cards.SA, xPending bool) bool {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return true
	}
	choices := strings.Split(sa.Params["Choices"], ",")
	ctx := &effects.Ctx{Source: id, Controller: p}
	effects.SetSVars(ctx, o.Face().SVars)
	legal := make([]string, 0, len(choices))
	for _, name := range choices {
		name = strings.TrimSpace(name)
		sub := cards.ResolveSVar(o.Face().SVars, name)
		if sub != nil && strings.TrimSpace(sub.Params["ValidTgts"]) != "" &&
			!e.targetSAAvailable(p, id, id, sub, 0, xPending) {
			continue
		}
		legal = append(legal, name)
	}
	legal = effects.CharmEligibleModes(e, id, sa, legal)
	min, _, repeat := effects.CharmModeBounds(e, ctx, sa, len(legal))
	return repeat || min <= len(legal)
}

// targetsAvailable reports whether the target requirement that can be proved
// before offering a spell or ability is satisfiable. Dynamic announcements
// remain offerable until the post-announcement targetAsk backstop evaluates
// them. excludeSelf is the CR 115.5 object for a spell, or zero for an
// activated ability whose Source permanent may target itself.
// targetsReadXPending reports whether targetsAvailable(sa) reads its
// xPending argument at all: only the Choices$ charm census and a
// ValidTgts$ census behind no Announce$ do. Every other shape answers from
// sa alone, so an offer gate may skip pricing the X announcement (two parsed
// costs per card) for it.
func targetsReadXPending(sa *cards.SA) bool {
	if sa == nil {
		return false
	}
	if sa.API == "Charm" && strings.TrimSpace(sa.Params["Choices"]) != "" {
		return true
	}
	return strings.TrimSpace(sa.Params["Announce"]) == "" && strings.TrimSpace(sa.Params["ValidTgts"]) != ""
}

func (e *Engine) targetsAvailable(p state.PlayerID, id, excludeSelf state.ObjID, sa *cards.SA, xPending bool) bool {
	if sa == nil {
		return true
	}
	if sa.API == "Charm" && strings.TrimSpace(sa.Params["Choices"]) != "" {
		return e.charmTargetsAvailable(p, id, sa, xPending)
	}
	// An Announce$ value can change the target restriction itself; its value
	// is not available until the cast transaction reaches the announcement
	// stage, so retain the post-announcement backstop for that shape.
	if strings.TrimSpace(sa.Params["Announce"]) != "" {
		return true
	}
	if strings.TrimSpace(sa.Params["ValidTgts"]) == "" {
		return true
	}
	return e.targetSAAvailable(p, id, excludeSelf, sa, 0, xPending)
}

// costAnnouncesX reports whether paying this cost announces a value for {X}
// before targets are chosen: a printed {X} mana symbol or a PayEnergy<X>
// energy part (Forge announces both through the same ability X). A cost that
// announces X makes every ValidTgts$ bound that reads that X (cmcEQX and its
// siblings) a DYNAMIC bound -- the same carve-out TargetMin$/TargetMax$/
// Announce$/Choices$ already have -- so the offer gate does not withhold the
// action on a bound whose value does not exist yet; the post-announcement
// askTarget backstop evaluates it once the X is fixed (CR 601.2b before
// 601.2c).
// costAnnouncesX reports whether the cost announces an X the cast or
// activation chooses (CR 601.2b/107.3i): a printed {X} mana symbol, a
// PayEnergy<X> part, an announced PayLife<X> payment, an announced
// SubCounter<X/Kind> removal, or a tapXType<X/Spec> part whose tap election
// announces it (the dynTapCost head). The tap-election clause does not add
// the announced-Sac clause: this gate's callers treat a true answer as "the
// X-bound targets are dynamic -- offer and evaluate at the ask", and the
// tap election is announced BEFORE the target ask (the tap stage precedes
// targetAsk in continueCast), so the bound is already fixed when targets
// are chosen either way; the clause only stops the offer gate from
// withholding the action on a bound whose value the tap election will
// supply (Aryel's powerLEX).
func costAnnouncesX(c Cost) bool {
	if c.X > 0 {
		return true
	}
	for _, part := range c.Energy {
		if part.Spec == "X" {
			return true
		}
	}
	if len(c.LifeX) > 0 {
		return true
	}
	for _, part := range c.SubCounter {
		if part.Announced {
			return true
		}
	}
	for _, part := range c.TapPermanent {
		if part.Dyn == "X" {
			return true
		}
	}
	// An announced Blight<X> part binds the cast's X exactly like Sac<X/Spec>
	// (Blighted Nightmare's `Blight<X> Return<1/CARDNAME>` ability): the
	// payment settles the announced count, so the X was announced even when
	// its value is 0.
	for _, part := range c.Blight {
		if part.Announced {
			return true
		}
	}
	return false
}

// specNamesXBound reports whether a ValidTgts$ spec carries a numeric bound
// whose right-hand side is the paid {X}: the <field><CMP>X family numericPred
// resolves through SpecContext.Resolve (powerGEX, cmcEQX, toughnessLTX,
// counters_GTX_<KIND>). Only these shapes are dynamic in X; a literal bound
// (cmcGE3) is static and stays gated at offer time.
var xBoundRe = regexp.MustCompile(`(?i)(power|toughness|cmc)(LE|GE|EQ|LT|GT)X|counters_(?:LE|GE|EQ|LT|GT)X_`)

func specNamesXBound(spec string) bool {
	return xBoundRe.MatchString(spec)
}

// castTargetsAvailable is the cast-offer guard: the spell card may not target
// itself (CR 115.5), so excludeSelf is the card id. A cost that announces an
// X (printed {X} or a SpellAbility Cost$ PayEnergy<X>) relaxes an X-bound
// spec to the post-announcement backstop (costAnnouncesX above).
func (e *Engine) castTargetsAvailable(p state.PlayerID, id state.ObjID, sa *cards.SA) bool {
	xPending := false
	if !targetsReadXPending(sa) {
		// xPending is never read for this shape (targetsReadXPending).
		return e.targetsAvailable(p, id, id, sa, false)
	}
	if o := e.G.Obj(id); o != nil && o.Face() != nil {
		xPending = costAnnouncesX(e.faceCost(o.Face()))
		if ab := o.Face().SpellAbility(); ab != nil {
			xPending = xPending || costAnnouncesX(e.parseCost(ab.Params["Cost"]))
		}
	}
	return e.targetsAvailable(p, id, id, sa, xPending)
}

// abilityTargetsAvailable is the activated-ability offer guard. It is what
// stops an ability with no legal target from being re-offered in a loop after
// the transaction aborts it (CR 602.2b / 601.2c: such an ability cannot be
// activated at all). Unlike a cast, an activated ability CAN target its own
// Source permanent (Mother of Runes targeting itself), so no self-exclusion
// applies -- EXCEPT for an attach ability (API$ Attach, the Equip/Reconfigure
// expansion): CR 701.3a attaches to ANOTHER permanent, and targetAsk
// excludes the source from an attach SA's pool, so the offer must exclude it
// too. Without that, a creature that GAINED an Equip ability (Trazyn the
// Infinite with an Equipment in its owner's graveyard) as the only creature
// its controller had was offered an Equip whose ask then found no legal
// target and aborted, forever (cardfuzz batch5 line 1). An ability cost that
// announces an X (a {X} mana symbol or PayEnergy<X>) relaxes an X-bound spec
// to the post-announcement backstop.
func (e *Engine) abilityTargetsAvailable(p state.PlayerID, id state.ObjID, ab *cards.SA) bool {
	var excludeSelf state.ObjID
	if ab != nil && ab.API == "Attach" {
		excludeSelf = id
	}
	if !targetsReadXPending(ab) {
		// xPending is never read for this shape (targetsReadXPending).
		return e.targetsAvailable(p, id, excludeSelf, ab, false)
	}
	return e.targetsAvailable(p, id, excludeSelf, ab, costAnnouncesX(e.parseCost(ab.Params["Cost"])))
}

// grantedAbility is one ability a continuous ability grant (CR 613.1f,
// state.ContinuousEffect.AddAbilities -- a Saga chapter's Animate) gives an
// object right now: the parsed AB and the SVar name on the granting face's
// table that re-resolves it.
type grantedAbility struct {
	sa *cards.SA
	// source is the object the grant came from (state.ContinuousEffect.Source):
	// the static's own permanent, which need not be the affected object the
	// ability is activated from. It is threaded into decision.Option.GrantSource
	// so the activation resolves the SVar body from here while the minted
	// ability's Source stays the recipient.
	source state.ObjID
	svar   string
	// gained marks an ability granted off a FOREIGN card's compiled face
	// (state.ContinuousEffect.GainedFaces): sa is that card's own ability,
	// gainedFrom is the foreign object id (in the scoped zone) and
	// gainedIdx is the index of sa in that face's Abilities. The activation
	// mints through GainedAbilityPush, which names both so a replay
	// re-resolves the identical SA; a zero gainedFrom means the ordinary
	// SVar-anchored grant. The grant's GainsValidAbilities$ filter and
	// GainsAbilitiesLimitPerTurn$ cap are applied at collection (inside
	// grantedAbilities), the one home both the offer loop and the mana
	// collector read, so no consumer can widen the grant.
	gained     bool
	gainedFrom state.ObjID
	gainedIdx  int
	// gainedFace is the foreign face sa was compiled on (gained only): the
	// SVar table a gained mana ability resolves against (gainedManaRef).
	gainedFace *cards.Face
}

// grantedAbilities collects the activated abilities the battlefield's
// AddAbilities grants give id right now. Each entry is gated on the granting
// effect actually applying to id (its Affects spec, "You" bound to the
// effect's own controller) and re-resolved against the granting source's
// face SVar table -- the name travels, never a parsed copy, so a replay
// re-executing the grant reads the identical body. Only AB$ lines grant;
// a name whose body is missing or is not an AB degrades to no grant (the
// same totality stance every SVar resolution takes). Order: active()'s own
// stable layer/timestamp sort, names in the grant's own order.
func (e *Engine) grantedAbilities(p state.PlayerID, id state.ObjID) []grantedAbility {
	var out []grantedAbility
	ces := e.active()
	// No grant anywhere in the list is one board-wide fact per active()
	// build (active_summary.go): skip the per-object scan outright. The scan
	// itself reads each entry in place rather than copying it.
	if !e.activeSummaryOf(ces).hasGrants {
		return nil
	}
	for i := range ces {
		ce := &ces[i]
		if len(ce.AddAbilities) == 0 && len(ce.GainedFaces) == 0 {
			continue
		}
		if !e.matchesSpecFrom(ce.Affects, id, ce.Controller, ce.Source) {
			continue
		}
		// A has-all-abilities-of grant (GainsAbilitiesOf$): each named
		// foreign face's own compiled Abilities are the recipient's to
		// activate -- ACTIVATED abilities only (the parameter means exactly
		// that; the triggered half rides GainedTriggerFaces and the
		// granted-trigger walk reads only that). The face's `Abilities` slice
		// holds only AB-kind SAs (cards' parser appends A: lines as AB/SP/ST
		// kinds; a gained activated ability is the AB ones), matched by the
		// same isManaAbilityAPI split the offer loop applies, so a gained
		// mana ability flows through the payment path like any other. The
		// grant's GainsValidAbilities$ filter (Sharkey's
		// `Activated.!ManaAbility`, Nicol Bolas Dragon-God's
		// `Activated.Loyalty`) and its GainsAbilitiesLimitPerTurn$ per-turn
		// cap are applied HERE -- the one home both the offer loop and the
		// mana collector (rules/mana_activation.go) read, so no consumer can
		// widen the grant. The index is the face-local position, which
		// GainedAbilityPush re-resolves against the same face a replay
		// rebuilds.
		for _, gf := range ce.GainedFaces {
			if gf.Face == nil {
				continue
			}
			for i, ab := range gf.Face.Abilities {
				if ab == nil || ab.Kind != "AB" {
					continue
				}
				if !e.gainsValidAbilitiesAdmits(ce.GainsValidAbilities, ab) {
					continue
				}
				if ce.GainsLimitPerTurn > 0 &&
					e.gainedActivationsThisTurn(id, gf.Obj, i) >= ce.GainsLimitPerTurn {
					continue
				}
				out = append(out, grantedAbility{sa: ab, source: ce.Source,
					gained: true, gainedFrom: gf.Obj, gainedIdx: i, gainedFace: gf.Face})
			}
		}
		if len(ce.AddAbilities) == 0 {
			continue
		}
		src := e.G.Obj(ce.Source)
		if src == nil || src.Face() == nil {
			continue
		}
		for _, nm := range ce.AddAbilities {
			svars := ce.SVars
			grantor := ce.AbilityGrantor
			if grantor == 0 {
				grantor = ce.Source
			}
			if svars == nil {
				svars = src.Face().SVars
			}
			ab := cards.ResolveSVar(svars, nm)
			if ab == nil || ab.Kind != "AB" {
				continue
			}
			out = append(out, grantedAbility{sa: ab, source: grantor, svar: nm})
		}
	}
	return out
}

// gainsValidAbilitiesAdmits reports whether the gained activated ability ab is

// grantedCyclingLines returns the DERIVED cycling keyword lines
// (cards.KeywordHead "Cycling" or "TypeCycling") id carries right now that no
// compiled face of its pile already expands. A printed K: line is expanded at
// link time into the pile's own abilities (the pile walk offers that one); a
// layer-6 AddKeyword$ grant (CR 613.1f -- Tectonic Reformation, Rhet-Tomb
// Mystic, Jo Grant, Homing Sliver) exists only in the derived keyword list and
// needs the synthesis the offer's keyword-cycling block runs. Coverage is
// decided against the pile's compiled facts in both directions -- a face whose
// own keyword list holds the line (the expansion the link built, or one the
// printed-A:-line guard suppressed) and a compiled ability tagged
// KeywordLine = the line -- so a card that both prints and is granted the same
// line is offered once. Duplicate derived entries (two identical grants)
// collapse to one; distinct lines (Cycling:2 and Cycling:R) each offer, the
// way two distinct printed K:Cycling lines would. Deterministic: Derived's
// slice order, dedup by first occurrence -- no map range reaches the list.
func (e *Engine) grantedCyclingLines(id state.ObjID) []string {
	if !e.mayDeriveCyclingLine(id) {
		if derivedMemoVerify {
			if got := e.grantedCyclingLinesFull(id); len(got) != 0 {
				panic(fmt.Sprintf("rules: cycling precheck skipped obj %d but the derived walk grants %q", id, got))
			}
		}
		return nil
	}
	return e.grantedCyclingLinesFull(id)
}

// mayDeriveCyclingLine is grantedCyclingLines' exact precheck
// (keywordmay.go): without a possible Cycling/TypeCycling head in the derived
// list the answer is nil, and the offer walk skips the full layer walk it
// otherwise paid for every card in every offered zone.
func (e *Engine) mayDeriveCyclingLine(id state.ObjID) bool {
	return e.mayHaveDerivedKeywordAnyH(id, kwhCycling, kwhTypeCycling)
}

// grantedCyclingLinesFull is grantedCyclingLines without the precheck.
func (e *Engine) grantedCyclingLinesFull(id state.ObjID) []string {
	var out []string
	for _, k := range e.Derived(id).Keywords {
		switch cards.KeywordHead(k) {
		case "Cycling", "TypeCycling":
		default:
			continue
		}
		dup := false
		for _, have := range out {
			if strings.EqualFold(have, k) {
				dup = true
				break
			}
		}
		if dup || e.printedCyclingCovered(id, k) || cards.GrantedCyclingAbility(k) == nil {
			continue
		}
		out = append(out, k)
	}
	return out
}

// verifyCyclingHandOnly panics when a derived cycling line of an object
// outside the hand synthesizes an ability whose zone gate would admit it:
// the offer walk skips the read there because every synthesized cycling
// body is a hand ability.
func verifyCyclingHandOnly(lines []string, id state.ObjID, z state.Zone) {
	for _, line := range lines {
		if ab := cards.GrantedCyclingAbility(line); ab != nil && abilityZoneOK(ab, z) {
			panic(fmt.Sprintf("rules: cycling line %q on obj %d is activatable from zone %v", line, id, z))
		}
	}
}

// printedCyclingCovered reports whether a compiled face of id's pile already
// carries the cycling keyword line -- either as a keyword entry of its own
// (the printed K: line the link expanded, or one the printed-A:-line guard
// suppressed) or as a compiled ability tagged KeywordLine = line. A stale
// object reads covered (withhold), the offer walk's degradation direction.
func (e *Engine) printedCyclingCovered(id state.ObjID, line string) bool {
	o := e.G.Obj(id)
	if o == nil {
		return true
	}
	for i := 0; i < o.PileFaceCount(); i++ {
		pf, ok := o.PileFaceAt(i)
		if !ok || pf.Face == nil {
			continue
		}
		for _, k := range pf.Face.Keywords {
			if strings.EqualFold(k, line) {
				return true
			}
		}
		for _, ab := range pf.Face.Abilities {
			if strings.EqualFold(ab.Params["KeywordLine"], line) {
				return true
			}
		}
	}
	return false
}

// gainsValidAbilitiesAdmits reports whether the gained activated ability ab is
// inside the granting static's GainsValidAbilities$ filter. The filter is
// comma alternatives, each `Activated` plus optional dot qualifiers, and an
// ABSENT filter admits everything. The corpus vocabulary (measured over the
// 31 GainsAbilitiesOf files): `Activated` (Drana and Linvala),
// `Activated.!ManaAbility` (Sharkey), `Activated.!Loyalty` (Scheming Fence),
// `Activated.Loyalty` (Nicol Bolas Dragon-God, Kasmina). A qualifier this
// build does not model fails closed -- that alternative admits nothing -- the
// filter convention every spec reader takes, so an unknown restriction can
// never widen the grant. The base word itself must be `Activated`
// (case-insensitive): a differently-named base admits nothing.
func (e *Engine) gainsValidAbilitiesAdmits(spec string, ab *cards.SA) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return true
	}
	for alt := range strings.SplitSeq(spec, ",") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		// The token splits on the FIRST dot: the base kind, then the qualifier
		// tail (kept whole -- a qualifier this grammar does not name fails
		// closed below rather than being silently dropped).
		dot := strings.IndexByte(alt, '.')
		base, tail := alt, ""
		if dot >= 0 {
			base, tail = alt[:dot], alt[dot+1:]
		}
		if !strings.EqualFold(base, "Activated") {
			continue
		}
		ok := true
		for q := range strings.SplitSeq(tail, ".") {
			switch strings.TrimSpace(q) {
			case "":
				// A trailing dot ("Activated."): no qualifier, vacuous.
			case "!ManaAbility":
				ok = ok && !isManaAbilityAPI(ab.API)
			case "!Loyalty":
				ok = ok && !e.isLoyaltyAbility(ab)
			case "Loyalty":
				ok = ok && e.isLoyaltyAbility(ab)
			default:
				// Unmodelled qualifier: fail closed for this alternative.
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// gainedActivationsThisTurn counts how many times the ACTIVATED ability at
// foreign face-local index idx of the foreign card `foreign` has been
// activated FROM the affected object id this turn. GainedAbilityPush events
// name all three (Obj = recipient, IDs[0] = foreign card, Amount = index), so
// the replayable log is the memory -- the loyaltyActivationsThisTurn fold
// pattern. TurnChange resets the count (the limit is per turn, not per
// battlefield stint: Mairsil's caged card sits in exile and never changes
// zones while the count matters, and the identity is the foreign ability,
// not the recipient).
func (e *Engine) gainedActivationsThisTurn(id, foreign state.ObjID, idx int) int {
	used := 0
	for _, ev := range e.L.Events {
		switch ev.Kind {
		case events.TurnChange:
			if int(ev.Player) < len(e.G.Players) {
				used = 0
			}
		case events.GainedAbilityPush, events.ManaActivate:
			// A gained MANA ability never goes on the stack, so its
			// activation is the ManaActivate marker carrying the same
			// (recipient, foreign card, index) triple (gainedManaRef); a
			// printed mana marker carries no IDs and never matches.
			if ev.Obj == id && len(ev.IDs) > 0 && ev.IDs[0] == foreign && int(ev.Amount) == idx {
				used++
			}
		}
	}
	return used
}

// adjustLandPlays reports how many land drops BEYOND the ordinary one
// (CR 305.2a) player p gets this turn: the SUM over the active
// additional-land-drops grants (Azusa, Oracle of Mul Daya, Exploration)
// whose Affects spec matches p -- the sum, never the max, because each
// grant's printed sentence modifies the one-drop normal independently
// (Azusa plus Exploration is three drops). Each grant's Affects is a
// PLAYER spec evaluated with MatchesPlayerSpecFrom against the granting
// effect's controller, so "Affected$ You" is the SOURCE's controller: a
// stolen Azusa grants its new controller, and a spec with a qualifier the
// matcher does not implement matches nobody (fail closed, no grant).
// "On each of your turns" is not evaluated here -- the offer gates only
// ever offer a play_land to the active player in a main phase, and the
// per-turn counter resets at the TurnChange untap. The walk is a pure read
// over active()'s sorted slice (never a map), so the resulting option list
// stays reproducible run to run.
func (e *Engine) adjustLandPlays(p state.PlayerID) int {
	total := 0
	ces := e.active()
	for i := range ces {
		ce := &ces[i]
		if ce.AdjustLandPlays <= 0 {
			continue
		}
		if !effects.MatchesPlayerSpecFrom(e.G, ce.Affects, p, ce.Controller, ce.Source) {
			continue
		}
		total += int(ce.AdjustLandPlays)
	}
	return total
}

// manaActivateLabel is the "Activate <name> for mana" option label, built
// once per card name per Engine: the offer walk mints it for every untapped
// mana source on every priority walk, and the string is immutable, so each
// walk's options can share it.
func (e *Engine) manaActivateLabel(name string) string {
	if l, ok := e.manaLabels[name]; ok {
		return l
	}
	l := "Activate " + name + " for mana"
	if e.manaLabels == nil {
		e.manaLabels = make(map[string]string)
	}
	e.manaLabels[name] = l
	return l
}

// existsOnBattlefield reports whether o is a permanent the engine treats as
// existing (CR 702.25b): it is on the battlefield and not phased out. A
// phased-out permanent is treated as though it does not exist -- it cannot be
// targeted (rules/stack.go candidatesFor), activated, tapped or sacrificed as
// a cost, its static and triggered abilities are off, and it does not stay in
// combat (events.Apply's PhaseOut fold removes it, CR 702.25c). PhasedOut is
// only ever true on a battlefield permanent (the PhaseOut fold is
// battlefield-gated, the Move fold clears it), so gating a walk that already
// restricts itself to the battlefield on it is exact. Battlefield action
// and cost walks use this helper; mana-ability discovery and trigger scanning
// separately reject phased-out objects. Other readers must gate where relevant.
func existsOnBattlefield(o *state.Object) bool {
	return o != nil && o.Zone == state.ZBattlefield && !o.PhasedOut
}

// legalActions enumerates everything p may legally do with priority. The
// result is the complete rules surface a client ever sees.
func (e *Engine) legalActions(p state.PlayerID) []decision.Option {
	return e.legalActionsPriced(p, nil)
}

func (e *Engine) legalActionsWithWindow(p state.PlayerID, w *windowCollector) []decision.Option {
	return e.legalActionsWalkWithWindow(p, nil, false, w)
}

// aftermathAlternateFace returns the Aftermath alternate face (face 1 --
// ALTERNATE starts face 1 in cards/parse.go) of a two-face Split card whose
// front face is current, or nil when the object is not a well-formed
// aftermath carrier: AlternateMode must be Split (a Room is Split too, but
// no Room half carries K:Aftermath, and the keyword gate is what keeps Rooms
// and Adventures on their own paths), the object must have exactly two
// faces, and the card must still be at its front face -- the aftermath half
// is cast only from a graveyard card whose printed front is showing.
func aftermathAlternateFace(o *state.Object) *cards.Face {
	if o == nil || o.Card == nil || o.Card.AlternateMode != "Split" || len(o.Card.Faces) != 2 || int(o.FaceIdx) != 0 {
		return nil
	}
	af := o.Card.Faces[1]
	if af == nil || !af.HasKeyword("Aftermath") {
		return nil
	}
	return af
}

// legalActionsPriced is legalActions with the mana affordability priced
// against an OVERBOUND hypothetical pool instead of the seat's floating one:
// hyp nil keeps the ordinary floating-pool pricing, hyp non-nil prices every
// cast/activation gate against *hyp -- the pool the seat would hold if it
// first floated every mana its untapped sources could produce (PotentialMana).
// The walk itself is otherwise IDENTICAL: same zones (hand, command zone,
// graveyard flashback, battlefield abilities), same timing, restriction,
// target and non-mana-cost gates, same live RaiseCost/ReduceCost composition
// (offerCostFor) -- so a potential action is by construction the same option
// the engine WOULD offer once the mana floated, never a client-side
// re-derivation that can drift from the engine's own cost rules.
//
// It is a pure read: no event is emitted, no state field is written, and the
// hypothetical pool lives only in local copies, so replay is untouched.
func (e *Engine) legalActionsPriced(p state.PlayerID, hyp *state.Mana) []decision.Option {
	return e.legalActionsWalk(p, hyp, false)
}

// legalActionsWalk is legalActionsPriced's body. castsOnly skips the
// sections that append only non-cast options -- mana activations, activated
// and granted abilities, station, unlock, turn face up and specialize -- so
// its result is exactly legalActionsPriced's "cast" options, in the same
// order with the same labels, objects, modes and costs (their Index values
// differ: the skipped options never took a slot). Every cast section runs
// first and reads nothing the skipped sections write, and the post-walk
// filters (no-mana-cost, inert hold-out, split second) decide each option
// on its own. The payment offer builder, which reads only plain casts, uses
// it to avoid pricing every battlefield ability it would discard.
func (e *Engine) legalActionsWalk(p state.PlayerID, hyp *state.Mana, castsOnly bool) []decision.Option {
	return e.legalActionsWalkWithWindow(p, hyp, castsOnly, nil)
}

func (e *Engine) legalActionsWalkWithWindow(p state.PlayerID, hyp *state.Mana, castsOnly bool, window *windowCollector) []decision.Option {
	// Count the walk before anything can early-return. A test-visible
	// diagnostic only: no event, no state mutation, no effect on replay or
	// chain heads (legalActionWalks is not copied by Clone and never reaches
	// a view, an option list or a log).
	e.legalActionWalks++
	// The walk is a pure read, so every Derived it makes is memoized for the
	// walk's duration (rules/derivedmemo.go).
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	// Build into the engine's scratch list (legalOptBuf) and hand the caller
	// an exactly-sized copy at the end: the growth reallocations stay on the
	// reusable buffer, never on the returned, retained Options.
	out := e.legalOptBuf[:0]
	e.legalOptBuf = nil
	add := func(kind, label string, obj state.ObjID) {
		out = append(out, decision.Option{Index: len(out), Kind: kind, Label: label, Obj: obj})
	}
	sorcery := e.sorcerySpeed(p)
	// hyp is the pricing mode the walk runs in: nil is the ordinary real-pool
	// offer walk; non-nil is the potential-action walk's hypothetical bound.
	// The two affordability gates below are the ONLY pricing difference:
	// every non-mana part (Sac/Discard/SubCounter/Tap) is checked against the
	// REAL state in both modes -- floating mana never satisfies a sacrifice --
	// and the cost composition (offerCostFor) is pool-independent, so the
	// two walks cannot drift inside the body they share.
	costStatics := costStaticSource{e: e}
	actionStatics := actionStaticSource{e: e}
	castRestricted := func(p state.PlayerID, id state.ObjID) bool {
		return e.castRestrictedUsing(actionStatics.get().cantCast, p, id)
	}
	abilityRestricted := func(p state.PlayerID, id state.ObjID, ab *cards.SA) bool {
		return e.abilityRestrictedUsing(actionStatics.get().cantActivate, p, id, ab)
	}
	offerCastable := func(p state.PlayerID, id state.ObjID, base Cost, scope costScope, ability bool) bool {
		return e.offerCastableUsing(costStatics.get(), p, id, base, scope, ability, hyp)
	}
	// offerCastableAsFace is offerCastable for an alternate-face cast route
	// (adventure_alt, adventure_recast, room_alt, split_alt, aftermath):
	// beginCast flips the object to `face` before pricing, so the gate prices
	// with the object showing that face too (faceprobe.go) -- otherwise a
	// modifier reading the card's characteristics (Thalia's
	// Card.nonCreature) matches the wrong face and an unpayable cast is
	// offered, reversed and re-offered forever. The walk's cached statics are
	// fetched BEFORE the probe (a lazy first collection inside it would cache
	// the probed face for the rest of the walk) and re-collected inside it
	// only when either face carries a cost-modifier static of its own.
	offerCastableAsFace := func(p state.PlayerID, id state.ObjID, face *cards.Face, base Cost, scope costScope) bool {
		statics := costStatics.get()
		var cur *cards.Face
		if o := e.G.Obj(id); o != nil {
			cur = o.Face()
		}
		return e.offerAsFace(id, face, func() bool {
			if faceHasCostStatics(cur) || faceHasCostStatics(face) {
				statics = e.collectCostStatics()
			}
			return e.offerCastableUsing(statics, p, id, base, scope, false, hyp)
		})
	}
	// castRestrictedAsFace is castRestricted for an alternate-face cast route
	// (modal_spell): the CantBeCast prohibition must be evaluated against the
	// face the cast flips to -- the same probe offerCastableAsFace prices
	// under, and the same face recheckIllegal (CR 601.2e) re-checks against
	// after beginCast's real FlipFace. CR 712.4d: while a Modal DFC's spell
	// is on the stack its characteristics are the chosen face's, so a
	// restriction that matches only one face decides only that face's offer
	// (a Card.nonCreature lockout withholds the sorcery back face but must
	// not touch the creature front, and ValidCard$ Creature the reverse).
	// The walk's cached CantBeCast statics are fetched BEFORE the probe: a
	// CantBeCast source is battlefield-only in collectActionStatics' walk and
	// the probed card sits in hand, so the collection is face-independent,
	// and a lazy first collection inside the probe would cache the probed
	// face for the rest of the walk (faceprobe.go). castRestrictionSources
	// re-reads the restricted card's OWN statics from o.Face() live, so a
	// back-face self-restriction is seen under the probe exactly as
	// recheckIllegal sees it after the real flip.
	castRestrictedAsFace := func(p state.PlayerID, id state.ObjID, face *cards.Face) bool {
		statics := actionStatics.get().cantCast
		return e.offerAsFace(id, face, func() bool {
			return e.castRestrictedUsing(statics, p, id)
		})
	}
	// affordable is the composed-cost gate the two direct e.castable sites of
	// the walk use (the may-play and escape walks price an already-composed
	// cost, so they cannot re-run the modifier composition offerCastable
	// owns); hyp==nil is the ordinary castable, hyp!=nil prices the same
	// composed cost against the hypothetical pool.
	affordable := func(q state.PlayerID, id state.ObjID, c Cost, ability bool) bool {
		if hyp == nil {
			return e.castable(q, id, c, ability)
		}
		return e.castablePriced(q, id, c, ability, *hyp)
	}
	offerCostFor := func(p state.PlayerID, id state.ObjID, base Cost, scope costScope) Cost {
		return e.offerCostForUsing(costStatics.get(), p, id, base, scope)
	}

	for _, id := range e.G.Zone(state.ZHand, p) {
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil {
			continue
		}
		// The printed keyword-cost heads this face can answer
		// (printed_heads.go): a clear bit skips that head's reader below.
		ph := printedHeadsOf(f)
		if derivedMemoVerify {
			verifyPrintedHeads(f, ph)
		}
		if f.IsLand() {
			if sorcery && e.G.Players[p].LandsPlayed < int32(1+e.adjustLandPlays(p)) {
				add("play_land", "Play "+f.Name, id)
				// A Modal DFC may also be played as its back land, even
				// when its front face is itself a land (CR 712.8).
				if back := modalLandBack(o); back != nil {
					out = append(out, decision.Option{Index: len(out), Kind: "play_land",
						Label: "Play " + back.Name, Obj: id, Mode: "modal_land"})
				}
			}
			// CR 702.37a/702.168a/702.169a: a LAND printed with the Morph/
			// Megamorph/Disguise keyword is ALSO castable face down for {3}
			// (Zoetic Cavern, Branch of Vitu-Ghazi).  That cast is a creature
			// SPELL, not a land play: it does not consume the land drop and
			// it goes on the stack (CR 305.1 "play a land" is a special
			// action), so it is offered even after the land play for the turn
			// has been used.  This block sits INSIDE the IsLand branch on
			// purpose: the front-face land must never acquire the ordinary
			// printed cast the nonland walk below offers, and only the
			// fixed-{3} face-down mode is added.  The same guards the existing
			// nonland face-down offer relies on are applied here explicitly
			// (this branch continues before the walk reaches them): the
			// castSuppressed/castRestricted prohibitions, the ordinary
			// spellTimingOK gate, and offerCastable's cost-modifier
			// affordability.  Like that offer it deliberately does NOT gate
			// on targetsAvailable (CR 708.4: a face-down spell has no targets)
			// and never charges the printed keyword parameter (the later
			// turn-face-up cost).
			if ph.has(phMorph | phMegamorph | phDisguise) {
				if fam := morphDownFamily(f); fam != "" && !e.castSuppressed(p, id) &&
					!castRestricted(p, id) && e.spellTimingOK(p, id, f, sorcery) &&
					offerCastable(p, id, Cost{Generic: 3}, spellScope(fam), false) {
					out = append(out, decision.Option{Index: len(out), Kind: "cast",
						Label: "Cast " + f.Name + " (face down)", Obj: id, Mode: fam})
				}
			}
			continue
		}
		// CR 712.8/712.4d: a Modal DFC in hand may be played as its back
		// face when that face is a land.  Keep this separate from the ordinary
		// front-face land path so its existing option remains byte-identical.
		if sorcery && e.G.Players[p].LandsPlayed < int32(1+e.adjustLandPlays(p)) {
			if back := modalLandBack(o); back != nil {
				out = append(out, decision.Option{Index: len(out), Kind: "play_land",
					Label: "Play " + back.Name, Obj: id, Mode: "modal_land"})
			}
		}
		if e.castSuppressed(p, id) {
			continue
		}
		// CR 712.8: a Modal DFC's nonland back face can be cast from hand
		// independently of its front face. The chosen face's timing, targets,
		// restriction and composed cost govern this offer; beginCast flips
		// provisionally. The prohibition gate is castRestrictedAsFace, not the
		// card-level castRestricted continue below: a restriction that matches
		// only the front face must not withhold the back-face offer, and one
		// that matches only the back face must not be missed by probing only
		// the front (both directions are regression-tested in
		// rules/modal_spell_face_restriction_test.go).
		if mf := modalSpellBack(o); mf != nil && e.spellTimingOK(p, id, mf, sorcery) &&
			e.castTargetsAvailable(p, id, mf.SpellAbility()) &&
			!castRestrictedAsFace(p, id, mf) {
			if offerCastableAsFace(p, id, mf, withSpellAbilityExtras(mf, e.faceCost(mf)), spellScope("")) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + mf.Name, Obj: id, Mode: "modal_spell"})
			}
		}
		// CR 709.4/709.5: a non-Room split card's alternate half is castable
		// on its own (mode split_alt, consumed by beginCast's FlipFace exactly
		// like room_alt) and, when the card carries K:Fuse, BOTH halves may be
		// cast as one fused spell (mode fuse, paying the combined cost and
		// resolving both halves). The split_alt offer reads the half's OWN
		// timing (review MAJOR 2), targets and cost through
		// splitCastTargetsAvailable, so a half with no legal target -- or one
		// the seat cannot pay for -- is withheld independently of the front
		// face. It stands ABOVE the front-face spellTimingOK gate below on
		// purpose: CR 709.4 gives each half its own timing, so a front-Sorcery
		// half must not withhold its instant alternate half during an
		// opponent's turn (incubation_incongruity, discovery_dispersal,
		// said_done, spring_mind). For that reason it also precedes the plain
		// cast in the option list for a split card.
		//
		// The prohibition gate is castRestrictedAsFace on the HALF being cast,
		// not the card-level castRestricted continue below (which reads the
		// front face). A restriction matching only the front half must not
		// withhold this half's offer, and one matching only this half must not
		// be missed by probing the front (the same two directions the
		// modal_spell offer fixed in e89e1a48b; regression-tested in
		// rules/alternate_face_restriction_test.go). castSuppressed above
		// stays shared: casting a half is casting the card.
		if sf := splitAlternateCastFace(o); sf != nil {
			// The target-conditional read is FACE-SCOPED (castWithFlashAsFace):
			// a grant on one half is judged against THAT half's own potential
			// targets and face-local statics, so the front half's grant cannot
			// offer the alternate half, and a qualifying alternate-half target
			// cannot be withheld because the front face is not a target. The
			// CR 601.2e recheck (recheckIllegal) re-runs the same face-scoped
			// read against the ANNOUNCED targets, so offer and enforcement
			// agree on one interpretation.
			instant := sf.IsInstant() || e.hasKeywordH(id, kwhFlash) || e.castWithFlashAsFace(p, id, sf)
			if (instant || sorcery) && e.splitCastTargetsAvailable(p, id, sf) &&
				!castRestrictedAsFace(p, id, sf) {
				if offerCastableAsFace(p, id, sf, withSpellAbilityExtras(sf, e.faceCost(sf)), spellScope("")) {
					out = append(out, decision.Option{Index: len(out), Kind: "cast",
						Label: "Cast " + sf.Name, Obj: id, Mode: "split_alt"})
				}
			}
		}
		// CR 714.3a: an Adventure spell face has its own timing and may be
		// cast from hand even when the creature front is not currently castable.
		// It stands above the front-face gate for the same reason split_alt
		// does, and its prohibition is probed against the ADVENTURE face it
		// casts (castRestrictedAsFace), not the creature front the card-level
		// continue below reads -- a restriction matching only one face must
		// decide only that face's offer.
		if int(o.FaceIdx) == 0 {
			if af := adventureSpellFace(o); af != nil && e.spellTimingOK(p, id, af, sorcery) &&
				e.castTargetsAvailable(p, id, af.SpellAbility()) && !castRestrictedAsFace(p, id, af) {
				if offerCastableAsFace(p, id, af, withSpellAbilityExtras(af, ParseCost(af.ManaCost)), spellScope("")) {
					out = append(out, decision.Option{Index: len(out), Kind: "cast",
						Label: "Cast " + af.Name, Obj: id, Mode: "adventure_alt"})
				}
			}
		}
		// CR 309.4b: either door of a Room may be cast. Mode room_alt is
		// consumed by beginCast, which records a FlipFace before the ordinary
		// cast transaction; from then on every cost/target/resolution reader
		// sees the selected face. Like the other alternate-face offers, this
		// precedes the front-face restriction gate and probes the door being
		// cast (rather than the displayed front face).
		if rf := roomAlternateCastFace(o); rf != nil {
			instant := rf.IsInstant() || e.hasKeywordH(id, kwhFlash)
			if (instant || sorcery) && e.castTargetsAvailable(p, id, rf.SpellAbility()) &&
				!castRestrictedAsFace(p, id, rf) {
				if offerCastableAsFace(p, id, rf, withSpellAbilityExtras(rf, e.faceCost(rf)), spellScope("")) {
					out = append(out, decision.Option{Index: len(out), Kind: "cast",
						Label: "Cast " + rf.Name, Obj: id, Mode: "room_alt"})
				}
			}
		}
		// The card-level continue withholds the ORDINARY front-face cast (and
		// every front-face alternative below it -- foretell, mayflash, kicker,
		// ...) when a CantBeCast restriction matches the front face. The
		// alternate-face offers above already ran, each probed against its own
		// face.
		if castRestricted(p, id) {
			continue
		}
		// CR 709.5: a fuse card's fused cast is a single spell with BOTH
		// halves' characteristics, so it is NOT an alternate-face cast: the
		// object stays at its front face (beginCast's fuse arm flips nothing).
		// The gate is therefore the front-face castRestricted continue just
		// above for the front half PLUS a castRestrictedAsFace probe for the
		// alternate half: a prohibition that matches either half's
		// characteristics withholds the fused offer (CR 709.5 -- one spell
		// with both halves' characteristics). Probing the alternate half is
		// the same scoped face read the split_alt offer uses and restores the
		// face before returning, so replay is untouched; it must NOT replace
		// the front-face continue, or a front-restricted card would offer its
		// fused cast. The recheck (CR 601.2e, cast.go's recheckIllegal) runs
		// the same both-halves rule so offer and enforcement agree.
		if ff, fa := fusedSplitFaces(o); ff != nil {
			if !castRestrictedAsFace(p, id, fa) &&
				e.fusedTimingOK(p, id, ff, fa, sorcery) &&
				e.splitCastTargetsAvailable(p, id, ff) && e.splitCastTargetsAvailable(p, id, fa) {
				if offerCastable(p, id, e.fuseCost(ff, fa), spellScope(""), false) {
					out = append(out, decision.Option{Index: len(out), Kind: "cast",
						Label: "Cast " + ff.Name + " // " + fa.Name + " (fused)", Obj: id, Mode: "fuse"})
				}
			}
		}
		// Foretell (CR 702.126a): the special action pays {2} and exiles the
		// card from the hand FACE DOWN -- never the keyword's own colon
		// parameter, which prices the LATER cast. The keyword read is DERIVED
		// (printed K:Foretell line plus a layer-6 AddKeyword$ Foretell grant --
		// Dream Devourer's "each nonland card in your hand without foretell has
		// foretell"), so a granted hand card gets the same {2} action; the
		// granted card's later cast prices through foretellCost's
		// printed-cost-less-{2} fallback, which IS the granted foretell cost
		// ("its foretell cost is equal to its mana cost reduced by {2}").
		// "During your turn" is the timing gate (deliberately NO
		// instant/sorcery-speed check, unlike Suspend -- CR 702.126a's action
		// text names only the turn, and a special action needs only priority,
		// never the ability to cast an instant), widened by a granted
		// `AddKeyword$ Foretell on any player's turn` player keyword (Cosmos
		// Charger). The block sits BEFORE the front-face timing gate: the
		// action's timing is its own turn window, never the card's cast
		// timing -- a creature with foretell is offered the action on any
		// step of its controller's turn (with a stack, in an upkeep), and
		// under the any-turn grant on any step of anyone's turn.
		// castRestricted/castSuppressed above still bound the offer.
		if _, ok := e.derivedKeywordParamH(id, kwhForetell); ok &&
			(e.G.Active == p || e.playerForetellsAnyTurn(p)) &&
			offerCastable(p, id, Cost{Generic: 2}, foretellScope(), false) {
			out = append(out, decision.Option{Index: len(out), Kind: "cast", Label: "Foretell " + f.Name, Obj: id, Mode: "foretell"})
		}
		if !e.spellTimingOK(p, id, f, sorcery) {
			// MayFlashCost (Forge's K:MayFlashCost, CR 702.8): when the ordinary
			// timing gate fails, a face printed with the keyword is NOT skipped
			// outright -- it may be cast at instant timing by paying the extra.
			// The mayflash offer below is the ONLY option this branch adds; the
			// plain cast and every other mode on the face stay behind the
			// sorcery-speed gate. When it is already sorcery timing the plain
			// cast is strictly cheaper, so no mayflash option is offered and the
			// face takes the ordinary path (no redundant duplicate offer).
			if ph.has(phMayFlashCost) && e.mayflashTimingOK(p, f) {
				if extra, ok := mayflashExtraCost(f); ok && e.castTargetsAvailable(p, id, f.SpellAbility()) {
					if offerCastable(p, id, withSpellAbilityExtras(f, e.castOfferBase(p, id)).Plus(extra), spellScope("mayflash"), false) {
						out = append(out, decision.Option{Index: len(out), Kind: "cast",
							Label: "Cast " + f.Name + " (may-flash)", Obj: id, Mode: "mayflash"})
					}
				}
			}
			continue
		}
		// targetsAvailable is the plain SpellAbility's target census, run on
		// first use and remembered for the rest of this card. Every offer
		// below that needs it tests it LAST among pure conjuncts, so a card
		// no offer can afford (the common case in a priority window) never
		// pays for the census; the walk is a pure read, so a late census
		// equals an eager one and each offer's answer is unchanged.
		taDone, taOK := false, false
		targetsAvailable := func() bool {
			if !taDone {
				taOK, taDone = e.castTargetsAvailable(p, id, f.SpellAbility()), true
			}
			return taOK
		}
		// offerCastable prices the MANA the offer will charge (601.2f
		// modifiers, then the commander tax) over the RAW base beginCast will
		// store; withSpellAbilityExtras adds the spell's own ADDITIONAL
		// non-mana parts on top. Both are needed and they compose in this
		// order: an additional cost is never reduced by a cost modifier, the
		// same reason the commander tax lands after the modifiers rather than
		// before them.
		//
		// The gate must see the SAME cost beginCast will charge, additional
		// non-mana parts included, or an unpayable cast gets offered and then
		// aborts having consumed nothing -- which, since the abort leaves the
		// board that produced the offer untouched, is an unbounded livelock
		// rather than a wasted click. That is exactly what Village Rites did
		// to a live 4-player game (see withSpellAbilityExtras in cast.go).
		// Only the plain cast folds the extras, matching beginCast's own
		// condition: the kicked/surged/flashback/miracle offers below set
		// Mode, and beginCast skips the fold for those.
		// convokeBase is the offer gate's composed RAW base: the printed mana
		// cost with CR 702.51 Convoke and CR 702.66 Improvise's generic credits.
		// castOfferBase is the shared recipe so the plain and mayflash offers
		// cannot drift (the mayflash branch above uses it too).
		convokeBase := e.castOfferBase(p, id)
		// An either-or additional cost (AlternateAdditionalCost) makes the
		// plain cast's gate existential: the cast is offerable when AT LEAST
		// ONE alternative part is payable (the choice itself is asked by the
		// cast flow, altAddAsk), never when all of them are unpayable. Cards
		// without the keyword keep the ordinary single-cost gate.
		var altParts []string
		if ph.has(phAlternateAdditionalCost) {
			altParts = altAddCostParts(f)
		}
		if len(altParts) > 0 {
			if targetsAvailable() {
				for _, part := range altParts {
					if offerCastable(p, id, withSpellAbilityExtras(f, convokeBase).Plus(e.parseCost(part)), spellScope(""), false) {
						add("cast", "Cast "+f.Name, id)
						break
					}
				}
			}
		} else if offerCastable(p, id, withSpellAbilityExtras(f, convokeBase), spellScope(""), false) && targetsAvailable() {
			add("cast", "Cast "+f.Name, id)
		}
		// Self-spell OptionalCost is a separate paid offer; the plain
		// cast above remains the decline path. Preserve static order.
		if views := e.optionalCostViews(costStatics.get(), p, id); len(views) > 0 && targetsAvailable() {
			for i, extra := range views {
				if offerCastable(p, id, withSpellAbilityExtras(f, convokeBase).Plus(extra), spellScope("optionalcost"), false) {
					out = append(out, decision.Option{Index: len(out), Kind: "cast",
						Label: "Cast " + f.Name + " (optional cost)", Obj: id, Mode: "optionalcost", AltCostIndex: i + 1})
				}
			}
		}
		for i, alt := range e.alternativeCosts(p, id) {
			if !targetsAvailable() {
				continue
			}
			// An announce-bearing alternative (the Shoal cycle's Announce$ X):
			// the exile filter's cmcEQX is bound to the value the caster will
			// announce, so the "some announcement is payable" gate is
			// existential over X — at least one exilable card must match at
			// SOME mana value. The gate below replaces offerCastable's
			// unannounced (X=0) exile check with that existential scan and
			// gates the mana-only remainder on the same cost minus its exile
			// parts; beginCast asks the X (xAsk's announce arm), then exAsk
			// re-walks the part with the announced X bound.
			gate := alt.cost
			if alt.announce != "" {
				gate.Exile = nil
				if len(e.altCostXCandidates(p, id, alt)) == 0 {
					continue
				}
			}
			// Ruling (Task 9 fix round 1, Important 1): this used to gate on
			// mana-only alt.CanPay, but ParseCost now produces Sac/SubCounter/
			// Tap parts that the cast flow enforces -- an AlternativeCost whose
			// Cost$ carries Sac<N/...> was offered without checking that N
			// matching permanents exist, and beginCast then asked a sacrifice
			// decision with zero options that no answer could escape. castable
			// is the same gate every other "cast" option uses.
			if offerCastable(p, id, gate, spellScope(""), false) {
				// AltCostIndex is i+1, not i: the zero value must mean "the
				// card's own cost" so every other Option literal in the tree
				// (play_land, activate, pass, and the base "cast" option
				// added just above via the shared add closure) needs no
				// change to keep meaning that.
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: altCostLabel(f.Name, i), Obj: id, AltCostIndex: i + 1})
			}
		}
		// The and/or Kicker (Forge's colon-separated two-part Kicker:<a>:<b>,
		// Wastescape Battlemage's "Kicker {G} and/or {1}{U}"): each part is an
		// independent optional additional cost (CR 601.2b), so each payable
		// combination is its own cast option -- part 1, part 2, or both. The
		// modes ride FlagKicked1/FlagKicked2 (modeFlags), which the
		// "Card.Self+kicked <n>" trigger and replacement specs read.
		if ph.has(phKicker) {
			if c1, c2, ok := twoPartKickerCosts(f); ok {
				base := e.rawBaseCost(p, id)
				for _, kp := range [...]struct {
					mode, label string
					cost        Cost
				}{
					{"kicked1", "Cast " + f.Name + " (kicked 1)", c1},
					{"kicked2", "Cast " + f.Name + " (kicked 2)", c2},
					{"kickedboth", "Cast " + f.Name + " (kicked both)", c1.Plus(c2)},
				} {
					if targetsAvailable() && offerCastable(p, id, base.Plus(kp.cost), spellScope(kp.mode), false) {
						out = append(out, decision.Option{Index: len(out), Kind: "cast",
							Label: kp.label, Obj: id, Mode: kp.mode})
					}
				}
			} else if kc, ok := kickerCost(f); ok && targetsAvailable() && offerCastable(p, id, e.rawBaseCost(p, id).Plus(kc), spellScope("kicked"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (kicked)", Obj: id, Mode: "kicked"})
			}
		}
		if ph.has(phSurge) {
			if sc, ok := surgeCost(f); ok && targetsAvailable() && e.spellsCastThisTurn(p) > 0 && offerCastable(p, id, sc, spellScope("surged"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (surged)", Obj: id, Mode: "surged"})
			}
		}
		// Entwine (CR 702.42a): "You may pay an additional [cost] as you cast
		// this spell." It is an optional additional cost paid ON TOP of the
		// printed cost (the buyback/offspring shape), so the entwined variant
		// is its own cast option priced base+cost through offerCastable's
		// shared gate. Unlike Escalate it is a flat cost, not a per-extra-mode
		// charge, so the offer needs no mode-count clamp and the magic is all
		// in beginCast's fold plus castModeAsk's all-modes announcement. The
		// offer is gated on the card actually having a Charm spell ability:
		// Entwine's ONLY modelled meaning is "choose all modes", so charging
		// its cost for a face with no modal body (no corpus carrier -- all 32
		// Entwine files are Charms) would take mana for nothing; such a face
		// keeps its printed-cast path and its coverage gap stays visible.
		if ph.has(phEntwine) {
			if ec, ok := entwineCost(f); ok && targetsAvailable() && isCharmSpell(f) &&
				offerCastable(p, id, e.rawBaseCost(p, id).Plus(ec), spellScope("entwined"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (entwined)", Obj: id, Mode: "entwined"})
			}
		}
		// Replicate (CR 702.55a): the replicated variant is its own cast
		// option paying the base cost plus ONE replicate payment -- one
		// payment is what gates the offer; the count ask (replicateAsk)
		// settles how many afterwards and the 601.2g payment window may still
		// produce mana for the composed total, exactly like a kicked cast, so
		// the max count cannot be fixed at offer time. The non-mana parts of
		// the payment fail closed in nonManaCastable (offerCastable's shared
		// tail), so the two tapXType carriers' replicate never offers.
		if ph.has(phReplicate) {
			if rc, ok := replicateCost(f); ok && targetsAvailable() &&
				offerCastable(p, id, e.rawBaseCost(p, id).Plus(rc), spellScope("replicated"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (replicated)", Obj: id, Mode: "replicated"})
			}
		}
		// Multikicker (CR 702.43): the multikicked variant is its own cast
		// option paying the base cost plus ONE multikicker payment -- the
		// replicate offer's exact shape (one payment is what gates the offer;
		// the count ask, multikickAsk, settles how many afterwards). No corpus
		// carrier pairs Kicker with Multikicker (measured), so this offer
		// never collides with the kicked family above.
		if ph.has(phMultikicker) {
			if mkc, ok := multikickerCost(f); ok && targetsAvailable() &&
				offerCastable(p, id, e.rawBaseCost(p, id).Plus(mkc), spellScope("multikicked"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (multikicked)", Obj: id, Mode: "multikicked"})
			}
		}
		// Squad (CR 702.66): the squadded variant pays the base cost plus ONE
		// squad payment -- the replicate/multikicker offer's exact shape (one
		// payment gates the offer; the count ask, squadAsk, settles how many
		// afterwards, and the 601.2g payment window may still produce mana for
		// the composed total). No corpus carrier pairs Squad with
		// Replicate/Multikicker/Kicker (measured over the 15 K:Squad files), so
		// this offer never collides with the count asks above. Non-mana parts
		// fail closed in nonManaCastable (offerCastable's shared tail), so a
		// squad cost ParseCost cannot price never offers.
		if ph.has(phSquad) {
			if sqc, ok := squadCost(f); ok && targetsAvailable() &&
				offerCastable(p, id, e.rawBaseCost(p, id).Plus(sqc), spellScope("squadded"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (squadded)", Obj: id, Mode: "squadded"})
			}
		}
		// Conspire (CR 702.78a): the conspired variant pays NO extra mana --
		// the base cost is unchanged and the cost is the tap of two untapped
		// creatures the caster controls that share a colour with the spell.
		// So unlike the replicate/multikicker offers there is no cost to
		// compose: the offer is gated on the same base cast being offerable
		// (re-checked with the base cost, exactly what the plain offer used)
		// AND on the derived-keyword read (hasCastConspire, the
		// hasCastConvoke shape, so a layer-6 grant reaching the stack matches)
		// AND on at least two eligible creatures existing. Do NOT route the
		// tap through offerCastable with a fabricated cost -- the tap has no
		// Cost$ representation; conspireAsk enforces it at announcement.
		if e.hasCastConspire(id) && targetsAvailable() &&
			len(e.conspireCandidates(p, id)) >= 2 &&
			offerCastable(p, id, withSpellAbilityExtras(f, convokeBase), spellScope(""), false) {
			out = append(out, decision.Option{Index: len(out), Kind: "cast",
				Label: "Cast " + f.Name + " (conspired)", Obj: id, Mode: "conspired"})
		}
		// Casualty is an optional additional sacrifice, not a mana cost.
		// Price the ordinary spell and require at least one creature whose
		// derived power meets the printed or layer-granted threshold. The
		// variable form (Casualty:X, Ob Nixilis, the Adversary) has no
		// threshold: the sacrificed creature's own power names the amount, so
		// any creature qualifies and the ask's power gate reads 0.
		if info, ok := e.casualtySpec(id); ok && targetsAvailable() {
			n := info.threshold
			if info.variable {
				n = 0
			}
			if len(e.casualtyCandidates(p, id, n)) > 0 &&
				offerCastable(p, id, withSpellAbilityExtras(f, convokeBase), spellScope(""), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (casualty)", Obj: id, Mode: "casualty"})
			}
		}
		// The alternative-cost keyword family (altcosts), from the hand: evoke
		// (CR 702), dash, overload and warp each become their own "cast" mode
		// option paying the printed keyword cost in place of the mana cost.
		// Blitz rides the same walk but reads the ID-AWARE grant (e.blitzCost):
		// a layer-6 AddKeyword$ Blitz grant (Henzie, Toolbox Torre) prices its
		// cost from the candidate card -- CardManaCost placeholders and the
		// grant's trailing spell filter -- where the printed-only
		// keywordAltCost read cannot see it.
		// Madness does NOT offer from the hand here (CR 702.35a: the madness
		// cast window opens only on the discard, through the pending-trigger
		// machinery, exactly like Miracle); warp additionally offers from the
		// graveyard and -- after an end-step exile -- from exile, in the walks
		// below.
		for _, ka := range [...]struct {
			mode, head string
			bit        printedHeads
		}{
			{"evoked", "Evoke", phEvoke}, {"dashed", "Dash", phDash}, {"overloaded", "Overload", phOverload}, {"warped", "Warp", phWarp},
		} {
			if !ph.has(ka.bit) {
				continue
			}
			alt, ok := keywordAltCost(f, ka.head)
			if !ok || (ka.mode != "overloaded" && !targetsAvailable()) ||
				!offerCastable(p, id, alt, spellScope(ka.mode), false) {
				continue
			}
			out = append(out, decision.Option{Index: len(out), Kind: "cast",
				Label: "Cast " + f.Name + " (" + ka.mode + ")", Obj: id, Mode: ka.mode})
		}
		if blitzes := e.blitzCosts(p, id); len(blitzes) > 0 && targetsAvailable() {
			for _, blitz := range blitzes {
				if !offerCastable(p, id, blitz.cost, spellScope(blitz.mode), false) {
					continue
				}
				label := "blitzed"
				if blitz.mode != "blitzed" {
					label += " (granted)"
				}
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (" + label + ")", Obj: id, Mode: blitz.mode})
			}
		}
		// Morph / Megamorph / Disguise (CR 702.37a/702.168a/702.169a), from
		// the hand: each family becomes its own "cast" mode option paying
		// the fixed {3} face-down cost in place of the mana cost. The
		// face-down spell has no targets and no printed spell abilities
		// (CR 708.4), so the offer deliberately does NOT gate on
		// targetsAvailable, unlike the keyword family above -- beginCast's
		// target stage (pc.faceDown) and the resolution reader's printed
		// abilities are skipped the same way. The printed keyword parameter
		// (the turn-face-up cost) is NOT paid now; the pay-time CastInfo's
		// mode flag records which family rode so a later turn-face-up action
		// can validate and pay against it. The timing gate is the ordinary
		// spellTimingOK the walk already ran above (CR 702.37a's "any time
		// you could cast a sorcery" -- morph prints only on creature faces).
		if ph.has(phMorph | phMegamorph | phDisguise) {
			if fam := morphDownFamily(f); fam != "" &&
				offerCastable(p, id, Cost{Generic: 3}, spellScope(fam), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (face down)", Obj: id, Mode: fam})
			}
		}
		// Emerge (CR 702.118a): "cast this spell by sacrificing a creature and
		// paying the emerge cost reduced by that creature's mana value". It is
		// a casting option, but NOT a plain substitution -- the cast sacrifices
		// a creature AND the cost is reduced by its mana value -- so it does
		// not ride the keyword family above. emergeOfferCost composes the
		// printed K:Emerge cost with the mandatory Sac<1/Creature> part,
		// priced separately for each sacrifice candidate. sacAsk permits only
		// candidates whose own reduced cost remains payable. Unsupported printed
		// cost shapes are withheld; the plain cast remains unaffected.
		if ph.has(phEmerge) {
			if _, ok := e.emergeOfferCost(p, id, f, func(c Cost) bool {
				return offerCastable(p, id, c, spellScope("emerged"), false)
			}); ok && targetsAvailable() {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (emerged)", Obj: id, Mode: "emerged"})
			}
		}
		// Bestow (CR 702.114a): the bestowed cast is its own "cast" option
		// paying the bestow cost in place of the mana cost, and the spell is
		// an Aura with enchant creature, so the offer gates on the targets of
		// the SYNTHESIZED attach SA -- the face has no SP of its own, so the
		// plain cast's targetsAvailable (from the nil SpellAbility) says
		// nothing about it. bestowCost prices every printed shape (an {X}, a
		// CollectEvidence<N>, and the colon-suffixed metadata line) and
		// withholds only a cost token ParseCost cannot model at all, the
		// replicate convention.
		if ph.has(phBestow) {
			if ba, ok := bestowCost(f); ok && e.castTargetsAvailable(p, id, bestowedAttachSA()) &&
				offerCastable(p, id, ba, spellScope("bestowed"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (bestowed)", Obj: id, Mode: "bestowed"})
			}
		}
		// Mutate (CR 702.140a): the mutate cast pays the mutate cost in place
		// of the mana cost and targets a non-Human creature its controller
		// owns. Like bestow, the gate is the SYNTHESIZED target SA's
		// feasibility -- the creature face has no SP for the mutation.
		if ph.has(phMutate) {
			if mc, ok := mutateCost(f); ok && e.castTargetsAvailable(p, id, mutateTargetSA()) &&
				offerCastable(p, id, mc, spellScope("mutated"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (mutated)", Obj: id, Mode: "mutated"})
			}
		}
		if ph.has(phBuyback) {
			if bc, ok := buybackCost(f); ok && offerCastable(p, id, e.rawBaseCost(p, id).Plus(bc), spellScope("buyback"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast", Label: "Cast " + f.Name + " (buyback)", Obj: id, Mode: "buyback"})
			}
		}
		// Offspring (CR 702.175a): the optional ADDITIONAL cost half, offered
		// beside the plain cast. e.offspringCost reads the DERIVED keyword list
		// with the stack-zone override (rules/offspring.go), so BOTH the
		// printed K:Offspring line and a layer-6 AddKeyword$ Offspring grant
		// reaching the cast (Zinnia, Valley's Voice's "Creature spells you cast
		// have offspring {2}") are priced through the ONE read -- the granted
		// cost is charged, never a printed one, and the offer and beginCast's
		// modeCost stage structurally cannot disagree. The offer gate is the
		// same base+additional composition beginCast will charge, and
		// targetsAvailable keeps a target-bearing creature spell's offer honest
		// (the plain cast's gate, which the offspring cast shares).
		if e.hasCastOffspring(id) && targetsAvailable() {
			if oc, ok := e.offspringCost(id); ok &&
				offerCastable(p, id, e.rawBaseCost(p, id).Plus(oc), spellScope("offspring"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (offspring)", Obj: id, Mode: "offspring"})
			}
		}
		if ph.has(phSuspend) {
			if sc, ok := suspendCost(f); ok {
				offer := sc.cost
				if sc.timeX {
					// XMin<N> is part of the announcement, not a later payment
					// preference: do not offer a Suspend X action that cannot pay
					// even its smallest legal X.
					offer = offer.WithX(sc.minTime)
				}
				if offerCastable(p, id, offer, spellScope("suspend"), false) {
					out = append(out, decision.Option{Index: len(out), Kind: "cast", Label: "Suspend " + f.Name, Obj: id, Mode: "suspend"})
				}
			}
		} // Plot (CR 701.34a): the alternative ACTION pays the K: line's colon
		// parameter and exiles the card with the plotted designation -- NO
		// counters (the K:Plot token is the COST {generic}+{colour}, never a
		// counter count). "Plot only as a sorcery" is the engine's own
		// sorcery-speed bool -- unlike the plain cast above, which follows the
		// face's own type timing, even an instant's plot action waits for its
		// controller's main phase with an empty stack. No target ask: the
		// action itself only exiles; the later plot_cast announces its own
		// targets. The free cast's later-turn gate lives in Object.PlottedTurn.
		if ph.has(phPlot) {
			if raw, ok := f.KeywordParam("Plot"); ok && sorcery &&
				offerCastable(p, id, ParseCost(raw), spellScope("plot"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Plot " + f.Name, Obj: id, Mode: "plot"})
			}
		}
	}

	// A may-play-from-zone grant (Conduit of Worlds, Crucible of Worlds, ...)
	// makes lands in a granted zone playable this turn. This is the SECOND
	// play_land source alongside the hand walk above, never a replacement for
	// it; the once-per-turn land-drop gate is the same sorcerySpeed &&
	// LandsPlayed < 1 condition the hand walk applies, so a graveyard land
	// and a hand land share one land drop. The zones walked, the Affects
	// filter each grant applies, and the deterministic order all come from
	// mayPlayLandIds.
	for _, id := range e.mayPlayLandIds(p) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		add("play_land", "Play "+o.Face().Name, id)
	}

	// The bare, parameterless K:Mayhem permission (Oscorp Industries) is a
	// land-only PLAY from the graveyard, not a cast -- the "Timing rules still
	// apply" land shape the mayhem cast walk withholds. It is a THIRD
	// play_land source alongside the hand walk and mayPlayLandIds, sharing
	// their once-per-turn land-drop gate and paying no mana. Deduped against a
	// land a MayPlay grant already offered so the same graveyard land never
	// yields two identical options.
	for _, id := range e.mayhemLandPlayIds(p) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		dup := false
		for _, prev := range out {
			if prev.Kind == "play_land" && prev.Obj == id {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		add("play_land", "Play "+o.Face().Name, id)
	}

	// A may-play-from-zone grant also makes NON-LAND cards in a granted zone
	// castable this turn (CR 401.5; Atsushi's "you may play those cards",
	// Opposition Agent's MayPlay+IgnoreColor static). This is a THIRD cast
	// source alongside the hand and command-zone walks, never a replacement;
	// the cast pays the card's PRINTED cost (the grant changes where it may
	// come from, not what it costs), with the grant's MayPlayIgnoreColor$
	// rider payable-as-any-colour consulted by castable (the card is still
	// in the granted zone here) and recorded on the pendingCast at beginCast
	// for the window and the payment to keep. A MayPlayLimit$ grant whose cap
	// is reached offers nothing through itself (mayPlaySpellIds). The offer
	// gate folds the card's own SpellAbility additional costs exactly like
	// the hand walk does, so an offered may-play cast and the cost beginCast
	// charges structurally cannot disagree.
	for _, off := range e.mayPlaySpellIds(p) {
		id := off.id
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil || f.IsLand() || castRestricted(p, id) || e.castSuppressed(p, id) {
			continue
		}
		if !e.spellTimingOK(p, id, f, sorcery) {
			continue
		}
		// The permission's ValidSA$ decides which cast shapes it permits
		// (Brokkos, Apex of Forever's `ValidSA$ Spell.Mutate` permits ONLY
		// the mutate cast from the graveyard, CR 903.3d/702.140a). An empty
		// ValidSA$ is the ordinary permission, so this splits the historical
		// single offer into its plain and mutate halves without changing any
		// unrestricted grant's behaviour.
		plain, mutate, blitz := e.mayPlayKinds(p, id)
		if plain && e.castTargetsAvailable(p, id, f.SpellAbility()) {
			base := e.rawBaseCost(p, id)
			// A MayPlayText$-typed offer carries its own permission's riders
			// (mayPlayPermissions); an untyped offer keeps the aggregate read
			// every existing grant used.
			if off.key == "" {
				if free, ok := e.mayPlayGrant(p, id); ok && free {
					// MayPlayWithoutManaCost$ True (the kw-mayplay predicate): the
					// mana part is free, exactly as beginCast's "mayplay" case will
					// charge it; non-mana additional costs still apply (CR 118.9).
					base = Cost{}
				}
			} else if off.free {
				base = Cost{}
			}
			// CR 118.3a: the granting static's RaiseCost$ surcharge is added on
			// top of the printed cost (Kotis, Sibsig Champion's "by exiling
			// three other cards ... in addition to paying its other costs").
			// Composed before offerCostFor so static cost modifiers apply to the
			// raised cost, the CR 601.2f order; the affordability gate below then
			// prices the whole cost against real state (nonManaCastable). A raise
			// mayPlayStatic could not price never reaches here -- mayPlayGrant
			// withholds the card -- but the defensive continue keeps the two
			// sites agreeing if that ever changes.
			if off.key == "" {
				if raise, hasRaise, priced := e.mayPlayRaiseCost(p, id); hasRaise {
					if !priced {
						continue
					}
					base = base.Plus(raise)
				}
			} else if off.hasRaise {
				if !off.priced {
					continue
				}
				base = base.Plus(off.raise)
			}
			cost := withSpellAbilityExtras(f, offerCostFor(p, id, base, spellScope("mayplay")))
			if affordable(p, id, cost, false) {
				label := "Cast " + f.Name
				if off.text != "" {
					// MayPlayText$ is the permission's label, so a card matching
					// several permissions offers one named option per still-unused
					// permission (Muldrotha's artifact creature).
					label = "Cast " + f.Name + " (" + off.text + ")"
				}
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: label, Obj: id, Mode: "mayplay", MayPlayPerm: off.key})
			}
		}
		// Mutate half: the permission names the mutate cast. The mutate cast
		// pays the mutate cost in place of the mana cost and targets a
		// non-Human creature its controller owns -- the same synthesized
		// target SA and the same cost substitution the hand walk's mutate
		// offer uses (legal.go's hand branch, beginCast's "mutated" case),
		// which prices the mutate cost regardless of the zone the card is
		// cast from. A may-play mutate carries no printed-mana "free"
		// exemption: MayPlayWithoutManaCost$ is a property of the permission,
		// but the mutate cost IS the mana cost this cast pays.
		if off.key == "" && mutate {
			if mc, ok := mutateCost(f); ok && e.castTargetsAvailable(p, id, mutateTargetSA()) &&
				offerCastable(p, id, mc, spellScope("mutated"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (mutated)", Obj: id, Mode: "mutated"})
			}
		}
		// Blitz half: the permission names the blitz cast (Sabin, Master Monk's
		// `ValidSA$ Spell.Blitz`, "You may cast CARDNAME from its graveyard
		// using its blitz ability"). CR 702.152a's alternative cost replaces
		// the mana cost, so the graveyard offer prices the printed K:Blitz
		// parameter (including its Discard<1/Card> part) through the same
		// keywordAltCost the hand walk uses; beginCast's "blitzed" case charges
		// exactly that cost, so offer and charge cannot drift.
		if off.key == "" && blitz {
			if bc, ok := keywordAltCost(f, "Blitz"); ok &&
				e.castTargetsAvailable(p, id, f.SpellAbility()) &&
				offerCastable(p, id, bc, spellScope("blitzed"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (blitzed)", Obj: id, Mode: "blitzed"})
			}
		}
	}
	// Command zone (CR 903.8, Commander format): a player may cast a
	// commander they own from the command zone. This is a SECOND cast source
	// alongside the hand walk above, never a replacement for it. A
	// commander's timing is its own card's -- a creature commander is
	// sorcery-speed, an instant/flash one instant-speed -- gated by the same
	// sorcery check every other cast uses. The offer is gated on castable
	// with the SAME taxed cost beginCast will charge (commanderTaxFor over
	// the same board), so an offered command-zone cast and the cost it pays
	// structurally cannot disagree. Only a Commander game offers anything
	// here; the explicit format gate is what keeps these rules out of every
	// other game (and TestCommanderTaxGatedOffOutsideCommanderFormat exercises
	// it with a commander actually present in the command zone of a
	// Constructed game), not the
	// incident that a Constructed command zone is normally empty.
	for _, id := range e.G.Zone(state.ZCommand, p) {
		if e.format != FormatCommander {
			continue
		}
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil {
			continue
		}
		if castRestricted(p, id) || e.castSuppressed(p, id) {
			continue
		}
		if !e.spellTimingOK(p, id, f, sorcery) {
			continue
		}
		targetsAvailable := e.castTargetsAvailable(p, id, f.SpellAbility())
		if targetsAvailable && offerCastable(p, id, e.rawBaseCost(p, id), spellScope(""), false) {
			add("cast", "Cast "+f.Name, id)
		}
		if targetsAvailable {
			for i, extra := range e.optionalCostViews(costStatics.get(), p, id) {
				if offerCastable(p, id, withSpellAbilityExtras(f, e.rawBaseCost(p, id)).Plus(extra), spellScope("optionalcost"), false) {
					out = append(out, decision.Option{Index: len(out), Kind: "cast", Label: "Cast " + f.Name + " (optional cost)", Obj: id, Mode: "optionalcost", AltCostIndex: i + 1})
				}
			}
		}
		// Alternative costs replace the printed mana cost but not additional
		// costs such as commander tax (CR 118.9d, 903.8). Dash and the other
		// cast alternatives therefore remain available from the command zone;
		// offerCostFor applies the same tax beginCast later charges.
		for _, ka := range [...]struct{ mode, head string }{
			{"evoked", "Evoke"}, {"dashed", "Dash"}, {"overloaded", "Overload"},
		} {
			alt, ok := keywordAltCost(f, ka.head)
			if !ok || (ka.mode != "overloaded" && !targetsAvailable) ||
				!offerCastable(p, id, alt, spellScope(ka.mode), false) {
				continue
			}
			out = append(out, decision.Option{Index: len(out), Kind: "cast",
				Label: "Cast " + f.Name + " (" + ka.mode + ")", Obj: id, Mode: ka.mode})
		}
		// Bestow (CR 702.114a), the command-zone half (a bestowed commander,
		// kestia_the_cultivator's shape): the same synthesized-attach-SA gate
		// the hand walk applies.
		if ba, ok := bestowCost(f); ok && e.castTargetsAvailable(p, id, bestowedAttachSA()) &&
			offerCastable(p, id, ba, spellScope("bestowed"), false) {
			out = append(out, decision.Option{Index: len(out), Kind: "cast",
				Label: "Cast " + f.Name + " (bestowed)", Obj: id, Mode: "bestowed"})
		}
		// Mutate (CR 702.140a), the command-zone half (a commander printed
		// with mutate may be cast for its mutate cost, CR 903.3d): the same
		// synthesized-target-SA gate the hand walk applies.
		if mc, ok := mutateCost(f); ok && e.castTargetsAvailable(p, id, mutateTargetSA()) &&
			offerCastable(p, id, mc, spellScope("mutated"), false) {
			out = append(out, decision.Option{Index: len(out), Kind: "cast",
				Label: "Cast " + f.Name + " (mutated)", Obj: id, Mode: "mutated"})
		}
		// Offspring (CR 702.175a), the command-zone half (a commander printed
		// with offspring, or granted it by a static): the same base+additional
		// composition the hand walk offers.
		if targetsAvailable && e.hasCastOffspring(id) {
			if oc, ok := e.offspringCost(id); ok &&
				offerCastable(p, id, e.rawBaseCost(p, id).Plus(oc), spellScope("offspring"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (offspring)", Obj: id, Mode: "offspring"})
			}
		}
		// Morph / Megamorph / Disguise (CR 702.37a/702.168a/702.169a), the
		// command-zone half: the same fixed-{3} face-down offer the hand walk
		// makes, on the same terms -- it deliberately does NOT gate on
		// targetsAvailable (CR 708.4: a face-down spell has no targets), and
		// the printed keyword parameter (the turn-face-up cost) is not paid
		// now. offerCastable composes the CR 903.8 commander tax on top of
		// the {3}, exactly what beginCast charges for this mode.
		if fam := morphDownFamily(f); fam != "" &&
			offerCastable(p, id, Cost{Generic: 3}, spellScope(fam), false) {
			out = append(out, decision.Option{Index: len(out), Kind: "cast",
				Label: "Cast " + f.Name + " (face down)", Obj: id, Mode: fam})
		}
	}

	// Harmonize is a graveyard alternative. It is offered as its own cast
	// transaction, then spellRestZone exiles it after resolution.
	for _, id := range e.G.Zone(state.ZGraveyard, p) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		f := o.Face()
		// The printed-keyword read is the cheap gate, so it runs first: every
		// gate here is a pure read, so the order changes no answer. A face with
		// no keyword line at all answers "absent" to every KeywordParam, so it
		// is skipped before the reader builds its Cost.
		if len(f.Keywords) == 0 {
			continue
		}
		hc, ok := harmonizeCost(f)
		if !ok || castRestricted(p, id) || e.castSuppressed(p, id) {
			continue
		}
		if !e.spellTimingOK(p, id, f, sorcery) {
			continue
		}
		if e.castTargetsAvailable(p, id, f.SpellAbility()) {
			hc, _ = e.harmonizePayment(p, id, hc)
			if offerCastable(p, id, hc, spellScope("harmonize"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast", Label: "Cast " + f.Name + " (harmonize)", Obj: id, Mode: "harmonize"})
			}
		}
	}

	// Flashback: a graveyard walk, same instant-speed timing as hand cards,
	// gated on the derived keyword (so a continuous-effect grant, e.g.
	// Snapcaster Mage, counts) rather than the printed one.
	for _, id := range e.G.Zone(state.ZGraveyard, p) {
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil || !e.hasKeywordH(id, kwhFlashback) {
			continue
		}
		if castRestricted(p, id) {
			continue
		}
		if e.castSuppressed(p, id) {
			continue
		}
		if !e.spellTimingOK(p, id, f, sorcery) {
			continue
		}
		if !e.castTargetsAvailable(p, id, f.SpellAbility()) {
			continue
		}
		if fc := e.flashbackCost(id); offerCastable(p, id, fc, spellScope("flashback"), false) {
			out = append(out, decision.Option{Index: len(out), Kind: "cast",
				Label: "Cast " + f.Name + " (flashback)", Obj: id, Mode: "flashback"})
		}
	}

	// Aftermath (CR 702.85a): the alternate face of a Split card may be cast
	// from its owner's graveyard for its printed mana cost (plus its own SP
	// Cost$ additional parts -- start_finish's Sac<1/Creature>), then exiled.
	// Gated on the ALTERNATE face's K:Aftermath, which is what excludes Rooms
	// (both halves are Rooms, neither carries Aftermath) and Adventures
	// (AlternateMode Adventure, not Split). Mode aftermath is consumed by
	// beginCast, which records a FlipFace to the alternate face before the
	// ordinary cast transaction -- rawBaseCost, targets and resolution then
	// read the aftermath face. The withSpellAbilityExtras fold prices the
	// face's own SP Cost$ parts exactly like the adventure_alt offer above:
	// without it a Finish-shaped gate would offer an unpayable cast. The
	// prohibition gate probes the AFTERMATH face being cast
	// (castRestrictedAsFace), not the front face still displayed in the
	// graveyard: a restriction matching only one half must decide only that
	// half's offer.
	for _, id := range e.G.Zone(state.ZGraveyard, p) {
		o := e.G.Obj(id)
		af := aftermathAlternateFace(o)
		if af == nil || castRestrictedAsFace(p, id, af) || e.castSuppressed(p, id) {
			continue
		}
		if !e.spellTimingOK(p, id, af, sorcery) {
			continue
		}
		if !e.castTargetsAvailable(p, id, af.SpellAbility()) {
			continue
		}
		if offerCastableAsFace(p, id, af, withSpellAbilityExtras(af, ParseCost(af.ManaCost)), spellScope("")) {
			out = append(out, decision.Option{Index: len(out), Kind: "cast",
				Label: "Cast " + af.Name + " (aftermath)", Obj: id, Mode: "aftermath"})
		}
	}

	// MayPlay static offers are the main-side walk above (mayPlayLandIds /
	// mayPlaySpellIds over the continuous-effect grants plus the self-grant
	// scan); the kw-mayplay branch's separate mayPlayGrantOffers walk is
	// retired here so a granted card is offered exactly once.

	// Warp from the graveyard requires a separate MayPlay Spell.Warp static;
	// Warp itself grants only the hand alternative. Timeline Culler is the
	// corpus shape carrying that explicit graveyard permission.
	for _, id := range e.G.Zone(state.ZGraveyard, p) {
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil || len(f.Keywords) == 0 {
			// No keyword line: KeywordParam("Warp") is absent.
			continue
		}
		wc, ok := keywordAltCost(f, "Warp")
		if !ok || !warpGraveyardAllowed(f) || castRestricted(p, id) || e.castSuppressed(p, id) {
			continue
		}
		instantSpeed := f.IsInstant() || e.hasKeywordH(id, kwhFlash)
		if !instantSpeed && !sorcery {
			continue
		}
		if !e.castTargetsAvailable(p, id, f.SpellAbility()) {
			continue
		}
		if offerCastable(p, id, wc, spellScope("warped"), false) {
			out = append(out, decision.Option{Index: len(out), Kind: "cast",
				Label: "Cast " + f.Name + " (warped)", Obj: id, Mode: "warped"})
		}
	}

	// Escape (CR 702.42a): a card in its owner's graveyard carrying the
	// Escape keyword -- printed (Kroxa's K:Escape) or granted by a continuous
	// effect (Underworld Breach's AddKeyword$ Escape, which Derived reads off
	// the layer system) -- may be cast for its escape cost: the card's mana
	// cost plus ExileFromGrave<N/Card.Other> parts, exiling N OTHER cards
	// from the same graveyard. The cast is a normal cast (CR 702.42a gives
	// no post-resolution destination change -- unlike flashback the spell
	// goes where it would otherwise go), so modeFlags marks it FlagEscaped
	// and the ETB machinery reads the flag through Card.Self+escaped.
	for _, id := range e.G.Zone(state.ZGraveyard, p) {
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil || !e.hasKeywordH(id, kwhEscape) || castRestricted(p, id) || e.castSuppressed(p, id) {
			continue
		}
		ec, ok := e.escapeCost(id)
		if !ok || !e.spellTimingOK(p, id, f, sorcery) ||
			!e.castTargetsAvailable(p, id, f.SpellAbility()) {
			continue
		}
		if affordable(p, id, offerCostFor(p, id, ec, spellScope("escape")), false) {
			out = append(out, decision.Option{Index: len(out), Kind: "cast",
				Label: "Cast " + f.Name + " (escape)", Obj: id, Mode: "escape"})
		}
	}

	// Retrace (CR 702.81a): a card in its owner's graveyard carrying the
	// Retrace keyword may be cast from there by paying its printed mana cost
	// PLUS an additional cost of discarding a land card. Unlike Escape this
	// is not a cost substitution, so the offer prices the ordinary plain-cast
	// base (castOfferBase credits Convoke/Improvise, withSpellAbilityExtras
	// adds the spell's own additional parts) with retraceExtra folded on top.
	// The discard is a real hand cost, so the offer is withheld unless a land
	// card is actually there to discard -- an option that cannot be paid must
	// never be offered (the offerCastable/withSpellAbilityExtras ruling).
	for _, id := range e.G.Zone(state.ZGraveyard, p) {
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil || !e.hasKeywordH(id, kwhRetrace) || castRestricted(p, id) || e.castSuppressed(p, id) {
			continue
		}
		if !e.spellTimingOK(p, id, f, sorcery) ||
			!e.castTargetsAvailable(p, id, f.SpellAbility()) {
			continue
		}
		rx := retraceExtra()
		if !e.discardCostPayable(p, id, rx.Discard, true) {
			continue
		}
		if offerCastable(p, id, withSpellAbilityExtras(f, e.castOfferBase(p, id)).Plus(rx), spellScope("retrace"), false) {
			out = append(out, decision.Option{Index: len(out), Kind: "cast",
				Label: "Cast " + f.Name + " (retrace)", Obj: id, Mode: "retrace"})
		}
	}

	// Jump-start (CR 702.84a): "You may cast this card from your graveyard by
	// discarding a card in addition to paying its other costs. Then exile this
	// card." The retrace shape with any card discardable in place of a land,
	// and the flashback destination on resolution. The keyword takes no
	// parameter (all 13 corpus lines are the bare K:Jump-start), so the whole
	// cost is the printed mana cost plus the additional Discard<1/Card>.
	// Gated on the derived keyword (HasKeyword), so a continuous-effect grant
	// would count; the same timing/target/restriction gates as every other
	// graveyard alt-cast, and the discount payable gate -- an option whose
	// discard cannot be paid must never be offered (the offerCastable ruling).
	for _, id := range e.G.Zone(state.ZGraveyard, p) {
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil || !e.hasKeywordH(id, kwhJumpStart) || castRestricted(p, id) || e.castSuppressed(p, id) {
			continue
		}
		if !e.spellTimingOK(p, id, f, sorcery) ||
			!e.castTargetsAvailable(p, id, f.SpellAbility()) {
			continue
		}
		js := jumpstartExtra()
		if !e.discardCostPayable(p, id, js.Discard, true) {
			continue
		}
		if offerCastable(p, id, withSpellAbilityExtras(f, e.castOfferBase(p, id)).Plus(js), spellScope("jumpstart"), false) {
			out = append(out, decision.Option{Index: len(out), Kind: "cast",
				Label: "Cast " + f.Name + " (jump-start)", Obj: id, Mode: "jumpstart"})
		}
	}

	// Mayhem (the Doom Prevails keyword): a card in its owner's graveyard
	// that was discarded THIS TURN may be cast for its mayhem cost -- a cost
	// SUBSTITUTION ("cast this card from your graveyard for {4}{R}"), not an
	// addition, and no post-resolution destination change: the oracle's only
	// rider is "Timing rules still apply", which spellTimingOK enforces, so
	// the spell resolves like an ordinary cast. The provenance gate is
	// log-derived (mayhemDiscardedThisTurn), the same shape the warp-recast
	// and foretell gates take, so a replayed game derives the same offer;
	// the cost helper reads the derived keyword list, so a continuous-effect
	// grant would count. The bare parameterless K:Mayhem is the "play this
	// card" LAND shape (Oscorp Industries) and is withheld here -- not a
	// cast. Offer and charge both go through mayhemCastCost, so they cannot
	// drift.
	for _, id := range e.G.Zone(state.ZGraveyard, p) {
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil {
			continue
		}
		// mayhemCastCost reads the derived Mayhem parameter, which is absent
		// whenever its subset precheck rules the head out (keywordmay.go).
		if !e.mayHaveDerivedKeywordH(id, kwhMayhem) {
			if derivedMemoVerify {
				e.verifyKeywordPrecheck(id, "Mayhem")
			}
			continue
		}
		mc, ok := e.mayhemCastCost(id)
		if !ok || castRestricted(p, id) || e.castSuppressed(p, id) || !e.mayhemDiscardedThisTurn(p, id) {
			continue
		}
		if !e.spellTimingOK(p, id, f, sorcery) ||
			!e.castTargetsAvailable(p, id, f.SpellAbility()) {
			continue
		}
		if offerCastable(p, id, withSpellAbilityExtras(f, mc), spellScope("mayhem"), false) {
			out = append(out, decision.Option{Index: len(out), Kind: "cast",
				Label: "Cast " + f.Name + " (mayhem)", Obj: id, Mode: "mayhem"})
		}
	}

	// Warp recast from exile (CR 702: "exile this creature at the beginning
	// of the next end step, then you may cast it from exile on a later
	// turn"). The exile-zone walk offers the cast only to a warp card that
	// the log shows was warp-cast and end-step-exiled, on a turn strictly
	// after that exile -- the flag alone cannot say it (CastFlags reset when
	// the permanent left the battlefield), but the log can. This later cast
	// pays the normal mana cost and is not itself flagged warped.
	for _, id := range e.G.Zone(state.ZExile, p) {
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil || o.IsToken {
			continue
		}
		// CR 714.3a: the main face of an Adventure card resting in the
		// adventure zone (exile, at its Adventure spell face) may be cast from
		// there. Mode adventure_recast is consumed by beginCast, which flips
		// the card back to its main face before the ordinary cast transaction
		// -- so everything downstream reads the main face. The provenance (the
		// card got here by RESOLVING an adventure_alt cast) is log-derived:
		// adventureZoneAvailable. The cost is built from the MAIN face
		// explicitly -- rawBaseCost reads o.Face(), which is still the
		// Adventure face in exile, exactly the reason the Room offer parses its
		// own face's cost too. The adventure-zone entry from a non-resolution
		// path (CR 714.3b) is out of scope, and adventureZoneAvailable refuses
		// it. The prohibition gate probes the MAIN face the recast actually
		// casts (castRestrictedAsFace), not the Adventure spell face still
		// displayed in exile: probing the displayed face would withhold an
		// unrestricted front cast or offer a prohibited one whenever the
		// restriction distinguishes the two faces.
		if adventureSpellFace(o) != nil && int(o.FaceIdx) == 1 && e.adventureZoneAvailable(id) &&
			!castRestrictedAsFace(p, id, o.Card.Faces[0]) && !e.castSuppressed(p, id) {
			front := o.Card.Faces[0]
			if e.spellTimingOK(p, id, front, sorcery) && e.castTargetsAvailable(p, id, front.SpellAbility()) &&
				offerCastableAsFace(p, id, front, ParseCost(front.ManaCost), spellScope("")) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + front.Name + " (from adventure zone)", Obj: id, Mode: "adventure_recast"})
			}
			continue
		}
		// Foretell cast (CR 702.126a): a card exiled face down by the {2}
		// Foretell ACTION (not by any other effect -- the flag is the action's
		// own provenance marker) may be cast from exile for its FORETELL cost
		// (the K: line's colon parameter) on a later turn. "Later turn" is
		// log-derived (foretellCastAvailable): the flag alone cannot say it,
		// the same reason the warp recast offer below is log-derived. The cast
		// follows the card's own timing (spellTimingOK) and targets. The block
		// sits BEFORE the warp gate's continue: a non-warp card (every foretell
		// carrier) would otherwise never reach it.
		if o.CastFlags&state.FlagForetold != 0 &&
			e.foretellCastAvailable(id) && !castRestricted(p, id) && !e.castSuppressed(p, id) &&
			e.spellTimingOK(p, id, f, sorcery) && e.castTargetsAvailable(p, id, f.SpellAbility()) {
			// The K:Foretell parameter prices the later cast (CR 702.126a);
			// a face with no parameter falls back to the rule's action default
			// {2} -- every corpus carrier carries one (measured 55/55), so the
			// fallback is latent. The RAW parsed cost is offered here; cost
			// modifiers (CR 601.2f) apply later, in manaToPay, exactly like
			// the other alternative-cost recasts.
			fc, ok := foretellCost(f)
			if ok && offerCastable(p, id, fc, spellScope("foretell_cast"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (foretold)", Obj: id, Mode: "foretell_cast"})
			}
		}
		// Plot's free cast (CR 701.34b): a card carrying the plotted
		// designation (Object.PlottedTurn, the AlterAttribute fold; only the
		// plot ACTION and the corpus's own Attribute grants set it, so an
		// arbitrary exiled Plot carrier is never offered) may be cast from
		// exile without paying its mana cost "on a later turn" -- strictly
		// after the turn it became plotted, NOT after a counter count (Plot
		// has no counters; that is Suspend's mechanic). It is a STANDING
		// permission that follows SORCERY timing whatever the face's own type
		// is -- even a plotted instant waits for its owner's main phase with
		// an empty stack -- so the gate is the engine's sorcery-speed bool
		// plus the face's activation-phase gates, re-offered every priority
		// round the permission holds. The offer sits BEFORE the warp gate's
		// continue, the foretell block's own reason.
		if _, ok := f.KeywordParam("Plot"); ok && o.PlottedTurn > 0 &&
			e.G.Turn > o.PlottedTurn && !castRestricted(p, id) && !e.castSuppressed(p, id) &&
			sorcery && e.spellTimingOK(p, id, f, true) &&
			e.castTargetsAvailable(p, id, f.SpellAbility()) {
			if offerCastable(p, id, Cost{}, spellScope("plot_cast"), false) {
				out = append(out, decision.Option{Index: len(out), Kind: "cast",
					Label: "Cast " + f.Name + " (plotted)", Obj: id, Mode: "plot_cast"})
			}
		}
		_, ok := keywordAltCost(f, "Warp")
		if !ok || !e.warpRecastAvailable(id) || castRestricted(p, id) || e.castSuppressed(p, id) {
			continue
		}
		instantSpeed := f.IsInstant() || e.hasKeywordH(id, kwhFlash)
		if !instantSpeed && !sorcery {
			continue
		}
		if !e.castTargetsAvailable(p, id, f.SpellAbility()) {
			continue
		}
		normal := e.rawBaseCost(p, id)
		if offerCastable(p, id, normal, spellScope("warp_recast"), false) {
			out = append(out, decision.Option{Index: len(out), Kind: "cast",
				Label: "Cast " + f.Name + " (from warp exile)", Obj: id, Mode: "warp_recast"})
		}
	}

	if !castsOnly {
		// Mana abilities may explicitly function from the battlefield, hand or
		// graveyard (Spirit Guides and Jack-o'-Lantern). availableManaAbilities
		// applies each ability's ActivationZone, Activator$ and full cost gate.
		// The battlefield is walked for EVERY seat, not just p's, because an
		// ability another player's Activator$ permits (Mana Cache's "Any player
		// may activate this ability") reaches them through p's offer; every other
		// object still fails the controller/selector gate in the choke point.
		// The walk only inspects each object's mana-ability list, so one scratch
		// buffer serves every object (taken from the Engine for the loop, so a
		// re-entrant walk allocates its own).
		masBuf := e.manaAbBuf
		e.manaAbBuf = nil
		for _, z := range []state.Zone{state.ZBattlefield, state.ZHand, state.ZGraveyard} {
			zonePlayers := []state.PlayerID{p}
			if z == state.ZBattlefield {
				zonePlayers = make([]state.PlayerID, len(e.G.Players))
				for seat := range zonePlayers {
					zonePlayers[seat] = state.PlayerID(seat)
				}
			}
			for _, zonePlayer := range zonePlayers {
				for _, id := range e.G.Zone(z, zonePlayer) {
					o := e.G.Obj(id)
					if z == state.ZBattlefield && !existsOnBattlefield(o) {
						// CR 702.25b: a phased-out permanent is treated as though it
						// does not exist, so its mana abilities are not offered. The
						// choke point appendAvailableManaAbilities is gated too, which
						// covers the payment windows this offer walk does not reach.
						continue
					}
					f := o.Face()
					if f == nil {
						continue
					}
					mas := e.appendAvailableManaAbilities(masBuf[:0], &actionStatics, p, id)
					masBuf = mas
					if len(mas) == 0 {
						continue
					}
					opt := decision.Option{Index: len(out), Kind: "activate", Label: e.manaActivateLabel(f.Name), Obj: id}
					// fb-led1: a mana ability that costs more than a bare tap is the
					// play the window exists for — carry its cost so the client's
					// empty-priority-window floor stops instead of passing it away.
					if marker := e.manaActivationCostMarker(mas); marker != "" {
						opt.Cost = marker
					}
					out = append(out, opt)
				}
			}
		}
		clear(masBuf)
		e.manaAbBuf = masBuf[:0]

		// Activated abilities (Task 10): every non-mana AB$ ability on a
		// permanent p controls, and every one on a card in p's graveyard whose
		// ActivationZone$ names the graveyard, offered as an "ability" option the
		// way a cast is (rules/activate.go resolves one). Each is gated on the
		// same rules the cast options are: its own zone matches ActivationZone$,
		// SorcerySpeed$ True needs a full sorcery window, no CantBeActivated
		// restriction scopes down to it, a {T} cost needs an untapped source that
		// is neither tapped nor (for a creature, CR 302.6) summoning-sick without
		// Haste, and -- the totality gate -- the whole cost must be castable
		// (mana payable, every Sac/Discard/SubCounter part satisfiable) before the option
		// is ever offered. Equip (Task 14's attachments) is carried by this same
		// loop -- its K:Equip expansion (cards/keywords.go) is an AB$ Attach with
		// SorcerySpeed$ True, so the gates above cover it with no carve-out, and
		// rules/activate.go resolves it exactly like any other ability. This was
		// not always true: Task 14 round 1 shipped a second, Equip-only loop and
		// deleted it again on the main merge (one offer path, one activation
		// path), so do not resurrect one.
		for _, z := range []state.Zone{state.ZBattlefield, state.ZStack, state.ZGraveyard, state.ZHand, state.ZExile} {
			zonePlayers := []state.PlayerID{p}
			if z != state.ZStack {
				// Every seat's zone, so an ability another player's Activator$
				// permits can reach that player's offer walk. The stack is the
				// exception: Zone(ZStack, p) returns the WHOLE stack regardless of
				// p, so it is walked once with the Activator$/controller
				// selector doing the filtering -- iterating seats would append the
				// same option once per seat.
				zonePlayers = make([]state.PlayerID, len(e.G.Players))
				for seat := range zonePlayers {
					zonePlayers[seat] = state.PlayerID(seat)
				}
			}
			for _, zonePlayer := range zonePlayers {
				for _, id := range e.G.Zone(z, zonePlayer) {
					o := e.G.Obj(id)
					if z == state.ZBattlefield && !existsOnBattlefield(o) {
						// CR 702.25b: a phased-out permanent is treated as though it
						// does not exist, so none of its printed activated abilities is
						// offered or activatable.
						continue
					}
					f := o.Face()
					if f == nil {
						continue
					}
					if z == state.ZExile && (o.FaceDown || !faceHasActivationZone(f, "Exile")) {
						// The exile walk exists only for an ability whose
						// ActivationZone$ names exile; skip every other exiled card
						// before the per-ability gates (the zone can be large).
						continue
					}
					if e.faceDownPrintedHides(o) {
						// CR 708.8: a face-down permanent's printed activated abilities
						// and mana abilities do not exist while it is face down, and
						// turn-face-up (CR 708.6) is not implemented -- nothing on a
						// face-down permanent is offered at all.
						continue
					}
					// CR 702.140d: a mutated permanent has the top card's abilities
					// PLUS all abilities of the cards beneath it. Walk the FLAT pile
					// list (top face first, then each under-card): the flat index is
					// the identity AbilityPush records and the activation-limit
					// census counts, and a top-face ability keeps exactly its old
					// index. abFace is the face that carries the ability -- an
					// under-card's label and SVar table must be its own, never the
					// pile top's.
					for i, pn := 0, o.PileAbilityCount(); i < pn; i++ {
						pa, okAb := o.PileAbilityAt(i)
						if !okAb {
							continue
						}
						ab := pa.SA
						abFace := o.PileFaceFor(pa.Merged)
						if abFace == nil {
							continue
						}
						if ab.Kind != "AB" {
							continue
						}
						// CR 605.1b: a mana ability is never a loyalty ability, so a
						// loyalty-marked AB$ Mana (Koth's [+1], Ugin, Eye of the
						// Storms' [0]: Add {C}{C}{C}) is NOT exempted here: it is a
						// loyalty ability, offered through this loop under the CR 606.3
						// gates below -- sorcery timing, once per permanent per turn --
						// exactly like every other [+N]/[-N]. The mana-ability path
						// (availableManaAbilitiesUsing) excludes it symmetrically; the
						// exclusion there is what closed the ulalek-eldrazi seed-1019
						// livelock (an un-tapping, gate-free, zero-cost repeatable
						// +3 colourless activation re-offered every priority window).
						//
						// The zone gate and the loyalty read come from the configured
						// ability's facts (mana_safacts.go: abilityZoneOK per zone and
						// isLoyaltyAbility, verified in the rules test binary) when the
						// ability has them, else from the ordinary readers. Both gates
						// only skip, so testing the zone first -- the one a hand,
						// graveyard or stack card's battlefield ability fails -- selects
						// exactly the abilities the mana-then-zone order did.
						var loyal bool
						if mf := e.manaFactsOf(ab); mf != nil {
							if manaSAFactsVerify && (mf.zoneOKFact(ab, z) != abilityZoneOK(ab, z) || mf.loyalty != e.isLoyaltyAbility(ab)) {
								panic(fmt.Sprintf("rules: configured ability facts for %q disagree with the zone/loyalty readers", ab.Line))
							}
							if !mf.zoneOKFact(ab, z) {
								continue
							}
							loyal = mf.loyalty
						} else {
							if !abilityZoneOK(ab, z) {
								continue
							}
							loyal = e.isLoyaltyAbility(ab)
						}
						if isManaAbilityAPI(ab.API) && !loyal {
							continue
						}
						if ab.Params["SorcerySpeed"] == "True" && !sorcery {
							continue
						}
						// Activator$ constrains who may activate the ability, not who
						// controls its source. Resolve You/Opponent relative to the
						// source's current controller and bind source-dependent selectors
						// to the ability's permanent; unknown selectors fail closed.
						if !e.activatorAllows(p, id, ab) {
							continue
						}
						// PlayerTurn$ True (Wishclaw Talisman's "Activate only during
						// your turn"): the ability is offered only while its
						// controller is the active player. CR 602.1b would otherwise
						// offer it on any player's priority. ActivationPhases$ and the
						// other window riders (OpponentTurn$, ActivationFirstCombat$,
						// ActivationAfterBlockers$) ride the same shared
						// offer-time gate, so one helper covers the cast and ability
						// halves alike.
						if !e.activationPhasesOK(p, ab) {
							continue
						}
						// ActivationGameTypes$ (activationGameTypesOK, above): a comma
						// list of the formats the ability exists in. In a Constructed
						// game every token list fails closed and the ability is
						// withheld -- one gate here covers both the real-pool offer and
						// the hypothetical walk (offerCastable's hyp variants share
						// this loop body).
						if raw, ok := ab.Params["ActivationGameTypes"]; ok && !activationGameTypesOK(e.format, raw) {
							continue
						}
						// CR 606.3: a planeswalker's loyalty ability may be activated
						// only at the time a sorcery could be played -- during the
						// controller's own main phase with an empty stack -- and a
						// player may not activate a loyalty ability of a PERMANENT if
						// any loyalty ability OF THAT PERMANENT has already been
						// activated this turn. The gate is per permanent, not per
						// ability index: after [+2] the [0] draw-three is just as
						// withheld as a second [+2]. sorcerySpeed already folds the
						// own-turn and main-phase halves; the once-per-turn half is
						// the loyaltyActivationsThisTurn scan below (the event-log
						// scan the ActivationLimit$ gate uses, keyed to the object
						// and bounded by its current battlefield stint, CR 400.7),
						// because no corpus loyalty ability carries ActivationLimit$
						// and the gate must exist anyway (before this gate the
						// [+2]/[0] abilities were offered, payable and repeatable
						// without bound -- the live Jace draw-three exploit).
						if loyal {
							if !sorcery {
								continue
							}
							if e.loyaltyActivationsThisTurn(id) >= e.loyaltyAbilityLimit(id) {
								continue
							}
						}
						if abilityRestricted(p, id, ab) {
							continue
						}
						// Activation$ (Sea Gate Wreckage's "Activate only if you have
						// no cards in hand"): the keyword activation condition at offer
						// time, the same funnel the CheckSVar$ gate below applies --
						// a gate you can read must not leave a paid no-op reachable.
						if !e.activationConditionOK(p, ab) {
							continue
						}
						// F05-2 (CR 733.2): a card whose activation aborted with no
						// progress twice in this window is held out here too, exactly
						// like the cast options above -- the suppression is per CARD
						// (abortCast keys it on pc.card, which for an activation is the
						// source), and the abort sites cover "cast/activation" alike.
						// Without this check an activation the payer cannot complete
						// (a mis-answered phyrexian pip, a pool that moved) re-offered
						// forever inside one priority window: measured, the commander
						// bench spun 20000 intents on Solphim's {1}{R/P}{R/P} ability
						// (seed 1295, 2026-09-15) because the ability-offer path never
						// read the map the abort wrote.
						if e.castSuppressed(p, id) {
							continue
						}
						if e.activationLimitBlocked(p, id, ab, i, "", pa.Merged) {
							continue
						}
						// kw:Boast (CR 702.142): a Boast ability (Forge's `Boast$ True`
						// parameter on the AB, not a K: keyword line) may be activated
						// only if the source creature attacked this turn, and only once
						// each turn. The once-per-turn half folds into the same
						// activation-event scan the ActivationLimit$ gate uses.
						if strings.EqualFold(strings.TrimSpace(ab.Params["Boast"]), "True") && !e.boastGateOK(id, i, "") {
							continue
						}
						cost := e.parseCost(ab.Params["Cost"])
						// The ability's own ReduceCost$ (Otawara's Channel): the CR
						// 601.2f composition the offer gate and beginActivation's
						// charge share, so an offered cost and the paid one agree.
						// ownReduceCostOffer resolves a target-dependent body against
						// the best legal root target, because the chosen target does
						// not exist at offer time (belt_of_giant_strength).
						if n := e.ownReduceCostOffer(p, id, ab, pa.Merged); n > 0 && cost.Generic >= n {
							cost.Generic -= n
						} else if n > 0 {
							cost.Generic = 0
						}
						if activationTapCostUnavailable(o, cost) || e.tapCostSick(id, cost) {
							continue
						}
						// CR 702.6 / CR 601.2f: a minted attach-cost SA (K:Equip/K:Fortify,
						// cards/kw_equip.go) whose rider carries AlternateCost$ -- the
						// fourth colon field -- is an alternative cost the activator may
						// pay INSTEAD of the printed one. Offer it as its own "ability"
						// option, exactly the way the cast walk offers an AlternativeCost
						// static's cost as its own "cast" option (AltCostIndex = 1 marks
						// "the alternate cost", 0 the printed one --
						// decision.Option.AltCostIndex). The rider is evaluated
						// INDEPENDENTLY of the printed cost: an equip whose printed cost
						// is unpayable but whose alternate is payable must still be
						// offered (that is the whole point of "pay {B} instead" for
						// Transmogrant's Crown). abilityAlternateCost scopes itself to
						// the minted attach-cost SAs (isAttachCostSA) -- an AB$ line's
						// own AlternateCost$ parameter stays unread here -- and fails
						// closed on an unpriceable rider, so no unpayable option is ever
						// offered, and the ability is withheld only when NEITHER cost is
						// payable.
						altCost, hasAlt := e.abilityAlternateCost(ab)
						printedOK := offerCastable(p, id, cost, abilityScope(ab), true)
						altOK := hasAlt && offerCastable(p, id, altCost, abilityScope(ab), true)
						if !printedOK && !altOK {
							continue
						}
						if !e.abilityTargetsAvailable(p, id, ab) {
							continue
						}
						// CR 603.2's intervening-if at activation: an ability whose
						// CheckSVar$ fails is not offered (Bloodsoaked Champion's Raid —
						// "Activate only if you attacked this turn"). Offer time, not
						// resolve time: the resolution runs the effect's own
						// Condition* gate (conditionMet) where the SA carries one; an
						// offered-but-gated activation that resolves into nothing would
						// be a paid no-op the offer loop could have withheld.
						if !e.sVarGateOK(p, id, ab, pa.Merged) {
							continue
						}
						// IsPresent$/PresentCompare$ (Mistveil Plains' "Activate only if
						// you control two or more white permanents"): the same offer-time
						// gate funnel as the CheckSVar$ read above.
						if !e.abilityPresentHolds(p, id, ab) {
							continue
						}
						// Adapt$ (CR 702.35a): "Activate only if this creature has no
						// +1/+1 counters on it" -- the offer-time twin of the effect's own
						// if-condition effects/counters.go enforces at resolution.
						if !e.adaptGateOK(id, ab) {
							continue
						}
						// Monstrosity$ (CR 701.31b): "Activate only if this creature
						// isn't monstrous" -- the once-only monstrosity gate, the same
						// offer-time funnel the Adapt$ gate above sits in.
						if !e.monstrosityGateOK(id, ab) {
							continue
						}
						// kw:Reconfigure (CR 702.150): the expansion's unattach half
						// carries Unattach$ True and is offered only while the source is
						// attached -- "unattach from a creature" has no legal action for
						// an unattached permanent, and a payable no-op the deterministic
						// bot can answer identically forever is the livelock shape the
						// offer gates exist to withhold.
						if strings.EqualFold(strings.TrimSpace(ab.Params["Unattach"]), "True") && o.AttachedTo == 0 {
							continue
						}
						if printedOK {
							out = append(out, decision.Option{Index: len(out), Kind: "ability",
								Label: abFace.Name + ": " + ab.Params["SpellDescription"], Obj: id, Ability: i,
								Cost:  e.abilityOfferCost(p, id, ab),
								Grant: e.abilityGrant(id, ab), Attach: ab.API == "Attach",
								GrantStatics: staticModesFromSVars(ab, abFace.SVars)})
						}
						if altOK {
							out = append(out, decision.Option{Index: len(out), Kind: "ability",
								Label: abFace.Name + ": " + ab.Params["SpellDescription"] + " (alternate cost)",
								Obj:   id, Ability: i, AltCostIndex: 1, Grant: e.abilityGrant(id, ab), Attach: ab.API == "Attach",
								GrantStatics: staticModesFromSVars(ab, abFace.SVars)})
						}
					}
					// Keyword-granted cycling (CR 613.1f): a layer-6 AddKeyword$
					// Cycling/TypeCycling grant (Tectonic Reformation, Rhet-Tomb Mystic,
					// Jo Grant, Homing Sliver) gives a hand card a cycling ability NO
					// printed face carries, so the pile walk above never offers it.
					// Synthesize the same body the printed expansion builds
					// (cards.GrantedCyclingAbility) and offer it through the same gates,
					// anchored on the derived keyword line beginActivation resolves --
					// exactly the SVar-anchor shape with the line standing in for the
					// name. A line the printed face (or a pile under-card) already
					// expands is skipped -- the printed offer exists -- and a shape the
					// synthesizer cannot model is skipped whole (fail closed). The
					// synthesized body carries no SorcerySpeed$/Tap/Loyalty/CheckSVar$
					// rider and no ReduceCost$ (its Discard-only cost is the whole
					// non-mana half), so the pile walk's rider gates have no twin here;
					// the offer gate prices the discard's satisfiability exactly as the
					// printed cycling offer does.
					//
					// Both synthesized bodies (cards' cyclingAbilitySA and
					// typeCyclingAbilitySA) carry ActivationZone$ Hand, so outside
					// the hand every line fails the abilityZoneOK gate below and
					// the derived-keyword read is skipped outright (verify mode
					// runs it and panics on a line the gate would have kept).
					var cyclingLines []string
					if z == state.ZHand {
						cyclingLines = e.grantedCyclingLines(id)
					} else if derivedMemoVerify {
						verifyCyclingHandOnly(e.grantedCyclingLinesFull(id), id, z)
					}
					for _, line := range cyclingLines {
						ab := cards.GrantedCyclingAbility(line)
						if ab == nil || !abilityZoneOK(ab, z) {
							continue
						}
						if abilityRestricted(p, id, ab) || e.castSuppressed(p, id) {
							continue
						}
						if e.activationLimitBlocked(p, id, ab, -1, line, 0) {
							continue
						}
						cost := e.parseCost(ab.Params["Cost"])
						if n := e.ownReduceCostOffer(p, id, ab, 0); n > 0 && cost.Generic >= n {
							cost.Generic -= n
						} else if n > 0 {
							cost.Generic = 0
						}
						if !offerCastable(p, id, cost, abilityScope(ab), true) {
							continue
						}
						if !e.abilityTargetsAvailable(p, id, ab) {
							continue
						}
						out = append(out, decision.Option{Index: len(out), Kind: "ability",
							Label: f.Name + ": " + ab.Params["SpellDescription"], Obj: id,
							Ability: -1, Keyword: line})
					}
				}
			}
		}

		// Granted activated abilities (CR 613.1f, rules/legal.go's
		// grantedAbilities): the abilities an AddAbilities continuous grant -- a
		// Saga chapter's Animate, "CARDNAME gains '{T}: Add {C}'." -- gives a
		// permanent. Non-mana ones are offered here with the SVar anchor
		// beginActivation resolves (the same anchor the max-speed "granted"
		// option carries); mana ones flow through availableManaAbilities below so
		// the "Tap for mana" priority action and the payment window share one
		// member set. Gates mirror the printed loop above, loyalty gate included:
		// a GAINED ability (GainsAbilitiesOf$, Nicol Bolas Dragon-God's
		// `GainsValidAbilities$ Activated.Loyalty`) and an SVar-anchored GRANT
		// (Rowan's Talent's AddAbility$ [+1]) can each be a loyalty ability, so
		// the CR 606.3 gates below apply to them exactly as to a printed one. The two
		// activation limits are checked here too, with the SVar-name identity
		// (see the gate's own comment below): Touch of Vitae carries
		// GameActivationLimit$ 1 on an Animate-delivered AddAbility$ body.
		for zonePlayer := range e.G.Players {
			for _, id := range e.G.Zone(state.ZBattlefield, state.PlayerID(zonePlayer)) {
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil || e.faceDownPrintedHides(o) {
					// A face-down permanent is not offered granted abilities: the
					// offer label reads the printed face name, which CR 708.8 says
					// does not exist while face down.
					continue
				}
				if !existsOnBattlefield(o) {
					// CR 702.25b: a phased-out permanent is treated as though it does
					// not exist, so none of its granted or gained activated abilities
					// is offered or activatable.
					continue
				}
				for _, ga := range e.grantedAbilities(p, id) {
					ab := ga.sa
					// isManaAbilityAPI, not a bare "Mana" check: a granted
					// ManaReflected flows through availableManaAbilities too (its
					// IsPresent$ gate lives in manaReflectedPresentHolds, which knows
					// the hasAbility Activated.otherAbility special form).
					if isManaAbilityAPI(ab.API) {
						continue
					}
					if ab.Params["SorcerySpeed"] == "True" && !sorcery {
						continue
					}
					// Activator$ applies to a granted/gained ability exactly as to a
					// printed one (the source is the recipient permanent).
					if !e.activatorAllows(p, id, ab) {
						continue
					}
					// CR 606.3 for a GAINED or GRANTED loyalty ability: the same
					// sorcery-timing and once-per-permanent gates the printed loop
					// applies -- a gained (GainsAbilitiesOf$) or SVar-granted
					// (AddAbility$: Rowan's Talent's "[+1]: Up to one target creature
					// gets +2/+0 ...") loyalty ability is a loyalty ability of THIS
					// permanent (the recipient), and loyaltyActivationsThisTurn counts
					// its GainedAbilityPush / GrantAbilityPush activations beside the
					// printed AbilityPush ones. The granted half was once exempt on the
					// premise that an AddAbility$ body is never a loyalty ability;
					// Rowan's Talent's body is one, and the exemption let a bot
					// activate it without bound (cardfuzz batch1 line 18: 20000
					// intents of "+1" on one Jaya Ballard in one main phase).
					if e.isLoyaltyAbility(ab) {
						if !sorcery {
							continue
						}
						if e.loyaltyActivationsThisTurn(id) >= e.loyaltyAbilityLimit(id) {
							continue
						}
					}
					// F05-2 (CR 733.2): the granted twin of the printed loop's
					// no-progress hold-out. abortCast keys the suppression on the
					// activation's source (pc.card), which for a granted or gained
					// ability is this recipient -- without the check a granted
					// activation whose transaction aborts (no legal target, an
					// unpayable cost) was re-offered inside one priority window
					// forever (cardfuzz batch5 line 1: Trazyn's gained Equip).
					if abilityRestricted(p, id, ab) || e.castSuppressed(p, id) {
						continue
					}
					cost := e.parseCost(ab.Params["Cost"])
					// The granted twin of the printed loop's own ReduceCost$ fold.
					if n := e.ownReduceCostOffer(p, id, ab, 0); n > 0 && cost.Generic >= n {
						cost.Generic -= n
					} else if n > 0 {
						cost.Generic = 0
					}
					if activationTapCostUnavailable(o, cost) || e.tapCostSick(id, cost) {
						continue
					}
					if !offerCastable(p, id, cost, abilityScope(ab), true) {
						continue
					}
					if !e.abilityTargetsAvailable(p, id, ab) {
						continue
					}
					// IsPresent$/PresentCompare$: the same offer-time gate the printed
					// loop applies, so a granted ability and its printed twin share one
					// eligibility set.
					if !e.abilityPresentHolds(p, id, ab) {
						continue
					}
					// Adapt$ (CR 702.35a): the granted twin of the printed loop's gate.
					if !e.adaptGateOK(id, ab) {
						continue
					}
					// A has-all-abilities-of gained ability (GainsAbilitiesOf$) is
					// offered with its foreign-card anchor; every other granted
					// ability keeps the SVar-name anchor (boastGateOK's and the
					// activation-limit gate's identity).
					if ga.gained {
						if strings.EqualFold(strings.TrimSpace(ab.Params["Boast"]), "True") && !e.boastGateOK(id, -1, "") {
							continue
						}
						if e.activationLimitBlocked(p, id, ab, -1, "", 0) {
							continue
						}
						out = append(out, decision.Option{Index: len(out), Kind: "ability",
							Label: o.Face().Name + ": " + ab.Params["SpellDescription"], Obj: id,
							GainedSource: ga.gainedFrom, GainedIdx: ga.gainedIdx, Attach: ab.API == "Attach"})
						continue
					}
					// kw:Boast (CR 702.142): the granted twin of the printed loop's
					// Boast gate. The identity is the SVar name the grant anchored on,
					// because beginGrantedActivation mints a DelayedPush rather than an
					// AbilityPush (boastGateOK reads both).
					if strings.EqualFold(strings.TrimSpace(ab.Params["Boast"]), "True") && !e.boastGateOK(id, -1, ga.svar) {
						continue
					}
					// The two activation limits, for a GRANTED ability: the same shared
					// gate the printed loop above calls, with the SVar-name identity
					// because the mint is a DelayedPush/GrantAbilityPush. The printed
					// loop's old claim -- "no corpus granted ability carries a limit" --
					// is FALSE: Touch of Vitae carries GameActivationLimit$ 1 on the
					// AddAbility$ body it animates onto a target. (That specific grant
					// does not resolve yet for an unrelated reason -- its SVar lives on
					// the Instant's face, while Animate resolves granted names off the
					// ANIMATED object's table; see the report's Issues.) The gate is kept
					// so a granted ability with a limit is never re-offered once used,
					// and self-animate grants -- where the SVar table IS the recipient's
					// -- are pinned by TestGameActivationLimitGrantedAbilityWithheldAfterOneUse.
					if e.activationLimitBlocked(p, id, ab, -1, ga.svar, 0) {
						continue
					}
					// Offer only what the activation can resolve. The collector
					// above reads the body off the emitting effect's captured SVar
					// table (ce.SVars), while beginGrantedActivation -- and the
					// GrantAbilityPush/DelayedPush mint a replay re-runs -- resolve
					// the NAME against the grantor object's faces. When the two
					// disagree (the grantor's face does not carry the table the
					// effect captured) the option was a silent no-op: chosen, it
					// emitted nothing and the identical board re-offered it forever
					// (cardfuzz batch7 line 2: a gained-Animate grant on Manascape
					// Refractor, 100x "Regenerate CARDNAME" in one main phase).
					if e.grantedSAFrom(ga.source, id, ga.svar) == nil {
						continue
					}
					out = append(out, decision.Option{Index: len(out), Kind: "ability",
						Label: o.Face().Name + ": " + ab.Params["SpellDescription"], Obj: id, SVar: ga.svar,
						GrantSource: ga.source, Attach: ab.API == "Attach"})
				}
			}
		}

		// kw:Station (CR 702.150, rules/station.go): each Spacecraft the player
		// controls may be stationed as a sorcery by tapping another creature.
		// The offer is gated on the sorcery window (Station only as a sorcery)
		// and on a legal tap candidate existing, so an unpayable station is
		// never offered; the tap candidate itself is the KChoose askStation
		// poses after the option is chosen.
		if sorcery {
			for _, id := range e.G.Zone(state.ZBattlefield, p) {
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil || !e.hasKeywordH(id, kwhStation) {
					continue
				}
				if !existsOnBattlefield(o) {
					// CR 702.25b: a phased-out permanent is treated as though it
					// does not exist, so it cannot be stationed.
					continue
				}
				if len(e.stationCandidates(p, id)) == 0 {
					continue
				}
				add("station", "Station "+o.Face().Name, id)
			}
			// Room unlock (CR 309.5, rules/rooms.go): a room whose second door is
			// still locked may be unlocked as a sorcery by paying that half's own
			// mana cost. The gate is the same castable total the cast options
			// use, so an unpayable unlock is never offered (and the payment on
			// the answer cannot disagree with the offer).
			for _, id := range e.G.Zone(state.ZBattlefield, p) {
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil || e.faceDownPrintedHides(o) {
					continue
				}
				if !existsOnBattlefield(o) {
					// CR 702.25b: a phased-out room is treated as though it does
					// not exist, so it cannot be unlocked.
					continue
				}
				cost, ok := e.unlockRoomCost(o)
				if !ok {
					continue
				}
				if offerCastable(p, id, cost, costScope{kind: "Ability"}, true) {
					add("unlock", "Unlock "+roomLockedFace(o).Name, id)
				}
			}
		}

		// kw:Start your engines (CR 702.179e, rules/speed.go): a max-speed
		// static grants its AddAbility$ while its controller has speed 4, and
		// the granted ability is offered through the same cost/target gates
		// every other activation uses. NOT sorcery-gated: the grant is an
		// ordinary activated ability (Amonkhet Raceway's {T}: pump) whose timing
		// is its own cost's -- it needs a priority window, not a main phase, so
		// the offer sits outside the sorcery block with the other activation
		// offers.
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			if !existsOnBattlefield(o) {
				// CR 702.25b: a phased-out permanent is treated as though it
				// does not exist, so its max-speed granted abilities are not
				// offered.
				continue
			}
			for _, ab := range e.maxSpeedAbilities(p, id) {
				// castSuppressed: the F05-2 no-progress hold-out every other
				// activation offer reads (an aborted activation is keyed on its
				// source).
				if abilityRestricted(p, id, ab) || e.castSuppressed(p, id) {
					continue
				}
				cost := e.parseCost(ab.Params["Cost"])
				if activationTapCostUnavailable(o, cost) || e.tapCostSick(id, cost) {
					continue
				}
				if !offerCastable(p, id, cost, abilityScope(ab), true) {
					continue
				}
				if !e.abilityTargetsAvailable(p, id, ab) {
					continue
				}
				// IsPresent$/PresentCompare$: the same offer-time gate the printed
				// loop applies, so a max-speed grant and its printed twin share one
				// eligibility set.
				if !e.abilityPresentHolds(p, id, ab) {
					continue
				}
				// Adapt$ (CR 702.35a): the max-speed grant's twin of the same gate.
				if !e.adaptGateOK(id, ab) {
					continue
				}
				// kw:Boast (CR 702.142): the max-speed grant is a third offer site
				// for an SVar-anchored ability, so it shares the Boast gate. The
				// identity is the SVar name beginGrantedActivation mints its
				// DelayedPush with (abSVarName), never a face index.
				sv := abSVarName(o.Face(), ab)
				if strings.EqualFold(strings.TrimSpace(ab.Params["Boast"]), "True") && !e.boastGateOK(id, -1, sv) {
					continue
				}
				out = append(out, decision.Option{Index: len(out), Kind: "granted",
					Label: o.Face().Name + ": " + ab.Params["SpellDescription"],
					Obj:   id, SVar: sv})
			}
		}
		// Morph-family turn face up (CR 708.6 / CR 116.2b, rules/morph_turnup.go):
		// a face-down permanent its controller cast with Morph, Megamorph or
		// Disguise may be turned face up as a SPECIAL ACTION any time they have
		// priority -- it is not sorcery-gated, does not use the stack, and its
		// only cost is the keyword's own printed parameter. The offer is gated on
		// the same floating pool the action pays, so the charge cannot disagree
		// with what was offered; a manifest or cloak carrier (no family flag) is
		// never offered here -- its turn-up is a separate subsystem.
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			// morphFaceUpCost answers false for any object that is not face
			// down; test that flag before building its cost.
			if o := e.G.Obj(id); o == nil || !o.FaceDown {
				continue
			}
			mf, ok := morphFaceUpCost(e.G.Obj(id))
			if !ok {
				continue
			}
			// morphTurnUpPayablePriced: hyp nil is the floating-pool gate the
			// action itself re-reads; the potential walk prices the same cost
			// against its hypothetical bound, so a float-gated turn-up is a
			// potential play like every other mana-costed offer.
			if !e.morphTurnUpPayablePriced(p, id, mf.cost, hyp) {
				continue
			}
			if e.turnFaceUpCantHappen(id) {
				// CR 614.1a: a live CantHappen turn-up replacement (Karlov
				// Watchdog's "permanents your opponents control can't be turned
				// face up during your turn") makes the special action illegal, so
				// the option is never offered -- and never paid for. The one match
				// predicate lives in rules/replacement.go (turnFaceUpCantHappen);
				// the submitted-option guard in rules/priority_guard.go re-reads
				// the same helper.
				continue
			}
			add("turn_face_up", "Turn face up ("+costPhrase(mf.cost)+")", id)
		}

		// Specialize is a no-stack special action and may be taken only as a
		// sorcery. Each legal target face is a separate option, avoiding a second
		// decision while preserving player choice.
		if sorcery {
			for _, id := range e.G.Zone(state.ZBattlefield, p) {
				o := e.G.Obj(id)
				if o == nil || o.Card == nil {
					continue
				}
				for i := 1; i < len(o.Card.Faces); i++ {
					// hyp nil is specializeLegal exactly; the potential walk
					// prices the printed cost against its hypothetical bound.
					cost, ok := e.specializeLegalPriced(p, id, i, hyp)
					if !ok {
						continue
					}
					add("specialize", "Specialize as "+o.Card.Faces[i].Name+" ("+costPhrase(cost)+")", id)
					out[len(out)-1].Mode = strconv.Itoa(i)
				}
			}
		}
	}

	// K:Split second (CR 702.62, rules/split_second.go): while a split-second
	// spell is on the stack, players can't cast spells or activate abilities
	// that aren't mana abilities. The filter runs here -- at the ONE choke
	// point every cast source (hand, command zone, may-play, flashback/
	// aftermath/harmonize/warp/escape, exile recasts) and both ability loops
	// flow through -- rather than at each of the ~30 append sites, so the next
	// cast source added to this walk is covered by construction. Playing a
	// land, mana abilities, Station and Room unlock stay legal; the Suspend
	// and Foretell offers ride the "cast" Kind but are special actions, not
	// spell casts, so they stay too.
	// CR 118.6: a no-mana-cost card is never cast by paying its mana cost
	// (rules/nomanacost.go).
	out = e.filterNoManaCostCasts(p, out)
	// Options the inert backstop caught changing nothing this window
	// (rules/priority_guard.go) stay out until the game changes state.
	out = e.filterInertHeldOut(out)
	if e.splitSecondHolds() {
		out = e.filterSplitSecondActions(out)
	}

	// Pass is second-to-last. A client that wants to do nothing must choose
	// it explicitly: from M2d-3 the FINAL option is "concede" (R-M3, always
	// last), and a client defaulting to the final option would concede on
	// every single priority decision.
	add("pass", "Pass priority", 0)
	// M2d-3 (R-M3): concession, last after pass. Choosing it emits the
	// existing PlayerLost event with Text "conceded" (CR 104.3a) -- see
	// handlePriority. Offered on every priority decision, i.e. to every
	// living seat: grantPriority never hands a Lost seat priority, so no
	// extra guard is needed here.
	add("concede", "Concede", 0)
	res := make([]decision.Option, len(out))
	copy(res, out)
	if window != nil {
		// Classify each of p's own visible candidates the walk did not
		// offer, keeping the first gate in walk order that withheld it. The
		// caller that owns the decision finishes (filters offered
		// candidates and bounds) the collector.
		e.windowClassify(p, res, window)
	}
	// Drop the scratch's string/Grant references so a retained buffer does
	// not pin the last walk's labels, then keep the grown array.
	clear(out)
	e.legalOptBuf = out[:0]
	if castsOnly && castsOnlyWalkVerify {
		verifyCastsOnlyWalk(res, e.legalActionsPriced(p, hyp))
	}
	return res
}

// castsOnlyWalkVerify makes every castsOnly walk also run the full walk and
// panic unless its options are exactly the full walk's "cast" options
// (Index aside). Set by the rules test binary (derivedmemo_verify_test.go),
// or at link time with derivedMemoVerifyFlag.
var castsOnlyWalkVerify = derivedMemoVerifyFlag != ""

func verifyCastsOnlyWalk(got, full []decision.Option) {
	var want []decision.Option
	for _, o := range full {
		if o.Kind == "cast" {
			want = append(want, o)
		}
	}
	var casts []decision.Option
	for _, o := range got {
		if o.Kind == "cast" {
			casts = append(casts, o)
		}
	}
	same := len(casts) == len(want)
	for i := 0; same && i < len(want); i++ {
		a, b := casts[i], want[i]
		a.Index, b.Index = 0, 0
		same = reflect.DeepEqual(a, b)
	}
	if !same {
		panic(fmt.Sprintf("rules: castsOnly walk casts %+v, full walk casts %+v", casts, want))
	}
}

// firstChosen is d.Chosen(in)[0] without materialising the chosen list (a
// heap copy of every chosen Option on every priority answer). It keeps
// Chosen's all-or-nothing contract: any out-of-range index, or no choice at
// all, is the same index-out-of-range panic the [0] of a nil list raised.
func firstChosen(d *decision.Decision, in decision.Intent) decision.Option {
	var none []decision.Option
	for _, c := range in.Choices {
		if c < 0 || c >= len(d.Options) {
			return none[0]
		}
	}
	if len(in.Choices) == 0 {
		return none[0]
	}
	return d.Options[in.Choices[0]]
}

func (e *Engine) handlePriority(d *decision.Decision, in decision.Intent) {
	opt := firstChosen(d, in)
	if opt.Kind != "pass" && opt.Kind != "concede" {
		// The inert backstop (rules/priority_guard.go): an action whose
		// handler emits nothing past the priority reset is recorded and
		// held out instead of being re-offered forever.
		mark := len(e.L.Events)
		defer e.inertPriorityBackstop(in.Player, opt, mark)
	}
	switch opt.Kind {
	case "pass":
		passes := e.G.Passes + 1
		if passes >= int32(e.G.AliveCount()) {
			if len(e.G.Stack) > 0 {
				e.resolveTop()
				// CR 117.5: nobody receives priority in the middle of a
				// resolution. A resolution that suspends on a mid-resolution
				// ask (a modal spell's KModes, an as-enters choose, an
				// unless-pay, a discard) is parked on that question: no player
				// has priority while the question is outstanding, so the log
				// must not record that priority returned to the active player
				// here. The one and only grant for that resolution happens when
				// it actually completes: the answering Submit re-enters
				// grantPriority (through resumeTriggerDrain / the step loop)
				// once e.resume is cleared, so an unsuspended resolution emits
				// the priority-returns-to-active marker below while a suspended
				// one defers it to its completion. Exactly one grant either
				// way; a resolution that suspends more than once (a nested ask)
				// still completes once and grants once.
				if e.Suspended() {
					return
				}
				// The pass count resets: priority returns to the active
				// player after a resolution, same as at the start of any
				// other step.
				e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
				return
			}
			// advanceStep's own emit carries the reset pass count; the count
			// this round reached is never itself a value anything observes.
			//
			// CR 514.3b (mayflashsac2, review round 3): an emptied cleanup-step
			// stack is NOT licence to advance. The rules require the cleanup
			// procedure to REPEAT, redoing its 514.1/514.2 actions, after a
			// trigger resolves or a player acts in this window -- an instant
			// cast here (Giant Growth) must have its 'until end of turn' effect
			// expire in the repeated cleanup, and a trigger that drew the
			// active player over the hand limit must face the repeat's
			// discard. advanceStep would instead begin the next turn outright,
			// skipping both. repeatCleanup runs the whole procedure once more
			// and itself reaches advanceStep only when nothing is waiting.
			if e.G.Step == state.StepCleanup {
				e.repeatCleanup()
				return
			}
			e.advanceStep()
			return
		}
		e.emit(events.Event{Kind: events.Priority, Player: e.G.NextAlive(e.G.Priority), Amount: passes})

	case "play_land":
		// A modal-land option is a face selection, not a generic land play.
		// Revalidate it against the current object before mutating state: the
		// priority option may have gone stale while another decision resolved.
		if opt.Mode == "modal_land" {
			if modalLandBack(e.G.Obj(opt.Obj)) == nil {
				return
			}
			e.emit(events.Event{Kind: events.FlipFace, Obj: opt.Obj, Amount: 1})
		}
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		// A land play uses the ordinary pending cast flow so payCast can put
		// LandPlayed after its MoveZone entry boundary. The MoveZone replacement
		// owns any "as this enters" choice, like every other entry path.
		//
		// The source zone is the object's CURRENT zone, not hardcoded to the
		// hand: since the MayPlay grants (rules/mayplay.go) the play_land
		// offer also comes from the graveyard, exile or the top of the
		// library, a hand land and a graveyard land must move From the zone
		// they were actually offered from (CR 118.3a -- the permission names
		// the zone it grants). A land that somehow left its zone between the
		// offer and the answer resolves from whatever zone it is in, and the
		// MoveZone/LandPlayed below still records a legal play.
		from := state.ZHand
		if o := e.G.Obj(opt.Obj); o != nil {
			from = o.Zone
		}
		e.cast = &pendingCast{player: in.Player, card: opt.Obj, from: from, mode: "land", ability: -1}
		e.continueCast()

	case "activate":
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		e.activateMana(in.Player, opt.Obj, false)

	case "ability":
		// Task 10: an activated ability (non-mana AB$) was chosen. Reset the
		// pass count the same way every other non-pass action does, then drive
		// the same cost flow a cast drives (rules/activate.go's
		// beginActivation -> pendingCast -> continueCast -> commitCast).
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		e.beginActivation(in.Player, opt)

	case "concede":
		// M2d-3 (R-M3): choosing the concede option emits the existing
		// PlayerLost event with Text "conceded" (CR 104.3a) -- one event,
		// the same one a 0-life elimination emits. PlayerLost's Apply marks
		// the seat Lost; this checkStateBased then sweeps its permanents
		// (CR 800.4a) and ends the game with the last remaining seat the
		// winner (checkGameOver, CR 104.2a) -- a concession is just another
		// way to be Lost. With three or more seats still alive, Submit's own
		// tail Advance continues the match with the Lost seat skipped
		// everywhere (grantPriority, NextAlive, beginTurn).
		e.emit(events.Event{Kind: events.PlayerLost, Player: in.Player, Text: "conceded"})
		e.checkStateBased()

	case "station":
		// kw:Station (CR 702.150, rules/station.go): the spacecraft is
		// stationed by tapping another creature the KChoose below names. The
		// pass-count reset matches every other non-pass action.
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		e.askStation(in.Player, opt)

	case "unlock":
		// Room unlock (CR 309.5, rules/rooms.go): pay the locked half's mana
		// cost and emit the DoorUnlock event. The offer gated on castable,
		// so the payment here cannot disagree with the offer; a stale option
		// (the room left play or was unlocked between offer and answer -- the
		// same seat's answer, so the board cannot have moved) degrades to a
		// no-op through unlockRoomCost's nil face.
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		o := e.G.Obj(opt.Obj)
		cost, ok := e.unlockRoomCost(o)
		if !ok {
			return
		}
		if !e.payMana(in.Player, cost) {
			return
		}
		e.emit(events.Event{Kind: events.DoorUnlock, Obj: opt.Obj})

	case "granted":
		// kw:Start your engines (CR 702.179e, rules/speed.go): a max-speed
		// static's granted ability, activated through the ordinary cost
		// payment and the delayed-shape ability mint.
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		e.beginGrantedActivation(in.Player, opt)

	case "specialize":
		e.specialize(in.Player, opt)

	case "turn_face_up":
		// Morph-family turn face up (CR 708.6 / CR 116.2b): a special action
		// -- no stack, no target, no response window. rules/morph_turnup.go
		// owns the payment and the TurnFaceUp/megamorph-counter events.
		e.turnFaceUp(in.Player, opt)

	case "cast":
		e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
		e.beginCast(in.Player, opt)
	}
}

// abilityGrant builds the server-side decision.Grant no-op flag for an
// activated ability ab on the permanent id (the option's Obj), only when the
// ability's whole effect is a pure, idempotent keyword grant. It returns nil
// for every other ability, so an additive or non-grant activation is never
// mistaken for a repeatable no-op. For a pure keyword grant it sets the two
// independent redundant halves the bot policy's no-op rule (A1) reads:
// Already (the granting permanent currently has every granted keyword, so
// the grant is already in effect) and Duplicate (an identical grant from the
// same source is already on the stack unresolved).
func (e *Engine) abilityGrant(id state.ObjID, ab *cards.SA) *decision.Grant {
	kw := pureGrantKeywords(ab)
	if len(kw) == 0 {
		return nil
	}
	g := &decision.Grant{Keywords: kw}
	all := true
	for _, k := range kw {
		if !e.HasKeyword(id, k) {
			all = false
			break
		}
	}
	g.Already = all
	g.Duplicate = e.grantPending(id, kw)
	return g
}

// pureGrantKeywords returns the keywords an SA grants when its whole effect is
// a pure, idempotent keyword grant AIMED AT ITS OWN SOURCE: a Pump adding only
// keywords (a non-empty KW$), with no additive stat change (NumAtt/NumDef), no
// SubAbility$ chain, and no colon-parametrised keyword. nil means the
// activation is not such a grant, so it stacks and must never be treated as a
// no-op. This is the engine-side definition of "idempotent keyword grant" that
// decision.Grant is built from.
//
// Every clause here excludes a corpus population the caller cannot reason
// about, measured over .cards/cardsfolder with /usr/bin/grep. Of the 588 raw
// A:AB$ Pump/PumpAll lines carrying a KW$ and no NumAtt/NumDef:
//
//   - 252 are TARGETED (ValidTgts$). The recipient is the target, not the
//     source, and the activation option carries no target at all -- targets
//     arrive at a later KTarget decision -- so neither "the source already has
//     this keyword" nor "an identical grant from this source is pending" says
//     anything about the creature that would actually receive it. Worse, the
//     pending check would refuse to aim a second copy at a DIFFERENT creature.
//   - 66 are PumpAll, granting to a filtered set rather than to Obj.
//   - 66 carry a SubAbility$ whose sub-effect is additive (DBUntap,
//     DBPutCounter, DBDealDamage), so suppressing the activation would forfeit
//     an untap, a counter or damage -- a worse bug than the one being fixed.
//   - 29 carry a colon-parametrised KW$, 20 of them Landwalk:<type>.
//     cards.KeywordHead strips at the colon, so Landwalk:Forest and
//     Landwalk:Island collapse to the same head on both sides of HasKeyword:
//     a creature with islandwalk would decline gaining forestwalk.
//
// Narrowing to the self-aimed Pump was measured to move no acceptance head and
// no botbench golden, so the wider form bought nothing it could be trusted on.
func pureGrantKeywords(ab *cards.SA) []string {
	if ab == nil || ab.API != "Pump" {
		return nil
	}
	if ab.Sub != nil {
		return nil
	}
	switch ab.Params["Defined"] {
	case "Self", "Parent":
	case "":
		if _, targeted := ab.Params["ValidTgts"]; targeted {
			return nil
		}
	default:
		return nil
	}
	if _, att := ab.Params["NumAtt"]; att {
		return nil
	}
	if _, def := ab.Params["NumDef"]; def {
		return nil
	}
	if strings.Contains(ab.Params["KW"], ":") {
		return nil
	}
	return grantKeywords(ab.Params["KW"])
}

// grantKeywords splits a KW$ parameter's keyword list into head-stripped
// words through cards.SplitKeywordList -- the shared Forge parser -- because
// this package cannot re-express the grammar without drifting from it. The
// Forge list separator is ampersand; a comma remains part of a parameter.
func grantKeywords(kw string) []string {
	var out []string
	for _, p := range cards.SplitKeywordList(kw) {
		out = append(out, cards.KeywordHead(p))
	}
	return out
}

// grantPending reports whether an identical keyword grant (the same granted
// keyword set) from the same source permanent id is already on the stack
// unresolved. It walks the shared stack order -- never a map -- and matches
// an ability object whose Source is id and whose own whole effect is the
// same pure keyword grant, so a stack spell, a trigger, or an additive
// activation from id never counts as a duplicate of an idempotent grant.
func (e *Engine) grantPending(id state.ObjID, kw []string) bool {
	for _, oid := range e.G.Zone(state.ZStack, 0) {
		o := e.G.Obj(oid)
		if o == nil || o.Source != id {
			continue
		}
		if grantSetsEqual(pureGrantKeywords(o.Ability), kw) {
			return true
		}
	}
	return false
}

// grantSetsEqual reports whether two granted keyword sets are the same,
// order-independently and case-insensitively.
func grantSetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		found := false
		for _, y := range b {
			if strings.EqualFold(x, y) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// faceHasActivationZone reports whether any activated ability printed on f
// names zone in its ActivationZone$.
func faceHasActivationZone(f *cards.Face, zone string) bool {
	for _, ab := range f.Abilities {
		if ab != nil && ab.Kind == "AB" && strings.TrimSpace(ab.Params["ActivationZone"]) == zone {
			return true
		}
	}
	return false
}
