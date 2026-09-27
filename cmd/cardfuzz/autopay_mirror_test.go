package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/paymirror"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

func mirrorFixture() []genDeck {
	var d genDeck
	d.Colour = "G"
	for i := 0; i < 60; i++ {
		switch {
		case i < 24:
			d.Cards = append(d.Cards, "Forest")
		case i < 36:
			d.Cards = append(d.Cards, "Llanowar Elves")
		case i < 50:
			d.Cards = append(d.Cards, "Grizzly Bears")
		default:
			d.Cards = append(d.Cards, "Hill Giant")
		}
	}
	return []genDeck{d, d}
}

func TestAutopayMirrorFlag(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	oldEnabled, oldOptions := autopayMirror, autopayMirrorOptions
	t.Cleanup(func() { autopayMirror, autopayMirrorOptions = oldEnabled, oldOptions })
	autopayMirror = true
	autopayMirrorOptions = paymirror.Options{Control: true}
	all, _ := parseAutoPay("all", false)
	_, gc := playGame(reg, mirrorFixture(), 9, 14, 20000, 0, true, false, all)
	if gc == nil || gc.ap.Planned == 0 || len(gc.mirrorVerdicts) == 0 {
		t.Fatalf("setup did not submit and mirror a planned cast: gc=%+v", gc)
	}
	for verdict, count := range gc.mirrorVerdicts {
		if verdict != string(paymirror.Equivalent) || count < 1 {
			t.Fatalf("ordinary mirror verdicts = %v; want equivalent only", gc.mirrorVerdicts)
		}
	}
	if len(gc.mirrorFailures) != 0 {
		t.Fatalf("equivalent baseline recorded failures: %+v", gc.mirrorFailures)
	}

	autopayMirrorOptions = paymirror.Options{Control: true, AfterRoute: func(e *rules.Engine) {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			return
		}
		for _, o := range d.Options {
			if o.Kind == "activate" {
				_ = e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}})
				return
			}
		}
	}}
	_, perturbedGame := playGame(reg, mirrorFixture(), 9, 14, 20000, 0, true, false, all)
	if perturbedGame == nil || len(perturbedGame.mirrorFailures) == 0 {
		t.Fatalf("perturbed mirror did not produce a failure record: %+v", perturbedGame)
	}
	found := false
	for _, record := range perturbedGame.mirrorFailures {
		if record.Kind == "mirror" && len(record.Sig) > len("mirror: ") {
			found = true
		}
	}
	if !found {
		t.Fatalf("perturbation records lack kind/sig: %+v", perturbedGame.mirrorFailures)
	}
	var record *failure
	for i := range perturbedGame.mirrorFailures {
		if perturbedGame.mirrorFailures[i].Kind == "mirror" {
			record = &perturbedGame.mirrorFailures[i]
			break
		}
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "mirror.jsonl")
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runRepro(reg, path, 1, 14, 20000, 0); code != 2 {
		t.Fatalf("-repro mirror exit = %d, want reproduced failure (2)", code)
	}
}
