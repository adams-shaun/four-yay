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
	keyword string
	negated bool
}

var keywordPredicates = map[string]keywordPredicate{}

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
		kp, ok := keywordPredicates[compact]
		return kp, ok
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
	"IsRenowned": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.Renowned
	},
	"kicked": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.CastFlags&state.FlagKicked != 0
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
	"surged": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.CastFlags&state.FlagSurged != 0
	},
	// The context-free entry: a caller with no SpecContext compares printed
	// names. matchPositive evaluates NamedCard through namePredicate below
	// before reaching this map, so every context-aware call sees the layer-3
	// name instead -- the same split typePredicate keeps for ExtraTypes.
	"NamedCard": func(g *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool {
		s := g.Obj(src)
		return s != nil && s.ChosenName != "" && sharesName(o, s.ChosenName, SpecContext{})
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
	// it to exile with moveZoneEvent. Both spellings use the same tracked
	// provenance; LKI refinements are outside this narrow association.
	"ExiledWithSource": func(_ *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool {
		return src != 0 && o.ExiledWith == src
	},
	"ExiledWithSourceLKI": func(_ *state.Game, o *state.Object, _ state.PlayerID, src state.ObjID) bool {
		return src != 0 && o.ExiledWith == src
	},
	// escaped is the CastFlags provenance of an escape cast (CR 702.42a): the
	// "sacrifice it unless it escaped" ETB family reads it through
	// Card.Self+escaped (Kroxa, Uro, Phlage), as do the escape-with-counters
	// replacement ValidCard$ specs. A card never escape-cast never matches.
	"escaped": func(_ *state.Game, o *state.Object, _ state.PlayerID, _ state.ObjID) bool {
		return o.CastFlags&state.FlagEscaped != 0
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
	// graveyard" ValidCard$ reads it at spellCastMatches time — the deferred
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

	// Flash and Mutate (task costfilter): Cunning Nightbonder's
	// `Card.hasKeywordFlash` and Pollywog Symbiote's `Creature.withMutate`
	// cost reductions, plus every other corpus withFlash/hasKeywordFlash
	// filter. Forge's hasKeyword<X> is the exact-keyword spelling of the same
	// test (CardProperty: card.hasKeyword(X), introduced so "withFlash" could
	// not prefix-match Flashback); this matcher's KeywordHead comparison is
	// already exact, so hasKeyword<X> registers as a plain alias of with<X>
	// for every keyword in this list. Any other hasKeyword<X> (Landwalk,
	// Enchant, ...) stays unknown and fails closed.
	for _, kw := range [...]string{"Flying", "Trample", "Deathtouch", "Lifelink",
		"Vigilance", "Reach", "Haste", "Indestructible", "First Strike", "Menace",
		"Flanking", "Horsemanship", "Defender", "Foretell", "Shadow", "Doctor's companion",
		"Flash", "Mutate"} {
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
		name != "sharesCardTypeWithOther" && name != "sharesAllCardTypesWithOther") {
		return "", "", false
	}
	arg = strings.TrimSpace(arg)
	if arg == "" || strings.ContainsAny(arg, ".+,!") {
		return "", "", false
	}
	switch arg {
	case "RememberedCard", "Remembered", "RememberedLKI", "TriggeredCard",
		"TriggeredCardLKICopy", "Targeted", "Self", "Commander", "Convoked":
		return name, arg, true
	case "Imprinted":
		// Forge special-cases only sharesCardTypeWith Imprinted (Semblance
		// Anvil); the other family members resolve Imprinted through
		// getDefinedCards, which this referent answers the same way.
		return name, arg, true
	}
	return "", "", false
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
	switch ref {
	case "Convoked":
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
	case "Commander":
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
	case "RememberedCard":
		for _, t := range sc.Remembered {
			if !t.IsPlayer {
				ts = append(ts, t)
				break // the FIRST card entry, per Forge's RememberedCard
			}
		}
	case "Remembered", "RememberedLKI":
		for _, t := range sc.Remembered {
			if !t.IsPlayer {
				ts = append(ts, t)
			}
		}
	case "TriggeredCard", "TriggeredCardLKICopy":
		if sc.TriggerCard != 0 {
			ts = append(ts, state.Target{Obj: sc.TriggerCard})
		}
	case "Targeted":
		ts = sc.ResolutionTargets
	case "Self":
		if sc.Source != 0 {
			ts = append(ts, state.Target{Obj: sc.Source})
		}
	case "Imprinted":
		// The SOURCE's live imprint association -- the same pile Defined$
		// Imprinted resolves (imprintPileTargets: an exiled card only while
		// it stays in exile, CR 607.2a). Forge reads the FIRST imprinted card
		// (Iterables.getFirst(source.getImprintedCards())), so only that one
		// is the referent. A source with nothing imprinted binds nothing and
		// the predicate fails closed.
		if pile := imprintPileTargets(g, &Ctx{Source: sc.Source}); len(pile) > 0 {
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
		if st.Mode != "Continuous" || !strings.Contains(st.Params["Affected"], "Self") {
			continue
		}
		for v := range strings.SplitSeq(st.Params["AddType"], ",") {
			if strings.TrimSpace(v) == "ChosenType" {
				return true
			}
		}
		for v := range strings.SplitSeq(st.Params["AddTypes"], ",") {
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
	switch ref {
	case "Targeted", "ParentTarget", "TriggeredCardLKICopy", "TriggeredAttackerLKICopy":
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
	switch ref {
	case "You":
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
	switch ref {
	case "You":
		if int(sc.You) >= 0 && int(sc.You) < len(g.Players) {
			return sc.You, true
		}
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
	switch ref {
	case "Targeted", "ParentTarget":
		// Resolution-only, exactly like the Targeted*/NotDefinedTargeted
		// families: SpecContext has ResolutionTargets set only by a
		// resolving context (effects.Ctx.SpecContext or a legality recheck),
		// never while a target offer is being built.
		if !sc.Resolving {
			return nil, false
		}
		out := make([]state.ObjID, 0, len(sc.ResolutionTargets))
		for _, t := range sc.ResolutionTargets {
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
	case "TriggeredCardLKICopy", "TriggeredAttackerLKICopy":
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
		switch class {
		case "Card", "Permanent", "Spell":
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
	switch arg {
	case "Card", "Permanent", "Spell":
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
	switch typ {
	case "Card", "Permanent", "Spell":
	default:
		if !predicateTypeWords[typ] {
			return "", false
		}
	}
	switch qual {
	case "Other", "YouCtrl":
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

// wordKind classifies a predicate word that is neither in the `predicates`
// map nor a numeric predicate. It is the single classifier shared by the
// positive path in MatchesObjectCtx and by the generic non<X> negation in
// nonPredicate, so a word is either a recognised shape or it is not -- the
// matcher and UnknownPredicates cannot disagree about it.
type wordKind int

const (
	wordUnknown wordKind = iota
	wordColor
	wordType
	wordColorless
	wordMultiColor
	wordMonoColor
	// The game/source-aware families. Each needs more than the object alone:
	// the game (for the active player and the commander list), the source
	// (for combat pairing), or the object's own zone/counters. They are
	// classified here so the matcher and UnknownPredicates cannot disagree
	// about whether a word is recognised, exactly as the type-word family is.
	wordInZone
	wordActivePlayerCtrl
	wordTopLibrary
	wordHasCounters
	// Mjölnir's Worthy equip qualifier (CR 702.6): a legendary creature that
	// is red or white and is not a Villain.
	wordWorthy
	// Forge's isSuspended: the card sits in exile carrying the Suspend
	// action's cast provenance (state.FlagSuspend, set by the suspend
	// alternate-cast action's own CastInfo). The game/state-aware family --
	// needs the object's zone and cast flags -- classified here so matcher
	// and UnknownPredicates agree. The corpus spells it Card.suspended
	// (Clockspinning's and Jhoira's Timebug's TgtZone$ Exile targets, Amy
	// Pond's Choices$ card election).
	wordSuspended
	// Forge's Card.canReceiveCounters <kind>: the object can have a counter of
	// <kind> placed on it. key is the counter kind. The corpus's only use is
	// the +1/+1 spelling on Experimental Lab // Staff Room's DBPutCounter
	// presence gate; a +1/+1 counter is hostable by a creature
	// (state.Object.EffectiveIsCreature, CR 708.5-aware), any other counter
	// kind by any battlefield permanent.
	wordCanReceiveCounters
	// Forge's Card.canBeTurnedFaceUp: the face-down battlefield permanent
	// has a real card face to reveal (CR 708.6). The corpus's only use is
	// Experimental Lab // Staff Room's DBTurnFaceUp presence gate.
	wordCanBeTurnedFaceUp
	// Forge's OppProtect: the object is a battle whose CR 310.10 protector
	// is an opponent of the evaluating controller (SpecContext's You). The
	// protector state lives on the battle object itself (state.Object
	// .Protector, recorded through the Choose "protector" event).
	wordOppProtect
	wordHistoric
	wordAdventureCard
	wordIsCommander
	wordBlockingSource
	wordBlockedBySource
	// The object is controlled by a player dealt combat damage this turn by
	// the resolving source. Resolution effects bind the combat-hit ledger in
	// SpecContext; an unbound filter context fails closed.
	wordControllerDealtCombatDamageBySource
	// Forge's faceDown: a face-down battlefield permanent (a manifested or
	// cloaked card). The game/state-aware family -- needs the object's own
	// zone, classified here so matcher and UnknownPredicates agree.
	wordFaceDown
	// Forge's IsRingbearer (CR 701.54e): the object is its controller's
	// Ring-bearer. Game/state-aware -- needs the object's zone and the
	// players' designations -- classified here so matcher and
	// UnknownPredicates agree.
	wordRingBearer
	// The resolution-only one-token TargetedPlayerCtrl grammar. Its target
	// binding comes from SpecContext rather than a new state tracker.
	wordTargetedPlayerCtrl
	wordTargetedPlayerOwn
	// The two-token space form "AttachedTo <X>": <X> is a literal type or
	// object class answerable from the object in hand (the base grammar).
	wordAttachedTo
	// The two-token space form "sharesCardTypeWith <X>": <X> is a
	// resolution-time referent (RememberedCard, TriggeredCard, ...) the
	// SpecContext resolves.
	wordSharesCardType
	// The creature-subtype twin "sharesCreatureTypeWith <X>": same referent
	// switch, the intersection is over creature subtypes (Heirloom Blade).
	wordSharesCreatureType
	// "sharesCardTypeWithOther <X>": the card-type intersection, excluding
	// the candidate itself (The Tale of Tamiyo's mill gate).
	wordSharesCardTypeOther
	// "sharesAllCardTypesWithOther <X>": same referent switch, but the
	// candidate must share EVERY one of its card types with some OTHER
	// object the referent names (Demonic Covenant's "two cards that share
	// all their card types were milled this way").
	wordSharesAllCardTypes
	// The two-token space form "EnchantedBy <Type>.<qual>": the candidate
	// bears an attached permanent of the named type whose qualifier holds
	// against that attached object (Daybreak Coronet's "creature with
	// another Aura attached to it", the Aura.YouCtrl family). The bare
	// "EnchantedBy" token keeps its map-predicate meaning (attachedBy) and
	// never reaches this classifier.
	wordEnchantedBy
	// Forge's zone-entry history predicates: "ThisTurnEntered" (the object
	// entered a zone this turn, any zone) and "ThisTurnEnteredFrom_<Zone>"
	// (it entered from <Zone>). Both read the per-object entry provenance
	// events.Move records, which the Count$ThisTurnEntered_* heads share.
	wordThisTurnEntered
	wordThisTurnEnteredFrom
	// The "<Colour>Source" family (Ojer Axonil's Card.RedSource+YouCtrl):
	// the object is a source carrying that colour -- CR 700.7's "a red
	// source" is a source with red in its colour characteristics, which for
	// the object in hand is exactly ColorsOf containing the colour. The
	// Colorless member is a source with no colours at all.
	wordColourSource
	wordColourSourceless
	// The name-predicate family (Forge CardProperty): named<Name> and
	// notnamed<Name> compare the candidate's name characteristics with the
	// argument text (key carries it, `;`/`_` normalised); sameName compares
	// it with the source card, or with the name referent encoded by its own
	// Remembered./Targeted./Triggered. base shape.
	wordNamed
	wordNotnamed
	wordSameName
	// wasCast is Forge's Card.wasCast: the object is a SPELL currently on
	// the stack -- announced, not yet resolved. The AffectedZone$ Stack
	// convoke/cascade grants key on it (Chief Engineer). An ability object
	// (Card == nil) was never cast.
	wordWasCast
	// Forge's Card.copiedSpell: the object is a copy of a spell or permanent
	// (CR 707), read off state.Object.IsCopy -- the same bit every other copy
	// read in the engine uses. It is classified here so the generic non<X>
	// negation can express nonCopiedSpell (The Heron Moon's
	// `Card.OppOwn+!token+nonCopiedSpell`), and so matcher and
	// UnknownPredicates agree.
	wordCopiedSpell
	// The three cast-provenance tokens (castprov1/2/3): wasCastFromYourHandByYou,
	// wasCastByYou and the bare wasCastFromYourHand. rules' castProvenanceAdmits
	// strips and evaluates them at every match site (it holds the event log and
	// the offer window the filter tier cannot reach), so the filter grammar's
	// own body is a recognised-but-fail-closed marker: it exists so the census
	// (UnknownPredicates) reports the token as recognised rather than unknown,
	// and so an unstripped direct filter call fails closed instead of matching.
	wordCastProvenance
	// The and/or Kicker's index form: "kicked 1" / "kicked 2" (the whole
	// two-token form survives the spec splitter) reads the specific part's
	// CastFlags bit. The bare "kicked" word stays in the predicates map.
	wordKickedIndex
	// Forge's numTypesGE<n> card property: the object's printed face carries
	// at least n distinct real card types (CR 205.1 -- the same vocabulary
	// count.go's CardTypes count property reads, cardTypeWords). key is the
	// decimal n, validated at classification time so the matcher's parse
	// cannot miss. Only the GE spelling is in the corpus (numTypesGE2 x2,
	// both on Rendmaw, Creaking Nest); the other comparison spellings stay
	// wordUnknown and fail closed.
	wordNumTypesGE
	// The pc1 object/game-context predicate families (task pc1ctx), the last
	// of the row's named stragglers. Each reads provenance the object alone
	// does not carry, so it is classified here and evaluated by wordMatches
	// against SpecContext/state:
	//
	// wordDealtDamageThisTurn is Forge's wasDealtDamageThisTurn -- the object
	// was dealt damage this turn (state.Object.WasDealtDamageThisTurn, the
	// per-turn provenance events.Apply's Damage case sets and TurnChange
	// clears). wordImprinted is IsImprinted -- the candidate is in the SOURCE
	// object's persistent imprint association (state.Object.Imprinted/
	// ImprintTokens/SeekFound, the same pile imprintPileTargets resolves for
	// Defined$ Imprinted). wordDefenderCtrl is DefenderCtrl -- controlled by
	// the defending player the resolving trigger captured
	// (TriggerContext.DefendingPlayer). wordNotDefinedTargeted is
	// NotDefinedTargeted -- the candidate is NOT among the resolving ability's
	// targets (SpecContext.ResolutionTargets while Resolving).
	// wordOpponentCtrl is the bare Opponent object predicate -- controlled by
	// an opponent of the evaluating controller (sc.You), the object-side twin
	// of the player grammar's `Opponent` base.
	wordDealtDamageThisTurn
	wordDamagedBy
	wordImprinted
	wordDefenderCtrl
	wordNotDefinedTargeted
	wordOpponentCtrl
	// wordEnchantedControllerCtrl is Forge's EnchantedControllerCtrl -- the
	// candidate is controlled by the controller of the permanent the SOURCE
	// Aura/Equipment is attached to. It is the object-side twin of the player
	// grammar's `Player.EnchantedController` clause (both resolve through
	// playerEnchantedController), closing the census for the three corpus
	// carriers (Snowblind's Land.Snow+EnchantedControllerCtrl, and
	// Disturbing Conversion's / So Tiny's Card.EnchantedControllerCtrl).
	wordEnchantedControllerCtrl
	wordChosenColor
	// wordHasNonBasicLandType is Forge's Card.hasANonBasicLandType: the object
	// is a LAND that has at least one land type outside CR 205.3i's five
	// basic land types (Desert, Gate, Locus, Urza's, Cave, ...). It is NOT
	// "is a nonbasic land" -- the Basic supertype is irrelevant (a Wastes is
	// a basic land with no land type and must NOT match; a Desert is a
	// nonbasic land and must match). The type vocabulary comes from
	// chooseNonbasicLandTypes so this predicate and the Nonbasic Land choose
	// cannot drift (the structural sharing the basic-land sibling used).
	wordHasNonBasicLandType
	// wordHasBasicLandType is Forge's Card.hasABasicLandType: the object is a
	// LAND that has at least one of CR 205.3i's five basic land types
	// (Plains, Island, Swamp, Mountain, Forest). It is NOT "has the Basic
	// supertype": a Wastes is a basic land with no basic land type and must
	// NOT match (CR 205.3i is explicit, and every corpus carrier's reminder
	// text says "a land card with a basic land type"). The five words come
	// from chooseBasicLandTypes so this predicate and the Basic Land choose
	// cannot drift.
	wordHasBasicLandType
	// wordDealtDamageByThisGame is Forge's wasDealtDamageByThisGame: the
	// candidate object was dealt damage this game by the SOURCE bound in
	// SpecContext (the bare, source-anchored spelling).
	// wordDealtDamageThisGameBy is the argument-taking sibling
	// wasDealtDamageThisGameBy <ref>: the candidate was dealt damage this
	// game by the objects <ref> resolves to (the corpus only ever says
	// Self). Both read state.Object.DamageTakenByGame, the game-long record
	// events.Apply's DamageProvenance case appends (the object-side twin of
	// Player.DamageTakenByGame the player qualifier reads).
	wordDealtDamageByThisGame
	wordDealtDamageThisGameBy
	// wordNotedFor is Forge's CardProperty `NotedFor<label>` (Card.NotedFor):
	// the candidate object carries the card-notation label <label> in
	// state.Object.Notes -- the object-side sibling of the player grammar's
	// `Player.NotedFor<label>` (which MatchesPlayerSpecFrom owns). It is
	// written by events.Apply's CardNoted case (a DB$ Pump body's `NoteCards$
	// Remembered/TriggeredSource | NoteCardsFor$`), so matcher and census
	// recognise it through this one classifier and no corpus spelling can
	// drift. An empty label (a bare `NotedFor`) stays wordUnknown and fails
	// closed, like a bare `named`.
	wordNotedFor
)

// wordPredicate classifies a bare predicate word. key is the WUBRG letter for
// wordColor, the corpus type word for wordType, and empty for the others. An
// unrecognised word is wordUnknown: both sides must fail closed on it, never
// turn it into an always-true predicate.
func wordPredicate(p string) (wordKind, string) {
	if suffix, ok := strings.CutPrefix(p, "DamagedBy"); ok {
		if suffix == "" {
			return wordDamagedBy, ""
		}
		base := strings.TrimSuffix(suffix, ".YouCtrl")
		if base != "Card" && base != "Giant" && base != "Spider" {
			return wordUnknown, suffix
		}
		if suffix != base && suffix != base+".YouCtrl" {
			return wordUnknown, suffix
		}
		return wordDamagedBy, suffix
	}
	if l, is := colorLetter[p]; is {
		return wordColor, l
	}
	if c, ok := strings.CutSuffix(p, "Source"); ok {
		if l, is := colorLetter[c]; is {
			return wordColourSource, l
		}
		if c == "Colorless" {
			return wordColourSourceless, ""
		}
	}
	// notnamed before named: both prefixes are literal token prefixes and
	// "notnamed..." does not start with "named", but checking in this order
	// documents that neither is a prefix of the other's grammar. An empty
	// argument (a bare `named`) stays recognised and never matches -- Forge
	// sharesNameWith("") is false.
	if name, ok := strings.CutPrefix(p, "notnamed"); ok {
		if name == "" {
			// A bare `notnamed` would negate to always-true (the negation of
			// "no name at all"), which the fail-closed contract forbids; it
			// stays unknown instead.
			return wordUnknown, ""
		}
		return wordNotnamed, nameArg(name)
	}
	if name, ok := strings.CutPrefix(p, "named"); ok {
		return wordNamed, nameArg(name)
	}
	if p == "sameName" {
		return wordSameName, ""
	}
	if p == "controllerWasDealtCombatDamageByThisTurn" {
		return wordControllerDealtCombatDamageBySource, ""
	}
	// Forge's inZone<Zone> property (CardProperty inZone<Zone>): the object
	// sits in the named zone. The old specific inZoneStack spelling folds
	// into the generic form (both mean Zone == ZStack); an unresolvable zone
	// name falls through to wordUnknown and fails closed.
	if z, ok := strings.CutPrefix(p, "inZone"); ok && z != "" {
		if _, is := parseZone(z); is {
			return wordInZone, z
		}
	}
	// Forge's inRealZone<X> property: the object's REAL (current) zone is
	// <X> -- the same live read inZone<X> gives, spelled to distinguish from
	// an LKI-based zone test (Not of This World's TargetValidTargeting$
	// Permanent.YouCtrl+inRealZoneBattlefield). An unresolvable zone name
	// falls through to wordUnknown and fails closed.
	if z, ok := strings.CutPrefix(p, "inRealZone"); ok && z != "" {
		if _, is := parseZone(z); is {
			return wordInZone, z
		}
	}
	// Forge's CardProperty NotedFor<label> (Card.NotedFor): the candidate
	// carries the card-notation label <label> in state.Object.Notes
	// (events.Apply's CardNoted fold -- a DB$ Pump body's `NoteCards$
	// Remembered/TriggeredSource | NoteCardsFor$`). Classified here so the
	// matcher and the UnknownPredicates census cannot disagree, exactly like
	// the inZone family above; a bare `NotedFor` (empty label) stays
	// wordUnknown and fails closed, like a bare `named`.
	if label, ok := strings.CutPrefix(p, "NotedFor"); ok && label != "" {
		return wordNotedFor, label
	}
	// The and/or Kicker's index form "kicked <n>" (Forge's Card.kicked with
	// the part index -- Wastescape Battlemage's "Card.Self+kicked 1"): the
	// bare "kicked" word is in the predicates map (any CastFlags kicker
	// bit); the index form reads the specific part's bit.
	if rest, ok := strings.CutPrefix(p, "kicked "); ok {
		switch strings.TrimSpace(rest) {
		case "1", "2":
			return wordKickedIndex, strings.TrimSpace(rest)
		}
	}
	// Forge's argument-taking wasDealtDamageThisGameBy <ref> (the_fallen's
	// ValidCards$ Planeswalker.wasDealtDamageByThisGame is the bare sibling
	// registered in the switch below). The trimmed argument is the key, the
	// same shape the `kicked <n>` index form above uses.
	if rest, ok := strings.CutPrefix(p, "wasDealtDamageThisGameBy "); ok {
		return wordDealtDamageThisGameBy, strings.TrimSpace(rest)
	}
	// Forge's argument-taking canReceiveCounters <kind> (Experimental Lab //
	// Staff Room's DBPutCounter presence gate is the corpus's only carrier):
	// the trimmed counter kind is the key, the same shape the `kicked <n>`
	// index form above uses. An empty argument stays wordUnknown.
	if rest, ok := strings.CutPrefix(p, "canReceiveCounters "); ok {
		if kind := strings.TrimSpace(rest); kind != "" {
			return wordCanReceiveCounters, kind
		}
	}
	switch p {
	case "Colorless":
		return wordColorless, ""
	case "MultiColor":
		return wordMultiColor, ""
	case "MonoColor":
		return wordMonoColor, ""
	case "Worthy":
		return wordWorthy, ""
	case "ChosenColor":
		return wordChosenColor, ""
	case "wasCast":
		return wordWasCast, ""
	case "CopiedSpell":
		return wordCopiedSpell, ""
	// The cast-provenance tokens are recognised here (so the census no longer
	// reports them unknown) but evaluated by rules' castProvenanceAdmits,
	// which strips them before the filter runs; wordMatches' body fails
	// closed. wasCastFromYourHandByYou is checked before the bare
	// wasCastFromYourHand because the bare token is a substring of the ByYou
	// spelling -- the same ordering rule castProvenanceAdmits documents.
	case "wasCastFromYourHandByYou", "wasCastByYou", "wasCastFromYourHand",
		"wasCastFromExile", "wasCastFromYourGraveyard",
		"wasCastFromYourGraveyardByYou", "wasCastFromTheirHand":
		return wordCastProvenance, p
	// The card-level CastSa property tokens (task castsa-provenance): the
	// five mana-spend spellings the payment path's tagged ManaAdd encoding
	// answers (task mayplay-mfa added CastSa Spell.ManaFromArtifact), plus
	// the cast-flag spellings Spell.Mayhem (state.FlagMayhem, stamped by
	// modeFlags' "mayhem" case) and Spell.Warp (state.FlagWarped, modeFlags'
	// "warped" case) — recognized here (the census no longer reports them
	// unknown) but evaluated by the provenance strips (rules' castSaAdmits
	// and the per-event walk in spellsCastThisTurnMatching;
	// effects/conditions.go's castSaAdmitsFilter for the ConditionPresent
	// gates), which remove the token before the filter runs; wordMatches'
	// body fails closed. CastSa Spell.MayPlaySource is stripped rules-side
	// too: castSaAdmits reads the cast's FlagMayPlay after payment, and the
	// cost-static chain answers it from the may-play permission the cast
	// rides (rules' castRidesMayPlayOf) -- so it is recognised here as well;
	// an effects-side read that no rules strip precedes still fails closed.
	case "CastSa Spell.ManaFromTreasure", "CastSa Spell.ManaFromCave",
		"CastSa Spell.ManaFromDesert", "CastSa Spell.ManaFromArtifact",
		"CastSa Spell.ManaSpent EQ0",
		"CastSa Spell.Mayhem", "CastSa Spell.Warp", "CastSa Spell.MayPlaySource":
		return wordCastProvenance, p
	case "ActivePlayerCtrl":
		return wordActivePlayerCtrl, ""
	case "TopLibrary":
		return wordTopLibrary, ""
	case "faceDown":
		return wordFaceDown, ""
	case "canBeTurnedFaceUp":
		return wordCanBeTurnedFaceUp, ""
	case "IsRingbearer":
		return wordRingBearer, ""
	case "HasCounters":
		return wordHasCounters, ""
	case "suspended":
		return wordSuspended, ""
	// The pc1 object/game-context families. Each is a bare predicate word
	// whose body reads provenance outside the object alone (see the
	// wordKind block's comment); classification here is what makes the
	// matcher and UnknownPredicates agree that the word is implemented.
	case "wasDealtDamageThisTurn":
		return wordDealtDamageThisTurn, ""
	case "wasDealtDamageByThisGame":
		return wordDealtDamageByThisGame, ""
	case "IsImprinted":
		return wordImprinted, ""
	case "DefenderCtrl":
		return wordDefenderCtrl, ""
	case "EnchantedControllerCtrl":
		return wordEnchantedControllerCtrl, ""
	case "NotDefinedTargeted":
		return wordNotDefinedTargeted, ""
	case "Opponent":
		return wordOpponentCtrl, ""
	case "OppProtect":
		return wordOppProtect, ""
	case "Historic":
		return wordHistoric, ""
	case "AdventureCard":
		return wordAdventureCard, ""
	case "IsCommander":
		return wordIsCommander, ""
	case "blockingSource":
		return wordBlockingSource, ""
	case "blockedBySource":
		return wordBlockedBySource, ""
	// Forge's Card.hasANonBasicLandType (the corpus's
	// `Land.hasANonBasicLandType` qualifier; Wonderscape Sage's
	// ConditionPresent gate). The bare word is classified here so the matcher
	// and the UnknownPredicates census share one recogniser; the `Land.` base
	// the corpus spells it under is the union spelling (the body re-checks
	// Land anyway, so a bare `Card.hasANonBasicLandType` stays correct too).
	case "hasANonBasicLandType":
		return wordHasNonBasicLandType, ""
	// Forge's Card.hasABasicLandType (the corpus's `Land.hasABasicLandType`
	// qualifier). The bare word is classified here so the matcher and the
	// UnknownPredicates census share one recogniser; the `Land.` base the
	// corpus spells it under is the union spelling (the body re-checks Land
	// anyway, so a bare `Card.hasABasicLandType` stays correct too).
	case "hasABasicLandType":
		return wordHasBasicLandType, ""
	}
	if p == "TargetedPlayerOwn" {
		return wordTargetedPlayerOwn, ""
	}
	if targetReferent(p) {
		return wordTargetedPlayerCtrl, ""
	}
	if p == "ThisTurnEntered" {
		return wordThisTurnEntered, ""
	}
	if z, is := strings.CutPrefix(p, "ThisTurnEnteredFrom_"); is && zoneWordKnown(z) {
		return wordThisTurnEnteredFrom, z
	}
	// The two-token space form "AttachedTo <X>": the whole "AttachedTo
	// Creature" token survives the spec splitter (a space is not a ',' '.'
	// or '+' delimiter), so it arrives here intact. The argument must be a
	// single literal type or object class the base grammar can answer from
	// the object in hand; a referent that needs resolution-time context
	// (AttachedTo Targeted) or a nested predicate (AttachedTo
	// Permanent.YouCtrl) stays wordUnknown and fails closed.
	if arg, ok := attachedToArg(p); ok {
		return wordAttachedTo, arg
	}
	// The BARE form "sharesCreatureTypeWith" (no space, no referent — the
	// whole token survives the spec splitter exactly like the two-token
	// form): the SOURCE itself is the shared referent, Forge's unqualified
	// reading in a source-anchored filter. The two corpus carriers are Titan
	// of Littjara's `SVar:X:Count$Valid Creature.YouCtrl+Other+
	// sharesCreatureTypeWith` (the Draw<X/You> cost's X) and Plane-Merge
	// Elf's Kinfall (`ValidCard$ Creature.YouCtrl+sharesCreatureTypeWith`).
	// The bare card-type siblings have no corpus carrier and stay unknown —
	// fail closed.
	if p == "sharesCreatureTypeWith" {
		return wordSharesCreatureType, "Self"
	}
	if name, arg, ok := sharesTypeArg(p); ok {
		switch name {
		case "sharesCreatureTypeWith":
			return wordSharesCreatureType, arg
		case "sharesCardTypeWithOther":
			return wordSharesCardTypeOther, arg
		case "sharesAllCardTypesWithOther":
			return wordSharesAllCardTypes, arg
		}
		return wordSharesCardType, arg
	}
	// The two-token space form "EnchantedBy <Type>.<qual>" (the whole token
	// survives the spec splitter -- a space is not a delimiter). Only the
	// shapes enchantedByArg validates become wordEnchantedBy; everything
	// else falls through to wordUnknown and fails closed.
	if arg, ok := enchantedByArg(p); ok {
		return wordEnchantedBy, arg
	}
	// Forge's numTypesGE<n> (CardProperty numTypesGE<n>): at least n
	// distinct card types on the printed face. A non-integer or
	// non-positive suffix stays wordUnknown and fails closed -- a bare
	// "numTypesGE" never matches anything, the same contract the other
	// argument-carrying classifiers keep.
	if n, ok := strings.CutPrefix(p, "numTypesGE"); ok && n != "" {
		if v, err := strconv.Atoi(n); err == nil && v > 0 {
			return wordNumTypesGE, n
		}
	}
	if predicateTypeWords[p] {
		return wordType, p
	}
	// Forge spells the established subtype Time Lord as two separate type
	// words on the face, but as one token in filters. Do not accept arbitrary
	// pairs of known type words as new subtype names.
	if p == "Time Lord" {
		return wordType, p
	}
	return wordUnknown, ""
}

// zoneWords maps the zone names the corpus's ThisTurnEnteredFrom_<Zone>
// predicate (and Forge's ZoneType.smartValueOf) spells to state zones.
var zoneWords = map[string]state.Zone{
	"Battlefield": state.ZBattlefield,
	"Graveyard":   state.ZGraveyard,
	"Hand":        state.ZHand,
	"Library":     state.ZLibrary,
	"Exile":       state.ZExile,
	"Stack":       state.ZStack,
	"Command":     state.ZCommand,
}

// zoneWordKnown reports whether a word names a zone (zoneWords membership),
// the recognition half of the ThisTurnEnteredFrom_<Zone> classifier -- an
// unknown zone word stays wordUnknown and fails closed.
func zoneWordKnown(z string) bool {
	_, ok := zoneWords[z]
	return ok
}

// wordMatches reports whether an object satisfies a positively-evaluated
// classifier from wordPredicate. Colorless is "no colour at all" and
// MultiColor "more than one colour"; MonoColor is its twin, "exactly one
// colour" (Tarnation Vista's EachColorAmong_Valid
// Permanent.YouCtrl+MonoColor -- a colourless permanent is not monocolored),
// all read off ColorsOf rather than the face directly -- so a Devoid card (CR 702.114, which ColorsOf already
// implements) is Colorless, which is the whole point of Devoid. The
// game/source-aware families read the live game, the object's own zone or
// counters, and the effect's source (for combat pairing and commander
// membership).
func wordMatches(kind wordKind, key string, g *state.Game, o *state.Object, sc SpecContext) bool {
	source := sc.Source
	switch kind {
	case wordSharesCardType:
		return sharesCardTypeWith(g, o, sc, key)
	case wordSharesCardTypeOther:
		return sharesCardTypeWithOther(g, o, sc, key)
	case wordSharesCreatureType:
		return sharesCreatureTypeWith(g, o, sc, key)
	case wordSharesAllCardTypes:
		return sharesAllCardTypesWithOther(g, o, sc, key)
	case wordColor:
		return strings.Contains(ColorsOf(o), key)
	case wordChosenColor:
		if source == 0 {
			return false
		}
		src := g.Obj(source)
		if src == nil {
			return false
		}
		chosen := colourLetter(src.ChosenColor)
		return chosen != 0 && strings.Contains(ColorsOf(o), string(chosen))
	case wordType:
		return hasTypePredicateCtx(o, key, sc)
	case wordColorless:
		return ColorsOf(o) == ""
	case wordControllerDealtCombatDamageBySource:
		if source == 0 || o.Controller < 0 {
			return false
		}
		// During resolution Source is the ability stack object; its source
		// permanent is the card that dealt the combat damage.
		if ability := g.Obj(source); ability != nil && ability.Ability != nil {
			source = ability.Source
		}
		for _, hit := range sc.CombatDamageHits {
			if hit.Source == source && hit.Player == o.Controller {
				return true
			}
		}
		return false
	case wordColourSource:
		return strings.Contains(ColorsOf(o), key)
	case wordColourSourceless:
		return ColorsOf(o) == ""
	case wordKickedIndex:
		// The and/or Kicker's part bits (state/object.go): the CastInfo
		// provenance the kicked1/kicked2/kickedboth cast modes ride. A part
		// never paid never matches, and the bare FlagKicked bit alone (a
		// single-cost Kicker) never matches an index form.
		switch key {
		case "1":
			return o.CastFlags&state.FlagKicked1 != 0
		case "2":
			return o.CastFlags&state.FlagKicked2 != 0
		}
		return false
	case wordNotedFor:
		// Forge's Card.NotedFor<label>: the candidate carries the card-notation
		// label in state.Object.Notes (events.Apply's CardNoted fold). Pure
		// object state -- no SpecContext binding, so the positive and the
		// '!'-negated spellings both evaluate on the object alone, and an
		// unnoted object simply does not match.
		return slices.Contains(o.Notes, key)
	case wordMultiColor:
		return len(ColorsOf(o)) > 1
	case wordMonoColor:
		return len(ColorsOf(o)) == 1
	case wordWorthy:
		colors := ColorsOf(o)
		return hasTypeCtx(o, "Legendary", sc) && !hasTypeCtx(o, "Villain", sc) &&
			(strings.Contains(colors, "R") || strings.Contains(colors, "W"))
	case wordWasCast:
		// Forge's wasCast: a spell (Card != nil) currently on the stack. An
		// ability object was activated, never cast. The AsStack override
		// (rules.derivedWith) admits the spell a cast is announcing, which is
		// still in hand at CR 601.2b but IS the spell being cast.
		return (o.Zone == state.ZStack || sc.AsStack) && o.Card != nil
	case wordCopiedSpell:
		// Forge's copiedSpell: the object IS a copy of a spell or permanent
		// (CR 707.10), read off state.Object.IsCopy. The CR 707.10h guard in
		// matchesObjectText already rejects a copy that has left both the
		// stack and the battlefield, so only a live spell/permanent copy
		// reaches here -- which is exactly the object nonCopiedSpell must
		// exclude. Its negation is nonCopiedSpell.
		return o.IsCopy
	case wordCastProvenance:
		// The three cast-provenance tokens (wasCastFromYourHandByYou,
		// wasCastByYou, bare wasCastFromYourHand) are evaluated at every
		// rules match site by castProvenanceAdmits -- which strips them from
		// the spec before the filter is reached, because their truth lives in
		// the event log and the pre-push offer window the filter tier cannot
		// read. A direct filter call that somehow still carries the token
		// therefore fails closed (this wordMatches body), never matches.
		return false
	case wordInZone:
		// Forge's inZone<Zone>: the object is in that zone (measured at the
		// corpus pin: inZoneBattlefield 272 raw occurrences, inZoneStack 30,
		// inZoneGraveyard 20, inZoneHand 9, inZoneLibrary 4, inZoneExile 4 --
		// InZones$ is a separate parameter key, not a predicate word).
		z, ok := parseZone(key)
		return ok && o.Zone == z
	case wordActivePlayerCtrl:
		// Forge's ActivePlayerCtrl: the object is controlled by the active
		// player -- the seat whose turn it is, g.Active.
		return o.Controller == g.Active
	case wordCanReceiveCounters:
		return canReceiveCounter(key, o)
	case wordCanBeTurnedFaceUp:
		// Forge's Card.canBeTurnedFaceUp: a face-down battlefield permanent
		// with a real card face to reveal (CR 708.6) -- morph/megamorph/
		// disguise, manifest or cloak. gorge's turn-up path (effects'
		// effSetState Mode$ TurnFaceUp) reveals any face-down battlefield
		// permanent, so this live FaceDown read is the engine's own answer;
		// the corpus's `+faceDown` qualifier beside it is redundant but
		// harmless.
		return o.FaceDown && o.Zone == state.ZBattlefield && o.Face() != nil
	case wordFaceDown:
		// Forge's faceDown: the object is a face-down battlefield permanent
		// (CR 708.5 -- a manifested or cloaked card). The same live state read
		// the rules-side scans gate on (faceDownPrintedHides); a face-down
		// EXILE (Hideaway) is not a permanent and never matches.
		//
		// AsFaceDown is a DERIVED-CHARACTERISTICS override, like AsStack just
		// above: a Moved replacement's ValidCard$ is evaluated BEFORE
		// events.Apply folds the face-down marker onto the object (the object
		// is still in its origin zone with FaceDown false), so a filter like
		// `Creature.faceDown+YouCtrl` (Veiled Ascension) would fail closed for
		// the very entry it names. rules/replacement.go sets this bit when the
		// intercepted move is a face-down battlefield entry.
		return sc.AsFaceDown || (o.FaceDown && o.Zone == state.ZBattlefield)
	case wordRingBearer:
		// Forge's IsRingbearer (CR 701.54e): the object is its controller's
		// Ring-bearer -- true exactly while it is on the battlefield under
		// that player's control and carries the seat's designation. The
		// designation's zone and control halves are enforced by events.Apply
		// (the battlefield-leave and ControlChange clears), so the live check
		// is the id comparison, and an object outside the battlefield (or an
		// LKI of a moved one) never matches.
		return o.Zone == state.ZBattlefield && g.IsRingBearer(o.Controller, o.ID)
	case wordTopLibrary:
		// Forge's TopLibrary: the object is the top card of its library --
		// index 0 of the owner's library slice, the card the next draw takes
		// (effects.drawFor draws lib[0]). A card deeper in the library never
		// matches, and a card that is not in a library at all never matches.
		if o.Zone != state.ZLibrary {
			return false
		}
		ids := g.Zone(state.ZLibrary, o.Owner)
		return len(ids) > 0 && ids[0] == o.ID
	case wordHasCounters:
		// Forge's HasCounters: the object has at least one counter of any
		// kind on it. Test the COUNT, not the slice length -- state's
		// AddCounter clamps a drained kind at zero without pruning the slot
		// (state/object.go), so a permanent whose counters were all removed
		// still carries a zero-count entry and must not match.
		return hasCounters(o.Counters)
	case wordSuspended:
		// Forge's isSuspended: the card is in exile carrying the Suspend
		// action's cast provenance. Time counters are deliberately NOT part
		// of the read: a suspended card whose counters were all removed by a
		// Clockspinning-style effect (or whose cast offer was declined) is
		// still the "suspended card" the corpus's specs name, and a
		// Clockspinning may put a time counter back on it.
		return o.Zone == state.ZExile && o.CastFlags&state.FlagSuspend != 0
	case wordOppProtect:
		// Forge's OppProtect: the object is a battle whose CR 310.10
		// protector is an opponent of the evaluating controller (sc.You).
		// No protector chosen yet never matches; a protector that is the
		// evaluating controller itself (their own battle after a control
		// change) never matches.
		if !o.ProtectorValid || o.Zone != state.ZBattlefield {
			return false
		}
		return int(o.Protector) >= 0 && int(o.Protector) < len(g.Players) &&
			o.Protector != sc.You && !g.Players[o.Protector].Lost
	case wordHistoric:
		// Forge's Historic: artifact, legendary, or Saga (the reminder text
		// on the Historic keyword).
		return hasTypeCtx(o, "Artifact", sc) || hasTypeCtx(o, "Legendary", sc) || hasTypeCtx(o, "Saga", sc)
	case wordAdventureCard:
		if o == nil || o.Card == nil || o.Card.AlternateMode != "Adventure" || len(o.Card.Faces) != 2 {
			return false
		}
		face := o.Card.Faces[1]
		if face == nil || (!face.IsInstant() && !face.IsSorcery()) {
			return false
		}
		for _, typ := range face.Types {
			if typ == "Adventure" {
				return true
			}
		}
		return false
	case wordIsCommander:
		// Forge's IsCommander: the object is one of a seat's commanders.
		// The commander list lives on the Players at genesis.
		for i := range g.Players {
			for _, c := range g.Players[i].Commanders {
				if c == o.ID {
					return true
				}
			}
		}
		return false
	case wordBlockingSource:
		// Forge's blockingSource: the object is a creature blocking the
		// source. BlockedBy is recorded on the attacked object, so the
		// source's BlockedBy names its blockers; this object is one of them.
		s := g.Obj(source)
		return s != nil && containsID(s.BlockedBy, o.ID)
	case wordBlockedBySource:
		// Forge's blockedBySource: the object is being blocked by the source
		// -- the source is one of THIS object's blockers.
		return containsID(o.BlockedBy, source)
	case wordNumTypesGE:
		// Forge's numTypesGE<n>: the object's printed face carries at least
		// n distinct real card types (CR 205.1). The vocabulary is the one
		// shared census cardTypeWords (effects/creature_types.go), the same
		// set count.go's CardTypes count property filters through, so the
		// predicate and the count head cannot disagree about what a "card
		// type" is. No allocation on this hot filter path: the nested loop
		// dedupes against the already-scanned prefix of the same (always
		// tiny) type list, and a nil face matches nothing.
		n, err := strconv.Atoi(key)
		if err != nil || n <= 0 || o.Face() == nil {
			return false
		}
		count := 0
		types := o.Face().Types
		for i, t := range types {
			if !cardTypeWords[t] {
				continue
			}
			dup := false
			for _, u := range types[:i] {
				if u == t {
					dup = true
					break
				}
			}
			if !dup {
				count++
			}
		}
		return count >= n
	case wordTargetedPlayerCtrl:
		matched, ok := matchTargetedPlayerCtrl(g, o, sc)
		return ok && matched
	case wordTargetedPlayerOwn:
		matched, ok := matchTargetedPlayerOwn(g, o, sc)
		return ok && matched
	case wordThisTurnEntered:
		// Forge's ThisTurnEntered: the object entered a zone this turn (any
		// zone). The flag is the same per-object provenance
		// events.Move records that the Count$ThisTurnEntered_* heads read.
		return o.EnteredThisTurn
	case wordThisTurnEnteredFrom:
		// Forge's ThisTurnEnteredFrom_<Zone>: the object entered from <Zone>
		// this turn. An unknown zone word never reaches here (the classifier
		// fails closed), so the map lookup cannot miss.
		return o.EnteredThisTurn && o.EnteredFrom == zoneWords[key]
	case wordNamed:
		// Forge CardProperty "named<X>": card.sharesNameWith the argument.
		return sharesName(o, key, sc)
	case wordNotnamed:
		// Forge implements no notnamed predicate and the corpus carries
		// none (measured); this engine gives the token the negation
		// semantics its shape implies rather than the always-true trap an
		// unrecognised-but-plausible token could be mistaken for.
		return !sharesName(o, key, sc)
	case wordDealtDamageThisTurn:
		// Forge's wasDealtDamageThisTurn: the object was dealt damage this
		// turn. The flag is the per-object provenance events.Apply's Damage
		// case sets (the same field the playercount HasProperty heads read)
		// and TurnChange clears; an object never damaged this turn reads
		// false. The by-source refinement (wasDealtDamageThisTurnBySource)
		// is a separate token and stays unknown.
		return o.WasDealtDamageThisTurn
	case wordDealtDamageByThisGame:
		// Forge's wasDealtDamageByThisGame (bare, source-anchored): the
		// candidate object's game-long damage record names the bound
		// source. Source==0 is the unbound case and fails closed
		// (contextPredicateBound refuses to invert it beneath '!').
		return damageGameRecordHas(o.DamageTakenByGame, source)
	case wordDamagedBy:
		if key == "" {
			return damageGameRecordHas(o.DamageTakenThisTurnBy, source)
		}
		base := strings.TrimSuffix(key, ".YouCtrl")
		spec := base
		if strings.HasSuffix(key, ".YouCtrl") {
			if base == "Card" {
				spec += ".YouCtrl"
			} else {
				spec = "Creature." + base + "+YouCtrl"
			}
		}
		for _, srcID := range o.DamageTakenThisTurnBy {
			srcObj := g.Obj(srcID)
			if srcObj != nil && MatchesObjectCtx(g, spec, srcObj, sc) {
				return true
			}
		}
		return false
	case wordDealtDamageThisGameBy:
		// Forge's wasDealtDamageThisGameBy <ref> (the_fallen's walker half
		// Planeswalker.wasDealtDamageByThisGame-by-Self is the bare sibling
		// above): the candidate was dealt damage this game by any object
		// <ref> resolves to. The ref resolves through the SAME shared
		// referent switch the sharesTypeWith family uses; an unresolvable
		// ref yields no referent and fails closed, never widened.
		for _, t := range sharesTypeReferents(g, sc, key) {
			if t.IsPlayer {
				continue
			}
			if damageGameRecordHas(o.DamageTakenByGame, t.Obj) {
				return true
			}
		}
		return false
	case wordImprinted:
		// Forge's IsImprinted: the candidate is in the SOURCE object's
		// persistent imprint association -- state.Object.Imprinted (the
		// exiled cards ImprintCards$ recorded), ImprintTokens (the tokens
		// ImprintTokens$ True minted) or SeekFound (the cards an Alchemy
		// Seek associated). This is the SAME pile imprintPileTargets
		// resolves for Defined$ Imprinted, read as a membership test; a
		// source with no association matches nothing. Source==0 is the
		// unbound case and fails closed (contextPredicateBound refuses to
		// invert it beneath '!').
		src := g.Obj(sc.Source)
		if src == nil {
			return false
		}
		// Judge the association against the candidate's OWN zone: for an
		// ordinary live filter that is the live zone (and expires as before),
		// but a zone-change trigger hands this predicate the event's LKI
		// snapshot, whose zone is where the card was a moment ago -- so an
		// imprinted card leaving exile still reads IsImprinted.
		return imprintAssociationContainsCandidate(g, src, o)
	case wordDefenderCtrl:
		// Forge's DefenderCtrl: the object is controlled by the defending
		// player of the resolving combat trigger (TriggerContext
		// .DefendingPlayer, bound from the Attacks/AttackersDeclared event).
		// Outside such a trigger the role is absent and this matches
		// nothing -- the conservative direction, never an invented
		// defender. The unbound case is refused beneath '!' too
		// (contextPredicateBound).
		return o.Controller == sc.DefendingPlayer.Player
	case wordNotDefinedTargeted:
		// Forge's NotDefinedTargeted: the candidate is NOT one of the
		// resolving ability's targets (SpecContext.ResolutionTargets, the
		// resolving object's recorded Targets). A resolving ability with no
		// targets (an empty, non-nil list) admits every candidate --
		// correctly, since nothing was targeted. The resolving gate is
		// contextPredicateBound's: outside a resolution the predicate is
		// unbound and refused beneath '!' rather than inverting an absence
		// into an always-true match.
		for _, t := range sc.ResolutionTargets {
			if !t.IsPlayer && t.Obj == o.ID {
				return false
			}
		}
		return true
	case wordOpponentCtrl:
		// The bare Opponent object predicate: the candidate is controlled
		// by an opponent of the evaluating controller -- the object-side
		// twin of the player grammar's `Opponent` base (matchesPlayerSpec's
		// base case `p != you`). The corpus spells this as a PLAYER filter
		// (`Player.Opponent`, 761 lines) far more often than as an object
		// one, and its object twin is already covered by OppCtrl (1331
		// files); recognizing the bare word closes the census without
		// widening any existing spelling.
		return o.Controller != sc.You
	case wordEnchantedControllerCtrl:
		// Forge's EnchantedControllerCtrl: the candidate is controlled by the
		// controller of the permanent this Aura/Equipment source is attached
		// to. playerEnchantedController is the same resolver the player-side
		// Player.EnchantedController clause uses, so the two spellings cannot
		// drift. An absent link fails closed.
		if ctrl, ok := playerEnchantedController(g, sc.Source); ok && ctrl == o.Controller {
			return true
		}
		return false
	case wordHasNonBasicLandType:
		// Forge's hasANonBasicLandType: a land with at least one land type
		// outside CR 205.3i's five basic land types, read through hasTypeCtx so
		// the layer-derived type list and Changeling agree with every other
		// type read. "Nonbasic land type" is a SUBTYPE test, not the Basic
		// supertype and not the negation of hasABasicLandType: a Wastes (basic,
		// no land type) has no nonbasic land type and does not match, while a
		// Desert or Gate (both nonbasic land types) does. A non-land with a
		// granted land type is excluded by the Land test, exactly as Forge's
		// Card.hasANonBasicLandType requires the Land card type.
		if !hasTypeCtx(o, "Land", sc) {
			return false
		}
		for _, t := range chooseNonbasicLandTypes {
			if hasTypeCtx(o, t, sc) {
				return true
			}
		}
		return false
	case wordHasBasicLandType:
		// Forge's hasABasicLandType: a land with one of the five basic land
		// types (CR 205.3i), read through hasTypeCtx so the layer-derived type
		// list and Changeling agree with every other type read. A Wastes is a
		// basic land with NO basic land type and does not match; a non-land
		// with a granted land type is excluded by the Land test, exactly as
		// Forge's Card.hasABasicLandType requires the Land card type.
		if !hasTypeCtx(o, "Land", sc) {
			return false
		}
		for _, t := range chooseBasicLandTypes {
			if hasTypeCtx(o, t, sc) {
				return true
			}
		}
		return false
	case wordSameName:
		// Forge CardProperty "sameName": card.sharesNameWith(source). The
		// referent is SpecContext.Source as MatchesObjectCtx rewrote it: the
		// resolving ability's source card by default (Evil Twin's
		// ValidTgts$ Creature.sameName), or the Remembered./Targeted./
		// Triggered. context object the alternative's base prefix names
		// (Eradicate's Remembered.sameName, Bifurcate's Targeted.*,
		// Bloodbond March's Triggered.sameName). Both sides use their full
		// name characteristics: a split card off the stack contributes both
		// halves' names (CR 709.4) and a moved DFC only its front face.
		if sc.Source == 0 {
			return false
		}
		return sharesNameWithObject(o, g.Obj(sc.Source), sc)
	case wordAttachedTo:
		// Forge's AttachedTo <X>: this object (an Aura or Equipment) is
		// attached to something, and the permanent it is attached to (its
		// own AttachedTo id) satisfies the base <X>, OR -- for the
		// resolution-time referent argument -- is one of the live objects
		// the referent names (Targeted/ParentTarget/the triggered card or
		// attacker). An unattached object (AttachedTo == 0), or one whose
		// attachment is gone, matches nothing. This is the two-token
		// counterpart of attachedBy, which reads the SOURCE's AttachedTo to
		// find what the source attaches to; here we read the candidate
		// object's own AttachedTo. The dotted two-token "<class>.<qual>"
		// form narrows the attached object by its qualifier (YouCtrl:
		// attached to a permanent the spec's you controls -- Umbra Mystic);
		// the key was validated by attachedToArg, so the re-split here
		// cannot miss.
		// The player-referent family: the candidate enchants the named seat
		// (Lynde / Witchbane Orb's `Curse.AttachedTo You`). A player-attached
		// Curse has AttachedTo == 0 and HasAttachedPlayer true, so this branch
		// precedes the bare AttachedTo==0 guard below. The zone check mirrors
		// the Player.EnchantedBy sweep (a departed Aura must not continue to
		// enchant -- matchesPlayerSingleSpec's EnchantedBy case checks
		// ZBattlefield explicitly).
		if pref, ok := attachedToPlayerReferent(key); ok {
			if o.Zone != state.ZBattlefield || !o.HasAttachedPlayer {
				return false
			}
			seat, bound := attachedToReferentPlayer(g, sc, pref)
			if !bound {
				return false
			}
			return o.AttachedPlayer == seat
		}
		if o.AttachedTo == 0 {
			return false
		}
		if ref, ok := attachedToReferent(key); ok {
			// The referent resolved from SpecContext; contextPredicateBound
			// already refused an unbound one, so a false here is a real
			// "attached to something else", never an absence.
			ids, bound := attachedToReferentObjects(g, sc, ref)
			if !bound {
				return false
			}
			for _, id := range ids {
				if id == o.AttachedTo {
					return true
				}
			}
			return false
		}
		a := g.Obj(o.AttachedTo)
		if a == nil {
			return false
		}
		if class, qual, ok := strings.Cut(key, "."); ok {
			fn, is := predicates[qual]
			if !is {
				return false
			}
			return matchesBase(g, class, a, sc) && fn(g, a, sc.You, sc.Source)
		}
		return matchesBase(g, key, a, sc)
	case wordEnchantedBy:
		// Forge's two-token "EnchantedBy <Type>.<qual>": the candidate bears
		// an attached permanent of the named type whose qualifier holds
		// against that attached object. The qualifier bodies are the map's
		// own (Other: the attached Aura is not the resolving source -- for a
		// cast the source is not yet attached, so any current Aura
		// qualifies; for a static whose source IS the attached Aura, like
		// Face of Divinity, Face itself is excluded; YouCtrl: the attached
		// Aura is controlled by the spec's you). The key was validated by
		// enchantedByArg, so the re-split here cannot miss.
		typ, qual, ok := strings.Cut(key, ".")
		if !ok {
			return false
		}
		fn, is := predicates[qual]
		if !is {
			return false
		}
		return hasAttachmentMatching(g, o.ID, sc, typ, fn)
	}
	return false
}

// contextPredicateBound reports whether a context-bound classifier word has
// the binding its body reads. The pc1 families that read more than the
// object alone are NotDefinedTargeted (needs a resolution, so the resolving
// object's targets exist), DefenderCtrl (needs the combat trigger's captured
// defending player) and IsImprinted (needs a source whose imprint association
// is being read); wordAttachedTo's resolution-time referent argument needs its
// own binding (a target list or a remembered trigger object). matchPositive
// consults this before dispatching to wordMatches and returns unknown
// (ok=false) when the binding is absent, so BOTH the positive and the
// leading-'!' negated spelling fail closed -- a recognised-but-false body
// would otherwise let a negated token match every object. The always-bound
// families (wasDealtDamageThisTurn, Opponent) are not listed: their bodies
// need only the object and the evaluating controller, both of which are
// always present. key is the classifier's argument (wordAttachedTo's <ref>),
// empty for the families that carry none.
func contextPredicateBound(g *state.Game, kind wordKind, key string, sc SpecContext) bool {
	switch kind {
	case wordNotDefinedTargeted:
		return sc.Resolving
	case wordDefenderCtrl:
		return sc.DefendingPlayer.IsPlayer
	case wordEnchantedControllerCtrl:
		// The body resolves the bearer of the SOURCE attachment, exactly as
		// the player grammar's Player.EnchantedController clause does. With no
		// source, or a source that is not attached, there is no controller to
		// name: refuse the token beneath '!' too rather than invert an absent
		// link into a match.
		if sc.Source == 0 {
			return false
		}
		o := g.Obj(sc.Source)
		return o != nil && o.AttachedTo != 0 && g.Obj(o.AttachedTo) != nil
	case wordImprinted, wordChosenColor:
		return sc.Source != 0
	case wordDealtDamageByThisGame:
		// The bare word names the bound source; without one it must not
		// invert an absent referent beneath '!'.
		return sc.Source != 0
	case wordDamagedBy:
		if key == "" {
			return sc.Source != 0
		}
		base := strings.TrimSuffix(key, ".YouCtrl")
		return base == "Card" || base == "Giant" || base == "Spider"
	case wordDealtDamageThisGameBy:
		// The argument form binds through <ref>; an unresolvable ref names no
		// source at all, so both the positive and the '!'-negated spelling
		// must fail closed rather than invert an empty referent.
		return len(sharesTypeReferents(g, sc, key)) > 0
	case wordAttachedTo:
		if ref, ok := attachedToReferent(key); ok {
			_, bound := attachedToReferentObjects(g, sc, ref)
			return bound
		}
		if ref, ok := attachedToPlayerReferent(key); ok {
			_, bound := attachedToReferentPlayer(g, sc, ref)
			return bound
		}
	}
	return true
}

// nonPredicate reports whether predicate p has the generic negation shape
// non<X>, and how to evaluate it: the classifier to negate and its key. For
// <X> a colour name it is wordColor (with the WUBRG letter); for <X> a
// type/supertype/subtype word in the corpus vocabulary it is wordType; for
// <X> Colorless it is wordColorless (so nonColorless is "has at least one
// colour"); for <X> CopiedSpell it is wordCopiedSpell (so nonCopiedSpell is
// "is not a copy of a spell", CR 707). The caller negates by evaluating
// wordMatches and inverting. ok is
// false for a p that is not a non<X> shape at all, or whose <X> is none of a
// colour, a known type word, Colorless, or CopiedSpell -- the caller must
// treat that as an
// unknown predicate and fail closed, never as an always-true !hasType. Only
// wordColor / wordType / wordColorless / wordCopiedSpell negate; a
// nonMultiColor remains unknown (nonChosenCard is handled by matchPositive,
// which holds the chosen-list context this classifier lacks). The four legacy
// non* entries in `predicates`
// (nonLand/nonCreature/nonBasic/nonBlack) are matched there first and never
// reach this path, but this path reproduces their result exactly, so the
// handwritten entries could be deleted without changing behaviour.
func nonPredicate(p string) (kind wordKind, key string, ok bool) {
	x, has := strings.CutPrefix(p, "non")
	if !has || x == "" {
		return wordUnknown, "", false
	}
	kind, key = wordPredicate(x)
	switch kind {
	case wordColor, wordType, wordColorless, wordCopiedSpell:
		return kind, key, true
	}
	return wordUnknown, "", false
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
	if _, ok := numericPred(p, nil, &state.Object{}, SpecContext{}); ok {
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
// the walker's resume arm reads.
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
		for _, n := range sc.EffectiveNames {
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
		return MatchesSpecCtx(g, strings.TrimSpace(sa.Params["ValidTgts"]), o.ID,
			SpecContext{You: spell.Controller, Source: spell.ID}), true
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
		// pruneGoads / expireTurnGoads), and SpecContext.StaticGoads -- the
		// live Goad$ True static route (a printed Shiny Impetus static and an
		// AddStaticAbilities$/StaticAbilities$ granted one alike) that the
		// engine's rules tier derives on demand and the caller binds. A
		// context with no binding answers the event-backed half only, exactly
		// the pre-staticgoad1 read; a bound context never needs a rules
		// pointer for it (immutable data, the DerivedTypes seam).
		if len(o.Goads) > 0 {
			return true, true
		}
		return sc.StaticGoads[o.ID], true
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
		// is the greatest on the battlefield. Net power is read the same way
		// numericPred's power predicates read it (face power plus +1/+1
		// counters); a continuous-effect power pump is not visible from here
		// -- recorded as a known limitation in AGENTS.md.
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
		mine := objectPower(o)
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
			if objectPower(other) > mine {
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
		if sc.ExtraKeywords == nil {
			has := objectHasKeyword(o, kp.keyword)
			if kp.negated {
				has = !has
			}
			return has, true
		}
		has := false
		for _, x := range sc.ExtraKeywords {
			if strings.EqualFold(cards.KeywordHead(x), kp.keyword) {
				has = true
				break
			}
		}
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
	if fn, ok := predicates[p]; ok {
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

// canReceiveCounter answers Forge's Card.canReceiveCounters <kind>: the object
// can have a counter of kind placed on it. A +1/+1 (or -1/-1) counter is
// hostable by a creature -- read through EffectiveIsCreature so a face-down
// permanent's folded set type decides -- and any other counter kind by any
// battlefield permanent with a face. Off the battlefield (or a face-less
// object) never matches.
func canReceiveCounter(kind string, o *state.Object) bool {
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
		return false
	}
	switch strings.ToUpper(kind) {
	case "P1P1", "M1M1":
		return o.EffectiveIsCreature()
	}
	return true
}

// hasType reads a printed type plus Changeling's type-defining ability. The
// rules package supplies SpecContext.Types when a layer-derived type list is
// available; this fallback remains deliberately useful to effects, which sits
// below rules and cannot import the layer engine.
func hasType(o *state.Object, t string) bool { return hasTypeID(o, t, 0) }

// hasTypeID is hasType with t's precompiled cards.InternTypeWord ordinal (0 =
// none): the printed type-line test is one bit test when the face is bound.
func hasTypeID(o *state.Object, t string, id cards.TypeWordID) bool {
	d, f := hasTypePrinted(o, t, id)
	if d != typeUndecided {
		return d == typeYes
	}
	// Intrinsic type-defining abilities, answered in every zone (CR 613.4a):
	// Changeling's keyword and the characteristic-defining
	// AddAllCreatureTypes$ True static (Mistform Ultimus). Both go through
	// the positive subtype vocabulary, so a non-creature word (Arcane,
	// Alara, Ajani) can never leak, and neither materialises subtypes into
	// the derived type list.
	return (f.HasKeywordID("Changeling", kwChangeling) || f.AllCreatureTypesCDA()) && changelingType(t)
}

// hasTypeSub is hasType with changelingType(t) supplied precomputed as sub
// (the compiled filter form classifies its type words once). Every function
// involved is pure, so reading sub first only skips the keyword probe for a
// word no Changeling can grant.
func hasTypeSub(o *state.Object, t string, id cards.TypeWordID, sub bool) bool {
	d, f := hasTypePrinted(o, t, id)
	if d != typeUndecided {
		return d == typeYes
	}
	return sub && (f.HasKeywordID("Changeling", kwChangeling) || f.AllCreatureTypesCDA())
}

type typeDecision uint8

const (
	typeUndecided typeDecision = iota
	typeYes
	typeNo
)

// hasTypePrinted is hasType up to (not including) its intrinsic-CDA tail:
// the bestow/reconfigure switches and the printed type line. Undecided means
// the answer is the CDA tail's, read off the returned face.
func hasTypePrinted(o *state.Object, t string, id cards.TypeWordID) (typeDecision, *cards.Face) {
	f := o.Face()
	if f == nil {
		return typeNo, nil
	}
	// CR 702.114e: a bestowed card attached to a creature is an Aura, not a
	// creature, in every filter read (Count$Valid, target offer, cost
	// candidates, statics' Affected$). Derived live state
	// (state.Object.BestowedAttached); the layer walk sees the same switch
	// through rules/layers.go's bestowedTypeSwitch, and hasTypeCtx inherits
	// this gate through the hasType call below.
	//
	// CR 702.114c: a card cast with its bestow ability is an Aura SPELL, not a
	// creature spell -- the same switch one zone earlier, derived from the
	// stack zone and the pay-time FlagBestowed provenance
	// (state.Object.BestowedAuraSpell). This is what keeps a bestowed cast
	// from firing "whenever you cast a creature spell" triggers.
	if o.BestowedAttached() || o.BestowedAuraSpell() {
		if strings.EqualFold(t, "Aura") {
			return typeYes, f
		}
		if strings.EqualFold(t, "Creature") {
			return typeNo, f
		}
	}
	// CR 702.150c: a Reconfigure card attached to a creature is not a
	// creature, in the same every-filter-read sense (the target ask's
	// ValidTgts$ Creature, a Count$Valid Creature census, the combat
	// eligibility scans). Equipment and Artifact stay true -- they are the
	// printed face's own types and the attached form keeps them.
	if o.ReconfiguredAttached() && !(o.FaceDown && o.Zone == state.ZBattlefield) {
		if strings.EqualFold(t, "Creature") {
			return typeNo, f
		}
	}
	if f.TypeLineHas(t, id) {
		return typeYes, f
	}
	return typeUndecided, f
}

// typePredicate handles the legacy predicate-map entries whose meaning is a
// type test. Keeping them in one context-aware path ensures layer-4 derived
// types and Changeling apply consistently to both positive and negated forms.
func typePredicate(p string, g *state.Game, o *state.Object, sc SpecContext) (bool, bool) {
	switch p {
	case "Legendary", "Basic", "Snow":
		return hasTypeCtx(o, p, sc), true
	case "nonLand":
		return !hasTypeCtx(o, "Land", sc), true
	case "nonCreature":
		return !hasTypeCtx(o, "Creature", sc), true
	case "nonBasic":
		return !hasTypeCtx(o, "Basic", sc), true
	case "ChosenType":
		s := g.Obj(sc.Source)
		return s != nil && s.ChosenType != "" && hasTypeCtx(o, s.ChosenType, sc), true
	case "IsNotChosenType":
		s := g.Obj(sc.Source)
		return s != nil && s.ChosenType != "" && !hasTypeCtx(o, s.ChosenType, sc), true
	case "ChosenCtrl":
		return chosenCtrlMatches(g, o, sc.Source), true
	}
	return false, false
}

// chosenCtrlMatches is the ONE implementation of the ChosenCtrl predicate
// (controlled by the source's secretly chosen player) the context-aware
// typePredicate path and the census-only predicates-map entry share, so the
// matcher and UnknownPredicates cannot drift. A source with no chosen player
// fails closed.
func chosenCtrlMatches(g *state.Game, o *state.Object, src state.ObjID) bool {
	s := g.Obj(src)
	if s == nil {
		return false
	}
	for _, t := range s.Chosen {
		if t.IsPlayer && o.Controller == t.Player {
			return true
		}
	}
	return false
}

// hasTypePredicateCtx matches a type word, or a space-separated multi-word
// subtype (Time Lord) as every one of its words. strings.Cut rather than
// strings.Fields: this is the filter hot path and must not allocate
// (TestSimpleFilterMatchingDoesNotAllocate).
func hasTypePredicateCtx(o *state.Object, t string, sc SpecContext) bool {
	return hasTypePredicateCtxPtr(o, t, &sc)
}

// hasTypePredicateCtxPtr is hasTypePredicateCtx through a pointer (see
// hasTypeCtxPtr).
func hasTypePredicateCtxPtr(o *state.Object, t string, sc *SpecContext) bool {
	if t == "" {
		return false
	}
	for {
		word, rest, more := strings.Cut(t, " ")
		if word != "" && !hasTypeCtxPtr(o, word, sc) {
			return false
		}
		if !more {
			return true
		}
		t = rest
	}
}

func hasTypeCtx(o *state.Object, t string, sc SpecContext) bool {
	return hasTypeCtxPtr(o, t, &sc)
}

// hasTypeCtxPtr is hasTypeCtx reading the context through a pointer, so the
// compiled predicate paths that already hold a *SpecContext do not copy the
// whole context per type test (see hasEffectiveNamePtr).
func hasTypeCtxPtr(o *state.Object, t string, sc *SpecContext) bool {
	return hasTypeCtxPtrID(o, t, 0, sc)
}

// hasTypeCtxPtrID is hasTypeCtxPtr with t's precompiled type-word ordinal
// (0 = none), which only the printed-face fallback reads.
func hasTypeCtxPtrID(o *state.Object, t string, id cards.TypeWordID, sc *SpecContext) bool {
	// ExtraTypes is the layer walk's accumulating type list for the ONE
	// object being matched: a plain value slice, deliberately not a callable
	// resolver. Any call made through a SpecContext field makes escape
	// analysis leak the whole context to the heap on every hot-path
	// construction (the statics/action hotspot pins measure exactly that),
	// while a slice field is read-only and allocation-free. It is checked
	// first because it is the layer walk's OWN types-so-far list, which can
	// differ from the published table mid-walk.
	for _, x := range sc.ExtraTypes {
		if strings.EqualFold(x, t) {
			return true
		}
	}
	// When the layer walk bound a types-so-far list, it is authoritative and
	// the pre-existing printed-face/Changeling fallback below is kept verbatim.
	// The published table is not consulted in that case: it may carry a type a
	// LATER effect grants, which would break the walk's ordering (rules/layers.go
	// clears sc.DerivedTypes for the same reason, but this guard keeps the
	// contract even for a caller that sets ExtraTypes without clearing it).
	if sc.ExtraTypes != nil {
		return hasTypeID(o, t, id)
	}
	// Outside the walk a published layer-4 entry makes the object's DERIVED
	// type list authoritative for type words: it already carries the printed
	// types the effect kept (rules' typeCharacteristics folds them in), so a
	// RemoveCardTypes$ cannot be resurrected by a fallback to the printed face,
	// while a granted word (a static's AddTypes$, AddAllCreatureTypes$) is
	// found exactly as the layer walk finds it. Only the intrinsic CDAs the
	// list deliberately does not materialise (Changeling's keyword, Mistform
	// Ultimus's AddAllCreatureTypes$ CDA) are added back on this path.
	if types, ok := derivedTypesForPtr(o, sc); ok {
		for _, x := range types {
			if strings.EqualFold(x, t) {
				return true
			}
		}
		return intrinsicCDAType(o, t)
	}
	// No derived entry: the printed face plus intrinsic CDAs, as before.
	return hasTypeID(o, t, id)
}

// hasTypeCtxSub is hasTypeCtx with changelingType(t) precomputed as sub: the
// same reads in the same order, the intrinsic-CDA tails taking sub.
func hasTypeCtxSub(o *state.Object, t string, id cards.TypeWordID, sub bool, sc *SpecContext) bool {
	for _, x := range sc.ExtraTypes {
		if strings.EqualFold(x, t) {
			return true
		}
	}
	if sc.ExtraTypes != nil {
		return hasTypeSub(o, t, id, sub)
	}
	for _, d := range sc.DerivedTypes {
		if d.ID != o.ID {
			continue
		}
		// derivedTypesFor's first entry for o is authoritative.
		for _, x := range d.Types {
			if strings.EqualFold(x, t) {
				return true
			}
		}
		if !sub {
			return false
		}
		f := o.Face()
		return f != nil && (f.HasKeywordID("Changeling", kwChangeling) || f.AllCreatureTypesCDA())
	}
	return hasTypeSub(o, t, id, sub)
}

// changelingType reports whether t is an actual creature subtype. This uses
// a positive authoritative vocabulary rather than treating every type word
// outside an exclusion list as a creature type: Arcane, Alara, and Ajani are
// respectively spell, plane, and planeswalker subtypes, not types Changeling
// grants.
func changelingType(t string) bool { return CreatureTypeWords(t) }

// intrinsicCDAType is hasType's intrinsic type-defining-ability branch on its
// own (Changeling's keyword, and the characteristic-defining
// AddAllCreatureTypes$ True static -- Mistform Ultimus). A layer-4 derived type
// list deliberately never materialises these subtypes, so hasTypeCtx must still
// answer them when the published table is authoritative for the object; the
// positive vocabulary keeps a non-creature word out.
func intrinsicCDAType(o *state.Object, t string) bool {
	f := o.Face()
	if f == nil {
		return false
	}
	return (f.HasKeywordID("Changeling", kwChangeling) || f.AllCreatureTypesCDA()) && changelingType(t)
}

// kwChangeling is Changeling's interned keyword head (cards.InternKeywordHead).
var kwChangeling = cards.InternKeywordHead("Changeling")

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

// noResolve is the resolver used whenever a caller has none of its own
// (MatchesSpec/MatchesSpecFrom, and the shape-only checks in
// UnknownPredicates/KnownPredicates below): every non-literal numeric RHS is
// a recognised shape that never matches, never a hard "unknown predicate".
func noResolve(string) (int32, bool) { return 0, false }

// numericPred handles the "<field><CMP><n>" family: powerLE2, cmcGE3, and so
// on, plus a right-hand side that is not a literal integer ("cmcEQY",
// "cmcEQChosen", "powerGEX"), which resolve looks up by name -- typically an
// {X} paid or a Chosen* value SpecContext.Resolve closes over. Returns
// ok=false when the token is not of this shape at all; ok=true with
// result=false when the shape is recognised but the RHS did not resolve, so
// a filter spec is either a hard "no" or "not this predicate", never a
// silent match.
// objectPower is the one net-power read the filter grammar shares (face
// power plus the summed P/T counter deltas of every counter kind --
// numericPred's power predicates and the greatestPower classifier both use
// it). A continuous-effect power pump is not visible from the filter path;
// the limitation is recorded in AGENTS.md.
func objectPower(o *state.Object) int {
	f := o.Face()
	if f == nil {
		return 0
	}
	dp, _ := o.CounterPTTotals()
	return f.Power() + int(dp)
}

// objectToughness is objectPower's counterpart, the same face-plus-counter
// read summed over every P/T counter kind the toughness family uses.
func objectToughness(o *state.Object) int {
	f := o.Face()
	if f == nil {
		return 0
	}
	_, dt := o.CounterPTTotals()
	return f.Toughness() + int(dt)
}

// objectBasePower / objectBaseToughness are the object-alone BASE P/T read:
// the printed face value with NO counters. They are the fallback for a filter
// call whose SpecContext carries no rules-bound base characteristic (a direct
// effects-level match, or the census probe). The rules tier binds the real
// base through SpecContext.BasePower/BaseToughness -- the value through layer
// 7b, before 7c modifies and 7d counters (CR 613.4) -- and that binding is
// authoritative when present. The fallback deliberately never adds a +1/+1
// counter, so a counter can never move a base-power comparison on a caller
// that forgot to bind.
func objectBasePower(o *state.Object) int {
	f := o.Face()
	if f == nil {
		return 0
	}
	return f.Power()
}

// objectBaseToughness is objectBasePower's toughness counterpart.
func objectBaseToughness(o *state.Object) int {
	f := o.Face()
	if f == nil {
		return 0
	}
	return f.Toughness()
}

// objectManaValue is the one mana-value read the extreme-CMC classifier
// shares with the rest of the engine: the compiled face's CMC, the same
// Face().Cmc() read manaValueOf, cascade and the Count$ walkers use. It is
// the analogue of objectPower for greatestCMC/lowerCMC, so the two extreme
// classifiers cannot drift on what a card's mana value is.
func objectManaValue(o *state.Object) int {
	f := o.Face()
	if f == nil {
		return 0
	}
	return int(f.Cmc())
}

// cmcSetMember reports whether o is in the comparison set a greatestCMC_<prop>
// suffix names, mirroring Forge's CardProperty branch: NonLandPermanent is the
// nonland-permanent predicate, and every other prop is the named card type
// (Forge's CardLists.getType -> CardPredicates.isType). An empty prop is not a
// shape Forge produces (greatestCMC_ always carries a suffix), so it fails
// closed to no set rather than widening to "every battlefield card".
func cmcSetMember(o *state.Object, prop string) bool {
	switch prop {
	case "":
		return false
	case "NonLandPermanent":
		return !hasType(o, "Land")
	case "Permanent":
		return true
	}
	return hasType(o, prop)
}

func numericPred(name string, g *state.Game, o *state.Object, sc SpecContext) (result, ok bool) {
	resolve := sc.Resolve
	if resolve == nil {
		resolve = noResolve
	}
	// counters_<CMP><n>_<KIND>: a counter-kind comparison, e.g. counters_EQ0_P1P1
	// ("no +1/+1 counters", the Undying condition). Reads the object's current
	// counter count of KIND off the object it is applied to -- which for a
	// zone-change trigger is the LKI snapshot, so a "dies" condition sees what
	// the permanent had the moment it left the battlefield, not the reset state
	// Move leaves behind (events/apply.go's Move clears Counters).
	if strings.HasPrefix(name, "counters_") {
		rest := name[len("counters_"):]
		// Rest is "<CMP><n>_<KIND>"; at minimum "EQ0_A".
		if len(rest) < 4 {
			return false, false
		}
		cmp := rest[:2]
		numStr, kind, okSplit := strings.Cut(rest[2:], "_")
		if !okSplit || kind == "" {
			return false, false
		}
		n, err := strconv.Atoi(numStr)
		if err != nil {
			v, resolved := resolve(numStr)
			if !resolved {
				return false, true // recognised shape, unresolvable RHS never matches
			}
			n = int(v)
		}
		have := o.Counter(kind)
		target := int32(n)
		switch cmp {
		case "LE":
			return have <= target, true
		case "GE":
			return have >= target, true
		case "EQ":
			return have == target, true
		case "LT":
			return have < target, true
		case "GT":
			return have > target, true
		}
		return false, false
	}
	// The four characteristic operands a power/toughness comparison may name, on
	// the candidate object this spec is matched against. `power`/`toughness`
	// are the CURRENT derived values; `basePower`/`baseToughness` are the base
	// through layer 7b (CR 613.4: printed or characteristic-defining, then a
	// 7b set, BEFORE 7c modifies and 7d counters). When the rules tier bound
	// the candidate's layer-derived values (SpecContext.HasDerivedPT /
	// HasBasePT, set by rules' matchesSpec) those are authoritative; an
	// unbound context -- a direct filter call or the census probe -- keeps the
	// object-alone read the power predicates always had (printed face plus
	// P1P1 counters for current, printed face alone for base).
	currentPower := func() int {
		if sc.HasDerivedPT {
			return int(sc.DerivedPower)
		}
		return objectPower(o)
	}
	currentToughness := func() int {
		if sc.HasDerivedPT {
			return int(sc.DerivedToughness)
		}
		return objectToughness(o)
	}
	basePowerValue := func() int {
		if sc.HasBasePT {
			return int(sc.BasePower)
		}
		return objectBasePower(o)
	}
	baseToughnessValue := func() int {
		if sc.HasBasePT {
			return int(sc.BaseToughness)
		}
		return objectBaseToughness(o)
	}
	// fieldValue reads one of the four characteristic operands by name. It is
	// the one place the RHS-operand spelling and the LHS field spelling agree,
	// so `powerGTbasePower` and `basePowerEQ1` read the same values. The
	// capitalised RHS spellings (`Power`/`Toughness`/`BasePower`/
	// `BaseToughness`) are Forge's alias for the same operand.
	fieldValue := func(operand string) (int, bool) {
		switch operand {
		case "power", "Power":
			return currentPower(), true
		case "toughness", "Toughness":
			return currentToughness(), true
		case "basePower", "BasePower":
			return basePowerValue(), true
		case "baseToughness", "BaseToughness":
			return baseToughnessValue(), true
		}
		return 0, false
	}
	// applyCmp folds the comparison operator. `NOT` is Forge's "not equal"
	// spelling (powerNOTbasePower, "power different from its base power");
	// the other four are the two-character operators. An unrecognised operator
	// reports ok=false so a caller can distinguish "not this shape" from a
	// recognised-but-false comparison.
	applyCmp := func(cmp string, lhs, rhs int) (result, ok bool) {
		switch cmp {
		case "LE":
			return lhs <= rhs, true
		case "GE":
			return lhs >= rhs, true
		case "EQ":
			return lhs == rhs, true
		case "LT":
			return lhs < rhs, true
		case "GT":
			return lhs > rhs, true
		case "NOT":
			return lhs != rhs, true
		}
		return false, false
	}
	for _, field := range [...]string{"power", "toughness", "cmc", "basePower", "baseToughness"} {
		// The P/T half of this vocabulary is ptNumericFields, which
		// SpecReadsPT's dependency test reads through ptNumericField; cmc is
		// the evaluator's one non-P/T field and is matched here only.
		if !strings.HasPrefix(name, field) {
			continue
		}
		rest := name[len(field):]
		if len(rest) < 3 {
			return false, false
		}
		cmp, numStr := rest[:2], rest[2:]
		if strings.HasPrefix(rest, "NOT") {
			cmp, numStr = "NOT", rest[3:]
			if numStr == "" {
				return false, false
			}
		}
		// powerLTtoughness / powerGTbasePower / powerNOTbasePower (and the
		// mirrors): compare two characteristics instead of a numeric RHS
		// (Assault Formation, Bedrock Tortoise, Baird Argivian Recruiter).
		// The shape is recognised before any object read, so UnknownPredicates'
		// nil-object probe resolves it and the matcher and the census cannot
		// disagree.
		if lhs, lhsOK := fieldValue(field); lhsOK {
			if rhs, rhsOK := fieldValue(numStr); rhsOK {
				return applyCmp(cmp, lhs, rhs)
			}
		}
		n, err := strconv.Atoi(numStr)
		if err != nil {
			v, resolved := resolve(numStr)
			if !resolved {
				return false, true // recognised shape, unresolvable RHS never matches
			}
			n = int(v)
		}
		f := o.Face()
		if f == nil {
			return false, true
		}
		var have int
		switch field {
		case "power":
			have = currentPower()
		case "toughness":
			have = currentToughness()
		case "basePower":
			have = basePowerValue()
		case "baseToughness":
			have = baseToughnessValue()
		case "cmc":
			// CR 202.3e: {X} counts as its chosen value in a spell's mana
			// value. A caller that has the chosen X in hand (the CR 601.2e
			// post-announcement cast-illegality recheck) passes it through
			// SpecContext.ManaValue; otherwise the printed cost is used,
			// which counts an un-chosen X as 0, exactly as the offer-time
			// 601.3a check must.
			if sc.HasManaValue {
				have = int(sc.ManaValue)
			} else {
				have = int(parseCMC(f.ManaCost))
			}
		}
		return applyCmp(cmp, have, n)
	}
	return false, false
}

var cmcBraceNormalizer = strings.NewReplacer("{", " ", "}", " ")

// parseCMC counts a mana cost's converted value without importing rules.
func parseCMC(cost string) int32 {
	cost = cmcBraceNormalizer.Replace(cost)
	if strings.EqualFold(strings.TrimSpace(cost), "no cost") {
		return 0
	}
	var n int32
	for sym := range strings.FieldsSeq(cost) {
		if v, err := strconv.Atoi(sym); err == nil {
			n += int32(v)
			continue
		}
		if sym != "X" {
			n++
		}
	}
	return n
}

// sameNameContextBase recognises only the three base-prefix forms which
// carry sameName's referent. Keeping this rewrite name-specific is important:
// Remembered.*, Targeted.*, and Triggered.* have many unrelated predicates
// whose grammar and behaviour this task must not expand.
func sameNameContextBase(base, rest string) bool {
	if !strings.HasPrefix(base, "Remembered") &&
		!strings.HasPrefix(base, "Targeted") &&
		!strings.HasPrefix(base, "Triggered") {
		return false
	}
	return hasPredicate(rest, "sameName")
}

func hasPredicate(rest, want string) bool {
	for p := range strings.SplitSeq(rest, "+") {
		if p == want {
			return true
		}
	}
	return false
}

// sameNameContextReferent resolves the object whose name a sameName context
// base names. An absent binding fails closed rather than falling back to the
// ability source.
func sameNameContextReferent(g *state.Game, base string, sc SpecContext) (state.ObjID, bool) {
	switch {
	case strings.HasPrefix(base, "Remembered"):
		for _, t := range sc.Remembered {
			if !t.IsPlayer && t.Obj != 0 && g.Obj(t.Obj) != nil {
				return t.Obj, true
			}
		}
	case strings.HasPrefix(base, "Targeted"):
		for _, t := range sc.ResolutionTargets {
			if !t.IsPlayer && t.Obj != 0 && g.Obj(t.Obj) != nil {
				return t.Obj, true
			}
		}
	case strings.HasPrefix(base, "Triggered"):
		if sc.TriggerCard != 0 && g.Obj(sc.TriggerCard) != nil {
			return sc.TriggerCard, true
		}
	}
	return 0, false
}

// isPermanentCard is Forge's card.isPermanent() reading used only by
// Targeted.Permanent+sameName: a battlefield object is permanent, and away
// from the battlefield a card's printed type decides it.
func isPermanentCard(o *state.Object) bool {
	if o.Zone == state.ZBattlefield {
		return true
	}
	f := o.Face()
	return f != nil && f.IsPermanent()
}

// matchesBase handles the base type, including a "non" prefix.
func matchesBase(g *state.Game, base string, o *state.Object, sc SpecContext) bool {
	if neg := strings.TrimPrefix(base, "non"); neg != base {
		return !matchesBase(g, neg, o, sc)
	}
	switch base {
	case "Any":
		return hasTypeCtx(o, "Creature", sc) || hasTypeCtx(o, "Planeswalker", sc) || hasTypeCtx(o, "Battle", sc)
	case "Card":
		return true
	case "Permanent":
		return o.Zone == state.ZBattlefield
	case "Affinity":
		// CR 702.41: Forge uses the keyword name as a filter base for
		// "a permanent with affinity" (Sojourner's Enforcermite), not as
		// a card type. Prefer the layer-derived keyword list when rules has
		// supplied one; the printed face is the effects-tier fallback.
		if sc.ExtraKeywords != nil {
			for _, k := range sc.ExtraKeywords {
				if strings.EqualFold(cards.KeywordHead(k), "Affinity") {
					return true
				}
			}
			return false
		}
		return o.Face() != nil && o.Face().HasKeyword("Affinity")
	case "PermanentCard":
		// This internal base spelling is selected by rules' target census
		// (targetSpecForZone) and Dig windows (permanentCardSpec) for Forge's
		// `Permanent` base evaluated AWAY from the battlefield, and by rules'
		// SpellCast trigger matcher (spellCastPermanentSpec) for the permanent
		// SPELL a "cast a permanent spell" trigger evaluates on the stack. A
		// permanent CARD is anything whose printed face is a permanent type
		// (CR 109.2) wherever the object sits; the bare `Permanent` case
		// above keeps the on-the-battlefield reading every other filter
		// depends on.
		return o.Face() != nil && o.Face().IsPermanent()
	case "Spell":
		return o.Zone == state.ZStack || sc.AsStack
	case "SpellAbility":
		// Forge's SpellAbility base (ValidSource$ SpellAbility.OppCtrl on the
		// "becomes the target of a spell or ability" family -- Thunderbreak
		// Regent and 51 more files): any spell or ability object on the stack.
		// The shared filter draws the Spell/SpellAbility line by zone alone;
		// the card-spell-only distinction TargetType$ Spell draws
		// (rules/stack.go's stack kind tokens) is that machinery's own, not
		// this one's.
		return o.Zone == state.ZStack
	}
	return hasTypeCtx(o, base, sc)
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
	// ExtraKeywords optionally supplies the layer-derived KEYWORD list for the
	// ONE object the spec is being matched against (a value slice, same
	// rationale as ExtraTypes). When non-nil it is authoritative for the
	// `with<Keyword>`/`without<Keyword>` predicates: it already holds printed
	// keywords, marker-counter grants and layer-6 AddKeyword$ grants, so a
	// caller with the layer walk in hand can gate on a granted keyword
	// (kw:Flanking's blocker check, Cavalry Master's `withFlanking` lord).
	// nil keeps the object-alone read (printed face plus counters).
	//
	// StaticGoads is the per-board complement: the set of battlefield ids a
	// live Goad$ True static currently goads (printed S: statics and
	// AddStaticAbilities$/StaticAbilities$ granted ones alike), derived by
	// the engine's rules tier and bound by the caller that holds it. The
	// IsGoaded predicate unions it with the event-backed o.Goads list; a nil
	// map keeps the object-alone read (the same no-binding convention
	// Remembered keeps). Immutable data bound per evaluation, never a
	// callable: the same escape-analysis rationale as EffectiveNames.
	StaticGoads   map[state.ObjID]bool
	ExtraKeywords []string
	// TargetableObjects is the rules tier's immutable snapshot of objects this
	// triggered spell can currently target under full rules legality.
	TargetableObjects []state.ObjID
	// EffectiveNames optionally supplies the layer-3 derived names (SetName$,
	// CR 613.1d) in force on the battlefield -- rules' layer walk computes
	// them and binds the result on every SpecContext it builds. Ordinary
	// filter callers leave it nil and fall back to the printed face name
	// above. Like ExtraTypes it is a plain value slice and deliberately not a
	// callable resolver: a call made through a SpecContext field makes escape
	// analysis leak the whole context (its Resolve closure included) to the
	// heap on every hot-path construction. It is also NOT a back-pointer into
	// rules: the slice is an immutable snapshot, so a context built from one
	// game can never read another game's board.
	//
	// Entries are only ever the renamed objects (a handful at most, nil on the
	// overwhelmingly common board), so the linear scan below is cheaper than
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
	// never a back-pointer into rules: a call made through a SpecContext field
	// makes escape analysis leak the whole context (its Resolve closure
	// included) to the heap on every hot-path construction, and a slice built
	// from one game can never read another game's board. Entries are only ever
	// the objects whose derived list differs from the printed face (nil on the
	// overwhelmingly common board), so the linear scan below is cheaper than
	// building a map.
	DerivedTypes []ObjectTypes
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
	return sc != nil && (sc.Resolve != nil || sc.Resolving)
}

// ObjectTypes binds one object to its layer-4 derived type list (CR
// 613.1d/613.1c). The list is the SAME shape rules' layer walk builds and
// Derived carries -- printed types (or the CR 708.5 face-down set) plus every
// granted/removed word -- so the ordinary filter grammar and the layer walk
// cannot disagree about an object's types.
type ObjectTypes struct {
	ID    state.ObjID
	Types []string
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
	for _, n := range sc.EffectiveNames {
		if n.ID == o.ID {
			return n.Name != ""
		}
	}
	return false
}

// matchesEffectiveName reports whether o's bound layer-3 name is name.
func matchesEffectiveName(o *state.Object, name string, sc SpecContext) bool {
	for _, n := range sc.EffectiveNames {
		if n.ID == o.ID {
			return n.Name != "" && n.Name == name
		}
	}
	return false
}

// hasDerivedTypeEntry reports whether sc binds a layer-4 derived type list for
// o. A spec that names a granted type is answered by the textual oracle, the
// same discipline the layer walk and hasEffectiveName keep. The compiled
// sidecar's type paths are now ExtraTypes/DerivedTypes-aware too (they route
// through hasTypeCtx), so both oracles agree; this gate keeps the textual
// oracle authoritative for a published derived list, where its ordering rules
// (layer4types.go) are explicit. It answers a BOOLEAN and never returns the
// list, so escape analysis does not summarise the whole context as leaking
// (the EffectiveNames contract above).
func hasDerivedTypeEntry(o *state.Object, sc SpecContext) bool {
	_, ok := derivedTypesForPtr(o, &sc)
	return ok
}

// hasDerivedTypeEntryPtr is hasDerivedTypeEntry through a pointer (see
// hasEffectiveNamePtr).
func hasDerivedTypeEntryPtr(o *state.Object, sc *SpecContext) bool {
	_, ok := derivedTypesForPtr(o, sc)
	return ok
}

// derivedTypesFor returns the layer-4 derived type list sc binds for o. It is a
// plain field read of immutable DATA, the same shape hasEffectiveName keeps:
// copying a slice header out of a struct field is what an inlineable caller
// does, not what leaks a context. It never hands back a callable and never
// reaches rules, so an effects call answered here depends on the event fold
// alone.
func derivedTypesFor(o *state.Object, sc SpecContext) ([]string, bool) {
	return derivedTypesForPtr(o, &sc)
}

// derivedTypesForPtr is derivedTypesFor through a pointer (see
// hasEffectiveNamePtr).
func derivedTypesForPtr(o *state.Object, sc *SpecContext) ([]string, bool) {
	if o == nil {
		return nil, false
	}
	for _, d := range sc.DerivedTypes {
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
		if strings.TrimSpace(spell.Ability.Params["ValidTgts"]) != "" {
			return spell.Ability
		}
		return nil
	}
	f := spell.Face()
	if f == nil {
		return nil
	}
	sa := f.SpellAbility()
	if sa == nil || strings.TrimSpace(sa.Params["ValidTgts"]) == "" {
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
	if ps := sc.PredicatePrograms; ps != nil && !hasEffectiveNamePtr(o, sc) && !hasDerivedTypeEntryPtr(o, sc) {
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
		all := true
		for p := range strings.SplitSeq(rest, "+") {
			if p == "" {
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
		all := true
		for p := range strings.SplitSeq(rest, "+") {
			if p == "" {
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
	return MatchesSpecCtx(g, spec, id, SpecContext{You: you, Source: source})
}

// MatchesSpec reports whether an object matches a Forge filter spec.
func MatchesSpec(g *state.Game, spec string, id state.ObjID, you state.PlayerID) bool {
	return MatchesSpecFrom(g, spec, id, you, 0)
}

// MatchesPlayerSpec is the player-side filter: You, Opponent, Player.
// All supported player-state qualifiers share MatchesPlayerSpecCtx; source-
// dependent properties fail closed when this unbound entry point is used.
func MatchesPlayerSpec(g *state.Game, spec string, p, you state.PlayerID) bool {
	return MatchesPlayerSpecCtx(g, spec, p, you, PlayerSpecCtx{})
}

// MatchesPlayerSpecFrom resolves the source object's event-backed choice and
// attachment state (Player.Chosen, Player.IsRemembered and
// Player.EnchantedController). Keeping the source explicit lets ordinary
// player filters retain their existing API while trigger matching can supply
// its owning permanent.
func MatchesPlayerSpecFrom(g *state.Game, spec string, p, you state.PlayerID, source state.ObjID) bool {
	return MatchesPlayerSpecCtx(g, spec, p, you, PlayerSpecCtx{Source: source})
}

// PlayerSpecCtx carries the event-role bindings a player filter can need
// beyond the source object. Source is the object whose attachment and choice
// state a Player.EnchantedController / Player.IsRemembered clause resolves
// against. DefendingPlayer is the defending-player role of the triggering
// event, which Player.TriggeredDefendingPlayer names; it is absent (IsPlayer
// false) outside a trigger that carries one, so that clause fails closed.
// EffectiveNames and DerivedTypes are the SAME layer-3/layer-4 tables
// SpecContext carries (see their doc comments there, including the
// escape-analysis rationale for plain immutable slices): the
// Player.controlsCreature / Player.controlsPermanent family evaluates its
// object spec through a nested SpecContext, so without them a creature a
// layer-4 static made a Goblin is not counted by a lord question even though
// every ordinary filter site now sees it. All fields are plain data so the
// rule-engine call sites (a trigger match, a static actor match and a layer
// restriction) can populate them without a resolver callback.
type PlayerSpecCtx struct {
	Source            state.ObjID
	DefendingPlayer   state.Target
	DelayedRemembered []state.Target
	OpponentOf        []state.Target
	// EffectiveNames is the layer-3 rename set (SetName$, CR 613.1d).
	EffectiveNames []ObjectName
	// DerivedTypes is the layer-4 derived type list (CR 613.1d/613.1c).
	DerivedTypes []ObjectTypes
}

// MatchesPlayerSpecCtx is the full player-side filter: the same grammar as
// MatchesPlayerSpecFrom, with the caller's event-role bindings supplied.
// Every rule resolves here, so a trigger match, a static actor match and a
// layer restriction agree by construction rather than by parallel copies.
func MatchesPlayerSpecCtx(g *state.Game, spec string, p, you state.PlayerID, pc PlayerSpecCtx) bool {
	for alt := range strings.SplitSeq(spec, ",") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		if matchesPlayerCompoundCtx(g, alt, p, you, pc) {
			return true
		}
	}
	return false
}

// matchesPlayerCompoundFrom evaluates ONE comma alternative as a `+`-joined
// conjunction of player clauses, each optionally negated with a leading `!`.
// Forge's player specs spell their properties this way -- Tribal
// Hellkite's `Choices$ Player.Opponent+!IsRemembered`
// ("choose an opponent at random that CARDNAME didn't attack"),
// `Player.Opponent+lifeEQX`, `Player.Opponent+!EnchantedBy`. Before this the
// whole `+` string was cut on the first `.` and fell into the unknown-
// qualifier branch, so every such spec matched NOBODY -- a Choices$ pool that
// is silently empty (Territorial Hellkite could never choose an opponent, so
// its random pick always took the no-candidate arm) and a ValidTgts$ pool
// that admits no target. A clause with no `+` is a one-clause conjunction and
// behaves exactly as before, so the single-qualifier grammar is unchanged.
func matchesPlayerCompoundCtx(g *state.Game, alt string, p, you state.PlayerID, pc PlayerSpecCtx) bool {
	for clause := range strings.SplitSeq(alt, "+") {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			return false
		}
		neg := strings.HasPrefix(clause, "!")
		if neg {
			clause = strings.TrimSpace(clause[1:])
		}
		// A bare property clause (IsRemembered/Chosen/ChosenPlayer) with no
		// source bound cannot be evaluated at all: fail the WHOLE conjunction
		// closed, rather than letting the negation invert the absence into a
		// match. Without this a caller that passes source 0 (MatchesPlayerSpec)
		// would newly admit every un-remembered player for
		// `Player.Opponent+!IsRemembered` -- a widened pool where the old
		// grammar matched nobody.
		if pc.Source == 0 && isBarePlayerProperty(clause) && clause != "IsCorrupted" {
			return false
		}
		if matchesPlayerClauseCtx(g, clause, p, you, pc) == neg {
			return false
		}
	}
	return true
}

// matchesPlayerClauseFrom evaluates ONE player clause (no `,` or `+`). It
// first resolves the BARE property spellings a compound uses
// (`IsRemembered`, `Chosen`, `ChosenPlayer`) against the source object's
// event-backed choice state, then falls back to the ordinary single-spec
// grammar (matchesPlayerSingleSpec) for a base.qualifier form. An absent
// source fails the bare property clauses closed, exactly as the qualified
// `Player.IsRemembered` spelling already does.
func matchesPlayerClauseCtx(g *state.Game, clause string, p, you state.PlayerID, pc PlayerSpecCtx) bool {
	if ref, is := strings.CutPrefix(clause, "wasDealtDamageThisGameBy "); is {
		// Forge's bare game-long damage-by-source clause (The Fallen's
		// `Player.Opponent+wasDealtDamageThisGameBy Self`): the same
		// reading as the base.qualifier form above, reached through the
		// compound grammar. Both spellings funnel into this one body via
		// the shared damageGameRecordHas reader.
		return playerDamageByRefThisGame(g, p, you, pc, ref)
	}
	if !isBarePlayerProperty(clause) {
		return matchesPlayerSingleSpec(g, clause, p, you, pc)
	}
	switch clause {
	case "IsCorrupted":
		return playerIsCorrupted(g, p)
	case "IsRemembered", "Chosen", "ChosenPlayer":
		o := g.Obj(pc.Source)
		if o == nil {
			return false
		}
		set := o.Chosen
		if clause == "IsRemembered" {
			set = o.Remembered
		}
		for _, t := range set {
			if t.IsPlayer && t.Player == p {
				return true
			}
		}
		return false
	}
	return false
}

// isBarePlayerProperty reports whether a player clause is one of the bare
// property spellings a compound uses (`IsRemembered`, `Chosen`,
// `ChosenPlayer`), as opposed to a base.qualifier form.
func isBarePlayerProperty(clause string) bool {
	if _, is := strings.CutPrefix(clause, "wasDealtDamageThisGameBy "); is {
		// Forge's bare wasDealtDamageThisGameBy <ref>: a compound clause with
		// no base of its own (The Fallen). Its base-position companion form
		// is handled by matchesPlayerSingleSpec; both are the shared player
		// grammar, so the census gate's knownBase consults this same list.
		return true
	}
	switch clause {
	case "IsRemembered", "Chosen", "ChosenPlayer", "IsCorrupted":
		return true
	}
	return false
}

// playerBaseMatches reports whether a bare player-spec base matches seat p
// relative to the perspective seat you. It is the shared base predicate the
// ordinary qualifier switch below already spells inline (Player/Any always,
// You is the perspective seat, Opponent/Other is anyone else) and that the
// source-anchored Chosen/IsRemembered membership read now consults first, so
// a membership read can never widen past its base. An unknown base fails
// closed, exactly as the inline switch does.
func playerBaseMatches(base string, p, you state.PlayerID) bool {
	switch base {
	case "Player", "Any":
		return true
	case "You":
		return p == you
	case "Opponent", "Other":
		return p != you
	}
	return false
}

// matchesPlayerSingleSpec is the original single-alternative player-spec
// evaluator: one clause, no `,` or `+` (the callers above split those).
func matchesPlayerSingleSpec(g *state.Game, spec string, p, you state.PlayerID, pc PlayerSpecCtx) bool {
	for alt := range strings.SplitSeq(spec, ",") {
		base, qualifier, qualified := strings.Cut(strings.TrimSpace(alt), ".")
		if inner, negated := strings.CutPrefix(qualifier, "!"); qualified && negated {
			// A negated qualifier after the dot (Crown of Doom's
			// `Player.!CardOwner`, `Player.!IsRemembered`,
			// `Player.!EnchantedBy`): the base must match and the positive
			// qualifier must NOT. Only qualifiers this evaluator reads are
			// negated -- an unread one would otherwise invert its fail-closed
			// false into admitting every seat -- and a source-anchored one
			// fails closed with no source bound.
			switch inner {
			case "CardOwner", "Owner", "IsRemembered", "EnchantedBy":
			default:
				continue
			}
			if inner != "EnchantedBy" && g.Obj(pc.Source) == nil {
				continue
			}
			if matchesPlayerSingleSpec(g, base, p, you, pc) && !matchesPlayerSingleSpec(g, base+"."+inner, p, you, pc) {
				return true
			}
			continue
		}
		if (base == "Player" || base == "Any") && qualified && (qualifier == "CardOwner" || qualifier == "Owner") {
			// Player.CardOwner and Player.Owner (Forge PlayerProperty): the
			// OWNER of the filter's source object (Crown of Doom's "target
			// player other than CARDNAME's owner" negates it). No source
			// bound fails closed. Owner is Personal Incarnation's
			// "Activator$ Player.Owner" -- "Only CARDNAME's owner may
			// activate this ability".
			if o := g.Obj(pc.Source); o != nil && o.Owner == p {
				return true
			}
			continue
		}
		if qualified && (qualifier == "Chosen" || qualifier == "IsRemembered") {
			// The source-anchored chosen/remembered membership read, on EVERY
			// base the grammar evaluates (Player/Any as always, plus the
			// qualified You/Opponent/Other spellings -- Will the Wise's
			// `Defined$ Opponent.!IsRemembered` "each opponent who doesn't"
			// is the corpus carrier). Reading it only on Player/Any left the
			// Opponent-base qualifier permanently false, which the `!`
			// spelling inverted into admitting EVERY opponent. The base is
			// checked FIRST, so a membership read cannot admit the source's
			// own controller under an `Opponent` base (or an opponent under a
			// `You` base); only the set membership is base-independent. The
			// set is still the source object's own event-backed list, so every
			// base reads one home.
			if !playerBaseMatches(base, p, you) {
				continue
			}
			o := g.Obj(pc.Source)
			if o == nil {
				continue
			}
			set := o.Chosen
			if qualifier == "IsRemembered" {
				set = o.Remembered
			}
			for _, t := range set {
				if t.IsPlayer && t.Player == p {
					return true
				}
			}
			continue
		}
		matchesBase := false
		switch base {
		case "Player", "Any":
			if kind, is := strings.CutPrefix(qualifier, "withMost"); is {
				// Forge's Player.withMost<kind> property (PlayerProperty), now
				// evaluated in the shared player filter so a control grant's
				// NewController$ and a trigger's Attacked$/restriction spec
				// resolve the same seat. Unknown kinds fail closed (no seat
				// matches) exactly like every other unlisted qualifier.
				if playerHasMost(g, p, kind) {
					return true
				}
				continue
			}
			if rem, is := strings.CutPrefix(qualifier, "controlsCard."); is {
				// This ticket models only the registration-relative card form.
				if rem == "IsTriggerRemembered" && playerControlsMatches(g, p, you, pc, "Card", rem) {
					return true
				}
				continue
			}
			if rem, is := strings.CutPrefix(qualifier, "controlsCreature."); is {
				// Forge's Player.controlsCreature.<objspec> / controlsPermanent.
				// <objspec> property (PlayerControlsCreatures/Permanents): the
				// seat qualifies when its battlefield holds an object matching
				// <objspec> as an object filter, with an optional trailing
				// _GE<n>-style count comparison. See playerControlsMatches.
				if playerControlsMatches(g, p, you, pc, "Creature", rem) {
					return true
				}
				continue
			}
			if rem, is := strings.CutPrefix(qualifier, "controlsPermanent."); is {
				if playerControlsMatches(g, p, you, pc, "Permanent", rem) {
					return true
				}
				continue
			}
			matchesBase = true
		case "You":
			matchesBase = p == you
		case "Opponent", "Other":
			matchesBase = p != you
		}
		if !matchesBase {
			continue
		}
		if !qualified {
			return true
		}
		if ref, is := strings.CutPrefix(qualifier, "wasDealtDamageThisGameBy "); is {
			// Forge's Player.wasDealtDamageThisGameBy <ref> qualifier
			// (diseased_vermin's ValidTgts$ Opponent.wasDealtDamageThisGameBy
			// Self): the seat qualifies when <ref>'s objects have dealt it
			// damage this game. This reads the SAME
			// state.Player.DamageTakenByGame record, through the SAME shared
			// damageGameRecordHas helper, as the object-side word -- never a
			// parallel re-implementation.
			if playerDamageByRefThisGame(g, p, you, pc, ref) {
				return true
			}
			continue
		}
		switch qualifier {
		case "OpponentOf Remembered":
			// This supported referent is bound by a resolving ChoosePlayer's
			// Ctx.Remembered. Other OpponentOf spellings remain fail-closed;
			// their event roles need distinct, explicit bindings.
			if base == "Player" || base == "Any" {
				for _, ref := range pc.OpponentOf {
					if ref.IsPlayer && int(ref.Player) < len(g.Players) &&
						int(p) < len(g.Players) && p != ref.Player {
						return true
					}
				}
			}
		case "IsCorrupted":
			if playerIsCorrupted(g, p) {
				return true
			}
		case "You":
			if p == you {
				return true
			}
		case "Opponent", "Other":
			if p != you {
				return true
			}
		case "Active":
			if p == g.Active {
				return true
			}
		case "NonActive":
			// NonActive is the complement of Active, evaluated after the
			// Player/You/Opponent/Other base has matched.
			if p != g.Active {
				return true
			}
		case "isMonarch":
			// CR 716.2's monarch designation, on the Player/Any base only:
			// the state-local qualifier a control static's GainControl$
			// value (Fealty to the Realm's "The monarch controls enchanted
			// creature") and any other player spec resolve through. A
			// qualified You/Opponent/Other base (You.isMonarch) still fails
			// closed, like every fx20 qualifier not listed here.
			if (base == "Player" || base == "Any") && g.IsMonarch(p) {
				return true
			}
		case "EnchantedBy":
			// An Aura may enchant a player of either seat, independent of
			// the source of the filter.
			for i := range g.Objs {
				o := &g.Objs[i]
				if o.Zone == state.ZBattlefield && o.HasAttachedPlayer && o.AttachedPlayer == p &&
					o.Face() != nil && o.Face().IsEnchantment() {
					return true
				}
			}
		case "EnchantedController":
			// Player.EnchantedController (Forge PlayerProperty): the seat is
			// the controller of the permanent this Aura/Equipment source is
			// attached to. The link is the source object's own battlefield
			// state, so a static actor match (Caster$/Activator$/ValidPlayer$),
			// a phase trigger's ValidPlayer$ and a layer restriction all
			// resolve it through this one clause.
			if ctrl, ok := playerEnchantedController(g, pc.Source); ok && ctrl == p {
				return true
			}
		case "descended":
			// CR 700.11: a permanent CARD entered this player's graveyard
			// this turn from any zone. ZoneEntry captures owner and card type at
			// the move, and TurnChange clears the ledger on replay as in play.
			if descendedThisTurn(g, p) > 0 {
				return true
			}
		case "TriggeredDefendingPlayer":
			// Player.TriggeredDefendingPlayer (Forge PlayerProperty): the
			// seat that defended against the triggering combat. The role is
			// carried by the caller through PlayerSpecCtx; with no defending
			// player bound (no trigger, or an event with no defender) the
			// clause fails closed rather than widening to the trigger's own
			// controller.
			if pc.DefendingPlayer.IsPlayer && pc.DefendingPlayer.Player == p {
				return true
			}
		default:
			// Player.counters_<CMP><n>_<KIND> (Forge PlayerProperty.Counters
			// spellings, e.g. Player.counters_EQ0_Contract): the seat's
			// counter count of KIND, read off its event-backed
			// state.Player.Counters (written only by events.Apply's
			// PlayerCounterChange), compared with the same <CMP> operators
			// the object-side counters_ predicate uses. The base has already
			// matched, so this is a pure player-state read.
			if op, n, kind, ok := splitPlayerCountCompare(qualifier); ok {
				if int(p) < len(g.Players) && playerCompare(g.Players[p].Counter(kind), op, n) {
					return true
				}
				continue
			}
			// Player.NotedFor<label> (Forge's PlayerProperty.NotedFor): the
			// seat qualifies when its event-backed note set names <label>.
			// The label is written by a DB$ Pump body's NoteCardsFor$
			// parameter (effects.effPump -> events.PlayerNoted), so the read
			// reaches the shared player filter every consumer already uses --
			// RepeatEach's RepeatPlayers$, Defined$ on Draw/Discard/ChangeZone,
			// the Continuous statics' Affected$ and a Count$ head alike. The
			// Player/Any base is required (a qualified You.NotedForX fails
			// closed, like every other qualifier here), the label is matched
			// EXACTLY (case-sensitive, as Forge's string set is), and an
			// out-of-range seat fails closed.
			if base == "Player" || base == "Any" {
				if label, is := strings.CutPrefix(qualifier, "NotedFor"); is && label != "" {
					if int(p) < len(g.Players) && playerHasNote(g, p, label) {
						return true
					}
					continue
				}
			}
			if int(p) < len(g.Players) {
				op, n, ok := splitPlayerCompare(qualifier)
				if ok && playerCompare(g.Players[p].Life, op, n) {
					return true
				}
			}
		}
	}
	return false
}

// damageGameRecordHas reports whether a game-long damage-by-source record
// (state.Object.DamageTakenByGame or state.Player.DamageTakenByGame) names
// src as a source that has dealt this recipient damage this game. It is the
// ONE shared reader behind both spellings: the object filter's
// wasDealtDamageThisGameBy/wasDealtDamageByThisGame words and the player
// filter's wasDealtDamageThisGameBy qualifier, so the object and player
// readings of Forge's game-long record can never drift. src==0 never matches
// (no source was bound; callers also refuse the unbound case through
// contextPredicateBound). Walked by index, never a map range.
func damageGameRecordHas(ids []state.ObjID, src state.ObjID) bool {
	if src == 0 {
		return false
	}
	for _, id := range ids {
		if id == src {
			return true
		}
	}
	return false
}

// playerDamageByRefThisGame is the shared body behind BOTH player spellings
// of Forge's game-long damage-by-source qualifier -- the base.qualifier form
// (diseased_vermin's Opponent.wasDealtDamageThisGameBy Self, reached through
// matchesPlayerSingleSpec) and the bare compound clause (The Fallen's
// Player.Opponent+wasDealtDamageThisGameBy Self, reached through
// matchesPlayerClauseCtx). It resolves <ref> through the same shared
// referent switch the object word uses and tests the seat's
// state.Player.DamageTakenByGame record with the shared
// damageGameRecordHas reader, so the object and player readings of one
// state record cannot drift. An out-of-range seat or an unresolvable ref
// matches nobody (fail closed).
func playerDamageByRefThisGame(g *state.Game, p, you state.PlayerID, pc PlayerSpecCtx, ref string) bool {
	if int(p) >= len(g.Players) {
		return false
	}
	sc := SpecContext{You: you, Source: pc.Source,
		TriggerContext: TriggerContext{DefendingPlayer: pc.DefendingPlayer}}
	for _, t := range sharesTypeReferents(g, sc, strings.TrimSpace(ref)) {
		if t.IsPlayer {
			continue
		}
		if damageGameRecordHas(g.Players[p].DamageTakenByGame, t.Obj) {
			return true
		}
	}
	return false
}

// descendedThisTurn counts the permanent CARDS that entered player p's
// graveyard this turn from any zone (CR 700.11). This is the single read of
// the fx20 descend provenance: the Player.descended predicate and the
// Count$YouDescendedThisTurn head both call it, so they cannot disagree.
// A token or a nonpermanent card never counts ($PermanentCard is folded
// from !IsToken/!IsCopy/IsPermanent at the move).
func descendedThisTurn(g *state.Game, p state.PlayerID) int32 {
	var n int32
	for _, entry := range g.Entered {
		if entry.To == state.ZGraveyard && entry.Owner == p && entry.PermanentCard {
			n++
		}
	}
	return n
}

// playerIsCorrupted applies the Corrupted threshold (three or more poison
// counters) to a valid player seat.
func playerIsCorrupted(g *state.Game, p state.PlayerID) bool {
	return int(p) >= 0 && int(p) < len(g.Players) && g.Players[p].Counter("POISON") >= 3
}

// playerHasNote reports whether the seat's event-backed player-notation set
// names label. The note set is state.Player.Notes, written only by
// events.Apply's PlayerNoted case (a DB$ Pump body's NoteCardsFor$), so a
// live game and a log-only replay answer identically. An out-of-range seat
// fails closed. There is no map range here (the slice is walked in its
// append order), so the result is deterministic.
func playerHasNote(g *state.Game, p state.PlayerID, label string) bool {
	if int(p) >= len(g.Players) {
		return false
	}
	for _, n := range g.Players[p].Notes {
		if n == label {
			return true
		}
	}
	return false
}

// splitCountCompare strips a trailing "_"-separated count comparison token
// ("GE1", "LT3", ...) from an object-spec remainder. It returns the remainder
// with the token removed, the comparison operator, the threshold, and whether
// a count token was present at all. A trailing token that is not a count
// comparison (e.g. the named-arg convention's "namedAether_Burst") stays part
// of the object spec, and a remainder with no "_" at all is returned whole.
func splitCountCompare(rem string) (string, string, int32, bool) {
	i := strings.LastIndex(rem, "_")
	if i < 0 {
		return rem, "", 0, false
	}
	tok := rem[i+1:]
	if len(tok) < len("GE0") {
		return rem, "", 0, false
	}
	op, digits := tok[:2], tok[2:]
	n, err := strconv.ParseInt(digits, 10, 32)
	if err != nil {
		return rem, "", 0, false
	}
	switch op {
	case "GE", "GT", "EQ", "LE", "LT":
		return rem[:i], op, int32(n), true
	}
	return rem, "", 0, false
}

// playerControlsMatches evaluates Forge's Player.controlsCreature.<spec> /
// controlsPermanent.<spec> qualifiers (PlayerProperty's
// PlayerControlsCreatures/PlayerControlsPermanents family): the seat
// qualifies when the required number of its battlefield objects match
// <spec> as an object filter. The count comparison rides a trailing
// "_GE<n>"-style token and defaults to an existential _GE1; a spec with no
// count token matches when at least one object does. The object filter is
// evaluated with the same SpecContext binding MatchesPlayerSpecFrom carries
// (the perspective seat and the source permanent), so the named<Name>,
// MultiColor, IsRemembered and EnchantedBy object predicates all resolve
// unchanged. The controlsCard.IsTriggerRemembered caller additionally binds
// the delayed registration's capture. A spec that matches nothing -- including
// one carrying an unmodelled predicate, which fails closed inside the object
// matcher -- never matches for that seat.
func playerControlsMatches(g *state.Game, p state.PlayerID, you state.PlayerID, pc PlayerSpecCtx, objBase, rem string) bool {
	spec, op, want, counted := splitCountCompare(rem)
	spec = objBase + "." + spec
	sc := SpecContext{
		You: you, Source: pc.Source,
		TriggerContext: TriggerContext{DelayedRemembered: pc.DelayedRemembered},
		// The nested object filter must see the same layer-3/layer-4
		// derived characteristics every ordinary filter site does; a
		// controlsCreature/controlsPermanent spec otherwise reads the
		// printed face alone.
		EffectiveNames: pc.EffectiveNames,
		DerivedTypes:   pc.DerivedTypes,
	}
	n := int32(0)
	for _, id := range g.Zone(state.ZBattlefield, p) {
		if MatchesObjectCtx(g, spec, g.Obj(id), sc) {
			n++
		}
	}
	if !counted {
		return n > 0
	}
	return playerCompare(n, op, want)
}

// playerHasMost is the shared evaluator for Forge's Player.withMost<kind>
// property (PlayerProperty.java). Kinds the corpus spells: Life (ties match
// -- every seat holding the maximum life), CardsInHand (Forge's
// strictly-greater scan keeps the FIRST holder in player order on a tie),
// PermanentInPlay (most permanents; ties match every holder) and
// Type<X>[Only] (most battlefield permanents of type X; "Only" requires a
// UNIQUE holder, and when the top count is shared nobody matches -- Forge
// returns false for every player). Dead seats take part in the scan exactly
// like Forge's game.getPlayers(); the control-grant caller walks only the
// living seats before consulting this, so a dead seat can win a filter match
// but never gain control.
func playerHasMost(g *state.Game, p state.PlayerID, kind string) bool {
	if int(p) >= len(g.Players) {
		return false
	}
	only := false
	if x, is := strings.CutSuffix(kind, "Only"); is {
		only = true
		kind = x
	}
	switch kind {
	case "Life":
		best := g.Players[0].Life
		for i := range g.Players {
			if g.Players[i].Life > best {
				best = g.Players[i].Life
			}
		}
		return g.Players[p].Life == best
	case "CardsInHand":
		// Forge's getPlayerWithMostCardsInHand starts with no candidate and
		// only binds when a player has a positive hand; all-empty hands name
		// nobody. Ties retain the first player in seat order.
		best, holder := 0, -1
		for i := range g.Players {
			if n := len(g.Zone(state.ZHand, state.PlayerID(i))); n > best {
				best, holder = n, i
			}
		}
		return holder >= 0 && int(p) == holder
	}
	count := func(pi state.PlayerID) int {
		n := 0
		for _, id := range g.Zone(state.ZBattlefield, pi) {
			o := g.Obj(id)
			if o == nil {
				continue
			}
			if t, is := strings.CutPrefix(kind, "Type"); is {
				if hasType(o, t) {
					n++
				}
				continue
			}
			if kind == "PermanentInPlay" {
				n++
			}
		}
		return n
	}
	best, holders := -1, 0
	for i := range g.Players {
		n := count(state.PlayerID(i))
		if n > best {
			best, holders = n, 1
		} else if n == best {
			holders++
		}
	}
	// Forge requires a unique leader for PermanentInPlay as well as the
	// explicit Type...Only spelling. A shared top count names nobody.
	if (only || kind == "PermanentInPlay") && holders != 1 {
		return false
	}
	return count(p) == best
}

// splitPlayerCompare accepts Forge's literal lifeGE1/lifeLT7 qualifiers.
func splitPlayerCompare(s string) (string, int32, bool) {
	op, rhs, ok := splitPlayerCompareToken(s)
	if !ok {
		return "", 0, false
	}
	n, err := strconv.ParseInt(rhs, 10, 32)
	if err != nil {
		return "", 0, false
	}
	return op, int32(n), true
}

func splitPlayerCompareToken(s string) (string, string, bool) {
	if !strings.HasPrefix(s, "life") || len(s) < len("lifeGE0") {
		return "", "", false
	}
	op := s[4:6]
	switch op {
	case "GE", "GT", "EQ", "LE", "LT":
		return op, s[6:], true
	}
	return "", "", false
}

// MatchesPlayerSpecWithSVars resolves symbolic life-comparison thresholds in
// the asking ability's SVar table, then delegates all other grammar to the
// shared player matcher. Unknown count bodies fail closed for that alternative.
func MatchesPlayerSpecWithSVars(h Host, c *Ctx, spec string, p, you state.PlayerID) bool {
	if h == nil || c == nil {
		return false
	}
	for alt := range strings.SplitSeq(spec, ",") {
		clauses := strings.Split(strings.TrimSpace(alt), "+")
		resolved := true
		for i, clause := range clauses {
			neg := strings.HasPrefix(clause, "!")
			plain := strings.TrimPrefix(clause, "!")
			base, qualifier, hasDot := strings.Cut(plain, ".")
			if !hasDot {
				base, qualifier = "Player", plain
			}
			if base != "Player" && base != "Opponent" && base != "Other" && base != "You" {
				continue
			}
			op, rhs, ok := splitPlayerCompareToken(qualifier)
			if !ok {
				continue
			}
			if _, err := strconv.ParseInt(rhs, 10, 32); err == nil {
				continue
			}
			body, found := c.SVars[rhs]
			if !found {
				if o := h.Game().Obj(c.Source); o != nil && o.Face() != nil {
					body, found = o.Face().SVars[rhs]
				}
			}
			if !found {
				resolved = false
				break
			}
			threshold, ok := EvalCountOK(h, c, body)
			if !ok {
				resolved = false
				break
			}
			prefix := ""
			if neg {
				prefix = "!"
			}
			clauses[i] = prefix + base + ".life" + op + strconv.FormatInt(int64(threshold), 10)
		}
		if resolved && MatchesPlayerSpecCtx(h.Game(), strings.Join(clauses, "+"), p, you, PlayerSpecCtx{
			Source:     c.Source,
			OpponentOf: c.Remembered,
		}) {
			return true
		}
	}
	return false
}

// playerEnchantedController returns the controller of the permanent this
// Aura/Equipment source is attached to, and whether that link exists. A
// missing source, an unattached source, or a bearer that has left the game
// fails closed. A player-attached Aura has no permanent bearer, so it does
// not satisfy this distinct EnchantedController property.
func playerEnchantedController(g *state.Game, source state.ObjID) (state.PlayerID, bool) {
	o := g.Obj(source)
	if o == nil || o.AttachedTo == 0 {
		return 0, false
	}
	bearer := g.Obj(o.AttachedTo)
	if bearer == nil {
		return 0, false
	}
	return bearer.Controller, true
}

// splitPlayerCountCompare parses Forge's player-counter qualifier
// counters_<CMP><n>_<KIND> (e.g. counters_EQ0_Contract). It mirrors the
// object-side counters_ predicate's grammar so the player and object
// spellings cannot drift; an unknown <CMP>, a missing/empty KIND or a
// non-integer RHS fails closed (a resolver-bearing RHS is not carried on the
// player side, so a symbolic RHS is simply unrecognised).
func splitPlayerCountCompare(s string) (string, int32, string, bool) {
	rest, ok := strings.CutPrefix(s, "counters_")
	if !ok || len(rest) < 4 {
		return "", 0, "", false
	}
	op := rest[:2]
	numStr, kind, okSplit := strings.Cut(rest[2:], "_")
	if !okSplit || kind == "" {
		return "", 0, "", false
	}
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return "", 0, "", false
	}
	switch op {
	case "GE", "GT", "EQ", "LE", "LT":
		return op, int32(n), kind, true
	}
	return "", 0, "", false
}

func playerCompare(have int32, op string, want int32) bool {
	switch op {
	case "GE":
		return have >= want
	case "GT":
		return have > want
	case "EQ":
		return have == want
	case "LE":
		return have <= want
	case "LT":
		return have < want
	}
	return false
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
	switch p {
	case "YouOwn", "YouCtrl", "YouControl", "YourControl", "YouControlled",
		"OppOwn", "OppCtrl", "OpponentOwns", "OpponentControls":
		return true
	}
	return false
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
// election entirely for a selector this reports true for, instead of offering
// a list that can only ever be empty (Mockingbird's
// "Choices$ Creature.Other+cmcLEY", whose Y is Count$CastTotalManaSpent).
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

// KnownPredicates lists the predicates this build implements, in sorted order.
func KnownPredicates() []string {
	out := make([]string, 0, len(predicates))
	for k := range predicates {
		out = append(out, k)
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
	switch strings.TrimPrefix(p, "hasAbility ") {
	case "Activated", "Activated.hasTapCost", "Activated.Exhaust":
		return strings.HasPrefix(p, "hasAbility ")
	}
	return false
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
		switch sub {
		case "Activated":
			return true
		case "Activated.hasTapCost":
			for _, tok := range strings.Fields(a.Params["Cost"]) {
				if tok == "T" {
					return true
				}
			}
		case "Activated.Exhaust":
			if strings.EqualFold(strings.TrimSpace(a.Params["Exhaust"]), "True") {
				return true
			}
		}
	}
	return false
}
