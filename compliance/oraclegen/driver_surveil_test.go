package oraclegen

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// TestScriptedLibraryAnswersPinDriverConsumer couples the driver contract to
// the Java consumer: reverting the choice-queue integration or its ordered
// library reconstruction must fail even when the generator output is unchanged.
func TestScriptedLibraryAnswersPinDriverConsumer(t *testing.T) {
	source, err := os.ReadFile("../../tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)
	for _, required := range []string{
		"public boolean scry(int value, Ability source, Game game)",
		"public Player.SurveilResult doSurveil(int value, Ability source, Game game)",
		"scriptedLibrarySelection(cards, game)",
		"List<String> queue = getChoices();",
		"TestPlayer.CHOICE_SKIP.equals(answer)",
		"getLibrary().getCards(game)",
		"if (cards.contains(card.getId()))",
		"for (Card card : lookedAtOrder)",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("ScenarioReplay.java no longer implements scripted library answer contract %q", required)
		}
	}
}

// TestArrangeAnswersMatchScriptedSurveilQueue pins the choice-queue sequence
// consumed by scry/surveil: selection, optional terminator, then order labels.
func TestArrangeAnswersMatchScriptedSurveilQueue(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "order", GorgeKind: "arrange",
		Options: 3, Min: 0, Max: 2,
		Picks: []string{"Forest"}, PickIdx: []int{1}, PickKinds: []string{"graveyard"},
	}
	got := xanswers([]rules.OracleDecision{d}, 1, nil, nil)
	want := [][]XAnswer{{
		{Seat: 0, Kind: "choice", Value: "Forest"},
		{Seat: 0, Kind: "choice", Value: "[choice_skip]"},
		{Seat: 0, Kind: "choice", Value: "Forest"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("arrange xanswers = %#v, want %#v", got, want)
	}
}
