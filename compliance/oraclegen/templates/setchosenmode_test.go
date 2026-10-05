package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestSetChosenModeGenericChoiceAnswersOnChoiceQueue is the real-card pin for
// the Theros-style Sieges: a DB$ GenericChoice | SetChosenMode$ True body makes
// XMage pose its pick through ChooseModeEffect -> controller.choose(Outcome.
// Neutral, Choice, game), the CHOICE queue, showing the option LABEL. The
// generated xmage_answers must therefore be {kind:"choice", value:"<label>"},
// NOT a numeric {kind:"mode"}. The five TDM Sieges are run because between them
// they carry every ModeChoice label Forge names (Abzan, Mardu, Jeskai, Temur,
// Sultai), so one clan each proves the whole label set routes.
func TestSetChosenModeGenericChoiceAnswersOnChoiceQueue(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	// The label gorge's mode decision picks for each Siege, and the clan it
	// names. Every Siege offers exactly its own two Choices$; gorge takes the
	// first, and the five firsts cover all five ModeChoice labels.
	want := map[string]string{
		"Barrensteppe Siege": "Abzan",
		"Windcrag Siege":     "Mardu",
		"Frostcliff Siege":   "Jeskai",
		"Glacierwood Siege":  "Temur",
		"Hollowmurk Siege":   "Sultai",
	}
	for _, name := range []string{"Barrensteppe Siege", "Windcrag Siege", "Frostcliff Siege", "Glacierwood Siege", "Hollowmurk Siege"} {
		t.Run(name, func(t *testing.T) {
			clan, ok := want[name]
			if !ok {
				t.Fatalf("no expected clan for %s", name)
			}
			item, skip := Generate(reg, name)
			if skip != nil {
				t.Fatalf("%s: %s", name, skip.Reason)
			}
			// The mode answer is posed at the resolve step, so the answers
			// live in the second (non-nil) group. A nil-or-empty answers list
			// would make the assertion vacuous, so require it.
			var got []oraclegen.XAnswer
			for _, step := range item.XAnswers {
				if len(step) > 0 {
					got = step
					break
				}
			}
			if len(got) == 0 {
				t.Fatalf("%s: generated no xmage_answers at all: %+v", name, item.XAnswers)
			}
			found := false
			for _, a := range got {
				if a.Seat == 0 && a.Kind == "choice" && a.Value == clan {
					found = true
					break
				}
				if a.Kind == "mode" {
					t.Fatalf("%s: emitted a numeric mode answer %+v; the SetChosenMode pick must reach the choice queue", name, a)
				}
			}
			if !found {
				t.Fatalf("%s: no {seat:0, kind:choice, value:%q} answer in %+v", name, clan, got)
			}
		})
	}
}

// TestCharmModeStillAnswersOnModeQueue guards the other side of the routing: an
// ordinary Charm must keep the numeric mode queue. Azorius Charm's first mode
// is a real charm mode and must not be diverted by the SetChosenMode scan.
func TestCharmModeStillAnswersOnModeQueue(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	item, skip := Generate(reg, "Azorius Charm")
	if skip != nil {
		t.Fatalf("Azorius Charm: %s", skip.Reason)
	}
	var got []oraclegen.XAnswer
	for _, step := range item.XAnswers {
		if len(step) > 0 {
			got = step
			break
		}
	}
	if len(got) == 0 {
		t.Fatalf("Azorius Charm: generated no xmage_answers at all: %+v", item.XAnswers)
	}
	foundMode := false
	for _, a := range got {
		if a.Kind == "mode" {
			foundMode = true
		}
		if a.Kind == "choice" && strings.EqualFold(a.Value, "Creatures you control gain lifelink until end of turn.") {
			t.Fatalf("Azorius Charm: its charm mode was diverted to the choice queue: %+v", got)
		}
	}
	if !foundMode {
		t.Fatalf("Azorius Charm: no numeric mode answer in %+v", got)
	}
}
