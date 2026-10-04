package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// countersAddedThisTurnArgsKnown accepts the complete measured grammar for
// Count$CountersAddedThisTurn. Unlike the ordinary filter parser, this count
// head cannot safely treat an unknown field as a filter that matches nothing:
// CheckSVar distinguishes that evaluated zero from an unresolvable Count$.
// Keep this narrow until a corpus carrier establishes another spelling.
//
// evalCountValidSelf models Forge's `Count$ValidSelf <spec>` head: the count
// of the "Self" card when it matches <spec>, 0 otherwise. Self is the card the
// SVar is evaluated for -- the triggering event's card under
// `CheckOnTriggeredCard$` (a dies trigger's dying creature), else the
// resolving source (a `CheckSVar$` gate). It is a RECOGNISED head even when
// <spec> is outside this build's filter grammar: an unreadable argument then
// fails closed with an evaluated zero instead of falling through to the
// unknown-head verdict, so a `GE1` gate reads a real false rather than an
// unresolvable body a caller might fail open. A missing Self binding is the
// same evaluated zero.
//
// The match runs over the shared filter matcher, so every predicate the
// engine already reads -- including `greatestPower...` and its
// `ControlledBy CardController` referent -- answers here without a second
// implementation. CR 603.10: a card that left the battlefield is judged as it
// last existed there, so the trigger's LKI snapshot is preferred when it
// names Self (a dying creature's controller is reset to its owner by the
// move, and its counters with it).
func evalCountValidSelf(h Host, c *Ctx, arg string) (int32, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return 0, true
	}
	g := h.Game()
	self := c.TriggerCard
	if self == 0 {
		self = c.Source
	}
	if self == 0 {
		return 0, true
	}
	o := g.Obj(self)
	if c.LKI != nil && c.LKI.ID == self {
		o = c.LKI
	}
	if o == nil {
		return 0, true
	}
	sc := c.SpecContext(c.Controller)
	sc.DerivedPTs = append(sc.DerivedPTs, GreatestPowerDerivedPTs(g, arg, h)...)
	if MatchesObjectCtx(g, arg, o, sc) {
		return 1, true
	}
	return 0, true
}

func countersAddedThisTurnArgsKnown(kind, actor, object string) bool {
	if !strings.EqualFold(kind, "Any") && !strings.EqualFold(kind, "P1P1") && !strings.EqualFold(kind, "LORE") {
		return false
	}
	if actor != "You" && actor != "Player" {
		return false
	}
	return countersAddedThisTurnArgsKnownSet.Has(object)
}

// playerSpecBaseKnown reports whether spec's base word (the text before the
// first qualifier separator) is one of the player-spec bases MatchesPlayerSpec
// resolves (You/Opponent/Other/Player/Any, matched case-insensitively). It is
// what keeps a head argument that names an OBJECT spec (Churning Reservoir's
// `Card.YouCtrl+inRealZoneBattlefield/Plus.X`) unresolvable rather than
// laundering it through an empty-player-set read as a legitimate evaluated
// zero — the fail-closed verdict (0,false), so the caller picks its own
// direction for the gate it serves.
func playerSpecBaseKnown(spec string) bool {
	base := spec
	if i := strings.IndexAny(base, ".+,"); i >= 0 {
		base = base[:i]
	}
	for _, known := range []string{"You", "Opponent", "Other", "Player", "Any"} {
		if strings.EqualFold(strings.TrimSpace(base), known) {
			return true
		}
	}
	return false
}

// evalThisTurnEntered parses a ThisTurnEntered_<Dest>[_from_<Origin>]_<Valid>
// tail -- the split Forge's own parser applies (workingCopy[0] = the head, so
// parts[0] here is <Dest>; at most five underscore parts, and a <Valid> tail
// of more than one token is rejoined). An unknown destination zone or an
// empty valid fails closed to zero rather than counting everything.
func evalThisTurnEntered(g *state.Game, c *Ctx, rest string) (int32, bool) {
	return evalThisTurnEnteredAs(g, c, c.Controller, rest)
}

// evalThisTurnEnteredAs is evalThisTurnEntered with the counted member's own
// perspective: the spec's You* qualifiers bind to `you`, not the resolving
// controller. The PlayerCount condition's per-member properties
// (Smuggler's Share's ThisTurnEntered_Battlefield_Land.YouCtrl, meaning "lands
// under THAT opponent's control") need exactly this — the counted member IS
// the filter's You.
func evalThisTurnEnteredAs(g *state.Game, c *Ctx, you state.PlayerID, rest string) (int32, bool) {
	// The optional trailing `$<Property>` sum form (Genesis of the Daleks'
	// `Count$ThisTurnEntered_Graveyard_from_Battlefield_Dalek$CardPower` --
	// "each of your opponents loses life equal to the total power of Daleks
	// that died this turn"): a recognised sum property is split off the
	// <Valid> tail and each matching entry contributes its value through the
	// shared objectProperty reader, the same per-object read the
	// `Count$Valid <spec>$CardPower` aggregate uses. An unrecognised property
	// keeps the WHOLE token as <Valid> -- the fail-closed read the
	// Count$Valid family also takes for a property it does not model, so a
	// `token$DifferentCardNames`-style qualifier can never be mistaken for a
	// sum. Only the count path splits here; parseThisTurnEnteredSpec stays
	// the one grammar PlayerCount validation shares, unchanged.
	prop := ""
	if spec, tail, ok := strings.Cut(rest, "$"); ok && modeledProperty(tail) {
		rest, prop = spec, strings.TrimSpace(tail)
	}
	dest, origin, valid, parsed := parseThisTurnEnteredSpec(rest)
	if !parsed {
		return 0, false
	}
	return countEnteredAs(g, c, you, dest, origin, valid, prop)
}

