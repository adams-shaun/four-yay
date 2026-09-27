package state

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// Counter is one counter kind on an object. A slice, not a map: it clones by
// copy and iterates in a fixed order.
type Counter struct {
	Kind string
	N    int32
}

// Target is a chosen target. Exactly one of Obj and Player is meaningful.
type Target struct {
	Obj      ObjID
	Player   PlayerID
	IsPlayer bool
}

// GoadEffect is one independently-lived CR 701.38 goad relationship.
// Source and Duration preserve the condition Forge attached to the effect;
// Controller is the target creature's controller when an AsLongAsControl
// relationship began. All fields are reconstructed by events.Apply.
type GoadEffect struct {
	Player     PlayerID
	Source     ObjID
	Controller PlayerID
	Duration   string
}

// SacrificedInfo is the last-known-information snapshot of an object at the
// instant it was sacrificed, captured before the sacrifice's zone change
// (CR 608.2g "last known information"): the object is in the graveyard by
// the time a resolving ability reads "the sacrificed creature's power"
// (Ghoulcaller Gisa), and Move resets its counters while a graveyard object
// has no characteristics from the layer system at all, so the values must be
// captured while the permanent is still on the battlefield. It carries only
// the characteristics the Sacrificed$<Property> SVar heads ask for -- power,
// toughness and mana value -- rather than a full Object clone, since that is
// all the corpus uses this mechanism for.
type SacrificedInfo struct {
	Obj       ObjID
	Power     int32
	Toughness int32
	ManaValue int32
	// Counters is the object's counter kinds and counts at the instant of
	// the sacrifice (the sacrificed-LKI ladder's third rung: a
	// cost-sacrificed source whose EachFromSource$ copy reads it after Move
	// cleared the live counters -- Zack Fair's self-sacrifice). Nil for the
	// P/T-only readers the Sacrificed$<Property> heads are.
	Counters []Counter
}

// LKIObject is the last-known-information snapshot captured by a
// ChangeZoneRememberLKI$ move. Controller and Owner preserve the pre-move
// relationship (events.Apply's Move resets a battlefield departure's
// controller to its owner); Snapshot preserves the object's pre-move
// characteristics and counters for resolution-local Remembered readers.
type LKIObject struct {
	Obj        ObjID
	Controller PlayerID
	Owner      PlayerID
	Snapshot   Object
}

