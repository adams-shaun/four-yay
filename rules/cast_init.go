package rules

import (
	"github.com/adams-shaun/gorge/effects"
)

func init() {
	effects.RegisterNonAPI("kw:Kicker", "kw:Surge", "kw:Flashback", "kw:Aftermath", "kw:Delve",
		// The alternative-cost keyword family (altcosts): each is implemented
		// to its CR shape with a named proof test in altcast_test.go --
		// kw:Evoke (alternative cast + ETB unconditional sacrifice), kw:Dash
		// (alternative cast + haste + delayed return), kw:Overload
		// (alternative cast + target-reads-each), kw:Warp (alternative cast
		// from hand/graveyard/exile + delayed exile), kw:Madness (exile on
		// discard + immediate cast offer), kw:Encore (graveyard activation
		// minting attacking token copies), and kw:AlternateAdditionalCost
		// (the mandatory either-or additional cost choice).
		"kw:Evoke", "kw:Dash", "kw:Overload", "kw:Warp", "kw:Madness",
		"kw:Encore", "kw:AlternateAdditionalCost",
		// kw:Mayhem (the Doom Prevails keyword): the graveyard recast and
		// the bare parameterless land-play form are read directly off the
		// K: line (rules/legal.go's offers, rules/cast.go's charge,
		// rules/cast_provenance.go's Spell.Mayhem condition), so the census
		// is told here exactly as the family above is. Proof tests:
		// mayhem_test.go, mayhem_castsa_test.go, mayhem_land_play_test.go,
		// mayhem_granted_test.go.
		"kw:Mayhem",
		// kw:Unearth (CR 702.84): the graveyard return is an ordinary
		// activated ability cards/kw_unearth.go expands from the K: line,
		// and its three riders are rules-layer (rules/unearth.go) -- the
		// haste grant, the end-step exile promise, and the exile-instead
		// replacement. Registered non-API because the keyword's whole
		// implementation lives in rules plus the one builtin SVar body.
		"kw:Unearth",
		// kw:Escalate: the modal additional cost "pay this for each mode chosen
		// beyond the first" -- read directly off the K: line by beginCast's
		// capture and the cast_modes answer handler's fold, and bounded by
		// castModeAsk's affordable-escalation clamp (no keyword expansion; the
		// plain Charm cast is the only way in). The mode ask's Max is clamped
		// to 1 + the affordable escalations so an unpayable mode count is
		// never offered. All 9 corpus carriers parse (7 plain mana,
		// tapXType<1/Creature> and Discard<1/Card>).
		"kw:Escalate",
		// kw:Entwine: CR 702.42, the OPTIONAL additional cost "pay this as you
		// cast a modal spell; if you do, follow the instructions of all its
		// modes". The offer lives in legal.go's hand/command-zone cast walk
		// (mode "entwined", composing the printed cost plus the entwine cost
		// through offerCastable), the charge in beginCast's "entwined" case,
		// and the all-modes announcement in castModeAsk (Min and Max forced to
		// the filtered legal count). The corpus forms -- plain mana (30
		// carriers) and a Sac<2/Land>/Sac<3/Land> sacrifice (Betrayal of
		// Flesh, Solar Tide) -- all price through ParseCost; an unpriceable
		// form fails closed in entwineCost and never offers. Proof:
		// rules/entwine_test.go.
		"kw:Entwine",
		// kw:Strive: CR 702.52, the mandatory additional cost "this spell
		// costs <cost> more for each target beyond the first" -- read directly
		// off the K: line by beginCast's capture (no keyword expansion; the
		// plain cast is the only way in) and priced by repriceForTargets once
		// the CR 601.2c target answer is in, folded into pc.cost so the mana
		// window and the payment charge the composed total. The per-extra
		// count is delta-tracked on pendingCast.striveUnits, so the fold is
		// idempotent across repriceForTargets' re-entries. Proof:
		// rules/strive_test.go.
		"kw:Strive",
		// kw:Escape: CR 702.135, the graveyard cast with its exile cost, read
		// off the K: line by derivedKeywordParam and gated in legal.go's
		// cast walk -- proved by TestUnderworldBreachGrantsEscapeAndTheEscape
		// CastResolves and TestKroxaEscapeCastDoesNotSacrificeOnETB. It was
		// implemented without being registered, so the coverage ratchet read
		// it as a gap.
		"kw:Escape",
		// kw:Retrace: CR 702.81a, the graveyard cast paying the printed mana
		// cost plus an additional discard-a-land cost. The offer lives in
		// legal.go's graveyard walk and the cost fold in beginCast's "retrace"
		// mode, both through retraceExtra so offer and charge cannot disagree.
		"kw:Retrace",
		// kw:Jump-start: CR 702.84a, the graveyard cast paying the printed
		// mana cost plus an additional discard-a-card cost and exiling the
		// card on resolution. The offer lives in legal.go's graveyard walk, the
		// cost fold in beginCast's "jumpstart" mode (both through
		// jumpstartExtra), and the exile destination in modeFlags' FlagJumpstart
		// read by spellRestZone/spellFizzleZone -- proved by
		// TestRadicalIdeaJumpstartCastsDiscardsAndExiles.
		"kw:Jump-start",
		"kw:Buyback", "kw:Transmute", "kw:Suspend", "kw:Convoke", "kw:Harmonize", "kw:Cycling",
		// kw:Cascade: CR 702.85, the cast trigger read directly off the K:
		// line (no keyword expansion — the printed K:Cascade and every
		// layer-6 AddKeyword$ Cascade grant reach hasCastCascade through the
		// one derived-keyword read, so the printed and granted routes cannot
		// disagree). The trigger is a real KeywordTriggerPush stack object;
		// its resolution is effects' Cascade primitive. Proof:
		// TestBloodbraidElfCascadeExilesUntilLesserAndOffersFreeCast,
		// TestCascadeDeclinedFoundCardGoesToBottom,
		// TestDarkApostleGrantedCascadeRegistersAndOffers in
		// rules/cascade_test.go.
		"kw:Cascade",
		// kw:Improvise: CR 702.66, the generic-only payment keyword -- its
		// announcement over the caster's untapped artifacts (the improvise
		// arm inside convokeAsk) and its greedy offer-gate credit
		// (improviseCost) are the proof, in cast.go.
		"kw:Improvise",
		// kw:TypeCycling: CR 702.28d (typed cycling), expanded by
		// cards/keywords.go into the Transmute-shaped library search
		// (AB$ ChangeZone | Origin$ Library | Destination$ Hand |
		// ChangeType$ <type>), whose reveal comes from the search's own
		// stated-quality default. Proof: TestTypeCyclingSearchesTheNamedType
		// and TestTypeCyclingBasicLandSearchesAnyBasic in
		// rules/alternative_costs_test.go.
		"kw:TypeCycling",
		// kw:Ninjutsu: CR 702.49, expanded by cards/kw_ninjutsu.go into an
		// ordinary hand-zone activated ability whose Cost$ carries the printed
		// ninjutsu mana cost plus Return<1/Creature.YouCtrl+attacking+unblocked>
		// (the CR 702.49a unblocked-attacker half, via the effects filter's
		// "unblocked" predicate) and whose body puts the card onto the
		// battlefield tapped and attacking via ChangeZone's Attacking$ True
		// rider, bound to the defender captured when the Return cost was paid
		// (pendingCast.ninjutsuDefender -> AbilityPush IDs -> Ctx.DefendingPlayer,
		// CR 702.49b). Proof: rules/ninjutsu_test.go.
		"kw:Ninjutsu",
		// kw:Level up: CR 702.87, expanded by cards/keywords.go into an
		// ordinary sorcery-speed PutCounter activation (CounterType$ LEVEL);
		// the level-band statics read the counter through the existing
		// counters_<CMP><n>_LEVEL predicate, so no separate path of its own.
		"kw:Level up",
		// kw:Outlast: CR 702.107, expanded by cards/kw_outlast.go into an
		// ordinary sorcery-speed PutCounter activation (CounterType$ P1P1,
		// Cost$ T <mana>); the leading T is the CR 702.107a tap cost and
		// makes the keyword repeatable only across untaps, with no
		// once-per-turn machinery of its own. Proof:
		// rules/outlast_test.go.
		"kw:Outlast",
		// kw:Class: CR 702.118, expanded by cards/keywords.go into one
		// sorcery-speed designation activator per level (gated below N), plus
		// the level's granted static/trigger/replacement, appended with its own
		// ClassBand$ band (rules/class_level.go). Level 1 is intrinsic; no
		// entry counter or replacement is created. Proof: rules/class_test.go.
		"kw:Class",
		// kw:Replicate: CR 702.55, expanded by cards/keywords.go into the
		// Storm-shaped copy trigger whose Amount$ Count$ReplicatePaid reads
		// the pay-time CastInfo's count; the cast flow's replicateAsk poses
		// the CR 601.2b count announcement.
		"kw:Replicate",
		// kw:Multikicker: CR 702.43, the count-ask cast shape -- the cast flow
		// (rules/legal.go's multikicked offer, rules/cast.go's multikickAsk)
		// poses the CR 601.2b count announcement and the trailing
		// FlagMultikicked CastInfo carries the count into Object.TimesKicked
		// for the Count$TimesKicked head (effects/count.go). No keyword
		// expansion: the K:Multikicker line is read directly.
		"kw:Multikicker",
		// kw:Conspire: CR 702.78, expanded by cards/keywords.go into the
		// Storm-shaped copy trigger whose Amount$ Count$Conspired reads the
		// pay-time CastInfo's flag; the cast flow's "conspired" offer
		// (rules/legal.go) plus conspireAsk (rules/cast.go) pose and pay the
		// two-creature tap.
		"kw:Conspire",
		// kw:Demonstrate: CR 702.152, expanded by cards/kw_demonstrate.go
		// into the SpellCast trigger on the card's own cast whose DB$
		// Demonstrate body (effects/demonstrate.go) poses the may-copy
		// election and the opponent choice; a layer-6 AddKeyword$ Demonstrate
		// grant (Silverquill Lecturer) reaches the same body through
		// rules/trigger_granted.go's checkGrantedDemonstrateTriggers. The
		// copies are ordinary StackCopy mints, so a creature-spell copy
		// becomes a token through the standing CR 707.10g fold.
		"kw:Demonstrate",
		// kw:Squad: CR 702.66, expanded by cards/keywords.go into a
		// ChangesZone self-entry trigger whose DB$ CopyPermanent body reads
		// Count$SquadPaid (the Replicate pattern); the cast flow's "squadded"
		// offer (rules/legal.go) plus squadAsk (rules/cast.go) pose and pay
		// the CR 601.2b count announcement, and the trailing FlagSquadPaid
		// CastInfo carries the count into Object.SquadPaid.
		"kw:Squad",
		// kw:Affinity: CR 702.41, expanded by cards/keywords.go into the
		// ordinary ReduceCost cost-static machinery (rules/statics.go's
		// collectCostStatics) -- no separate cast path of its own.
		"kw:Affinity",
		// kw:Undaunted: CR 702.105, expanded by cards/kw_undaunted.go into
		// the ordinary ReduceCost cost-static machinery.
		"kw:Undaunted",
		// kw:Embalm / kw:Eternalize: CR 702.128 / 702.129, expanded by
		// cards/keywords.go into one graveyard-zone CopyPermanent activation
		// whose cost exiles the card itself (ExileFromGrave<1/CARDNAME>) and
		// whose token copy carries the keyword's modified characteristics --
		// the Encore graveyard-activation shape with a different effect.
		// kw:Gravestorm: CR 702.84, expanded by cards/keywords.go into the
		// Storm-shaped copy trigger whose Amount$
		// Count$ThisTurnEntered_Graveyard_from_Battlefield_Permanent reads the
		// zone-aware count in effects.countEntered.
		"kw:Gravestorm",
		"kw:Embalm", "kw:Eternalize",
		// kw:Plot: CR 701.34, the hand-origin alternative ACTION -- pay the
		// K:Plot colon parameter, exile the card face up with the plotted
		// designation stamped with the current turn (NO counters: the free
		// cast's only restriction is CR 701.34b's "on a later turn"), and
		// offer a free cast at sorcery timing from a later turn onward (the
		// exile-zone walk; no upkeep ask, unlike Suspend's cast-if-able). No
		// keyword expansion: the K:Plot line is read directly. Proof:
		// rules/plot_test.go.
		// kw:Bargain: CR 702.166, the optional additional cost "you may
		// sacrifice an artifact, enchantment, or token as you cast this
		// spell". The offer lives in legal.go's hand cast walk (the
		// "bargained" mode, priced through the same Spell.Bargain cost
		// statics), the election in bargainAsk (rules/cast.go), and the
		// pay-time FlagBargained provenance in modeFlags -- read by the
		// Count$Bargained/Count$Bargain heads, the bare Condition$ Bargain
		// gate, the `bargained` predicate and the Spell.Bargain constraint.
		// Proof: rules/bargain_test.go.
		"kw:Bargain",
		"kw:Plot")
}