// parseThisTurnEnteredSpec splits a ThisTurnEntered_<Dest>[_from_<Origin>]_<Valid>
// tail into its parts (Forge's own parser applies the same underscore split:
// parts[0] is <Dest>; at most five underscore parts; a <Valid> tail of more
// than one token is rejoined). ok is false when the shape is not a well-formed
// spec at all -- too few or too many parts, an unknown destination or origin
// zone word, or an EMPTY <Valid>. It is the ONE grammar both
// evalThisTurnEnteredAs (which counts with it) and playerPropertyModelled
// (which validates a PlayerCount$Condition property before ranging a possibly
// empty group) share, so a shape one accepts cannot be rejected by the other
// -- the drift hazard the previous prefix-only check carried, where
// `ThisTurnEntered_` and `ThisTurnEntered_Nonsense` passed the pre-check and
// an empty group laundered them into a legitimate-looking (0, true).
func parseThisTurnEnteredSpec(rest string) (dest state.Zone, origin *state.Zone, valid string, ok bool) {
	parts := strings.Split(strings.TrimSpace(rest), "_")
	if len(parts) < 2 || len(parts) > 5 {
		return 0, nil, "", false
	}
	d, known := zoneWords[parts[0]]
	if !known {
		return 0, nil, "", false
	}
	if len(parts) >= 3 && parts[1] == "from" {
		if len(parts) < 4 {
			return 0, nil, "", false
		}
		o, known := zoneWords[parts[2]]
		if !known {
			return 0, nil, "", false
		}
		v := strings.Join(parts[3:], "_")
		if strings.TrimSpace(v) == "" {
			return 0, nil, "", false
		}
		return d, &o, v, true
	}
	v := strings.Join(parts[1:], "_")
	if strings.TrimSpace(v) == "" {
		return 0, nil, "", false
	}
	return d, nil, v, true
}

// countEntered folds the per-add entry list over one destination zone (and
// optionally one origin zone): the entries whose object matches valid from
// the resolving controller's perspective are counted, or -- when prop names a
// modelled sum property -- their values are summed through objectProperty
// (the Genesis of the Daleks total-power form).
func countEnteredAs(g *state.Game, c *Ctx, you state.PlayerID, dest state.Zone, origin *state.Zone, valid, prop string) (int32, bool) {
	if valid == "" {
		return 0, false
	}
	// The two CheckOnTriggeredCard carriers spell the event player's filter as
	// `<base>.ControlledBy CardController`; peel that referent before the
	// ordinary object matcher and constrain each historical entry by the
	// entering object's controller. Unknown/unbound referents fail closed.
	controlledByCardController := false
	if base, ref, ok := strings.Cut(valid, ".ControlledBy "); ok && ref == "CardController" {
		valid = base
		controlledByCardController = true
	}
	var players []state.PlayerID
	if controlledByCardController {
		var ok bool
		players, ok = controlReferentPlayers(g, c.SpecContext(you), "ControlledBy", "CardController")
		if !ok {
			return 0, true
		}
	}
	var n int32
	for _, e := range g.Entered {
		if e.To != dest {
			continue
		}
		if origin != nil && e.From != *origin {
			continue
		}
		// Evaluate the spec in the entry's DESTINATION zone: an object that
		// entered a non-battlefield zone has already left the battlefield,
		// so the ordinary matcher's `Permanent` base (o.Zone ==
		// ZBattlefield) would reject every such entry. matchesZoneSpecCtx
		// reads a non-battlefield `Permanent` base as a permanent CARD
		// (Forge's Card.isPermanent()), which is what Gravestorm's
		// Count$ThisTurnEntered_Graveyard_from_Battlefield_Permanent needs.
		if !matchesZoneSpecCtx(g, valid, e.Obj, c.SpecContext(you), e.To) {
			continue
		}
		if controlledByCardController {
			o := g.Obj(e.Obj)
			controlled := false
			if o != nil {
				for _, p := range players {
					if o.Controller == p {
						controlled = true
						break
					}
				}
			}
			if !controlled {
				continue
			}
		}
		// The plain count form, and the $<Property> sum form's per-entry
		// contribution (CardPower's printed face plus its +1/+1 counters, the
		// objectProperty read the Count$Valid aggregate shares). A property
		// the split did not recognise never reaches here -- it stayed in
		// <Valid> and failed the match above.
		if prop == "" {
			n++
			continue
		}
		n += objectProperty(g, e.Obj, prop)
	}
	return n, true
}