// CastFlags bits record how an object was cast. Several can be set at once
// (a spell can be both kicked and cast via flashback), so they are
// OR-combined into one byte rather than modeled as separate bools.
const (
	FlagKicked uint64 = 1 << iota // CR 601.2b: paid an optional additional cost
	FlagSurged
	FlagFlashback
	FlagMiracle
	// Appended below the four original bits, following the enum's own
	// append-only precedent: these mark alternative-cost casts (CR 601.2b
	// records how a spell was cast) read by the ETB/keyword machinery.
	FlagEvoked     // evoke: paid the evoke cost (CR 702)
	FlagDashed     // dash: paid the dash cost (CR 702)
	FlagOverloaded // overload cast (CR 702)
	FlagWarped     // warp cast: exile at next end step, may recast from exile (CR 702)
	// FlagBuyback returns the resolving spell to its owner's hand.
	FlagBuyback
	// FlagHarmonize and FlagSuspend exile the spell after it resolves.
	FlagHarmonize
	FlagSuspend
	// FlagEscaped marks a cast paid for with its Escape cost (CR 702.42a);
	// it survives onto the permanent, where the "sacrifice it unless it
	// escaped" ETB family and the escape-with-counters replacements read it
	// through the Card.Self+escaped spec.
	FlagEscaped
	// FlagMayPlay marks a cast made through a may-play-from-zone grant
	// (CR 401.5); the MayPlayLimit$ once-per-turn cap reads it from the log
	// (rules' mayPlaysThisTurn), the same CastInfo provenance marker the
	// Suspend flag is.
	FlagMayPlay
	// FlagKicked1/FlagKicked2 mark the and/or Kicker's two independent
	// optional costs ("Kicker {G} and/or {1}{U}", Forge's colon-separated
	// two-part Kicker:<a>:<b> line -- the Volver/Battlemage cycle family,
	// 18 corpus files): each records WHICH kicker was paid, so the
	// "Card.Self+kicked 1" / "kicked 2" trigger/replacement specs read the
	// specific part. Any part paid also sets FlagKicked, so every bare
	// "kicked" reader (the Condition$ Kicked gate, the spellScope
	// constraint) keeps meaning "some kicker was paid". Appended after the
	// enum's own append-only precedent.
	FlagKicked1
	FlagKicked2
	// FlagNoCounter marks a spell whose payment spent mana produced by an
	// AddsNoCounter$ mana ability (Cavern of Souls' creature-of-the-chosen
	// type mana, Boseiju's instant-or-sorcery mana): "that spell can't be
	// countered". It rides the pay-time CastInfo provenance marker (rules'
	// payCast ORs it into the same event the X value and mode flags ride) and
	// is read by the Counter primitive through the can't-be-countered gate.
	FlagNoCounter
	// FlagAdventure marks a cast of an Adventure card's Adventure spell face
	// (CR 714.3a); it is the provenance the resolution reader (spellRestZone)
	// uses to exile the spell into the adventure zone instead of the
	// graveyard. The zone's own provenance -- "this card sits in the
	// adventure zone" -- is log-derived (rules' adventureZoneAvailable)
	// because CastFlags reset on the very stack->exile move the resolution
	// makes. Appended per the enum's own append-only precedent.
	FlagAdventure
	// FlagReplicated marks a cast that paid its Replicate cost at least once
	// (CR 702.55a). The payment COUNT rides the same pay-time CastInfo's
	// Amount into state.Object.ReplicateTimes -- no carrier pairs {X} with
	// Replicate (measured), so the two never compete for the Amount field --
	// and the copy trigger's Count$ReplicatePaid reads it off the cast spell.
	// Appended per the enum's own append-only precedent.
	FlagReplicated
	// FlagConverged marks a cast whose pay-time CastInfo carries CR
	// 107.4f-family converge provenance: the Amount is the number of
	// distinct colours (WUBRG) of mana actually spent to cast the spell,
	// routed into Object.ConvergeColours. Emitted only for faces carrying a
	// Count$Converge SVar (rules/cast.go's faceWantsConverge), so a
	// non-converge cast stays byte-identical. Appended per the enum's own
	// append-only precedent.
	FlagConverged
	// FlagBestowed marks a cast paid for with the card's Bestow cost
	// (CR 702.114a): the spell was an Aura spell with enchant creature, and
	// the permanent that enters attached reverts to a creature when the
	// attachment ends. It is the provenance rules/stack.go's resolution
	// reader uses to substitute the synthesized Aura attach spell for the
	// face's (absent) spell ability. Appended per the enum's own
	// append-only precedent.
	FlagBestowed
	// FlagMultikicked marks a cast whose pay-time CastInfo carries CR
	// 702.43 multikicker provenance: the Amount is the number of times the
	// multikicker cost was paid, routed into Object.TimesKicked. Any
	// multikicked cast also sets FlagKicked (a multikicked cast IS a kicked
	// cast -- the bare predicate and the Condition$ Kicked gate keep
	// matching). Emitted only for a multikicked-mode cast with count > 0 or
	// a plain-Kicker cast mode whose face carries a Count$TimesKicked SVar
	// (rules/cast.go's faceWantsTimesKicked), so unrelated kicked casts stay
	// byte-identical. Appended per the enum's own append-only precedent.
	FlagMultikicked
	// FlagForetold marks a cast paid for with the card's Foretell cost
	// (CR 702.126a). The flag is SET TWICE in a foretold card's life, once
	// by each provenance marker: the {2} face-down hand exile (rules'
	// payCast foretell branch -- the action is not a cast, so only this
	// action ever sets it there) and the later foretell-cost cast from exile
	// (modeFlags("foretell_cast")), so the cast spell and the permanent it
	// becomes both carry it -- the stack->battlefield persistence is what
	// lets an ETB reader (Lupine Harbingers' CheckSVar$ WasForetold) and
	// Count$Foretold read it. An ordinary cast or any other way into exile
	// never sets it. Appended per the enum's own append-only precedent.
	FlagForetold
	// FlagManaSpent marks a cast whose pay-time CastInfo carries the TOTAL
	// mana actually spent to cast it (CR 601.2h's payment; task castprov1's
	// Count$CastTotalManaSpent capture, the FlagConverged pattern: the flag
	// routes the Amount into Object.ManaSpent instead of overwriting X).
	// Only a face whose SVar table reads the count (faceWantsCastSpend)
	// emits the event, so every unrelated cast stays byte-identical.
	// Appended per the enum's own append-only precedent.
	FlagManaSpent
	// FlagManaSnowSpent marks a cast whose pay-time CastInfo carries the
	// SNOW-unit part of the total mana spent to cast it (CR 107.4h; task
	// castfilter1's filtered Count$CastTotalManaSpent Snow capture, the
	// FlagManaSpent pattern: the flag routes the Amount into
	// Object.ManaSnowSpent instead of overwriting X or the unfiltered
	// total). It rides its own trailing pay-time CastInfo immediately after
	// FlagManaSpent's, so the two totals never share an event. Appended per
	// the enum's own append-only precedent.
	FlagManaSnowSpent
	// FlagManaTreasureSpent / FlagManaCaveSpent / FlagManaDesertSpent mark a
	// cast whose pay-time CastInfo carries the TREASURE-/CAVE-/DESERT-sourced
	// part of the total mana spent to cast it (task castfilter2's filtered
	// Count$CastTotalManaSpent <Type> captures, the FlagManaSnowSpent
	// pattern: the flag routes the Amount into its Object field instead of
	// overwriting X, the total or an earlier tag). Each rides its OWN
	// trailing pay-time CastInfo immediately after the previous tag's, so
	// the four totals never share an event, and events.Apply's CastInfo
	// switch checks the NEWEST flag first (Desert, Cave, Treasure, then
	// Snow, then the total) because the emission order is total, snow,
	// Treasure, Cave, Desert and every later event carries all earlier
	// flags. Appended per the enum's own append-only precedent.
	FlagManaTreasureSpent
	FlagManaCaveSpent
	FlagManaDesertSpent
	// FlagManaArtifactSpent marks a cast whose pay-time CastInfo carries the
	// ARTIFACT-sourced part of the total mana spent to cast it (task
	// mayplay-mfa: the filtered Count$CastTotalManaSpent Artifact capture and
	// the CastSa Spell.ManaFromArtifact predicate, the FlagManaDesertSpent
	// pattern). It rides its OWN trailing pay-time CastInfo immediately after
	// Desert's -- the fifth and newest tag -- so the five totals never share
	// an event, and events.Apply's CastInfo switch checks the NEWEST flag
	// first (Artifact, Desert, Cave, Treasure, then Snow, then the total).
	// Appended per the enum's own append-only precedent.
	FlagManaArtifactSpent
	// FlagReplaceGraveyard marks a cast begun by a DB$ Play SA whose
	// ReplaceGraveyard$ Exile rider says the played spell must not rest in
	// the graveyard: "If that spell would be put into your graveyard this
	// turn, exile it instead" (Goblin Dark-Dwellers). The provenance of a
	// Play SA is cast-time (task replplay1), so the bit ORs into the same
	// pay-time CastInfo every other mode flag rides, and the resolution
	// reader spellRestZone (and the fizzle reader spellFizzleZone) uses it
	// to send the played card to exile. Appended per the enum's own
	// append-only precedent.
	FlagReplaceGraveyard
	// FlagAftermath marks a cast of a Split card's Aftermath alternate face
	// from the graveyard (CR 702.85a; the Flashback convention): the flag is
	// what the resolution reader (spellRestZone) and the fizzle reader
	// (spellFizzleZone) read to exile the card instead of the graveyard,
	// both on resolution and when countered. Appended per the enum's own
	// append-only precedent.
	FlagAftermath
	// FlagConspired marks a cast whose Conspire tap (CR 702.78a) was
	// actually paid: as the spell was cast, two untapped creatures the
	// caster controlled that shared a colour with it were tapped. The flag
	// is the provenance the Conspire keyword expansion's copy trigger reads
	// through Count$Conspired, so a DECLINED/plain cast (no tap paid) emits
	// no flag and resolves exactly like the plain cast. Appended per the
	// enum's own append-only precedent.
	FlagConspired
	// FlagMutated marks a spell cast for its Mutate cost (CR 702.140a). It is
	// the provenance rules/stack.go's resolution reader uses to merge the
	// spell's card into its target instead of moving it to the battlefield as
	// an ordinary permanent. FlagMutatedTop carries CR 702.140b's placement
	// choice (the mutating card goes on TOP of the target); its absence means
	// the mutating card goes UNDER. Both are set by the pay-time CastInfo the
	// cast's mutate-placement answer rides, so the choice is replay-derived.
	// Appended per the enum's own append-only precedent.
	FlagMutated
	FlagMutatedTop
	// FlagSquadPaid marks a cast that paid its Squad cost at least once
	// (CR 702.66): "As an additional cost to cast this spell, you may pay
	// [cost] any number of times." The payment COUNT rides the same
	// pay-time CastInfo's Amount into Object.SquadPaid, and the keyword
	// expansion's ETB trigger reads it through Count$SquadPaid to create
	// that many token copies. Appended per the enum's own append-only
	// precedent.
	FlagSquadPaid
	// FlagOffspringPaid marks a cast that paid its Offspring additional cost
	// (CR 702.175a): "You may pay an additional [cost] as you cast this
	// spell. If you do, when this creature enters, create a 1/1 token copy
	// of it." Offspring is paid at most once, so the provenance is a bool,
	// not a count: the keyword expansion's ETB trigger reads it through
	// Count$OffspringPaid to decide whether to mint the 1/1 copy. Appended
	// per the enum's own append-only precedent.
	FlagOffspringPaid
	// FlagOptionalCostPaid marks a self-spell OptionalCost additional cost.
	FlagOptionalCostPaid
	// FlagConvoked marks a cast whose pay-time CastInfo carries CR 702.66
	// convoke provenance: the creatures the caster tapped to help pay for
	// the cast ride the event's IDs into Object.Convoked. The flag is what
	// Defined$ Convoked reads (Lethal Scheme's connive sub, Venerated
	// Loxodon's and Zephyr Singer's ETB triggers). Emitted only for a face
	// whose SVar table or abilities reference the selector (rules/cast.go's
	// faceWantsConvoked), so every unrelated convoke cast stays
	// byte-identical. Appended per the enum's own append-only precedent.
	FlagConvoked
	// FlagFused marks a Fuse cast (CR 702.101b) of a non-Room Split card:
	// one spell paid the combined mana cost of both halves and resolves both
	// halves' spell abilities in sequence. It is the provenance
	// rules/stack.go's resolution reader dispatches on to run BOTH faces
	// rather than the single Face().SpellAbility(). Appended per the enum's
	// own append-only precedent.
	FlagFused
	// FlagManaExpendCast marks a cast whose pay-time CastInfo is the
	// trig:ManaExpend wake-up (the FlagManaSpent pattern): the Amount is the
	// mana the cast's payment spent (state.Mana pips summed), read by the
	// crossing matcher (rules/trigmatch_cast.go's manaExpendMatches). Emitted
	// only when a ManaExpend trigger face is on the casting player's
	// battlefield (rules/cast.go's manaExpendReaderOut), so every game without
	// a carrier stays byte-identical. The cumulative per-turn tally the
	// crossing is measured against is ENGINE SCRATCH, not this event: it must
	// include casts made before the carrier entered, which emit no event.
	// Appended per the enum's own append-only precedent.
	FlagManaExpendCast
	// FlagJumpstart marks a cast paid for with the card's Jump-start keyword
	// (CR 702.84a): the card was cast from the graveyard by discarding a
	// card in addition to paying its other costs. It is the provenance
	// rules/stack.go's resolution reader (spellRestZone) and fizzle reader
	// (spellFizzleZone) use to exile the card instead of the graveyard, both
	// on resolution and when countered. Appended per the enum's own
	// append-only precedent.
	FlagJumpstart
	// FlagMayFlashSac marks a spell cast off-sorcery through its K:MayFlashSac
	// permission (CR 702.8's "you may cast this as though it had flash" plus
	// the keyword's own "sacrifice it at the beginning of the next cleanup
	// step" rider). It is set by the pay-time CastInfo only when the cast was
	// NOT at a time a sorcery could have been cast, so the keyword's ETB hook
	// (rules/altcast.go's altCostEnter) can register the delayed sacrifice.
	// Appended per the enum's own append-only precedent.
	FlagMayFlashSac
	// FlagCompleated marks the pay-time CastInfo carrying life paid for a
	// printed K:Compleated planeswalker's Phyrexian symbols.
	FlagCompleated
	// FlagMayhem marks a cast paid for with the card's K:Mayhem alternative
	// cost (the Doom Prevails "may cast this card from your graveyard for
	// <cost> if you discarded it this turn" keyword): the flag is the
	// provenance the Card.CastSa Spell.Mayhem condition reads (Sandman's
	// Quicksand's "if this spell's mayhem cost was paid" split). Appended
	// per the enum's own append-only precedent.
	FlagMayhem
	// FlagMorphed / FlagMegamorphed / FlagDisguised mark a face-down cast
	// paid for with the {3} morph-family alternative cost (CR 702.37a
	// Morph, 702.168a Megamorph, 702.169a Disguise). The flag names the
	// KEYWORD FAMILY the cast rode: the resolution reader
	// (rules/stack.go resolveTop) dispatches on the set to resolve the
	// spell with no printed spell abilities and no targets (CR 708.4),
	// rules/resolution.go's moveResolvedOffStack re-carries the face-down
	// entry marker so the permanent enters face down (CR 708.5), and a
	// later turn-face-up action reads the family to price its cost (the
	// printed keyword parameter, unchanged on the face). They shape the
	// resolution like FlagBestowed/FlagFused do, so they are deliberately
	// NOT in CastProvenanceFlags. Appended per the enum's own append-only
	// precedent.
	FlagMorphed
	FlagMegamorphed
	FlagDisguised
	// FlagPromisedGift marks a spell cast with the CR 702.168 Gift promise:
	// the caster named an opponent as the gift's receiver. It is folded by
	// events.GiftPromise from the cast-flow election and read by the
	// PromisedGift filter predicate, the Count$PromisedGift head and the
	// Defined$ Promised / TokenOwner$ Promised referent. It is a
	// CastProvenanceFlag because the promise is a statement about the CAST
	// (a copy was never cast, so it must not inherit it -- CR 707.10).
	// Appended per the enum's own append-only precedent.
	FlagPromisedGift
	// FlagRebound marks a spell cast from its controller's HAND whose face
	// carries K:Rebound (CR 702.95a: "If you cast this spell from your hand,
	// exile it as it resolves"). It is set by payCast from the pendingCast's
	// origin zone, so the re-bound cast from exile -- which CR 702.95e says
	// "doesn't rebound again" -- carries no bit and resolves to the
	// graveyard like any other spell. rules/stack.go's spellRestZone reads it
	// to exile on resolution (the fizzle reader deliberately does not: a
	// countered rebound spell never resolves and stays in the graveyard), and
	// rules/resolution.go's moveResolvedOffStack reads it to register the
	// delayed upkeep recast. It IS a CastProvenanceFlag: CR 702.95a's
	// exile-and-promise is conditioned on the spell having been CAST from its
	// controller's hand, so a stack copy -- put on the stack, never cast
	// (CR 707.10/706.10) -- must not inherit it; the resolution destination
	// follows the flag, not the other way round.
	// Appended per the enum's own append-only precedent.
	FlagRebound
	// FlagBlitzed marks a cast paid for with the card's Blitz cost (CR
	// 702.152a): the spell was cast from hand (or, via a MayPlay$ Spell.Blitz
	// static, from a graveyard) for the keyword's alternative cost. The
	// permanent it becomes gains haste and "When this creature dies, draw a
	// card", and is sacrificed at the beginning of the next end step -- all
	// read by rules/altcast.go's entry hook (blitzEnter). It IS a
	// CastProvenanceFlag: the whole rider package is conditioned on the spell
	// having been CAST for its blitz cost ("If you cast this spell for its
	// blitz cost"), so a stack copy -- put on the stack, never cast
	// (CR 707.10) -- must not inherit it. Appended per the enum's own
	// append-only precedent.
	FlagBlitzed
	// FlagAddsCounters marks a cast whose payment consumed mana produced by
	// an ability carrying an AddsCounters$ rider (Opal Palace, Biophagus,
	// Animal Attendant, Guildmages' Forum): "if this mana is spent to cast
	// [a matching spell], it enters with additional counters". The grants
	// ride the event's Text into Object.ManaAddsCounterGrants, and rules'
	// entry-counter plan reads the snapshotted riders at battlefield entry.
	// It IS a CastProvenanceFlag: a stack copy was never cast (CR 707.10).
	// Appended after main's FlagBlitzed to preserve its bit.
	FlagAddsCounters
)

