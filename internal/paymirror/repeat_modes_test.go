package paymirror

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestMapAnswerCanRepeatModes(t *testing.T) {
	// Rootcast Apprenticeship offers four modes and permits choosing three;
	// the recorded choice [0,1,0] intentionally repeats its first mode.
	recordedOptions := []decision.Option{
		{Index: 0, Kind: "mode", Label: "Put a +1/+1 counter"},
		{Index: 1, Kind: "mode", Label: "Copy a permanent"},
		{Index: 2, Kind: "mode", Label: "Create a token"},
		{Index: 3, Kind: "mode", Label: "Sacrifice a permanent"},
	}
	mirrorOptions := []decision.Option{
		{Index: 10, Kind: "mode", Label: "Put a +1/+1 counter"},
		{Index: 11, Kind: "mode", Label: "Copy a permanent"},
		{Index: 12, Kind: "mode", Label: "Create a token"},
		{Index: 13, Kind: "mode", Label: "Sacrifice a permanent"},
	}
	r := Recorded{
		Kind: decision.KModes, Player: state.PlayerID(0), Prompt: "Choose three modes",
		Min: 3, Max: 3, Options: recordedOptions, Choices: []int{0, 1, 0},
	}
	d := &decision.Decision{
		Kind: decision.KModes, Player: state.PlayerID(0), Prompt: "Choose three modes",
		Min: 3, Max: 3, Options: mirrorOptions,
	}

	choices, rest, note, ok := mapAnswer(r, d)
	if !ok {
		t.Fatalf("repeated mode answer was not mappable (would report follow_up_unmappable): %s", note)
	}
	if want := []int{10, 11, 10}; !reflect.DeepEqual(choices, want) {
		t.Fatalf("mapped choices = %v, want %v", choices, want)
	}
	if len(rest) != 0 {
		t.Fatalf("mapped rest = %v, want empty", rest)
	}
}
