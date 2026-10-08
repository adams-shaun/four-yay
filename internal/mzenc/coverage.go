package mzenc

import (
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// Canonical feature-family ids. These are the families the mzenc design
// (docs/superpowers/specs/2026-10-07-mzenc-design.md §5) names. Both the
// specFamilies set, the unsupportedFeatures register and EVERY
// w.unsupported[...] walk-site key and w.emit(...) site use these constants,
// so a walk site and its register entry cannot drift in spelling.
const (
	// globals
	famTurnStep      = "TurnStep"
	famDecisionType  = "DecisionType"
	famDecisionsText = "DecisionsText"

	// stack
	famStack              = "Stack"
	famStackTargets       = "StackTargets"
	famStackAbilityDetail = "StackAbilityDetail"

	// exile (per zone)
	famExile = "Exile"
	// famExileZoneNames is a caveat, not an upstream §5 family: the gorGE view
	// exposes one flat exile list with no per-zone name, so processExile cannot
	// reproduce StateEncoder's per-zone nesting. It is a walk site's recorded
	// gap and stays on the register so the ratchet holds the hole honestly.
	famExileZoneNames = "ExileZoneNames"

	// player / opponent
	famIsActivePlayer   = "IsActivePlayer"
	famIsDecisionPlayer = "IsDecisionPlayer"
	famLifeTotal        = "LifeTotal"
	famLibraryCount     = "LibraryCount"
	famManaPool         = "ManaPool"
	famPlayerCounters   = "PlayerCounters"
	famDayNight         = "DayNight"
	famCanPlayLand      = "CanPlayLand"
	famInPayManaMode    = "InPayManaMode"
	famActivating       = "Activating"
	famMicroDecisions   = "MicroDecisions"
	famAttachments      = "Attachments"
	famBattlefield      = "Battlefield"
	famGraveyard        = "Graveyard"
	famHand             = "Hand"
	famCommandZone      = "CommandZone"
	// famEmblem is a caveat family, not an upstream §5 bullet: StateEncoder's
	// processCommandZone also walks command-zone Emblems (StateEncoder.java:
	// 461-471, 486-495), but view.PlayerView exposes no emblem list, so the
	// walk site records the gap on the register and emits nothing.
	famEmblem         = "Emblem"
	famGlobalWatchers = "GlobalWatchers"

	// permanent / card
	famCard             = "Card"
	famPermanent        = "Permanent"
	famCreature         = "Creature"
	famColors           = "Colors"
	famSubtypes         = "Subtypes"
	famDynamicTypes     = "DynamicTypes"
	famDynamicAbilities = "DynamicAbilities"
	famCanAttack        = "CanAttack"
	famCanBlock         = "CanBlock"
	famPermanentFlags   = "PermanentFlags"
	famImprinted        = "Imprinted"
	famPaired           = "Paired"
	famTargetedBy       = "TargetedBy"
	famPermanentExile   = "PermanentExile"
	famCardAbilities    = "CardAbilities"
)

// specFamilies is the complete set of MageZero feature families transcribed
// from the mzenc design §5. Every family is either emitted by the walker on a
// state that exercises it, or named in unsupportedFeatures; the coverage
// ratchet (TestExtractorCoverageRatchetMatches) holds both directions.
var specFamilies = map[string]bool{
	famTurnStep:           true,
	famDecisionType:       true,
	famDecisionsText:      true,
	famStack:              true,
	famStackTargets:       true,
	famStackAbilityDetail: true,
	famExile:              true,
	famExileZoneNames:     true,
	famIsActivePlayer:     true,
	famIsDecisionPlayer:   true,
	famLifeTotal:          true,
	famLibraryCount:       true,
	famManaPool:           true,
	famPlayerCounters:     true,
	famDayNight:           true,
	famCanPlayLand:        true,
	famInPayManaMode:      true,
	famActivating:         true,
	famMicroDecisions:     true,
	famAttachments:        true,
	famBattlefield:        true,
	famGraveyard:          true,
	famHand:               true,
	famCommandZone:        true,
	famEmblem:             true,
	famGlobalWatchers:     true,
	famCard:               true,
	famPermanent:          true,
	famCreature:           true,
	famColors:             true,
	famSubtypes:           true,
	famDynamicTypes:       true,
	famDynamicAbilities:   true,
	famCanAttack:          true,
	famCanBlock:           true,
	famPermanentFlags:     true,
	famImprinted:          true,
	famPaired:             true,
	famTargetedBy:         true,
	famPermanentExile:     true,
	famCardAbilities:      true,
}

// unsupportedFeatures is the register of §5 families the walker cannot yet
// express through gorGE's view projection. Each key MUST be written by the
// matching w.unsupported[...] walk site (the same constant), and the ratchet
// fails if a registered family is ever emitted or a walk site writes a family
// that is not registered.
var unsupportedFeatures = map[string]string{
	famStackAbilityDetail: "view.StackView carries no Kicks / CostTag / selected-modes / XValue detail",
	famExileZoneNames:     "view.PlayerView.Exile is one flat list with no per-zone name, so upstream's per-zone exile nesting is not reproducible",
	famDayNight:           "the view carries no day/night state",
	famInPayManaMode:      "the view carries no in-pay-mana-mode flag",
	famActivating:         "the view carries no activating flag",
	famMicroDecisions:     "the view carries no ChosenTargets / ChosenChoices / UseChoices / AmountChoices sequences",
	famEmblem:             "view.PlayerView carries no command-zone emblem list, so upstream's Emblem walk is not reproducible",
	famGlobalWatchers:     "the view carries no global watcher counters (SpellsCastThisTurn, LifeGained/Lost, TokensCreated)",
	famDynamicTypes:       "view.CardView carries no dynamic type / colour / subtype projection",
	famDynamicAbilities:   "view.CardView carries no dynamic permanent-ability list",
	famCardAbilities:      "the view carries no static / activated / triggered ability list",
}

// ProcessStateReport runs the SAME walk as ProcessState but additionally
// records which feature families the walker actually emitted and which it
// could not express. It is the coverage-ratchet entry point and stays off the
// hot path (ProcessState allocates neither map). The returned maps are copies;
// ranging them here is bookkeeping only and never reaches the id/hash path.
func ProcessStateReport(v view.View, ch view.Chars, seat state.PlayerID, decisionType int, decisionsText string) (emitted map[string]bool, unsupported map[string]bool) {
	seen := map[string]bool{}
	uns := map[string]bool{}
	processState(v, ch, seat, decisionType, decisionsText, seen, uns)

	emitted = make(map[string]bool, len(seen))
	for k, on := range seen {
		emitted[k] = on
	}
	out := make(map[string]bool, len(uns))
	for k, on := range uns {
		out[k] = on
	}
	return emitted, out
}
