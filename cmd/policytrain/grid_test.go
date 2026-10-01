package main

import (
	"bytes"
	"encoding/json"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
)

// The CLI's pn12 flags: -features mz writes a checkpoint that loads as mz,
// a diagnostic feature set or the joint encoding writes none, -max-game-index
// trains on the nested subset only, and -eval-json carries the readout. With
// no pn12 flag the checkpoint is the one the pre-pn12 path writes, byte for
// byte (the same Load, the same training).
func TestGridFlags(t *testing.T) {
	path := writeJSONLCorpus(t, 48)
	dir := t.TempDir()
	base := []string{"-corpus", path, "-epochs", "2", "-embed", "8", "-hidden", "8", "-batch", "8", "-seed", "3", "-holdout", "0"}
	runOK := func(extra ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := run(append(append([]string(nil), base...), extra...), &stdout, &stderr); code != 0 {
			t.Fatalf("run %v = %d: %s", extra, code, stderr.String())
		}
		return stdout.String()
	}
	plain := filepath.Join(dir, "plain.gpol")
	runOK("-out", plain)
	v1 := filepath.Join(dir, "v1.gpol")
	runOK("-out", v1, "-features", "v1", "-actions", "split")
	a, _ := os.ReadFile(plain)
	b, _ := os.ReadFile(v1)
	if !bytes.Equal(a, b) {
		t.Fatal("explicit -features v1 -actions split differs from the default run")
	}
	mz := filepath.Join(dir, "mz.gpol")
	evalJSON := filepath.Join(dir, "eval.json")
	runOK("-out", mz, "-features", "mz", "-max-game-index", "24", "-eval-corpus", path, "-eval-json", evalJSON, "-arm", "mz-half")
	m, err := policynet.LoadCheckpointFile(mz)
	if err != nil || m.Features != policynet.FeaturesMZ {
		t.Fatalf("mz checkpoint: %v features %v", err, m)
	}
	var rep GridReport
	data, err := os.ReadFile(evalJSON)
	if err != nil || json.Unmarshal(data, &rep) != nil {
		t.Fatalf("eval json: %v", err)
	}
	if rep.Arm != "mz-half" || rep.TrainGames != 24 || rep.EvalExamples != 48 || len(rep.Kinds) != 1 || rep.Kinds[0].N != 48 {
		t.Fatalf("eval report %+v", rep)
	}
	// The honest own-library set (mz-ownlib) is opt-in by the same flag and
	// checkpointable: it writes a checkpoint that loads as mz-ownlib.
	ownlib := filepath.Join(dir, "ownlib.gpol")
	runOK("-out", ownlib, "-features", "mz-ownlib")
	if m, err := policynet.LoadCheckpointFile(ownlib); err != nil || m.Features != policynet.FeaturesMZOwnLib {
		t.Fatalf("mz-ownlib checkpoint: %v features %v", err, m)
	}
	for _, extra := range [][]string{{"-features", "mz-opphand"}, {"-actions", "joint"}} {
		out := filepath.Join(dir, "none.gpol")
		runOK(append([]string{"-out", out}, extra...)...)
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Fatalf("%v wrote a checkpoint", extra)
		}
	}
	var stderr bytes.Buffer
	if code := run(append(append([]string(nil), base...), "-out", filepath.Join(dir, "x"), "-features", "bogus"), &bytes.Buffer{}, &stderr); code != 2 {
		t.Fatalf("bogus feature set: exit %d", code)
	}
}

// evalGrid reads the joint encoding at the card level and the attackers
// subsets as whole sets under both admission rules.
func TestEvalGridCardLevelAndSets(t *testing.T) {
	m := policynet.NewModel(policynet.TableRows, 4, 4, rand.New(rand.NewPCG(1, 1)))
	opt := func(pref, bot bool) policynet.Option {
		return policynet.Option{Dense: make([]float32, policynet.OptionDenseWidth),
			Target: policynet.OptionTarget{Labelled: true, Preferred: pref}, BotPick: bot}
	}
	st := policynet.State{Dense: make([]float32, policynet.DenseWidth)}
	// Joint priority: two expansions of card 1; whichever the model picks,
	// the pick's card is compared. Card 1 is preferred through its SECOND
	// expansion only, so a pick of the first expansion still counts.
	joint := policynet.Example{Kind: decision.KPriority, TeacherChoice: 1, State: st,
		Options:   []policynet.Option{opt(false, true), opt(false, false), opt(true, false)},
		JointCard: []int{0, 1, 1}}
	scores := m.Score(joint.State, joint.Options)
	best := 0
	for k := range scores {
		if scores[k] > scores[best] {
			best = k
		}
	}
	got := evalGrid(m, []policynet.Example{joint})
	wantHit := joint.JointCard[best] == 1
	if len(got) != 1 || got[0].OverrideN != 1 || (got[0].OverrideTop1 == 1) != wantHit {
		t.Fatalf("joint card-level readout %+v (best %d)", got, best)
	}
	// An empty teacher attack declaration is counted by the set readout.
	atk := policynet.Example{Kind: decision.KAttackers, State: st, Options: []policynet.Option{opt(false, false), opt(false, false)}}
	got = evalGrid(m, []policynet.Example{atk})
	if len(got) != 1 || got[0].SetN != 1 || got[0].N != 0 {
		t.Fatalf("empty attack declaration: %+v", got)
	}
	// autoThreshold: straddle -> 0; one-signed -> mean; tie -> refuse.
	if r, ok := autoThreshold([]float32{-1, 2}); !ok || r != 0 {
		t.Fatal("straddle")
	}
	if r, ok := autoThreshold([]float32{1, 3}); !ok || r != 2 {
		t.Fatal("mean")
	}
	if _, ok := autoThreshold([]float32{1, 1}); ok {
		t.Fatal("tie")
	}
}
