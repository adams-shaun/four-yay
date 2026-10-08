package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestFaceStepSensitiveTokens(t *testing.T) {
	plain := &cards.Face{Name: "plain", Abilities: []*cards.SA{{Kind: "AB", API: "Draw", Params: map[string]string{"Cost": "T"}}}}
	if faceStepSensitive(plain) {
		t.Fatal("plain ability flagged step-sensitive")
	}
	for name, f := range map[string]*cards.Face{
		"ability phases": {Abilities: []*cards.SA{{Kind: "AB", Params: map[string]string{"ActivationPhases": "Upkeep"}}}},
		"sub chain":      {Abilities: []*cards.SA{{Kind: "AB", Sub: &cards.SA{Kind: "DB", Params: map[string]string{"ConditionPhases": "Main1"}}}}},
		"static phases":  {Statics: []cards.Static{{Mode: "CantBeCast", Params: map[string]string{"Phases": "End of Turn"}}}},
		"svar head":      {SVars: map[string]string{"X": "Count$InOwnMainPhase.1.0"}},
		"first combat":   {Abilities: []*cards.SA{{Kind: "AB", Params: map[string]string{"ActivationFirstCombat": "True"}}}},
		"sneak":          {Keywords: []string{"Sneak:2 R"}},
		"paradigm":       {Keywords: []string{"Paradigm"}},
	} {
		if !faceStepSensitive(f) {
			t.Errorf("%s: not flagged step-sensitive", name)
		}
	}
	// A face that grows after the first read is re-scanned.
	g := &cards.Face{Name: "grows"}
	if faceStepSensitive(g) {
		t.Fatal("empty face flagged")
	}
	g.Statics = append(g.Statics, cards.Static{Mode: "Continuous", Params: map[string]string{"Phases": "Main1"}})
	if !faceStepSensitive(g) {
		t.Fatal("grown face kept its stale answer")
	}
}
