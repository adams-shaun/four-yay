package effects

import (
	"iter"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// A Forge filter spec is alternatives separated by "," (OR). Each alternative
// is a base type, optionally negated with a "non" prefix, followed by
// ".pred+pred+..." (AND). A raw comma in a named<Name>/notnamed<Name>
// argument is part of the printed name when what follows is not another
// filter alternative; filterAlternatives owns that ambiguity in one place.
//
// Unknown predicates never match. A filter that silently widens is how a rules
// engine quietly does the wrong thing, so the failure mode is "this card does
// nothing", which testing catches, rather than "this card does too much",
// which it does not.

type predFn func(g *state.Game, o *state.Object, you state.PlayerID, source state.ObjID) bool

// keywordPredicates maps a `with<Keyword>`/`without<Keyword>` predicate name
// to the keyword it tests and whether it is negated. The predicate-map
// functions below can only answer from the object alone (printed face plus
// marker counters), but a rules caller that has already run the layer walk
// can bind the FULL derived keyword list through SpecContext.ExtraKeywords --
// exactly the ExtraTypes seam for layer-4 types -- so a layer-6 AddKeyword$
// grant is visible to the filter that gates it (Cavalry Master's
// `withFlanking` lord, kw:Flanking's `withoutFlanking` blocker check).
type keywordPredicate struct {
	keyword      string
	parameter    string
	hasParameter bool
	negated      bool
}

var keywordPredicates = map[string]keywordPredicate{}

var parameterizedKeywordPredicateHeads = map[string]string{
	"hasKeywordLandwalk": "Landwalk",
	"hasKeywordEnchant":  "Enchant",
}

// keywordPredicateFor classifies a supported with<X>/without<X> token. Forge
// scripts retain spaces in keyword names, while the registry keys are compact;
// only these keyword-predicate forms are normalized, leaving every other
// predicate argument byte-for-byte significant.
func keywordPredicateFor(p string) (keywordPredicate, bool) {
	if kp, ok := keywordPredicates[p]; ok {
		return kp, true
	}
	if strings.HasPrefix(p, "with") || strings.HasPrefix(p, "without") || strings.HasPrefix(p, "hasKeyword") {
		compact := strings.ReplaceAll(p, " ", "")
		if kp, ok := keywordPredicates[compact]; ok {
			return kp, true
		}
		// Landwalk and Enchant carry answerable keyword parameters in Forge
		// filters (e.g. hasKeywordLandwalk:Island and
		// hasKeywordEnchant:Creature). Keep this vocabulary explicit: a
		// parameter on another keyword, or an empty parameter, is unsupported.
		name, parameter, hasParameter := strings.Cut(compact, ":")
		keyword, supported := parameterizedKeywordPredicateHeads[name]
		if hasParameter && len(parameter) > 0 && supported {
			return keywordPredicate{keyword: keyword, parameter: parameter, hasParameter: true}, true
		}
	}
	return keywordPredicate{}, false
}

var predicates = map[string]predFn{
	"YouCtrl": func(g *state.Game, o *state.Object, you state.PlayerID, _ state.ObjID) bool {
		return o.Controller == you
	},
	"YouDontCtrl": func(g *state.Game, o *state.Object, you state.PlayerID, _ state.ObjID) bool {
		return o.Controller != you
	},
	"OppCtrl": func(g *state.Game, o *state.Object, you state.PlayerID, _ state.ObjID) bool {
		return o.Controller != you
	},
	"YouOwn": func(g *state.Game, o *state.Object, you state.PlayerID, _ state.ObjID) bool { return o.Owner == you },
	// YouDontOwn is Forge's CardProperty YouDontOwn: the card's owner is not
	// the evaluating controller (Gonti, Canny Acquisitor's "spells you cast
	// but don't own").
	"YouDontOwn": func(g *state.Game, o *state.Object, you state.PlayerID, _ state.ObjID) bool { return o.Owner != you },
	// hasXCost is Forge's CardProperty hasXCost: the card's mana cost
	// carries at least one {X} (ManaCost.countX > 0) -- Zimone, Infinite
	// Analyst's "spell with {X} in its mana cost". A face-down object has no
	// mana cost (CR 708.2), so it never matches.
	"hasXCost": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		if o == nil || o.FaceDown || o.Face() == nil {
			return false
		}
		for tok := range strings.FieldsSeq(o.Face().ManaCost) {
			if tok == "X" {
				return true
			}
		}
		return false
	},
	// DrawnThisTurn is Forge's Card.getDrawnThisTurn (Captain Eberhart's
	// "spells cast from among cards you drew this turn"): the object's last
	// Draw is this turn's and it has since moved nowhere but the stack --
	// state.Object.DrawnTurn, stamped by events.Apply's Draw fold and cleared
	// by every other move (Forge keeps the flag only onto the stack).
	"DrawnThisTurn": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o != nil && o.DrawnTurn != 0 && o.DrawnTurn == g.Turn
	},
	"foretold": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.CastFlags&state.FlagForetold != 0
	},
	// tokenCreated is Forge's zone-entry provenance predicate. In the
	// Count$ThisTurnEntered_* specs that use it, a token's IsToken marker is
	// the exact per-object meaning: tokens that leave cease to exist, and the
	// entry list is cleared at TurnChange. A resolving creature-spell copy
	// that enters the battlefield is also a token under CR 707.10g, so it
	// correctly matches through the same marker.
	"tokenCreated": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.IsToken
	},
	// firstTurnControlled is Forge's Card.isFirstTurnControlled: the
	// permanent came under its controller's control since that player's most
	// recent turn began. That is exactly the object's summoning-sickness
	// flag, which events.Apply raises for EVERY permanent (not only
	// creatures) on a battlefield entry and on a control change and clears
	// at its controller's TurnChange. Rocket Launcher's `IsPresent$
	// Card.Self+!firstTurnControlled` ("activate only if you've controlled it
	// continuously since the beginning of your most recent turn") and the
	// Master of Arms / Norritt / Seasinger families read it; before it was
	// recognised the spec failed closed and Rocket Launcher's only ability
	// was never offered (cardfuzz coverage audit).
	"firstTurnControlled": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.Zone == state.ZBattlefield && o.SummonSick
	},
	"OppOwn":   func(g *state.Game, o *state.Object, you state.PlayerID, _ state.ObjID) bool { return o.Owner != you },
	"Self":     func(g *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool { return o.ID == src },
	"Other":    func(g *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool { return o.ID != src },
	"tapped":   func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool { return o.Tapped },
	"untapped": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool { return !o.Tapped },
	// phasedOut is Forge's Card.isPhasedOut (CR 702.25b): a phased-out
	// BATTLEFIELD permanent. Phasing is a status, not a zone, so a card that
	// left the battlefield (its PhasedOut cleared by the Move fold, CR
	// 702.25e) never matches; the zone half is read here rather than in the
	// predicate name to keep every phased-out spec in one home.
	"phasedOut": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.PhasedOut && o.Zone == state.ZBattlefield
	},
	// phasedOutOther is the source-relative spelling (The War Doctor's
	// `Permanent.phasedOutOther`): a phased-out battlefield permanent that is
	// not the source itself. Like every bare `Other`, an unbound source
	// (id 0) matches nothing rather than widening to every permanent.
	"phasedOutOther": func(g *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool {
		return src != 0 && o.ID != src && o.PhasedOut && o.Zone == state.ZBattlefield
	},
	"attacking": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool { return o.IsAttacking },
	// unblocked is the CR 509.1h "attacking creature ... with no creatures
	// blocking it" predicate: the object is attacking and no blocker is
	// recorded on it. It is the filter half of ninjutsu's activated cost
	// (K:Ninjutsu's Return<1/Creature.YouCtrl+attacking+unblocked>, the one
	// corpus consumer), and it fails closed for anything not attacking -- a
	// non-attacker is never unblocked, so a blocked or non-attacking
	// creature can neither pay the cost nor match the spec.
	"unblocked": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.IsAttacking && len(o.BlockedBy) == 0
	},
	// attackingYou is the source-relative attacker predicate (Watchdog's and
	// Boarded Window's continuous `Affected$ Creature.attackingYou`, Ice
	// Floe's/Hunting Kavu's/Snow Fortress's `Creature.attackingYou` target
	// specs, and Stalking Leonin's). It reads the defender the combat engine
	// records on state.Object.Attacking at DeclareAttackers, so it stays exact
	// through extra combats. An absent source (a call site that passes 0) or a
	// source that has left the game fails closed, never "every attacker": the
	// widening direction would let a removal spell hit a creature attacking
	// somebody else.
	"attackingYou": func(g *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool {
		if !o.IsAttacking || src == 0 {
			return false
		}
		s := g.Obj(src)
		return s != nil && o.Attacking == s.Controller
	},
	// Mangara/Tomik count attackers at you or your planeswalkers. Attacking
	// and AttackingBattle (state/object.go) distinguish a battle protector
	// from a planeswalker defender; battles must not be counted.
	"attackingYouOrYourPWLKI": func(g *state.Game, o *state.Object, you state.PlayerID, _ state.ObjID) bool {
		if !o.IsAttacking || o.Attacking != you {
			return false
		}
		if o.AttackingBattle == 0 {
			return true
		}
		b := g.Obj(o.AttackingBattle)
		if b == nil || b.Controller != you || b.Face() == nil {
			return false
		}
		f := b.Face()
		return f.IsPlaneswalker() && !f.IsCreature()
	},
	"blocking": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool { return isBlocking(g, o.ID) },
	"token":    func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool { return o.IsToken },
	"Legendary": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return hasType(o, "Legendary")
	},
	// Basic is a supertype used by the common hidden-library ChangeZone
	// filter Land.Basic (Evolving Wilds and the ramp/tutor family).
	"Basic": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return hasType(o, "Basic")
	},
	// Snow is a supertype used as a predicate in Count$Valid specs (Withering
	// Wisps' "Swamp.Snow+YouCtrl"). hasType already sees the Snow type word
	// (Types: Basic Snow Land Swamp), so the predicate is the same shape as
	// Legendary above; without it such a spec failed closed to zero, which is
	// why a computed ActivationLimit of "number of snow Swamps you control"
	// silently became 0.
	"Snow": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return hasType(o, "Snow")
	},
	"nonLand": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool { return !hasType(o, "Land") },
	"nonCreature": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return !hasType(o, "Creature")
	},
	// nonBasic / nonBlack close the two most common non* predicates the
	// transaction target census reads (Wasteland's Land.nonBasic, an
	// Executioner's Capsule's Creature.nonBlack). A basic land carries the
	// "Basic" type word; nonBlack is a colour test, not a type test.
	"nonBasic": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return !hasType(o, "Basic")
	},
	"nonBlack": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return !strings.Contains(ColorsOf(o), "B")
	},
	// IsSuspected is CR 702.157's suspected designation (task alterattr1;
	// Nelly Borca's "goad all suspected creatures", Hot Pursuit's "all
	// goaded and/or suspected creatures", the DBDebuff family's
	// "Creature.OppCtrl+IsSuspected"). It reads the event-backed status the
	// events.AlterAttribute fold maintains; a permanent that left the
	// battlefield or changed controller has already been cleared by those
	// folds, so the predicate cannot read a stale designation.
	"IsSuspected": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.Suspected
	},
	// IsPrepared is CR 722.3a's prepared designation, read from the status
	// maintained by events.AlterAttribute and cleared when the permanent leaves.
	"IsPrepared": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.Prepared
	},
	// harnessed is the Infinity Stone designation set by AlterAttribute.
	"harnessed": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.Harnessed
	},
	// attackedThisCombat uses the event-folded attack stamp and live combat clock.
	"attackedThisCombat": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.AttackedCombat != 0 && o.AttackedTurn == g.Turn && o.AttackedCombat == g.CombatsThisTurn
	},
	// IsSaddled is CR 702.171b's until-end-of-turn designation. The turn
	// stamp makes it expire without a cleanup event and is preserved by
	// controller changes.
	"IsSaddled": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o != nil && o.SaddledTurn == g.Turn
	},
	// IsMonstrous is CR 701.31b's monstrous designation (task
	// agent-20260919T190014Z): the 8 corpus statics keyed on it
	// (`Affected$ Card.Self+IsMonstrous` -- Domesticated Hydra's trample,
	// Fleecemane Lion, Colossus of Akros, ...) grant through the ordinary
	// layer walk, and Polis Crusher's trigger-side `IsPresent$
	// Card.Self+IsMonstrous` intervening-if evaluates through the shared
	// gate. It reads the event-backed status the events.AlterAttribute fold
	// (the Monstrous case) maintains; a permanent that left the battlefield
	// has already been cleared by the Move fold, so the predicate cannot
	// read a stale designation.
	"IsMonstrous": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.Monstrous
	},
	// IsSolved is a Case's solved designation (CR 719.3b): the Case
	// cycle's `IsPresent$ Card.Self+!IsSolved` solve gate and its
	// `Card.Self+IsSolved` "Solved --" statics, triggers and replacements.
	// It reads the event-backed flag the events.AlterAttribute fold sets.
	"IsSolved": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.Solved
	},
	"IsRenowned": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.Renowned
	},
	"kicked": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.CastFlags&state.FlagKicked != 0
	},
	// Bargained is CR 702.166's CastFlags provenance: the object is a spell or
	// permanent whose cast elected the optional additional sacrifice. It is
	// the SAME state.FlagBargained bit the Count$Bargained/Count$Bargain
	// heads, the bare Condition$ Bargain gate and the Spell.Bargain cost
	// constraint read; a copy (never cast) fails closed.
	"bargained": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.CastFlags&state.FlagBargained != 0
	},
	// PromisedGift is Forge's Card.PromisedGift (CR 702.168): the object is a
	// spell or permanent whose cast opted into the Gift keyword's promise.
	// The bit is folded by events.GiftPromise from the cast-flow election and
	// preserved across the stack->battlefield move, so it reads on the spell
	// during resolution (Perch Protection's ConditionPresent$
	// Card.Self+PromisedGift) and on the permanent at its ETB (Kitnap's
	// ConditionPresent$ Card.PromisedGift). Absent a promise it fails closed
	// to false -- a card that never carried the keyword, or a copy (never
	// cast), matches neither the bare nor the '!' form's positive half.
	"PromisedGift": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.CastFlags&state.FlagPromisedGift != 0
	},
	// Teamwork is Forge's Card.Self+Teamwork (CR 702.194b): the object is a
	// spell or permanent whose cast paid the K:Teamwork:N optional additional
	// tap cost. Object.TeamworkPaid is folded by events.Apply from the pay-time
	// FlagTeamworkPaid and survives later CastInfo events and the
	// stack->battlefield move, so it reads on the spell during resolution
	// (Timeline Inquiry's ConditionPresent$ Card.Self+Teamwork) and on the
	// permanent at its ETB. Absent a paid tap it fails closed to false.
	"Teamwork": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.TeamworkPaid
	},
	"surged": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.CastFlags&state.FlagSurged != 0
	},
	// The context-free entry: a caller with no SpecContext compares printed
	// names. matchPositive evaluates NamedCard through namePredicate below
	// before reaching this map, so every context-aware call sees the layer-3
	// name instead -- the same split typePredicate keeps for ExtraTypes.
	"NamedCard": func(g *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool {
		s := g.Obj(src)
		return s != nil && s.ChosenName != "" && sharesName(o, s.ChosenName, NewSpecContext(0, 0))
	},
	"ChosenType": func(g *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool {
		s := g.Obj(src)
		return s != nil && s.ChosenType != "" && hasType(o, s.ChosenType)
	},
	"IsNotChosenType": func(g *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool {
		s := g.Obj(src)
		return s != nil && s.ChosenType != "" && !hasType(o, s.ChosenType)
	},
	// ChosenCtrl is the secretly-chosen-player control predicate (Stalking
	// Leonin's ConditionPresent$ Card.ChosenCtrl, the twin of ChosenType):
	// the object is controlled by the player the source's Secretly$ True
	// ChoosePlayer chose. A source with no chosen player never matches --
	// fail closed, so the condition gate denies instead of widening. This map
	// entry is the census-only classifier (recognisedPredicate/UnknownPredicates
	// consult predicates[]); the context-aware matcher path runs
	// typePredicate first, and both call the one chosenCtrlMatches helper so
	// the two can never diverge.
	"ChosenCtrl": func(g *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool {
		return chosenCtrlMatches(g, o, src)
	},
	// An object records this association in events.Apply when an effect moves
	// it to exile with moveZoneEvent, or in the source's forward ExiledCards
	// list via exiledWithAssociation. Both spellings read the one shared
	// exiledBySource helper; LKI refinements are outside this narrow
	// association.
	"ExiledWithSource": func(g *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool {
		return exiledBySource(g, o, src)
	},
	"ExiledWithSourceLKI": func(g *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool {
		return exiledBySource(g, o, src)
	},
	// escaped is the CastFlags provenance of an escape cast (CR 702.42a): the
	// "sacrifice it unless it escaped" ETB family reads it through
	// Card.Self+escaped (Kroxa, Uro, Phlage), as do the escape-with-counters
	// replacement ValidCard$ specs. A card never escape-cast never matches.
	"escaped": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.CastFlags&state.FlagEscaped != 0
	},
	// sneaked is the CastFlags provenance of a sneak cast (CR 702.190a):
	// "if this creature's sneak cost was paid" reads it through
	// Card.Self+sneaked (Leonardo, Leader in Blue), Card.sneaked (Turncoat
	// Kunoichi), Card.ThisTurnEntered+sneaked (Karai, Future of the Foot) and
	// Count$ValidStack Card.Self+sneaked (The Last Ronin's Technique). The
	// bit is stamped by the pay-time CastInfo (rules/cast.go's modeFlags) and
	// survives the stack->battlefield move, so the spell and the permanent it
	// becomes both read it. A card never sneak-cast never matches, and
	// neither does a stack copy (state.CastProvenanceFlags strips it --
	// CR 707.10). This map entry is the ONE home for the read: every rules
	// and effects match site that carries the token consults it.
	"sneaked": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.CastFlags&state.FlagSneaked != 0
	},
	// Suspend capability and status are intentionally separate. A card has
	// suspend when it is printed with K:Suspend or received the event-backed
	// grant; it is suspended only while that capability card is exiled with a
	// positive TIME counter.
	"withSuspend": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o != nil && (o.SuspendGranted || (o.Face() != nil && o.Face().HasKeyword("Suspend")))
	},
	"withoutSuspend": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o == nil || !(o.SuspendGranted || (o.Face() != nil && o.Face().HasKeyword("Suspend")))
	},
	"suspended": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o != nil && o.Zone == state.ZExile && o.Counter("TIME") > 0 &&
			(o.SuspendGranted || (o.CastFlags&state.FlagSuspend) != 0 || (o.Face() != nil && o.Face().HasKeyword("Suspend")))
	},
	// wasCastFromGraveyard is the CastFlags provenance of a GRAVEYARD-ORIGIN
	// cast (CR 601.2b): any of FlagFlashback, FlagHarmonize or FlagEscaped.
	// The same object-aware read the Count$wasCastFromGraveyard branch head
	// shares (effects/count.go) and its compiled twin mirrors
	// (effects/compiled_predicate.go's predicateTermWasCastFromGraveyard);
	// state.ObjectWasCastFromGraveyard is the one home, so the three cannot
	// disagree. Ash Zealot's "whenever a player casts a spell from a
	// graveyard" ValidCard$ reads it at trigmatch.spellCastMatches time — the deferred
	// cast trigger fires after payCast's CastInfo, so the bit is already
	// stamped — as do River Kelpie's draws and Laquatus's Disdain's counter.
	// A card never so cast never matches, and neither does a stack copy: a
	// copy was put on the stack, never cast (CR 707.10), even though
	// StackCopy leaves the graveyard-origin bits inherited.
	"wasCastFromGraveyard": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return state.ObjectWasCastFromGraveyard(o)
	},
	// notExertedThisTurn is CR 702.100a's offer gate (task exert1): the
	// object has NOT been exerted this turn. The event-backed read is
	// events.Apply's Exert fold (state.Object.ExertedThisTurn). Combat
	// Celebrant's `IsPresent$ Creature.Self+notExertedThisTurn` is the
	// corpus's one carrier; the predicate is a recognised-shape entry (the
	// compiled predicate layer marks an unlisted term `maybe` and falls
	// through to this textual oracle, so no twin term is owed), and
	// UnknownPredicates classifies it through the same predicates map, so
	// the census and the matcher cannot disagree.
	"notExertedThisTurn": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return !o.ExertedThisTurn
	},
	// enlistedThisCombat is CR 702.160's enlist marker (task enlist1): the
	// creature enlisted another creature in the CURRENT combat. The stamp
	// (state.Object.EnlistedTurn/EnlistedCombat, folded by events.Apply's
	// Enlist case) is compared against the live game clock, so a later combat
	// in the same turn -- an extra combat phase -- no longer matches, which
	// is what "this combat" means. Aradesh, the Founder's
	// `Mode$ Attacks | ValidCard$ Creature.YouCtrl+enlistedThisCombat` is the
	// corpus's one carrier; the predicate is a recognised-shape entry (the
	// compiled predicate layer marks an unlisted term `maybe` and falls
	// through to this textual oracle, so no twin term is owed), and
	// UnknownPredicates classifies it through the same predicates map, so
	// the census and the matcher cannot disagree.
	"enlistedThisCombat": func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.EnlistedTurn == g.Turn && o.EnlistedCombat == g.CombatsThisTurn
	},
	// CrewedThisTurn is CR 702.122's crew marker (Forge's
	// Creature.CrewedThisTurn / Card.CrewedThisTurn): the creature crewed the
	// source permanent ("it" -- the Vehicle whose trigger or effect carries
	// the spec) this turn. It is SOURCE-RELATIVE, so it reads the spec's
	// source id: the pairing (state.Object.CrewedVehicles/CrewedTurn, folded
	// by events.Apply's Crew case from the crew cost's tap payment) is
	// compared against the live turn. A missing source (a call site that
	// passes 0) fails closed, never "every crewer": the widening direction
	// would let a Vehicle's trigger target a creature that crewed a different
	// Vehicle. Turtle Van, Getaway Car, Golden Argosy, Leisure Bicycle,
	// Smogbelcher Chariot and Subterranean Schooner are the corpus carriers;
	// the predicate is a recognised-shape entry (the compiled predicate layer
	// marks an unlisted term `maybe` and falls through to this textual oracle,
	// so no twin term is owed), and UnknownPredicates classifies it through
	// the same predicates map, so the census and the matcher cannot disagree.
	"CrewedThisTurn": func(g *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool {
		return pairedWithSourceThisTurn(g, o, src)
	},
	// SaddledThisTurn is CR 702.171's saddler marker (Forge's
	// Creature.SaddledThisTurn: "a creature that saddled it this turn"), the
	// exact twin of CrewedThisTurn: the creature tapped to pay the Mount's
	// Saddle cost this turn. SOURCE-RELATIVE and fail-closed on a missing
	// source for the same reason. events.Apply's Saddle fold records the
	// pairing in the same per-turn list the Crew fold keeps. Calamity,
	// Galloping Inferno, Rambling Possum, Fortune, the Gitrog's ride and
	// Giant Beaver are the corpus carriers.
	"SaddledThisTurn": func(g *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool {
		return pairedWithSourceThisTurn(g, o, src)
	},
	// Permanent is Forge's CardProperty.Permanent (card.isPermanent()): the
	// printed face is a permanent type, in ANY zone (CR 109.2). This is the
	// PREDICATE half of the pair; the bare `Permanent` BASE keeps the
	// on-the-battlefield reading matchesBase gives it (Permanent.YouCtrl,
	// `Affected$ Permanent`, the Count$Valid family all depend on that), and
	// the two per-caller normalizers (effects/permanentCardSpec for Dig
	// windows, rules' targetSpecForZone for target specs) keep rewriting the
	// leading BASE `Permanent` -> `PermanentCard`. The word was previously
	// classified unknown, so `Card.Permanent` and every `<base>.Permanent` /
	// `<base>+Permanent` spelling failed closed and matched NOTHING -- 22
	// corpus carriers (Badlands Revival's return-a-permanent-card, Deadly
	// Brew's ConditionPresent$ gate, Auntie's Sentence's DiscardValid$, Six's
	// retrace grant). isPermanentCard is the same reading the
	// Targeted.Permanent+sameName contextual path already uses; the compiled
	// predicate layer marks the unlisted term `maybe` and falls through to
	// this textual oracle, so no twin term is owed (the
	// notExertedThisTurn entry's contract, above), and UnknownPredicates
	// classifies it through this same map, so census and matcher cannot
	// disagree.
	"Permanent": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return isPermanentCard(o)
	},
}

