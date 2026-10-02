package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/policynet"
)

// tdVisits writes a visit corpus of `games` games of four records each, the
// records' turn numbered tag+1.. in file order so a test can tell which
// records a load kept. Every game's seat won; root values are 0.2.
func tdVisits(t *testing.T, path string, games, tag int) {
	t.Helper()
	var buf bytes.Buffer
	n := 0
	for g := 0; g < games; g++ {
		for k := 0; k < 4; k++ {
			n++
			st := policynet.State{Dense: make([]float32, policynet.DenseWidth),
				Sparse: []policynet.Feature{{Row: uint16(5 + k), Value: 1}}}
			rec := policynet.VisitRecord{
				RecordType: policynet.VisitRecordType, SchemaVersion: policynet.VisitSchemaVersion,
				EncoderHash: fmt.Sprintf("%016x", policynet.EncoderHashFor(policynet.FeaturesMZ)), World: "clairvoyant", Sims: 20,
				GameID: fmt.Sprintf("m0000p%04dg0", g), Deck: "D", Seed: uint64(1000 + g),
				Kind: "priority", Turn: int32(tag + n), Sequence: uint64(k), State: policynet.EncodeOnPolicyState(st),
				Cands: [][]int{{0}, {1}}, Visits: []int{5, 15}, Prior: []float64{0.5, 0.5},
				Q: []float64{0.5, 0.5}, RootValue: 0.2, Outcome: 1, OutcomeKnown: true,
			}
			for i := 0; i < 2; i++ {
				o := policynet.Option{Dense: make([]float32, policynet.OptionDenseWidth), Hashed: []policynet.Feature{{Row: uint16(40 + i), Value: 1}}}
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

// The window keeps the newest n records: later files of the list first, and
// within a file its tail; a file wholly outside the window is never opened.
func TestVisitsWindowKeepsTheNewestRecords(t *testing.T) {
	dir := t.TempDir()
	a, b, c := filepath.Join(dir, "a.jsonl"), filepath.Join(dir, "b.jsonl"), filepath.Join(dir, "c.jsonl")
	tdVisits(t, b, 3, 200)        // 12 records, turns 201..212
	tdVisits(t, c, 3, 300)        // 12 records, turns 301..312
	list := a + "," + b + "," + c // a does not exist: reading it would fail
	var out bytes.Buffer
	exs, _, err := loadVisitTrain(visitsArgs{corpora: list, window: 20, label: policynet.VisitLabelTeacher}, &out)
	if err != nil {
		t.Fatal(err)
	}
	var turns []int32
	for _, ex := range exs {
		turns = append(turns, ex.Turn)
	}
	want := []int32{205, 206, 207, 208, 209, 210, 211, 212, 301, 302, 303, 304, 305, 306, 307, 308, 309, 310, 311, 312}
	if !reflect.DeepEqual(turns, want) {
		t.Fatalf("window 20 kept turns %v, want %v", turns, want)
	}
	if !strings.Contains(out.String(), a+": outside the -visits-window") {
		t.Fatalf("the skipped file is not reported:\n%s", out.String())
	}
	// Window 0 reads everything, so the missing file is an error.
	if _, _, err := loadVisitTrain(visitsArgs{corpora: list, label: policynet.VisitLabelTeacher}, &out); err == nil {
		t.Fatal("window 0 must read every file")
	}
}

// -visits-td-lambda stamps the trajectory target on every loaded example,
// computed before the window cuts a game.
func TestVisitsTDLambdaTargetsReachTheExamples(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.jsonl")
	tdVisits(t, p, 2, 0)
	var out bytes.Buffer
	exs, _, err := loadVisitTrain(visitsArgs{corpora: p, window: 3, tdLambda: 0.5, label: policynet.VisitLabelTeacher}, &out)
	if err != nil || len(exs) != 3 {
		t.Fatalf("%d examples, %v", len(exs), err)
	}
	// The second game's records 1..3 (of 0..3): y3 = 0.5*1 + 0.5*0.2 = 0.6,
	// y2 = 0.5*0.6 + 0.1 = 0.4, y1 = 0.5*0.4 + 0.1 = 0.3.
	for i, want := range []float64{0.3, 0.4, 0.6} {
		if !exs[i].HasTDTarget || math.Abs(exs[i].TDTarget-want) > 1e-12 {
			t.Errorf("example %d target %v (%v), want %v", i, exs[i].TDTarget, exs[i].HasTDTarget, want)
		}
	}
	exs, _, err = loadVisitTrain(visitsArgs{corpora: p, label: policynet.VisitLabelTeacher}, &out)
	if err != nil || len(exs) != 8 || exs[0].HasTDTarget {
		t.Fatalf("default load: %d examples, td %v, %v", len(exs), exs[0].HasTDTarget, err)
	}
}

func loadCkpt(t *testing.T, path string) *policynet.Model {
	t.Helper()
	m, err := policynet.LoadCheckpointFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// -init continues from the loaded weights, and -visits-policy-weight 0 then
// trains the value head alone: the policy head's blocks stay bit-identical
// to the checkpoint's while the value head moves.
func TestVisitsInitContinuesAndPolicyWeightZeroFreezesThePolicyHead(t *testing.T) {
	dir := t.TempDir()
	corpus := filepath.Join(dir, "train.jsonl")
	syntheticVisits(t, corpus, 20)
	a, b, c := filepath.Join(dir, "a.gpol"), filepath.Join(dir, "b.gpol"), filepath.Join(dir, "c.gpol")
	geom := []string{"-embed", "8", "-hidden", "8", "-value-hidden", "4", "-value-weight", "1", "-lr", "0.5", "-holdout", "0"}
	var out, errb bytes.Buffer
	args := append([]string{"-visits-corpus", corpus, "-out", a, "-epochs", "2", "-seed", "3"}, geom...)
	if rc := run(args, &out, &errb); rc != 0 {
		t.Fatalf("rc %d: %s", rc, errb.String())
	}
	// Value only, continuing from a under another seed.
	args = append([]string{"-visits-corpus", corpus, "-out", b, "-init", a, "-epochs", "1", "-seed", "9",
		"-visits-policy-weight", "0", "-visits-td-lambda", "0.95"}, geom...)
	if rc := run(args, &out, &errb); rc != 0 {
		t.Fatalf("value-only rc %d: %s", rc, errb.String())
	}
	ma, mb := loadCkpt(t, a), loadCkpt(t, b)
	if !reflect.DeepEqual(ma.HidW, mb.HidW) || !reflect.DeepEqual(ma.HidB, mb.HidB) || !reflect.DeepEqual(ma.OutW, mb.OutW) || ma.OutB != mb.OutB {
		t.Fatal("policy weight 0 moved the policy head")
	}
	if reflect.DeepEqual(ma.VHidW, mb.VHidW) && reflect.DeepEqual(ma.VOutW, mb.VOutW) && ma.VOutB == mb.VOutB {
		t.Fatal("the value head did not train")
	}
	// The default policy weight continues from a too, and moves its head.
	args = append([]string{"-visits-corpus", corpus, "-out", c, "-init", a, "-epochs", "1", "-seed", "9"}, geom...)
	if rc := run(args, &out, &errb); rc != 0 {
		t.Fatalf("continue rc %d: %s", rc, errb.String())
	}
	mc := loadCkpt(t, c)
	if reflect.DeepEqual(ma.HidW, mc.HidW) {
		t.Fatal("continuing with the policy term on left the policy head at the checkpoint's values")
	}
	// One more epoch from a is not two epochs from scratch under seed 9: the
	// run really started at a.
	fresh := filepath.Join(dir, "fresh.gpol")
	args = append([]string{"-visits-corpus", corpus, "-out", fresh, "-epochs", "1", "-seed", "9"}, geom...)
	if rc := run(args, &out, &errb); rc != 0 {
		t.Fatalf("fresh rc %d: %s", rc, errb.String())
	}
	if reflect.DeepEqual(loadCkpt(t, fresh).HidW, mc.HidW) {
		t.Fatal("-init was ignored: the continued run equals a fresh one")
	}
	// A geometry that is not the checkpoint's is refused, naming both.
	errb.Reset()
	args = []string{"-visits-corpus", corpus, "-out", c, "-init", a, "-epochs", "1", "-embed", "8", "-hidden", "16",
		"-value-hidden", "4", "-value-weight", "1"}
	if rc := run(args, &out, &errb); rc == 0 || !strings.Contains(errb.String(), "hidden 8") || !strings.Contains(errb.String(), "16") {
		t.Fatalf("mismatched -init: rc %d, %s", rc, errb.String())
	}
	// Policy weight 0 with no value term trains nothing.
	errb.Reset()
	args = []string{"-visits-corpus", corpus, "-out", c, "-epochs", "1", "-visits-policy-weight", "0"}
	if rc := run(args, &out, &errb); rc == 0 || !strings.Contains(errb.String(), "-value-weight") {
		t.Fatalf("policy weight 0 without a value term: rc %d, %s", rc, errb.String())
	}
}

// With none of the new flags, the trainer's checkpoint is the one it wrote
// before they existed: the same bytes whether the defaults are spelt or not.
func TestVisitsDefaultsUnchanged(t *testing.T) {
	dir := t.TempDir()
	corpus := filepath.Join(dir, "train.jsonl")
	syntheticVisits(t, corpus, 12)
	a, b := filepath.Join(dir, "a.gpol"), filepath.Join(dir, "b.gpol")
	base := []string{"-visits-corpus", corpus, "-epochs", "2", "-embed", "8", "-hidden", "8", "-value-hidden", "4", "-value-weight", "1"}
	var out, errb bytes.Buffer
	if rc := run(append([]string{"-out", a}, base...), &out, &errb); rc != 0 {
		t.Fatal(errb.String())
	}
	spelt := append([]string{"-out", b, "-visits-td-lambda", "0", "-visits-policy-weight", "1", "-visits-window", "0"}, base...)
	if rc := run(spelt, &out, &errb); rc != 0 {
		t.Fatal(errb.String())
	}
	ra, _ := os.ReadFile(a)
	rb, _ := os.ReadFile(b)
	if len(ra) == 0 || !bytes.Equal(ra, rb) {
		t.Fatal("spelling the new flags' defaults changed the checkpoint")
	}
}
