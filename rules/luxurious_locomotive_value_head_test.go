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
	// TokenAmount. CrewSize is an implemented evaluator head (the creatures
	// that crewed the source this turn), so it must resolve at this boundary
	// and be registered as a coverage primitive: a card whose amount reads an
	// unregistered head would join the playable pool with a gate that can
	// never pass.
	e := layerEngine(t)
	source := e.G.AddObject(card, 0).ID
	if source == 0 {
		t.Fatal("precondition: the source object was not created, so the evaluator would read no source")
	}
	ctx := &effects.Ctx{Source: source, Controller: 0, SVars: face.SVars}
	amount, resolved := effects.EvalCountOK(e, ctx, cards.ValueHeadRecipeExpressions(body)["CrewSize"])
	if !resolved {
		t.Fatal("Count$CrewSize no longer resolves; the D8 evaluator arm or its modelled-head registration regressed")
	}
	if amount != 0 {
		t.Fatalf("Count$CrewSize = %d with no crewer this turn, want 0", amount)
	}
	if !effects.Supported()["count:CrewSize"] {
		t.Fatal("count:CrewSize is not a registered coverage primitive; add it to effects.modelledValueHeads")
	}
}