// colorLetter maps a colour's English name to its WUBRG letter -- note Blue
// is "U", not "B" (col[:1] would collide with Black). Colour predicates
// (and their non<X> negations) read ColorsOf, not the face directly, so a
// Devoid card (effects.ColorsOf) matches no colour predicate, Green included.
var colorLetter = map[string]string{"White": "W", "Blue": "U", "Black": "B", "Red": "R", "Green": "G"}

// objectHasKeyword answers the `with<Keyword>`/`without<Keyword>` filter
// predicates: does this object have keyword k from its printed face OR from a
// CR 122.1b marker counter? A menace counter (Butch DeLoria), a trample
// counter (Owen Grady), and so on grant the keyword through
// cards.CounterKeyword's ONE classifier, so a filter and the engine's own
// HasKeyword cannot disagree about a counter-granted keyword. This does not
// see layer-6 AddKeyword$ grants (a continuous-effect grant needs the engine's
// layer walk, which this predicate has no access to); a counter grant is
// answerable from the object alone.
// ObjectKeywords is one entry of the layer-derived keyword table rules
// publishes for resolving filters (SpecContext.Layers.DerivedKeywords): a
// battlefield object whose current keyword list differs from what its
// printed face and keyword counters give, with that current list.
type ObjectKeywords struct {
	ID       state.ObjID
	Keywords []string
}

// LayerTables groups the board-wide derived-characteristic tables rules
// publishes -- the sparse, escape-safe form of effects.Chars a filter reads
// for objects whose derived characteristics differ from their printed face
// -- held as the ONE field Layers in Ctx, SpecContext and PlayerSpecCtx, so
// every derivation copies them together (a table added here reaches every
// context at once, W1d) and (*Ctx).SpecContext stays within the inlining
// budget (a warm caller-built Ctx must not escape --
// TestEvalCountValidZoneScanIsAllocationFree and rules' warm Derived pin).
type LayerTables struct {
	// EffectiveNames optionally supplies the layer-3 derived names (SetName$,
	// CR 613.1d) in force on the battlefield -- rules' layer walk computes
	// them and binds the result on every SpecContext it builds. Ordinary
	// filter callers leave it nil and fall back to the printed face name.
	// Like ExtraTypes it is a plain value slice and deliberately not a
	// callable resolver: a call made through a SpecContext field makes escape
	// analysis leak the whole context (its Resolve closure included) to the
	// heap on every hot-path construction. It is also NOT a back-pointer into
	// rules: the slice is an immutable snapshot, so a context built from one
	// game can never read another game's board.
	//
	// Entries are only ever the renamed objects (a handful at most, nil on the
	// overwhelmingly common board), so the linear scan is cheaper than
	// building a map.
	EffectiveNames []ObjectName
	// DerivedTypes optionally supplies the layer-4 derived type list (CR
	// 613.1d/613.1c -- AddTypes$, RemoveCardTypes$, AddAllCreatureTypes$, a
	// face-down CR 708.5 set) for objects on the battlefield, keyed by id. It
	// is what makes the ORDINARY filter grammar -- target offer and legality,
	// cost sites, Count$Valid, CantTarget specs -- see a type a continuous
	// effect granted, exactly as ExtraTypes makes the layer walk see it.
	//
	// It is an immutable value slice, deliberately not a callable resolver and
	// never a back-pointer into rules (EffectiveNames' rationale). Entries are
	// only ever the objects whose derived list differs from the printed face
	// (nil on the overwhelmingly common board), so the linear scan is cheaper
	// than building a map.
	DerivedTypes []ObjectTypes
	// StaticGoads is the set of battlefield ids a live Goad$ True static
	// currently goads (printed S: statics and AddStaticAbilities$/
	// StaticAbilities$ granted ones alike), derived by the engine's rules
	// tier and bound by the caller that holds it. The IsGoaded predicate
	// unions it with the event-backed o.Goads list; a nil map keeps the
	// object-alone read (the same no-binding convention Remembered keeps).
	// Immutable data bound per evaluation, never a callable: the same
	// escape-analysis rationale as EffectiveNames.
	StaticGoads map[state.ObjID]bool
	// DerivedColors optionally supplies the layer-5 derived colours (CR
	// 613.1e -- SetColor$, AddColor$, an Animate's Colors$) of the battlefield
	// objects whose colours differ from their printed ones, keyed by id. It
	// is what makes the colour predicates (Black, nonBlack, Colorless,
	// MultiColor, ChosenColor, SharesColorWith) see a colour a continuous
	// effect set. The same immutable value-slice shape as DerivedTypes, for
	// the same reasons; nil on the overwhelmingly common board.
	DerivedColors []ObjectColors
	// DerivedKeywords optionally supplies the layer-derived keyword lists of
	// the battlefield objects whose keywords differ from their printed face
	// (rules' EffectiveKeywords table, published to a resolving Ctx). It is
	// consulted only when ExtraKeywords is unbound: the board-wide twin of
	// that per-candidate bind, the same immutable value-slice shape as
	// DerivedTypes/DerivedColors and nil on the common board.
	DerivedKeywords []ObjectKeywords
}

// keywordPredicateMatches reads bare keyword predicates through keywordInCtx
// and parameterized Landwalk/Enchant predicates from the same derived keyword
// binding, falling back to the printed face where no layer result is bound.
func keywordPredicateMatches(o *state.Object, kp keywordPredicate, sc *SpecContext) bool {
	if !kp.hasParameter {
		return keywordInCtx(o, kp.keyword, sc)
	}
	list := sc.ExtraKeywords
	if list != nil && (o == nil || o.ID != sc.ExtraKeywordsOwner) {
		list = nil
	}
	if list == nil && o != nil {
		for i := range sc.Layers.DerivedKeywords {
			if sc.Layers.DerivedKeywords[i].ID == o.ID {
				list = sc.Layers.DerivedKeywords[i].Keywords
				if list == nil {
					list = []string{}
				}
				break
			}
		}
	}
	if list == nil {
		if o == nil || o.Face() == nil {
			return false
		}
		list = o.Face().Keywords
	}
	for _, keyword := range list {
		head, parameter, hasParameter := strings.Cut(keyword, ":")
		// Enchant keyword parameters may continue with restrictions after the
		// primary type (Enchant:Creature.YouCtrl); filters name that primary
		// type. Landwalk's parameter is a single land type.
		primary, _, _ := strings.Cut(strings.TrimSpace(parameter), ".")
		if hasParameter && strings.EqualFold(strings.TrimSpace(head), kp.keyword) && strings.EqualFold(primary, kp.parameter) {
			return true
		}
	}
	return false
}

// keywordInCtx is THE keyword read of the with<X>/without<X> predicates. A
// caller-bound ExtraKeywords (rules' matchesSpec, the ONE candidate's full
// derived list) is authoritative; otherwise a published DerivedKeywords
// entry for the object (a resolving effect's board-wide table -- Seismic
// Rupture's "each creature without flying" walk sees Ajani's granted
// flying); otherwise the printed face plus keyword counters.
func keywordInCtx(o *state.Object, kw string, sc *SpecContext) bool {
	list := sc.ExtraKeywords
	if list != nil && (o == nil || o.ID != sc.ExtraKeywordsOwner) {
		list = nil
	}
	if list == nil && o != nil {
		for i := range sc.Layers.DerivedKeywords {
			if sc.Layers.DerivedKeywords[i].ID == o.ID {
				list = sc.Layers.DerivedKeywords[i].Keywords
				if list == nil {
					list = []string{}
				}
				break
			}
		}
	}
	if list == nil {
		return objectHasKeyword(o, kw)
	}
	for _, x := range list {
		if strings.EqualFold(cards.KeywordHead(x), kw) {
			return true
		}
	}
	return false
}

// PrintedHasKeyword is the printed-face-plus-keyword-counters keyword read
// the filter falls back to when nothing derived is bound. rules' derived
// keyword table compares against it to publish only the objects whose
// current keywords differ.
func PrintedHasKeyword(o *state.Object, k string) bool { return objectHasKeyword(o, k) }

func objectHasKeyword(o *state.Object, k string) bool {
	if o == nil || o.Face() == nil {
		return false
	}
	if o.Face().HasKeyword(k) {
		return true
	}
	for _, c := range o.Counters {
		if c.N <= 0 {
			continue
		}
		if name, ok := cards.CounterKeyword(c.Kind); ok && strings.EqualFold(cards.KeywordHead(name), k) {
			return true
		}
	}
	return false
}

