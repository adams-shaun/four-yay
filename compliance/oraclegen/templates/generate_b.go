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
func GenerateB(reg *cards.Registry, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
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
	if triggerSubs(req.Sub) {
		c, ok := reg.Lookup(name)
		if !ok || req.Face < 0 || req.Face >= len(c.Faces) {
			return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "not in corpus"}
		}
		return triggerFires(reg, c.Faces[req.Face], name, req)
	}
	if staticSubs(req.Sub) {
		c, ok := reg.Lookup(name)
		if !ok || req.Face < 0 || req.Face >= len(c.Faces) {
			return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "not in corpus"}
		}
		return staticRequirement(reg, c.Faces[req.Face], name, req)
	}
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "level B: no template for " + req.Sub}
}
