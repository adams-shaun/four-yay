package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestConditionPreludeSacrificeIsASacrifice(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	preludes := conditionPreludes(reg, nil, map[string]string{
		"X": "PlayerCountPropertyYou$SacrificedThisTurn",
	})
	var sacrifice *conditionPrelude
	for i := range preludes {
		p := &preludes[i]
		if containsString(p.hand, "Village Rites") {
			sacrifice = p
		}
		for _, step := range p.steps {
			if strings.Contains(step.Card, "Murder") && !containsString(p.hand, "Village Rites") {
				t.Fatalf("Murder destroy candidate is not a sacrifice: %+v", p)
			}
		}
	}
	if sacrifice == nil {
		t.Fatalf("no Village Rites sacrifice candidate: %+v", preludes)
	}
	if !containsString(sacrifice.battlefield, "Grizzly Bears") {
		t.Fatalf("sacrifice fodder not on battlefield: %v", sacrifice.battlefield)
	}
	castFound, answerFound := false, false
	for _, step := range sacrifice.steps {
		if strings.TrimPrefix(step.Card, "p0:") == "Village Rites" {
			castFound = true
			for _, answer := range step.Answers {
				answerFound = answerFound || answer.Kind == "choose" && len(answer.Pick) == 1 && answer.Pick[0] == "Grizzly Bears"
			}
		}
	}
	if !castFound || !answerFound {
		t.Fatalf("prelude must cast Village Rites with an explicit Bears sacrifice answer: %+v", sacrifice.steps)
	}
}