func init() {
	// kw:Changeling (CR 702.73) is a characteristic-defining type grant, not an
	// effect: it is answered in changelingSubtype below, in every zone, and
	// proved by TestMistformUltimusIsEveryCreatureTypeInEveryZone. It was
	// implemented without being registered, so the coverage ratchet read it as
	// a gap on every changeling carrier.
	RegisterNonAPI("kw:Changeling")

	// Register keyword predicates only for heads the exact keyword reader can
	// answer. Forge's hasKeyword<X> is the exact-keyword spelling of with<X>
	// (CardProperty: card.hasKeyword(X)); keywordPredicateFor and
	// UnknownPredicates share this registry, so unsupported names remain
	// unknown to both matching and census. Keep the recognized vocabulary
	// explicit: InternKeywordHead assigns ids but cannot enumerate valid heads.
	for _, kw := range [...]string{"Flying", "Trample", "Deathtouch", "Lifelink",
		"Vigilance", "Reach", "Haste", "Indestructible", "First Strike", "Double Strike", "Menace",
		"Flanking", "Horsemanship", "Defender", "Foretell", "Shadow", "Doctor's companion",
		"Flash", "Mutate", "Decayed", "Hexproof", "Ward", "Toxic", "Infect",
		"Morph", "Megamorph", "Devoid", "Madness", "Persist", "Phasing", "Unearth",
		"Cascade", "Convoke", "Cycling", "Disturb", "Flashback", "Kicker", "Multikicker",
		"Landwalk", "Enchant"} {
		k := kw
		with := func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
			return objectHasKeyword(o, k)
		}
		predicates["with"+strings.ReplaceAll(k, " ", "")] = with
		predicates["hasKeyword"+strings.ReplaceAll(k, " ", "")] = with
		predicates["without"+strings.ReplaceAll(k, " ", "")] = func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
			return !objectHasKeyword(o, k)
		}
		keywordPredicates["with"+strings.ReplaceAll(k, " ", "")] = keywordPredicate{keyword: k}
		keywordPredicates["hasKeyword"+strings.ReplaceAll(k, " ", "")] = keywordPredicate{keyword: k}
		keywordPredicates["without"+strings.ReplaceAll(k, " ", "")] = keywordPredicate{keyword: k, negated: true}
	}
	// These read ColorsOf, not the face directly, so Devoid (effects.ColorsOf)
	// correctly stops a card from matching any colour predicate, Green included.
	for _, c := range [...]string{"White", "Blue", "Black", "Red", "Green"} {
		letter := colorLetter[c]
		predicates[c] = func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
			return strings.Contains(ColorsOf(o), letter)
		}
	}
	// StrictlyOther is Forge's other spelling of the same "not the source"
	// test Other already implements.
	predicates["StrictlyOther"] = predicates["Other"]
	// StrictlySelf is the mirror spelling of the same source-identity read:
	// the candidate is exactly the spec source object itself, read live. The
	// measured carriers are trigger gates on the source card itself -- 35
	// `IsPresent$ Card.StrictlySelf` (the graveyard-reanimate family's
	// "if it's on the battlefield" presence gate: Animate Dead, Dance of the
	// Dead, Necromancy, Genesis, ...) and 4 `ValidCard$ Card.StrictlySelf` --
	// where the live id read is the whole meaning. The 3
	// `ValidSA$ Spell.ManaFromCard.StrictlySelf` spellcast-provenance
	// carriers stay fail closed on their own unread ManaFromCard word, so
	// this alias cannot reach them.
	predicates["StrictlySelf"] = predicates["Self"]
	// EffectSource is the Effect-delivered spelling of Self: the effect's own
	// source object (Card.EffectSource in a StaticAbilities$ body's
	// ValidCard$). The spec is evaluated with src = the registered effect's
	// source, exactly what Self reads, so the two spellings are aliases. Before
	// this the token was unknown and every such spec matched NOTHING (fail
	// closed) -- the CanAttackDefender grant bodies' dominant shape
	// ("EFFECTSOURCE can attack this turn as though it didn't have defender",
	// 18 corpus carriers) being the case that surfaced it.
	predicates["EffectSource"] = predicates["Self"]
	// ExiledWithEffectSource is the Effect-delivered spelling of the same
	// exiled-by-this-source provenance: the effect's source card is what
	// exiled the candidate (Opposition Agent/Valki-style MayPlay grants name
	// it), the same tracked ExiledWith field ExiledWithSource reads.
	predicates["ExiledWithEffectSource"] = predicates["ExiledWithSource"]
	// EquippedBy / EnchantedBy / AttachedBy: the candidate is the permanent
	// source is attached to (attachedBy below). Task 14 wires all three to the
	// same predicate -- Forge spells "attached to" three ways depending on
	// whether the source is Equipment, an Aura, or a generic script.
	predicates["EquippedBy"] = attachedBy
	predicates["EnchantedBy"] = attachedBy
	predicates["AttachedBy"] = attachedBy
	// FortifiedBy: the same "attached to" relation spelled for Fortifications
	// (CR 702.67) -- the candidate is the land the Fortification source is
	// attached to (C.A.M.P.'s TapsForMana ValidCard$ Card.FortifiedBy,
	// Darksteel Garrison's Affected$/ValidCard$ Land.FortifiedBy). The source
	// side is spelled through the identical AttachedTo field an Equip or Aura
	// ride sets, so attachedBy serves all four spellings.
	predicates["FortifiedBy"] = attachedBy
	// CanEnchantEquippedBy: the candidate card could legally be attached to
	// the creature the resolving source attaches to -- Mantle of the
	// Ancients' "return ... Aura and/or Equipment cards that could be
	// attached to enchanted creature" (ValidTgts$
	// Aura.CanEnchantEquippedBy+YouOwn,Equipment.CanEnchantEquippedBy+YouOwn)
	// and Holy Avenger's "put an Aura card from your hand onto the
	// battlefield attached to it" (ChangeType$ Aura.CanEnchantEquippedBy),
	// the two corpus carriers. The referent creature is the source itself
	// when the source is a creature (Holy Avenger's equipped creature fires
	// the trigger), else the permanent the source is attached to (Mantle's
	// bearer). An Aura candidate matches when the bearer still satisfies the
	// candidate's K:Enchant spec -- the same test the CR 704.5m SBA runs
	// (rules/attach.go auraStillMatchesEnchant); an Equipment candidate when
	// the bearer is a creature (CR 704.5n); anything else admits nothing. A
	// source with no referent (gone, or an unattached non-creature) and an
	// Enchant spec this filter cannot evaluate both fail closed inside the
	// filter, never over-offering an attachment the SBA would just sweep.
	predicates["CanEnchantEquippedBy"] = canEnchantEquippedBy
	// equipped / enchanted: the IS-side counterpart of the pair above -- the
	// candidate itself carries the attachment. Auriok Steelshaper's IsPresent$
	// Card.Self+equipped ("as long as CARDNAME is equipped") reads the first;
	// the corpus also spells the Aura case +enchanted (21 files carrying a
	// bare +enchanted, e.g. Krond the Dawn-Clad's IsPresent$
	// Card.Self+enchanted). The state the attach path maintains: some
	// battlefield permanent whose face carries the kind's type word names the
	// candidate in its AttachedTo.
	predicates["equipped"] = func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return hasAttachmentOfKind(g, o.ID, "Equipment")
	}
	predicates["enchanted"] = func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return hasAttachmentOfKind(g, o.ID, "Aura")
	}
	predicates["Enchanted"] = predicates["enchanted"]
	// Attached: Forge's bare `Attached` CardProperty -- the candidate is
	// itself attached to a permanent. It is the IS-side twin of the
	// two-token wordAttachedTo below and the ONE-token form of the same
	// relation: read the candidate's OWN AttachedTo (never the source's,
	// which is attachedBy/AttachedBy's meaning), requiring the bearer to
	// still be a live object. The corpus spells it only as a filter
	// predicate (Count$Valid Equipment.Attached / Aura.Attached, the
	// ChangeType$ and ValidCards$ families: 22 files), where the base type
	// word does the object-kind narrowing. An unattached object matches
	// nothing; a bearer that has left is not an attachment.
	predicates["Attached"] = func(g *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return objectIsAttached(g, o)
	}
	// modified: Forge's CardProperty.modified (CR 700.9) -- a permanent is
	// modified if it has one or more counters on it, is equipped, or is
	// enchanted by an Aura its controller controls. This is the gate the
	// attacks-trigger family carrying `ValidCard$ Creature.modified+YouCtrl`
	// (Arna Kennerüd, Skycaptain; Kodama of the West Tree; Akki Battle Squad;
	// Kami of Celebration; 35 corpus files) needs; before it, the unknown
	// token failed closed, so no such trigger ever fired and every
	// `Count$Valid ...modified...` read 0. The predicate is
	// player-INDEPENDENT: CR 700.9 reads the candidate permanent's OWN
	// controller for the Aura clause, and every corpus carrier adds a
	// separate `+YouCtrl` token to pick the seat, so no player argument is
	// folded in here. A recognised-shape entry: the compiled predicate layer
	// marks an unlisted term `maybe` and falls through to this textual oracle,
	// so no twin term is owed, and UnknownPredicates classifies it through
	// this same map, so census and matcher cannot disagree.
	predicates["modified"] = modifiedPermanent
	// NoAbilities: Forge's Card.hasNoAbilities (and the filter spelling
	// `Creature.NoAbilities`), the CR 113.12 ruling directly on point for
	// Muraganda Petroglyphs -- an Aura that grants flying stops the +2/+2,
	// while one that only says the creature "is red" does not. The five
	// corpus carriers are Fang-Druid Summoner (ETB search), Muraganda
	// Petroglyphs (continuous buff), Ruxa, Patient Professor (buff and
	// reanimation), Rise from the Wreck (graveyard target) and Jasmine Boreal
	// of the Seven (CantBlockBy, RestrictValid and the negated
	// `Creature.!NoAbilities` blocker clause). A recognised-shape map entry:
	// the compiled predicate layer falls through to this textual oracle and
	// UnknownPredicates classifies it through the same map, so census and
	// matcher cannot disagree.
	predicates["NoAbilities"] = noAbilitiesPermanent
	// Soulbond's "PairedWith" and "Paired" predicates (CR 702.103): the
	// Affected$ spec `Creature.PairedWith` names the creature a source is
	// paired with, and `Creature.Self+Paired` names the source itself when it
	// is paired. PairedWith reads source.Paired (the source is the effect's
	// own permanent).
	predicates["Paired"] = func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.Paired != 0
	}
	predicates["PairedWith"] = func(g *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool {
		s := g.Obj(src)
		return s != nil && s.Paired == o.ID && o.Zone == state.ZBattlefield
	}
	// withSoulbond composes with PairedWith: inspect the selected candidate's
	// keyword, not the source permanent's keyword.
	predicates["withSoulbond"] = func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return objectHasKeyword(o, "Soulbond")
	}
	keywordPredicates["withSoulbond"] = keywordPredicate{keyword: "Soulbond"}
}

// spellIsTargetingArg parses one `IsTargeting <target-spec>` body (the
// `IsTargeting ` prefix is present; the base -- Spell, SpellAbility -- has
// already been cut by the alternative walk), returning the argument. ok is
// false for every body that is not this shape, and for the UNSUPPORTED shapes
// inside it: an empty argument, and an argument carrying a '+'. The candidate
// grammar splits a '+' into separate predicates of the CANDIDATE's chain; an
// `IsTargeting` argument, however, is a TARGET spec whose own '+' conjunction
// belongs to the target match, and the two cannot be told apart from a single
// token (a bare `Valid Permanent+Other` reaches the walk as `IsTargeting Valid
// Permanent` plus `Other`, which applies `Other` to the candidate spell, not
// the targeted permanent). So an argument carrying a '+' is rejected WHOLE at
// the alternative level (spellIsTargetingAlt) and never read as its truncated
// head; Forge spells the common conjunction `~Other`, which the evaluator
// rewrites itself (spellIsTargetingInner). The matcher and the
// UnknownPredicates census share this one parser, so the two cannot disagree.
func spellIsTargetingArg(p string) (arg string, ok bool) {
	arg, ok = strings.CutPrefix(p, "IsTargeting ")
	if !ok {
		return "", false
	}
	arg = strings.TrimSpace(arg)
	if arg == "" || strings.Contains(arg, "+") {
		return "", false
	}
	return arg, true
}

// spellIsTargetingShape reports whether one filter alternative (its base and
// its entire remainder before any '+' split) is Forge's base-qualified
// `Spell.IsTargeting ...` form, or the SpellAbility spelling, optionally under
// a leading '!' negation. It is the shape test the matcher, the compiled
// compiler and the UnknownPredicates census share, so all three agree on which
// alternatives are this form and which are ordinary candidate predicates. The
// base is PART of the form: only Spell and SpellAbility qualify, so a bare
// `IsTargeting` token reached under any other base is not this predicate.
// body is the remainder after the base and an optional '!'; ok is false for an
// alternative that is not this form at all. It does NOT require the argument to
// be answerable -- see spellIsTargetingAlt for that.
func spellIsTargetingShape(base, rest string) (body string, neg, ok bool) {
	if base != "Spell" && base != "SpellAbility" {
		return "", false, false
	}
	body = rest
	if b, has := strings.CutPrefix(body, "!"); has {
		neg = true
		body = b
	}
	if !strings.HasPrefix(body, "IsTargeting") {
		return "", false, false
	}
	return body, neg, true
}

// spellIsTargetingAlt is spellIsTargetingShape plus the argument parse: ok is
// true only for the COMPLETE, answerable spelling (an `IsTargeting ` body with
// a non-empty argument carrying no '+'). Every other shape of the form -- an
// empty argument, a '+' the candidate grammar would split, a malformed ValidX,
// an unknown inner predicate (all judged by spellIsTargetingArgRecognised) --
// fails the WHOLE alternative closed, never a truncated partial evaluation.
func spellIsTargetingAlt(base, rest string) (arg string, neg, ok bool) {
	body, neg, shape := spellIsTargetingShape(base, rest)
	if !shape {
		return "", false, false
	}
	arg, ok = spellIsTargetingArg(body)
	return arg, neg, ok
}

// spellIsTargetingInner normalises one `IsTargeting` argument into the
// target-spec each of the spell's targets is matched against. It is the ONE
// home of the argument conventions the ConditionPresent$ gate (Shiko and
// Narset Unified, Orvar, the All-Form) established: a `Valid ` prefix names
// Forge's target-validity spec and is stripped; any other `Valid`-prefixed
// spelling (ValidX) is a shape this grammar does not read (ok=false -- the
// caller fails closed); a `~Other` suffix is Forge's "other than <source>"
// idiom, rewritten to the filter grammar's own `+Other` conjunction so the
// exclusion is the matcher's source-relative reading (predicates["Other"]).
func spellIsTargetingInner(arg string) (spec string, ok bool) {
	if inner, has := strings.CutPrefix(arg, "Valid "); has {
		spec = strings.TrimSpace(inner)
	} else if strings.HasPrefix(arg, "Valid") {
		// A malformed `ValidX` is not a shape this evaluator reads.
		return "", false
	} else {
		spec = arg
	}
	// `Self` and `Other` are entries in the `predicates` map -- they are
	// source-relative predicate words, not filter BASES -- so a bare (or
	// `Valid `-prefixed) argument naming one is normalised here to the
	// base-qualified spelling the target matcher reads, exactly the
	// `Card.Self`/`Card.Other` spelling the rest of the filter grammar uses.
	// Without this the whole bare word is read as a base, matchesBase finds no
	// such base, and the argument silently reports "does not target" -- and
	// worse, `!`-negating that false result answers a question the build cannot
	// actually read. The normalisation is the ONE home the compiled matcher
	// (matchesObjectText), the zone matcher and the census all share, so they
	// stay in agreement.
	if spec == "Self" || spec == "Other" {
		spec = "Card." + spec
	}
	if inner, has := strings.CutSuffix(spec, "~Other"); has {
		spec = strings.TrimSpace(inner) + "+Other"
	}
	return spec, true
}

// spellIsTargetingArgRecognised reports whether an `IsTargeting` argument is
// a COMPLETE form this build can answer: its normalised target-spec carries
// no unknown predicate. Recognition is reached by the same path the evaluator
// takes -- the alternative-level matcher (spellIsTargetingAlt) uses the same
// inner normalisation and the same target matchers -- so a spec the target
// matcher itself would fail closed on stays unknown to the census, never
// recognised-but-false.
func spellIsTargetingArgRecognised(arg string) bool {
	spec, ok := spellIsTargetingInner(arg)
	return ok && len(UnknownPredicates(spec)) == 0
}

// spellIsTargetingMatches evaluates one normalised `IsTargeting` target-spec
// against a candidate stack spell or ability: met when ANY of the spell's
// recorded targets (state.Object.Targets -- the same chosen-target provenance
// the engine's target offers record, or SpecContext.ProposedTargets for a
// spell still being announced) matches the spec. Object targets go
// through MatchesSpecCtx and player targets through MatchesPlayerSpecCtx --
// the two matchers the rest of the filter grammar uses -- with the caller's
// SpecContext binding You/Source, so YouCtrl/Self/Other clauses and the
// `~Other`-rewritten +Other resolve against the same perspective the rest of
// the spec sees. An untargeted spell (an empty list) is a resolved non-match,
// never an unresolved gate; a target record with no object behind it is
// skipped, not a match. The pointer form is the compiled hot path's entry
// (SpecContext is large and must not be copied per candidate); the value form
// is the convenience wrapper the text path and the condition gate share.
func spellIsTargetingMatches(g *state.Game, spec string, o *state.Object, sc SpecContext) bool {
	return spellIsTargetingMatchesPtr(g, spec, o, &sc)
}

func spellIsTargetingMatchesPtr(g *state.Game, spec string, o *state.Object, sc *SpecContext) bool {
	if o == nil || sc == nil {
		return false
	}
	targets := o.Targets
	if sc.ProposedTargets != nil {
		targets = sc.ProposedTargets
	}
	for _, tgt := range targets {
		if tgt.IsPlayer {
			if MatchesPlayerSpecCtx(g, spec, tgt.Player, sc.You, PlayerSpecCtx{Source: sc.Source}) {
				return true
			}
			continue
		}
		if tgt.Obj == 0 {
			continue
		}
		if MatchesSpecCtx(g, spec, tgt.Obj, *sc) {
			return true
		}
	}
	return false
}

// sharesColorBareReferent reports whether a `SharesColorWith ` argument is
// one of the bare one-word referents the token grammar binds. It is the one
// recognition point positiveRecognised (the census) and specialPositiveToken
// (the compiled dispatch) share, so a bare referent is either recognised by
// both or unknown to both. Two referents are bound: TriggeredProduced
// (C.A.M.P.'s mana set) and ChosenCard (Guard Dogs' chosen permanent).
//
// ChosenCard was held back until the sub-target-before-gate ordering landed
// (task agent-20261001T030154Z-694d075b's stop rule): its carrier's
// ConditionDefined$ Targeted gate is evaluated by effects.conditionMet BEFORE
// the DB sub's own mid-resolution target ask is posed, so on the empty group
// a recognised predicate resolved the gate false and the whole sub -- target
// ask included -- was skipped. That ordering is now fixed in conditionMet's
// Targeted branch (an empty group on an SA that carries ValidTgts$ and whose
// ask has not run is UNRESOLVED, so Resolve dispatches the sub and its ask
// is posed there).
func sharesColorBareReferent(arg string) bool {
	return arg == "TriggeredProduced" || arg == "ChosenCard"
}

// sharesColorShape reports whether an alternative's rest is Forge's
// base-qualified `SharesColorWith Valid <spec>` unit form: the whole
// argument after `Valid ` is a colour-share spec, so the alternative is ONE
// unit and the argument's '+' conjunctions belong to the INNER spec, never
// the candidate predicates (the Spell.IsTargeting precedent -- the corpus's
// `Card.SharesColorWith Valid Creature.Legendary+YouCtrl` would otherwise be
// torn into a recognised head and an orphaned YouCtrl). The bare one-word
// referent (TriggeredProduced) is a predicate token, not this unit. The '!'-negated spelling is NOT part of the unit form: it stays an
// unknown predicate, exactly today's fail-closed read (Invoke Prejudice).
func sharesColorShape(rest string) (arg string, ok bool) {
	arg, ok = strings.CutPrefix(rest, "SharesColorWith Valid ")
	if !ok || strings.TrimSpace(arg) == "" {
		return "", false
	}
	return strings.TrimSpace(arg), true
}

// sharesColorArgRecognised reports whether a `SharesColorWith Valid <spec>`
// unit argument is a COMPLETE form this build can answer: its inner spec
// carries no unknown predicate. Recognition is reached by the same path the
// evaluator takes -- the alternative-level matcher uses this same check -- so
// an inner spec the ordinary matcher would fail closed on stays unknown to
// UnknownPredicates, never recognised-but-false.
func sharesColorArgRecognised(arg string) bool {
	return len(UnknownPredicates(arg)) == 0
}

// sharesColorUnitMatches evaluates one `SharesColorWith Valid <spec>`
// alternative's unit: met when the candidate object's colours (ColorMaskOf,
// the same read every colour predicate takes) intersect the colours of at
// least one BATTLEFIELD object the inner spec admits, matched from the
// resolving context's own perspective (sc.You/sc.Source bound, so YouCtrl and
// Self resolve as the rest of the spec would). An empty match set is "shares
// with nothing", a resolved non-match, never an unresolved gate.
func sharesColorUnitMatches(g *state.Game, spec string, o *state.Object, sc SpecContext) bool {
	if o == nil {
		return false
	}
	cand := colorMaskCtx(o, &sc)
	for i := range g.Objs {
		m := &g.Objs[i]
		if m.Zone != state.ZBattlefield || !MatchesSpecCtx(g, spec, m.ID, sc) {
			continue
		}
		if colorMaskCtx(m, &sc)&cand != 0 {
			return true
		}
	}
	return false
}

// sharesColorWithChosenMatches reports whether the candidate o shares a
// colour with any card the resolution's choose recorded. The chosen set is
// resolved exactly as ChosenTargets/resolutionChosenCards resolve it (the
// SAME set the ordinary Card.ChosenCard predicate and the ConditionDefined$
// ChosenCard group read): the in-flight choice the SpecContext carries
// (sc.Chosen/sc.ChosenValid, seeded by (*Ctx).SpecContext from Ctx.Chosen),
// else the source object's EVENT-BACKED chosen list (state.Object.Chosen,
// the Choose "chosen" fold choiceRecord emits). The event-backed fallback is
// load-bearing for a carrier whose resolution has no in-flight Ctx.Chosen
// bound (Guard Dogs' DBPrevent) and must read the source's fold. An
// unbound choose is a resolved non-match -- never an unresolved gate -- the
// same convention the empty TriggerMana set takes. The colour read is the
// shared ColorMaskOf the ordinary colour predicates and sharesColorUnitMatches
// both take.
func sharesColorWithChosenMatches(g *state.Game, o *state.Object, sc SpecContext) bool {
	chosen := sc.Chosen
	if !sc.ChosenValid && len(chosen) == 0 {
		chosen = ChosenTargetsFrom(g, sc.Source)
	}
	if len(chosen) == 0 {
		return false
	}
	cand := colorMaskCtx(o, &sc)
	for _, t := range chosen {
		if t.IsPlayer {
			continue
		}
		c := g.Obj(t.Obj)
		if c != nil && colorMaskCtx(c, &sc)&cand != 0 {
			return true
		}
	}
	return false
}

