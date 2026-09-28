package v2engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
)

// LocalGame is one game played in-process through the wire: this server
// answers reset and step, two v2agent.Agents answer game_start, choose and
// game_over, and every message is the canonical line either side would send
// over a pipe. It is a development harness (fast iteration, tests), not the
// reference host: there is no live validator and no clock.
type LocalGame struct {
	GameID string
	// Deck is the catalog id both seats play (the benchmark's mirrors), or
	// Decks names one per seat when set.
	Deck  string
	Decks [2]string
	// Secret seeds the game (spec 11.6's game_secret is derived from it).
	Secret uint64
	// Start is the seat taking the first turn ("p0" or "p1").
	Start string
	// AgentSeeds are the seats' agent_seed values.
	AgentSeeds [2]uint64
	// MaxDecisions and MaxSteps cap the game (defaults 10000, 100000).
	MaxDecisions, MaxSteps int64
	// OnChoose, when set, sees every choose request line before the
	// acting agent answers it (tests: shadow fidelity).
	OnChoose func(seat int, request []byte)
}

// LocalResult is a finished local game.
type LocalResult struct {
	Outcome        string
	Classification string
	Winner         string // "" for none
	Reason         string
	Decisions      [2]int
}

// catalogRows returns a catalog deck's decklist rows.
func (s *Server) catalogRows(id string) ([]any, bool) {
	for _, c := range s.catalog {
		if c.id == id {
			rows := make([]any, 0, len(c.rows))
			for _, r := range c.rows {
				rows = append(rows, r)
			}
			return rows, true
		}
	}
	return nil, false
}