// objectProperty reads one Count$Valid-spec "$Property" aggregate term over
// a single object: its face value plus the summed P/T counter deltas for
// power/toughness, the mana value for CardManaCost. An unknown property reads
// 0 — the same conservative no-op every unmodelled head here takes.
func objectProperty(g *state.Game, id state.ObjID, prop string) int32 {
	o := g.Obj(id)
	if o == nil || o.Face() == nil {
		return 0
	}
	switch objectPropertyCodes.Code(string(strings.TrimSpace(prop))) {
	case objectPropertyCardPower:
		dp, _ := o.CounterPTTotals()
		return int32(o.Face().Power()) + dp
	case objectPropertyCardToughness:
		_, dt := o.CounterPTTotals()
		return int32(o.Face().Toughness()) + dt
	case objectPropertyCardManaCost:
		return o.Face().ManaValue()
	}
	return 0
}

// modeledProperty reports whether a Count$Valid-spec "$Property" aggregate
// term is one objectProperty can evaluate; an unknown property read 0 — the
// same conservative no-op every unmodelled head here takes — and a gate over
// one must fail open rather than enforce that zero.
func modeledProperty(prop string) bool {
	return modeledPropertySet.Has(strings.TrimSpace(prop))
}

// lifeExtreme answers PlayerCount...$LowestLifeTotal / $HighestLifeTotal:
// the lowest/highest CURRENT life total among the given players. Anything
// else — another property, or an empty group (no extreme exists) — reports
// (0, false): the caller degrades to unresolvable, so a gate over the shape
// fails open rather than enforcing a fake zero.
func lifeExtreme(g *state.Game, players []state.PlayerID, prop string) (int32, bool) {
	prop = strings.TrimSpace(prop)
	if prop != "LowestLifeTotal" && prop != "HighestLifeTotal" {
		return 0, false
	}
	best := int32(0)
	seen := false
	for _, p := range players {
		if int(p) < 0 || int(p) >= len(g.Players) {
			continue
		}
		life := g.Players[p].Life
		if !seen || (prop == "LowestLifeTotal" && life < best) || (prop == "HighestLifeTotal" && life > best) {
			best, seen = life, true
		}
	}
	if !seen {
		return 0, false
	}
	return best, true
}

// opponentGroup returns the living players other than the resolving
// controller — the group PlayerCountOpponents$ and (by the reading
// documented at its dispatch site) PlayerCountRegisteredOpponents$ both
// count over.
// playerGroupCount answers Forge's count property `Amount` on a
// PlayerCount<group>$ head: 1 per member, so the value is the group's size --
// the "one each" bound (SVar:OneEach:PlayerCountOpponents$Amount) and the
// per-player target maximum the TargetsForEachPlayer$ shape reads. Any other
// property fails closed to the ordinary dispatch.
func playerGroupCount(players []state.PlayerID, rest string) (int32, bool) {
	if strings.TrimSpace(rest) != "Amount" {
		return 0, false
	}
	return int32(len(players)), true
}

func opponentGroup(g *state.Game, c *Ctx) []state.PlayerID {
	var opps []state.PlayerID
	for _, p := range g.AliveFrom(0) {
		if p != c.Controller {
			opps = append(opps, p)
		}
	}
	return opps
}