// triggerManaColorMask maps the fixed-order WUBRGC TriggerMana set-string
// (rules/mana_activation.go's manaProducedSince stamp) to a colour mask. C
// (colourless) is not a colour and contributes nothing -- colorless mana
// shares no colour with anything.
func triggerManaColorMask(mana string) ColorMask {
	var m ColorMask
	for i := 0; i < len(mana); i++ {
		m |= colorBit(mana[i])
	}
	return m
}

// matchSharesColorWith evaluates one bare-referent `SharesColorWith
// <referent>` predicate token against the candidate o. Two referent forms
// are bound:
//
//	TriggeredProduced -- C.A.M.P.'s "If that creature shares a color with the
//	mana that land produced": the candidate's colours intersect the
//	fixed-order WUBRGC TriggerMana set the TapsForMana trigger captured
//	(effects/trigger_referents.go), on the resolving context's SpecContext
//	through (*Ctx).SpecContext. Colourless (C) is not a colour, so it
//	contributes nothing; an EMPTY set -- the window produced no coloured
//	mana, or the referent is read outside a stamped trigger context -- is
//	"shares nothing", a resolved false, never an unresolved gate.
//
//	ChosenCard -- Guard Dogs' "shares a color with that permanent": the
//	candidate's colours intersect the colours of any card the resolution's
//	choose recorded (SpecContext.Chosen, sharesColorWithChosenMatches). An
//	unbound choose is a resolved non-match, the same convention
//	TriggeredProduced's empty set takes.
//
//	Every other spelling -- MostProminentColor, Imprinted, Equipped,
//	TopOfLibrary, Sacrificed, LastCastThisTurn, the bare form, and the
//	`Valid <spec>` unit (alternative-level,
//	sharesColorShape) -- returns ok=false here, so a token carrying one fails
//	closed exactly as it did before these referents were bound.
func matchSharesColorWith(g *state.Game, p string, o *state.Object, sc SpecContext) (result, ok bool) {
	arg, has := strings.CutPrefix(p, "SharesColorWith ")
	if !has || !sharesColorBareReferent(arg) {
		return false, false
	}
	if arg == "ChosenCard" {
		return sharesColorWithChosenMatches(g, o, sc), true
	}
	return ColorMaskOf(o)&triggerManaColorMask(sc.TriggerMana) != 0, true
}

// positiveRecognised reports whether a predicate token p is a recognised
// positive-evaluation shape: an entry in the `predicates` map, a numeric
// <field><CMP><n> predicate, a generic non<X> negation whose <X> is a
// recognised classifier, or a wordPredicate classifier word. It is the single
// recognition source shared by the evaluator (matchPositive) and by
// UnknownPredicates, so the matcher and the census cannot disagree about
// whether a word is recognised. An unrecognised word is "the engine does not
// know", never "true" -- that is the fail-closed contract.
func positiveRecognised(p string) bool {
	if _, ok := keywordPredicateFor(p); ok {
		return true
	}
	// IsGoaded is evaluated by matchPositive against SpecContext (the map's
	// legacy predFn signature carries no SpecContext), so it is listed here
	// like IsRemembered/EffectSource to keep the matcher and the
	// UnknownPredicates census in agreement.
	if p == "IsGoaded" {
		return true
	}
	if p == "IsRemembered" || p == "IsTriggerRemembered" || p == "EffectSource" || p == "token$DifferentCardNames" || strings.HasPrefix(p, "greatestPower") {
		// EffectSource is matched by matchPositive against SpecContext.Source;
		// listing it here keeps the matcher and the UnknownPredicates census
		// (both driven by positiveRecognised) in agreement.
		return true
	}
	// Forge's extreme-mana-value properties: greatestCMC_<prop>[ControlledBy
	// <ref>] and the bare lowestCMC. Recognised here so the matcher and the
	// census cannot disagree about the family (the classifier is shared).
	if strings.HasPrefix(p, "greatestCMC_") || strings.HasPrefix(p, "lowestCMC") {
		return true
	}
	if p == "TriggeredNewCard" || p == "TriggeredCard" || strings.HasPrefix(p, "ChosenMode") && len(p) > len("ChosenMode") {
		return true
	}
	// Forge's base-qualified `SharesColorWith <referent>` predicate (C.A.M.P.'s
	// TriggeredProduced, Guard Dogs' ChosenCard). The `SharesColorWith Valid
	// <spec>` spelling is recognised at the ALTERNATIVE level, sharesColorShape
	// (the IsTargeting precedent). Every other spelling (MostProminentColor,
	// Imprinted, Equipped, TopOfLibrary, Sacrificed, LastCastThisTurn, the bare
	// form) stays unrecognised: those carriers keep
	// today's fail-open/closed behaviour, and UnknownPredicates keeps reporting
	// them.
	if arg, has := strings.CutPrefix(p, "SharesColorWith "); has {
		return sharesColorBareReferent(arg)
	}
	if hasAbilityToken(p) {
		return true
	}
	if positiveRecognisedWord(p) {
		return true
	}
	return false
}

// positiveRecognisedWord is the wordPredicate-driven half of
// positiveRecognised: a recognised classifier word (map predicate, numeric
// predicate, generic non<X> negation, or wordPredicate word).
func positiveRecognisedWord(p string) bool {
	if p == "ChosenCard" || p == "ChosenCardStrict" || p == "nonChosenCard" || p == "RememberedPlayerCtrl" || p == "CanBeTargetedByTriggeredSpellAbility" {
		return true
	}
	if _, _, ok := controlReferent(p); ok {
		return true
	}
	if _, ok := predicates[p]; ok {
		return true
	}
	if _, ok := numericPred(p, nil, &state.Object{}, NewSpecContext(0, 0)); ok {
		return true
	}
	if _, _, ok := nonPredicate(p); ok {
		return true
	}
	if kind, _ := wordPredicate(p); kind != wordUnknown {
		return true
	}
	return false
}

// recognisedPredicate reports whether a predicate token p is recognised by
// this build at all, including a leading '!'. A !<X> is recognised exactly
// when <X> is a recognised positive-evaluation predicate, by the same
// resolution the positive path uses. A second '!' (!!X) is never a
// recognised shape -- the double negation is not part of this grammar, so it
// fails closed like any unknown.
func recognisedPredicate(p string) bool {
	if positiveRecognised(p) {
		return true
	}
	if x, has := strings.CutPrefix(p, "!"); has && x != "" {
		return positiveRecognised(x)
	}
	return false
}

// nameArg normalises a named<Name>/notnamed<Name> argument the way Forge's
// CardProperty does: a card name containing a comma is written with ';' (the
// spec's own ',' is the OR delimiter -- `namedCalim; Djinn Emperor` names
// "Calim, Djinn Emperor"), and '_' stands for a space (`namedAether_Burst`).
func nameArg(p string) string {
	return nameArgNormalizer.Replace(p)
}

// nameArgNormalizer is built once: a Replacer is safe for concurrent use.
var nameArgNormalizer = strings.NewReplacer(";", ",", "_", " ")

// filterAlternatives splits the OR grammar without tearing a raw comma out of
// a named<Name>/notnamed<Name> argument. Forge normally spells a name comma
// as ';', but real scripts also carry e.g. Card.namedKorlash, Heir to
// Blackblade in Grandeur costs. A comma remains part of that name unless its
// right side begins a syntactic filter alternative (Card.namedX,Creature...;
// both the dotted and bare-base forms are recognised). Keeping the splitter
// shared means matching, quality classification, and the unknown-predicate
// census all parse the same filter.
func filterAlternatives(spec string) iter.Seq[string] {
	return func(yield func(string) bool) {
		start := 0
		for i := 0; i < len(spec); i++ {
			if spec[i] != ',' || rawNameComma(spec[start:i], spec[i+1:]) {
				continue
			}
			if !yield(spec[start:i]) {
				return
			}
			start = i + 1
		}
		yield(spec[start:])
	}
}

// FilterAlternatives exposes filterAlternatives to rules (the one package
// above effects): rules-side spec rewriting (the cast-provenance qualifier
// split, task castprov1) must split alternatives EXACTLY as the filter does,
// so the two cannot disagree about where a comma is a boundary.
func FilterAlternatives(spec string) iter.Seq[string] { return filterAlternatives(spec) }

// SpecReadsKeywords reports whether the spec consults the walk's derived
// KEYWORD list when a caller binds SpecContext.ExtraKeywords -- i.e. whether
// applying another layer-6 effect could change what the spec matches (the
// CR 613.8 dependency test). Exactly two shapes read it: a
// `with<Keyword>`/`without<Keyword>` predicate (keywordPredicates, matched
// by the same exact token lookup the evaluator uses) and the Affinity base,
// whose context-aware match reads ExtraKeywords directly. rules' layer walk
// (cli-20260923T060000Z-layers-dep613) uses this to detect CR 613.6
// dependencies among layer-6 effects without paying for a full spec match
// per candidate pair; a spec that reads no keyword can never gain or lose
// its match to a keyword grant, so it always keeps timestamp order.
//
// The answer is a pure function of the spec, cached in a direct-mapped front
// keyed by the string's data pointer and length (specFront's pattern): the
// layer walk asks it for every layer-6/7 effect of every derivation.
func SpecReadsKeywords(spec string) bool {
	slot := &readsKeywordsFront[specFrontSlot(spec)&(1<<readsKeywordsBits-1)]
	if ent := slot.Load(); ent != nil && ent.spec == spec {
		if VerifySpecCaches && specReadsKeywords(spec) != ent.reads {
			panic("effects: SpecReadsKeywords front entry disagrees with a recompute: " + spec)
		}
		return ent.reads
	}
	reads := specReadsKeywords(spec)
	slot.Store(&specBoolEntry{spec: spec, reads: reads})
	return reads
}

// VerifySpecCaches makes every hit of the per-spec fronts added for the
// derived walk recompute and panic on a difference. The rules and effects
// test binaries set it.
var VerifySpecCaches bool

// specBoolEntry is one immutable front entry for a boolean spec property.
type specBoolEntry struct {
	spec  string
	reads bool
}

const readsKeywordsBits = 12

var readsKeywordsFront [1 << readsKeywordsBits]atomic.Pointer[specBoolEntry]

// specReadsKeywords is SpecReadsKeywords' uncached body.
func specReadsKeywords(spec string) bool {
	// Fast path: every keyword predicate spells out `with`, and the Affinity
	// base carries `ffinity` (case-insensitive shapes are capitalised in
	// practice, so the lowercase probe stays cheap); anything else is a
	// single reject on the hot walk.
	if !strings.Contains(spec, "with") && !strings.Contains(spec, "ffinity") {
		return false
	}
	for alt := range filterAlternatives(spec) {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		base, rest, _ := strings.Cut(alt, ".")
		if strings.EqualFold(base, "Affinity") {
			return true
		}
		for p := range strings.SplitSeq(rest, "+") {
			if p == "" {
				continue
			}
			// A leading ! negates the predicate in the filter grammar; the
			// keyword dependency still exists because a layer-6 grant can
			// change the result of that predicate.
			p, _ = strings.CutPrefix(p, "!")
			if _, ok := keywordPredicateFor(p); ok {
				return true
			}
		}
	}
	return false
}

// ptNumericField reports whether a predicate name is one of the four P/T
// comparison spellings numericPred evaluates against the candidate's
// layer-derived power/toughness (current and base, CR 613.4). It is exactly
// the field vocabulary numericPred's comparison loop walks, so
// SpecReadsPT's dependency test and the evaluator cannot disagree: a field
// the evaluator reads derived P/T for is a field the dependency test
// reports, and cmc (the one non-P/T field in the evaluator's loop, read off
// the face) is deliberately excluded.
func ptNumericField(name string) bool {
	for _, field := range ptNumericFields {
		if strings.HasPrefix(name, field) {
			return true
		}
	}
	return false
}

// ptNumericFields is the P/T half of numericPred's comparison-field
// vocabulary, in the evaluator's own order. cmc is the evaluator's other
// field and reads the printed mana cost, never a derived characteristic, so
// it does not belong here.
var ptNumericFields = [...]string{"power", "toughness", "basePower", "baseToughness"}

// SpecReadsPT reports whether the spec consults the candidate's layer-derived
// power/toughness when a caller binds SpecContext.DerivedPower /
// DerivedToughness / BasePower / BaseToughness -- i.e. whether binding those
// four values can change what the spec matches. The shapes are
// numericPred's four comparison fields (power/toughness/basePower/
// baseToughness, including the two-characteristic forms like
// powerGTbasePower, matched by ptNumericField).
//
// effects cannot derive P/T itself (rules sits above it), so Count$Valid's
// fold binds the values through rules' FilterDerivedPT bridge. That bridge
// runs a FULL rules layer walk per battlefield candidate, and a
// characteristic-defining ability that counts permanents (Master of
// Etherium's X:Count$Valid Artifact.YouCtrl) makes every candidate's
// derivation run the same count again: the in-progress frame guard stops the
// cycle but not the factorial fan-out, so a spec that reads no P/T must not
// pay for the bind at all. rules' layer walk keeps its unconditional bind
// (there the candidate's P/T is already in hand); only this count-site fold
// consults the dependency test.
func SpecReadsPT(spec string) bool {
	// Fast path: every P/T field begins with "power" or "toughness" (basePower
	// and baseToughness carry the same lowercase tails), so a spec with
	// neither substring can never read one -- one reject on the hot path.
	if !strings.Contains(spec, "ower") && !strings.Contains(spec, "oughness") {
		return false
	}
	for alt := range filterAlternatives(spec) {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		_, rest, _ := strings.Cut(alt, ".")
		for p := range strings.SplitSeq(rest, "+") {
			if p == "" {
				continue
			}
			// A leading ! negates the predicate; the P/T dependency still
			// exists because binding the values can flip the predicate's
			// result (and so the negated result).
			p, _ = strings.CutPrefix(p, "!")
			if ptNumericField(p) {
				return true
			}
		}
	}
	return false
}

// StripPredicateToken removes the EXACT predicate token from ONE filter
// alternative's "+" chain, returning the stripped alternative and whether
// the token was present. The token argument is the exact predicate text to
// remove — "pred" for the positive spelling or "!pred" for the negated one
// (the caller owns the polarity: the cast-provenance split evaluates the two
// spellings as opposite requirements). The token may ride the base's first
// predicate ("Card.wasCastFromYourHandByYou") or a later chain link
// ("Creature.!token+YouCtrl+!wasCastFromYourHandByYou"); both shapes strip
// to the remainder. The base itself (before the first angle-bracket-0 dot)
// is never touched, and an ARGUMENTED spelling of the token
// ("CastSaSource$CardManaCost", "CastSaSource/Plus.2") is a different token
// and is left in place. An alternative that is nothing but the token has no
// base and strips to "" -- the filter then fails closed on it (no corpus
// carrier writes that shape).
func StripPredicateToken(alt, token string) (string, bool) {
	base, preds := splitAltBasePreds(alt)
	if preds == "" {
		return alt, false
	}
	parts := strings.Split(preds, "+")
	out := parts[:0]
	had := false
	for _, p := range parts {
		if p == token {
			had = true
			continue
		}
		out = append(out, p)
	}
	if !had {
		return alt, false
	}
	if len(out) == 0 {
		return base, true
	}
	return base + "." + strings.Join(out, "+"), true
}

// splitAltBasePreds splits one filter alternative at the first
// angle-bracket-depth-0 dot: the base, then the "+" predicate chain
// (possibly empty). A dot inside a named<X.Y>-style argument is not the
// boundary.
func splitAltBasePreds(alt string) (string, string) {
	depth := 0
	for i := 0; i < len(alt); i++ {
		switch alt[i] {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case '.':
			if depth == 0 {
				return alt[:i], alt[i+1:]
			}
		}
	}
	return alt, ""
}

// stripBareCastSaSource removes the exact bare !CastSaSource predicate from
// every comma alternative of a Count$ThisTurnCast_ spec, reporting whether it
// was present anywhere. The bare qualifier is Forge's "other than the spell
// being cast" device (Hotheaded Giant's "unless you've cast another red
// spell this turn", Dream Thief's "another blue spell", Storm Entity's
// "each other spell cast this turn" -- the resolving spell's own PutOnStack
// is unavoidably in the window when an ETB gate reads the count); rules'
// SpellsCastThisTurnMatchingExcluding supplies the exclusion. The ARGUMENTED
// forms (!CastSaSource$CardManaCost, !CastSaSource/Plus.2 -- call_forth_the_
// tempest, thunder_salvo) are different tokens and stay in place, failing
// closed downstream as they always did.
func stripBareCastSaSource(spec string) (string, bool) {
	if !strings.Contains(spec, "CastSaSource") {
		return spec, false
	}
	var b strings.Builder
	first := true
	has := false
	for alt := range filterAlternatives(spec) {
		s1, hadNeg := StripPredicateToken(alt, "!CastSaSource")
		s2, hadPos := StripPredicateToken(s1, "CastSaSource")
		if hadNeg || hadPos {
			has = true
		}
		if !first {
			b.WriteByte(',')
		}
		b.WriteString(s2)
		first = false
	}
	return b.String(), has
}

// stripCastSaSourceAggregate removes the ARGUMENTED !CastSaSource$<Property>
// token (call_forth_the_tempest's `Card.YouCtrl+!CastSaSource$CardManaCost`:
// "damage equal to the total mana value of other spells you've cast this
// turn") from every comma alternative of a Count$ThisTurnCast_ spec,
// returning the stripped spec and the property to AGGREGATE over the
// matching casts instead of counting them one each (the aggregation
// precedent is the zone-count heads' `$<Property>` suffix read). ok is false
// when no alternative carries the token.
func stripCastSaSourceAggregate(spec string) (rest, prop string, ok bool) {
	if !strings.Contains(spec, "!CastSaSource$") {
		return "", "", false
	}
	var b strings.Builder
	first, found := true, false
	for alt := range filterAlternatives(spec) {
		s := alt
		if _, preds := splitAltBasePreds(alt); preds != "" {
			parts := strings.Split(preds, "+")
			out := parts[:0]
			had := false
			for _, p := range parts {
				if strings.HasPrefix(p, "!CastSaSource$") {
					had = true
					if !found {
						prop = strings.TrimPrefix(p, "!CastSaSource$")
					}
					continue
				}
				out = append(out, p)
			}
			if had {
				found = true
				base, _ := splitAltBasePreds(alt)
				if len(out) == 0 {
					s = base
				} else {
					s = base + "." + strings.Join(out, "+")
				}
			}
		}
		if !first {
			b.WriteByte(',')
		}
		b.WriteString(s)
		first = false
	}
	if !found || prop == "" {
		return "", "", false
	}
	return b.String(), prop, true
}

