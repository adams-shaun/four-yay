package policynet

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
)

// visitFixture is the value fixture with a visit target over its four
// options: single-choice candidates {0},{1},{2},{3}, or subset candidates
// {} (declare nothing), {0,1}, {1,2,3}, {3}.
func visitFixture(t *testing.T, subset bool, seed uint64) (*Model, Example, LossConfig) {
	t.Helper()
	m, ex, lc := valueFixture(rand.New(rand.NewPCG(seed, seed+1)))
	for i := range ex.Options {
		ex.Options[i].Target = OptionTarget{Labelled: true}
	}
	vt := &VisitTarget{Cands: [][]int{{0}, {1}, {2}, {3}}, Pi: []float64{0.1, 0.5, 0.15, 0.25}, Teacher: 1}
	if subset {
		vt.Cands = [][]int{{}, {0, 1}, {1, 2, 3}, {3}}
	}
	ex.Visits = vt
	return m, ex, lc
}

// TestVisitGradientFiniteDifference pins the soft candidate cross-entropy's
// gradient (q - pi routed to every option of a candidate) and the joint value
// term against a central finite difference, for both candidate shapes.
func TestVisitGradientFiniteDifference(t *testing.T) {
	for _, subset := range []bool{false, true} {
		m, ex, lc := visitFixture(t, subset, 7)
		g := m.NewGrads()
		g.Zero()
		st := m.LossGrad(ex, lc, g)
		if st.Parts.Rank <= 0 || st.Parts.ValueHead <= 0 || !st.Eligible {
			t.Fatalf("subset=%v: fixture needs a policy and a value term: %+v", subset, st)
		}
		if lst := m.Loss(ex, lc); lst.Loss != st.Loss || lst.Parts != st.Parts {
			t.Fatalf("Loss %+v and LossGrad %+v disagree", lst, st)
		}
		fdAll(t, m, ex, lc, g)
	}
}

// The candidate logits' softmax is the search prior's softmax over
// CandidateScore: for a subset kind the Bernoulli log-likelihood differs from
// the summed scores by a constant every candidate shares.
func TestCandidateLogitsMatchTheSearchPrior(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	scores := make([]float32, 6)
	ys := make([]float64, 6)
	for i := range scores {
		scores[i] = float32(rng.NormFloat64() * 2)
		ys[i] = float64(scores[i])
	}
	cands := [][]int{{}, {0, 2}, {1, 2, 3, 5}, {4}}
	z := CandidateLogits(cands, ys)
	ref := make([]float64, len(cands))
	for c, ch := range cands {
		ref[c] = CandidateScore(true, scores, ch)
	}
	for c := 1; c < len(cands); c++ {
		if d := (z[c] - z[0]) - (ref[c] - ref[0]); math.Abs(d) > 1e-5 {
			t.Fatalf("candidate %d: logit gap %v, CandidateScore gap %v", c, z[c]-z[0], ref[c]-ref[0])
		}
	}
	single := [][]int{{1}, {4}}
	z = CandidateLogits(single, ys)
	for c, ch := range single {
		if z[c] != CandidateScore(false, scores, ch) {
			t.Fatalf("single candidate %d: %v vs %v", c, z[c], CandidateScore(false, scores, ch))
		}
	}
}

func fixtureRecord(ex Example) VisitRecord {
	rec := VisitRecord{
		RecordType: VisitRecordType, SchemaVersion: VisitSchemaVersion,
		EncoderHash: fmt.Sprintf("%016x", EncoderHashFor(FeaturesMZ)), World: "clairvoyant", Sims: 10,
		GameID: "m0000p0000g0", Deck: "Burn", Seed: 9, Kind: "priority", Turn: 5, Sequence: 40,
		State: EncodeOnPolicyState(ex.State), DiagRows: []uint16{2, 63, 0}, DiagVals: []float32{1, 1, 0.5},
		Cands: [][]int{{0}, {1}, {2}}, Visits: []int{3, 6, 1}, Prior: []float64{0.4, 0.3, 0.3},
		Q: []float64{0.4, 0.6, 0.2}, RootValue: 0.55, Choice: 1, Outcome: 1, OutcomeKnown: true,
	}
	for i := 0; i < 3; i++ {
		rec.Options = append(rec.Options, EncodeOnPolicyOption(ex.Options[i], true))
	}
	return rec
}