// PlayLocal plays lg on s with agents[0] as p0 and agents[1] as p1.
func (s *Server) PlayLocal(lg LocalGame, agents [2]*v2agent.Agent) (LocalResult, error) {
	var res LocalResult
	decks := lg.Decks
	if decks[0] == "" {
		decks = [2]string{lg.Deck, lg.Deck}
	}
	if lg.MaxDecisions == 0 {
		lg.MaxDecisions = 10000
	}
	if lg.MaxSteps == 0 {
		lg.MaxSteps = 100000
	}
	if lg.Start == "" {
		lg.Start = "p0"
	}
	var rows [2][]any
	for i := range decks {
		r, ok := s.catalogRows(decks[i])
		if !ok {
			return res, fmt.Errorf("unknown catalog deck %q", decks[i])
		}
		rows[i] = r
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("v2engine-local:%d", lg.Secret)))
	secret := hex.EncodeToString(sum[:])
	names := map[string]bool{} // lookup only
	var domain []any
	for _, rs := range rows {
		for _, r := range rs {
			n := r.(map[string]any)["name"].(string)
			if !names[n] {
				names[n] = true
				domain = append(domain, n)
			}
		}
	}
	rulesMsg := map[string]any{"opponent_decklist": "visible", "mulligan": "none", "starting_player": "host_assigned",
		"starting_seat": lg.Start, "card_name_domain": map[string]any{"domain_id": "sha256:local", "names": domain},
		"extensions": []any{}, "probe": false}
	hello := s.hello()
	profile := map[string]any{"rules_supported": hello["rules_supported"], "observation": hello["observation"],
		"decision_kinds": hello["decision_kinds"], "engine_defaults": hello["engine_defaults"],
		"rewind": hello["rewind"], "fairness": hello["fairness"], "extensions": hello["extensions"]}
	for i := 0; i < 2; i++ {
		msg := map[string]any{"request_type": "game_start", "protocol": v2agent.Protocol,
			"request_id": fmt.Sprintf("gs-%d", i), "game_id": lg.GameID, "seat": fmt.Sprintf("p%d", i),
			"format":        Format,
			"own_deck":      map[string]any{"deck_id": "sha256:own", "name": decks[i], "decklist": rows[i]},
			"opponent_deck": map[string]any{"deck_id": "sha256:opp", "name": decks[1-i], "decklist": rows[1-i]},
			"rules":         rulesMsg, "engine": hello["engine"], "engine_profile": profile,
			"time_control": map[string]any{"startup_ms": 120000, "game_start_ms": 60000, "bank_ms": 600000,
				"increment_ms": 2000, "max_decision_ms": 60000, "engine_step_ms": 120000},
			"limits": map[string]any{"max_decisions": lg.MaxDecisions, "max_steps": lg.MaxSteps,
				"max_seat_decisions_per_turn": 500, "max_seat_decisions_per_game": 4999, "max_seat_steps_per_game": 49999},
			"resources":  map[string]any{"cpus": 1, "memory_mb": 4096, "gpu": false, "engine_cpus": 1},
			"agent_seed": lg.AgentSeeds[i]}
		line, err := v2agent.CanonicalLine(msg)
		if err != nil {
			return res, err
		}
		agents[i].HandleLine(line)
	}
	n := 0
	send := func(msg map[string]any) (map[string]any, error) {
		n++
		msg["protocol"] = v2agent.Protocol
		msg["request_id"] = fmt.Sprintf("%s-h-%d", lg.GameID, n)
		line, err := v2agent.CanonicalLine(msg)
		if err != nil {
			return nil, err
		}
		out := s.HandleLine(line)
		var resp map[string]any
		dec := json.NewDecoder(bytes.NewReader(out))
		dec.UseNumber()
		if err := dec.Decode(&resp); err != nil {
			return nil, fmt.Errorf("engine response is not JSON: %s", out)
		}
		if resp["response_type"] == "error" {
			return nil, fmt.Errorf("engine error: %s", out)
		}
		return resp, nil
	}
	resp, err := send(map[string]any{"request_type": "reset", "game_id": lg.GameID, "format": Format,
		"seats": []any{
			map[string]any{"seat": "p0", "deck": map[string]any{"deck_id": "sha256:x", "catalog_id": decks[0]}},
			map[string]any{"seat": "p1", "deck": map[string]any{"deck_id": "sha256:x", "catalog_id": decks[1]}},
		},
		"rules": rulesMsg, "game_secret": secret, "max_decisions": lg.MaxDecisions, "max_steps": lg.MaxSteps})
	if err != nil {
		return res, err
	}
	for resp["response_type"] == "decision" {
		sd, _ := resp["seat_decision"].(map[string]any)
		seat := 0
		if sd["acting_seat"] == "p1" {
			seat = 1
		}
		n++
		req, err := v2agent.CanonicalLine(map[string]any{"request_type": "choose", "protocol": v2agent.Protocol,
			"request_id": fmt.Sprintf("%s-c-%d", lg.GameID, n), "game_id": lg.GameID, "decision": sd,
			"clock": map[string]any{"remaining_ms": 600000, "max_decision_ms": 60000}})
		if err != nil {
			return res, err
		}
		if lg.OnChoose != nil {
			lg.OnChoose(seat, req)
		}
		out := agents[seat].HandleLine(req)
		res.Decisions[seat]++
		var ans struct {
			ResponseType string `json:"response_type"`
			Selection    struct {
				CandidateID int `json:"candidate_id"`
			} `json:"selection"`
		}
		if err := json.Unmarshal(out, &ans); err != nil || ans.ResponseType != "choice" {
			return res, fmt.Errorf("agent p%d answered %s", seat, out)
		}
		cands, _ := sd["candidates"].([]any)
		if ans.Selection.CandidateID < 0 || ans.Selection.CandidateID >= len(cands) {
			return res, fmt.Errorf("agent p%d chose candidate %d of %d", seat, ans.Selection.CandidateID, len(cands))
		}
		cand := cands[ans.Selection.CandidateID].(map[string]any)
		resp, err = send(map[string]any{"request_type": "step", "game_id": lg.GameID, "expected_step": resp["step"],
			"selection": map[string]any{"candidate_id": ans.Selection.CandidateID, "semantic_echo": cand["semantic"]}})
		if err != nil {
			return res, err
		}
	}
	term := resp
	if resp["response_type"] != "terminal" {
		return res, fmt.Errorf("engine ended without a terminal: %v", resp["response_type"])
	}
	res.Outcome, _ = term["outcome"].(string)
	res.Classification, _ = term["classification"].(string)
	res.Reason, _ = term["reason"].(string)
	if w, ok := term["winner"].(string); ok {
		res.Winner = w
	}
	for i := 0; i < 2; i++ {
		line, _ := v2agent.CanonicalLine(map[string]any{"request_type": "game_over", "protocol": v2agent.Protocol,
			"request_id": fmt.Sprintf("go-%d", i), "game_id": lg.GameID, "terminal": map[string]any{
				"outcome": res.Outcome, "classification": res.Classification, "winner": term["winner"],
				"reason": res.Reason, "seat_step_count": res.Decisions[i]}})
		agents[i].HandleLine(line)
	}
	return res, nil
}