// CastProvenanceFlags is the ONE home for the CastFlags bits whose reader
// turns them into an obligation conditioned on the object having been CAST
// ("if you cast it ..."). A stack copy is PUT on the stack, never cast
// (CR 707.10/706.10), so events.Apply's StackCopy case strips this set from
// the flags it inherits: the copy resolves, Move turns it into a token and
// clears IsCopy, and rules/altcast.go's entry hook would otherwise read the
// inherited bit and hand a never-cast token the obligation.
//
// FlagMayFlashSac, FlagMayhem and FlagMayPlay are in the set. FlagMayPlay's
// reader is the CastSa Spell.MayPlaySource provenance predicate: a copied
// spell was not cast through a may-play permission.
// FlagMayhem's reader is
// Sandman's Quicksand's Card.CastSa Spell.Mayhem condition -- "if this
// spell's mayhem cost was PAID" is a statement about the cast, so a copy
// (never cast) must not inherit it.
//
// The three sibling bits
// that entry hook also reads -- FlagEvoked, FlagDashed, FlagWarped -- are
// conditioned on an alternative COST having been paid, which is a choice
// made as the spell was cast and which the copy rules do carry for the
// comparable cases (the copied-kicker precedent), so changing them is a
// separate ruling with its own corpus measurement. Add a bit here only when
// its reader's condition is the cast itself.
// FlagPromisedGift joins the set: the promise is made as the spell is cast
// (CR 702.168a), so a stack copy -- put on the stack, never cast -- cannot
// inherit it and the copy's PromisedGift predicate and Count$PromisedGift
// head read false.
// FlagRebound joins the set for the same reason: CR 702.95a's rider is
// conditioned on the cast ("If you cast this spell from your hand, exile it
// as it resolves"), so a stack copy -- never cast, its origin a stack mint
// rather than a hand -- resolves without the exile-and-promise.
// FlagAddsCounters joins the set: the mana-spend rider's grant is conditioned
// on the mana having been spent to CAST this spell ("if you spend this mana to
// cast your commander, it enters with ..."), so a copy -- put on the stack,
// never cast -- must not inherit the grants. FlagBlitzed is likewise a
// cast-cost-conditioned entry rider.
const CastProvenanceFlags = FlagMayFlashSac | FlagMayhem | FlagMayPlay | FlagPromisedGift | FlagRebound | FlagBlitzed | FlagAddsCounters

// ExilesLeavingStack reports whether a cast carrying these flags is a
// keyword cast whose card is exiled as it leaves the stack, whichever way it
// leaves it (resolving, fizzling or being countered): flashback (CR 702.34a),
// jump-start (CR 702.84a), aftermath (CR 702.85a) and harmonize (whose
// "exile it instead of putting it into your graveyard" is the same
// destination).
//
// This is ONE home for the set so a new keyword with the same destination
// cannot be added to rules' resolution/fizzle readers and forgotten by the
// counter path in effects (which is the shape that missed jump-start's
// predecessor flags): every site reads this predicate. It is deliberately
// narrower than each caller's full condition -- spellRestZone also exiles
// copies, an adventure face and a ReplaceGraveyard$ play, and spellFizzleZone
// the same minus the adventure face -- so those extras stay at their call
// sites and only the keyword-family set is shared here.
func ExilesLeavingStack(flags uint64) bool {
	return flags&(FlagFlashback|FlagHarmonize|FlagJumpstart|FlagAftermath) != 0
}

// WasCastFromGraveyard reports whether a cast carrying these flags was made
// from a graveyard by a keyword that grants such a cast: flashback
// (CR 702.32a), jump-start (CR 702.84a), harmonize and escape (CR 702.42a).
// It is the flags-only body of the Card.wasCastFromGraveyard predicate
// (effects/filter.go); object-aware readers must call
// ObjectWasCastFromGraveyard instead, which adds the never-cast guard.
func WasCastFromGraveyard(flags uint64) bool {
	return flags&(FlagFlashback|FlagHarmonize|FlagJumpstart|FlagEscaped) != 0
}

// ObjectWasCastFromGraveyard is the ONE home for the object-aware read of
// the graveyard-origin cast provenance: the flags test PLUS the never-cast
// guard. A stack copy is PUT on the stack, never cast (CR 707.10/706.10),
// so IsCopy reads false even though events.Apply's StackCopy case leaves the
// graveyard-origin bits inherited (state.CastProvenanceFlags does not strip
// them). Every reader must call this, never the flags test directly.
func ObjectWasCastFromGraveyard(o *Object) bool {
	return o != nil && !o.IsCopy && WasCastFromGraveyard(o.CastFlags)
}

// ModeChoice is one ChoiceRestriction$ pick recorded on an object: the
// chosen Choices$ SVar name and the restriction scope the picking Charm
// named. Turn and Combat stamp the event-folded combat identity for
// ModeScopeYourLastCombat picks; ThisTurn and ThisGame do not use the stamp.
type ModeChoice struct {
	Mode   string
	Scope  string
	Turn   int32
	Combat int32
}

// The ChoiceRestriction$ scopes and the events.Choose counter key a pick is
// recorded under. state owns them so effects (which emits the pick) and events
// (which folds it) cannot drift apart. ThisTurn, ThisGame and YourLastCombat
// are modelled end to end; YourLastCombat stamps are aged at each controller
// combat start.
const (
	ModeScopeThisTurn       = "ThisTurn"
	ModeScopeThisGame       = "ThisGame"
	ModeScopeYourLastCombat = "YourLastCombat"

	// ModeChoiceCounterPrefix + a scope is the events.Choose Counter value
	// that records one pick (Text is the chosen mode name).
	ModeChoiceCounterPrefix = "mode-"
)

