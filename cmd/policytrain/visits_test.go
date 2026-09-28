package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/policynet"
)

// syntheticVisits writes a tiny visit corpus (no Forge script is read): per
// game, decisions whose three single-choice candidates are told apart by
// one hashed option row, where the TEACHER always favours the option
// carrying row 7 and the bot's answer (candidate 0) never does; the
// recording seat wins every even game, and the state carries that in a
// hashed row the value head can read.
func syntheticVisits(t *testing.T, path string, games int) {
	t.Helper()
	var buf bytes.Buffer
	for g := 0; g < games; g++ {
		win := g%2 == 0
		for k := 0; k < 6; k++ {
			st := policynet.State{Dense: make([]float32, policynet.DenseWidth)}
			st.Dense[0] = float32(k + 1)
			row := uint16(900)
			if win {
				row = 901
			}
			st.Sparse = []policynet.Feature{{Row: 5, Value: 1}, {Row: row, Value: 1}}
			good := 1 + (g+k)%2 // the teacher's option position
			rec := policynet.VisitRecord{
				RecordType: policynet.VisitRecordType, SchemaVersion: policynet.VisitSchemaVersion,
				EncoderHash: fmt.Sprintf("%016x", policynet.EncoderHashFor(policynet.FeaturesMZ)), World: "clairvoyant", Sims: 20,
				GameID: fmt.Sprintf("m0000p%04dg0", g), Deck: []string{"Burn", "Spy"}[g%2], Seed: uint64(1000 + g),
				Kind: "priority", Turn: int32(k + 1), Sequence: uint64(k), State: policynet.EncodeOnPolicyState(st),
				Cands: [][]int{{0}, {1}, {2}}, Visits: []int{4, 4, 4}, Prior: []float64{1.0 / 3, 1.0 / 3, 1.0 / 3},
				Q: []float64{0.5, 0.5, 0.5}, RootValue: 0.5, OutcomeKnown: true,
			}
			if win {
				rec.Outcome = 1
			}
			rec.Visits[good] = 14
			for i := 0; i < 3; i++ {
				o := policynet.Option{Dense: make([]float32, policynet.OptionDenseWidth), Hashed: []policynet.Feature{{Row: uint16(40 + i), Value: 1}}}
				if i == good {
					o.Hashed = append(o.Hashed, policynet.Feature{Row: 7, Value: 1})
				}
				o.BotPick = i == 0
				rec.Options = append(rec.Options, policynet.EncodeOnPolicyOption(o, true))
			}
			raw, err := json.Marshal(rec)
			if err != nil {
				t.Fatal(err)
			}
			buf.Write(append(raw, '\n'))
		}
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVisitsModeTrainsEvaluatesAndReloads(t *testing.T) {
	dir := t.TempDir()
	corpus, held := filepath.Join(dir, "train.jsonl"), filepath.Join(dir, "held.jsonl")
	syntheticVisits(t, corpus, 40)
	syntheticVisits(t, held, 10)
	ckpt, report := filepath.Join(dir, "s.gpol"), filepath.Join(dir, "r.json")
	var out, errb bytes.Buffer
	args := []string{"-visits-corpus", corpus, "-visits-eval", held, "-visits-report", report, "-out", ckpt,
		"-epochs", "20", "-embed", "8", "-hidden", "8", "-value-hidden", "4", "-value-weight", "1", "-lr", "0.5", "-seed", "3"}
	if rc := run(args, &out, &errb); rc != 0 {
		t.Fatalf("rc %d: %s", rc, errb.String())
	}
	var evs []VisitEval
	raw, err := os.ReadFile(report)
	if err != nil || json.Unmarshal(raw, &evs) != nil || len(evs) != 1 {
		t.Fatalf("report %s: %v", raw, err)
	}
	ev := evs[0]
	if ev.N != 60 || ev.Top1Teacher < 0.95 || ev.OverrideN != 60 || ev.CE >= ev.CEUniform || ev.AUC < 0.9 {
		t.Fatalf("teacher student eval %+v\n%s", ev, out.String())
	}
	// Behaviour cloning on the same states: the student keeps the bot.
	bc := filepath.Join(dir, "bc.gpol")
	out.Reset()
	args = []string{"-visits-corpus", corpus, "-visits-label", "bot", "-visits-eval", held, "-out", bc,
		"-epochs", "20", "-embed", "8", "-hidden", "8", "-value-hidden", "4", "-value-weight", "1", "-lr", "0.5", "-seed", "3"}
	if rc := run(args, &out, &errb); rc != 0 {
		t.Fatalf("bc rc %d: %s", rc, errb.String())
	}
	if !strings.Contains(out.String(), "picks bot 1.0000") {
		t.Fatalf("bc student does not keep the bot:\n%s", out.String())
	}
	// Eval-only: -epochs 0 scores -init and writes the same readout.
	out.Reset()
	report2 := filepath.Join(dir, "r2.json")
	if rc := run([]string{"-visits-eval", held, "-init", ckpt, "-epochs", "0", "-visits-report", report2}, &out, &errb); rc != 0 {
		t.Fatalf("eval-only rc %d: %s", rc, errb.String())
	}
	raw2, _ := os.ReadFile(report2)
	var evs2 []VisitEval
	if json.Unmarshal(raw2, &evs2) != nil || len(evs2) != 1 || evs2[0].Top1Teacher != ev.Top1Teacher || evs2[0].AUC != ev.AUC {
		t.Fatalf("eval-only readout %s differs from the trained run's", raw2)
	}
	// The diagnostic student is a measurement: no checkpoint.
	diag := filepath.Join(dir, "diag.gpol")
	out.Reset()
	if rc := run([]string{"-visits-corpus", corpus, "-visits-diag", "-out", diag, "-epochs", "1", "-embed", "8", "-hidden", "8"}, &out, &errb); rc != 0 {
		t.Fatalf("diag rc %d: %s", rc, errb.String())
	}
	if _, err := os.Stat(diag); err == nil || !strings.Contains(out.String(), "measurement only") {
		t.Fatalf("a diagnostic student must not be checkpointed:\n%s", out.String())
	}
}
