package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestLuxuriousLocomotiveNestedTokenAmountValueHead(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Luxurious Locomotive")
	if !ok || len(card.Faces) == 0 {
		t.Fatal("Luxurious Locomotive missing from corpus")
	}
	face := card.Faces[0]
	body, ok := face.SVars["TrigTreasure"]
	if !ok || body != "DB$ Token | TokenAmount$ Count$CrewSize | TokenScript$ c_a_treasure_sac" {
		t.Fatalf("TrigTreasure precondition = %q, want token recipe with TokenAmount$ Count$CrewSize", body)
	}
	mentioned := false
	for _, trigger := range face.Triggers {
		if strings.Contains(trigger.Params["Execute"], "TrigTreasure") {
			mentioned = true
			break
		}
	}
	if !mentioned {
		t.Fatal("TrigTreasure is not referenced by a trigger effect")
	}
	found := false
	for _, head := range face.ValueHeads() {
		if head == "count:CrewSize" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ValueHeads() = %v, want count:CrewSize", face.ValueHeads())
	}

	// Check the exact nested amount at the same evaluator boundary used by
	// TokenAmount. CrewSize is currently not an implemented evaluator head;
	// false is the expected fail-closed verdict that this new attribution
	// makes visible to Registry.Unsupported.
	e := layerEngine(t)
	source := e.G.AddObject(card, 0).ID
	ctx := &effects.Ctx{Source: source, Controller: 0, SVars: face.SVars}
	if _, resolved := effects.EvalCountOK(e, ctx, cards.ValueHeadRecipeExpressions(body)["CrewSize"]); resolved {
		t.Fatal("Count$CrewSize unexpectedly resolved; update the evaluator-alignment expectation and coverage registration")
	}
}