// Object is any game object: a card in a zone, a permanent, or a spell on the
// stack. One struct keeps identity stable across zone changes.
type Object struct {
	ID         ObjID
	Card       *cards.Card
	FaceIdx    uint8
	Owner      PlayerID
	Controller PlayerID
	Zone       Zone

	Tapped     bool
	SummonSick bool
	Damage     int32
	Counters   []Counter

	// Zone-entry and damage history are derived exclusively in events.Apply
	// from MoveZone/Draw/PutOnStack and Damage. They persist until the next
	// TurnChange, so filters can answer Forge's ThisTurnEntered* and
	// wasDealtDamageThisTurn vocabulary without scanning a partial event log.
	// EnteredFrom is meaningful only while EnteredThisTurn is true.
	EnteredThisTurn        bool
	EnteredFrom            Zone
	WasDealtDamageThisTurn bool
	// DamageTakenByGame lists, in append order, every damage SOURCE that has
	// dealt this object damage this game (game-long; never cleared at
	// TurnChange). Appended by events.Apply's DamageProvenance case with a
	// dedup, the object-side twin of Player.DamageTakenByGame, so the
	// wasDealtDamageByThisGame / wasDealtDamageThisGameBy object predicates
	// read it as a membership test. CloneDeep deep-copies it so a snapshot
	// never aliases the live object's backing array.
	DamageTakenByGame []ObjID
	// The control-acquisition tuple (AcqTurn, AcqStep) records WHEN this
	// object last came under its current controller's control on the
	// battlefield: stamped by events.Apply on every battlefield ENTRY (Move,
	// a real CR 400.7 new-object zone change — a battlefield→battlefield
	// stay is not a new acquisition) and on every battlefield ControlChange.
	// kw:Echo's intervening-if (CR 702.35a) compares it against the
	// controller's Player.LastUpkeepTurn. Written ONLY inside events.Apply
	// so a live game and a replay derive it identically; Clone copies both
	// with the struct.
	AcqTurn int32
	AcqStep Step
	// ActivatedThisTurn counts the non-mana activated abilities whose
	// activation minted an AbilityPush with this source this turn
	// (events.Apply's AbilityPush case, and the granted/gained mints --
	// GrantAbilityPush, GainedAbilityPush, KeywordAbilityPush and a
	// self-grant's "granted ability" DelayedPush -- which are activations
	// of the same recipient). Mana abilities never mint one (CR
	// 605.3a: they are structurally off the stack), so the count is exactly
	// the repeatable-ability churn a bot policy needs to bound its own
	// loop-shaped activations (Basalt Monolith's "{3}: Untap this artifact"
	// re-enabling its own tap forever) without ever misreading a human's
	// legal unlimited activations -- the count is advice, never a gate.
	ActivatedThisTurn int32

	// AttacksThisTurn counts the DeclareAttackers events this object has
	// attacked in this turn (events.Apply's DeclareAttackers case), reset in
	// TurnChange's per-object loop. Extra combats within one turn share
	// g.Turn and do NOT reset it, so a "attacks for the first time each
	// turn" trigger (rules/trigger_match.go attacksMatches' FirstAttack$)
	// reads count == 1 at fire time -- trigger matching runs on the FOLDED
	// event, so the event's own attack is already counted.
	AttacksThisTurn int32

	// ExertedThisTurn records CR 702.100a's exert election (task exert1):
	// the permanent was exerted this turn. Set only by events.Apply's Exert
	// case; reset in TurnChange's per-object loop (a per-turn fact) and
	// cleared with ExertSkipUntap when the permanent leaves the battlefield
	// (CR 400.7: a new object never carries the old object's exerted
	// status). The filter predicate notExertedThisTurn reads it, which is
	// what Combat Celebrant's IsPresent$ offer gate evaluates.
	ExertedThisTurn bool

	// ExertSkipUntap records CR 702.100b's other lifetime: an exerted
	// creature won't untap during its controller's NEXT untap step, a
	// window that spans the turn boundary (TurnChange fires between the
	// exerting turn's cleanup and the next untap step), so TurnChange does
	// NOT reset it. It is consumed at use, the regeneration-shield
	// expire-at-use precedent: the untap-step scan (rules/turn.go
	// finishUntapStep) skips the untap of a permanent carrying it and emits
	// an Exert event with Amount -1, whose fold clears the flag. Cleared
	// with ExertedThisTurn on leaving the battlefield. Untap effects are
	// unaffected: CR 702.100b names only the untap step, and the skip is
	// implemented in the turn scan, never in effects.TryUntap.
	ExertSkipUntap bool

	// EnlistedTurn and EnlistedCombat stamp the CR 702.160 enlist action (the
	// `K:Enlist` keyword, task enlist1): the turn and combat phase in which
	// this attacking creature last enlisted another creature. They are set
	// together by events.Apply's Enlist case, so the enlistedThisCombat
	// filter predicate (effects/filter.go) can answer "enlisted THIS combat"
	// against the live g.Turn/g.CombatsThisTurn -- a same-turn extra combat
	// begins with a higher CombatsThisTurn and the stamp correctly no longer
	// matches it. Both are cleared in TurnChange's per-object loop (a
	// per-combat fact) and when the permanent leaves the battlefield (CR
	// 400.7: a new object never carries the old object's enlist status).
	EnlistedTurn   int32
	EnlistedCombat int32

	// preStackEntry* carries a card's entry history only while it is on the
	// stack. events.Apply captures it before PutOnStack overwrites the public
	// fields, then restores and clears it for CR 733.1's logged reverse move.
	// It is never a second source of truth for a completed zone change.
	PreStackEntryThisTurn bool
	PreStackEntryFrom     Zone
	// PreStackEnteredLen is the entry-list boundary before a proposed cast.
	// A CR 733.1 reverse restores that boundary, removing the proposal and
	// every reversible cost move it made without touching earlier casts.
	PreStackEnteredLen int
	HasPreStackEntry   bool

	// Incarnation advances whenever an object crosses the battlefield
	// boundary. ObjID is stable for the match, but a permanent that leaves and
	// returns is a new object under CR 400.7; delayed and keyword-triggered
	// actions snapshot this value when tied to that permanent incarnation.
	Incarnation uint32

	// Stack-only.
	Ability           *cards.SA
	Source            ObjID
	SourceIncarnation uint32
	// StackKind is stamped when an event mints this stack object. It is
	// deliberately carried on the object rather than re-derived from Source:
	// CR 113.7a still identifies an ability after its source has left or
	// changed faces.
	StackKind      StackObjKind
	StackKindKnown bool
	Targets        []Target
	// Remembered carries a triggered ability's Ctx.Remembered from the
	// moment it was queued (rules.checkTriggers) through to resolution. An
	// ability object has no Face (Ruling F3) and therefore no card-script
	// route back to "the object that caused this trigger" once it is
	// sitting on the stack, so that has to be data on the object itself,
	// the same way Targets already is for a genuine chosen target. Task 20.
	Remembered []Target

	// Combat-only.
	IsAttacking bool
	Attacking   PlayerID
	// AttackingBattle is the non-player permanent this creature is attacking,
	// or 0 when it attacks a player: a CR 310.7 battle (its ObjID) or a
	// planeswalker (CR 508.1). Attacking still names the defender's seat -- the
	// battle's PROTECTOR or the planeswalker's controller (the player who blocks
	// and whose seat the attack is scoped to) -- so the two fields together
	// carry the whole defender: a player defender leaves AttackingBattle zero, a
	// battle or planeswalker defender sets it to that permanent's ObjID. Set by
	// events.Apply's DeclareAttackers case from the event's Obj and cleared
	// wherever IsAttacking is.
	AttackingBattle ObjID
	BlockedBy       []ObjID
	// EncoreAttackTurn/Defender record "attacks that opponent this turn if
	// able" on an encore token. Zero Turn means no requirement; turns begin
	// at 1, so the zero value is unambiguous.
	EncoreAttackTurn     int32
	EncoreAttackDefender PlayerID

	// Goads holds CR 701.38 attack requirements. Relationships can have the
	// default next-turn lifetime, be permanent, or depend on source/control.
	Goads []GoadEffect

	// Suspected is CR 702.157's suspected designation (the Blame Game precon's
	// Nelly Borca / Hot Pursuit family): a suspected creature has menace and
	// can't block. The designation ends when the permanent leaves the
	// battlefield or another player gains control of it, so events.Apply
	// clears it on both paths -- the same two folds that clear the Ring-bearer
	// designation. It is a plain status field: a plain value copy in
	// CloneDeep carries it, and only events.AlterAttribute (the primitive the
	// api:AlterAttribute effect emits) may set it.
	Suspected bool

	// Monstrous is CR 701.31b's monstrous designation (Giggling
	// Skitterspike's `{5}: Monstrosity 5`): a creature becomes monstrous
	// when a monstrosity ability resolves, and the designation lasts for
	// the rest of the game -- CR 701.31 gives it NO controller-change end,
	// so events.Apply clears it only when the permanent leaves the
	// battlefield (a later battlefield entry is a new permanent, CR 701.31b
	// in reverse). It is a plain status field: a plain value copy in
	// CloneDeep carries it, and only events.AlterAttribute (the mark
	// effPutCounter emits for a `Monstrosity$` PutCounter line) may set it.
	Monstrous bool

	// Renowned is CR 702.112b's renowned designation: it lasts while this
	// permanent remains on the battlefield, regardless of controller changes.
	// Leaving the battlefield clears it; a later entry is a new permanent.
	// Like Monstrous, it is a plain status field copied by CloneDeep and set
	// only by an events.AlterAttribute fold.
	Renowned bool

	// PhasedOut is CR 702.25's phased-out status (api:Phases): the
	// permanent is on the battlefield but is treated as though it does not
	// exist. It is NOT a zone change -- the object keeps its Zone and its
	// Zone() membership -- so no Move fires and no leaves/enters trigger
	// sees it; the status is folded by events.PhaseOut and cleared by
	// events.Apply's Move when the permanent actually leaves the
	// battlefield (CR 400.7: a later entry is a new object, CR 702.25e).
	// Phase-in happens at its controller's untap step (CR 702.25d, rules
	// finishUntapStep). Every reader that treats a permanent as existing
	// gates on it: targeting (rules/stack.go candidatesFor), the layer
	// static/level walk (rules/layers.go staticEffects/activeStatics),
	// combat (rules/combat.go canAttack), the SBA sweep (rules/sba.go)
	// and the view projection (view). A plain value copy in CloneDeep
	// carries it.
	PhasedOut bool
	// WontPhaseInNormal is the CR 702.25d exception carried by a Phases
	// effect; its phase-in must come from that effect's return instruction.
	WontPhaseInNormal bool

	// SuspendGranted is the replayed characteristic grant made by a
	// Pump/PumpAll KW$ Suspend effect. It is separate from CastFlags.FlagSuspend:
	// the latter records the suspend action, while this records gaining the
	// keyword on an exiled card.
	SuspendGranted bool

	// PlottedTurn stamps the turn a card gained CR 701.34's plotted
	// designation (0 = not plotted), via the events.AlterAttribute fold -- the
	// plot ACTION (rules/cast.go) and the corpus's DB$ AlterAttribute |
	// Attributes$ Plotted family both grant it. The designation is pure
	// provenance for the free cast's "on a later turn" gate (rules/legal.go's
	// exile walk compares Game.Turn against it); it ends when the card leaves
	// exile (events.Apply's Move), the CR 701.34c end condition, so a later
	// return to exile cannot revive the permission. A plain value copy in
	// CloneDeep carries it.
	PlottedTurn int32

	// Timestamp orders continuous effects. Assigned from Game.Clock whenever
	// the object enters the battlefield.
	Timestamp uint32

	// Cast-time metadata. X and CastFlags matter while this object is a
	// spell on the stack and, once it resolves, on the permanent it becomes
	// (an ETB "if it was kicked" trigger needs to read them off the
	// permanent) -- events.Move resets both when the object leaves the
	// battlefield.
	X int32
	// CastFlags records how an object was cast (the Flag* bits below).
	// Widened from uint32 to uint64 when FlagSquadPaid became the 33rd bit:
	// the flag word is never serialized -- the event stream carries the flag
	// NAMES as text (events.FlagsString/FlagsFrom) and state.Object is
	// re-derived by replay -- so widening the in-memory word cannot move the
	// hash chain or any golden replay.
	CastFlags uint64
	// ReplicateTimes is CR 702.55a's count of replicate payments the cast
	// made, carried by the pay-time CastInfo's FlagReplicated Amount (the
	// X-overwrite guard: the flag routes the Amount here instead of into X).
	// It rides the same provenance window as X/CastFlags and resets
	// alongside them in events.Move.
	ReplicateTimes int32
	// SquadPaid is CR 702.66's count of squad payments the cast made,
	// carried by the pay-time CastInfo's FlagSquadPaid Amount (the
	// X-overwrite guard: the flag routes the Amount here instead of into X).
	// It rides the same provenance window as X/CastFlags and resets
	// alongside them in events.Move; a copy of the spell was never cast and
	// reads 0 (so a squad token's own ETB trigger creates no further
	// copies).
	SquadPaid int32
	// OffspringPaid is CR 702.175a's provenance that the spell's optional
	// Offspring additional cost was paid as it was cast (a bool, not a
	// count: Offspring is paid at most once). It rides the same provenance
	// window as X/CastFlags and resets alongside them in events.Move; a
	// COPY of the spell was never cast and reads false (the same reading
	// Count$ReplicatePaid documents).
	OffspringPaid bool
	// OptionalCostPaid records the boolean paid provenance for Count$OptionalGenericCostPaid.
	OptionalCostPaid bool
	// ConvergeColours is the number of distinct colours (WUBRG) of mana
	// actually spent to cast the spell (CR 107.4f-family converge), carried
	// by the pay-time CastInfo's FlagConverged Amount. It rides the same
	// provenance window as X/CastFlags and resets alongside them in
	// events.Move; a copy of the spell was never cast and reads 0.
	ConvergeColours int32
	// TimesKicked is CR 702.43's count of times the spell's multikicker cost
	// was paid as it was cast, carried by the pay-time CastInfo's
	// FlagMultikicked Amount (the X-overwrite guard: the flag routes the
	// Amount here instead of into X). A plain-Kicker cast mode's count (1,
	// or 2 for a paid-both two-part Kicker) rides the same flag so the 11
	// legacy Count$TimesKicked carriers read real counts. It rides the same
	// provenance window as X/CastFlags and resets alongside them in
	// events.Move; a COPY of the spell was never kicked and reads 0 (the
	// same reading Count$ReplicatePaid documents).
	TimesKicked int32
	// Conspired is CR 702.78a's provenance that the spell's Conspire tap was
	// paid as it was cast, carried by the pay-time CastInfo's FlagConspired
	// (a bool, not a count: Conspire never copies more than once). It rides
	// the same provenance window as X/CastFlags and resets alongside them in
	// events.Move; a COPY of the spell was never cast and reads false (the
	// same reading Count$ReplicatePaid documents).
	Conspired bool
	// Convoked is CR 702.66's "each creature that convoked it": the ids of
	// the creatures the caster tapped to help pay for the spell's cast,
	// carried by the pay-time CastInfo's FlagConvoked IDs (the
	// Defined$ Convoked selector reads it -- Lethal Scheme's connive sub
	// while the spell is on the stack, Venerated Loxodon's and Zephyr
	// Singer's ETB triggers after it resolves into a permanent). It rides
	// the same provenance window as X/CastFlags and resets alongside them
	// in events.Move; a COPY of the spell was never convoked for and reads
	// empty.
	Convoked []ObjID
	// ManaSpent is the TOTAL mana actually spent to cast the spell (CR
	// 601.2h's payment -- the spent delta's pips summed over every slot),
	// carried by the pay-time CastInfo's FlagManaSpent Amount (the
	// X-overwrite guard: the flag routes the Amount here instead of into
	// X). Convoke contributions are taps and Delve exiles cards, so neither
	// rides the delta: a convoke-only cast's total spend is a real zero.
	// It rides the same provenance window as X/CastFlags and resets
	// alongside them in events.Move; a copy of the spell was never cast and
	// a cheated-in permanent reads 0.
	ManaSpent int32
	// ManaSnowSpent is the SNOW-unit part of ManaSpent: how many of the mana
	// units the cast's payment spent were produced by a Snow permanent (CR
	// 107.4h). It is carried by the pay-time CastInfo's FlagManaSnowSpent
	// Amount (the X-overwrite guard: the flag routes the Amount here instead
	// of into X), the filtered Count$CastTotalManaSpent Snow head's
	// provenance. Snow units are consumed alongside their pool slot
	// (resolveManaWith's parallel tally), so this never exceeds ManaSpent for
	// the same slot; a cast that spent no snow mana is a real 0. It rides the
	// same provenance window as ManaSpent and resets alongside it in
	// events.Move; a copy of the spell was never cast and a cheated-in
	// permanent reads 0.
	ManaSnowSpent int32
	// ManaTreasureSpent / ManaCaveSpent / ManaDesertSpent are the
	// TREASURE-/CAVE-/DESERT-sourced parts of ManaSpent: how many of the
	// mana units the cast's payment spent were produced by a permanent of
	// that type (task castfilter2, the ManaSnowSpent pattern). They are
	// carried by the pay-time CastInfo's FlagManaTreasureSpent /
	// FlagManaCaveSpent / FlagManaDesertSpent Amounts (the X-overwrite
	// guard: each flag routes its Amount here instead of into X, the total
	// or an earlier tag). Typed units are consumed after plain ones and
	// before snow (resolveManaWith's takeUnit order), so the typed splits
	// never exceed ManaSpent for the same slot and never overlap the snow
	// split; a cast that spent none of a tag is a real 0. They ride the
	// same provenance window as ManaSpent and reset alongside it in
	// events.Move; a copy of the spell was never cast and a cheated-in
	// permanent reads 0.
	ManaTreasureSpent int32
	ManaCaveSpent     int32
	ManaDesertSpent   int32
	// ManaAddsCounterGrants are the AddsCounters$ mana-spend rider grants this
	// cast earned (Opal Palace's "if you spend this mana to cast your
	// commander, it enters with ... counters", Biophagus, Animal Attendant,
	// Guildmages' Forum). Each entry is one producing ABILITY's rider
	// snapshotted at PRODUCTION (Filter, Kind, Amount -- a literal or the
	// resolved SVar body) together with Count, how many of that ability's
	// mana units the payment actually spent. It rides the pay-time CastInfo's
	// FlagAddsCounters Text payload and is used verbatim by rules'
	// entry-counter plan: the rider is never re-read from the source's
	// current face at entry, so a copied/modified source or a second,
	// rider-less ability of the same permanent cannot change the grant, and
	// Count rider units yield Count grants. It rides the same provenance
	// window as X/CastFlags and resets alongside them in events.Move; a stack
	// copy was never cast and reads empty (the flag is stripped by
	// CastProvenanceFlags and the field is cleared at the mint).
	ManaAddsCounterGrants []ManaAddsCounterGrant
	// ManaArtifactSpent is the ARTIFACT-sourced part of ManaSpent (task
	// mayplay-mfa): how many of the mana units the cast's payment spent were
	// produced by an Artifact permanent -- Sol Ring, Arcane Signet, the whole
	// mana-rock family. It is carried by the pay-time CastInfo's
	// FlagManaArtifactSpent Amount (the X-overwrite guard, the same pattern
	// the Treasure/Cave/Desert parts take). Artifact units are consumed in
	// the same typed slot partition (resolveManaWith's takeUnit), so this
	// never exceeds ManaSpent for the same slot; a cast that spent no
	// artifact mana is a real 0. It rides the same provenance window as
	// ManaSpent and resets alongside it in events.Move; a copy of the spell
	// was never cast and a cheated-in permanent reads 0.
	ManaArtifactSpent int32
	// CompleatedLifePaid is the amount of life paid for Phyrexian symbols on
	// a printed K:Compleated cast. It follows the cast provenance window and
	// is consumed by events.Move when the spell enters as a planeswalker.
	CompleatedLifePaid int32
	// NotedNumber is the number a trigger's Execute$ body noted onto the
	// CARD (Lupine Harbingers' T:Mode$ ChangesZone | Destination$ Exile
	// trigger executing DB$ Pump | NoteNumber$ Count$YourTurns -- the
	// corpus's one NoteNumber$ carrier). events.NotedNumber carries it,
	// Count$NotedNumber reads it, and it resets with the X/CastFlags window
	// when the permanent leaves the battlefield: the note is made in exile
	// and consumed by the ETB machinery of the cast it later becomes, and a
	// fresh exile re-notes it.
	NotedNumber int32

	// RuntimeSVars is the per-object runtime SVar store (Forge's
	// sa.setSVar / Card.setSVar): the named numeric values an
	// api:StoreSVar body writes during resolution and later reads -- a
	// characteristic-defining ability (Minion of the Wastes' SetPower$
	// LifePaidOnETB), a token's TokenPower$ (Phyrexian Processor), or a
	// Count$ name -- resolve against. It OVERLAYS the printed face SVar
	// table: a runtime entry of a name wins over the card's printed body
	// because the printed body is the default the write replaces. Written
	// only through events.StoreSVar (so a replay derives the identical
	// value), keyed by name and read by exact lookup, so map order can
	// never reach an event, an option list or a view. Cleared together
	// with the cast-time window when the permanent leaves the battlefield
	// (CR 400.7: an object that leaves and returns is a new object).
	RuntimeSVars map[string]int32

	// Notes is the set of card-notation labels this object carries, in the
	// order they were noted (Forge's Card.addNotedFor: `NoteCards$
	// Remembered/TriggeredSource | NoteCardsFor$ <label>` on a DB$ Pump body
	// appends <label> here; the shared card filter's `Card.NotedFor<label>`
	// qualifier reads it -- ChooseCard's Choices$, DB$ Play's Valid$ and
	// RepeatEach's RepeatCards$. CopyPermanent's RevealFromExile cost is an
	// evidenced corpus shape but remains unsupported.
	// It is append-only and never re-ordered, so it is deterministic on
	// replay, and a re-note of the same label does not duplicate the entry.
	// Written ONLY by events.Apply's CardNoted case, so a live game and a
	// log-only reconstruction derive it identically; CloneDeep copies it
	// (the slice is appended to in place, so sharing the backing array would
	// let either game corrupt the other). A note is card-identity provenance,
	// not zone-local state -- the setup-path carriers note cards sitting in
	// exile -- so unlike RuntimeSVars it is never cleared on a zone move;
	// the player-side sibling lives on state.Player.Notes.
	Notes []string

	// Chosen* record answers to "as this enters/resolves, choose ..."
	// effects: a card name, a creature type, a number, a colour (the
	// K:ETBReplacement ChooseColor family -- Utopia Sprawl, Caged Sun,
	// Quirion Elves; the letter the mana path reads when a Produced$ Chosen
	// ability resolves). Reset alongside X/CastFlags when the object leaves
	// the battlefield.
	ChosenName   string
	ChosenType   string
	ChosenNumber int32
	ChosenColor  string
	// RiotChoice is set by the logged as-enters Riot choice. It survives the
	// hand/stack path and Move consumes it on battlefield entry.
	RiotChoice string
	// UntapChoice records the permanent's answer to its untap-step election.
	// It is folded by events.Choose so a replay makes the same turn-based
	// decision; the turn boundary clears it before the next election.
	UntapChoice string
	// UnleashChoice is set by the logged as-enters Unleash choice (CR 702.86:
	// "counter" = enter with a +1/+1 counter, "plain" = enter without). It
	// survives the hand/stack path and Move consumes it on battlefield entry,
	// exactly like RiotChoice.
	UnleashChoice string
	// GiftPromisedTo is the CR 702.168 Gift promise's receiver: the opponent
	// the cast's election named. The promise itself is the CastFlags bit
	// state.FlagPromisedGift (folded by events.GiftPromise, preserved across
	// the stack->battlefield move and stripped from a stack copy by
	// CastProvenanceFlags), so the PromisedGift predicate, the
	// Count$PromisedGift head and the Defined$ Promised referent share one
	// home for the bit; this field carries only the receiver, read by
	// Defined$ Promised / TokenOwner$ Promised. Zero when no promise was
	// made, and reset with the CastFlags window when the object leaves the
	// battlefield or the stack.
	GiftPromisedTo PlayerID
	// Protector is the CR 310.10 Siege protector: the opponent its
	// controller chose to protect this Battle as it entered. It is a property
	// of the battle (not a counter), recorded through a Choose "protector"
	// event so it is replay-derived, and reset when the object leaves the
	// battlefield (a re-entering battle is protected afresh). ProtectorValid
	// distinguishes "no protector chosen yet" from a real protector: seat 0
	// is a legal opponent, so a zero Protector alone is ambiguous.
	Protector      PlayerID
	ProtectorValid bool
	// LastNotedMana is the mana type the object's last RememberCostMana$
	// activation paid with (Jeweled Amulet: "note the type of mana spent to
	// pay this activation cost") — the colour letter(s) of the mana the
	// payment actually spent, in WUBRG order. Folded by events.Choose's
	// "noted-mana" marker; read by effMana's Produced$ "Special
	// LastNotedType". Empty when nothing has been noted (or the object left
	// and returned — a move clears it with the rest of the per-copy state).
	LastNotedMana string
	// IntrinsicKeywords are keyword choices that become part of this
	// permanent's characteristics (currently Riot's haste choice).
	IntrinsicKeywords []string
	// Chosen is the current card/player choice. It is distinct from
	// Remembered: Forge uses Player.Chosen for the most recent choice and
	// Player.IsRemembered for choices explicitly marked RememberChosen$.
	Chosen []Target
	// ETBCloneChoice is the event-backed answer to an ETB copy replacement.
	// Valid distinguishes a decline (zero object) from no election.
	ETBCloneChoice      ObjID
	ETBCloneChoiceValid bool

	// ChosenModes carries a modal spell's CR 601.2b announcement or a modal
	// triggered ability's CR 603.3c placement choice to resolution: the SVar
	// names of the chosen Choices$ sub-abilities, in execution order.
	// Resolution builds Ctx.Modes from this so effCharm runs exactly the
	// chosen modes instead of asking again. It is a cache maintained beside
	// the already-logged ModeChosen marker; replay re-poses and re-answers the
	// same decision through the identical code path, so it is not a second
	// source of truth. Nil when no modal announcement has been made; a
	// NON-nil empty slice is an announcement of zero modes ("choose up to
	// N" answered with none), which must resolve as nothing rather than
	// re-asking -- copy it with CloneChosenModes, which keeps that
	// distinction.
	ChosenModes []string

	// ModeChoices is the persistent per-object log a Charm's ChoiceRestriction$
	// reads (task charm-choice-restriction): every mode this object has chosen
	// under the scope the picking Charm named. Unlike ChosenModes it is NOT
	// cleared when the choosing stack object resolves -- the whole point is
	// that a LATER trigger instance on the same source sees the earlier pick --
	// so it lives on the source permanent and is folded by events.Choose's
	// scope-keyed pick markers. TurnChange prunes ThisTurn entries, combat start
	// ages YourLastCombat entries, and Move's battlefield departure block clears
	// all entries (CR 400.7 -- a permanent that leaves and returns is a new
	// object), so a re-entered permanent offers every mode again.
	// CurCombatTurn/CurCombatCombat identify this object's controller's most
	// recently begun combat and rotate YourLastCombat picks at combat start.
	ModeChoices     []ModeChoice
	CurCombatTurn   int32
	CurCombatCombat int32

	// EncodedCards holds the CIPHER spell cards exiled ENCODED on this creature
	// (CR 702.99a: "exile this spell card encoded on a creature you control").
	// It is the creature-side home of the association: the encoded card is an
	// ordinary card in exile, and each entry stays a live link only while that
	// card remains in exile AND this object remains on the battlefield --
	// events.Move prunes an entry whose card left exile, and clears the whole
	// list when this object leaves the battlefield, so nothing reads a stale
	// link. Event-backed through the Imprint kind's "encoded" Text
	// discriminator, so a replay rebuilds the same association. The combat
	// trigger the encoded card grants reads it at damage time.
	EncodedCards []ObjID

	// Imprinted holds cards ImprintCards$ explicitly associated with this
	// object. It is distinct from ExiledCards: Forge's host card has separate
	// imprintedCards and exiledCards collections, and their consumers must not
	// make an ordinary exile satisfy an Imprinted selector. It is state
	// because later abilities (Chrome Mox) refer to it after the originating
	// resolution has ended.
	Imprinted []ObjID
	// ImprintTokens holds the TOKENS a Token/CopyPermanent effect imprinted on
	// this object through ImprintTokens$ True (Forge's imprintedCards written
	// by TokenEffect) -- the association a following SubAbility$' `Defined$
	// Imprinted` (Timothar's Animate, Intrude on the Mind's PutCounter, Ugin's
	// Effect) reads. Deliberately separate from Imprinted: that list is the CR
	// 607.2a exiled-card link whose reader may only consume entries still in
	// exile, while a token imprint is a battlefield permanent and must
	// resolve while it is on the battlefield.
	ImprintTokens []ObjID
	// SeekFound holds the cards an Alchemy Seek associated with this object
	// through ImprintFound$ True. Forge's SeekEffect writes imprintedCards,
	// but the found cards sit in a HAND at continuation time -- the zone a
	// chained `Defined$ Imprinted` body (Spawning Pod, Gitrog, Kardum, Puppet
	// Raiser) immediately moves on -- so the ordinary Imprinted list's CR
	// 607.2a exiled-only reader would hide them. A separate list keeps the
	// exile-only Imprinted contract intact while letting the seek-found cards
	// resolve wherever they currently sit. Event-backed through the Imprint
	// kind's "seek-found" Text discriminator and cleared by ClearImprinted$.
	SeekFound []ObjID
	// ExiledCards holds cards this object exiled through ChangeZone (Forge's
	// hostCard.exiledCards). The association exists only while the card
	// remains in exile; events.Move removes it when the card leaves. It is
	// what DefinedCards$ ExiledWith consumes, not the Imprinted list above,
	// and it is distinct from the ExiledWith ObjID field below (that one is
	// the reverse relationship: what exiled THIS card).
	ExiledCards []ObjID

	// ExileReturn holds the cards this object exiled through ChangeZone's
	// Duration$ UntilHostLeavesPlay (the Oblivion Ring / Banisher Priest
	// pattern): each entry's From is the zone the card was exiled from, and
	// when this object leaves the battlefield the rules sweep returns every
	// entry whose object is still in exile to that zone under its owner's
	// control. Like ExiledCards it is a zone relationship, not an imprint:
	// events.Move prunes entries naming an object that left exile by any
	// other path, so a sweep never returns a card whose exile was some other
	// effect's business. Event-backed through the Imprint kind's
	// "until-host-leaves" Text discriminator.
	ExileReturn []ExileReturnEntry

	// MergedCards holds the cards stacked BENEATH a mutated permanent's top
	// card (CR 702.140d), top-of-pile first. Card/FaceIdx on the object always
	// describe the TOP card; each entry here is one card that mutated below it.
	// A merged-under card is not an independent permanent: its object is parked
	// in ZCeased (which has no membership list, so no battlefield scan sees it)
	// and only the pile's own departure moves it (events.Move), which is
	// CR 702.140e's "each card that's merged ... moves to its owner's
	// graveyard" -- and the same move for every other zone. TimesMutated is
	// CR 702.140f's count of how many times this permanent has mutated, read by
	// Count$TimesMutated.
	MergedCards  []MergedCard
	TimesMutated int32

	// AttachedTo is the permanent this Aura or Equipment is attached to; 0
	// means unattached. Reset whenever the object itself leaves the
	// battlefield (events.Move) -- an Aura or Equipment cannot stay
	// "attached" once it isn't a permanent.
	AttachedTo ObjID
	// AttachedPlayer is the player this Aura enchants. HasAttachedPlayer
	// distinguishes seat zero from an unattached Aura. Only events.Attach
	// writes the link; a permanent and a player are mutually exclusive.
	AttachedPlayer    PlayerID
	HasAttachedPlayer bool

	// LastBearer is the permanent this object was MOST RECENTLY attached to
	// before it became unattached, 0 if it never was. It answers the corpus's
	// "objects that WERE attached to it" reads (Cass, Hand of Vengeance;
	// Rhuk, Hexgold Nabber; Fumble; Murderous Spoils), which a trigger
	// resolves only AFTER the attachment sweep has cleared AttachedTo. Like
	// LastNotedMana this field is written ONLY inside events.Apply: set by
	// the Unattached fold from that event's former-bearer carrier, set by
	// the Move-leaves-battlefield fold from the pre-clear AttachedTo, and
	// cleared by an Attach fold that re-attaches. It is a pure fold of
	// already-recorded event bytes, so replay rebuilds it identically and no
	// event encoding or chain head changes.
	LastBearer ObjID

	// ExiledWith is the object whose effect most recently put this card into
	// exile. events.Apply derives it from a MoveZone event's existing IDs
	// carrier (or, for Hideaway's face-down exile, the Counter/Amount
	// carrier), so Card.ExiledWithSource filters replay without ambient
	// state. Zero means no tracked exile provenance.
	ExiledWith ObjID
	// FaceDown records a face-down exile (CR 702.75 Hideaway). It is state,
	// rather than merely a Secret event flag, so later projections know not to
	// reveal the card to another player.
	FaceDown bool
	// MayLookPlayer is the player a Dig's or ChangeZone's WithMayLook$ True
	// authorises to look at this face-down exiled card (Forge's Card.mayLook),
	// with HasMayLook distinguishing seat 0 from "nobody". It is state, not
	// merely an event flag, because the permission lasts as long as the card
	// remains exiled, so a projection long after the exiling event still knows
	// who may see the face. events.Apply folds it from the existing MoveZone
	// Counter/Amount/IDs carriers (the "exiled_with_face_down_maylook" marker),
	// so replay rebuilds it with no new event kind and no Event field change.
	MayLookPlayer PlayerID
	HasMayLook    bool
	// Cloaked records the cloak variant of the face-down battlefield entry
	// (CR 708.5's cloak: a 2/2 creature with ward {2}, turn-face-up cost =
	// the card's mana cost). It folds from the MoveZone Counter value
	// "entered_cloaked" exactly as FaceDown folds from "entered_face_down"
	// -- no new event kind, no Event field change -- and is cleared wherever
	// FaceDown is (leaving the battlefield; a future turn-face-up path).
	Cloaked bool

	// FaceDownSetType is the face-down set type a ChangeZone FaceDownSetType$
	// named (Yedora's "Land & Forest", Missy's "Artifact & Creature &
	// Cyberman"), stored raw as Forge writes it. Empty means CR 708.5's plain
	// face: a vanilla 2/2 creature. While FaceDown and on the battlefield it
	// replaces the synthetic {Creature} type set in the layer-4 derivation.
	FaceDownSetType string
	// FaceDownPower/FaceDownToughness and FaceDownHasPT carry the folded
	// FaceDownPower$/FaceDownToughness$ pair (Magar's 3/3). HasPT reports
	// whether a pair was named; without it a face-down Creature is CR 708.5's
	// 2/2 and a non-Creature set type derives 0/0.
	FaceDownPower     int32
	FaceDownToughness int32
	FaceDownHasPT     bool

	// Paired is the permanent this Soulbond creature is paired with (CR 702.103):
	// a creature its controller may pair it with when either enters an the
	// battlefield, as long as the controller controls both. 0 means unpaired.
	// Reset whenever the object leaves the battlefield (events.Move).
	Paired ObjID

	// IsToken and IsCopy mark an object that only ever exists on the stack
	// or the battlefield (CR 111.7 tokens, CR 707.10 copies). A token copy
	// minted by Myriad (CR 702.109) or a "create a token copy" effect
	// (CR 706.2, DB$ CopyPermanent) carries BOTH and legitimately lives on
	// the battlefield. See Ephemeral.
	IsToken  bool
	IsCopy   bool
	IsMyriad bool

	// CopyMayChooseTarget is CR 707.10c's new-target permission for ONE copy
	// on the stack, carried per copy instance rather than re-derived from the
	// copied spell's text. It is set true by the StackCopy fold when the
	// CREATING CopySpellAbility SA declared MayChooseTarget$ True (the event's
	// Amount discriminator) -- so an external copier (Mirari, Cloven Casting,
	// a Storm or Replicate copy) that is not part of the copied spell's own
	// text still grants the election. rules/stack.go's resolveTop asks the
	// copy's controller exactly once while this is true and records the answer
	// through TargetsChosen, whose fold clears the flag; a log-only replay
	// rebuilds set-then-cleared identically.
	CopyMayChooseTarget bool

	// CopyNonLegendary is the CopySpellAbility NonLegendary$ True rider (The
	// Sixth Doctor's "copy it, except the copy isn't legendary" and the
	// corpus's six-carrier family): a copied legendary permanent SPELL
	// resolves into a token that must not carry the Legendary supertype, or
	// the CR 704.5j legend rule would bin one of the pair. It is set by the
	// StackCopy fold from the creating effect's event OR inherited from a
	// spell copy being copied again (CR 707.2), and read by rules'
	// typeCharacteristics as a layer-4 base strip, so every derived-type
	// reader (Derived.Types, legendaryUnderLayers) agrees without touching
	// the printed face. Engine-runtime, rebuilt from the event stream on
	// replay like CopyMayChooseTarget; false on every ordinary object.
	CopyNonLegendary bool

	// CopyLoyalty (with CopyLoyaltySet) is the CopySpellAbility SetLoyalty$
	// rider (the corpus's one carrier is Ob Nixilis, the Adversary's
	// Casualty:X script): the override of the copy's CR 306.5b starting
	// loyalty, delivered RESOLVED by the creating effect (the casualty amount
	// the payment computed -- the sacrificed creature's power). It is set by
	// the StackCopy fold from the creating effect's event OR inherited from a
	// spell copy being copied again (CR 707.2), and read by
	// events.EntryCounterGrants when the copy enters the battlefield.
	// Engine-runtime, rebuilt from the event stream on replay like
	// CopyNonLegendary; CopyLoyaltySet false on every ordinary object.
	CopyLoyalty    int32
	CopyLoyaltySet bool

	// CopyFace is the CR 613.1a copy-effect basis for a permanent that became a
	// copy of another (DB$ Clone): while non-nil, Face() returns THIS face
	// instead of the object's own card face, so every read site -- name,
	// abilities, keywords, types, colours, P/T, mana production -- sees the
	// copied characteristics with no per-caller plumbing. It is set and cleared
	// ONLY inside events.Apply (the ClonePermanent fold and Move's
	// leaves-the-battlefield reset), so a live game and a replay derive it
	// identically. The clone's modifier parameters (AddTypes$/SetColor$/
	// AddKeywords$/SetPower$/SetToughness$) are separate layer-4/5/6/7
	// continuous effects registered by the primitive, so this face stays the
	// source's PRINTED face and the layer walk applies the exceptions in CR 613
	// order on top. nil on every object that is not a copy.
	CopyFace *cards.Face
	// CopyGainThisAbility records the clone's GainThisAbility$ True rider: the
	// synthetic CopyFace already carries the ORIGINAL object's abilities (and
	// SVar table) so the ability that produced the copy survives the copy.
	// Engine-runtime, rebuilt from the ClonePermanent event on replay like
	// CopyFace.
	CopyGainThisAbility bool

	// GainedFace is the foreign face a HAS-ALL-ABILITIES-OF ability wrapper
	// (events.GainedAbilityPush / GainedTriggerPush) was minted from -- the
	// per-stack-instance provenance rules/pile.go's gainedOwnedFace recovers.
	// The granting static can END between the push and the resolution (the
	// foreign card leaves the scoped zone, the static's named set re-derives),
	// and the live-grant recovery scans then find no owner, so a resolving
	// wrapper must not depend on them. Set ONLY inside events.Apply from the
	// exact face that provided the compiled SA -- the same write-site
	// discipline CopyFace takes -- so a live game and every replay, including
	// the log-only state reconstruction, derive it identically. nil on every
	// wrapper that is not a gained mint (and on every permanent).
	GainedFace *cards.Face
	// GainedFrom is the foreign OBJECT GainedFace was taken from (the
	// GainedAbilityPush/GainedTriggerPush IDs[0]), set beside GainedFace and
	// under the same Apply-only discipline. It is what lets a gained body that
	// itself GRANTS by name (a gained Animate's Abilities$/Triggers$, Manascape
	// Refractor animating through Spawning Pool's ability) name an object
	// whose face actually carries the SVar table, since the recipient's own
	// face does not. 0 on every object that is not a gained mint.
	GainedFrom ObjID

	// Unlocked marks one face of an Enchantment Room (CR 309): the door the
	// room was CAST as is unlocked from entry; DoorUnlock (the unlock
	// activation) flips this when the OTHER half's door is paid for. A
	// room's locked half's abilities are inactive; after the unlock both
	// halves' rules text is live (rules-side scans consult this field). Only
	// events.Apply writes it, so a replay rebuilds it.
	Unlocked bool
}

