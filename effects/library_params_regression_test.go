package effects

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestRepeatTypesFromIncludesRegisteredKindredType(t *testing.T) {
	h, src, lib := riderBoard(t,
		"Name:KindredRelic\nTypes:Kindred Artifact\nOracle:x\n",
		riderLand)
	h.Emit(events.Event{Kind: events.Imprint, Obj: src, IDs: append([]state.ObjID(nil), lib...), Text: "seek-found"})
	got, ok := repeatEachTypesFrom(h, &Ctx{Source: src, Controller: 0}, "ValidLibrary Card.IsImprinted")
	if !ok {
		t.Fatal("RepeatTypesFrom selector unexpectedly unsupported")
	}
	if !reflect.DeepEqual(got, []string{"Artifact", "Kindred", "Land"}) {
		t.Fatalf("RepeatTypesFrom types = %v, want [Artifact Kindred Land]", got)
	}
}

func TestSurveilRememberKeptIsCompiledAndShared(t *testing.T) {
	sa := &cards.SA{API: "Surveil", Params: map[string]string{"RememberKept": " True "}}
	p := SurveilOf(sa)
	if !p.RememberKept {
		t.Fatal("compiled RememberKept should be true")
	}
	if got := SurveilOf(sa); got != p {
		t.Fatal("SurveilOf did not reuse the bound compiled parameters")
	}
}