// eachAlternatives recognises Forge's multi-type search grammar:
// "EACH <typeA>[.preds] & <typeB>[.preds] ..." -- one pick of EACH listed
// type (Krosan Verge's "EACH Forest & Plains", Conflux's five Card.<Colour>
// clauses). The prefix is exactly Forge's spelling (the trimmed spec starts
// with "EACH "); the remainder splits on '&' into sub-specs, each an
// ORDINARY filter spec -- dots and '+' predicates intact. No type word, no
// predicate token in this grammar is '&' or contains it, so a flat split is
// the top-level split. An EACH spec matches a candidate when ANY listed
// sub-spec matches it; the per-type one-pick structure lives with the
// hidden-library search (effects/zone.go), which reads the sub-specs in
// order to build its option Groups.
func eachAlternatives(spec string) ([]string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(spec), "EACH ")
	if !ok || strings.TrimSpace(rest) == "" {
		return nil, false
	}
	parts := strings.Split(rest, "&")
	subs := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			// A malformed EACH spec ("A & & B") is not split: it keeps the
			// old whole-string behaviour rather than half-matching.
			return nil, false
		}
		subs = append(subs, p)
	}
	return subs, true
}

// EachTypeGroups partitions an ordered candidate list among the sub-specs of
// an EACH ChangeType spec (eachAlternatives): each candidate joins the FIRST
// sub-spec that matches it, in sub-spec order, so a card matching two listed
// qualities (Conflux's multicolour cards under "EACH Card.White & Card.Blue
// & ...") is offered ONCE, in one Group, and picking it can never block the
// other type's pick. A candidate matching no sub-spec cannot occur when the
// list was filtered by the union matcher (matchesObjectText's EACH arm) but
// is dropped here all the same. Group ti's members keep the candidate list's
// order; a sub-spec with no candidate keeps a nil slot. Deterministic: no
// map iteration reaches the result. The three pickers that pose an EACH ask
// (the hidden-library search, the public-origin hidden pick and the hand
// move -- effects/zone.go) all build their option structure through this one
// helper, so their per-type pick structure cannot drift.
func EachTypeGroups(g *state.Game, subs []string, ids []state.ObjID, sc SpecContext) [][]state.ObjID {
	out := make([][]state.ObjID, len(subs))
	for _, id := range ids {
		for ti, sub := range subs {
			if MatchesSpecCtx(g, sub, id, sc) {
				out[ti] = append(out[ti], id)
				break
			}
		}
	}
	return out
}

// eachStructuredOptions fills d with an EACH ask's per-type option
// structure: one option per candidate of every nonempty group (groups in
// sub-spec order, members in candidate order), the sub-spec's ordinal in
// Option.Group, and returns the achievable pick ceiling -- each group's
// min(perType, size) summed, the largest answer the per-Group cap admits.
// The caller sets d.Max to it (a ceiling the option list cannot exceed) and
// carries perType in Decision.GroupLimit when it is above 1. noLooking hides
// card names (the library search's NoLooking$ gate); kind is the option Kind
// the walker's answer carries.
func eachStructuredOptions(g *state.Game, d *decision.Decision, groups [][]state.ObjID,
	perType int32, noLooking bool, owner state.PlayerID, kind string) int {
	ceiling := 0
	for ti, ids := range groups {
		if len(ids) == 0 || perType == 0 {
			continue
		}
		take := len(ids)
		if int(perType) < take {
			take = int(perType)
		}
		for _, id := range ids {
			name := "a card"
			if o := g.Obj(id); o != nil && o.Face() != nil && !noLooking {
				name = o.Face().Name
			}
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: kind, Label: name, Obj: id, Player: owner, Group: strconv.Itoa(ti)})
		}
		ceiling += take
	}
	return ceiling
}

// rawNameComma reports whether the comma after left belongs to the last
// predicate of the current alternative. Once '+' has started another
// predicate, a name argument is complete and cannot own a following comma.
func rawNameComma(left, right string) bool {
	_, predicates, has := strings.Cut(left, ".")
	if !has {
		return false
	}
	last := predicates[strings.LastIndexByte(predicates, '+')+1:]
	if _, ok := strings.CutPrefix(last, "named"); !ok {
		if _, ok := strings.CutPrefix(last, "notnamed"); !ok {
			return false
		}
	}
	return !startsFilterAlternative(strings.TrimSpace(right))
}

// startsFilterAlternative recognises the base at the start of an alternative,
// independently of the candidate object. The filter grammar permits the
// universal bases and every known type word (with the ordinary non<X> base
// negation); a name continuation such as "Heir to Blackblade" is none of
// those. A card name literally ending in ", Creature" remains intrinsically
// ambiguous with the documented OR grammar and must use Forge's ';' spelling.
func startsFilterAlternative(s string) bool {
	base := s
	if i := strings.IndexAny(base, ".+,"); i >= 0 {
		base = base[:i]
	}
	base = strings.TrimSpace(base)
	if base == "CARDNAME" || base == "Any" || base == "Card" || base == "Permanent" || base == "Spell" || base == "Affinity" {
		return true
	}
	base = strings.TrimPrefix(base, "non")
	return predicateTypeWords[base]
}

// nameCharacteristics returns o's names as a name-comparison sees them --
// Forge Card.sharesNameWith. A SPLIT card away from the stack (and, as this
// engine scopes it, the battlefield, where a selected face stands for the
// permanent) has both halves' names combined (CR 709.4). A transforming DFC has
// only its front-face characteristics in those zones (CR 712.8a), even when
// its retained FaceIdx (events.Move does not reset it) still identifies the
// face it had while transformed. On the battlefield and stack every layout
// uses its selected face. Empty names are omitted; an ability object (Card
// nil) has no name.
// A layer-3 name (SetName$, CR 613.1d) OVERRIDES everything below, split
// halves included, and is handled by sharesName/sharesNameWithObject before
// they reach here -- see hasEffectiveName for why it cannot be folded into
// this function's return value.
func nameCharacteristics(o *state.Object) []string {
	if o == nil || o.Card == nil {
		return nil
	}
	offPlay := o.Zone != state.ZStack && o.Zone != state.ZBattlefield
	if offPlay && o.Card.AlternateMode == "Split" {
		var names []string
		for _, f := range o.Card.Faces {
			if f != nil && f.Name != "" {
				names = append(names, f.Name)
			}
		}
		return names
	}
	f := o.Face()
	if offPlay && len(o.Card.Faces) != 0 {
		f = o.Card.Faces[0]
	}
	if f == nil || f.Name == "" {
		return nil
	}
	return []string{f.Name}
}

// namePredicate evaluates the name-comparison predicates that must read the
// layer-3 effective name rather than the printed face. ok is false for every
// other token, which then falls through to the ordinary predicate map.
func namePredicate(p string, g *state.Game, o *state.Object, sc SpecContext) (bool, bool) {
	if p != "NamedCard" {
		return false, false
	}
	s := g.Obj(sc.Source)
	return s != nil && s.ChosenName != "" && sharesName(o, s.ChosenName, sc), true
}

// sharesName reports whether o's name characteristics include name -- Forge
// Card.sharesNameWith(String). An empty name never matches.
func sharesName(o *state.Object, name string, sc SpecContext) bool {
	if name == "" {
		return false
	}
	// Applicability and timestamp order live in rules' layer walk, which hands
	// the result down as the EffectiveNames value slice; the filter tier never
	// re-derives them (a battlefield scan cannot see Affected$ applicability, a
	// conditional SetName$, or timestamp order between two competing effects).
	// A caller with no rules-supplied context reads the printed name.
	if hasEffectiveName(o, sc) {
		return matchesEffectiveName(o, name, sc)
	}
	for _, n := range nameCharacteristics(o) {
		if n == name {
			return true
		}
	}
	return false
}

// sharesNameWithObject reports whether o and src have at least one name in
// common -- Forge Card.sharesNameWith(Card), which compares the full name
// sets of BOTH cards. A split source in a library or graveyard therefore
// shares a name with a card named for either of its halves (CR 709.4).
func sharesNameWithObject(o, src *state.Object, sc SpecContext) bool {
	if hasEffectiveName(src, sc) {
		// The renamed source has exactly one name characteristic. The bound
		// string is passed straight into sharesName, never returned, so the
		// context's content still does not escape.
		for _, n := range sc.Layers.EffectiveNames {
			if n.ID == src.ID {
				return sharesName(o, n.Name, sc)
			}
		}
		return false
	}
	for _, n := range nameCharacteristics(src) {
		if sharesName(o, n, sc) {
			return true
		}
	}
	return false
}

// matchPositive evaluates a recognised positive-evaluation predicate token p
// to its boolean. ok is false for an unknown token OR an unbound trigger
// referent. The latter remains a recognised grammar shape for the census, but
// cannot be negated into a match when its resolution context is absent.
func matchPositive(g *state.Game, p string, o *state.Object, sc SpecContext) (result, ok bool) {
	if hasAbilityToken(p) {
		return objectHasAbility(o, strings.TrimPrefix(p, "hasAbility ")), true
	}
	if p == "token$DifferentCardNames" {
		// Forge's token$DifferentCardNames set-level qualifier (Sandsteppe
		// War Riders, Gimbal Gremlin Prodigy, Audience with Trostani, Neriv
		// Crackling Vanguard -- "the number of differently named <X> tokens
		// you control"). The distinctness is a COUNT-site read
		// (evalCountBody's Count$Valid walk strips the qualifier and counts
		// distinct face names over the matches); per object the recognised
		// meaning is "is a token", so the matcher and UnknownPredicates
		// agree the qualifier is known and a non-count read of it admits
		// every matching token without the distinctness narrowing.
		return o.IsToken, true
	}
	if mode, has := strings.CutPrefix(p, "ChosenMode"); has && mode != "" {
		// ChosenMode<X> reads the candidate object's event-backed modal
		// announcement. An unresolved choice has no recorded mode and fails
		// closed; the ordinary ! wrapper would invert that result for a negated
		// predicate (no corpus carrier uses !ChosenMode).
		return slices.Contains(o.ChosenModes, mode), true
	}
	if p == "ChosenCard" || p == "ChosenCardStrict" || p == "nonChosenCard" {
		// Forge's ChosenCard and ChosenCardStrict are one predicate for this
		// build: the candidate is (or, under nonChosenCard, is not) one of the
		// resolution's chosen cards (SpecContext.Chosen, seeded from the source
		// object's event-backed choice for a context outside the resolution).
		// The corpus uses ChosenCardStrict 66 files, almost all as a
		// ValidSource$/ValidCard$ gate on a ChooseSource answer (Deflecting
		// Palm's `Card.ChosenCardStrict,Emblem.ChosenCard`), and every carrier
		// means exactly that membership; the non-strict spelling stays the
		// ordinary chosen-list read. An unbound ChosenValid fails closed,
		// including beneath '!' -- the conservative direction this predicate
		// family has always taken.
		if !sc.ChosenValid {
			return false, true
		}
		chosen := false
		for _, t := range sc.Chosen {
			if !t.IsPlayer && t.Obj == o.ID {
				chosen = true
				break
			}
		}
		if p == "nonChosenCard" {
			chosen = !chosen
		}
		return chosen, true
	}
	if _, has := strings.CutPrefix(p, "SharesColorWith "); has {
		// Forge's base-qualified `SharesColorWith <referent>` predicate (C.A.M.P.,
		// Guard Dogs). The `Valid <spec>` spelling is an ALTERNATIVE-level unit
		// (sharesColorShape), so a token carrying it is unknown here by
		// construction; matchSharesColorWith binds the two bare referents and
		// carries their fail-closed conventions.
		return matchSharesColorWith(g, p, o, sc)
	}
	if p == "RememberedPlayerCtrl" {
		// Forge's RememberedPlayerCtrl: controlled by a player this
		// resolution remembers (Price of Progress's "each player ... they
		// control" inside RepeatEach). Resolution-only; with no remembered
		// player there is no binding, so it fails closed even beneath '!'.
		return matchControlReferent(g, o, sc, "ControlledBy", "RememberedPlayer")
	}
	if p == "CanBeTargetedByTriggeredSpellAbility" {
		// Forge's Card.canBeTargetedBy(theTriggeredSpellAbility): the
		// candidate is an object the SPELL whose cast fired the trigger could
		// legally target -- Feather, Radiant Arbiter's ChooseCard pool
		// ("other creatures that spell could target"), the one corpus
		// carrier. The binding is the ctx TriggerCard the cast/activation
		// trigger roles captured (the spell on the stack); with no binding,
		// an off-stack spell, or a spell with no target-declaring SA, it
		// fails closed -- under the ordinary convention a spec that matches
		// nothing, and beneath '!' a recognised shape whose absent binding
		// cannot be negated into a match.
		//
		// The effects tier evaluates the explicit target spec; rules publishes
		// the candidates that pass full target legality as immutable data on
		// SpecContext. This keeps effects below rules without a callable
		// resolver or a rules pointer on state.Game.
		if sc.TriggerCard == 0 {
			return false, false
		}
		spell := g.Obj(sc.TriggerCard)
		if spell == nil || spell.Zone != state.ZStack {
			return false, true
		}
		sa := triggeredSpellTargetSA(spell)
		if sa == nil {
			return false, true
		}
		if !targetableObject(sc.TargetableObjects, o.ID) {
			return false, true
		}
		return MatchesSpecCtx(g, TargetsOf(sa).ValidTgts, o.ID,
			NewSpecContext(spell.Controller, spell.ID)), true
	}
	if p == "TriggeredNewCard" || p == "TriggeredCard" {
		// Forge's bare TriggeredNewCard / TriggeredCard property
		// (CardProperty "the card that triggered this ability") inside an
		// ordinary filter spec -- the "you may exile it" cost idiom's
		// `Cost$ ExileAnyGrave<1/Card.TriggeredNewCard>` (Cavalier of Thorns,
		// Doombot Harbinger, Creeping Chill's TriggeredCard; exg1, 18 corpus
		// carriers). Cost-part specs evaluate through this same grammar, so
		// the binding arrives through SpecContext.TriggerContext: the
		// triggered-cost window binds the resolving ability's trigger
		// context, and the candidate matches exactly the card the triggering
		// event captured. Everywhere else -- an activated ability's offer or
		// ask, a static, a hand-built context -- the zero TriggerCard fails
		// CLOSED (ok=false): the spec matches nothing, never an invented
		// referent.
		if sc.TriggerCard == 0 {
			return false, false
		}
		return o.ID == sc.TriggerCard, true
	}
	if p == "blockingTriggeredAttacker" {
		// Forge's Creature.blockingTriggeredAttacker (She-Hulk,
		// Wallbreaker's blocker count): the candidate is a battlefield
		// creature currently blocking the become-blocked trigger's blocked
		// attacker -- the ctx TriggerCard the per-attacker queue entry
		// carried. Resolution-only: with no TriggerCard binding (no trigger
		// ctx, or a trigger whose batch matched none) it fails closed, even
		// beneath '!'. BlockedBy lives on the ATTACKER (events.Apply's
		// DeclareBlockers case appends the blocker to the attacked
		// permanent), so the read is the triggered attacker's own list -- the
		// same read isBlocking makes, scoped to one attacker instead of any.
		if sc.TriggerCard == 0 {
			return false, true
		}
		if o.Zone != state.ZBattlefield {
			return false, true
		}
		a := g.Obj(sc.TriggerCard)
		if a == nil || a.Zone != state.ZBattlefield {
			return false, true
		}
		for _, b := range a.BlockedBy {
			if b == o.ID {
				return true, true
			}
		}
		return false, true
	}
	if p == "EffectSource" {
		// Forge's Card.EffectSource (CardProperty "EffectSource"): the
		// candidate IS the ability's/effect's own source object. A real
		// corpus class (86 raw occurrences over 79 files, in ValidCard$,
		// ValidTarget$, ValidCreature$, ValidAttacker$, IsPresent$,
		// Affected$, ...), it failed closed before because the spec was
		// parsed as an unknown predicate word. Resolved against the
		// SpecContext's Source, so it works wherever a caller binds one
		// (the layer walk's restriction specs, target offers from a source,
		// the MustAttack requirement matcher); with no source bound it
		// fails closed, exactly as the unknown word did.
		return sc.Source != 0 && o.ID == sc.Source, true
	}
	if p == "IsGoaded" {
		// CR 701.38's goaded condition (Hot Pursuit's
		// "GainControl | AllValid$ Creature.IsGoaded,Creature.IsSuspected",
		// Vengeful Ancestor's ValidCard$ trigger, Bothersome Quasit's
		// CantBlock static). Two bindings UNION, the same shape IsRemembered
		// keeps: the event-backed relationship list o.Goads (expired
		// relationships are pruned by the same folds that prune the list --
		// pruneGoads / expireTurnGoads), and SpecContext.Layers.StaticGoads -- the
		// live Goad$ True static route (a printed Shiny Impetus static and an
		// AddStaticAbilities$/StaticAbilities$ granted one alike) that the
		// engine's rules tier derives on demand and the caller binds. A
		// context with no binding answers the event-backed half only, exactly
		// the pre-staticgoad1 read; a bound context never needs a rules
		// pointer for it (immutable data, the DerivedTypes seam).
		if len(o.Goads) > 0 {
			return true, true
		}
		return sc.Layers.StaticGoads[o.ID], true
	}
	if p == "IsTriggerRemembered" {
		// Only a delayed registration's captured set binds this predicate;
		// the source's persistent Remembered list is unrelated. An absent
		// or empty capture stays unknown even under negation.
		if len(sc.DelayedRemembered) == 0 {
			return false, false
		}
		for _, t := range sc.DelayedRemembered {
			if !t.IsPlayer && t.Obj == o.ID {
				return true, true
			}
		}
		return false, true
	}
	if p == "IsRemembered" {
		// Forge's IsRemembered (CardProperty "IsRemembered" ->
		// source.isRemembered(card)): the candidate is in the remembered list
		// of the resolving ability's source. Two bindings approximate the one
		// Forge list and are UNIONED, both fail-closed to no-match when empty:
		// the resolution's Remembered set (Ctx.Remembered -- what
		// RememberChanged$/RememberChosen$/RememberDiscarded$ and the trigger
		// capture added this walk, the "each card exiled this way" follow-up
		// shape), and the source object's event-backed Remembered (what an
		// earlier resolution remembered durably, Forge's persistent host list).
		for _, t := range sc.Remembered {
			if !t.IsPlayer && t.Obj == o.ID {
				return true, true
			}
		}
		if src := g.Obj(sc.Source); src != nil {
			for _, t := range src.Remembered {
				if !t.IsPlayer && t.Obj == o.ID {
					return true, true
				}
			}
		}
		return false, true
	}
	if rest, has := strings.CutPrefix(p, "greatestPower"); has {
		// Forge's greatestPower[ControlledBy <players>] (CardProperty): the
		// candidate is a battlefield creature controlled by the named players
		// (all battlefield creatures when no ControlledBy suffix is present)
		// whose net power no other creature in that set exceeds -- TIES MATCH,
		// every creature at the maximum is "the greatest". The candidate must
		// itself be in the set (Forge's non-LKI contains check), so a creature
		// not controlled by the named players never matches even if its power
		// is the greatest on the battlefield. Bound callers supply layer-derived
		// power for the entire comparison set; direct calls retain objectPower's
		// printed-plus-counter fallback.
		var players []state.PlayerID
		if ref, is := strings.CutPrefix(rest, "ControlledBy"); is {
			var ok bool
			players, ok = controlReferentPlayers(g, sc, "ControlledBy", strings.TrimSpace(ref))
			if !ok {
				// An unbound referent (no resolution, no remembered player)
				// matches nothing rather than degrading to the uncontrolled
				// whole-battlefield reading.
				return false, true
			}
		}
		if o.Zone != state.ZBattlefield || !hasType(o, "Creature") {
			return false, true
		}
		inSet := len(players) == 0
		for _, p := range players {
			if o.Controller == p {
				inSet = true
			}
		}
		if !inSet {
			return false, true
		}
		mine := objectPowerInContext(o, sc)
		for i := range g.Objs {
			other := &g.Objs[i]
			if other.Zone != state.ZBattlefield || !hasType(other, "Creature") || other.ID == o.ID {
				continue
			}
			if len(players) > 0 {
				controlled := false
				for _, p := range players {
					if other.Controller == p {
						controlled = true
					}
				}
				if !controlled {
					continue
				}
			}
			if objectPowerInContext(other, sc) > mine {
				return false, true
			}
		}
		return true, true
	}
	if rest, has := strings.CutPrefix(p, "greatestCMC_"); has {
		// Forge's greatestCMC_<prop>[ControlledBy <players>] (CardProperty):
		// the candidate is a battlefield card of type <prop> whose mana value
		// no other battlefield <prop> exceeds -- TIES MATCH, every card at
		// the maximum matches. The comparison SET is defined by the <prop>
		// suffix, never by the filter base: Forge takes every battlefield
		// card, filters it to the suffix's type (NonLandPermanent meaning
		// nonland permanents, anything else the named card type), optionally
		// restricts it to the ControlledBy players, keeps the highest-CMC
		// members and requires the candidate to be among them. So a card not
		// of the suffix type never matches even at the global maximum, and a
		// card of the right type matches only if nothing in the (possibly
		// player-restricted) set is dearer.
		//
		// The ControlledBy split is a string cut exactly as Forge's is
		// (prop.contains("ControlledBy") then split), so the corpus's
		// CreatureControlledByRemembered and
		// NonLandPermanentControlledByRemembered forms resolve their set
		// filter to Creature / NonLandPermanent and their players through the
		// same controlReferentPlayers grammar greatestPower uses. A
		// ControlledBy referent this build cannot bind fails closed (matches
		// nobody) rather than degrading to the uncontrolled whole-field read.
		prop := rest
		var players []state.PlayerID
		if i := strings.Index(prop, "ControlledBy"); i >= 0 {
			ref := strings.TrimSpace(prop[i+len("ControlledBy"):])
			prop = prop[:i]
			var ok bool
			players, ok = controlReferentPlayers(g, sc, "ControlledBy", ref)
			if !ok {
				return false, true
			}
		}
		inSet := o.Zone == state.ZBattlefield && cmcSetMember(o, prop)
		if inSet && len(players) > 0 {
			inSet = false
			for _, pl := range players {
				if o.Controller == pl {
					inSet = true
				}
			}
		}
		if !inSet {
			return false, true
		}
		mine := objectManaValue(o)
		for i := range g.Objs {
			other := &g.Objs[i]
			if other.ID == o.ID || other.Zone != state.ZBattlefield || !cmcSetMember(other, prop) {
				continue
			}
			if len(players) > 0 {
				controlled := false
				for _, pl := range players {
					if other.Controller == pl {
						controlled = true
					}
				}
				if !controlled {
					continue
				}
			}
			if objectManaValue(other) > mine {
				return false, true
			}
		}
		return true, true
	}
	if strings.HasPrefix(p, "lowestCMC") {
		// Forge's lowestCMC (CardProperty): the candidate is a battlefield
		// card whose mana value no other NONLAND battlefield card is below --
		// TIES MATCH (the strict = test admits every tied minimum). Forge's
		// lowestCMC carries no _ suffix, so there is no type/player set
		// filter; the only exclusion is lands, which the reminder text states
		// as "target nonland permanent with the lowest mana value" (Culling
		// Scales, the one corpus carrier). Forge also skips immutable cards;
		// that state has no equivalent in this build and no corpus carrier
		// needs it -- recorded as a deliberate narrowing, not approximated.
		// The candidate is compared even when it is a land (Forge has no
		// candidate-in-set check here); every real carrier's base constrains
		// it to a nonland permanent anyway, so no correct target is lost.
		mine := objectManaValue(o)
		for i := range g.Objs {
			other := &g.Objs[i]
			if other.Zone != state.ZBattlefield || hasType(other, "Land") {
				continue
			}
			if objectManaValue(other) < mine {
				return false, true
			}
		}
		return true, true
	}
	if op, ref, recognised := controlReferent(p); recognised {
		return matchControlReferent(g, o, sc, op, ref)
	}
	// Type predicates must use the derived layer-4 type list when rules
	// supplies one. Keep this before the generic predicate map: its legacy
	// functions deliberately remain useful to callers without a SpecContext,
	// but must not bypass the context-aware matcher here.
	if result, ok := typePredicate(p, g, o, sc); ok {
		return result, true
	}
	// Keyword predicates must use the derived keyword list when rules
	// supplies one, for the same reason type predicates use ExtraTypes: the
	// predicate-map functions read the object alone and cannot see a layer-6
	// AddKeyword$ grant. Keep this before the generic predicate map so a
	// context-aware caller never has its bound list bypassed.
	if kp, ok := keywordPredicateFor(p); ok {
		has := keywordPredicateMatches(o, kp, &sc)
		if kp.negated {
			has = !has
		}
		return has, true
	}
	// Name predicates must use the layer-3 derived name when rules supplies
	// one, for the same reason type predicates must use the derived type
	// list: the predicates map's legacy function carries no SpecContext.
	if result, ok := namePredicate(p, g, o, sc); ok {
		return result, true
	}
	// Colour predicates must use the layer-5 derived colours when rules
	// supplies them: the map's legacy colour functions carry no SpecContext,
	// so with a table bound they fall through to the word path below.
	if fn, ok := predicates[p]; ok && !(len(sc.Layers.DerivedColors) != 0 && colourMapPredicate(p)) {
		return fn(g, o, sc.You, sc.Source), true
	}
	if res, ok := numericPred(p, g, o, sc); ok {
		return res, true
	}
	if nkind, nkey, ok := nonPredicate(p); ok {
		// non<X> is the negation of a recognised classifier: the object
		// matches when the positive classifier does not.
		return !wordMatches(nkind, nkey, g, o, sc), true
	}
	if kind, key := wordPredicate(p); kind != wordUnknown {
		// The cast-provenance tokens (wasCastFromYourHandByYou, wasCastByYou,
		// bare wasCastFromYourHand) are recognised by wordPredicate so the
		// CENSUS no longer reports them unknown, but the filter tier itself
		// cannot answer them (the truth is in the event log and the pre-push
		// offer window rules' castProvenanceAdmits reads). Returning ok=false
		// here makes BOTH the positive and the '!'-negated spelling fail
		// closed: a recognised-but-false body would let a NEGATED token match
		// every object in a direct filter call -- recognised-and-inert is
		// worse than unknown for a negation. rules strips every provenance
		// token before the filter runs, so this path is reached only by an
		// unstripped caller, which is not entitled to a provenance answer.
		if kind == wordCastProvenance {
			return false, false
		}
		if kind == wordChosenColor {
			src := g.Obj(sc.Source)
			if src == nil || colourLetter(src.ChosenColor) == 0 {
				return false, false
			}
		}
		// The pc1 context-bound classifiers (NotDefinedTargeted, DefenderCtrl,
		// IsImprinted) are likewise recognised so the census reports them, but
		// their binding may be absent outside a resolution/combat trigger/no
		// source. An unbound one must fail closed for the NEGATED spelling as
		// well, so it returns unknown (ok=false) rather than a false a
		// caller could invert into a match -- the same contract
		// wordCastProvenance keeps.
		if !contextPredicateBound(g, kind, key, sc) {
			return false, false
		}
		return wordMatches(kind, key, g, o, sc), true
	}
	return false, false
}

