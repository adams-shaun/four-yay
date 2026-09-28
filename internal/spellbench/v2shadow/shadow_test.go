package v2shadow_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
	"github.com/adams-shaun/gorge/internal/spellbench/v2engine"
	"github.com/adams-shaun/gorge/internal/spellbench/v2shadow"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// playLocal plays one in-process protocol game and returns the result and
// the two policies' stats (nil for a non-shadow seat).
func playLocal(t *testing.T, srv *v2engine.Server, deck string, secret uint64, pols [2]v2agent.Policy, onChoose func(int, []byte)) (v2engine.LocalResult, [2]*v2agent.Agent) {
	t.Helper()
	var agents [2]*v2agent.Agent
	for i := range agents {
		a, err := v2agent.New(pols[i], v2agent.Options{Name: "t", Version: "1", SkipStrictCheck: true})
		if err != nil {
			t.Fatal(err)
		}
		agents[i] = a
	}
	res, err := srv.PlayLocal(v2engine.LocalGame{GameID: fmt.Sprintf("t-%s-%d", deck, secret), Deck: deck, Secret: secret,
		Start: fmt.Sprintf("p%d", secret%2), AgentSeeds: [2]uint64{secret*7 + 1, secret*7 + 2}, OnChoose: onChoose}, agents)
	if err != nil {
		t.Fatalf("%s/%d: %v", deck, secret, err)
	}
	return res, agents
}