// playerCountDefinedRegistered answers the PlayerCountDefinedRegistered[.Other]$
// properties. The two life-extreme properties route through the shared
// playerCountExtreme (whose LifeLostThisTurn arm is the Host's log-derived
// read — Knight of the Ebon Legion's and Y'shtola's
// HighestLifeLostThisTurn gates); HasPropertyLostLifeThisTurn counts the
// group members who lost any life this turn; and
// HasPropertywasDealtCombatDamageThisTurnBy <spec>[ <op><n>] counts the
// group members who were dealt combat damage this turn by a source matching
// the Forge spec (Lost Monarch of Ifnir's Zombie, Estinien Varlineau's
// Card.Self,Dragon, Blitzball's Creature.Legendary). Anything else reports
// (0, false) — unresolvable, so a gate over it fails per its caller's
// documented direction rather than enforcing a fake zero.
func playerCountDefinedRegistered(h Host, g *state.Game, c *Ctx, group []state.PlayerID, prop, arg string) (int32, bool) {
	prop = strings.TrimSpace(prop)
	// A `HasProperty…` head may carry Forge's /Op count suffix
	// (belbe_corrupted_observer's `PlayerCountOpponents$HasPropertyLostLife
	// ThisTurn/Twice`). Split it here so the property switch below reads the
	// bare name; the suffix applies inside hasPropertyLostLifeCount.
	base, _, _ := strings.Cut(prop, "/")
	base = strings.TrimSpace(base)
	// Deliberately NO lifeExtreme call here: the brief names exactly three
	// resolvable properties on this group, and the life-TOTAL extremes
	// (HighestLifeTotal/LowestLifeTotal) are not among them — they stay
	// (0, false) on DefinedRegistered[.Other]$ even though the sibling
	// Players$/Opponents$ arms resolve them. No corpus carrier reads a life
	// total extreme through this head; if one ever does, widening is a
	// one-line change with its own pin.
	switch playerCountDefinedRegisteredCodes.Code(string(base)) {
	case playerCountDefinedRegisteredExtremeLifeLostThisTurn:
		// The life-lost extremes — Knight of the Ebon Legion's and
		// Y'shtola's gates. playerCountExtreme's LifeLostThisTurn arm is the
		// Host's log-derived read.
		return playerCountExtreme(h, g, c, group, prop, arg)
	case playerCountDefinedRegisteredHasPropertyLostLifeThisTurn:
		// "a player [other than you] lost life this turn" — the shared read
		// every group's HasPropertyLostLifeThisTurn carrier uses. Calling the
		// helper (rather than inlining the count) is what keeps the property
		// from resolving on one group and failing closed on its sibling.
		return hasPropertyLostLifeCount(h, group, prop)
	case playerCountDefinedRegisteredHasPropertywasDealtCombatDam:
		spec, op, threshold, ok := splitPropertyThreshold(arg)
		if !ok {
			return 0, false
		}
		hits := h.CombatDamageToPlayersThisTurn()
		sc := c.SpecContext(c.Controller)
		var n int32
		for _, p := range group {
			var got int32
			for _, hit := range hits {
				if hit.Player != p {
					continue
				}
				// Zone is set to the battlefield: Forge's bare `Permanent`
				// base reads o.Zone == ZBattlefield (effects/filter.go's
				// matchesBase), and the captured source WAS a permanent on
				// the battlefield when it dealt the damage.
				o := &state.Object{ID: hit.Source, Card: hit.Card, FaceIdx: hit.FaceIdx, Controller: hit.Controller, Zone: state.ZBattlefield}
				if MatchesObjectCtx(g, spec, o, sc) {
					got++
				}
			}
			if countOpHolds(op, threshold, got) {
				n++
			}
		}
		return n, true
	}
	// Any other property is NOT resolvable: (0, false), the documented
	// fail-closed verdict. The HighestValid/LowestValid zone-count extremes
	// of playerCountExtreme are deliberately not offered on this group (the
	// brief names exactly the three properties above, and no corpus carrier
	// reaches a zone-count extreme here); only the two LifeLostThisTurn
	// extremes route into playerCountExtreme, above.
	return 0, false
}

// hasPropertyStateBacked answers the narrowly supported player-state
// HasProperty heads on the three ordinary living-player groups. Zone counts
// are evaluated from each member's perspective, so You/YouOwn selectors refer
// to that member rather than the resolving controller.
func hasPropertyStateBacked(h Host, g *state.Game, c *Ctx, group []state.PlayerID, prop, arg string) (int32, bool) {
	base, op, hasOp := strings.Cut(strings.TrimSpace(prop), "/")
	base = strings.TrimSpace(base)
	if strings.TrimSpace(arg) != "" {
		return 0, false
	}

	var qualifies func(state.PlayerID) bool
	switch {
	case base == "HasPropertyIsCorrupted":
		qualifies = func(p state.PlayerID) bool { return playerIsCorrupted(g, p) }
	case base == "HasPropertyisMonarch":
		qualifies = func(p state.PlayerID) bool { return g.IsMonarch(p) }
	case base == "HasPropertywasDealtDamageThisTurn":
		qualifies = func(p state.PlayerID) bool { return h.DamageTakenThisTurn(p) > 0 }
	case base == "HasPropertywasDealtNonCombatDamageThisTurn":
		// Grim Repriser / Whiplash Wordsmith: "an opponent was dealt
		// noncombat damage this turn" -- the Host's per-turn ledger of seats
		// dealt damage outside a combat damage assignment.
		qualifies = h.WasDealtNoncombatDamageThisTurn
	case base == "HasPropertywasDealtNonCombatDamageLastTurn":
		// Command the Stage: "... was dealt noncombat damage last turn".
		qualifies = h.WasDealtNoncombatDamageLastTurn
	case base == "HasPropertywasDealtCombatDamageThisTurn":
		hits := h.CombatDamageToPlayersThisTurn()
		qualifies = func(p state.PlayerID) bool {
			for _, hit := range hits {
				if hit.Player == p {
					return true
				}
			}
			return false
		}
	default:
		zone := state.Zone(0)
		prefix := ""
		switch {
		case strings.HasPrefix(base, "HasPropertyHasCardsInHand_"):
			zone, prefix = state.ZHand, "HasPropertyHasCardsInHand_"
		case strings.HasPrefix(base, "HasPropertyHasCardsInGraveyard_"):
			zone, prefix = state.ZGraveyard, "HasPropertyHasCardsInGraveyard_"
		default:
			return 0, false
		}
		tail := strings.TrimPrefix(base, prefix)
		i := strings.LastIndexByte(tail, '_')
		if i <= 0 || i == len(tail)-1 {
			return 0, false
		}
		spec, cmp := tail[:i], tail[i+1:]
		parsedOp, threshold, ok := parseCountCompare(cmp)
		if !ok || (parsedOp != "GE" && parsedOp != "GT" && parsedOp != "LE") {
			return 0, false
		}
		// An unread card spec fails CLOSED before any member is evaluated:
		// the zone matcher reports only a match boolean, so an unrecognized
		// predicate (Card.NoSuchPredicate) or base (NoSuchBase) would match
		// nothing, read as a fabricated count of zero and return (0, true) --
		// a condition gate would then enforce a zero that no rule stated.
		// unreadZoneSpec validates through the same classifiers the matcher
		// itself walks (UnknownPredicates, the census's shared base/predicate
		// vocabulary), so a spec the matcher would silently zero out here
		// reads unresolvable instead.
		if unreadZoneSpec(spec) {
			return 0, false
		}
		qualifies = func(p state.PlayerID) bool {
			var cards int32
			for _, id := range g.Zone(zone, p) {
				if matchesZoneSpecCtx(g, spec, id, c.SpecContext(p), zone) {
					cards++
				}
			}
			return countOpHolds(parsedOp, threshold, cards)
		}
	}
	n := hasPropertyCount(group, qualifies)
	if hasOp {
		n = applyCountOp(n, op)
	}
	return n, true
}

