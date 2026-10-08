package v2engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

// parityGames numbers the games these parity tests play, so each driver's
// synthetic decision ids stay distinct. It is atomic because the three tests
// that play games now run in parallel (they share no server; only this
// counter is package state).
var parityGames atomic.Int64

// protocolGame plays one game on a Server through the wire with the two
// v2agent policies answering, and returns the terminal and the engine.
func protocolGame(t *testing.T, s *Server, deckID string, seed uint64, start string, pols [2]v2agent.Policy) (map[string]any, *Game) {
	t.Helper()
	s.seedOverride = &seed
	var agents [2]*v2agent.Agent
	gid := fmt.Sprintf("g-parity-%s-%d", deckID, seed)
	for i := range agents {
		a, err := v2agent.New(pols[i], v2agent.Options{Name: "t", Version: "1", SkipStrictCheck: true})
		if err != nil {
			t.Fatal(err)
		}
		agents[i] = a
		a.HandleLine([]byte(fmt.Sprintf(`{"request_type":"game_start","protocol":"spellbench/v2","request_id":"r-0","game_id":%q,"seat":"p%d","agent_seed":%d}`, gid, i, seed)))
	}
	pig := int(parityGames.Add(1))
	dr := &driver{t: t, s: s, kinds: map[string]int{}, n: pig * 1000000}
	resp := dr.send(map[string]any{"request_type": "reset", "game_id": gid, "format": Format,
		"seats": []any{
			map[string]any{"seat": "p0", "deck": map[string]any{"deck_id": "sha256:x", "catalog_id": deckID}},
			map[string]any{"seat": "p1", "deck": map[string]any{"deck_id": "sha256:x", "catalog_id": deckID}},
		},
		"rules": map[string]any{"opponent_decklist": "visible", "mulligan": "none", "starting_player": "host_assigned",
			"starting_seat": start, "card_name_domain": map[string]any{"domain_id": "sha256:x", "names": []any{}},
			"extensions": []any{}, "probe": false},
		"game_secret": strings.Repeat("cd", 32), "max_decisions": 20000, "max_steps": 60000})
	n := 0
	for resp["response_type"] == "decision" {
		sd := resp["seat_decision"].(map[string]any)
		seatIdx := 0
		if sd["acting_seat"] == "p1" {
			seatIdx = 1
		}
		n++
		req, _ := v2agent.CanonicalLine(map[string]any{"request_type": "choose", "protocol": v2agent.Protocol,
			"request_id": fmt.Sprintf("r-%d", n), "game_id": gid, "decision": sd,
			"clock": map[string]any{"remaining_ms": 1000, "max_decision_ms": 1000}})
		out := agents[seatIdx].HandleLine(req)
		var ans struct {
			Selection struct {
				CandidateID  int             `json:"candidate_id"`
				SemanticEcho json.RawMessage `json:"semantic_echo"`
			} `json:"selection"`
		}
		if err := json.NewDecoder(bytes.NewReader(out)).Decode(&ans); err != nil {
			t.Fatalf("agent answer %s: %v", out, err)
		}
		cand := sd["candidates"].([]any)[ans.Selection.CandidateID].(map[string]any)
		resp = dr.send(map[string]any{"request_type": "step", "game_id": gid, "expected_step": resp["step"],
			"selection": map[string]any{"candidate_id": ans.Selection.CandidateID, "semantic_echo": cand["semantic"]}})
	}
	return resp, s.game
}

