// Level-B trigger recipes for the opponent-side event causes: an opponent's
// completed library search, an opponent's discard, an opponent gaining
// control of a p0 permanent, and a Craft ability exiling the watcher itself.
// Each cause is built from ops the runner already has (cast, activate) and
// the fire probe (gorge's own trigger matcher) still decides whether the row
// fires.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// librarySearchProbes are the tutors p1 casts for a searched-library trigger:
// a completed search emits the engine's SearchedLibrary marker naming p1,
// whatever the search picked (the runner's fallback takes the first offered
// card).
var librarySearchProbes = []string{"Demonic Tutor", "Diabolic Tutor"}

// discardVictimProbes are the cards p1 is left holding, one cause each, so a
// Land/Creature/any/nonland-noncreature OppOwn discard filter always has a
// victim its filter accepts (Mind Rot discards the whole remaining hand).
var discardVictimProbes = []string{"Forest", "Grizzly Bears", "Shock"}

// craftProbe is the Craft card whose ability exiles the watcher as its
// material: its {1}{U} Craft takes one other artifact, and the watcher (an
// artifact) is the only legal material on the board.
const craftProbe = "Braided Net"

// threatenVictim is the creature p1's Threaten takes for a
// changes-controller cause: a name the watcher's own ETB (which relocates
// the opposing baseline bear at setup) cannot shadow, so the target ask
// cannot match a permanent whose original controller is not the trigger's
// You.
const threatenVictim = "Llanowar Elves"

// opponentCauseRecipe builds the causes for the opponent-side event
// sub-families. ok is false for every other sub-family, which baseTriggerRecipe
// then treats as it always has.
func opponentCauseRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, sub string) (causes []triggerCause, why string, ok bool) {
	add := func(c triggerCause, yes bool) {
		if yes {
			causes = append(causes, c)
		}
	}
	switch sub {
	case "trigger.searched-library":
		// p1 casts a tutor during p1's first main phase (a sorcery): the
		// search's pick is answered by the runner's fallback (the first
		// offered card), and the completed search's marker names p1. The ask
		// is mid-resolution, so the trigger is on the stack only once that
		// pick is answered: the probe ends at the next priority decision,
		// exactly as the scry causes do.
		for _, probe := range librarySearchProbes {
			card, exists := reg.Lookup(probe)
			if !exists || len(card.Faces) == 0 || !hasType(card.Faces[0], "Sorcery") {
				continue
			}
			st, yes := castProbe(reg, probe)
			if !yes {
				continue
			}
			st.Seat, st.Card = 1, "p1:"+probe
			// p1 holds priority after its cast: its pass, then p0's, resolve
			// the tutor into the search ask, and the trailing pass_to stops at
			// the priority decision the queued trigger waits behind (the
			// runner's resolve drains the whole stack, so the checkpoint must
			// be a step of the cause itself).
			add(triggerCause{
				opponentHand: []string{probe},
				steps: []oraclegen.Step{
					{Op: "pass_to", Step: "main1", Active: "p1"},
					st,
					{Op: "pass", Seat: 1},
					{Op: "pass", Seat: 0},
					{Op: "pass_to", Decision: "priority"},
				},
			}, true)
		}
	case "trigger.discarded-opponent":
		// Mind Rot at p1, with the victim the row's filter accepts first in
		// p1's hand: setup leaves p1 holding exactly that card, so the discard
		// ask's fallback discards it.
		for _, victim := range discardVictimProbes {
			c, yes := castCause(reg, name, "Mind Rot", "p1")
			if !yes {
				continue
			}
			c.opponentHand = []string{victim}
			add(c, true)
		}
	case "trigger.changes-controller":
		// p1's Threaten takes p0's probe creature during p1's first main
		// phase: the GainControl's ControlChange names p1 as the new
		// controller with the original controller (p0) as the trigger's You.
		// The target is a probe beside the watcher, never the watcher itself,
		// whose controller the matcher reads; the victim is named so the
		// target ask cannot match the Zidane-ETB-relocated baseline bear
		// (whose original controller is not You).
		if st, yes := castProbe(reg, "Threaten", "p0:"+threatenVictim); yes {
			st.Seat, st.Card = 1, "p1:Threaten"
			add(triggerCause{
				opponentHand: []string{"Threaten"},
				battlefield:  []string{threatenVictim},
				steps: []oraclegen.Step{
					{Op: "pass_to", Step: "main1", Active: "p1"},
					st,
					{Op: "pass", Seat: 1},
					{Op: "pass", Seat: 0},
				},
			}, true)
		}
	case "trigger.exiled-craft":
		add(craftExileCause(reg, name))
	default:
		return nil, "", false
	}
	if len(causes) == 0 {
		return nil, "probe not in corpus", true
	}
	return causes, "", true
}

