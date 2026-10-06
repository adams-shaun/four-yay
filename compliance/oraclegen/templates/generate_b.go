// Level-B dispatch. GenerateB is the one entry point for a level-B
// requirement's scenario, dispatched by the requirement's sub-family. It is
// deliberately a stub in L2: every requirement is a template gap until a
// template family lands (L5 activate, L7 trigger, L11 static, L13 combat),
// each adding one dispatch line here. Level-A Generate is untouched, so no
// level-A byte or verdict moves.
package templates

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// GenerateB builds the level-B scenario serving one requirement, or says why
// none does. A requirement the classifier already marked as a gap
// (req.Gap != "") reports that gap; every other requirement reports that no
// template serves its sub-family yet. Both reasons carry the "level B: "
// prefix so the gate's level-A wording ("no generated scenario (...)") reads
// as a template gap and adopt.Bucket classifies it unchanged.
func GenerateB(reg *cards.Registry, name string, req levelb.Requirement) (item oraclegen.Item, skip *oraclegen.Skip) {
	// Level-B builders all converge here, unlike level-A's Template.item.
	// Apply the face-level structural opt-in centrally so trigger, activate,
	// combat and static items cannot silently omit it. Preserve any options
	// the family builder already attached.
	defer func() {
		if item.ID == "" {
			return
		}
		c, ok := reg.Lookup(name)
		if !ok || req.Face < 0 || req.Face >= len(c.Faces) || !oraclegen.CanShuffleLibrary(c.Faces[req.Face]) {
			return
		}
		if !hasCompareOption(item.Compare, oraclegen.CompareNoLibraryOrder) {
			item.Compare = append(item.Compare, oraclegen.CompareNoLibraryOrder)
		}
		// A shuffle-then-draw draws random cards in XMage. Make the shuffled
		// zones uniform so the drawn hand is the same whatever the order; only
		// a setup holding several distinct shuffled names compares the hand by
		// size instead.
		if oraclegen.ShufflesThenDraws(c.Faces[req.Face]) {
			if !oraclegen.UniformShuffledZones(&item.Scenario, name) && !hasCompareOption(item.Compare, oraclegen.CompareHandCount) {
				item.Compare = append(item.Compare, oraclegen.CompareHandCount)
			}
		}
	}()
	if req.Gap != "" {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "level B: " + req.Gap}
	}
	if activateSubs(req.Sub) {
		c, ok := reg.Lookup(name)
		if !ok || req.Face < 0 || req.Face >= len(c.Faces) {
			return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "not in corpus"}
		}
		return activateAbility(reg, c.Faces[req.Face], name, req)
	}
	if req.Sub == "static.cost" {
		c, ok := reg.Lookup(name)
		if !ok || req.Face < 0 || req.Face >= len(c.Faces) {
			return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "not in corpus"}
		}
		return costStatic(reg, c.Faces[req.Face], name, req)
	}
	if triggerSubs(req.Sub) {
		c, ok := reg.Lookup(name)
		if !ok || req.Face < 0 || req.Face >= len(c.Faces) {
			return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "not in corpus"}
		}
		return triggerFires(reg, c.Faces[req.Face], name, req)
	}
	if combatSubs(req.Sub) {
		c, ok := reg.Lookup(name)
		if !ok || req.Face < 0 || req.Face >= len(c.Faces) {
			return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "not in corpus"}
		}
		return combatRequirement(reg, c.Faces[req.Face], name, req)
	}
	if staticSubs(req.Sub) {
		c, ok := reg.Lookup(name)
		if !ok || req.Face < 0 || req.Face >= len(c.Faces) {
			return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "not in corpus"}
		}
		return staticRequirement(reg, c.Faces[req.Face], name, req)
	}
	if req.Sub == "static.continuous" {
		c, ok := reg.Lookup(name)
		if !ok || req.Face < 0 || req.Face >= len(c.Faces) {
			return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "not in corpus"}
		}
		return staticContinuous(reg, c.Faces[req.Face], name, req)
	}
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "level B: no template for " + req.Sub}
}

func hasCompareOption(options []string, want string) bool {
	for _, option := range options {
		if option == want {
			return true
		}
	}
	return false
}
