package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// evalCountBodyProvenance evaluates the two whole-body provenance heads,
// ThisTurnCast_<spec> and ThisTurnActivated_<spec>, which keep their spaces
// and therefore claim the body before the generic head/argument split.
func evalCountBodyProvenance(h Host, c *Ctx, body string, depth int) (int32, bool, bool) {
	// ThisTurnCast_<spec> keeps the WHOLE body as the spec, before the
	// generic head/space split below: a Forge count spec can carry a space
	// (Rain of Riches' "Card.YouCtrl+CastSa Spell.ManaFromTreasure" — the
	// card-level CastSa property token), and the split would truncate the
	// spec at the space and drop the property. The /Op suffix was already
	// cut by the caller. Space-free specs take the identical path they took
	// through the switch arm (the same reads, the same returns), so every
	// existing carrier is byte-identical.
	if rest, ok := strings.CutPrefix(body, "ThisTurnCast_"); ok {
		if stripped, selfExcl := stripBareCastSaSource(rest); selfExcl {
			return int32(h.SpellsCastThisTurnMatchingExcluding(c.Controller, stripped, c.Source)), true, true
		}
		// The ARGUMENTED forms (task castprov2) peel the token and reuse the
		// same Excluding read:
		//
		//   - !CastSaSource/<op> (thunder_salvo's /Plus.2): this form never
		//     reaches this arm — evalCountExprOK's GENERIC /Op peel cuts the
		//     body at the first "/" before the head parse, leaving the bare
		//     !CastSaSource for the bare arm above and handing the op to the
		//     ordinary applyCountOp — which is exactly the oracle's reading
		//     (the exclusion count, then Plus.2). Pinned by
		//     TestThunderSalvoXIsTwoPlusOtherSpellsCast.
		//   - !CastSaSource$<Property> (call_forth_the_tempest's
		//     $CardManaCost): the matching casts' objects, the property
		//     AGGREGATED over them instead of counting 1 each (the zone-count
		//     heads' `$Property` precedent). An unknown property fails closed
		//     to (0, false), the unresolvable verdict.
		if stripped, prop, ok2 := stripCastSaSourceAggregate(rest); ok2 {
			av, aok := aggregateCastProperty(h, h.EachSpellCastThisTurnMatching(c.Controller, stripped, c.Source), prop)
			return av, aok, true
		}
		// The COUNT-level `$<Property>` suffix (rootha_mastering_the_moment's
		// `Instant.YouCtrl,Sorcery.YouCtrl$GreatestCardManaCost` and
		// april_oneil_hacktivist's `Card.YouCtrl$CardTypes`): the matching
		// casts' objects, the property folded over them instead of counting
		// them one each. This is the SAME `$Property` precedent as the zone
		// head (Count$Valid <spec>$GreatestCardManaCost), but on the
		// cast-count branch. The peel runs AFTER the two CastSaSource arms so
		// it can never steal the predicate-token `$` of an argumented
		// `!CastSaSource$<Property>` (call_forth_the_tempest), and it fires
		// ONLY when the segment after the last `$` is in the admitted
		// vocabulary -- an unadmitted suffix keeps the byte-identical
		// whole-token read below.
		if stripped, prop, ok3 := stripCastSourceAggregate(rest); ok3 {
			av, aok := aggregateCastSourceProperty(h, h.EachSpellCastThisTurnMatching(c.Controller, stripped, c.Source), prop)
			return av, aok, true
		}
		return int32(h.SpellsCastThisTurnMatching(c.Controller, rest)), true, true
	}
	// ThisTurnActivated_<spec> (Professor Hojo's and Tezzeret, Betrayer of
	// Flesh's "the first activated ability ... each turn" gates, the Equip/
	// Cycling/Exhaust families' counts): the activated abilities activated
	// this turn matching an `Activated.<props>` spec, the in-flight
	// activation being priced (Ctx.AffectedAbility) included -- Forge records
	// an activation before its cost is adjusted, which is why those gates
	// read LE1. The spec keeps its spaces (IsTargeting Valid <spec>), so the
	// arm runs before the head split, exactly as ThisTurnCast_ does. The log
	// fold lives in rules; a Host without it leaves the head unmodelled.
	if rest, ok := strings.CutPrefix(body, "ThisTurnActivated_"); ok {
		// The Ctx's fields are passed by value, never the *Ctx itself: a Ctx
		// handed to an interface method escapes, and the hot layer walk
		// builds one per object (TestEvalCountValidZoneScanIsAllocationFree).
		provider, okP := h.(interface {
			AbilitiesActivatedThisTurnMatching(you state.PlayerID, affected state.ObjID, ab *cards.SA, targets []state.Target, spec string) (int32, bool)
		})
		if !okP {
			return 0, false, true
		}
		av, aok := provider.AbilitiesActivatedThisTurnMatching(c.Controller, c.AffectedObj, c.AffectedAbility, c.Targets, strings.TrimSpace(rest))
		return av, aok, true
	}
	return 0, false, false
}