func TestVisitRecordExampleTargets(t *testing.T) {
	_, ex, _ := visitFixture(t, false, 11)
	rec := fixtureRecord(ex)
	got, err := rec.Example(VisitLabelTeacher, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := []float64{0.3, 0.6, 0.1}; !close3(got.Visits.Pi, want) || got.Visits.Teacher != 1 || got.TeacherChoice != 1 {
		t.Fatalf("teacher target %+v", got.Visits)
	}
	if !got.Options[1].Target.Preferred || got.Options[0].Target.Preferred || !got.Options[2].Target.Labelled {
		t.Fatal("teacher candidate's option must be the one Preferred; every option Labelled")
	}
	if got.TeacherValue != 0.55 || !got.HasTeacherValue || got.Outcome != 1 || got.Pair != "Burn" || got.Seed != 9 {
		t.Fatalf("metadata %+v", got)
	}
	if len(got.State.Sparse) != len(ex.State.Sparse) {
		t.Fatal("the redacted state must not carry the diagnostic rows")
	}
	bot, _ := rec.Example(VisitLabelBot, false, 1)
	if !close3(bot.Visits.Pi, []float64{1, 0, 0}) || bot.Visits.Teacher != 1 {
		t.Fatalf("bot label %+v", bot.Visits)
	}
	sharp, _ := rec.Example(VisitLabelTeacher, false, 0.5)
	// visits^2 = 9, 36, 1 -> /46
	if !close3(sharp.Visits.Pi, []float64{9.0 / 46, 36.0 / 46, 1.0 / 46}) {
		t.Fatalf("temperature 0.5 target %v", sharp.Visits.Pi)
	}
	diag, _ := rec.Example(VisitLabelTeacher, true, 1)
	if len(diag.State.Sparse) != len(ex.State.Sparse)+3 {
		t.Fatalf("diag state has %d rows, want %d", len(diag.State.Sparse), len(ex.State.Sparse)+3)
	}
	for i := 1; i < len(diag.State.Sparse); i++ {
		if diag.State.Sparse[i].Row < diag.State.Sparse[i-1].Row {
			t.Fatal("diag rows not merged in row order")
		}
	}
	bad := rec
	bad.Cands = [][]int{{0}, {7}, {2}}
	if _, err := bad.Example(VisitLabelTeacher, false, 1); err == nil {
		t.Fatal("an out-of-range candidate position must be refused")
	}
	bad = rec
	bad.Visits = []int{0, 0, 0}
	if _, err := bad.Example(VisitLabelTeacher, false, 1); err == nil {
		t.Fatal("an unvisited root must be refused")
	}
}

func close3(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-12 {
			return false
		}
	}
	return true
}

// LoadVisits reads plain and gzip corpora (concatenated gzip members, the
// botbench writer's shape), caps by games and refuses a foreign record.
func TestLoadVisits(t *testing.T) {
	_, ex, _ := visitFixture(t, false, 13)
	dir := t.TempDir()
	var plain bytes.Buffer
	var gz bytes.Buffer
	for g := 0; g < 3; g++ {
		var member bytes.Buffer
		zw := gzip.NewWriter(&member)
		for k := 0; k < 2; k++ {
			rec := fixtureRecord(ex)
			rec.GameID = fmt.Sprintf("m0000p%04dg0", g)
			raw, _ := json.Marshal(rec)
			plain.Write(append(raw, '\n'))
			zw.Write(append(raw, '\n'))
		}
		zw.Close()
		gz.Write(member.Bytes())
	}
	pp, gp := filepath.Join(dir, "v.jsonl"), filepath.Join(dir, "v.jsonl.gz")
	os.WriteFile(pp, plain.Bytes(), 0o644)
	os.WriteFile(gp, gz.Bytes(), 0o644)
	for _, p := range []string{pp, gp} {
		exs, recs, st, err := LoadVisits(p, VisitLoadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(exs) != 6 || len(recs) != 6 || st.Games != 3 || st.Features != FeaturesMZ || st.Overrides != 6 || st.World != "clairvoyant" {
			t.Fatalf("%s: %d examples, stats %+v", p, len(exs), st)
		}
		if recs[0].State.Dense != nil || recs[0].Options != nil {
			t.Fatal("the metadata records must drop the encoded payload")
		}
	}
	exs, _, st, err := LoadVisits(gp, VisitLoadOptions{MaxGames: 2, Diag: true})
	if err != nil || len(exs) != 4 || st.Games != 2 || st.Features != FeaturesMZOppHand {
		t.Fatalf("capped diag load: %d examples, %+v, %v", len(exs), st, err)
	}
	rec := fixtureRecord(ex)
	rec.RecordType = "onpolicy-v1"
	raw, _ := json.Marshal(rec)
	os.WriteFile(pp, raw, 0o644)
	if _, _, _, err := LoadVisits(pp, VisitLoadOptions{}); err == nil {
		t.Fatal("a foreign record type must be refused")
	}
}