// MergedCard is one card stacked beneath a mutated permanent's top card
// (CR 702.140d). Obj names the card's parked object (in ZCeased); Card and
// FaceIdx are the card's identity, carried on the value so the ability scans
// can read it without a second object lookup and so a parked object that is
// somehow gone still leaves the card's abilities live.
type MergedCard struct {
	Obj     ObjID
	Card    *cards.Card
	FaceIdx uint8
}

// ExileReturnEntry is one ChangeZone Duration$ UntilHostLeavesPlay exile:
// the exiled object and the zone it was exiled from (which the return moves
// it back to; a battlefield-origin exile returns to the battlefield, a
// hand-origin one to the hand).
type ExileReturnEntry struct {
	Obj  ObjID
	From Zone
}

// BestowedAttached reports whether o is a card printed with Bestow that is
// currently attached to a permanent (CR 702.114e: while attached to a
// creature the bestowed permanent is an Aura with enchant creature, not a
// creature; unattached it is a creature again). It is derived from live
// state -- AttachedTo and the printed face -- so every replay and every read
// site derives the switch identically and no event field carries a marker.
// An unattached bestowed card, and any object printed without Bestow, is
// never "bestowed attached".
func (o *Object) BestowedAttached() bool {
	return o.AttachedTo != 0 && o.Face() != nil && o.Face().HasKeyword("Bestow")
}