// craftExileCause activates the probe Craft ability whose only legal material
// is the watcher itself (an artifact, the one other artifact on p0's board),
// so the runner's material-pick fallback exiles the source and its Exiled
// trigger fires from the move.
func craftExileCause(reg *cards.Registry, name string) (triggerCause, bool) {
	card, ok := reg.Lookup(craftProbe)
	if !ok || len(card.Faces) == 0 {
		return triggerCause{}, false
	}
	pf := card.Faces[0]
	prefixes, why := oraclegen.XMageAbility(pf)
	if why != "" {
		return triggerCause{}, false
	}
	for i, sa := range pf.Abilities {
		if !sa.IsActivated() || !strings.HasPrefix(strings.ToLower(sa.Params["KeywordLine"]), "craft") {
			continue
		}
		mana, gap := activationCostIn(sa.ParamStr(cards.PKCost), "battlefield", craftProbe)
		if gap != "" {
			continue
		}
		prefix, okp := prefixes[i]
		if !okp {
			continue
		}
		idx := i
		return triggerCause{
			battlefield: []string{craftProbe},
			steps: []oraclegen.Step{{
				Op: "activate", Seat: 0, Card: "p0:" + craftProbe,
				Mana: mana, AbilityIndex: &idx,
			}},
			xability: []string{prefix},
		}, true
	}
	return triggerCause{}, false
}

// opponentArtifactAbilityCause activates a probe artifact's first non-mana,
// untargeted ability for p1 during p1's first main phase: Avalanche of Sector
// 7's artifact half reads the AbilityPush the activation emits (ValidSA$
// Activated.OppCtrl, ValidSAonCard$ Activated.YouCtrl — p1 activating its own
// artifact). The probe is Sensei's Divining Top: its {1} rearrange ability
// neither targets nor moves the source, so the source is still ON THE
// BATTLEFIELD when the paid-cost push fires the inZoneBattlefield read (a
// sacrificial probe like Mind Stone is already in the graveyard at push
// time and would never match).
func opponentArtifactAbilityCause(reg *cards.Registry, probe string) (triggerCause, bool) {
	card, ok := reg.Lookup(probe)
	if !ok || len(card.Faces) == 0 {
		return triggerCause{}, false
	}
	pf := card.Faces[0]
	prefixes, why := oraclegen.XMageAbility(pf)
	if why != "" {
		return triggerCause{}, false
	}
	for i, sa := range pf.Abilities {
		if !sa.IsActivated() || strings.EqualFold(sa.API, "Mana") || sa.ParamStr(cards.PKValidTgts) != "" {
			continue
		}
		mana, gap := activationCostIn(sa.ParamStr(cards.PKCost), "battlefield", probe)
		if gap != "" {
			continue
		}
		prefix, okp := prefixes[i]
		if !okp {
			continue
		}
		idx := i
		return triggerCause{
			opponentBattlefield: []string{probe},
			steps: []oraclegen.Step{
				{Op: "pass_to", Step: "main1", Active: "p1"},
				{Op: "activate", Seat: 1, Card: "p1:" + probe, Mana: mana, AbilityIndex: &idx},
			},
			// Parallel to steps: the pass_to carries no rule text, the
			// activate its own.
			xability: []string{"", prefix},
		}, true
	}
	return triggerCause{}, false
}
