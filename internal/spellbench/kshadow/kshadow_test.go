package kshadow

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/spellbench/v1agent"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// fixture loads a recorded mtg-kernel decision (x_kernel_v5 included) and
// its mirror deck.
func fixture(t *testing.T, name string) (*Setup, *v1agent.Decision) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	raw, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Deck     string          `json:"deck"`
		Decision json.RawMessage `json:"decision"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	d, err := v1agent.ParseDecision(f.Decision)
	if err != nil || d.Kernel == nil {
		t.Fatalf("parse: %v", err)
	}
	s, err := NewSetup(reg, [2]string{f.Deck, f.Deck})
	if err != nil {
		t.Fatal(err)
	}
	return s, d
}

// TestStagedShadowMatchesObservation stages each fixture and checks the
// re-read shadow against the observation on every compared field.
func TestStagedShadowMatchesObservation(t *testing.T) {
	for name, want := range map[string]decision.Kind{
		"priority": decision.KPriority, "attack": decision.KAttackers, "block": decision.KBlockers,
	} {
		t.Run(name, func(t *testing.T) {
			s, d := fixture(t, name)
			sh := s.Build(&d.Kernel.Obs, Options{Seed: 7})
			if sh.Fatal != "" {
				t.Fatalf("fatal: %s (lossy %v)", sh.Fatal, sh.Lossy)
			}
			if k := sh.E.Pending().Kind; k != want {
				t.Fatalf("pending %s, want %s", k, want)
			}
			f := Compare(sh, &d.Kernel.Obs)
			for _, k := range f.Fields() {
				if f.Mismatch[k] > 0 {
					t.Errorf("%s: %d/%d mismatched (%s)", k, f.Mismatch[k], f.Checked[k], f.Examples[k])
				}
			}
		})
	}
}

// TestPriorityCandidatesMap checks that every non-mana kernel candidate at
// a staged priority decision has its gorge option.
func TestPriorityCandidatesMap(t *testing.T) {
	s, d := fixture(t, "priority")
	sh := s.Build(&d.Kernel.Obs, Options{Seed: 3})
	m := MapDecision(sh, d)
	for i, g := range m.Gorge {
		if !m.Mana[i] && g < 0 {
			t.Errorf("kernel candidate %d (%s) unmapped", i, d.Candidates[i].Kind())
		}
	}
	// And back: each mapped option maps to its own candidate.
	pd := sh.E.Pending()
	for i, g := range m.Gorge {
		if g < 0 || g >= PotentialBase {
			continue
		}
		in := decision.Intent{Seq: pd.Seq, Player: pd.Player, Choices: []int{g}}
		if g >= PaymentBase {
			a := pd.PaymentActions[g-PaymentBase]
			in = decision.Intent{Seq: pd.Seq, Player: pd.Player, Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: a.Plans[0]}}
		}
		k, why := kernelIndexPriority(sh, d, pd, in)
		if k != i {
			t.Errorf("option %d maps back to %d (%s), want %d", g, k, why, i)
		}
	}
}

// TestBuildIsDeterministic: the same observation and seed stage the same
// engine (event log head).
func TestBuildIsDeterministic(t *testing.T) {
	s, d := fixture(t, "priority")
	a := s.Build(&d.Kernel.Obs, Options{Seed: 11})
	b := s.Build(&d.Kernel.Obs, Options{Seed: 11})
	if a.E.L.Head() != b.E.L.Head() {
		t.Fatalf("heads differ: %s vs %s", a.E.L.Head(), b.E.L.Head())
	}
}

// TestRedealKeepsPublicAndSizes: a redealt world keeps every public zone,
// our hand, and every hidden zone's size, and moves only the opponent's
// hand/library and both library orders.
func TestRedealKeepsPublicAndSizes(t *testing.T) {
	s, d := fixture(t, "priority")
	sh := s.Build(&d.Kernel.Obs, Options{Seed: 5})
	w := sh.E.CloneHypothetical(99)
	Redeal(w, sh, rand.New(rand.NewPCG(1, 2)))
	for p := state.PlayerID(0); p < 2; p++ {
		for _, z := range []state.Zone{state.ZHand, state.ZLibrary, state.ZBattlefield, state.ZGraveyard, state.ZExile} {
			if len(w.G.Zone(z, p)) != len(sh.E.G.Zone(z, p)) {
				t.Errorf("seat %d zone %v size changed", p, z)
			}
		}
		for _, z := range []state.Zone{state.ZBattlefield, state.ZGraveyard, state.ZExile} {
			a, b := w.G.Zone(z, p), sh.E.G.Zone(z, p)
			for i := range a {
				if a[i] != b[i] {
					t.Errorf("seat %d public zone %v changed", p, z)
					break
				}
			}
		}
	}
	mine, orig := w.G.Zone(state.ZHand, sh.Me), sh.E.G.Zone(state.ZHand, sh.Me)
	for i := range mine {
		if mine[i] != orig[i] {
			t.Fatal("our hand changed")
		}
	}
	if pd := w.Pending(); pd == nil || pd.Seq != sh.E.Pending().Seq {
		t.Fatal("the world is not at the root decision")
	}
}

// TestPolicyModesAnswer drives every shadow mode through the three recorded
// decisions: each answer is in range, nothing panics, and every decision is
// counted under exactly one source.
func TestPolicyModesAnswer(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, mode := range []string{ModeTactical, ModeRoll, ModeAZ, ModeAZTac} {
		t.Run(mode, func(t *testing.T) {
			roll := DefaultRoll()
			roll.Worlds = 2
			p, err := New(Config{Reg: reg, Mode: mode, Sims: 4, Roll: roll, Seed: 1})
			if err != nil {
				t.Fatal(err)
			}
			p.GameStart(&v1agent.GameStart{GameID: "g", Seat: "p0", CatalogIDs: []string{"CawGates", "CawGates"}})
			n := 0
			for _, name := range []string{"priority", "attack", "block"} {
				_, d := fixture(t, name)
				p.seat = d.ActingSeat
				k := p.Choose(d)
				if k < 0 || k >= len(d.Candidates) {
					t.Fatalf("%s: answer %d of %d", name, k, len(d.Candidates))
				}
				n++
			}
			if p.Stats.Panics != 0 {
				t.Fatalf("%d panics (reasons %v)", p.Stats.Panics, p.Stats.Reasons)
			}
			total := 0
			for _, m := range p.Stats.Answered {
				for _, v := range m {
					total += v
				}
			}
			if total != n {
				t.Fatalf("answered %d, want %d", total, n)
			}
			if p.Stats.Answered[ClassPriority]["gorge"] != 1 {
				t.Errorf("priority not answered by the shadow: %v %v", p.Stats.Answered, p.Stats.Reasons)
			}
		})
	}
}