// evalCountBodyCost evaluates the scalar heads claimed right after the
// head/argument split: the TotalDamageReceivedThisTurn shortcut, the
// Convoked$/TargetedByTarget$ forms, the fuzz-cov3 heads and the first
// switch-arm group (YouDescendedThisTurn through SquadPaid).
func evalCountBodyCost(h Host, c *Ctx, g *state.Game, head, arg string, depth int) (int32, bool, bool) {
	if head == "TotalDamageReceivedThisTurn" && arg == "" {
		self := c.TriggerCard
		if self == 0 {
			self = c.Source
		}
		if self == 0 {
			return 0, true, true
		}
		o := h.Game().Obj(self)
		if c.LKI != nil && c.LKI.ID == self {
			o = c.LKI
		}
		if o == nil {
			return 0, true, true
		}
		return o.DamageReceivedThisTurn, true, true
	}
	// Convoked$Amount (CR 702.66): the number of creatures that convoked the
	// resolving spell's cast -- Forge's own `SVar:X:Convoked$Amount` head.
	// The count reads the SAME source-object provenance the Defined$ Convoked
	// selector reads (effects/context.go's definedSpec): the pay-time
	// CastInfo's FlagConvoked IDs, folded onto Object.Convoked by
	// events.Apply and preserved across the stack->battlefield move, so the
	// ETB half of Ancient Imperiosaur and Knight-Errant of Eos reads the same
	// set the spell on the stack did. A cast with no convoke, an absent
	// source and a copy all read a legitimate zero (the modelled-head
	// convention every other cast-provenance count takes, NOT the
	// unresolvable verdict the fallthrough gives). A creature that left play
	// after convoking still counts -- CR 702.66 counts the creatures that
	// CONVOKED, and Object.Convoked holds their ids.
	//
	// This is a `<Head>$<Property>` body, so it carries its OWN optional /Op
	// exactly like PlayerCountHasLost$Amount/Times.10: a
	// `Count$Convoked$Amount/Twice` gets the suffix peeled upstream by
	// evalCountExprOK and applied generically, while the corpus's bare
	// `SVar:X:Convoked$Amount/Twice` (Ancient Imperiosaur's two
	// counters-per-creature) reaches here with the suffix intact and must
	// strip it before the exact-name compare. Splitting it here -- not in a
	// second Twice arm -- keeps ONE composition path for the op.
	if rest, ok := strings.CutPrefix(head, "Convoked$"); ok {
		name, op, hasOp := strings.Cut(rest, "/")
		if strings.TrimSpace(name) != "Amount" || hasOp && !validConvokedCountOp(op) {
			return 0, false, true
		}
		n := int32(0)
		if o := g.Obj(c.Source); o != nil {
			n = int32(len(o.Convoked))
		}
		if hasOp {
			n = applyCountOp(n, op)
		}
		return n, true, true
	}

	// TargetedByTarget$Valid <spec> (Forge; 2 corpus carriers -- Not of This
	// World's SVar:CheckTgt:TargetedByTarget$Valid Card.powerGE7+YouCtrl and
	// Bane's Contingency's ...IsCommander+YouCtrl+inZoneBattlefield): the
	// number of objects, among the targets of the spell/ability(ies) that the
	// resolving source targets, that match <spec> -- the NESTED read one
	// level past spellIsTargetingMatches' own .Targets walk. The resolving
	// source's targets are Ctx.Targets (registry.go), the same binding the
	// Targeted ref reads; the cost-modifier static path binds the cast's
	// chosen or potential targets onto it (rules' modAmountX), so Not of This
	// World's Amount$ Compare gate sees Giant Growth's chosen target both at
	// the offer gate and at the CR 601.2c reprice. Object targets of each
	// targeted spell are matched through MatchesSpecCtx with the resolving
	// context's SpecContext (You/Source/Remembered bound as everywhere else);
	// player targets cannot match a Card... spec and are skipped. An absent
	// or empty Ctx.Targets is a legitimate zero -- a MODELLED head, not the
	// unresolvable verdict -- so the Compare gate fails closed at 0 (no
	// reduction) instead of erroring. A property other than Valid (the only
	// form either carrier uses) and an empty spec fail closed per the
	// unmodelled-property convention.
	if rest, ok := strings.CutPrefix(head, "TargetedByTarget$"); ok {
		if strings.TrimSpace(rest) != "Valid" || arg == "" {
			return 0, false, true
		}
		n := int32(0)
		sc := c.SpecContext(c.Controller)
		for _, t := range c.Targets {
			if t.IsPlayer || t.Obj == 0 {
				continue
			}
			inner := g.Obj(t.Obj)
			if inner == nil {
				continue
			}
			for _, it := range inner.Targets {
				if it.IsPlayer || it.Obj == 0 {
					continue
				}
				isc := sc
				// The zone-count fold's derived-PT bind (zoneCountFold.visit):
				// a battlefield inner target's numeric filter must read rules'
				// layer-derived characteristics (Syr Elenora's power-equals-hand
				// size), not the printed face -- skip the bind entirely unless
				// the spec reads a P/T field, the same dependency guard.
				if io := g.Obj(it.Obj); io != nil && io.Zone == state.ZBattlefield && SpecReadsPT(arg) {
					if provider, ok := h.(interface {
						FilterDerivedPT(state.ObjID) (power, toughness, basePower, baseToughness int32, ok bool)
					}); ok {
						if power, toughness, basePower, baseToughness, found := provider.FilterDerivedPT(it.Obj); found {
							isc.DerivedPower, isc.DerivedToughness, isc.HasDerivedPT = power, toughness, true
							isc.BasePower, isc.BaseToughness, isc.HasBasePT = basePower, baseToughness, true
						}
					}
				}
				if MatchesSpecCtx(g, arg, it.Obj, isc) {
					n++
				}
			}
		}
		return n, true, true
	}

	// The fuzz-cov3 heads (effects/count_cov3.go): heads that previously
	// matched nothing below, so consulting them first changes no verdict
	// another arm gave.
	if n, ok := evalCov3Head(h, c, head, arg, depth); ok {
		return n, true, true
	}
	if n, ok := evalCov3PlayerHead(h, c, head, arg, depth); ok {
		return n, true, true
	}

	switch evalCountBodyCostCodes.Code(string(head)) {
	case evalCountBodyCostYouDescendedThisTurn:
		// The number of times the resolving controller descended this turn
		// (CR 700.11): The Mycotyrant's TokenAmount$ X and Molten Collapse's
		// CharmNum$ Count$Compare Y GE1.2.1. Reads the same fx20 provenance
		// ledger as the Player.descended predicate (descendedThisTurn), so
		// the head and the predicate cannot drift. An out-of-range seat and
		// an empty ledger are both a MODELLED zero -- never the unresolvable
		// verdict, which would make an SVar gate fail closed.
		if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
			return 0, true, true
		}
		return descendedThisTurn(g, c.Controller), true, true
	case evalCountBodyCostCompare:
		return evalCompare(h, c, arg, depth), true, true
	case evalCountBodyCostMostCardName:
		// Forge's Count$MostCardName <spec> (task api-winsgame; 4 corpus
		// carriers -- Mechanized Production, Endless Atlas, Chrome
		// Replicator, Sceptre of Eternal Glory): the GREATEST number of
		// battlefield objects matching <spec> that share one face name.
		// Mechanized Production's "eight or more artifacts with the same
		// name as one another" is exactly this read over
		// Artifact.YouCtrl. An empty spec is unresolvable (fail closed),
		// the same verdict every other argument-taking head gives a
		// missing argument.
		av, aok := countMostCardName(h, c, arg)
		return av, aok, true
	case evalCountBodyCostResolvedThisTurn:
		// Forge's Count$ResolvedThisTurn: how many times the resolving ability
		// has resolved this turn, the current resolution included ("if this is
		// the FOURTH time ... transform"). rules binds the per-ability tally
		// onto Ctx; an unbound Ctx reads a legitimate zero, the same
		// modelled-head zero every other count gives (NOT the unresolvable
		// verdict), so the SVar gate fails closed at 0 rather than failing
		// open and running its sub on the first resolution (the Sephiroth
		// transform defect this head's absence caused).
		return c.ResolvedThisTurn, true, true
	case evalCountBodyCostCardNumColors:
		if o := g.Obj(c.Source); o != nil {
			return int32(len(h.ObjectColors(o))), true, true
		}
		return 0, true, true
	case evalCountBodyCostValidSelf:
		// Forge's Count$ValidSelf <Card$property> reads a property of the
		// source object ITSELF rather than of a scanned zone set (Diligent
		// Zookeeper's `SVar:AffectedX:Count$ValidSelf
		// Card$CreatureType/LimitMax.10`, whose AffectedX
		// AddPower$/AddToughness$ the layer-7c modify walk anchors on each
		// affected creature). c.Source is that object: for an AffectedX
		// static rules.staticAmountOn binds the recipient's id as the
		// anchor, and the /LimitMax.<n> cap arrives as the Count$ /Op
		// suffix (clamped by countDistinctLimitMax above, which admits this
		// head for the bounded CreatureType property). The corpus's other
		// ValidSelf argument shapes -- Card.!IsPrepared (the prepared
		// mechanic), Card.IsSuspected, and the unprefixed
		// Creature.greatestPowerControlledByCardController -- are NOT this
		// distinct-creature-type read, so they route to evalCountValidSelf
		// below, which matches Self through the shared filter matcher and
		// fails closed with an evaluated zero when that match cannot be read.
		// Counted over the object's effective layer-4 types (falling back to
		// the printed face when no derived entry exists), with the SAME
		// subtype vocabulary the sibling Count$Valid <spec>$CreatureType
		// distinct-set read uses (creatureSubtypeWords). AffectedX P/T reads
		// run in layer 7, after layer 4 establishes these characteristics.
		// The seen set is read only through len, so no map ordering reaches
		// an event or view. Every OTHER argument is the event-anchored Self
		// match (Kraven's greatest-power death gate); ONE home for the head so
		// this AffectedX read cannot be preempted by an earlier dispatch.
		if strings.TrimSpace(arg) != "Card$CreatureType" {
			av, aok := evalCountValidSelf(h, c, arg)
			return av, aok, true
		}
		o := g.Obj(c.Source)
		if o == nil || o.Face() == nil {
			return 0, true, true
		}
		types := o.Face().Types
		for _, derived := range c.Layers.DerivedTypes {
			if derived.ID == o.ID {
				types = derived.Types
				break
			}
		}
		seen := make(map[string]bool)
		for _, typ := range types {
			if creatureSubtypeWords[typ] {
				seen[typ] = true
			}
		}
		return int32(len(seen)), true, true
	case evalCountBodyCostCardNumAttacksThisTurn:
		// Forge's Count$CardNumAttacksThisTurn: how many times THIS object has
		// attacked this turn (Moraug, Fury of Akoum's "+1/+0 for each time it
		// has attacked this turn"). state.Object.AttacksThisTurn is the
		// event-folded per-object tally -- events.Apply increments it on each
		// DeclareAttackers and TurnChange resets it -- so the read is
		// deterministic and replay-stable. c.Source is the object the count
		// anchors on: for the AffectedX static that is the recipient creature
		// (rules.staticAmountOn binds the affected id), and for an ordinary
		// SVar body it is the resolving source. A missing source reads a
		// legitimate zero -- the modelled-head convention, NOT the
		// unresolvable verdict.
		if o := g.Obj(c.Source); o != nil {
			return o.AttacksThisTurn, true, true
		}
		return 0, true, true
	case evalCountBodyCostXPaid:
		// CR 107.3i: the {X} paid for the resolving spell or ability. On a
		// TRIGGER of a permanent that was cast for {X} the ability object's
		// own X is zero (a trigger was never paid an X), so the paid value
		// is read off the source permanent, which CastInfo carried out of
		// the cast onto the battlefield object (Meathook Massacre II's
		// SVar:X:Count$xPaid driving "each player sacrifices X creatures").
		if c.X != 0 {
			return c.X, true, true
		}
		if o := g.Obj(c.Source); o != nil {
			return o.X, true, true
		}
		return 0, true, true
	case evalCountBodyCostReplicatePaid:
		// CR 702.55a: the number of replicate payments the resolving spell's
		// cast made, carried by the pay-time CastInfo's FlagReplicated Amount
		// (rules/cast.go's replicateAsk and payCast). Read off the SOURCE --
		// the cast spell, the same provenance read xPaid makes -- so a replay
		// derives the same count; a copy of the spell was never cast and
		// reads 0.
		if o := g.Obj(c.Source); o != nil {
			return o.ReplicateTimes, true, true
		}
		return 0, true, true
	case evalCountBodyCostSquadPaid:
		// CR 702.66: the number of squad payments the resolving spell's cast
		// made ("you may pay [cost] any number of times"), carried by the
		// pay-time CastInfo's FlagSquadPaid Amount (rules/cast.go's squadAsk
		// and payCast). The same provenance read ReplicatePaid makes: read off
		// the SOURCE -- the cast spell on the stack, and in the keyword
		// expansion's ETB trigger the permanent the spell became (the
		// stack->battlefield move preserves the field) -- so a replay derives
		// the same count; a copy of the spell was never cast and reads 0.
		if o := g.Obj(c.Source); o != nil {
			return o.SquadPaid, true, true
		}
		return 0, true, true
	}
	return 0, false, false
}

