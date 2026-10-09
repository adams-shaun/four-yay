// Crew/Saddle-perspective trigger classification (ticket
// agent-20261009T174759Z-80a5bc9a): Mode$ Crewed and Mode$ Saddled fire on
// the marker events rules/trigmatch's crewed.go matches, with Forge's
// ValidCrew$ naming the CREWING/SADDLING creature. The recipes' cause is the
// reverse of the attack-activation cause: a probe Vehicle or Mount on p0's
// battlefield activates its own Crew/Saddle ability, electing the row's
// source as the tapped body, so the marker event's Obj is the source.
// Balthier and Fran's "Whenever a Vehicle crewed by CARDNAME this turn
// attacks" is the same family read the other way round: the cause crews a
// probe Vehicle with the source, then attacks with the crewed Vehicle.
package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// The three crew/saddle-perspective sub-families. Exported so the
// compliance/oraclegen/templates recipe and the triggerFires dispatch share
// one spelling.
const (
	CrewedSub               = "trigger.crewed"
	SaddledSub              = "trigger.saddled"
	AttacksCrewedVehicleSub = "trigger.attacks-crewed-vehicle"
)

// classifyCrewedTrigger admits the crew/saddle-perspective shapes. Every
// other trigger keeps its previous routing: the mode cases are disjoint from
// the ones classifyEventTrigger's own switch and classifyTapCombatTrigger
// handle, so running this before them moves a requirement only between
// sub-families, never in or out of the requirement set.
func classifyCrewedTrigger(t *cards.Trigger) (sub string, ok bool) {
	if t.HasParam(cards.PKCondition) || t.HasParam(cards.PKCheckSVar) || t.HasParam(cards.PKIsPresent) || t.HasParam(cards.PKTeamwork) {
		return "", false
	}
	switch t.ModeKind() {
	case cards.TriggerCrewed, cards.TriggerSaddled:
		// The cause crews/saddles with the trigger's source, so ValidCrew$
		// must admit Card.Self (Tiana's comma list does, among its
		// alternatives); every other spelling stays a gap.
		if !validCrewAdmitsSelf(t.ParamStr(cards.PKValidCrew)) {
			return "", false
		}
		// The cause runs in turn 1's main phase on p0's turn, so a phase
		// list outside Main1/Main2 and an opponent-turn gate are not
		// satisfiable by it.
		if phase := t.ParamStr(cards.PKPhase); phase != "" && !phaseListHasMain(phase) {
			return "", false
		}
		if strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKOpponentTurn)), "True") {
			return "", false
		}
		if zones := t.ParamStr(cards.PKTriggerZones); zones != "" && !strings.EqualFold(zones, "Battlefield") {
			return "", false
		}
		if t.ModeKind() == cards.TriggerCrewed {
			return CrewedSub, true
		}
		return SaddledSub, true
	case cards.TriggerAttacks:
		// "Whenever a Vehicle crewed by CARDNAME this turn attacks" (Balthier
		// and Fran): the cause crews a probe Vehicle with the source, then
		// attacks with it, so the filter's base must be Vehicle and the
		// crew-by marker the exact ThisTurn spelling the engine's predicate
		// reads. Every other CrewedBySource spelling stays a gap.
		card := t.ParamStr(cards.PKValidCard)
		if !filterHasToken(card, "Vehicle") || !filterHasToken(card, "CrewedBySourceThisTurn") {
			return "", false
		}
		if fc := strings.TrimSpace(t.ParamStr(cards.PKFirstCombat)); fc != "" && !strings.EqualFold(fc, "True") {
			return "", false
		}
		return AttacksCrewedVehicleSub, true
	}
	return "", false
}

// validCrewAdmitsSelf reports whether a ValidCrew$ filter admits Card.Self:
// the whole filter, or one of its comma-separated alternatives.
func validCrewAdmitsSelf(filter string) bool {
	for _, alt := range strings.Split(filter, ",") {
		if strings.EqualFold(strings.TrimSpace(alt), "Card.Self") {
			return true
		}
	}
	return false
}

// phaseListHasMain reports whether a Phase$ list admits a main phase: the
// cause runs in turn 1's first main phase, so Main1 (and Main2, the same
// turn) both satisfy any Phase$ list the recipes can play.
func phaseListHasMain(phase string) bool {
	for _, p := range strings.Split(phase, ",") {
		p = strings.TrimSpace(p)
		if strings.EqualFold(p, "Main1") || strings.EqualFold(p, "Main2") {
			return true
		}
	}
	return false
}