// unreadZoneSpec reports whether a zone-count HasProperty's card spec is
// unread — malformed, an unknown base word, or carrying a predicate no part
// of the filter recognises. It is the object-free validation mirror of
// matchesZoneSpecCtx: that matcher answers only a match boolean, so the sole
// classifier shared with it is the UnknownPredicates census plus the base
// vocabulary matchesBase dispatches on (its special bases, the CR 205.1
// card-type words, and the creature-subtype words hasTypeCtx reads).
// Anything outside that vocabulary fails closed here, so the count head can
// never turn an unread spec into a fabricated zero.
func unreadZoneSpec(spec string) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return true
	}
	// Comma alternatives are validated per alternative — the base of the
	// WHOLE spec is only the first alternative's (Mysterious Stranger's
	// `Instant,Sorcery` is two valid bases, not one unknown one).
	for alt := range filterAlternatives(spec) {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		base, _, _ := strings.Cut(alt, ".")
		base = strings.TrimSpace(base)
		if neg := strings.TrimPrefix(base, "non"); neg != base {
			base = strings.TrimSpace(neg)
		}
		if base == "" {
			return true
		}
		if !unreadZoneSpecKeys.Has(base) {
			if !cardTypeWords[base] && !CreatureTypeWords(base) {
				return true
			}
		}
	}
	return len(UnknownPredicates(spec)) != 0
}

// hasPropertyLostLifeCount answers PlayerCount*$HasPropertyLostLifeThisTurn
// over a group, honouring Forge's /Op suffix. It is the shared read the
// Players$, Opponents$, RegisteredOpponents$ and DefinedRegistered[.Other]$
// arms all use, so the property can never resolve on one group and fail
// closed on its sibling (the class the fix closes).
func hasPropertyLostLifeCount(h Host, group []state.PlayerID, prop string) (int32, bool) {
	base, op, hasOp := strings.Cut(prop, "/")
	if strings.TrimSpace(base) != "HasPropertyLostLifeThisTurn" {
		return 0, false
	}
	n := hasPropertyCount(group, func(p state.PlayerID) bool { return h.LifeLostThisTurn(p) > 0 })
	if hasOp {
		n = applyCountOp(n, op)
	}
	return n, true
}

// hasPropertyCount counts the group members satisfying pred, in the group's
// own deterministic order.
func hasPropertyCount(group []state.PlayerID, pred func(state.PlayerID) bool) int32 {
	var n int32
	for _, p := range group {
		if pred(p) {
			n++
		}
	}
	return n
}