type evalCountBodyCostCode uint16

const (
	evalCountBodyCostYouDescendedThisTurn evalCountBodyCostCode = iota + 1
	evalCountBodyCostCompare
	evalCountBodyCostMostCardName
	evalCountBodyCostResolvedThisTurn
	evalCountBodyCostCardNumColors
	evalCountBodyCostValidSelf
	evalCountBodyCostCardNumAttacksThisTurn
	evalCountBodyCostXPaid
	evalCountBodyCostReplicatePaid
	evalCountBodyCostSquadPaid
	// The "plain" heads evalCountBodySimple claims. They share this
	// vocabulary (there is a hard cap on StrCodes tables, so a new one is not
	// an option); evalCountBodyCost's own switch has no case for them, so its
	// matched verdict stays false and the dispatch reaches the simple phase.
	evalCountBodyCostIsPrime
	evalCountBodyCostImprintedSize
	evalCountBodyCostFinishedEndOfTurnsThisTurn
)

var evalCountBodyCostCodes = state.NewStrCodes(
	state.StrEntry[evalCountBodyCostCode]{Key: "YouDescendedThisTurn", Val: evalCountBodyCostYouDescendedThisTurn},
	state.StrEntry[evalCountBodyCostCode]{Key: "Compare", Val: evalCountBodyCostCompare},
	state.StrEntry[evalCountBodyCostCode]{Key: "MostCardName", Val: evalCountBodyCostMostCardName},
	state.StrEntry[evalCountBodyCostCode]{Key: "ResolvedThisTurn", Val: evalCountBodyCostResolvedThisTurn},
	state.StrEntry[evalCountBodyCostCode]{Key: "CardNumColors", Val: evalCountBodyCostCardNumColors},
	state.StrEntry[evalCountBodyCostCode]{Key: "ValidSelf", Val: evalCountBodyCostValidSelf},
	state.StrEntry[evalCountBodyCostCode]{Key: "CardNumAttacksThisTurn", Val: evalCountBodyCostCardNumAttacksThisTurn},
	state.StrEntry[evalCountBodyCostCode]{Key: "xPaid", Val: evalCountBodyCostXPaid},
	state.StrEntry[evalCountBodyCostCode]{Key: "ReplicatePaid", Val: evalCountBodyCostReplicatePaid},
	state.StrEntry[evalCountBodyCostCode]{Key: "SquadPaid", Val: evalCountBodyCostSquadPaid},
	state.StrEntry[evalCountBodyCostCode]{Key: "IsPrime", Val: evalCountBodyCostIsPrime},
	state.StrEntry[evalCountBodyCostCode]{Key: "ImprintedSize", Val: evalCountBodyCostImprintedSize},
	state.StrEntry[evalCountBodyCostCode]{Key: "FinishedEndOfTurnsThisTurn", Val: evalCountBodyCostFinishedEndOfTurnsThisTurn},
)