// BestowedAuraSpell reports whether o is a card printed with Bestow that is
// currently a bestowed SPELL on the stack (CR 702.114c: a card cast with its
// bestow ability is an Aura spell with enchant creature, not a creature
// spell, so it is an Aura and not a Creature to every type read -- including
// abilities that trigger when a player casts a creature spell). Derived from
// live state -- the object's zone and the pay-time FlagBestowed provenance --
// the same derive-don't-store discipline BestowedAttached practises, so every
// replay and every read site derives the switch identically.
//
// Once the spell resolves the object is on the battlefield, so this reads
// false and the attached switch (BestowedAttached) takes over; an object
// printed without Bestow, or a bestowed card cast for its plain mana cost
// (no FlagBestowed), is never a bestowed Aura spell.
func (o *Object) BestowedAuraSpell() bool {
	return o.Zone == ZStack && o.CastFlags&FlagBestowed != 0 &&
		o.Face() != nil && o.Face().HasKeyword("Bestow")
}

// ReconfiguredAttached reports whether o is a card printed with Reconfigure
// that is currently attached to a permanent (CR 702.150c: while attached,
// the permanent is not a creature; unattached it is a creature again).
// Derived from live state -- AttachedTo and the printed face -- the same
// discipline BestowedAttached practises, so every replay and every read
// site derives the switch identically and no event field carries a marker.
// An unattached reconfigure card, and any object printed without
// Reconfigure, is never "reconfigured attached".
func (o *Object) ReconfiguredAttached() bool {
	return o.AttachedTo != 0 && o.Face() != nil && o.Face().HasKeyword("Reconfigure")
}

