package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

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
	w := mayPlayIndexWalk{e: e}
	// The walk's card test, shared by the indexed and scanned enumeration
	// arms below (mayplay_index.go): a faceless object and a non-land are
	// both refused, exactly as the nested scan's per-card gate did.
	landKeep := func(o *state.Object) bool { return o.Face() != nil && o.Face().IsLand() }
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
		// Exile and graveyard are public zones keyed by the card's OWNER,
		// and a grant's cards can sit in another seat's slice (Opposition
		// Agent exiles a card from an OPPONENT's searching library, then
		// lets its controller play it), so every seat's slice is walked in
		// deterministic seat order -- the same shape mayPlaySpellIds'
		// walk already is. The Affects match decides ownership claims;
		// walking the slices only enumerates candidates.
		for _, pr := range w.pairs(&ce, mayPlayWalkZones(zones, all), false, landKeep) {
			dup := false
			for _, s := range seen {
				if s.zone == pr.zone && s.id == pr.id {
					dup = true
					break
				}
			}
			if dup {
				continue
			}
			seen = append(seen, offered{pr.zone, pr.id})
			out = append(out, pr.id)
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
	w := mayPlayIndexWalk{e: e}
	// The walk's card test, shared by the indexed and scanned enumeration
	// arms below (mayplay_index.go): a faceless object and a land are both
	// refused, exactly as the nested scan's per-card gate did.
	spellKeep := func(o *state.Object) bool { return o.Face() != nil && !o.Face().IsLand() }
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
		for _, pr := range w.pairs(&ce, mayPlayWalkZones(zones, all), true, spellKeep) {
			consider(pr.zone, pr.id, "")
		}
	}
	return out
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