// matchPredicate evaluates a predicate token in a filter conjunction,
// including the leading-'!' negation. ok is false for an unknown shape or an
// unbound trigger referent, so the caller must fail closed. A !<X> negates the positive evaluation of <X>; when
// <X> is itself not recognised, !<X> is unknown too -- the negation of "I do
// not know" is not "yes".
func matchPredicate(g *state.Game, p string, o *state.Object, sc SpecContext) (result, ok bool) {
	if x, has := strings.CutPrefix(p, "!"); has {
		if x == "" {
			return false, false
		}
		r, rek := matchPositive(g, x, o, sc)
		if !rek {
			return false, false
		}
		return !r, true
	}
	return matchPositive(g, p, o, sc)
}

func isBlocking(g *state.Game, id state.ObjID) bool {
	for i := range g.Objs {
		for _, b := range g.Objs[i].BlockedBy {
			if b == id {
				return true
			}
		}
	}
	return false
}

func matchesBase(g *state.Game, base string, o *state.Object, sc SpecContext) bool {
	if neg := strings.TrimPrefix(base, "non"); neg != base {
		return !matchesBase(g, neg, o, sc)
	}
	if isTargetedCardBase(base) {
		id, bound := targetedCardSelfReferent(sc)
		return bound && o.ID == id
	}
	switch matchesBaseCodes.Code(string(base)) {
	case matchesBaseAny:
		return hasTypeCtx(o, "Creature", sc) || hasTypeCtx(o, "Planeswalker", sc) || hasTypeCtx(o, "Battle", sc)
	case matchesBaseCard:
		return true
	case matchesBasePermanent:
		return o.Zone == state.ZBattlefield
	case matchesBaseAffinity:
		// CR 702.41: Forge uses the keyword name as a filter base for
		// "a permanent with affinity" (Sojourner's Enforcermite), not as
		// a card type. Prefer the layer-derived keyword list when rules has
		// supplied one; the printed face is the effects-tier fallback.
		if sc.ExtraKeywords != nil && o.ID == sc.ExtraKeywordsOwner {
			for _, k := range sc.ExtraKeywords {
				if strings.EqualFold(cards.KeywordHead(k), "Affinity") {
					return true
				}
			}
			return false
		}
		return o.Face() != nil && o.Face().HasKeyword("Affinity")
	case matchesBasePermanentCard:
		// This internal base spelling is selected by rules' target census
		// (targetSpecForZone) and Dig windows (permanentCardSpec) for Forge's
		// `Permanent` base evaluated AWAY from the battlefield, and by rules'
		// SpellCast trigger matcher (trigmatch.SpellCastPermanentSpec) for the permanent
		// SPELL a "cast a permanent spell" trigger evaluates on the stack. A
		// permanent CARD is anything whose printed face is a permanent type
		// (CR 109.2) wherever the object sits; the bare `Permanent` case
		// above keeps the on-the-battlefield reading every other filter
		// depends on.
		return o.Face() != nil && o.Face().IsPermanent()
	case matchesBaseSpell:
		return o.Zone == state.ZStack || sc.AsStack
	case matchesBaseSpellAbility:
		// Forge's SpellAbility base (ValidSource$ SpellAbility.OppCtrl on the
		// "becomes the target of a spell or ability" family -- Thunderbreak
		// Regent and 51 more files): any spell or ability object on the stack.
		// The shared filter draws the Spell/SpellAbility line by zone alone;
		// the card-spell-only distinction TargetType$ Spell draws
		// (rules/stack.go's stack kind tokens) is that machinery's own, not
		// this one's.
		return o.Zone == state.ZStack
	case matchesBaseOutlaw, matchesBaseHistoric:
		return batchWordBase(matchesBaseCodes.Code(string(base)), o, sc)
	}
	return hasTypeCtx(o, base, sc)
}

// batchWordBase dispatches batch-word bases through their shared evaluators.
func batchWordBase(code matchesBaseCode, o *state.Object, sc SpecContext) bool {
	switch code {
	case matchesBaseOutlaw:
		return outlawMatches(o, sc)
	case matchesBaseHistoric:
		return historicMatches(o, sc)
	default:
		return false
	}
}

// SpecContext carries the extra state a filter spec beyond MatchesSpec's
// three plain arguments needs: the perspective seat, the effect's source
// (CARDNAME/Self/Other/StrictlyOther/NamedCard/ChosenType are relative to it), and
// an optional resolver for a numeric predicate whose right-hand side is not a
// literal (an SVar name such as "Y" or "Chosen"). A nil Resolve leaves that
// family of RHS forever unresolvable -- MatchesSpec/MatchesSpecFrom's
// contract -- rather than guessing at what the name might mean.
type SpecContext struct {
	TriggerContext
	You    state.PlayerID
	Source state.ObjID
	// CombatDamageHits is the per-turn combat-to-player ledger for
	// resolution-time predicates that refer to damage dealt by Source.
	CombatDamageHits []CombatDamageHit
	Resolve          func(name string) (int32, bool)
	// PredicatePrograms is an optional immutable compiled-text sidecar. A nil
	// value keeps the textual matcher authoritative for synthetic fixtures and
	// dynamic source strings.
	PredicatePrograms *PredicatePrograms
	// ResolutionTargets are the state.Object.Targets of the spell or ability
	// currently resolving. They are deliberately absent while a target offer is
	// built: Targeted* is self-referential and cannot determine legality before
	// its own targets have been chosen. Resolving distinguishes a real empty
	// target list from no resolving object at all.
	ResolutionTargets []state.Target
	// ParentTargets are the PARENT ability's already-chosen targets, bound
	// (ParentBound) only while the offer for a SubAbility$'s OWN ValidTgts$ is
	// built during the parent's resolution. "Exile up to one target Equipment
	// attached to that creature" (`Equipment.AttachedTo ParentTarget`) names a
	// target chosen BEFORE this one, so unlike the self-referential case
	// ResolutionTargets' doc rules out, the Targeted*/ParentTarget referents
	// are well defined for this offer. Read through TargetBinding only.
	ParentTargets []state.Target
	ParentBound   bool
	// ProposedTargets are the announced-but-not-yet-recorded targets of a spell
	// being cast or a permission being checked (CR 601.2c runs while the
	// announced spell is still in hand, so state.Object.Targets is necessarily
	// empty). When non-nil it is authoritative for the `Spell.IsTargeting`
	// predicate: a target-conditional static (CastWithFlash's ValidSA$, a cost
	// static's ValidSpell$) must read the proposed targets rather than the
	// candidate object's recorded list. Nil keeps state.Object.Targets
	// authoritative (the stack/resolution path); an empty non-nil slice is a
	// proposed list that matches nothing (a resolved non-match, never an
	// unresolved gate).
	ProposedTargets []state.Target
	// AsStack is a DERIVED-CHARACTERISTICS override, not a resolution fact:
	// rules.derivedWith sets it while evaluating an AffectedZone$ Stack grant
	// for the spell a cast is announcing (CR 601.2b runs while the announced
	// spell is still in hand). It makes the wasCast predicate treat the
	// announced spell as the cast spell it is; nothing else reads it, and it
	// is absent from every resolution- and target-time evaluation.
	AsStack bool
	// AsFaceDown is the same class of DERIVED-CHARACTERISTICS override for
	// the faceDown predicate: a Moved replacement's ValidCard$ is evaluated
	// ahead of the Move it intercepts, so the entering object is not yet
	// FaceDown and is still in its origin zone. rules/replacement.go sets it
	// while matching a face-down battlefield entry (manifest/cloak/FaceDown$),
	// making `Creature.faceDown+...` specs match the entry they name. Like
	// AsStack it is absent from every other evaluation.
	AsFaceDown bool
	// ExcludeFromBattlefieldCount is the object that is being evaluated by
	// an Updated battlefield-entry replacement body. Its entry has been
	// folded by then, but CR 614.12 counts the battlefield as it would be
	// immediately before the permanent enters.
	ExcludeFromBattlefieldCount state.ObjID
	// Remembered is the resolving spell or ability's Remembered set (a
	// RepeatEach iteration binds its subject here). Like ResolutionTargets it
	// is meaningful only while Resolving. It is also the Remembered.* base
	// prefix's context referent (contextReferent): Eradicate's
	// `ChangeType$ Remembered.sameName` shares names with the captured card.
	Remembered []state.Target
	// RememberedPlayers is the CONSULTATION-time binding for a registered
	// restriction's captured players (state.ContinuousEffect
	// .RememberedPlayers). Resolution-time callers leave it zero -- their
	// remembered players ride the Remembered targets above -- so its
	// presence never changes a resolution read. rules' block consultation
	// (combat.BlockRestricted) binds it because a static consultation never has
	// Resolving set: without the channel, the registered CantBlockBy body's
	// ValidBlocker$ Creature.RememberedPlayerCtrl clause (The Motherlode,
	// Excavator) would resolve nobody and fail closed.
	RememberedPlayers []state.PlayerID
	// Chosen is the current resolution's selected cards/players. It is used
	// by Forge's ChosenCard/nonChosenCard predicates, not persisted game state.
	Chosen      []state.Target
	ChosenValid bool
	Resolving   bool
	// ManaValue overrides the object's mana value for cmc predicates, with
	// HasManaValue set. It carries the CR 202.3e chosen-X effect: a caller
	// that has the chosen {X} passes the resulting mana value here so a
	// cmc restriction re-checked late (CR 601.2e) sees the spell as it is,
	// not as it was offered. Zero value with HasManaValue false is the
	// ordinary path (the printed cost, X as 0).
	ManaValue    int32
	HasManaValue bool
	// ExtraTypes optionally supplies layer-4-derived types for the ONE object
	// the spec is being matched against -- the layer walk (rules/layers.go's
	// matchesWithTypes) binds the types its effect applications have
	// accumulated so far. Ordinary filter callers leave it nil and fall back
	// to the printed type line (plus Changeling) above. A value slice,
	// deliberately not a callable resolver: a call made through a
	// SpecContext field makes escape analysis leak the whole context (its
	// Resolve closure included) to the heap on every hot-path construction.
	ExtraTypes []string
	// ExtraTypesOwner is the ONE object ExtraTypes describes. Only that object
	// reads the list as authoritative; any other object a predicate inspects
	// (Aura.Other, a Self/Other comparison) keeps the printed-face read.
	ExtraTypesOwner state.ObjID
	// ExtraKeywords optionally supplies the layer-derived KEYWORD list for the
	// ONE object the spec is being matched against (a value slice, same
	// rationale as ExtraTypes). When non-nil it is authoritative for the
	// `with<Keyword>`/`without<Keyword>` predicates: it already holds printed
	// keywords, marker-counter grants and layer-6 AddKeyword$ grants, so a
	// caller with the layer walk in hand can gate on a granted keyword
	// (kw:Flanking's blocker check, Cavalry Master's `withFlanking` lord).
	// nil keeps the object-alone read (printed face plus counters).
	ExtraKeywords []string
	// ExtraKeywordsOwner is the ONE object ExtraKeywords describes. Only that
	// object reads the list as authoritative; any other object a relational
	// predicate inspects keeps the published-table/printed-face read.
	ExtraKeywordsOwner state.ObjID
	// TargetableObjects is the rules tier's immutable snapshot of objects this
	// triggered spell can currently target under full rules legality.
	TargetableObjects []state.ObjID
	// Layers is the board's derived-characteristic tables (LayerTables:
	// layer-3 names, layer-4 types, layer-5 colours, layer-6 keywords, static
	// goads), bound by rules on every context it builds and copied from a
	// resolving Ctx by (*Ctx).SpecContext. The zero value reads the printed
	// face.
	Layers LayerTables
	// DerivedPTs optionally supplies layer-derived current power for all
	// objects participating in a greatestPower comparison. Like DerivedTypes,
	// this is an immutable value table, never a rules back-pointer or resolver.
	DerivedPTs []ObjectPower
	// DerivedPower/DerivedToughness optionally supply the candidate object's
	// CURRENT derived power/toughness (printed plus every applied continuous
	// effect in layer order, then 7d counters) and BasePower/BaseToughness its
	// BASE values through layer 7b (CR 613.4: printed or characteristic-
	// defining, then a set, BEFORE 7c modifies and 7d counters). rules' layer
	// walk binds them from Engine.Derived for the ONE object a spec is matched
	// against (matchesSpec), so the numeric power/basePower predicates read the
	// same values the rest of the engine does. HasDerivedPT / HasBasePT mark
	// the binding present: an unbound context (a direct filter call or the
	// census probe) falls back to the object-alone read, exactly the power
	// predicates' pre-binding behaviour. Plain value fields, deliberately not a
	// callable resolver: the same escape-analysis rationale as ExtraTypes.
	DerivedPower     int32
	DerivedToughness int32
	HasDerivedPT     bool
	BasePower        int32
	BaseToughness    int32
	HasBasePT        bool
}