func (o *Object) Face() *cards.Face {
	// CR 613.1a: a copy effect is the FIRST layer, so while one applies the
	// object's characteristics come from the copied face. Routing it here is
	// what makes every Face() reader in the tree see the copy by construction
	// (CR 707.2) rather than each call site having to ask the layer system.
	if o.CopyFace != nil {
		return o.CopyFace
	}
	if o.Card == nil || int(o.FaceIdx) >= len(o.Card.Faces) {
		return nil
	}
	return o.Card.Faces[o.FaceIdx]
}

// faceDownEffective reports whether this object's face is currently hidden by
// CR 708.5: it is FaceDown and on the battlefield. While so, every printed
// characteristic is replaced by the face-down set.
func (o *Object) faceDownEffective() bool {
	return o.FaceDown && o.Zone == ZBattlefield
}

// FaceDownTypeWords is the effective base type set of a face-down battlefield
// permanent (CR 708.5): {Creature} when no FaceDownSetType$ was folded, else
// the set type split on Forge's " & " join. A non-face-down object returns
// its printed types. It returns a fresh slice so a caller can append to it
// (the layer-4 walk does) without aliasing stored state.
func (o *Object) FaceDownTypeWords() []string {
	if !o.faceDownEffective() {
		if f := o.Face(); f != nil {
			return append([]string(nil), f.Types...)
		}
		return nil
	}
	set := strings.TrimSpace(o.FaceDownSetType)
	if set == "" {
		return []string{"Creature"}
	}
	parts := strings.Split(set, " & ")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"Creature"}
	}
	return out
}

// EffectiveIsCreature reports whether this object is a creature right now,
// honouring CR 708.5: while a battlefield object is face down its PRINTED face
// does not exist, so creature-ness comes from the folded FaceDownSetType$
// (default Creature). Every printed-face "is this a creature" read that gates
// a creature rule (combat, the creature SBAs, convoke, protection) must go
// through here, or a manifested non-creature or a face-down set type that
// drops Creature reads the wrong answer.
func (o *Object) EffectiveIsCreature() bool {
	if o.faceDownEffective() {
		for _, w := range o.FaceDownTypeWords() {
			if w == "Creature" {
				return true
			}
		}
		return false
	}
	f := o.Face()
	return f != nil && f.IsCreature()
}