// playerCountCondition answers Forge's PlayerCount<group>$Condition<OP><RHS>
// <property> family — the per-member threshold count. For each member of the
// group the named property is evaluated FROM THAT MEMBER'S OWN PERSPECTIVE
// (a YouCtrl qualifier in the property's spec names the member), and the
// member is counted when the property satisfies <OP> against <RHS>.
//
// The shared dispatch is what keeps this family from resolving on one group
// and failing closed on its sibling (the class the fix closes): every group
// arm that carries a property dispatch — Players$, Opponents$,
// RegisteredOpponents$ and both DefinedRegistered spellings — routes its
// `Condition...` head here before falling through to playerCountExtreme, so
// the property evaluators below are the ONE grammar for all of them.
//
// <RHS> is either a literal (ConditionGE2 CardsDrawn) or an SVar NAME
// resolved PER MEMBER (Anya, Merciless Angel's `ConditionLTZ LifeTotal`, whose
// Z is `PlayerCountDefinedPlayer.PlayerUID_RelativePlayerUID$StartingLife/
// HalfDown` = that member's half starting life; Game Over's `ConditionLEY
// LifeTotal` is the same shape). A property or RHS this build cannot evaluate
// reports (0, false) — UNRESOLVABLE, never a fake zero — so a gate over it
// degrades per its caller's documented direction (triggers and statics fail
// closed, Num reads zero) rather than enforcing a made-up count.
func playerCountCondition(h Host, g *state.Game, c *Ctx, group []state.PlayerID, rest, arg string) (int32, bool) {
	cond, ok := strings.CutPrefix(strings.TrimSpace(rest), "Condition")
	if !ok || len(cond) < 3 {
		return 0, false
	}
	op := strings.ToUpper(cond[:2])
	switch CmpOpOf(op) {
	case CmpGE, CmpGT, CmpLE, CmpLT, CmpEQ:
	default:
		return 0, false
	}
	rhs := strings.TrimSpace(cond[2:])
	prop := strings.TrimSpace(arg)
	if rhs == "" || prop == "" {
		return 0, false
	}
	// The RHS is a literal when it parses as an integer; else it is an SVar
	// name resolved PER MEMBER (Anya's Z, Game Over's Y). A name with no body
	// anywhere is unreadable — (0, false), never a threshold of 0. The
	// property's GRAMMAR is validated ONCE, BEFORE the group is ranged, and
	// when the group is EMPTY the RHS body is resolved once too, against the
	// resolving context: an empty group would otherwise never reach the
	// per-member checks and the head would report a legitimate-looking
	// (0, true) for a property or RHS this build cannot evaluate — the leak an
	// `...LE0`-shaped gate evaluates as true over nothing. The per-member loop
	// still re-validates (a member can make a modelled property unevaluable,
	// e.g.
	// LifeTotal of a gone seat).
	lit, litErr := strconv.ParseInt(rhs, 10, 32)
	literalOK := litErr == nil
	var rhsBody string
	if !literalOK {
		body, found := "", false
		if c.SVars != nil {
			body, found = c.SVars[rhs]
		}
		if !found {
			if o := g.Obj(c.Source); o != nil && o.Face() != nil {
				body, found = o.Face().SVars[rhs]
			}
		}
		if !found {
			return 0, false
		}
		rhsBody = body
	}
	if !playerPropertyModelled(prop) {
		return 0, false
	}
	// An SVar RHS is resolved PER MEMBER, so an EMPTY group would never
	// evaluate its body at all and a body this build cannot evaluate would be
	// laundered into the empty-group zero alongside a readable one. Resolve it
	// ONCE against the resolving context when there is no member to resolve it
	// with: a body whose head matches nothing is UNRESOLVABLE (0, false), the
	// same verdict a live group's per-member read gives. (A body that resolves
	// against the sentinel is a modelled threshold, so the empty group keeps
	// its honest (0, true); a body that resolves for the sentinel but not for a
	// member on a live group is still caught by the loop's per-member check.)
	if !literalOK && len(group) == 0 {
		sub := *c
		if _, ok := evalCountExprOK(h, &sub, rhsBody, 0); !ok {
			return 0, false
		}
	}
	var n int32
	for _, m := range group {
		v, okv := playerMemberProperty(h, g, c, m, prop)
		if !okv {
			return 0, false
		}
		var threshold int32
		if literalOK {
			threshold = int32(lit)
		} else {
			// Evaluate the body with the MEMBER as the relative player: this
			// is what makes `...RelativePlayerUID$StartingLife/HalfDown`
			// answer the member's own threshold (and a per-member count body
			// its own perspective).
			sub := *c
			sub.Controller = m
			tv, tok := evalCountExprOK(h, &sub, rhsBody, 0)
			if !tok {
				return 0, false
			}
			threshold = tv
		}
		if countOpHolds(op, threshold, v) {
			n++
		}
	}
	return n, true
}

// playerMemberProperty evaluates one property of Forge's player-count
// condition family from the counted member's own perspective: their current
// life total, their per-turn draw / discard / cast census, the count of cards
// that entered a named zone this turn under their control (or owned by them),
// and — through relativePlayerProperty — the relative-player group. Each read
// is shared with the head of the same name elsewhere (the Host per-turn
// predicates, evalThisTurnEnteredAs), so the count family and the standalone
// heads can never drift apart. An unmodelled property reports (0, false) —
// unresolvable, the caller's documented direction, never a fake zero.
func playerMemberProperty(h Host, g *state.Game, c *Ctx, m state.PlayerID, prop string) (int32, bool) {
	switch playerMemberPropertyCodes.Code(string(strings.TrimSpace(prop))) {
	case playerMemberPropertyLifeTotal:
		if int(m) < 0 || int(m) >= len(g.Players) {
			return 0, false
		}
		return g.Players[m].Life, true
	case playerMemberPropertyCardsDrawn:
		return h.CardsDrawnThisTurn(m), true
	case playerMemberPropertyCardsDiscardedThisTurn:
		return h.CardsDiscardedThisTurn(m), true
	case playerMemberPropertySpellsCastThisTurn:
		return int32(h.SpellsCastThisTurnBy(m)), true
	}
	if rest, ok := strings.CutPrefix(strings.TrimSpace(prop), "ThisTurnEntered_"); ok {
		return evalThisTurnEnteredAs(g, c, m, rest)
	}
	return 0, false
}