// ObjectName binds one object to its layer-3 derived name.
type ObjectName struct {
	ID   state.ObjID
	Name string
}

// ResolutionStateBound reports whether this context is bound to resolution
// state, so a filter verdict over it cannot be reproduced from a printed face
// alone and must not be shared with another resolution. (*Ctx).SpecContext
// installs the numeric-RHS Resolve closure for a paid X, an SVar table or a
// published roll; Resolving marks the rest (Remembered, ResolutionTargets,
// Chosen, the layer tables). A context that reports false answers exactly
// what the resolver-free walk would, so a memo may serve it.
func (sc *SpecContext) ResolutionStateBound() bool {
	return sc != nil && (sc.Resolve != nil || sc.Resolving || sc.ParentBound)
}

// TargetBinding is the one read of "the targets the Targeted*/ParentTarget
// referents name": the resolving object's own targets while Resolving, else
// the parent ability's targets while a sub-ability's target offer is built
// (ParentBound), else unbound (ok=false; the predicate fails closed, also
// under '!').
func (sc *SpecContext) TargetBinding() ([]state.Target, bool) {
	switch {
	case sc.Resolving:
		return sc.ResolutionTargets, true
	case sc.ParentBound:
		return sc.ParentTargets, true
	}
	return nil, false
}

// ObjectTypes binds one object to its layer-4 derived type list (CR
// 613.1d/613.1c). The list is the SAME shape rules' layer walk builds and
// Derived carries -- printed types (or the CR 708.5 face-down set) plus every
// granted/removed word -- so the ordinary filter grammar and the layer walk
// cannot disagree about an object's types.
type ObjectTypes struct {
	ID    state.ObjID
	Types []string
	// AllCreatureTypes is the finished layer-4 semantic marker; a false
	// value must not fall back to the object's printed CDA.
	AllCreatureTypes bool
}

// ObjectPower binds one object's layer-derived current power.
type ObjectPower struct {
	ID    state.ObjID
	Power int32
}

// hasEffectiveName reports whether the context binds a layer-3 name for o.
//
// Every read of EffectiveNames answers a BOOLEAN and never returns one of its
// strings to a caller. That is load-bearing: a function that returns a string
// sourced from a SpecContext field makes escape analysis summarise the whole
// context's content as leaking, which forces the caller's *Ctx (and the
// Resolve closure over it) to the heap on every hot-path construction --
// exactly what TestEvalCountValidZoneScanIsAllocationFree pins against.
func hasEffectiveName(o *state.Object, sc SpecContext) bool {
	return hasEffectiveNamePtr(o, &sc)
}

// hasEffectiveNamePtr is hasEffectiveName reading the context through a
// pointer: the hot filter paths (matchesObjectPtr) hold a *SpecContext, and
// dereferencing it into the by-value form copied the whole context per call.
func hasEffectiveNamePtr(o *state.Object, sc *SpecContext) bool {
	if o == nil {
		return false
	}
	for _, n := range sc.Layers.EffectiveNames {
		if n.ID == o.ID {
			return n.Name != ""
		}
	}
	return false
}

// matchesEffectiveName reports whether o's bound layer-3 name is name.
func matchesEffectiveName(o *state.Object, name string, sc SpecContext) bool {
	for _, n := range sc.Layers.EffectiveNames {
		if n.ID == o.ID {
			return n.Name != "" && n.Name == name
		}
	}
	return false
}

// hasDerivedTypeEntryPtr is hasDerivedTypeEntry through a pointer (see
// hasEffectiveNamePtr).
func hasDerivedTypeEntryPtr(o *state.Object, sc *SpecContext) bool {
	_, ok := derivedTypesForPtr(o, sc)
	return ok
}

// derivedTypesForPtr is derivedTypesFor through a pointer (see
// hasEffectiveNamePtr).
func derivedTypesForPtr(o *state.Object, sc *SpecContext) ([]string, bool) {
	if o == nil {
		return nil, false
	}
	for _, d := range sc.Layers.DerivedTypes {
		if d.ID == o.ID {
			return d.Types, true
		}
	}
	return nil, false
}

// triggeredSpellTargetSA derives the target-declaring SA of a stack spell
// object: an ability wrapper's own SA when it carries ValidTgts$ (a
// triggered/activated ability the cast family named), else the face's spell
// ability. A spell whose SA declares no ValidTgts$ (or a modal spell whose
// mode structure this effects-side read cannot see) yields nil -- the
// CanBeTargetedByTriggeredSpellAbility predicate fails closed on it, the
// narrow direction for a choice pool. Never the rules side's modalTargetSA:
// that lives above the effects tier.
func targetableObject(ids []state.ObjID, id state.ObjID) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func triggeredSpellTargetSA(spell *state.Object) *cards.SA {
	if spell == nil {
		return nil
	}
	if spell.Ability != nil {
		if TargetsOf(spell.Ability).Targeted() {
			return spell.Ability
		}
		return nil
	}
	f := spell.Face()
	if f == nil {
		return nil
	}
	sa := f.SpellAbility()
	if sa == nil || !TargetsOf(sa).Targeted() {
		return nil
	}
	return sa
}

// MatchesObjectCtx applies one Forge filter spec to an object VALUE rather
// than to a live-game id -- the same grammar (base type plus .A+B+...
// predicate conjunction, alternatives ORed on ",") MatchesSpecCtx applies to
// `g.Obj(id)`, but with the object handed in, so a caller can match against
// something that is not (or is no longer) reachable by id: the last-known-
// information snapshot a zone-change trigger holds (effects.Ctx.LKI, CR
// 603.10), or a card that has since left the battlefield. An IsCopy object
// that has left the stack (CR 707.10h: a copy that changes zones ceases to
// exist) never matches anything regardless of spec.
func MatchesObjectCtx(g *state.Game, spec string, o *state.Object, sc SpecContext) bool {
	return matchesObjectPtr(g, spec, o, &sc)
}

// matchesObjectPtr is MatchesObjectCtx with the context passed by pointer:
// SpecContext is large, and the per-object hot path must not copy it at
// every call level.
func matchesObjectPtr(g *state.Game, spec string, o *state.Object, sc *SpecContext) bool {
	if o == nil {
		return false
	}
	// A renamed or layer-4-altered object must be answered by the textual
	// oracle: the compiled program path reads the printed face and cannot see
	// EffectiveNames or DerivedTypes, so a compiled `named<X>` or a compiled
	// Goblin type test would silently miss the derived characteristic. The same
	// discipline the layer walk keeps for ExtraTypes (rules/layers.go), scoped
	// here to the one object that actually carries a change.
	cs := compiledSpecFor(spec)
	if ps := sc.PredicatePrograms; ps != nil && !hasEffectiveNamePtr(o, sc) && !hasDerivedTypeEntryPtr(o, sc) &&
		(len(sc.Layers.DerivedColors) == 0 || !hasDerivedColorEntryPtr(o, sc)) {
		switch ps.evaluateCS(cs, spec, g, o, sc) {
		case PredicateYes:
			return true
		case PredicateNo:
			return false
		}
	}
	return compiledMatch(cs, g, o, sc)
}

