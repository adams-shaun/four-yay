package effects

import (
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

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
	// "SharesColorWithOther <X>": the colour twin of sharesCardTypeWithOther
	// -- the candidate shares at least one colour with an OTHER object the
	// referent names (Sphinx's Tutelage's and Grindstone's "two cards that
	// share a color were milled this way" repeat gate).
	wordSharesColorOther
	// "sharesAllCardTypesWithOther <X>": same referent switch, but the
	// candidate must share EVERY one of its card types with some OTHER
	// object the referent names (Demonic Covenant's "two cards that share
	// all their card types were milled this way").
	wordSharesAllCardTypes
	// sharesNameWith <referent> compares the candidate's full name set with
	// the names of supported resolution-time object referents.
	wordSharesNameWith
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
	// Forge's Outlaw batch word (the reminder text on every carrier: Assassins,
	// Mercenaries, Pirates, Rogues and Warlocks are outlaws). It is a union of
	// creature subtypes, not a single type word, so it needs its own kind --
	// classified here so the matcher and UnknownPredicates agree.
	wordOutlaw
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
	// wordFullyUnlocked is Forge's Card.FullyUnlocked: the Room permanent on
	// the battlefield has BOTH unlocked designations (CR 709.5c), i.e. the
	// cast face is unlocked and the alternate door has been unlocked on top
	// of it. The corpus's only filter carrier is Ghostly Dancers' UnlockDoor
	// pool (`Room.YouCtrl+!FullyUnlocked`), which excludes a Room that has no
	// locked door left to unlock. The body reads both door designations from
	// state.Object, as maintained by the DoorUnlock/DoorLock folds.
	wordFullyUnlocked
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
	// FORGE_REF fb4d809 respells the same predicate as a player filter on the
	// object's controller (Steel Hellkite); the two forms mean one thing.
	if p == "controllerWasDealtCombatDamageByThisTurn" || p == "ControlledBy Player.wasDealtCombatDamageThisTurnBySource" {
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
		switch wordPredicateRestCodes.Code(string(strings.TrimSpace(rest))) {
		case wordPredicateRestKickerIndex:
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
	switch wordPredicateWordCodes.Code(string(p)) {
	case wordPredicateWordColorless:
		return wordColorless, ""
	case wordPredicateWordMultiColor:
		return wordMultiColor, ""
	case wordPredicateWordMonoColor:
		return wordMonoColor, ""
	case wordPredicateWordWorthy:
		return wordWorthy, ""
	case wordPredicateWordChosenColor:
		return wordChosenColor, ""
	case wordPredicateWordWasCast:
		return wordWasCast, ""
	case wordPredicateWordCopiedSpell:
		return wordCopiedSpell, ""
	// The cast-provenance tokens are recognised here (so the census no longer
	// reports them unknown) but evaluated by rules' castProvenanceAdmits,
	// which strips them before the filter runs; wordMatches' body fails
	// closed. wasCastFromYourHandByYou is checked before the bare
	// wasCastFromYourHand because the bare token is a substring of the ByYou
	// spelling -- the same ordering rule castProvenanceAdmits documents.
	case wordPredicateWordCastProvenance:
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
	case wordPredicateWordCastSa:
		return wordCastProvenance, p
	case wordPredicateWordActivePlayerCtrl:
		return wordActivePlayerCtrl, ""
	case wordPredicateWordTopLibrary:
		return wordTopLibrary, ""
	case wordPredicateWordFaceDown:
		return wordFaceDown, ""
	case wordPredicateWordCanBeTurnedFaceUp:
		return wordCanBeTurnedFaceUp, ""
	case wordPredicateWordIsRingbearer:
		return wordRingBearer, ""
	case wordPredicateWordHasCounters:
		return wordHasCounters, ""
	case wordPredicateWordSuspended:
		return wordSuspended, ""
	// The pc1 object/game-context families. Each is a bare predicate word
	// whose body reads provenance outside the object alone (see the
	// wordKind block's comment); classification here is what makes the
	// matcher and UnknownPredicates agree that the word is implemented.
	case wordPredicateWordWasDealtDamageThisTurn:
		return wordDealtDamageThisTurn, ""
	case wordPredicateWordWasDealtDamageByThisGame:
		return wordDealtDamageByThisGame, ""
	case wordPredicateWordIsImprinted:
		return wordImprinted, ""
	case wordPredicateWordDefenderCtrl:
		return wordDefenderCtrl, ""
	case wordPredicateWordEnchantedControllerCtrl:
		return wordEnchantedControllerCtrl, ""
	case wordPredicateWordNotDefinedTargeted:
		return wordNotDefinedTargeted, ""
	case wordPredicateWordOpponent:
		return wordOpponentCtrl, ""
	case wordPredicateWordOppProtect:
		return wordOppProtect, ""
	case wordPredicateWordHistoric:
		return wordHistoric, ""
	case wordPredicateWordAdventureCard:
		return wordAdventureCard, ""
	case wordPredicateWordIsCommander:
		return wordIsCommander, ""
	case wordPredicateWordBlockingSource:
		return wordBlockingSource, ""
	case wordPredicateWordBlockedBySource:
		return wordBlockedBySource, ""
	// Forge's Card.hasANonBasicLandType (the corpus's
	// `Land.hasANonBasicLandType` qualifier; Wonderscape Sage's
	// ConditionPresent gate). The bare word is classified here so the matcher
	// and the UnknownPredicates census share one recogniser; the `Land.` base
	// the corpus spells it under is the union spelling (the body re-checks
	// Land anyway, so a bare `Card.hasANonBasicLandType` stays correct too).
	case wordPredicateWordHasANonBasicLandType:
		return wordHasNonBasicLandType, ""
	// Forge's Card.hasABasicLandType (the corpus's `Land.hasABasicLandType`
	// qualifier). The bare word is classified here so the matcher and the
	// UnknownPredicates census share one recogniser; the `Land.` base the
	// corpus spells it under is the union spelling (the body re-checks Land
	// anyway, so a bare `Card.hasABasicLandType` stays correct too).
	case wordPredicateWordHasABasicLandType:
		return wordHasBasicLandType, ""
	case wordPredicateWordFullyUnlocked:
		return wordFullyUnlocked, ""
	// Forge's Outlaw batch word: the candidate carries at least one of the
	// five outlaw creature subtypes (Assassins, Mercenaries, Pirates, Rogues,
	// Warlocks -- the reminder text on every carrier). The matcher reads the
	// layer-aware subtype list, so a type-changing effect is honoured.
	case wordPredicateWordOutlaw:
		return wordOutlaw, ""
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
	if arg, ok := sharesNameWithArg(p); ok {
		return wordSharesNameWith, arg
	}
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
		switch wordPredicateSharesCodes.Code(string(name)) {
		case wordPredicateSharesCreatureTypeWith:
			return wordSharesCreatureType, arg
		case wordPredicateSharesCardTypeWithOther:
			return wordSharesCardTypeOther, arg
		case wordPredicateSharesColorWithOther:
			return wordSharesColorOther, arg
		case wordPredicateSharesAllCardTypesWithOther:
			return wordSharesAllCardTypes, arg
		case wordPredicateSharesNameWith:
			return wordSharesNameWith, arg
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

// outlawSubtypes are the creature subtypes Forge's Outlaw batch word names
// (the reminder text on every carrier: "Assassins, Mercenaries, Pirates,
// Rogues, and Warlocks are outlaws"). The order is fixed so the matcher is
// deterministic.
var outlawSubtypes = [...]string{"Assassin", "Mercenary", "Pirate", "Rogue", "Warlock"}

// outlawMatches reads the five outlaw subtypes through the layer-aware matcher.
func outlawMatches(o *state.Object, sc SpecContext) bool {
	for _, sub := range outlawSubtypes {
		if hasTypeCtx(o, sub, sc) {
			return true
		}
	}
	return false
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
func wordColorCountMatches(kind wordKind, o *state.Object, sc SpecContext) bool {
	switch kind {
	case wordMultiColor:
		return len(colorsCtx(o, &sc)) > 1
	case wordMonoColor:
		return len(colorsCtx(o, &sc)) == 1
	case wordColorless:
		return colorMaskCtx(o, &sc) == 0
	default:
		return false
	}
}

func wordMatches(kind wordKind, key string, g *state.Game, o *state.Object, sc SpecContext) bool {
	if kind == wordMultiColor || kind == wordMonoColor || kind == wordColorless {
		return wordColorCountMatches(kind, o, sc)
	}
	source := sc.Source
	switch kind {
	case wordSharesCardType:
		return sharesCardTypeWith(g, o, sc, key)
	case wordSharesCardTypeOther:
		return sharesCardTypeWithOther(g, o, sc, key)
	case wordSharesColorOther:
		return sharesColorWithOther(g, o, &sc, key)
	case wordSharesCreatureType:
		return sharesCreatureTypeWith(g, o, sc, key)
	case wordSharesAllCardTypes:
		return sharesAllCardTypesWithOther(g, o, sc, key)
	case wordSharesNameWith:
		return sharesNameReferentMatches(g, o, sc, key)
	case wordColor:
		return strings.Contains(colorsCtx(o, &sc), key)
	case wordChosenColor:
		return chosenColorMatches(g, o, sc)
	case wordType:
		return hasTypePredicateCtx(o, key, sc)
	case wordFullyUnlocked:
		return o != nil && o.RoomFullyUnlocked()
	case wordOutlaw:
		return outlawMatches(o, sc)
	case wordControllerDealtCombatDamageBySource:
		return dealtCombatDamageBySource(g, o, sc, source)
	case wordColourSource:
		return strings.Contains(ColorsOf(o), key)
	case wordColourSourceless:
		return ColorsOf(o) == ""
	case wordKickedIndex:
		// The and/or Kicker's part bits (state/object.go): the CastInfo
		// provenance the kicked1/kicked2/kickedboth cast modes ride. A part
		// never paid never matches, and the bare FlagKicked bit alone (a
		// single-cost Kicker) never matches an index form.
		switch wordMatchesCodes.Code(string(key)) {
		case wordMatches1:
			return o.CastFlags&state.FlagKicked1 != 0
		case wordMatches2:
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
	case wordWorthy:
		colors := colorsCtx(o, &sc)
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
		bound, _ := sc.TargetBinding()
		for _, t := range bound {
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
		return sc.Resolving || sc.ParentBound
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
	case wordSharesNameWith:
		return len(sharesTypeReferents(g, sc, key)) > 0
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
	case wordColor, wordType, wordColorless, wordCopiedSpell, wordOutlaw:
		return kind, key, true
	}
	return wordUnknown, "", false
}

type wordPredicateRestCode uint16

const (
	wordPredicateRestKickerIndex wordPredicateRestCode = iota + 1
)

var wordPredicateRestCodes = state.NewStrCodes(
	state.StrEntry[wordPredicateRestCode]{Key: "1", Val: wordPredicateRestKickerIndex},
	state.StrEntry[wordPredicateRestCode]{Key: "2", Val: wordPredicateRestKickerIndex},
)

type wordPredicateWordCode uint16

const (
	wordPredicateWordColorless wordPredicateWordCode = iota + 1
	wordPredicateWordMultiColor
	wordPredicateWordMonoColor
	wordPredicateWordWorthy
	wordPredicateWordChosenColor
	wordPredicateWordWasCast
	wordPredicateWordCopiedSpell
	wordPredicateWordCastProvenance
	wordPredicateWordCastSa
	wordPredicateWordActivePlayerCtrl
	wordPredicateWordTopLibrary
	wordPredicateWordFaceDown
	wordPredicateWordCanBeTurnedFaceUp
	wordPredicateWordIsRingbearer
	wordPredicateWordHasCounters
	wordPredicateWordSuspended
	wordPredicateWordWasDealtDamageThisTurn
	wordPredicateWordWasDealtDamageByThisGame
	wordPredicateWordIsImprinted
	wordPredicateWordDefenderCtrl
	wordPredicateWordEnchantedControllerCtrl
	wordPredicateWordNotDefinedTargeted
	wordPredicateWordOpponent
	wordPredicateWordOppProtect
	wordPredicateWordHistoric
	wordPredicateWordAdventureCard
	wordPredicateWordIsCommander
	wordPredicateWordBlockingSource
	wordPredicateWordBlockedBySource
	wordPredicateWordHasANonBasicLandType
	wordPredicateWordHasABasicLandType
	wordPredicateWordFullyUnlocked
	wordPredicateWordOutlaw
)

var wordPredicateWordCodes = state.NewStrCodes(
	state.StrEntry[wordPredicateWordCode]{Key: "Colorless", Val: wordPredicateWordColorless},
	state.StrEntry[wordPredicateWordCode]{Key: "MultiColor", Val: wordPredicateWordMultiColor},
	state.StrEntry[wordPredicateWordCode]{Key: "MonoColor", Val: wordPredicateWordMonoColor},
	state.StrEntry[wordPredicateWordCode]{Key: "Worthy", Val: wordPredicateWordWorthy},
	state.StrEntry[wordPredicateWordCode]{Key: "ChosenColor", Val: wordPredicateWordChosenColor},
	state.StrEntry[wordPredicateWordCode]{Key: "wasCast", Val: wordPredicateWordWasCast},
	state.StrEntry[wordPredicateWordCode]{Key: "CopiedSpell", Val: wordPredicateWordCopiedSpell},
	state.StrEntry[wordPredicateWordCode]{Key: "wasCastFromYourHandByYou", Val: wordPredicateWordCastProvenance},
	state.StrEntry[wordPredicateWordCode]{Key: "wasCastByYou", Val: wordPredicateWordCastProvenance},
	state.StrEntry[wordPredicateWordCode]{Key: "wasCastFromYourHand", Val: wordPredicateWordCastProvenance},
	state.StrEntry[wordPredicateWordCode]{Key: "wasCastFromExile", Val: wordPredicateWordCastProvenance},
	state.StrEntry[wordPredicateWordCode]{Key: "wasCastFromYourGraveyard", Val: wordPredicateWordCastProvenance},
	state.StrEntry[wordPredicateWordCode]{Key: "wasCastFromYourGraveyardByYou", Val: wordPredicateWordCastProvenance},
	state.StrEntry[wordPredicateWordCode]{Key: "wasCastFromTheirHand", Val: wordPredicateWordCastProvenance},
	state.StrEntry[wordPredicateWordCode]{Key: "CastSa Spell.ManaFromTreasure", Val: wordPredicateWordCastSa},
	state.StrEntry[wordPredicateWordCode]{Key: "CastSa Spell.ManaFromCave", Val: wordPredicateWordCastSa},
	state.StrEntry[wordPredicateWordCode]{Key: "CastSa Spell.ManaFromDesert", Val: wordPredicateWordCastSa},
	state.StrEntry[wordPredicateWordCode]{Key: "CastSa Spell.ManaFromArtifact", Val: wordPredicateWordCastSa},
	state.StrEntry[wordPredicateWordCode]{Key: "CastSa Spell.ManaSpent EQ0", Val: wordPredicateWordCastSa},
	state.StrEntry[wordPredicateWordCode]{Key: "CastSa Spell.Mayhem", Val: wordPredicateWordCastSa},
	state.StrEntry[wordPredicateWordCode]{Key: "CastSa Spell.Warp", Val: wordPredicateWordCastSa},
	state.StrEntry[wordPredicateWordCode]{Key: "CastSa Spell.MayPlaySource", Val: wordPredicateWordCastSa},
	state.StrEntry[wordPredicateWordCode]{Key: "ActivePlayerCtrl", Val: wordPredicateWordActivePlayerCtrl},
	state.StrEntry[wordPredicateWordCode]{Key: "TopLibrary", Val: wordPredicateWordTopLibrary},
	state.StrEntry[wordPredicateWordCode]{Key: "faceDown", Val: wordPredicateWordFaceDown},
	state.StrEntry[wordPredicateWordCode]{Key: "canBeTurnedFaceUp", Val: wordPredicateWordCanBeTurnedFaceUp},
	state.StrEntry[wordPredicateWordCode]{Key: "IsRingbearer", Val: wordPredicateWordIsRingbearer},
	state.StrEntry[wordPredicateWordCode]{Key: "HasCounters", Val: wordPredicateWordHasCounters},
	state.StrEntry[wordPredicateWordCode]{Key: "suspended", Val: wordPredicateWordSuspended},
	state.StrEntry[wordPredicateWordCode]{Key: "wasDealtDamageThisTurn", Val: wordPredicateWordWasDealtDamageThisTurn},
	state.StrEntry[wordPredicateWordCode]{Key: "wasDealtDamageByThisGame", Val: wordPredicateWordWasDealtDamageByThisGame},
	state.StrEntry[wordPredicateWordCode]{Key: "IsImprinted", Val: wordPredicateWordIsImprinted},
	state.StrEntry[wordPredicateWordCode]{Key: "DefenderCtrl", Val: wordPredicateWordDefenderCtrl},
	state.StrEntry[wordPredicateWordCode]{Key: "EnchantedControllerCtrl", Val: wordPredicateWordEnchantedControllerCtrl},
	state.StrEntry[wordPredicateWordCode]{Key: "NotDefinedTargeted", Val: wordPredicateWordNotDefinedTargeted},
	state.StrEntry[wordPredicateWordCode]{Key: "Opponent", Val: wordPredicateWordOpponent},
	state.StrEntry[wordPredicateWordCode]{Key: "OppProtect", Val: wordPredicateWordOppProtect},
	state.StrEntry[wordPredicateWordCode]{Key: "Historic", Val: wordPredicateWordHistoric},
	state.StrEntry[wordPredicateWordCode]{Key: "AdventureCard", Val: wordPredicateWordAdventureCard},
	state.StrEntry[wordPredicateWordCode]{Key: "IsCommander", Val: wordPredicateWordIsCommander},
	state.StrEntry[wordPredicateWordCode]{Key: "blockingSource", Val: wordPredicateWordBlockingSource},
	state.StrEntry[wordPredicateWordCode]{Key: "blockedBySource", Val: wordPredicateWordBlockedBySource},
	state.StrEntry[wordPredicateWordCode]{Key: "hasANonBasicLandType", Val: wordPredicateWordHasANonBasicLandType},
	state.StrEntry[wordPredicateWordCode]{Key: "hasABasicLandType", Val: wordPredicateWordHasABasicLandType},
	state.StrEntry[wordPredicateWordCode]{Key: "FullyUnlocked", Val: wordPredicateWordFullyUnlocked},
	state.StrEntry[wordPredicateWordCode]{Key: "Outlaw", Val: wordPredicateWordOutlaw},
)

type wordPredicateSharesCode uint16

const (
	wordPredicateSharesCreatureTypeWith wordPredicateSharesCode = iota + 1
	wordPredicateSharesCardTypeWithOther
	wordPredicateSharesColorWithOther
	wordPredicateSharesAllCardTypesWithOther
	wordPredicateSharesNameWith
)

var wordPredicateSharesCodes = state.NewStrCodes(
	state.StrEntry[wordPredicateSharesCode]{Key: "sharesCreatureTypeWith", Val: wordPredicateSharesCreatureTypeWith},
	state.StrEntry[wordPredicateSharesCode]{Key: "sharesCardTypeWithOther", Val: wordPredicateSharesCardTypeWithOther},
	state.StrEntry[wordPredicateSharesCode]{Key: "SharesColorWithOther", Val: wordPredicateSharesColorWithOther},
	state.StrEntry[wordPredicateSharesCode]{Key: "sharesAllCardTypesWithOther", Val: wordPredicateSharesAllCardTypesWithOther},
	state.StrEntry[wordPredicateSharesCode]{Key: "sharesNameWith", Val: wordPredicateSharesNameWith},
)

type wordMatchesCode uint16

const (
	wordMatches1 wordMatchesCode = iota + 1
	wordMatches2
)

var wordMatchesCodes = state.NewStrCodes(
	state.StrEntry[wordMatchesCode]{Key: "1", Val: wordMatches1},
	state.StrEntry[wordMatchesCode]{Key: "2", Val: wordMatches2},
)