// TestHeuristicParityWithInProcess: the python heuristic (v2agent's exact
// port) playing through this server must play the same game, intent for
// intent, as the in-process port of the same bot on gorge's manual
// surface (internal/spellbench/builtins, sb-heuristic-manual), given the
// same gorge seed and starting seat. Any divergence is a mapping choice of
// this package (doc.go), reported per deck.
func TestHeuristicParityWithInProcess(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	s := newTestServer(t, "manual")
	same, total := 0, 0
	for di, id := range spellbench.BenchmarkPool {
		deck, err := spellbench.Deck(reg, spellbench.PauperKernel, id)
		if err != nil {
			t.Fatal(err)
		}
		for k := 0; k < 2; k++ {
			seed := uint64(7000 + 10*di + k)
			cfg := rules.Config{Seed: seed, Names: []string{"p0", "p1"}, Decks: [][]*cards.Card{deck, deck},
				Tokens: reg.Tokens, NameUniverse: reg.Universe()}
			start := fmt.Sprintf("p%d", rules.New(cfg).G.StartingPlayer)
			seats := []seat.Seat{builtins.New(builtins.Heuristic, builtins.Manual, 1), builtins.New(builtins.Heuristic, builtins.Manual, 2)}
			o, e, err := gbench.PlayGame(cfg, seats, 0, 60000, gbench.Hooks{})
			if err != nil {
				t.Fatal(err)
			}
			term, g := protocolGame(t, s, id, seed, start, [2]v2agent.Policy{&v2agent.Heuristic{}, &v2agent.Heuristic{}})
			if term["classification"] == "halted" {
				t.Errorf("%s seed %d: protocol game halted: %v", id, seed, term["reason"])
				continue
			}
			inWinner := fmt.Sprintf("p%d_win", o.WinnerSeat)
			if o.Draw {
				inWinner = "draw"
			}
			match := term["outcome"] == inWinner && g.e.G.Turn == e.G.Turn && g.submits == o.Intents &&
				g.e.G.Players[0].Life == e.G.Players[0].Life && g.e.G.Players[1].Life == e.G.Players[1].Life
			total++
			if match {
				same++
			} else {
				pi := g.e.L.Intents
				if len(pi) == g.submits+1 {
					pi = pi[1:] // the engine-answered starting-player choice
				}
				t.Log(firstDivergence(cfg, e.L.Intents, pi))
			}
			t.Logf("%-9s seed %d start %s: in-process %s turns %d intents %d life %d/%d | protocol %v turns %d intents %d life %d/%d | same=%v",
				id, seed, start, inWinner, e.G.Turn, o.Intents, e.G.Players[0].Life, e.G.Players[1].Life,
				term["outcome"], g.e.G.Turn, g.submits, g.e.G.Players[0].Life, g.e.G.Players[1].Life, match)
		}
	}
	t.Logf("identical games: %d of %d", same, total)
	if same*2 < total {
		t.Errorf("only %d of %d heuristic games replay identically through the protocol", same, total)
	}
}

// firstDivergence replays the common prefix of two intent streams on a
// fresh engine and describes the decision where they part.
func firstDivergence(cfg rules.Config, a, b []decision.Intent) string {
	i := 0
	for i < len(a) && i < len(b) && fmt.Sprint(a[i].Choices, a[i].Rest, a[i].Payment != nil) == fmt.Sprint(b[i].Choices, b[i].Rest, b[i].Payment != nil) {
		i++
	}
	e := rules.New(cfg)
	e.Advance()
	for k := 0; k < i; k++ {
		in := a[k]
		if d := e.Pending(); d != nil {
			in.Seq = d.Seq
		}
		if err := e.Submit(in); err != nil {
			return fmt.Sprintf("replay failed at %d: %v", k, err)
		}
	}
	d := e.Pending()
	if d == nil {
		return fmt.Sprintf("streams part at intent %d with no pending decision", i)
	}
	var opts []string
	for _, o := range d.Options {
		opts = append(opts, fmt.Sprintf("%d:%s/%s(obj %d)", o.Index, o.Kind, o.Label, o.Obj))
	}
	ca, cb := "none", "none"
	if i < len(a) {
		ca = fmt.Sprint(a[i].Choices, a[i].Rest)
	}
	if i < len(b) {
		cb = fmt.Sprint(b[i].Choices, b[i].Rest)
	}
	return fmt.Sprintf("first divergence at intent %d (turn %d %s): %s p%d min %d max %d options %v: in-process %s, protocol %s",
		i, e.G.Turn, e.G.Step, d.Kind, d.Player, d.Min, d.Max, opts, ca, cb)
}