// matchesObjectText is the original textual filter evaluator. It remains the
// oracle for unbound and partially compiled predicate programs.
func matchesObjectText(g *state.Game, spec string, o *state.Object, sc SpecContext) bool {
	if o == nil {
		return false
	}
	// CR 707.10h: a copy of a SPELL that has left the stack (countered,
	// fizzled, or otherwise gone) matches nothing -- it is a transient
	// reference, not a real object anymore. That is what IsCopy+off-stack
	// was meant to catch, but a blanket "any zone but the stack" also
	// rejected a permanent copy legitimately living on the battlefield
	// (Clone, Rite of Replication, a Myriad/Encore token copy, ...), which
	// must match ordinary filters -- including its own and every bystander's
	// ChangesZone triggers -- exactly like any other permanent. Only reject
	// a copy that is neither on the stack (still a spell) nor on the
	// battlefield (still a permanent).
	if o.IsCopy && o.Zone != state.ZStack && o.Zone != state.ZBattlefield {
		return false
	}
	resolve := sc.Resolve
	if resolve == nil {
		resolve = noResolve
	}
	// Forge's EACH multi-type search grammar: the spec is a '&' list of
	// ordinary sub-specs, and the union matches. Sub-specs are evaluated
	// through this same oracle, so their own predicates and bases keep the
	// ordinary semantics (a bare "EACH Forest & Plains" previously reached
	// the type walk as ONE base and matched nothing).
	if subs, ok := eachAlternatives(spec); ok {
		for _, sub := range subs {
			if matchesObjectText(g, sub, o, sc) {
				return true
			}
		}
		return false
	}
	for alt := range filterAlternatives(spec) {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		base, rest, _ := strings.Cut(alt, ".")
		asc := sc
		contextualSameName := sameNameContextBase(base, rest)
		// Forge's sameName forms can name their referent in the base:
		// Remembered.sameName, Targeted.Permanent+sameName, and
		// Triggered.sameName. Rewrite only those name-predicate alternatives;
		// a global rewrite would activate unrelated Remembered/Targeted/
		// Triggered filters outside this task's scope.
		if contextualSameName {
			ref, bound := sameNameContextReferent(g, base, asc)
			if !bound {
				continue
			}
			asc.Source = ref
			base = "Card"
		}
		if base == "CARDNAME" {
			// CR 201.5: a self-reference means this object, not another
			// object with the same name. Without a source, fail closed.
			if sc.Source == 0 || o.ID != sc.Source {
				continue
			}
		} else if !matchesBase(g, base, o, sc) {
			continue
		}
		// Forge's base-qualified `Spell.IsTargeting <target-spec>` form (and
		// the SpellAbility spelling): the WHOLE argument after `IsTargeting `
		// is the target spec, so this alternative is ONE unit -- its '+'
		// conjunctions, if any, belong to the TARGET match and are never split
		// into candidate predicates. An argument this grammar cannot answer
		// whole (a '+', an empty or malformed Valid argument, an unknown inner
		// predicate) fails the whole alternative closed: the token-level path
		// no longer recognises IsTargeting, so a truncated head is an unknown
		// predicate, never a partial match.
		if _, stNeg, shape := spellIsTargetingShape(base, rest); shape {
			if arg, _, ok := spellIsTargetingAlt(base, rest); ok && spellIsTargetingArgRecognised(arg) {
				spec, _ := spellIsTargetingInner(arg)
				met := spellIsTargetingMatches(g, spec, o, sc)
				if stNeg {
					met = !met
				}
				if met {
					return true
				}
			}
			continue
		}
		// Forge's base-qualified `SharesColorWith Valid <spec>` form: the WHOLE
		// argument after `Valid ` is the colour-share spec, so this alternative
		// is ONE unit -- its '+' conjunctions, if any, belong to the INNER spec
		// (jaded_response's `Spell.SharesColorWith Valid Creature.YouCtrl`; a
		// corpus '+' spelling like `Card.SharesColorWith Valid
		// Creature.Legendary+YouCtrl`) and are never torn into candidate
		// predicates. An argument this grammar cannot answer whole (an empty
		// inner spec, an unknown inner predicate) fails the whole alternative
		// closed: the token-level path recognises only the bare one-word
		// referents, so a truncated head is an unknown predicate, never a
		// partial match. An empty match set is "shares with nothing", a
		// resolved non-match.
		if arg, shape := sharesColorShape(rest); shape {
			if sharesColorArgRecognised(arg) && sharesColorUnitMatches(g, arg, o, sc) {
				return true
			}
			continue
		}
		all := true
		for p := range strings.SplitSeq(rest, "+") {
			if p == "" || (isTargetedCardBase(base) && compilePredicateTermCodes.Code(p) == compilePredicateTermSelf) {
				continue
			}
			// Permanent is an auxiliary part of Forge's
			// Targeted.Permanent+sameName base spelling, not a globally
			// implemented predicate. Limit its type-based reading to that
			// contextual sameName form so Card.Permanent remains fail-closed.
			if contextualSameName && p == "Permanent" {
				if !isPermanentCard(o) {
					all = false
					break
				}
				continue
			}
			// matchPredicate evaluates every recognised shape -- the predicates
			// map, a numeric predicate, a generic non<X> negation, a
			// wordPredicate classifier word, and a leading-'!' negation of any
			// of those -- to a boolean. An unrecognised token (ok == false) is
			// unknown, so it fails closed: never an always-true fallback, which
			// would silently widen the filter instead of showing up as a
			// missing action.
			res, ok := matchPredicate(g, p, o, asc)
			if !ok || !res {
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

// MatchesSpecCtx is MatchesSpec/MatchesSpecFrom's full form: the same
// grammar as MatchesObjectCtx, applied to the object g.Obj(id) names.
func MatchesSpecCtx(g *state.Game, spec string, id state.ObjID, sc SpecContext) bool {
	o := g.Obj(id)
	if o == nil {
		return false
	}
	return matchesObjectPtr(g, spec, o, &sc)
}

// MatchesSpecCtxPtr is MatchesSpecCtx reading the caller's context in place
// (no copy of the large SpecContext); the match may write scratch fields of
// *sc, so the caller must not reuse it.
func MatchesSpecCtxPtr(g *state.Game, spec string, id state.ObjID, sc *SpecContext) bool {
	o := g.Obj(id)
	if o == nil {
		return false
	}
	return matchesObjectPtr(g, spec, o, sc)
}

// matchesZoneSpecCtx matches a filter over a known zone. Forge's Permanent
// base names a permanent card when a count already scoped the candidates to a
// non-battlefield zone; it must not re-check the object's current zone and
// reject every graveyard, hand, library, or exile card. All other bases and
// predicates retain MatchesObjectCtx's ordinary semantics -- including the
// CR 707.10h IsCopy rejection below, which matchesObjectText applies on the
// ordinary path and which this zone-aware path must not silently drop.
func matchesZoneSpecCtx(g *state.Game, spec string, id state.ObjID, sc SpecContext, zone state.Zone) bool {
	o := g.Obj(id)
	if o == nil {
		return false
	}
	// CR 707.10h, the same rejection matchesObjectText applies: a copy that
	// is neither on the stack (still a spell) nor on the battlefield (still
	// a permanent, CR 707.10g) has ceased to exist and matches nothing,
	// whatever the spec. Without it a resolving spell copy the engine parks
	// in exile is counted by every Count$ThisTurnEntered_<off-battlefield
	// zone> head (Ennis, Debate Moderator's Count$ThisTurnEntered_Exile_Card
	// fired on exiled Storm copies). A battlefield copy is real and stays
	// matchable -- Clone/Rite of Replication precedent.
	if o.IsCopy && o.Zone != state.ZStack && o.Zone != state.ZBattlefield {
		return false
	}
	if zone == state.ZBattlefield {
		return matchesObjectPtr(g, spec, o, &sc)
	}
	return compiledMatchZone(compiledSpecFor(spec), g, o, &sc, zone)
}

// matchesZoneSpecText is matchesZoneSpecCtx's textual oracle for a
// non-battlefield zone (the IsCopy rejection already applied): the reference
// the compiled matchZone is held equal to.
func matchesZoneSpecText(g *state.Game, spec string, o *state.Object, sc SpecContext, zone state.Zone) bool {
	// filterAlternatives, not a raw comma split: a Count$Valid<Zone>
	// Card.named<Name> argument may carry its printed comma.
	for alt := range filterAlternatives(spec) {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		base, rest, _ := strings.Cut(alt, ".")
		if base == "CARDNAME" {
			if sc.Source == 0 || o.ID != sc.Source {
				continue
			}
		} else if !matchesBaseInZone(g, base, o, sc, zone) {
			continue
		}
		// The base-qualified Spell.IsTargeting form is one unit, exactly as in
		// matchesObjectText; the zone-aware base already fails it closed off
		// the stack, so this only keeps the two paths textually equal.
		if _, stNeg, shape := spellIsTargetingShape(base, rest); shape {
			if arg, _, ok := spellIsTargetingAlt(base, rest); ok && spellIsTargetingArgRecognised(arg) {
				spec, _ := spellIsTargetingInner(arg)
				met := spellIsTargetingMatches(g, spec, o, sc)
				if stNeg {
					met = !met
				}
				if met {
					return true
				}
			}
			continue
		}
		// The base-qualified `SharesColorWith Valid <spec>` form is one unit,
		// exactly as in matchesObjectText (sharesColorShape): the zone-aware
		// base already fails the non-land candidates closed off the
		// battlefield, so this only keeps the two paths textually equal.
		if arg, shape := sharesColorShape(rest); shape {
			if sharesColorArgRecognised(arg) && sharesColorUnitMatches(g, arg, o, sc) {
				return true
			}
			continue
		}
		all := true
		for p := range strings.SplitSeq(rest, "+") {
			if p == "" || (isTargetedCardBase(base) && compilePredicateTermCodes.Code(p) == compilePredicateTermSelf) {
				continue
			}
			res, ok := matchPredicate(g, p, o, sc)
			if !ok || !res {
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

func matchesBaseInZone(g *state.Game, base string, o *state.Object, sc SpecContext, zone state.Zone) bool {
	if neg := strings.TrimPrefix(base, "non"); neg != base {
		return !matchesBaseInZone(g, neg, o, sc, zone)
	}
	if isTargetedCardBase(base) {
		return matchesBase(g, base, o, sc)
	}
	if base != "Permanent" || zone == state.ZBattlefield {
		return matchesBase(g, base, o, sc)
	}
	return hasTypeCtx(o, "Artifact", sc) || hasTypeCtx(o, "Creature", sc) || hasTypeCtx(o, "Enchantment", sc) ||
		hasTypeCtx(o, "Land", sc) || hasTypeCtx(o, "Planeswalker", sc) || hasTypeCtx(o, "Battle", sc)
}

// MatchesSpecFrom is MatchesSpecCtx with an explicit source object, which the
// CARDNAME base and Self/Other predicates are relative to, and no numeric-RHS
// resolver.
func MatchesSpecFrom(g *state.Game, spec string, id state.ObjID, you state.PlayerID, source state.ObjID) bool {
	return MatchesSpecCtx(g, spec, id, NewSpecContext(you, source))
}

// MatchesSpec reports whether an object matches a Forge filter spec.
func MatchesSpec(g *state.Game, spec string, id state.ObjID, you state.PlayerID) bool {
	// MatchesSpecFrom with no source, spelled out so this wrapper stays
	// within the inlining budget.
	return MatchesSpecCtx(g, spec, id, NewSpecContext(you, 0))
}

// SearchStatesQuality reports whether a search's card filter (a ChangeType
// spec) states a QUALITY of the cards to be found -- a card type, subtype,
// colour, name, or any other characteristic that narrows what counts -- rather
// than only a quantity. CR 701.23b lets a player searching a hidden zone for
// cards with a stated quality decline to find (even if a matching card is
// present), while CR 701.23d requires a player searching only for a quantity
// ("a card", "three cards") to find that many, or as many as the zone holds.
//
// A spec is quantity-only when every comma alternative is a bare card clause:
// the universal `Card`/`Any` base with only possession/control predicates
// (YouOwn, YouCtrl, ...) and no type/subtype/colour/name restriction. Any
// alternative whose base names a type/identity other than `Card`/`Any`, or
// that carries any predicate other than a possession/control word, states a
// quality. Forge's `Mandatory$ True` is applied by the hidden-library search
// as an explicit prohibition on failing to find; it does not change this
// classification of the filter itself.
func SearchStatesQuality(spec string) bool {
	// An EACH spec states a quality when ANY listed sub-spec does -- every
	// real carrier lists a named type, so an EACH library search keeps
	// CR 701.23b's fail-to-find allowance. A quantity-only EACH (none in the
	// corpus) would keep the mandatory-find reading of its sub-specs.
	if subs, ok := eachAlternatives(spec); ok {
		for _, sub := range subs {
			if SearchStatesQuality(sub) {
				return true
			}
		}
		return false
	}
	for alt := range filterAlternatives(spec) {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		base, rest, _ := strings.Cut(alt, ".")
		if base != "Card" && base != "Any" {
			return true
		}
		for p := range strings.SplitSeq(rest, "+") {
			if p == "" {
				continue
			}
			if !possessionPredicate(p) {
				return true
			}
		}
	}
	return false
}

// possessionPredicate reports whether a predicate word restricts only who owns
// or controls the card, not what the card is -- so a `Card.YouOwn` search is
// still a bare quantity search under CR 701.23d. Every other predicate word is
// a quality clause, so it fails closed to "states quality" (the conservative
// direction: it preserves 701.23b's fail-to-find allowance rather than making
// a stated-quality search mandatory).
func possessionPredicate(p string) bool {
	return possessionPredicateSet.Has(p)
}

// SpecNeedsResolver reports whether spec carries a numeric predicate whose
// right-hand side is not a literal integer -- "cmcLEY", "powerGEX",
// "counters_EQX_P1P1" -- the shape numericPred hands to SpecContext.Resolve.
//
// A caller with no resolver (MatchesSpec/MatchesSpecFrom, whose noResolve
// path is documented above) gets "recognised shape, never matches" for every
// such predicate, so the spec silently admits NOTHING rather than failing
// loudly. This function is how such a caller tells that empty answer apart
// from a genuinely empty match set: rules' ETB-copy whitelist withholds the
// election for resolver-dependent selectors it cannot supply. Mockingbird's
// "Choices$ Creature.Other+cmcLEY" is the supported exception: its Y resolves
// through the source's captured Count$CastTotalManaSpent at both ETB points.
//
// The walk mirrors UnknownPredicates' -- EACH split, alternatives, '+'
// conjuncts, a leading '!' stripped -- so the two censuses see the same token
// set. The literal-RHS shapes numericPred resolves without a resolver
// (powerLTtoughness and its mirrors) are NOT reported.
func SpecNeedsResolver(spec string) bool {
	if subs, ok := eachAlternatives(spec); ok {
		for _, sub := range subs {
			if SpecNeedsResolver(sub) {
				return true
			}
		}
		return false
	}
	for alt := range filterAlternatives(spec) {
		_, rest, _ := strings.Cut(strings.TrimSpace(alt), ".")
		for p := range strings.SplitSeq(rest, "+") {
			if p == "" {
				continue
			}
			if predicateNeedsResolver(strings.TrimPrefix(p, "!")) {
				return true
			}
		}
	}
	return false
}

// predicateNeedsResolver is the per-token half of SpecNeedsResolver. It
// recognises exactly the two numericPred branches that fall back to
// SpecContext.Resolve when their right-hand side is not a literal integer.
func predicateNeedsResolver(name string) bool {
	if rest, ok := strings.CutPrefix(name, "counters_"); ok {
		if len(rest) < 4 {
			return false
		}
		numStr, kind, okSplit := strings.Cut(rest[2:], "_")
		if !okSplit || kind == "" {
			return false
		}
		_, err := strconv.Atoi(numStr)
		return err != nil
	}
	for _, field := range [...]string{"power", "toughness", "cmc"} {
		rest, ok := strings.CutPrefix(name, field)
		if !ok || len(rest) < 3 {
			continue
		}
		numStr := rest[2:]
		// The characteristic-vs-characteristic shapes need no resolver.
		if (field == "power" || field == "toughness") &&
			(numStr == "power" || numStr == "toughness" || numStr == "Power" || numStr == "Toughness") {
			return false
		}
		_, err := strconv.Atoi(numStr)
		return err != nil
	}
	return false
}

// UnknownPredicates lists tokens in a spec this build does not implement. The
// card-validation pass uses it to refuse cards it would otherwise misplay.
func UnknownPredicates(spec string) []string {
	var out []string
	// An EACH spec is split first: the sub-specs' unknowns are the union, so
	// the census is truthful for the multi-type grammar (the dotted dotted
	// form previously leaked the '&' join and every later clause as garbage
	// predicate tokens; the bare form's unknown base was never checked at
	// all, because a base-position token is not a predicate).
	if subs, ok := eachAlternatives(spec); ok {
		for _, sub := range subs {
			out = append(out, UnknownPredicates(sub)...)
		}
		sort.Strings(out)
		return out
	}
	for alt := range filterAlternatives(spec) {
		alt = strings.TrimSpace(alt)
		base, rest, _ := strings.Cut(alt, ".")
		// Forge's base-qualified Spell.IsTargeting form is ONE unit: when the
		// argument is complete and answerable whole it is recognised; every
		// other shape of the form (an empty argument, a '+' the candidate
		// grammar would split, a malformed ValidX, an unknown inner predicate)
		// is ONE unknown token -- the whole form, never its truncated '+'
		// head, so the matcher and this census stay in agreement.
		if body, _, shape := spellIsTargetingShape(base, rest); shape {
			if arg, _, ok := spellIsTargetingAlt(base, rest); ok && spellIsTargetingArgRecognised(arg) {
				continue
			}
			out = append(out, strings.TrimSpace(body))
			continue
		}
		// Forge's base-qualified `SharesColorWith Valid <spec>` form is ONE
		// unit (sharesColorShape): when the argument is complete and
		// answerable whole it is recognised; every other shape of the form (an
		// empty inner spec, an unknown inner predicate) is ONE unknown token --
		// the whole form, never its truncated '+' head, so the matcher and
		// this census stay in agreement.
		if arg, shape := sharesColorShape(rest); shape {
			if sharesColorArgRecognised(arg) {
				continue
			}
			out = append(out, strings.TrimSpace(rest))
			continue
		}
		for p := range strings.SplitSeq(rest, "+") {
			if p == "" {
				continue
			}
			// recognisedPredicate is the single classifier the matcher
			// (matchPredicate) and this census walk share, so a token is
			// either recognised by both or unknown to both -- including a
			// leading-'!' negation, which is recognised only when its inner
			// word is.
			if recognisedPredicate(p) {
				continue
			}
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// hasAbilityToken recognises Forge's `hasAbility <SA spec>` card property in
// the forms this build reads: `hasAbility Activated` (the object has an
// activated ability), `hasAbility Activated.hasTapCost` (one whose cost
// includes {T} -- Magewright's Stone's target) and `hasAbility
// Activated.Exhaust` (an exhaust ability). The matcher and the
// UnknownPredicates census share it, so an unread sub-spec
// (Activated.otherAbility) stays unknown to both and fails closed.
func hasAbilityToken(p string) bool {
	switch hasAbilityTokenCodes.Code(string(strings.TrimPrefix(p, "hasAbility "))) {
	case hasAbilityTokenActivated:
		return strings.HasPrefix(p, "hasAbility ")
	}
	return false
}

// noAbilitiesPermanent is the CR 113.12 body for `NoAbilities`: it reports
// whether the object has NO abilities at all, matching Forge's
// Card.hasNoAbilities(). An ability, for this predicate, is a printed
// keyword, an S: static, an R: replacement, a T: trigger, or an activated
// `AB` ability -- with two exclusions Forge also makes: a land's mana
// ability (isLandAbility) and the card's own plain cast (an `SP` entry,
// isBasicSpell + only-a-mana-cost). Counter-granted keywords count too, the
// same positive CounterKeyword scan objectHasKeyword uses.
//
// Reads the PRINTED face plus counter keywords only. A keyword or ability
// granted by a continuous effect (CR 113.12's granted-flying case) is not
// visible here -- predFn carries no SpecContext and this ticket does not
// thread one through the map. That is a known deviation, recorded in the
// commit message.
//
// Fail closed: a nil object or face is NOT proof of "no abilities", so it
// answers false ("has abilities") -- the fail-closed direction, never a
// silent widening of the selection. A face-down battlefield permanent has no
// abilities (CR 708.2), so it answers true; that is the OPPOSITE polarity of
// objectHasAbility's guard.
func noAbilitiesPermanent(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
	if o == nil {
		return false
	}
	f := o.Face()
	if f == nil {
		return false
	}
	if o.FaceDown && o.Zone == state.ZBattlefield {
		return true
	}
	if len(f.Keywords) > 0 || len(f.Statics) > 0 || len(f.Repls) > 0 || len(f.Triggers) > 0 {
		return false
	}
	for _, c := range o.Counters {
		if c.N > 0 {
			if _, ok := cards.CounterKeyword(c.Kind); ok {
				return false
			}
		}
	}
	for _, a := range f.Abilities {
		if a == nil {
			continue
		}
		switch a.CompiledKind() {
		case cards.SAKindActivated:
			// A land's mana ability is not an ability for this
			// predicate (Forge's isLandAbility).
			if hasType(o, "Land") && a.APIKind() == cards.APIMana {
				continue
			}
			return false
		case cards.SAKindSpell:
			// An SP entry is the card's own cast. Forge skips it only
			// when it is the basic spell AND its cost is mana-only
			// (isBasicSpell + isOnlyManaCost); an SP carrying an
			// additional non-mana cost (Makeshift Mauler's
			// ExileFromGrave) or any other extra spell ability counts
			// as an ability. A DB/ST entry is not among the spell
			// abilities hasNoAbilities reads, so it is skipped.
			if a.APIKind() == cards.APIPermanentCreature && costOnlyMana(a.ParamStr(cards.PKCost)) {
				continue
			}
			return false
		}
	}
	return true
}

// costOnlyMana reports whether a Forge cost string demands nothing beyond
// mana (Forge's Cost.isOnlyManaCost). The empty string is the no-Cost$ line
// case: the ability is paid with the card's printed mana cost, so it is
// mana-only. A cost carrying any non-mana component -- HasNonMana misses the
// CollectEvidence and RollDice heads -- or any token this build does not
// model is NOT mana-only, the fail-closed direction, so an unread token can
// never make an ability read as absent and silently widen the selection.
func costOnlyMana(raw string) bool {
	c := costvocab.ParseCost(raw)
	if c.HasNonMana() {
		return false
	}
	return len(c.Unknown) == 0 && len(c.Evidence) == 0 && len(c.RollDice) == 0
}

// objectHasAbility answers a recognised hasAbility sub-spec over the
// object's printed face's activated (AB) abilities.
func objectHasAbility(o *state.Object, sub string) bool {
	f := o.Face()
	if f == nil || (o.FaceDown && o.Zone == state.ZBattlefield) {
		return false
	}
	for _, a := range f.Abilities {
		if a == nil || a.Kind != "AB" {
			continue
		}
		switch objectHasAbilityCodes.Code(string(sub)) {
		case objectHasAbilityActivated:
			return true
		case objectHasAbilityActivatedHasTapCost:
			for _, tok := range strings.Fields(ActivationOf(a).Cost) {
				if tok == "T" {
					return true
				}
			}
		case objectHasAbilityActivatedExhaust:
			if strings.EqualFold(strings.TrimSpace(a.ParamStr(cards.PKExhaust)), "True") {
				return true
			}
		}
	}
	return false
}

var possessionPredicateSet = state.NewNameSet(
	"YouOwn",
	"YouCtrl",
	"YouControl",
	"YourControl",
	"YouControlled",
	"OppOwn",
	"OppCtrl",
	"OpponentOwns",
	"OpponentControls",
)

type matchesBaseCode uint16

const (
	matchesBaseAny matchesBaseCode = iota + 1
	matchesBaseCard
	matchesBasePermanent
	matchesBaseAffinity
	matchesBasePermanentCard
	matchesBaseSpell
	matchesBaseSpellAbility
	matchesBaseOutlaw
	matchesBaseHistoric
)

var matchesBaseCodes = state.NewStrCodes(
	state.StrEntry[matchesBaseCode]{Key: "Any", Val: matchesBaseAny},
	state.StrEntry[matchesBaseCode]{Key: "Card", Val: matchesBaseCard},
	state.StrEntry[matchesBaseCode]{Key: "Permanent", Val: matchesBasePermanent},
	state.StrEntry[matchesBaseCode]{Key: "Affinity", Val: matchesBaseAffinity},
	state.StrEntry[matchesBaseCode]{Key: "PermanentCard", Val: matchesBasePermanentCard},
	state.StrEntry[matchesBaseCode]{Key: "Spell", Val: matchesBaseSpell},
	state.StrEntry[matchesBaseCode]{Key: "SpellAbility", Val: matchesBaseSpellAbility},
	state.StrEntry[matchesBaseCode]{Key: "Outlaw", Val: matchesBaseOutlaw},
	state.StrEntry[matchesBaseCode]{Key: "Historic", Val: matchesBaseHistoric},
)

type hasAbilityTokenCode uint16

const (
	hasAbilityTokenActivated hasAbilityTokenCode = iota + 1
)

var hasAbilityTokenCodes = state.NewStrCodes(
	state.StrEntry[hasAbilityTokenCode]{Key: "Activated", Val: hasAbilityTokenActivated},
	state.StrEntry[hasAbilityTokenCode]{Key: "Activated.hasTapCost", Val: hasAbilityTokenActivated},
	state.StrEntry[hasAbilityTokenCode]{Key: "Activated.Exhaust", Val: hasAbilityTokenActivated},
)

type objectHasAbilityCode uint16

const (
	objectHasAbilityActivated objectHasAbilityCode = iota + 1
	objectHasAbilityActivatedHasTapCost
	objectHasAbilityActivatedExhaust
)

var objectHasAbilityCodes = state.NewStrCodes(
	state.StrEntry[objectHasAbilityCode]{Key: "Activated", Val: objectHasAbilityActivated},
	state.StrEntry[objectHasAbilityCode]{Key: "Activated.hasTapCost", Val: objectHasAbilityActivatedHasTapCost},
	state.StrEntry[objectHasAbilityCode]{Key: "Activated.Exhaust", Val: objectHasAbilityActivatedExhaust},
)

// pairedWithSourceThisTurn reports whether o paid, this turn, a Crew or Saddle
// cost for the permanent src: the pairing events.Apply's Crew and Saddle folds
// record in o.CrewedVehicles, stamped with o.CrewedTurn. A missing source
// fails closed.
func pairedWithSourceThisTurn(g *state.Game, o *state.Object, src state.ObjID) bool {
	if src == 0 || o == nil || o.CrewedTurn != g.Turn {
		return false
	}
	for _, v := range o.CrewedVehicles {
		if v == src {
			return true
		}
	}
	return false
}