// playerPropertyModelled reports whether prop names a property the condition
// family's evaluators model at all. playerCountCondition calls it BEFORE
// ranging the group so an empty group cannot launder an unmodelled property
// into a legitimate (0, true) — the per-member checks would never run, and an
// `...LE0`-shaped gate over an unmodelled property would evaluate TRUE over
// nothing. It must stay in lock-step with playerMemberProperty's switch: a
// property modelled there but missed here fails a non-empty group's count too
// (the fail-closed direction, still wrong), and the reverse re-opens the
// empty-group leak this guard closes. The ThisTurnEntered_ branch uses the
// SAME parseThisTurnEnteredSpec the evaluator does, so a structural shape one
// accepts cannot be rejected by the other (a bare `ThisTurnEntered_` prefix
// or an unknown zone word is NOT modelled, however well it prefixes).
func playerPropertyModelled(prop string) bool {
	if playerPropertyModelledSet.Has(strings.TrimSpace(prop)) {
		return true
	}
	if rest, ok := strings.CutPrefix(strings.TrimSpace(prop), "ThisTurnEntered_"); ok {
		_, _, _, parsed := parseThisTurnEnteredSpec(rest)
		return parsed
	}
	return false
}

// relativePlayerProperty answers the
// PlayerCountDefinedPlayer.PlayerUID_RelativePlayerUID$<Property> family: the
// named property of the player the read is FOR (the resolving context's
// Controller — playerCountCondition sets it to the counted member). Only
// StartingLife is modelled (the /Op suffix — Anya's /HalfDown — applies
// through the shared applyCountOp). An unknown property fails closed to
// (0, false), the unresolvable verdict.
func relativePlayerProperty(h Host, prop string) (int32, bool) {
	name, op, hasOp := strings.Cut(strings.TrimSpace(prop), "/")
	if name != "StartingLife" {
		return 0, false
	}
	v := h.StartingLife()
	if hasOp {
		v = applyCountOp(v, op)
	}
	return v, true
}

// playerCountExtreme answers the PlayerCount<group>$<Property> properties
// that are not a life total (lifeExtreme's pair is tried first at the
// dispatch site). Two families, the corpus's measured population over these
// heads:
//
//   - HighestValid/LowestValid <spec> and the zone-scoped spellings
//     (HighestValidGraveyard, LowestValidHand, ...): the highest/lowest,
//     over the group, of the count of objects the member has in the named
//     zone that match the spec, each member counted from their OWN
//     perspective — a YouCtrl qualifier in the spec names the counted
//     member, not the resolving controller (Land Tax's "if an opponent
//     controls more lands than you": SVar Y =
//     PlayerCountOpponents$HighestValid Land.YouCtrl, SVarCompare$ GTX;
//     Defense of the Heart's PlayerCountOpponents$HighestValid
//     Creature.YouCtrl, GE3). An empty zone or an all-zero group is a
//     readable extreme of 0, never unresolvable — a count always has one
//     answer.
//   - HighestLifeLostThisTurn/LowestLifeLostThisTurn: the extreme, over the
//     group, of the Host's log-derived LifeLostThisTurn (Bloodchief
//     Ascension's "if an opponent lost 2 or more life this turn" gate).
//
// An unknown property reports (0, false): unresolvable, so a gate over one
// fails per its caller's documented direction rather than enforcing a fake
// zero. Iterating the given group slice in order (never a map) keeps the
// extreme deterministic; ties match, like lifeExtreme's.
func playerCountExtreme(h Host, g *state.Game, c *Ctx, players []state.PlayerID, prop, spec string) (int32, bool) {
	prop = strings.TrimSpace(prop)
	highest := false
	switch {
	case strings.HasPrefix(prop, "Highest"):
		highest = true
		prop = prop[len("Highest"):]
	case strings.HasPrefix(prop, "Lowest"):
		highest = false
		prop = prop[len("Lowest"):]
	default:
		return 0, false
	}
	if strings.HasPrefix(prop, "Counters.") {
		kind := strings.TrimPrefix(prop, "Counters.")
		if kind != "Poison" {
			return 0, false
		}
		var best int32
		seen := false
		for _, p := range players {
			if int(p) < 0 || int(p) >= len(g.Players) {
				continue
			}
			v := g.Players[p].Counter("POISON")
			if !seen || (highest && v > best) || (!highest && v < best) {
				best, seen = v, true
			}
		}
		if !seen {
			return 0, false
		}
		_, op, hasOp := strings.Cut(prop, "/")
		if hasOp {
			best = applyCountOp(best, op)
		}
		return best, true
	}
	if _, ok := playerScalarProperty(h, g, 0, prop); ok && strings.TrimSpace(spec) == "" {
		// A per-player zone size or tally (Highest/LowestCardsInHand,
		// HighestCardsInGraveyard, HighestCardsDrawn -- fuzz-cov3).
		best, seen := int32(0), false
		for _, p := range players {
			v, ok := playerScalarProperty(h, g, p, prop)
			if !ok {
				continue
			}
			if !seen || (highest && v > best) || (!highest && v < best) {
				best, seen = v, true
			}
		}
		if !seen {
			return 0, true
		}
		return best, true
	}
	if prop == "LifeLostThisTurn" {
		if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
			return 0, false
		}
		best, seen := int32(0), false
		for _, p := range players {
			v := h.LifeLostThisTurn(p)
			if !seen || (highest && v > best) || (!highest && v < best) {
				best, seen = v, true
			}
		}
		if !seen {
			return 0, false
		}
		return best, true
	}
	zone, ok := countZone(prop)
	if !ok || strings.TrimSpace(spec) == "" {
		return 0, false
	}
	best, seen := int32(0), false
	for _, p := range players {
		if int(p) < 0 || int(p) >= len(g.Players) {
			continue
		}
		var n int32
		for _, id := range g.Zone(zone, p) {
			if matchesZoneSpecCtx(g, spec, id, c.SpecContext(p), zone) {
				n++
			}
		}
		if !seen || (highest && n > best) || (!highest && n < best) {
			best, seen = n, true
		}
	}
	if !seen {
		return 0, false
	}
	return best, true
}