// EffectiveIsArtifact reports whether this object is an artifact right now,
// honouring CR 708.5 like EffectiveIsCreature: while a battlefield object is
// face down its PRINTED face does not exist, so a manifested or cloaked
// artifact reads its folded face-down type set (which never names Artifact
// today, but the fold, not the corpus, decides). The Improvise announcement
// and its offer-gate credit (rules/cast.go) are the readers; other artifact
// reads (e.g. the Affinity keyword's Count$Valid spec path) go through the
// ordinary filter grammar and do not call this.
func (o *Object) EffectiveIsArtifact() bool {
	if o.faceDownEffective() {
		for _, w := range o.FaceDownTypeWords() {
			if w == "Artifact" {
				return true
			}
		}
		return false
	}
	f := o.Face()
	return f != nil && f.IsArtifact()
}

// MergedFaceAt returns the face of the i-th card stacked beneath the top card
// (0 is the first under-card), or nil when i is out of range. It is the one
// accessor the ability scans use so a merged card's face is resolved the same
// way everywhere.
func (o *Object) MergedFaceAt(i int) *cards.Face {
	if i < 0 || i >= len(o.MergedCards) {
		return nil
	}
	mc := &o.MergedCards[i]
	if mc.Card == nil || int(mc.FaceIdx) >= len(mc.Card.Faces) {
		return nil
	}
	return mc.Card.Faces[mc.FaceIdx]
}

// Ephemeral reports whether this object has, right now, ceased to exist: a
// copy of a spell or ability once it has LEFT THE STACK (CR 707.10h -- a copy
// of a spell that has left the stack is a transient reference, not a real
// object), a token once it has left the battlefield (CR 111.7 -- so IsToken
// alone is not enough, a battlefield token is a perfectly real permanent),
// or an ability object (no card, Card == nil -- always ephemeral, since it
// never legitimately exists off the stack at all).
//
// The IsCopy half is therefore ZONE-AWARE, exactly like effects/filter.go's
// own guard: a battlefield object carrying IsCopy is a real permanent --
// Myriad (CR 702.109) and populate ("create a token copy", CR 706.2) mint
// their tokens as IsToken+IsCopy and legitimately keep them on the
// battlefield, and a copy of a permanent SPELL that has resolved onto the
// battlefield (CR 707.10g, token1's stack-entry clear) is likewise real.
// Only a copy that is neither on the stack (still a spell) nor on the
// battlefield (still a permanent) has ceased to exist.
//
// This build parks such objects in exile rather than deleting them, and
// callers (view.cardViews and any future zone-listing code) consult this
// single definition instead of re-deriving it, so the "copy off the stack
// and battlefield, token off the battlefield, or cardless" rule cannot drift
// between call sites.
func (o *Object) Ephemeral() bool {
	return (o.IsCopy && o.Zone != ZStack && o.Zone != ZBattlefield) ||
		(o.IsToken && o.Zone != ZBattlefield) || o.Card == nil
}

func (o *Object) Counter(kind string) int32 {
	// "ALL" is Forge's CounterType.ALL marker, meaning every counter kind
	// on the object summed -- not a real counter kind (no corpus script
	// names one "ALL"; the removal spellings use AllCounters$ True). The
	// CardCounters.ALL count family (Backstreet Bruiser, Maester Seymour,
	// Lux Artillery's "counters among ...") reads through this one home,
	// so the three CardCounters.<KIND> call sites cannot disagree.
	//
	// The engine's OWN status markers are excluded from the sum: "Shield"
	// (the this-turn regeneration shield) and "Deathtouched" (the CR 702.2b
	// lethal mark) ride an ordinary CounterChange for want of a status field
	// and are cleared only at end-of-turn cleanup, so mid-turn -- after
	// combat, exactly when an attack-trigger X is read -- every marked
	// creature would otherwise inflate an ALL sum by 1-2 per mark. The
	// same exclusion the AddCounter doubler gate applies (rules/replacement.go
	// reads state.InternalCounterMarker) now governs the ALL read too.
	// Callers that want a specific marker keep asking for it by name
	// (effects/regeneration.go, rules/combat.go, rules/sba.go) -- the
	// exclusion lives only in the ALL branch.
	if kind == "ALL" {
		var n int32
		for _, c := range o.Counters {
			if InternalCounterMarker(c.Kind) {
				continue
			}
			n += c.N
		}
		return n
	}
	for _, c := range o.Counters {
		if c.Kind == kind {
			return c.N
		}
	}
	return 0
}

// InternalCounterMarker reports whether a counter name is one of the engine's
// own status markers rather than a counter a card could name. Both ride an
// ordinary CounterChange -- the engine has no per-object status field, so a
// marker is recorded as a counter -- and both are SET with Amount 1:
//
//   - "Shield", the this-turn regeneration shield (effects/counters.go's
//     effRegenerate sets it, effects/regeneration.go reads it back, and
//     rules/combat.go consumes one per destruction);
//   - "Deathtouched", the CR 702.2b lethal mark (rules/combat.go's combat
//     assignment, rules/replacement.go's replacement-applied damage,
//     effects/damage.go), read by rules/sba.go's destruction check.
//
// The helper lives in state because the two consumers sit on either side of
// the dependency line: Object.Counter's "ALL" sum (this file, read by the
// CardCounters.ALL count family) and rules/replacement.go's AddCounter
// doubler gate (rules imports state, never the reverse). Excluding the
// markers by name is safe: every counter kind the corpus scripts is
// upper-case (P1P1, LORE, AGE, TIME, STUN, CHARGE, ENERGY, POISON, LOYALTY,
// ...), so no real kind can collide with either mixed-case marker name.
func InternalCounterMarker(name string) bool {
	return name == "Shield" || name == "Deathtouched"
}

// AddCounter adds n counters of a kind, creating the entry if needed. Counters
// never go negative: removing more than present clamps at zero.
func (o *Object) AddCounter(kind string, n int32) {
	for i := range o.Counters {
		if o.Counters[i].Kind == kind {
			o.Counters[i].N += n
			if o.Counters[i].N < 0 {
				o.Counters[i].N = 0
			}
			return
		}
	}
	if n > 0 {
		o.Counters = append(o.Counters, Counter{kind, n})
	}
}

// CloneDeep returns a value copy of o whose slice fields (Counters, Targets,
// Remembered, BlockedBy, Chosen, Goads, ChosenModes, SeekFound, EncodedCards)
// are independently backed, so mutating
// the copy's slices can never alias o's -- everything else (Card, a shared
// pointer into the immutable compiled corpus, plus every scalar field) is
// correct as a plain value copy. This is the one definition of "deep-copy an
// Object": Game.Clone (the whole live arena), rules.Engine's own last-known-
// information capture (emit, before events.Emit mutates the live object)
// and Engine.Clone's copy of a pending trigger's Ctx.LKI all call this
// rather than each repeating the same four-line copy, so a future slice or
// map field added to Object needs updating in exactly one place to stay
// deep.
func (o *Object) CloneDeep() Object {
	c := *o
	c.Counters = append([]Counter(nil), o.Counters...)
	c.Targets = append([]Target(nil), o.Targets...)
	c.Remembered = append([]Target(nil), o.Remembered...)
	c.BlockedBy = append([]ObjID(nil), o.BlockedBy...)
	c.Chosen = append([]Target(nil), o.Chosen...)
	c.Goads = append([]GoadEffect(nil), o.Goads...)
	c.ChosenModes = CloneChosenModes(o.ChosenModes)
	c.IntrinsicKeywords = append([]string(nil), o.IntrinsicKeywords...)
	c.Imprinted = append([]ObjID(nil), o.Imprinted...)
	c.DamageTakenByGame = append([]ObjID(nil), o.DamageTakenByGame...)
	c.ImprintTokens = append([]ObjID(nil), o.ImprintTokens...)
	c.EncodedCards = append([]ObjID(nil), o.EncodedCards...)
	c.SeekFound = append([]ObjID(nil), o.SeekFound...)
	c.Notes = append([]string(nil), o.Notes...)
	c.ExiledCards = append([]ObjID(nil), o.ExiledCards...)
	c.ExileReturn = append([]ExileReturnEntry(nil), o.ExileReturn...)
	c.MergedCards = append([]MergedCard(nil), o.MergedCards...)
	c.RuntimeSVars = cloneRuntimeSVars(o.RuntimeSVars)
	return c
}

// cloneRuntimeSVars deep-copies a runtime SVar table so a cloned game never
// aliases the original's map (a mutation of one would silently move the
// other's store, the same aliasing hazard CloneDeep's slice copies guard
// against). nil in, nil out.
func cloneRuntimeSVars(m map[string]int32) map[string]int32 {
	if m == nil {
		return nil
	}
	out := make(map[string]int32, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// SacrificedInfoOf captures the last-known-information snapshot of the object
// id names at the instant it is about to be sacrificed -- power, toughness and
// mana value -- so a later Sacrificed$<Property> SVar can answer "the
// sacrificed creature's power" without reading a now-graveyard object. It
// must be called BEFORE the sacrifice's zone change: the object is still a
// battlefield permanent then, so its face and any +1/+1 counters (the
// layer-7d portion of its P/T, preserved because they have not yet been reset
// by Move) are live. A missing object or faceless object (an ability or stack
// wrapper) degrades to the zero snapshot rather than panicking.
//
// The power/toughness convention deliberately matches the engine's existing
// effects-side reads (effects/count.go's Count$CardPower/Count$CardToughness:
// face value plus P1P1, not the full layer-system Derived) so the new
// Sacrificed$ heads agree with their nearest existing analogue.
func SacrificedInfoOf(g *Game, id ObjID) SacrificedInfo {
	o := g.Obj(id)
	if o == nil || o.Face() == nil {
		return SacrificedInfo{Obj: id}
	}
	p := int32(o.Face().Power()) + o.Counter("P1P1")
	t := int32(o.Face().Toughness()) + o.Counter("P1P1")
	var counters []Counter
	for i := range o.Counters {
		if o.Counters[i].N > 0 {
			counters = append(counters, o.Counters[i])
		}
	}
	return SacrificedInfo{Obj: id, Power: p, Toughness: t, ManaValue: o.Face().Cmc(), Counters: counters}
}

// CloneChosenModes copies a ChosenModes announcement, preserving the
// nil (no announcement) versus non-nil empty (zero modes announced)
// distinction resolution relies on.
func CloneChosenModes(m []string) []string {
	if m == nil {
		return nil
	}
	return append(make([]string, 0, len(m)), m...)
}
