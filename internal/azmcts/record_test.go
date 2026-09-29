package azmcts

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/seat"
)

// The visit record is the search re-expressed for a student: the same
// candidates (their options as positions in the stored list), the same
// visits, and -- the contract the distillation rests on -- a student whose
// candidate softmax, computed from the RECORD alone, is exactly the prior
// the search computed on the live engine.
func TestVisitRecordReproducesTheSearchPrior(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	m := policynet.NewModel(policynet.TableRows, 4, 3, rand.New(rand.NewPCG(21, 22)))
	m.InitValue(2, rand.New(rand.NewPCG(23, 24)))
	m.Features = policynet.FeaturesMZ
	for _, kind := range []decision.Kind{decision.KAttackers, decision.KPriority} {
		e, d, bot := botPosition(t, cfg, kind, 0, 3000)
		opts := DefaultOptions()
		opts.Sims, opts.Seed = 6, 3
		res := searchAt(t, e, d, bot, m, opts)
		checkResult(t, d, bot, res, 6)
		rec := visitRecord(e, d, res, policynet.FeaturesMZ, "clairvoyant", 6)
		if rec.EncoderHash != fmt.Sprintf("%016x", policynet.EncoderHashFor(policynet.FeaturesMZ)) || rec.Kind != kind ||
			rec.Subset != (kind == decision.KAttackers) || len(rec.Cands) != len(res.Candidates) {
			t.Fatalf("%s: record header %+v", kind, rec)
		}
		for c, in := range res.Candidates {
			if len(rec.Cands[c]) != len(in.Choices) {
				t.Fatalf("%s: candidate %d has %d stored options for %d choices", kind, c, len(rec.Cands[c]), len(in.Choices))
			}
		}
		ex, err := rec.Example(policynet.VisitLabelTeacher, false, 1)
		if err != nil {
			t.Fatal(err)
		}
		q := m.CandidateProbs(ex)
		for c := range q {
			if math.Abs(q[c]-res.Prior[c]) > 1e-5 {
				t.Fatalf("%s: student prior from the record %v, search prior %v", kind, q, res.Prior)
			}
		}
		if ex.Visits.Teacher != policynet.VisitTeacher(res.Visits) {
			t.Fatalf("%s: teacher %d", kind, ex.Visits.Teacher)
		}
	}
}

// Recording only reads: a recorded game replays the unrecorded one byte for
// byte, and records every searched decision.
func TestRecorderLeavesTheGameUnchanged(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	sc := DefaultSeatConfig()
	sc.Search.Sims = 3
	sc.Source = testSeatSource
	plain, err := NewSeat(cfg.Seed^1, nil, sc)
	if err != nil {
		t.Fatal(err)
	}
	want, err := playAZ(t, cfg, plain, 300)
	if err != nil {
		t.Fatal(err)
	}
	rec, _ := NewSeat(cfg.Seed^1, nil, sc)
	var n int
	rec.SetRecorder(func(r policynet.VisitRecord) {
		if r.World != "clairvoyant" || r.Sims != 3 || len(r.Cands) < 2 {
			t.Errorf("record %+v", r)
		}
		n++
	})
	got, err := playAZ(t, cfg, rec, 300)
	if err != nil {
		t.Fatal(err)
	}
	if got != want || n == 0 {
		t.Fatalf("recorded head %s (%d records), plain head %s", got, n, want)
	}
}

// The PriorOnly student with no network argmaxes a uniform prior, whose tie
// goes to candidate 0 -- the bot's answer -- so it is exactly the bot (the
// student arm's same-seed control). It never asks for a world: the Source
// NewSeat requires is never called.
func TestPriorOnlySeatWithoutANetworkIsTheBot(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	sc := DefaultSeatConfig()
	sc.PriorOnly = true
	sc.Source = testSeatSource
	az, err := NewSeat(cfg.Seed^1, nil, sc)
	if err != nil {
		t.Fatal(err)
	}
	azHead, err := playAZ(t, cfg, az, 400)
	if err != nil {
		t.Fatal(err)
	}
	botHead, err := playAZ(t, cfg, seat.NewBot(cfg.Seed^1), 400)
	if err != nil {
		t.Fatal(err)
	}
	if azHead != botHead {
		t.Fatalf("prior-only az head %s, bot head %s", azHead, botHead)
	}
}

// With a network the PriorOnly seat plays the prior's argmax: a network whose
// prior favours a non-bot candidate changes the game, deterministically.
func TestPriorOnlySeatFollowsItsNetwork(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	m := policynet.NewModel(policynet.TableRows, 4, 3, rand.New(rand.NewPCG(31, 32)))
	m.InitValue(2, rand.New(rand.NewPCG(33, 34)))
	m.Features = policynet.FeaturesMZ
	sc := DefaultSeatConfig()
	sc.PriorOnly = true
	sc.Source = testSeatSource
	var picks, nonBot int
	prev := Watch
	Watch = func(d Diag) {
		picks++
		if d.Choice != 0 {
			nonBot++
		}
	}
	t.Cleanup(func() { Watch = prev })
	heads := make([]string, 2)
	for i := range heads {
		az, err := NewSeat(cfg.Seed^1, m, sc)
		if err != nil {
			t.Fatal(err)
		}
		if heads[i], err = playAZ(t, cfg, az, 400); err != nil {
			t.Fatal(err)
		}
	}
	if heads[0] != heads[1] || picks == 0 || nonBot == 0 {
		t.Fatalf("heads %v, %d prior picks, %d off the bot", heads, picks, nonBot)
	}
}

// HeuristicLeaf uses the network as the prior only: the value head is never
// read, so perturbing it leaves the search byte-identical, while the same
// perturbation moves a network-leaf search.
func TestHeuristicLeafIgnoresTheValueHead(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KAttackers, 0, 3000)
	m := policynet.NewModel(policynet.TableRows, 4, 3, rand.New(rand.NewPCG(41, 42)))
	m.InitValue(2, rand.New(rand.NewPCG(43, 44)))
	m.Features = policynet.FeaturesMZ
	opts := DefaultOptions()
	opts.Sims, opts.Seed, opts.HeuristicLeaf = 8, 5, true
	a := searchAt(t, e, d, bot, m, opts)
	opts.HeuristicLeaf = false
	na := searchAt(t, e, d, bot, m, opts)
	m.VOutB += 3
	opts.HeuristicLeaf = true
	b := searchAt(t, e, d, bot, m, opts)
	opts.HeuristicLeaf = false
	nb := searchAt(t, e, d, bot, m, opts)
	if a.RootValue != b.RootValue || fmt.Sprint(a.Visits, a.Q) != fmt.Sprint(b.Visits, b.Q) {
		t.Fatalf("heuristic-leaf search moved with the value head: %v/%v vs %v/%v", a.Visits, a.Q, b.Visits, b.Q)
	}
	if na.RootValue == nb.RootValue {
		t.Fatal("a network-leaf search must read the value head")
	}
	if fmt.Sprint(a.Prior) != fmt.Sprint(na.Prior) {
		t.Fatal("the prior must be the network's in both modes")
	}
}