var countersAddedThisTurnArgsKnownSet = state.NewNameSet(
	"Creature",
	"Creature.YouCtrl",
	"Permanent.YouCtrl",
	"Card.Self",
	"Card.EffectSource",
)

var modeledPropertySet = state.NewNameSet(
	"CardPower",
	"CardToughness",
	"CardManaCost",
)

var unreadZoneSpecKeys = state.NewNameSet("Any", "Card", "Permanent", "PermanentCard", "Spell", "SpellAbility", "CARDNAME", "Affinity")

var playerPropertyModelledSet = state.NewNameSet(
	"LifeTotal",
	"CardsDrawn",
	"CardsDiscardedThisTurn",
	"SpellsCastThisTurn",
)

type objectPropertyCode uint16

const (
	objectPropertyCardPower objectPropertyCode = iota + 1
	objectPropertyCardToughness
	objectPropertyCardManaCost
)

var objectPropertyCodes = state.NewStrCodes(
	state.StrEntry[objectPropertyCode]{Key: "CardPower", Val: objectPropertyCardPower},
	state.StrEntry[objectPropertyCode]{Key: "CardToughness", Val: objectPropertyCardToughness},
	state.StrEntry[objectPropertyCode]{Key: "CardManaCost", Val: objectPropertyCardManaCost},
)

type playerCountDefinedRegisteredCode uint16

const (
	playerCountDefinedRegisteredExtremeLifeLostThisTurn playerCountDefinedRegisteredCode = iota + 1
	playerCountDefinedRegisteredHasPropertyLostLifeThisTurn
	playerCountDefinedRegisteredHasPropertywasDealtCombatDam
)

var playerCountDefinedRegisteredCodes = state.NewStrCodes(
	state.StrEntry[playerCountDefinedRegisteredCode]{Key: "HighestLifeLostThisTurn", Val: playerCountDefinedRegisteredExtremeLifeLostThisTurn},
	state.StrEntry[playerCountDefinedRegisteredCode]{Key: "LowestLifeLostThisTurn", Val: playerCountDefinedRegisteredExtremeLifeLostThisTurn},
	state.StrEntry[playerCountDefinedRegisteredCode]{Key: "HasPropertyLostLifeThisTurn", Val: playerCountDefinedRegisteredHasPropertyLostLifeThisTurn},
	state.StrEntry[playerCountDefinedRegisteredCode]{Key: "HasPropertywasDealtCombatDamageThisTurnBy", Val: playerCountDefinedRegisteredHasPropertywasDealtCombatDam},
)

type playerMemberPropertyCode uint16

const (
	playerMemberPropertyLifeTotal playerMemberPropertyCode = iota + 1
	playerMemberPropertyCardsDrawn
	playerMemberPropertyCardsDiscardedThisTurn
	playerMemberPropertySpellsCastThisTurn
)

var playerMemberPropertyCodes = state.NewStrCodes(
	state.StrEntry[playerMemberPropertyCode]{Key: "LifeTotal", Val: playerMemberPropertyLifeTotal},
	state.StrEntry[playerMemberPropertyCode]{Key: "CardsDrawn", Val: playerMemberPropertyCardsDrawn},
	state.StrEntry[playerMemberPropertyCode]{Key: "CardsDiscardedThisTurn", Val: playerMemberPropertyCardsDiscardedThisTurn},
	state.StrEntry[playerMemberPropertyCode]{Key: "SpellsCastThisTurn", Val: playerMemberPropertySpellsCastThisTurn},
)
