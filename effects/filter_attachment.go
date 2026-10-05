package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// attachedBy reports whether o is the permanent src is currently attached
// to. Affected$ Creature.EquippedBy on an Equipment's static matches exactly
// the equipped creature: source.AttachedTo is that creature's ID, and the
// candidate must be the object that field names. Only a battlefield source
// is a possible attachment (an Aura/Equipment that is not a permanent cannot
// be "attached" to anything), so a non-battlefield src matches no candidate.
func attachedBy(g *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool {
	s := g.Obj(src)
	return s != nil && s.AttachedTo == o.ID && s.Zone == state.ZBattlefield
}

// canEnchantEquippedBy is the CanEnchantEquippedBy predicate body; see the
// registration above for the spelling's carriers and the referent rule.
func canEnchantEquippedBy(g *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool {
	s := g.Obj(src)
	if s == nil {
		return false
	}
	// The resolving source may be the Face-less ability/trigger wrapper a
	// TriggerPush minted (the placement ask's SpecContext.Source is that
	// wrapper, rules/trigger_queue.go pushTrigger) -- its Source field names
	// the permanent that carries the ability (Ruling T20-b). Unwrap before
	// reading the creature/attach referent, or Mantle's own placement ask
	// would see an unattached Face-less object and admit nothing.
	if s.Face() == nil && s.Source != 0 {
		if real := g.Obj(s.Source); real != nil {
			s = real
		}
	}
	bearer := s
	if !hasType(bearer, "Creature") {
		bearer = g.Obj(s.AttachedTo)
		if bearer == nil {
			return false
		}
	}
	return attachableTo(g, o, bearer)
}

// attachableTo reports whether the (possibly off-battlefield) card o could
// legally be attached to the battlefield permanent bearer: an Aura when the
// bearer satisfies its K:Enchant spec, an Equipment when the bearer is a
// creature (CR 704.5n), anything else never. Evaluated from the candidate's
// own controller seat (the Aura's YouCtrl is the Aura controller's), the same
// seat the CR 704.5m SBA's auraStillMatchesEnchant test uses.
func attachableTo(g *state.Game, o *state.Object, bearer *state.Object) bool {
	if o == nil || bearer == nil || bearer.Zone != state.ZBattlefield {
		return false
	}
	f := o.Face()
	if f == nil {
		return false
	}
	switch {
	case hasType(o, "Aura"):
		param, ok := f.KeywordParam("Enchant")
		if !ok || strings.TrimSpace(param) == "" {
			return true
		}
		spec, _, _ := strings.Cut(param, ":")
		return MatchesSpecFrom(g, strings.TrimSpace(spec), bearer.ID, o.Controller, o.ID)
	case hasType(o, "Equipment"):
		bf := bearer.Face()
		return bf != nil && bf.IsCreature()
	}
	return false
}

// hasAttachmentOfKind reports whether any battlefield permanent whose face
// carries the type word kind names id in its AttachedTo -- the state the
// equip/attach path maintains (rules/attach_test.go pins the SBA that
// detaches on death, so a dead Equipment never counts). The scan walks the
// deterministic AliveFrom/zone slices, never a map, so it is replay-safe as
// a filter predicate. A candidate itself off the battlefield (a graveyard
// card a hidden search spec asks about) is still eligible as the attachment
// TARGET read: AttachedTo only ever names the bearer, so the scan alone
// decides.
func hasAttachmentOfKind(g *state.Game, id state.ObjID, kind string) bool {
	for _, p := range g.AliveFrom(0) {
		for _, sid := range g.Zone(state.ZBattlefield, p) {
			s := g.Obj(sid)
			if s == nil || s.AttachedTo != id {
				continue
			}
			if s.Face() != nil && hasType(s, kind) {
				return true
			}
		}
	}
	return false
}

// modifiedPermanent is the CR 700.9 body for the `modified` CardProperty: a
// permanent is modified if it has one or more counters of any kind on it, is
// equipped, or is enchanted by an Aura its controller also controls. The
// counter loop skips non-positive entries exactly as objectHasKeyword does
// (a counter removed to zero leaves a zero-N record behind); the Equipment
// half reuses hasAttachmentOfKind, whose printed-face type read is the same
// limit the neighbouring `equipped` predicate carries. The Aura half cannot
// reuse it because CR 700.9 adds the controller condition -- the Aura must be
// controlled by the candidate's controller -- so it scans the same
// deterministic battlefield slices itself. The predicate needs no player
// argument; the candidate's own controller is what the Aura clause reads.
func modifiedPermanent(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
	if o == nil {
		return false
	}
	for _, c := range o.Counters {
		if c.N > 0 {
			return true
		}
	}
	if hasAttachmentOfKind(g, o.ID, "Equipment") {
		return true
	}
	for _, p := range g.AliveFrom(0) {
		for _, sid := range g.Zone(state.ZBattlefield, p) {
			s := g.Obj(sid)
			if s == nil || s.AttachedTo != o.ID {
				continue
			}
			if s.Face() != nil && hasType(s, "Aura") && s.Controller == o.Controller {
				return true
			}
		}
	}
	return false
}

// sharesTypeArg splits the space-bearing two-token predicates
// "sharesCardTypeWith <X>", "sharesCreatureTypeWith <X>" and
// "sharesAllCardTypesWithOther <X>", and
// "sharesCardTypeWithOther <X>" and classifies their shared referent. The referent is a resolution-time object list: the
// remembered set (RememberedCard — its first card entry, Braids's "a
// permanent that shares a card type with it" — Remembered, RememberedLKI),
// the triggering card (TriggeredCard/TriggeredCardLKICopy, Heirloom
// Blade's "a creature card that shares a creature type with it"), the
// resolution's targets (Targeted), the source itself (Self), the resolving
// source's commanders (Commander), or the creatures that convoked the
// resolving spell (Convoked). The
// predicate NAME is returned alongside the referent so the dispatch can
// tell the CARD-type and CREATURE-type readings apart. A referent with no
// live binding — and any other <X>, including a nested predicate — is
// unrecognised: the token stays unknown and the spec fails closed, never
// widened.
func sharesTypeArg(p string) (name, arg string, ok bool) {
	name, arg, ok = strings.Cut(p, " ")
	if !ok || (name != "sharesCardTypeWith" && name != "sharesCreatureTypeWith" &&
		name != "sharesCardTypeWithOther" && name != "sharesAllCardTypesWithOther" &&
		name != "SharesColorWithOther") {
		return "", "", false
	}
	arg = strings.TrimSpace(arg)
	if arg == "" || strings.ContainsAny(arg, ".+,!") {
		return "", "", false
	}
	switch sharesTypeArgCodes.Code(string(arg)) {
	case sharesTypeArgRememberedCard, sharesTypeArgRemembered, sharesTypeArgTriggeredCard, sharesTypeArgTargeted:
		return name, arg, true
	case sharesTypeArgImprinted:
		// Forge special-cases only sharesCardTypeWith Imprinted (Semblance
		// Anvil); the other family members resolve Imprinted through
		// getDefinedCards, which this referent answers the same way.
		return name, arg, true
	}
	return "", "", false
}

// sharesNameWithArg recognises the supported two-token name-comparison
// referents. It matches on the shared referent resolver's OWN vocabulary
// table, whose supported referents (Targeted, Remembered, RememberedCard,
// TriggeredCard) each carry a distinct code, so an unintended referent cannot
// leak in through a shared code. Anything else stays unbound and fails closed.
func sharesNameWithArg(p string) (string, bool) {
	name, arg, ok := strings.Cut(p, " ")
	if !ok || wordPredicateSharesCodes.Code(name) != wordPredicateSharesNameWith {
		return "", false
	}
	arg = strings.TrimSpace(arg)
	switch sharesTypeReferentsCodes.Code(arg) {
	case sharesTypeReferentsTargeted, sharesTypeReferentsRemembered,
		sharesTypeReferentsRememberedCard, sharesTypeReferentsTriggeredCard:
		return arg, true
	}
	return "", false
}

func isTargetedCardBase(base string) bool {
	return definedCardPoolCodes.Code(base) == definedCardPoolTargetedCard
}

func sharesNameReferentMatches(g *state.Game, o *state.Object, sc SpecContext, key string) bool {
	for _, target := range sharesTypeReferents(g, sc, key) {
		if !target.IsPlayer {
			if referent := g.Obj(target.Obj); referent != nil && sharesNameWithObject(o, referent, sc) {
				return true
			}
		}
	}
	return false
}

// SpecUsesConvokedAmount reports whether spec reads the `Convoked$Amount`
// count head (or any future `Convoked$<Property>` sibling) -- Forge's spelling
// for "the number of creatures that convoked it" (CR 702.66). It is the
// count-head sibling of SpecUsesConvokedReferent and the ONE classifier the
// provenance gate (rules' faceWantsConvoked) shares, so a face whose SVar or
// ability parameter reads the count always has Object.Convoked captured at
// cast time and a face that does not stays byte-identical. The corpus writes
// the body BOTH with and without the `Count$` prefix
// (`SVar:X:Convoked$Amount`, `SVar:X:Convoked$Amount/Twice`), so the match is
// on the `Convoked$` head-family marker itself, not on a `Count$` prefix the
// bare form omits -- the same tolerance that makes the next `Convoked$<X>`
// head work without a second gate arm. `Defined$ Convoked` and the
// `...With Convoked` referent do NOT contain `Convoked$`, so neither arm this
// replaces is shadowed.
func SpecUsesConvokedAmount(spec string) bool {
	return strings.Contains(spec, "Convoked$")
}

// SpecUsesConvokedReferent reports whether spec is a filter that names the
// Convoked referent anywhere in its comma-alternative list (Everything Comes
// to Dust's `Creature.!sharesCreatureTypeWith Convoked,Artifact,Enchantment`).
// It is the ONE classifier the provenance gate (rules' faceWantsConvoked) and
// the matcher (sharesTypeArg -> sharesTypeReferents) share, so a face whose
// filter reads Convoked always has Object.Convoked captured at cast time and
// a face that does not stays byte-identical. The walk mirrors
// UnknownPredicates' token split (comma alternatives, the `.` base separator,
// the `+` conjunction, a leading `!`), so it recognises exactly the position
// sharesTypeArg recognises.
func SpecUsesConvokedReferent(spec string) bool {
	for alt := range filterAlternatives(spec) {
		_, rest, _ := strings.Cut(strings.TrimSpace(alt), ".")
		for p := range strings.SplitSeq(rest, "+") {
			p = strings.TrimPrefix(p, "!")
			if _, arg, ok := sharesTypeArg(p); ok && arg == "Convoked" {
				return true
			}
		}
	}
	return false
}

// sharesTypeReferents resolves the SHARED referent switch of the
// sharesCardTypeWith/sharesCreatureTypeWith family into the live objects it
// names (empty = an unbound referent; both callers fail closed on that), so
// the two readings can never disagree about which objects <X> names.
// "Commander" (task mordorparams1, Path of Ancestry's "shares a creature
// type with your commander") names the commanders, read live like every
// other referent.
func sharesTypeReferents(g *state.Game, sc SpecContext, ref string) []state.Target {
	var ts []state.Target
	switch sharesTypeReferentsCodes.Code(string(ref)) {
	case sharesTypeReferentsConvoked:
		// CR 702.66's "each creature that convoked it" (Everything Comes to
		// Dust's `Creature.!sharesCreatureTypeWith Convoked`): the creatures
		// the caster tapped to help pay for the resolving spell's cast,
		// carried by the pay-time CastInfo's FlagConvoked IDs into
		// Object.Convoked -- the SAME provenance the Defined$ Convoked
		// selector reads, so the two readings of "convoked" can never
		// disagree. The referent is the resolving source itself (a spell
		// still on the stack); a source with no convoke, a copy, or a
		// creature that has since left play contributes nothing -- fail
		// closed, never widened.
		if o := g.Obj(sc.Source); o != nil {
			for _, id := range o.Convoked {
				if g.Obj(id) != nil {
					ts = append(ts, state.Target{Obj: id})
				}
			}
		}
	case sharesTypeReferentsCommander:
		// Forge's Commander referent: the resolving source's CONTROLLER's
		// commanders (Path of Ancestry's "a creature spell that shares a
		// creature type with your commander" -- the one corpus carrier). The
		// commander list lives on the Players at genesis (wordIsCommander
		// reads the same table). A source with no controller, or a seat with
		// no commanders, binds nothing -- fail closed, never widened.
		if sc.Source == 0 {
			break
		}
		o := g.Obj(sc.Source)
		if o == nil {
			break
		}
		if int(o.Controller) < len(g.Players) {
			for _, c := range g.Players[o.Controller].Commanders {
				ts = append(ts, state.Target{Obj: c})
			}
		}
	case sharesTypeReferentsRememberedCard:
		for _, t := range sc.Remembered {
			if !t.IsPlayer {
				ts = append(ts, t)
				break // the FIRST card entry, per Forge's RememberedCard
			}
		}
	case sharesTypeReferentsRemembered, sharesTypeReferentsRememberedLKI:
		for _, t := range sc.Remembered {
			if !t.IsPlayer {
				ts = append(ts, t)
			}
		}
	case sharesTypeReferentsTriggeredCard, sharesTypeReferentsTriggeredCardLKICopy:
		if sc.TriggerCard != 0 {
			ts = append(ts, state.Target{Obj: sc.TriggerCard})
		}
	case sharesTypeReferentsTargeted:
		ts, _ = sc.TargetBinding()
	case sharesTypeReferentsSelf:
		if sc.Source != 0 {
			ts = append(ts, state.Target{Obj: sc.Source})
		}
	case sharesTypeReferentsImprinted:
		// The SOURCE's live imprint association -- the same pile Defined$
		// Imprinted resolves (imprintPileTargets: an exiled card only while
		// it stays in exile, CR 607.2a). Forge reads the FIRST imprinted card
		// (Iterables.getFirst(source.getImprintedCards())), so only that one
		// is the referent. A source with nothing imprinted binds nothing and
		// the predicate fails closed.
		if pile := imprintPileTargets(g, NewCtxPtr(sc.Source, 0, CtxInit{})); len(pile) > 0 {
			ts = append(ts, pile[0])
		}
	}
	return ts
}

// sharesCardTypeWith reports whether o shares at least one CARD type with
// any object the referent names (Forge Card.sharesCardTypeWith: an
// intersection over the card types — Artifact, Creature, Enchantment, Land,
// Planeswalker, Battle — not supertypes or subtypes). The referent object
// is read live from the game, so a remembered card in the graveyard still
// answers from its own face (CR 603.10's LKI reading applies to
// power/toughness/counters, not types). An unbound referent matches
// nothing — fail closed, never widened.
func sharesCardTypeWith(g *state.Game, o *state.Object, sc SpecContext, ref string) bool {
	// The candidate's ACTUAL card types, enumerated from its printed face
	// through the CR 205.1 vocabulary (cardTypeWords) — the same enumeration
	// sharesAllCardTypesWithOther probes. A FIXED probe list would miss the
	// Instant/Sorcery half of the vocabulary, so two instants "sharing a
	// card type" (Possibility Storm's dig, Cemetery Gatekeeper's trigger)
	// would never intersect. A face enumerating to no card type is
	// malformed; fail closed.
	var oTypes []string
	if f := o.Face(); f != nil {
		for _, x := range f.Types {
			if cardTypeWords[x] {
				oTypes = append(oTypes, x)
			}
		}
	}
	if len(oTypes) == 0 {
		return false
	}
	for _, t := range sharesTypeReferents(g, sc, ref) {
		if t.IsPlayer {
			continue
		}
		r := g.Obj(t.Obj)
		if r == nil {
			continue
		}
		for _, cardType := range oTypes {
			if hasType(r, cardType) {
				return true
			}
		}
	}
	return false
}

// sharesCardTypeWithOther reports whether o shares at least one CARD type
// with an OTHER object the referent names. It is the intersection sibling of
// sharesAllCardTypesWithOther: only the candidate identity exclusion differs
// from sharesCardTypeWith.
func sharesCardTypeWithOther(g *state.Game, o *state.Object, sc SpecContext, ref string) bool {
	var oTypes []string
	if f := o.Face(); f != nil {
		for _, x := range f.Types {
			if cardTypeWords[x] {
				oTypes = append(oTypes, x)
			}
		}
	}
	if len(oTypes) == 0 {
		return false
	}
	for _, t := range sharesTypeReferents(g, sc, ref) {
		if t.IsPlayer {
			continue
		}
		r := g.Obj(t.Obj)
		if r == nil || r.ID == o.ID {
			continue
		}
		for _, cardType := range oTypes {
			if hasType(r, cardType) {
				return true
			}
		}
	}
	return false
}

// sharesColorWithOther reports whether o shares at least one COLOUR with an
// OTHER object the referent names (Forge Card.sharesColorWith over the
// referent minus the candidate itself): Sphinx's Tutelage's and Grindstone's
// repeat gate `Remembered$Valid Card[.nonLand+]SharesColorWithOther
// Remembered` counts the milled cards that share a colour with another
// milled card, so with two milled cards it reaches 2 exactly when they share
// a colour. Colourless shares nothing. Colours read through colorMaskCtx
// (layer-5 derived on the battlefield, the printed face elsewhere -- a
// milled card answers from its face). An unbound referent matches nothing.
func sharesColorWithOther(g *state.Game, o *state.Object, sc *SpecContext, ref string) bool {
	mine := colorMaskCtx(o, sc)
	if mine == 0 {
		return false
	}
	for _, t := range sharesTypeReferents(g, *sc, ref) {
		if t.IsPlayer {
			continue
		}
		r := g.Obj(t.Obj)
		if r == nil || r.ID == o.ID {
			continue
		}
		if colorMaskCtx(r, sc)&mine != 0 {
			return true
		}
	}
	return false
}

// sharesAllCardTypesWithOther reports whether o shares EVERY one of its CARD
// types with some OTHER object the referent names (Forge
// Card.sharesAllCardTypesWithOther — Demonic Covenant's "If two cards that
// share all their card types were milled this way, sacrifice ...", whose
// SVar is `Remembered$Valid Card.sharesAllCardTypesWithOther Remembered`):
// every card type of the candidate is also a card type of the other object,
// which is Forge's allMatch-over-the-candidate's-card-types read. With
// exactly two remembered cards (this carrier's NumCards$) the count reaches
// 2 only when the two cards' card-type sets are identical, which is the
// oracle's "share all their card types".
//
// "Other" is Forge's own suffix: the matched object must be a different
// object than the candidate (by identity — two milled copies of the same
// card name are different objects and do share all their card types). The
// referent objects are read live from the game, so a remembered card in the
// graveyard still answers from its own face, exactly like
// sharesCardTypeWith's read. An unbound referent matches nothing — fail
// closed, never widened.
func sharesAllCardTypesWithOther(g *state.Game, o *state.Object, sc SpecContext, ref string) bool {
	// The candidate's ACTUAL card types, enumerated from its printed face and
	// filtered by the CR 205.1 card-type vocabulary (cardTypeWords — the same
	// set Count$Valid...$CardTypes counts for Tarmogoyf). Probing a FIXED
	// list instead would trivially pass any candidate whose card types are
	// all outside the list (an Instant or a Sorcery would "share all its
	// card types" with anything — the false positive the GE2 gate exists to
	// prevent), so the probe is the enumeration, not a list. A face that
	// enumerates to no card type at all is malformed; fail closed rather
	// than trivially matching.
	var oTypes []string
	if f := o.Face(); f != nil {
		for _, x := range f.Types {
			if cardTypeWords[x] {
				oTypes = append(oTypes, x)
			}
		}
	}
	if len(oTypes) == 0 {
		return false
	}
	for _, t := range sharesTypeReferents(g, sc, ref) {
		if t.IsPlayer {
			continue
		}
		r := g.Obj(t.Obj)
		if r == nil || r.ID == o.ID {
			continue
		}
		all := true
		for _, cardType := range oTypes {
			if !hasType(r, cardType) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// faceIsTheChosenType reports whether the object's face carries the
// "CARDNAME is the chosen type in addition to its other types" static
// (Titan of Littjara, Adaptive Automaton, Metallic Mimic, Roaming Throne,
// Multiversal Passage, Thran Portal): a Continuous static whose Affected$
// names the host itself and whose AddType$/AddTypes$ value is the
// ChosenType indirection. For such a referent the recorded "as this
// enters" choice counts among its creature types in a
// sharesCreatureTypeWith read: the grant is materialised only inside the
// layer walk (rules' resolveChosenTypes against the host's recorded
// choice), which a count or target filter evaluation never runs, so the
// probe reads the static's own shape instead. A referent with no recorded
// choice grants nothing — the same fail-closed direction the layer walk
// takes.
func faceIsTheChosenType(r *state.Object) bool {
	if r == nil || r.Face() == nil || r.ChosenType == "" {
		return false
	}
	for _, st := range r.Face().Statics {
		if st.Mode != "Continuous" || !strings.Contains(st.ParamStr(cards.PKAffected), "Self") {
			continue
		}
		for v := range strings.SplitSeq(st.ParamStr(cards.PKAddType), ",") {
			if strings.TrimSpace(v) == "ChosenType" {
				return true
			}
		}
		for v := range strings.SplitSeq(st.ParamStr(cards.PKAddTypes), ",") {
			if strings.TrimSpace(v) == "ChosenType" {
				return true
			}
		}
	}
	return false
}

// sharesCreatureTypeWith reports whether o shares at least one CREATURE
// subtype with any object the referent names (Forge
// Card.sharesCreatureTypeWith: an intersection over the creature subtypes —
// Heirloom Blade's "a creature card that shares a creature type with it").
// The candidate's subtypes are read context-aware (hasTypeCtx: layer grants
// and Changeling reach it); the referent's own subtypes are read from its
// live face exactly like sharesCardTypeWith's card-type read (hasType,
// which handles Changeling on the referent's side too) — plus the chosen
// type when the face's own "is the chosen type" static grants it
// (faceIsTheChosenType: Titan of Littjara's Illusion-Bear read below).
// An unbound referent matches nothing — fail closed, never widened.
func sharesCreatureTypeWith(g *state.Game, o *state.Object, sc SpecContext, ref string) bool {
	for _, t := range sharesTypeReferents(g, sc, ref) {
		if t.IsPlayer {
			continue
		}
		r := g.Obj(t.Obj)
		if r == nil || r.Face() == nil {
			continue
		}
		for _, word := range r.Face().Types {
			if !CreatureTypeWords(word) {
				continue
			}
			if hasTypeCtx(o, word, sc) && hasType(r, word) {
				return true
			}
		}
		// The chosen-type grant: the recorded choice is one of the
		// referent's creature types exactly as the layer walk materialises
		// it. The printed-face hasType above cannot see the grant (it lives
		// in rules' layer-4 emission, not on Face().Types), so this read is
		// the grant's own — and CreatureTypeWords keeps a non-creature
		// recorded choice (a ChooseType over another category) out.
		if w := r.ChosenType; w != "" && CreatureTypeWords(w) && faceIsTheChosenType(r) && hasTypeCtx(o, w, sc) {
			return true
		}
	}
	return false
}

// objectIsAttached reports whether o is attached to a live object: its own
// AttachedTo names an object still reachable in the game. It is the shared
// body of the bare `Attached` predicate and the literal `AttachedTo <X>`
// matcher, so the two readings of "is attached" can never disagree. A bearer
// that has left (AttachedTo stale, g.Obj nil) is not an attachment.
func objectIsAttached(g *state.Game, o *state.Object) bool {
	if g == nil || o == nil || o.AttachedTo == 0 {
		return false
	}
	return g.Obj(o.AttachedTo) != nil
}

// attachedToReferent classifies the resolution-time referent forms beyond a
// literal type/class argument that `AttachedTo <ref>` accepts: the resolving
// ability's own targets (Targeted, ParentTarget -- effects' Ctx.Targets is
// the resolution's list, and ParentTarget is the same list here), the card a
// trigger captured (TriggeredCardLKICopy, read from the same Remembered set
// effects' Defined$ TriggeredCardLKICopy resolves), and the attacking
// creature an Attacks trigger captured (TriggeredAttackerLKICopy, the
// TriggeredAttacker family's Remembered read -- Arna, Skycaptain's real
// source filter). It returns the canonical spelling and true; any other
// token (including a nested predicate or an unrecognised referent) is
// rejected so the classifier and UnknownPredicates stay in agreement.
func attachedToReferent(ref string) (string, bool) {
	switch attachedToReferentCodes.Code(string(ref)) {
	case attachedToReferentTargeted:
		return ref, true
	}
	return "", false
}

// attachedToPlayerReferent classifies the PLAYER-referent forms `AttachedTo
// <ref>` accepts -- a Curse attached to a seat rather than a permanent. `You`
// is the whole family today (Lynde, Cheerful Tormentor's `Choices$
// Curse.AttachedTo You`, Witchbane Orb's `ValidCards$ Curse.AttachedTo You`):
// the candidate must be attached to the spec's you (SpecContext.You, the
// resolving controller). It is deliberately separate from attachedToReferent
// -- a player is not an object, and a player attachment is not represented by
// state.Object.AttachedTo (its ObjID), but by the AttachedPlayer/
// HasAttachedPlayer pair. Its companion resolution is
// attachedToReferentPlayer. Any other token (an object referent, a nested
// predicate, an unrecognised role) is rejected so the classifier and
// UnknownPredicates stay in agreement.
func attachedToPlayerReferent(ref string) (string, bool) {
	if ref == "You" {
		return ref, true
	}
	return "", false
}

// attachedToReferentPlayer resolves an attachedToPlayerReferent spelling to
// the live seat it names in this SpecContext, and reports whether the
// referent is BOUND. `You` binds SpecContext.You; an absent binding (a
// SpecContext whose You is not a real seat of the game) returns (0, false) so
// both the matcher and contextPredicateBound fail closed -- never an invented
// seat and never an always-true negation.
func attachedToReferentPlayer(g *state.Game, sc SpecContext, ref string) (state.PlayerID, bool) {
	if g == nil {
		return 0, false
	}
	if ref == "You" && int(sc.You) >= 0 && int(sc.You) < len(g.Players) {
		return sc.You, true
	}
	return 0, false
}

// attachedToReferentObjects resolves an attachedToReferent spelling to the
// live object ids it names in this SpecContext, and reports whether the
// referent is BOUND. An absent binding (Targeted outside a resolution, or a
// trigger referent with no remembered object), or a stale object ID, returns
// (nil, false) so both the matcher and contextPredicateBound fail closed -- never an invented
// bearer and never an always-true negation. Player-only entries are dropped
// because state.Object.AttachedTo can only name an object; the PLAYER link
// is the separate AttachedPlayer/HasAttachedPlayer pair, read by
// attachedToReferentPlayer for the `AttachedTo You` spelling.
//
// Cardinality: the supported binding is EXACTLY ONE object. A plural binding
// (a resolution with several object targets, or a trigger that remembered
// several objects) is ambiguous -- the grammar of `AttachedTo <ref>` names
// THE referent's bearer, and with two or more bearers named the predicate
// cannot say which one the candidate must be attached to without inventing a
// plural-match rule the corpus spelling does not define -- so a plural
// binding returns (nil, false) too: the predicate is unbound and both the
// positive and the leading-'!' negated spelling fail closed (the measured
// carriers -- Strip Bare, Hubris, Fiery Annihilation's TargetMax$ 1, Arna,
// Rhuk -- all bind singly; Silence the Believers' Strive is the one plural-
// capable carrier and fails closed at >= 2 targets rather than guessing).
func attachedToReferentObjects(g *state.Game, sc SpecContext, ref string) ([]state.ObjID, bool) {
	if g == nil {
		return nil, false
	}
	switch attachedToReferentObjectsCodes.Code(string(ref)) {
	case attachedToReferentObjectsTargeted:
		// Resolution-only, exactly like the Targeted*/NotDefinedTargeted
		// families: SpecContext has ResolutionTargets set only by a
		// resolving context (effects.Ctx.SpecContext or a legality recheck),
		// never while a target offer is being built.
		// The one exception is a SUB-ability's own offer, built while the
		// parent resolves: its ParentTarget is the parent's already-chosen
		// target (SpecContext.TargetBinding).
		bound, ok := sc.TargetBinding()
		if !ok {
			return nil, false
		}
		out := make([]state.ObjID, 0, len(bound))
		for _, t := range bound {
			if !t.IsPlayer && t.Obj != 0 {
				if g.Obj(t.Obj) == nil {
					return nil, false
				}
				out = append(out, t.Obj)
			}
		}
		// Ambiguous plural binding: unbound, never an any-of guess (see the
		// cardinality note on the function). An empty list stays bound -- the
		// resolution exists and named no object, so the positive match is a
		// real false and the negation a real true.
		if len(out) > 1 {
			return nil, false
		}
		return out, true
	case attachedToReferentObjectsTriggeredCardLKICopy:
		// The Remembered set the trigger captured, the same read the
		// Defined$ selector of the same name makes (effects/context.go). No
		// remembered OBJECT means the trigger bound nothing: fail closed.
		out := make([]state.ObjID, 0, len(sc.Remembered))
		for _, t := range sc.Remembered {
			if !t.IsPlayer && t.Obj != 0 {
				if g.Obj(t.Obj) == nil {
					return nil, false
				}
				out = append(out, t.Obj)
			}
		}
		// No remembered object: the trigger bound nothing. Two or more:
		// ambiguous plural binding, unbound like the absent one (see the
		// cardinality note on the function).
		if len(out) != 1 {
			return nil, false
		}
		return out, true
	}
	return nil, false
}

// attachedToArg splits the space-bearing two-token predicate "AttachedTo <X>"
// into its argument and reports whether the argument is (a) a single literal
// type or object class the base grammar (matchesBase) can answer from the
// object in hand, or (b) the dotted two-token form "AttachedTo <class>.<qual>"
// whose qualifier is evaluated against the attached object itself (the
// counterpart of the adjacent enchantedByArg's <Type>.<qual>). The dotted
// allowlist is exactly YouCtrl — the only measured qualifier (Umbra Mystic's
// "Aura.AttachedTo Permanent.YouCtrl" grant; 6 occurrences / 5 files). <class>
// keeps the bare form's object-class / type-word validation, so
// a player is neither an object class nor a type word in this OBJECT-side
// grammar (the player-side EnchantedBy is handled separately). It returns false for any token that
// is not one of these shapes: a different predicate name, no space, an empty
// argument, an argument carrying a nested predicate ('+'/','), a dotted
// qualifier outside the allowlist, a referent needing resolution-time context
// such as "AttachedTo Targeted", or a word that is neither an object class nor
// a corpus type word. Consuming tokens that are not these shapes keeps the
// matcher and UnknownPredicates agreeing, because a token either becomes a
// wordAttachedTo classifier here or it does not -- there is no middle where
// one side sees it and the other does not.
func attachedToArg(p string) (string, bool) {
	name, arg, has := strings.Cut(p, " ")
	if !has || name != "AttachedTo" {
		return "", false
	}
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", false
	}
	// The dotted two-token form "<class>.<qual>": the qualifier rides the
	// object the candidate is attached to (wordAttachedTo's matcher case
	// evaluates it there), so it is validated here once for both the matcher
	// and the recognition path.
	if class, qual, ok := strings.Cut(arg, "."); ok {
		if qual != "YouCtrl" {
			return "", false
		}
		switch attachedToClassCodes.Code(string(class)) {
		case attachedToClassCard:
		default:
			if !predicateTypeWords[class] {
				return "", false
			}
		}
		return class + "." + qual, true
	}
	if strings.ContainsAny(arg, "+,") {
		return "", false
	}
	// The resolution-time referent forms (Targeted/ParentTarget/triggered
	// card/attacker): recognised here, resolved against SpecContext by
	// wordMatches, and refused beneath '!' when their binding is absent
	// (contextPredicateBound). A referent is not a type word, so it is
	// checked before the literal type-word fallback.
	if _, ok := attachedToReferent(arg); ok {
		return arg, true
	}
	switch attachedToArgCodes.Code(string(arg)) {
	case attachedToArgCard:
		return arg, true
	}
	// The player-referent family `AttachedTo You` (Witchbane Orb, Lynde): a
	// Curse attached to the PLAYER. state.Object.AttachedTo is an ObjID and a
	// player is not an object, but the engine now carries the player link on
	// state.Object.AttachedPlayer/HasAttachedPlayer (written only by
	// events.Attach's player branch), so this is a real, modelable read rather
	// than the recognised-and-inert unknown it once was. It is checked before
	// the literal type-word fallback so `You` cannot be mistaken for the
	// `Types:Legendary Planeswalker You` spelling (a different position
	// entirely).
	if _, ok := attachedToPlayerReferent(arg); ok {
		return arg, true
	}
	if predicateTypeWords[arg] {
		return arg, true
	}
	return "", false
}

// enchantedByArg splits the space-bearing two-token predicate
// "EnchantedBy <Type>.<qual>" into its argument halves and validates both.
// <Type> is a literal object class or corpus type word the base grammar
// answers from the attached object in hand (every carrier names Aura), and
// <qual> is one of the possession/otherness map predicates the qualifier is
// evaluated against THE ATTACHED OBJECT -- Other (not the resolving source:
// Daybreak Coronet's "another Aura attached to it", and Face of Divinity's
// static excluding Face itself) and YouCtrl (controlled by the spec's you:
// the Killian / Eriette / Archon / Kaima / Dawn Evangel family). A bare
// "EnchantedBy" token never reaches this parser -- the predicates map's
// attachedBy ("the permanent the resolving source is attached to") is
// consulted first on both the matcher and the recognition path and keeps
// its own meaning. Any other shape -- a resolution-time referent
// (EnchantedBy Aura.Targeted), a nested predicate (EnchantedBy
// Aura.Permanent.YouCtrl), a qualifier outside the allowlist, an
// unrecognised type word, or an absent argument -- stays unrecognised:
// the token fails closed and UnknownPredicates keeps reporting it.
func enchantedByArg(p string) (string, bool) {
	name, arg, has := strings.Cut(p, " ")
	if !has || name != "EnchantedBy" {
		return "", false
	}
	arg = strings.TrimSpace(arg)
	if arg == "" || strings.ContainsAny(arg, "+,!") {
		return "", false
	}
	typ, qual, hasDot := strings.Cut(arg, ".")
	if !hasDot || typ == "" || qual == "" || strings.Contains(qual, ".") {
		return "", false
	}
	switch enchantedByTypeCodes.Code(string(typ)) {
	case enchantedByTypeCard:
	default:
		if !predicateTypeWords[typ] {
			return "", false
		}
	}
	switch enchantedByQualCodes.Code(string(qual)) {
	case enchantedByQualOther:
	default:
		return "", false
	}
	return typ + "." + qual, true
}

// hasAttachmentMatching reports whether any battlefield permanent attached
// to id (some permanent's AttachedTo names id) satisfies the base typ and
// the qualifier fn evaluated against THAT ATTACHED OBJECT. The scan is the
// hasAttachmentOfKind walk (deterministic AliveFrom/zone slices, never a
// map), so it is replay-safe as a filter predicate; an attachment list is
// not stored on the bearer, so the scan is the only source.
func hasAttachmentMatching(g *state.Game, id state.ObjID, sc SpecContext, typ string, fn predFn) bool {
	for _, p := range g.AliveFrom(0) {
		for _, sid := range g.Zone(state.ZBattlefield, p) {
			a := g.Obj(sid)
			if a == nil || a.AttachedTo != id {
				continue
			}
			if !matchesBase(g, typ, a, sc) {
				continue
			}
			if fn(g, a, sc.You, sc.Source) {
				return true
			}
		}
	}
	return false
}

type sharesTypeArgCode uint16

const (
	sharesTypeArgRememberedCard sharesTypeArgCode = iota + 1
	sharesTypeArgImprinted
	sharesTypeArgRemembered
	sharesTypeArgTriggeredCard
	sharesTypeArgTargeted
)

var sharesTypeArgCodes = state.NewStrCodes(
	state.StrEntry[sharesTypeArgCode]{Key: "RememberedCard", Val: sharesTypeArgRememberedCard},
	state.StrEntry[sharesTypeArgCode]{Key: "Remembered", Val: sharesTypeArgRemembered},
	state.StrEntry[sharesTypeArgCode]{Key: "RememberedLKI", Val: sharesTypeArgRememberedCard},
	state.StrEntry[sharesTypeArgCode]{Key: "TriggeredCard", Val: sharesTypeArgTriggeredCard},
	state.StrEntry[sharesTypeArgCode]{Key: "TriggeredCardLKICopy", Val: sharesTypeArgRememberedCard},
	state.StrEntry[sharesTypeArgCode]{Key: "Targeted", Val: sharesTypeArgTargeted},
	state.StrEntry[sharesTypeArgCode]{Key: "Self", Val: sharesTypeArgRememberedCard},
	state.StrEntry[sharesTypeArgCode]{Key: "Commander", Val: sharesTypeArgRememberedCard},
	state.StrEntry[sharesTypeArgCode]{Key: "Convoked", Val: sharesTypeArgRememberedCard},
	state.StrEntry[sharesTypeArgCode]{Key: "Imprinted", Val: sharesTypeArgImprinted},
)

type sharesTypeReferentsCode uint16

const (
	sharesTypeReferentsConvoked sharesTypeReferentsCode = iota + 1
	sharesTypeReferentsCommander
	sharesTypeReferentsRememberedCard
	sharesTypeReferentsRemembered
	sharesTypeReferentsRememberedLKI
	sharesTypeReferentsTriggeredCard
	sharesTypeReferentsTriggeredCardLKICopy
	sharesTypeReferentsTargeted
	sharesTypeReferentsSelf
	sharesTypeReferentsImprinted
)

var sharesTypeReferentsCodes = state.NewStrCodes(
	state.StrEntry[sharesTypeReferentsCode]{Key: "Convoked", Val: sharesTypeReferentsConvoked},
	state.StrEntry[sharesTypeReferentsCode]{Key: "Commander", Val: sharesTypeReferentsCommander},
	state.StrEntry[sharesTypeReferentsCode]{Key: "RememberedCard", Val: sharesTypeReferentsRememberedCard},
	state.StrEntry[sharesTypeReferentsCode]{Key: "Remembered", Val: sharesTypeReferentsRemembered},
	state.StrEntry[sharesTypeReferentsCode]{Key: "RememberedLKI", Val: sharesTypeReferentsRememberedLKI},
	state.StrEntry[sharesTypeReferentsCode]{Key: "TriggeredCard", Val: sharesTypeReferentsTriggeredCard},
	state.StrEntry[sharesTypeReferentsCode]{Key: "TriggeredCardLKICopy", Val: sharesTypeReferentsTriggeredCardLKICopy},
	state.StrEntry[sharesTypeReferentsCode]{Key: "Targeted", Val: sharesTypeReferentsTargeted},
	state.StrEntry[sharesTypeReferentsCode]{Key: "Self", Val: sharesTypeReferentsSelf},
	state.StrEntry[sharesTypeReferentsCode]{Key: "Imprinted", Val: sharesTypeReferentsImprinted},
)

type attachedToReferentCode uint16

const (
	attachedToReferentTargeted attachedToReferentCode = iota + 1
)

var attachedToReferentCodes = state.NewStrCodes(
	state.StrEntry[attachedToReferentCode]{Key: "Targeted", Val: attachedToReferentTargeted},
	state.StrEntry[attachedToReferentCode]{Key: "ParentTarget", Val: attachedToReferentTargeted},
	state.StrEntry[attachedToReferentCode]{Key: "TriggeredCardLKICopy", Val: attachedToReferentTargeted},
	state.StrEntry[attachedToReferentCode]{Key: "TriggeredAttackerLKICopy", Val: attachedToReferentTargeted},
)

type attachedToReferentObjectsCode uint16

const (
	attachedToReferentObjectsTargeted attachedToReferentObjectsCode = iota + 1
	attachedToReferentObjectsTriggeredCardLKICopy
)

var attachedToReferentObjectsCodes = state.NewStrCodes(
	state.StrEntry[attachedToReferentObjectsCode]{Key: "Targeted", Val: attachedToReferentObjectsTargeted},
	state.StrEntry[attachedToReferentObjectsCode]{Key: "ParentTarget", Val: attachedToReferentObjectsTargeted},
	state.StrEntry[attachedToReferentObjectsCode]{Key: "TriggeredCardLKICopy", Val: attachedToReferentObjectsTriggeredCardLKICopy},
	state.StrEntry[attachedToReferentObjectsCode]{Key: "TriggeredAttackerLKICopy", Val: attachedToReferentObjectsTriggeredCardLKICopy},
)

type attachedToClassCode uint16

const (
	attachedToClassCard attachedToClassCode = iota + 1
)

var attachedToClassCodes = state.NewStrCodes(
	state.StrEntry[attachedToClassCode]{Key: "Card", Val: attachedToClassCard},
	state.StrEntry[attachedToClassCode]{Key: "Permanent", Val: attachedToClassCard},
	state.StrEntry[attachedToClassCode]{Key: "Spell", Val: attachedToClassCard},
)

type attachedToArgCode uint16

const (
	attachedToArgCard attachedToArgCode = iota + 1
)

var attachedToArgCodes = state.NewStrCodes(
	state.StrEntry[attachedToArgCode]{Key: "Card", Val: attachedToArgCard},
	state.StrEntry[attachedToArgCode]{Key: "Permanent", Val: attachedToArgCard},
	state.StrEntry[attachedToArgCode]{Key: "Spell", Val: attachedToArgCard},
)

type enchantedByTypeCode uint16

const (
	enchantedByTypeCard enchantedByTypeCode = iota + 1
)

var enchantedByTypeCodes = state.NewStrCodes(
	state.StrEntry[enchantedByTypeCode]{Key: "Card", Val: enchantedByTypeCard},
	state.StrEntry[enchantedByTypeCode]{Key: "Permanent", Val: enchantedByTypeCard},
	state.StrEntry[enchantedByTypeCode]{Key: "Spell", Val: enchantedByTypeCard},
)

type enchantedByQualCode uint16

const (
	enchantedByQualOther enchantedByQualCode = iota + 1
)

var enchantedByQualCodes = state.NewStrCodes(
	state.StrEntry[enchantedByQualCode]{Key: "Other", Val: enchantedByQualOther},
	state.StrEntry[enchantedByQualCode]{Key: "YouCtrl", Val: enchantedByQualOther},
)