func shadowPolicy(t *testing.T, name string) *v2shadow.Policy {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cfg, err := v2shadow.NamedConfig(name, reg)
	if err != nil {
		t.Fatal(err)
	}
	p, err := v2shadow.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestShadowPlaysWholeGames: sb-tactical on the shadow plays full protocol
// games against the heuristic on every pool deck without a panic, a policy
// failure or an error frame, and answers nearly every decision itself.
func TestShadowPlaysWholeGames(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	srv, err := v2engine.New(v2engine.Options{Registry: reg, Mana: "manual", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	decisions, fallbacks, wins := 0, 0, 0
	for i, deck := range []string{"Burn", "Faeries", "Elves", "Affinity"} {
		sp := shadowPolicy(t, "tactical")
		res, agents := playLocal(t, srv, deck, uint64(4100+i), [2]v2agent.Policy{sp, &v2agent.Heuristic{}}, nil)
		if res.Classification != "natural" {
			t.Errorf("%s: %s %s", deck, res.Classification, res.Reason)
		}
		if sp.Stats.Panics != 0 || agents[0].Stats.PolicyFallbacks != 0 || len(agents[0].Stats.Errors) != 0 {
			t.Errorf("%s: panics %d, agent fallbacks %d, errors %v", deck, sp.Stats.Panics, agents[0].Stats.PolicyFallbacks, agents[0].Stats.Errors)
		}
		decisions += sp.Stats.Decisions
		fallbacks += sp.Stats.Fallbacks()
		if res.Winner == "p0" {
			wins++
		}
	}
	t.Logf("shadow-tactical won %d of 4 against the heuristic; %d of %d decisions fell back", wins, fallbacks, decisions)
	if decisions == 0 || fallbacks*20 > decisions {
		t.Errorf("fallbacks %d of %d decisions (more than 5%%)", fallbacks, decisions)
	}
	if wins < 3 {
		t.Errorf("shadow-tactical won only %d of 4 against the heuristic", wins)
	}
}

// TestShadowSearchIsDeterministic: without a clock guard, the searching
// shadow policy plays the same game for the same seeds.
func TestShadowSearchIsDeterministic(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	run := func() (string, int) {
		srv, err := v2engine.New(v2engine.Options{Registry: reg, Mana: "manual", Version: "test"})
		if err != nil {
			t.Fatal(err)
		}
		sp := shadowPolicy(t, "search-w2-h1")
		res, _ := playLocal(t, srv, "Burn", 4200, [2]v2agent.Policy{sp, shadowPolicy(t, "tactical")}, nil)
		return fmt.Sprintf("%s %s %v", res.Outcome, res.Reason, res.Decisions), sp.Stats.Searched
	}
	a, na := run()
	b, nb := run()
	if a != b || na != nb {
		t.Fatalf("searching shadow differs between runs: %q (%d searched) vs %q (%d)", a, na, b, nb)
	}
	if na == 0 {
		t.Fatalf("no decision was searched")
	}
}

// TestStagingMatchesObservation: at every priority decision of a game the
// staged shadow reproduces the observation's public state (life, zone
// sizes, battlefield and hand names, tapped state, the stack's size).
func TestStagingMatchesObservation(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	srv, err := v2engine.New(v2engine.Options{Registry: reg, Mana: "manual", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	var setups [2]*v2shadow.Setup
	var trackers [2]*v2shadow.Tracker
	seats := [2]*recHeuristic{{}, {}}
	checked, mismatches := 0, 0
	onChoose := func(seat int, req []byte) {
		var msg struct {
			GameID   string               `json:"game_id"`
			Decision v2agent.SeatDecision `json:"decision"`
		}
		if json.Unmarshal(req, &msg) != nil || msg.Decision.Context.Kind != "priority" {
			return
		}
		obs := &msg.Decision.Observation
		if setups[seat] == nil {
			s, err := v2shadow.NewSetup(reg, seats[seat].gs)
			if err != nil {
				t.Fatal(err)
			}
			setups[seat], trackers[seat] = s, v2shadow.NewTracker()
		}
		sh := setups[seat].Build(obs, trackers[seat], v2shadow.Options{Seed: uint64(msg.Decision.SeatStep), Priority: true})
		if sh.Fatal != "" {
			return
		}
		checked++
		if d := diffShadow(sh, obs); d != "" {
			mismatches++
			if mismatches <= 5 {
				t.Logf("seat %d step %d: %s", seat, msg.Decision.SeatStep, d)
			}
		}
	}
	playLocal(t, srv, "Faeries", 4300, [2]v2agent.Policy{seats[0], seats[1]}, onChoose)
	t.Logf("checked %d staged priority decisions, %d mismatched", checked, mismatches)
	if checked == 0 || mismatches*50 > checked {
		t.Errorf("%d of %d staged decisions differ from their observation", mismatches, checked)
	}
}

// recHeuristic is the heuristic, remembering its game_start.
type recHeuristic struct {
	v2agent.Heuristic
	gs *v2agent.GameStart
}

func (r *recHeuristic) GameStart(g *v2agent.GameStart) { r.gs = g; r.Heuristic.GameStart(g) }

func diffShadow(sh *v2shadow.Shadow, obs *v2agent.Observation) string {
	g := sh.E.G
	for i := range obs.Players {
		p := &obs.Players[i]
		pid := state.PlayerID(0)
		if p.Seat == "p1" {
			pid = 1
		}
		if g.Players[pid].Life != p.Life {
			return fmt.Sprintf("%s life %d vs %d", p.Seat, g.Players[pid].Life, p.Life)
		}
		if n := len(g.Zone(state.ZHand, pid)); n != int(p.HandCount) {
			return fmt.Sprintf("%s hand %d vs %d", p.Seat, n, p.HandCount)
		}
		if n := len(g.Zone(state.ZLibrary, pid)); n != int(p.LibraryCount) {
			return fmt.Sprintf("%s library %d vs %d", p.Seat, n, p.LibraryCount)
		}
		want := map[string]int{}
		for _, r := range p.Battlefield {
			if r.CardName != nil {
				k := *r.CardName
				if r.Permanent != nil && r.Permanent.Tapped {
					k += " (tapped)"
				}
				want[k]++
			}
		}
		got := map[string]int{}
		for _, id := range g.Zone(state.ZBattlefield, pid) {
			o := g.Obj(id)
			if o == nil || o.Face() == nil || o.Controller != pid {
				continue
			}
			k := sh.E.Name(id)
			if k == "" {
				k = o.Face().Name
			}
			if o.Tapped {
				k += " (tapped)"
			}
			got[k]++
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			return fmt.Sprintf("%s battlefield %v vs %v", p.Seat, got, want)
		}
	}
	if len(g.Stack) != len(obs.Stack) {
		return fmt.Sprintf("stack %d vs %d", len(g.Stack), len(obs.Stack))
	}
	return ""
}
