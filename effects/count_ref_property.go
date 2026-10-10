package effects

import (
	"math/bits"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// evalRefProperty resolves one "<Ref>$<Property>[...][/Op]" count body over
// the objects a target reference names. Refs: Targeted/ParentTarget/
// ThisTargetedCard name the resolving ability's chosen targets, AllTargeted
// the whole root/sub-ability chain union (Ctx.AllTargets when the cost site
// bound it, Ctx.Targets otherwise; see refTargets);
// TriggeredCard (and its LKI spellings) and TriggeredAttacker name the
// objects the firing trigger remembered; Remembered is the plain form. A
// property this build does not model (or a body with no $ at all -- every
// other head in this evaluator) returns false, and the caller degrades to
// zero exactly as before this evaluator existed.
//
// The per-object answers mirror evalCountBody's own source-anchored heads:
// CardPower/CardToughness read the face plus marked P1P1 counters
// (battlefield layer output for a battlefield object; a graveyard object's
// face), CardManaCost the face's converted cost, CardCounters.<KIND> one
// counter kind, Valid the count of referenced objects matching a card spec
// (unknown predicates fail closed inside the matcher, so an unreadable
// filter counts zero, never everything). Several references sum -- Forge's
// Count$ reads the same way -- and the /Op suffix applies through
// applyCountOp like every other head. depth is the evaluator's own
// recursion depth, passed through so the suffix's SVar-named operand
// (Doran, Besieged by Time's TriggeredAttacker$CardPower/Minus.Z1) resolves
// the named body against the same face's SVar table the SVar$ head reads,
// instead of silently dropping it (applyCountOp's numeric-only read parsed
// no number and left the base value standing, which turned "+X/+X where X
// is the difference between power and toughness" into "+power/+power").
func evalRefProperty(h Host, c *Ctx, expr string, depth int) (int32, bool) {
	ref, prop, found := strings.Cut(expr, "$")
	if !found || h == nil {
		return 0, false
	}
	prop, op, hasOp := strings.Cut(prop, "/")
	prop = strings.TrimSpace(prop)
	var ts []state.Target
	if ref == "TriggerObjectsCards" {
		ts = c.Captured
	} else {
		var ok bool
		ts, ok = refTargets(h, c, ref)
		if !ok {
			return 0, false
		}
	}
	if (ref == "TargetedObjects" || ref == "TargetedObjectsDistinct") && prop == "Amount" {
		n := int32(len(ts))
		if hasOp {
			n = applyCountOpOperand(h, c, n, op, depth)
		}
		return n, true
	}
	g := h.Game()
	var n int32
	triggerObjectTypes := map[string]bool{}
	// The Colors distinct-set property over a reference's objects (task
	// levelb-static-count-attachments): `ExiledWith$Colors` (Sunbird Effigy's
	// characteristic-defining P/T) counts the DISTINCT colours among the
	// referenced cards, the same distinct-set read
	// evalCountBody's Count$Valid <spec>$Colors makes over zone matches
	// (Shimmercreep's Vivid). The fold is a bitmask read only through
	// OnesCount8, so no order ever reaches an event or a view; the CDA
	// overwrite arm ColorMaskOf already routes, so an exiled card whose
	// printed face carries a SetColor$ CDA contributes that colour.
	var colorsSeen ColorMask
	// The Different* distinct-set property family over a reference's objects
	// (task diffcount1): `Remembered$DifferentCardManaCost` (Azor's Gateway,
	// Sanctum of the Sun settling X, Atemsis All-Seeing). The set is read
	// through len, so no map ordering ever reaches an event or a view.
	diffKind := differentPropertyKindOf(prop)
	var seenDiffValues map[int32]bool
	var seenDiffNames map[string]bool
	if diffKind != diffNone {
		if diffKind == diffName {
			seenDiffNames = make(map[string]bool)
		} else {
			seenDiffValues = make(map[int32]bool)
		}
	}
	for _, t := range ts {
		if t.IsPlayer {
			continue
		}
		o := g.Obj(t.Obj)
		lki := c.LKI != nil && c.LKI.ID == t.Obj
		if lki {
			// A zone-change trigger must read the causing object's snapshot,
			// not the same id after Move has cleared its counters and removed
			// battlefield layers. triggerLKI carries this value through the
			// TriggerPush wrapper to both initial and resumed resolution.
			o = c.LKI
		} else if ref == "Remembered" {
			for _, rememberedLKI := range c.Snap.ChangeZone {
				if rememberedLKI.Obj == t.Obj && rememberedLKI.Snapshot.Card != nil {
					snapshot := rememberedLKI.Snapshot
					o = &snapshot
					lki = true
					break
				}
			}
		}
		if o == nil {
			continue
		}
		f := o.Face()
		switch {
		case (ref == "TriggerObjectsCards" || ref == "TriggerRemembered") && prop == "CardTypes":
			if f != nil {
				for _, typ := range f.Types {
					if cardTypeWords[typ] {
						triggerObjectTypes[typ] = true
					}
				}
			}
		case ref == "TriggerObjectsCards" && prop == "GreatestCardManaCost":
			if f != nil && f.Cmc() > n {
				n = f.Cmc()
			}
		case prop == "CardPower":
			if f != nil {
				if lki && c.Snap.PTValid {
					n += c.Snap.Power
				} else if pt, ok := targetPTLKI(c, o); ok && !lki {
					n += pt.Power
				} else {
					n += refPower(h, o, lki)
				}
			}
		case prop == "GreatestCardPower":
			// Greatest-of, never summed (task greatestcardpower): the extreme
			// aggregate the ref-scoped carriers size X with -- Aloy, Savior of
			// Meridian's discover and Shriekwood Devourer's "untap up to X
			// lands" over TriggerObjectsAttackers, Shadowgrange Archfiend's
			// life gain over RememberedLKI. The per-object value is the same
			// derived, layer-aware read the CardPower case above falls back
			// to: live layer output through h.Power on the battlefield, the
			// printed face plus P/T counters for a remembered object that
			// already left (Shadowgrange's sacrificed creatures are in the
			// graveyard at the read). Property-scoped, not ref-scoped: every
			// ref that reaches this switch was already resolved to its
			// referent set by refTargets, so one case serves all three
			// carriers. An empty referent set keeps n = 0 and still resolves.
			if f != nil {
				if p := refPower(h, o, lki); p > n {
					n = p
				}
			}
		case prop == "CardToughness":
			if f != nil {
				if lki && c.Snap.PTValid {
					n += c.Snap.Toughness
				} else if pt, ok := targetPTLKI(c, o); ok && !lki {
					n += pt.Toughness
				} else {
					n += refToughness(h, o, lki)
				}
			}
		case prop == "CardManaCost" || prop == "CardManaCostLKI":
			// CardManaCostLKI (56 raw corpus lines -- 51
			// TriggeredSpellAbility$CardManaCostLKI, Sunbird's Invocation's
			// PeekAmount X among them) is Forge's LKI spelling of the same
			// property: the mana value the object HAD when the triggering
			// event happened. A face's mana value never changes and the lki
			// swap above already binds the zone-change snapshot when one is
			// carried, so the LKI spelling reads the same number the plain
			// spelling does -- one shared case, so the two cannot disagree.
			if f != nil {
				n += f.Cmc()
			}
		case prop == "CardNumColors":
			// The referenced object's colour count (task rv2b-countheads):
			// Mana Drain's drain (Lurking Spinecrawler's
			// TriggeredCard$CardNumColors, Moonveil Regent's and Mana
			// Cannons' Targeted$CardNumColors), read off the object the ref
			// names rather than the resolving source the plain
			// Count$CardNumColors head reads. h.ObjectColors is the SAME read
			// that head uses -- live layer-5 colours on a battlefield
			// permanent, the printed face's colours elsewhere (and on the lki
			// snapshot above, whose Face points at the unchanged card) -- so
			// the two spellings cannot disagree about one object.
			n += int32(len(h.ObjectColors(o)))
		case strings.HasPrefix(prop, "CardCounters."):
			// ALL is the sum over every kind (the same wildcard the plain
			// Count$CardCounters.ALL head reads -- Kinsbaile Borderguard's
			// TriggeredCard$CardCounters.ALL), never a literal kind lookup.
			// CR 608.2b/h: an object target that has left the battlefield is
			// read with the counters it had there, so Dismantle's
			// `X:Targeted$CardCounters.ALL` still sizes the placement after
			// the chained Destroy cleared the live counters. The trigger
			// snapshot (lki) has already substituted its own object above and
			// stays authoritative.
			if !lki && ref == "Targeted" {
				if cs, ok := targetCountersLKI(c, t.Obj, o); ok {
					oc := *o
					oc.Counters = cs
					o = &oc
				}
			}
			// A delayed trigger's remembered card has no trigger snapshot: the
			// registration outlives the death that made it. Its counters are
			// the ones it left the battlefield with (Nine-Lives Familiar's
			// "return it with one fewer revival counter").
			if !lki && o.Zone != state.ZBattlefield && delayedRemembers(c, t.Obj) {
				if dh, ok := h.(departureCountersHost); ok {
					if cs, ok := dh.DepartureCounters(t.Obj); ok {
						oc := *o
						oc.Counters = cs
						o = &oc
					}
				}
			}
			if strings.EqualFold(strings.TrimPrefix(prop, "CardCounters."), "ALL") {
				n += sumCounters(o.Counters)
			} else {
				n += o.Counter(strings.TrimPrefix(prop, "CardCounters."))
			}
		case prop == "Colors":
			colorsSeen |= ColorMaskOf(o)
		case prop == "Amount":
			// The count of referenced objects themselves (SVar:X:ExiledWith$Amount,
			// the same "how many" the Remembered$Amount head answers for the
			// Remembered ref). Players in the list do not count.
			n++
		case prop == "Valid" || strings.HasPrefix(prop, "Valid "):
			spec := strings.TrimSpace(strings.TrimPrefix(prop, "Valid"))
			sc := c.SpecContext(c.Controller)
			sc.DerivedPTs = append(sc.DerivedPTs, GreatestPowerDerivedPTs(g, spec, h)...)
			if (lki && MatchesObjectCtx(g, spec, o, sc)) ||
				(!lki && MatchesSpecCtx(g, spec, t.Obj, sc)) {
				n++
			}
		case prop == "Converge":
			// CR 107.4f-family converge, the TRIGGER-relative spelling: the
			// distinct-colour spend count of the cast the firing trigger is
			// about (Magmablood Archaic's SVar:Y:TriggeredCard$Converge), not
			// the resolving ability's own cast the plain Count$Converge head
			// at evalCountBody's "Converge" case reads off c.Source. Same
			// provenance discipline as that head and as TriggerPaidX: the
			// value was stamped on the cast spell by payCast's trailing
			// FlagConverged CastInfo BEFORE the deferred SpellCast trigger
			// re-walk fired, so a replay derives the same number; when the
			// read object IS the triggering card the fire-time snapshot
			// TriggerConverge wins over the live field, because a spell
			// countered between trigger push and resolution has had its
			// stack->graveyard move clear ConvergeColours while the colours
			// were spent regardless (CR 601.2h: the payment is not undone).
			// A copy of the spell was never cast and reads 0.
			if c.TriggerCard != 0 && t.Obj == c.TriggerCard {
				n += c.TriggerConverge
			} else {
				n += o.ConvergeColours
			}
		case prop == "CastTotalManaSpent" || strings.HasPrefix(prop, "CastTotalManaSpent "):
			// CR 601.2h / 106.12's payment, the TRIGGER-relative spelling: the
			// TOTAL mana actually spent to cast the spell the firing trigger is
			// about (Muse Seeker's "unless five or more mana was spent to cast
			// that spell", Aetherflux Conduit's energy gain), not the resolving
			// ability's own cast the plain Count$CastTotalManaSpent head at
			// evalCountBody reads off c.Source. The argument selects the same
			// subset that head's <Type> filter does (the bare total, the snow
			// part, or a modelled producer tag -- state.TypedManaTags), through
			// the one shared castManaSpentTotals.byTag so the two forms cannot
			// disagree. Same provenance discipline as Converge and TriggerPaidX:
			// the spend was stamped on the cast spell by payCast BEFORE the
			// deferred SpellCast trigger re-walk fired, so a replay derives the
			// same number; when the read object IS the triggering card the
			// fire-time snapshot wins over the live fields, because a spell that
			// has left the stack (countered, or resolved onto the battlefield)
			// before the trigger resolves has had its ManaSpent/ManaSnowSpent/
			// typed captures zeroed by the stack->zone move while the mana was
			// spent regardless (CR 601.2h: the payment is not undone). A copy
			// of the spell was never cast and reads 0.
			arg := strings.TrimSpace(strings.TrimPrefix(prop, "CastTotalManaSpent"))
			if c.TriggerCard != 0 && t.Obj == c.TriggerCard {
				n += castManaSpentTotals{total: c.TriggerManaSpent, snow: c.TriggerManaSnowSpent, typed: c.TriggerManaTyped}.byTag(arg)
			} else if snap, ok := targetManaSpentLKI(c, o); ok {
				// The target has left the stack (a Counter earlier in this
				// chain): its live captures are zeroed by the move, so read the
				// resolution-start snapshot instead (CR 608.2b/h).
				n += snap.byTag(arg)
			} else {
				n += manaSpentTotalsOf(o).byTag(arg)
			}
		default:
			if diffKind != diffNone {
				switch {
				case seenDiffNames != nil:
					if f := o.Face(); f != nil {
						seenDiffNames[f.Name] = true
					}
				case diffKind == diffPower:
					// The derived power, with the zone-change snapshot when one
					// is carried -- the CardPower case's exact read.
					if lki && c.Snap.PTValid {
						seenDiffValues[c.Snap.Power] = true
					} else {
						seenDiffValues[refPower(h, o, lki)] = true
					}
				case diffKind == diffToughness:
					if lki && c.Snap.PTValid {
						seenDiffValues[c.Snap.Toughness] = true
					} else {
						seenDiffValues[refToughness(h, o, lki)] = true
					}
				default:
					if v, ok := differentPropertyValue(h, o, diffKind); ok {
						seenDiffValues[v] = true
					}
				}
				continue
			}
			return 0, false
		}
	}
	if (ref == "TriggerObjectsCards" || ref == "TriggerRemembered") && prop == "CardTypes" {
		n = int32(len(triggerObjectTypes))
	}
	if prop == "Colors" {
		n = int32(bits.OnesCount8(uint8(colorsSeen)))
	}
	if diffKind != diffNone {
		if seenDiffNames != nil {
			n = int32(len(seenDiffNames))
		} else {
			n = int32(len(seenDiffValues))
		}
	}
	if hasOp {
		n = applyCountOpOperand(h, c, n, op, depth)
	}
	return n, true
}

// evalPlayerRefProperty resolves one "TargetedPlayer$<Property>[...][/Op]"
// (and the siblings "ThisTargetedPlayer$..." and "TargetedController$...")
// count body over the PLAYERS a target reference names -- the
// <Ref>$<Property> family's player-valued half, which evalRefProperty's
// object loop structurally cannot serve (it `continue`s every IsPlayer target
// and its property switch is object-only). The player list is the generic
// pre-ask's answered set (Ctx.PickedTargets) when non-nil, else the
// resolution's own Ctx.Targets, filtered to IsPlayer entries --
// effects/context.go's Defined$ Targeted precedence exactly, so the head
// answers the player the resolving body acts on. The count of players can be
// several; Forge's own Count sums over the referenced players the same way
// evalRefProperty sums over referenced objects.
//
// TargetedController$<Property> is the same family read through the CONtroller
// of the target list: its players are controllersOf(Ctx.Targets/PickedTargets)
// -- the exact resolution effects/context.go's Defined$ TargetedController
// case already uses -- so it shares this arm's whole property switch.
//
// A ref this arm does not special-case (TriggeredTarget, TriggeredPlayer,
// TriggeredDefendingPlayer, ...) is resolved through effects/context.go's
// shared definedSpec -- the SAME resolver the body's own Defined$ spelling
// goes through -- so the ref list is not a hand-maintained duplicate that
// can miss the next sibling. The LifeTotal head is wired for those refs
// (task rv2b-countheads: Quietus Spike's and Ebonblade Reaper's
// TriggeredTarget$LifeTotal/HalfUp); every OTHER property on such a ref
// still fails closed, so this arm's wider ref set cannot silently widen the
// player count semantics. An unknown ref or an unmodelled property returns
// false and the caller degrades to zero.
//
// Properties (the heads the 81-file corpus population is dominated by and
// that are exactly definable today): Amount (the number of player targets),
// LifeTotal (the player's current
// life), CardsInHand/CardsInLibrary/CardsInGraveyard (zone sizes),
// CreaturesInPlay (battlefield creatures the player controls), the Valid
// head and its countZone family (Valid/ValidHand/ValidGraveyard/
// ValidLibrary/ValidExile/ValidStack over a card spec; the referenced
// player is the filter's You, so `TargetedPlayer$ValidGraveyard
// Instant.YouOwn,Sorcery.YouOwn` counts the TARGET player's own
// instants/sorceries in THEIR graveyard -- The Mouth of Sauron's head),
// LifeLostThisTurn (the shared Host predicate), DamageThisTurn (the
// Host's per-player damage-taken fold, implemented engine-side beside
// LifeLostThisTurn) and Counters.Poison. The /Op suffix applies through
// applyCountOp like every other head. A property this build does not
// model (StartingLife, DomainPlayer, CardsDrawn, ...) or a ref
// outside the two names, the Defined$-resolved names above, the
// vote-carrier ref
// TriggeredPlayersOpponentVotedDiff (trig:Vote) and the DamageAll batch
// ref TriggeredPlayersTargets (trig:DamageAll; both refs' only property
// is Amount) returns false, and the caller degrades to zero
// exactly as evalRefProperty's default always did. CardsDiscardedThisTurn
// is the one optional head the brief allowed in: the shared Host predicate
// already existed.
func evalPlayerRefProperty(h Host, c *Ctx, expr string) (int32, bool) {
	ref, prop, found := strings.Cut(expr, "$")
	if !found || h == nil {
		return 0, false
	}
	prop, op, hasOp := strings.Cut(prop, "/")
	prop = strings.TrimSpace(prop)
	var ts []state.Target
	refCode := evalPlayerRefPropertyCodes.Code(string(ref))
	switch refCode {
	case evalPlayerRefPropertyTargetedPlayer:
		ts = c.Targets
		if c.PickedTargets != nil {
			ts = c.PickedTargets
		}
	case evalPlayerRefPropertyTargetedController:
		// The target list read through its controllers: the same
		// PickedTargets-else-Targets precedence as the TargetedPlayer arm,
		// converted with the shared controllersOf helper the Defined$
		// TargetedController case in effects/context.go also uses, so a
		// count head and a Defined$ spelling of the ref cannot disagree.
		// Lullmage's Domination's SVar:CheckTgt reads
		// `TargetedController$CardsInGraveyard` through the ReduceCost
		// static's Count$Compare (agent-20260928T043626Z-b7e271c1).
		src := c.Targets
		if c.PickedTargets != nil {
			src = c.PickedTargets
		}
		ts = controllersOf(h.Game(), src)
	case evalPlayerRefPropertyTriggeredPlayersOpponentVote:
		// The canonical vote-finished carrier's diff set (trig:Vote): the
		// fire-time referent capture is the ONLY binding, so a count read
		// outside a Vote resolution fails closed to the empty list -- the
		// same convention the vote's own Defined$ spellings take. Amount is
		// the count of those opponents (Erestor's SVar:X, the scry size).
		for _, p := range c.TriggeredOpponentsVotedDiff {
			ts = append(ts, state.Target{Player: p, IsPlayer: true})
		}
	default:
		// A ref this arm does not special-case, but the ENGINE's shared
		// Defined$ resolver already names (TriggeredTarget, TriggeredPlayer,
		// TriggeredDefendingPlayer, TriggeredCardController, ...): resolve it
		// through definedSpec -- the SAME resolver the body's own Defined$
		// spelling goes through -- and keep its player entries, so the ref
		// list is not a hand-maintained duplicate that can miss the next
		// sibling. This ticket (rv2b-countheads) wires the LifeTotal head for
		// those refs (Quietus Spike's / Ebonblade Reaper's
		// TriggeredTarget$LifeTotal/HalfUp). The property is confined to the
		// one head the ticket names so the change cannot widen the family's
		// other player count semantics (CardsInHand, Valid, Counters.Poison,
		// ...) for refs that used to fail closed; an unknown ref or property
		// still fails closed to (0, false).
		if prop != "LifeTotal" {
			return 0, false
		}
		pts, ok := definedSpec(h, c, ref)
		if !ok {
			return 0, false
		}
		for _, t := range pts {
			if t.IsPlayer {
				ts = append(ts, t)
			}
		}
	case evalPlayerRefPropertyTriggeredCapturedPlayers:
		// The firing trigger's fire-time PLAYER capture (Ctx.Captured) read
		// on purpose. The plain Remembered heads (Remembered$Amount,
		// Count$RememberedNumber) exclude that capture -- Forge's host
		// remembered list never holds the event referent -- so a body whose
		// count IS the referent set must name it through this Triggered*-
		// family ref instead. Its one user is the synthesized Melee pump
		// (cards.MeleePumpCount): rules captures one player ref per distinct
		// opponent attacked in the declaration (rules/melee.go
		// meleeRemembered) and the stack wrapper's logged Remembered comes
		// back as Captured at resolution, so replay and stack copies read the
		// same count. Amount is the only property.
		for _, t := range c.Captured {
			if t.IsPlayer {
				ts = append(ts, t)
			}
		}
	case evalPlayerRefPropertyTriggeredPlayersTargets:
		// The batch's matching TARGET PLAYERS (trig:DamageAll): Malcolm
		// Keen-Eyed Navigator's and Hordewing Skaab's SVar:X reads the count
		// of opponents the damage batch dealt damage to ("create a Treasure
		// token for each opponent dealt damage" / "draw cards equal to the
		// number of opponents dealt damage this way"). The capture is the
		// fire-time batch target set, filtered to its player entries in
		// first-seen order; Amount is the count of those players.
		for _, t := range c.TriggerDamageTargets {
			if t.IsPlayer {
				ts = append(ts, state.Target{Player: t.Player, IsPlayer: true})
			}
		}
	}
	// TriggeredPlayersOpponentVotedDiff is the canonical vote-finished
	// carrier's diff set (trig:Vote); its ONLY documented property is Amount
	// (Erestor's SVar:X, the scry size). Confine the head to it here, so the
	// ref cannot silently inherit LifeTotal/CardsInHand/Valid... sums that
	// belong to TargetedPlayer/ThisTargetedPlayer -- the contract the
	// evalPlayerRefProperty doc states. TriggeredPlayersTargets (the
	// DamageAll batch ref) takes the same confinement: its only modelled
	// property is Amount, so a future property on it fails closed to zero
	// instead of silently reading the batch players' current zone sizes.
	if ref == "TriggeredPlayersOpponentVotedDiff" && prop != "Amount" {
		return 0, false
	}
	if (ref == "TriggeredPlayersTargets" || ref == "TriggeredCapturedPlayers") && prop != "Amount" {
		return 0, false
	}
	g := h.Game()
	var n int32
	for _, t := range ts {
		if !t.IsPlayer {
			continue
		}
		p := t.Player
		switch {
		case prop == "LifeTotal":
			n += g.Players[p].Life
		case prop == "CardsInHand":
			n += int32(len(g.Zone(state.ZHand, p)))
		case prop == "CardsInLibrary":
			n += int32(len(g.Zone(state.ZLibrary, p)))
		case prop == "CardsInGraveyard":
			n += int32(len(g.Zone(state.ZGraveyard, p)))
		case prop == "LifeLostThisTurn":
			n += h.LifeLostThisTurn(p)
		case prop == "DamageThisTurn":
			n += h.DamageTakenThisTurn(p)
		case prop == "CardsDiscardedThisTurn":
			n += h.CardsDiscardedThisTurn(p)
		case prop == "TotalCommanderCastFromCommandZone" || prop == "CommanderCastFromCommandZone":
			n += h.CommanderCastsFromCommandZone(p)
		case prop == "Counters.Poison":
			for _, pc := range g.Players[p].Counters {
				if pc.Kind == "POISON" {
					n += pc.N
				}
			}
		case prop == "CreaturesInPlay":
			// Battlefield creatures the referenced player controls, through
			// the same spec matcher the Valid family uses.
			for _, id := range g.Zone(state.ZBattlefield, p) {
				if matchesZoneSpecCtx(g, "Creature", id, c.SpecContext(p), state.ZBattlefield) {
					n++
				}
			}
		case prop == "Amount" && (refCode == evalPlayerRefPropertyTargetedPlayer ||
			refCode == evalPlayerRefPropertyTriggeredPlayersOpponentVote ||
			refCode == evalPlayerRefPropertyTriggeredPlayersTargets ||
			ref == "TriggeredCapturedPlayers"):
			n++
		default:
			// The Valid head and its countZone family: "Valid <spec>",
			// "ValidGraveyard <spec>", ... -- the exact template of the
			// Count$Valid<zone> head, scoped to the referenced player's zone
			// and matched with the referenced player as the filter's You.
			head, spec, _ := strings.Cut(prop, " ")
			spec = strings.TrimSpace(spec)
			zone, ok := countZone(head)
			if !ok {
				return 0, false
			}
			if spec == "" {
				// A head with no filter counts the zone itself (corpus:
				// every TargetedPlayer$Valid... occurrence carries a spec;
				// the bare form stays the honest reading rather than a
				// fail-closed zero).
				n += int32(len(g.Zone(zone, p)))
				continue
			}
			for _, id := range g.Zone(zone, p) {
				if matchesZoneSpecCtx(g, spec, id, c.SpecContext(p), zone) {
					n++
				}
			}
		}
	}
	if hasOp {
		n = applyCountOpOperand(h, c, n, op, 0)
	}
	return n, true
}

// refPower/refToughness use rules' derived characteristics while a referenced
// object is a battlefield permanent. A referred-to object that already left
// keeps the LKI-compatible printed-face-plus-P/T-counters fallback: no live
// layer applies in a graveyard, and asking Host for it would read a different
// state. The counter sum covers every P/T counter kind (CR 613.7d), not just
// the +1/+1 / -1/-1 pair.
// inProgressDerivedPTHost is implemented by rules.Engine so a Count$ read made
// during layer 7 can consume the current walk's value instead of recursively
// asking the host to derive the same object again. It is optional to preserve
// the small effects.Host contract and its test doubles.
type inProgressDerivedPTHost interface {
	InProgressDerivedPT(id state.ObjID) (power, toughness int32, ok bool)
}

func refPower(h Host, o *state.Object, snapshot bool) int32 {
	if !snapshot && o.Zone == state.ZBattlefield {
		if provider, ok := h.(inProgressDerivedPTHost); ok {
			if power, _, found := provider.InProgressDerivedPT(o.ID); found {
				return power
			}
		}
		return h.Power(o.ID)
	}
	dp, _ := o.CounterPTTotals()
	return int32(o.Face().Power()) + dp
}

func refToughness(h Host, o *state.Object, snapshot bool) int32 {
	if !snapshot && o.Zone == state.ZBattlefield {
		if provider, ok := h.(inProgressDerivedPTHost); ok {
			if _, toughness, found := provider.InProgressDerivedPT(o.ID); found {
				return toughness
			}
		}
		return h.Toughness(o.ID)
	}
	_, dt := o.CounterPTTotals()
	return int32(o.Face().Toughness()) + dt
}

// departureCountersHost is the optional host read behind a delayed trigger's
// last-known counters: the counters an object carried when it last left the
// battlefield, derived from the event log so a replay answers identically.
type departureCountersHost interface {
	DepartureCounters(state.ObjID) ([]state.Counter, bool)
}

// delayedRemembers reports whether id is in the resolving delayed trigger's
// own registration capture.
func delayedRemembers(c *Ctx, id state.ObjID) bool {
	if c == nil {
		return false
	}
	for _, t := range c.TriggerContext.DelayedRemembered {
		if !t.IsPlayer && t.Obj == id {
			return true
		}
	}
	return false
}

type evalPlayerRefPropertyCode uint16

const (
	evalPlayerRefPropertyTargetedPlayer evalPlayerRefPropertyCode = iota + 1
	evalPlayerRefPropertyTargetedController
	evalPlayerRefPropertyTriggeredPlayersOpponentVote
	evalPlayerRefPropertyTriggeredCapturedPlayers
	evalPlayerRefPropertyTriggeredPlayersTargets
)

var evalPlayerRefPropertyCodes = state.NewStrCodes(
	state.StrEntry[evalPlayerRefPropertyCode]{Key: "TargetedPlayer", Val: evalPlayerRefPropertyTargetedPlayer},
	state.StrEntry[evalPlayerRefPropertyCode]{Key: "ThisTargetedPlayer", Val: evalPlayerRefPropertyTargetedPlayer},
	state.StrEntry[evalPlayerRefPropertyCode]{Key: "TargetedController", Val: evalPlayerRefPropertyTargetedController},
	state.StrEntry[evalPlayerRefPropertyCode]{Key: "TriggeredPlayersOpponentVotedDiff", Val: evalPlayerRefPropertyTriggeredPlayersOpponentVote},
	state.StrEntry[evalPlayerRefPropertyCode]{Key: "TriggeredCapturedPlayers", Val: evalPlayerRefPropertyTriggeredCapturedPlayers},
	state.StrEntry[evalPlayerRefPropertyCode]{Key: "TriggeredPlayersTargets", Val: evalPlayerRefPropertyTriggeredPlayersTargets},
)
