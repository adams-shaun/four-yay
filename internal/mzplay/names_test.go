package mzplay

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/mzbridge"
	"github.com/adams-shaun/gorge/internal/searchbench"
)

const labelVocab = "dim\t64\n" +
	"A\t0\tPass\n" +
	"A\t1\t{T}: Add {B}.\n" +
	"A\t7\tCast Grow from the Ashes\n" +
	"A\t8\tCast Think Twice\n" +
	"A\t9\tEquip {1}\n" +
	"A\t10\tEquip {1} <i>({1}: Attach to target creature you control. Equip only as a sorcery.)</i>\n" +
	"A\t11\tFlashback {2}{U}\n" +
	"A\t12\tPlay Forest\n" +
	"A\t13\t{T}: Surveil 1. <i>(Look at the top card of your library. You may put that card into your graveyard.)</i>\n" +
	"A\t14\t{2}{R}: {this} gets +1/+0 until end of turn.\n" +
	"A\t15\tCrew 3 <i>(Tap any number of creatures: This Vehicle becomes an artifact creature.)</i>\n" +
	"A\t16\t{T}, Sacrifice {this}: Draw a card.\n" +
	"T\t0\tStop Choosing\nT\t1\tPlayerA\nT\t2\tPlayerB\nT\t3\tGrizzly Bears\n"

// TestActionLabelsReachTheVocabulary: every kind of root candidate is given
// upstream's ability.toString() and lands on that string's vocabulary index.
func TestActionLabelsReachTheVocabulary(t *testing.T) {
	vocab, err := mzbridge.ParseVocab(strings.NewReader(labelVocab))
	if err != nil {
		t.Fatal(err)
	}
	ix := newActionIndexer(vocab)
	tail := vocab.ActionHashStart()
	for _, tc := range []struct {
		name            string
		a               searchbench.LiveAction
		rule, flashback string
		wantLabel       string
		wantIndex       int // -1: the hashed tail
	}{
		{"pass", searchbench.LiveAction{Kind: "pass", Label: "Pass"}, "", "", "Pass", 0},
		{"cast", searchbench.LiveAction{Kind: "cast", Name: "Think Twice"}, "", "", "Cast Think Twice", 8},
		{"kicked cast is the same spell ability", searchbench.LiveAction{Kind: "cast", Name: "Grow from the Ashes", Mode: "kicked"}, "", "", "Cast Grow from the Ashes", 7},
		{"cast through a permission", searchbench.LiveAction{Kind: "cast", Name: "Think Twice", Mode: "mayplay"}, "", "", "Cast Think Twice", 8},
		{"flashback", searchbench.LiveAction{Kind: "cast", Name: "Think Twice", Mode: "flashback"}, "", "{2}{U}", "Flashback {2}{U}", 11},
		{"flashback with no known cost", searchbench.LiveAction{Kind: "cast", Name: "Think Twice", Mode: "flashback"}, "", "", "Cast Think Twice", 8},
		{"land", searchbench.LiveAction{Kind: "land", Name: "Forest"}, "", "", "Play Forest", 12},
		{"land outside the vocabulary", searchbench.LiveAction{Kind: "land", Name: "Karakas"}, "", "", "Play Karakas", -1},
		{"ability by rule", searchbench.LiveAction{Kind: "ability", Label: "Activate Shivan: pump"}, "{2}{R}: {this} gets +1/+0 until end of turn.", "", "{2}{R}: {this} gets +1/+0 until end of turn.", 14},
		{"ability, Oracle self-reference", searchbench.LiveAction{Kind: "ability"}, "{2}{R}: this creature gets +1/+0 until end of turn.", "", "{2}{R}: {this} gets +1/+0 until end of turn.", 14},
		{"ability, self-reference in the cost", searchbench.LiveAction{Kind: "ability"}, "{T}, Sacrifice this artifact: Draw a card.", "", "{T}, Sacrifice {this}: Draw a card.", 16},
		{"equip", searchbench.LiveAction{Kind: "ability"}, "{1}: Equip 1", "", "Equip {1}", 9},
		{"reminder text", searchbench.LiveAction{Kind: "ability"}, "{T}: Surveil 1.", "", "{T}: Surveil 1. <i>(Look at the top card of your library. You may put that card into your graveyard.)</i>", 13},
		{"reminder text, keyword only", searchbench.LiveAction{Kind: "ability"}, "Crew 3", "", "Crew 3 <i>(Tap any number of creatures: This Vehicle becomes an artifact creature.)</i>", 15},
		{"ability without a rule keeps its label", searchbench.LiveAction{Kind: "ability", Label: "Activate Relic: Cycling"}, "", "", "Activate Relic: Cycling", -1},
		{"unknown ability text", searchbench.LiveAction{Kind: "ability"}, "{9}: You win the game.", "", "{9}: You win the game.", -1},
		{"nameless", searchbench.LiveAction{}, "", "", "?", -1},
	} {
		label, known := ix.resolve(ActionLabel(tc.a, tc.rule, tc.flashback))
		if label != tc.wantLabel {
			t.Errorf("%s: label %q, want %q", tc.name, label, tc.wantLabel)
			continue
		}
		got := vocab.ActionIndex(label)
		switch {
		case tc.wantIndex >= 0 && (got != tc.wantIndex || !known):
			t.Errorf("%s: %q at index %d (known %v), want %d", tc.name, label, got, known, tc.wantIndex)
		case tc.wantIndex < 0 && (got < tail || got >= vocab.Dim() || known):
			t.Errorf("%s: %q at index %d (known %v), want the hashed tail [%d,%d)", tc.name, label, got, known, tail, vocab.Dim())
		}
	}
	// "Equip {1}" has a slot of its own, so the reminder alias does not
	// claim it for the longer entry.
	if _, ok := ix.alias["Equip {1}"]; ok {
		t.Error("the reminder alias shadows a label with a slot of its own")
	}
}

func TestFlashbackCost(t *testing.T) {
	for kw, want := range map[string]string{
		"Flashback:2 U":    "{2}{U}",
		"Flashback:5 U U":  "{5}{U}{U}",
		"Flashback:1 WU":   "{1}{W/U}",
		"Flashback:Sac<1>": "",
		"Flying":           "",
		"Flashback:X R R":  "{X}{R}{R}",
		"Flashback:2 B B ": "{2}{B}{B}",
	} {
		if got := flashbackCost(&cards.Face{Keywords: []string{"Haste", kw}}); got != want {
			t.Errorf("%q: %q, want %q", kw, got, want)
		}
	}
	if flashbackCost(nil) != "" {
		t.Error("nil face")
	}
}

func TestDecisionTexts(t *testing.T) {
	if got := attackText("Grizzly Bears"); got != "attack with: Grizzly Bears?" {
		t.Errorf("attack text %q", got)
	}
	if got := blockText("Wall of Wood"); got != "choose which creature to block for Wall of Wood:Choose a target:attacking creature" {
		t.Errorf("block text %q", got)
	}
	at, text := decisionHead(nil, nil)
	if at != mzbridge.Priority || text != "priority" {
		t.Errorf("no decision: %v %q", at, text)
	}
	at, text = decisionHead(nil, &decision.Decision{Kind: decision.KPriority})
	if at != mzbridge.Priority || text != "priority" {
		t.Errorf("priority: %v %q", at, text)
	}
	at, text = decisionHead(nil, &decision.Decision{Kind: decision.KChoose, Prompt: "Choose a colour"})
	if at != mzbridge.MakeChoice || text != "Choose a colour" {
		t.Errorf("choice: %v %q", at, text)
	}
}
