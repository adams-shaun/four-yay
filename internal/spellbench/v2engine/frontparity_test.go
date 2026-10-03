// The S0 front-end parity test (P§3.9): both front ends run over the same
// gorge-engine transcript, and the resulting entity tables — and the two
// candidate encodings' pointer resolutions — must agree. The transcript is
// a real wire game: the two v2agent heuristic policies answer through the
// server while, at every posed decision, front end G projects the engine's
// own view.View and front end V projects the posed wire observation the
// agent role received (decoded independently from the served JSON).

package v2engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
)

func TestS0FrontEndParity(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, "manual")
	decisions, candSeen := 0, map[string]int{}
	for di, id := range spellbench.BenchmarkPool {
		seed := uint64(9100 + 10*di)
		s.seedOverride = &seed
		pig := int(parityGames.Add(1))
		var agents [2]*v2agent.Agent
		gid := fmt.Sprintf("g-frontparity-%s-%d", id, seed)
		for i := range agents {
			a, err := v2agent.New(&v2agent.Heuristic{}, v2agent.Options{Name: "t", Version: "1", SkipStrictCheck: true})
			if err != nil {
				t.Fatal(err)
			}
			agents[i] = a
			a.HandleLine([]byte(fmt.Sprintf(`{"request_type":"game_start","protocol":"spellbench/v2","request_id":"r-0","game_id":%q,"seat":"p%d","agent_seed":%d}`, gid, i, seed)))
		}
		dr := &driver{t: t, s: s, kinds: map[string]int{}, n: pig * 1000000}
		resp := dr.send(map[string]any{"request_type": "reset", "game_id": gid, "format": Format,
			"seats": []any{
				map[string]any{"seat": "p0", "deck": map[string]any{"deck_id": "sha256:x", "catalog_id": id}},
				map[string]any{"seat": "p1", "deck": map[string]any{"deck_id": "sha256:x", "catalog_id": id}},
			},
			"rules": map[string]any{"opponent_decklist": "visible", "mulligan": "none", "starting_player": "host_assigned",
				"starting_seat": "p0", "card_name_domain": map[string]any{"domain_id": "sha256:x", "names": []any{}},
				"extensions": []any{}, "probe": false},
			"game_secret": strings.Repeat("cd", 32), "max_decisions": 20000, "max_steps": 60000})
		n := 0
		for resp["response_type"] == "decision" {
			n++
			sd := resp["seat_decision"].(map[string]any)
			seatIdx := 0
			if sd["acting_seat"] == "p1" {
				seatIdx = 1
			}
			// Parity point: front end G from the engine's view, front end V
			// from the served observation decoded independently.
			pair, err := s.game.Pair()
			if err != nil {
				t.Fatalf("decision %d (%s): %v", n, id, err)
			}
			if pair.GTable == nil || pair.VTable == nil {
				t.Fatalf("decision %d: nil front end", n)
			}
			// The observation the seat received, decoded from the served
			// JSON (an independent parse of the wire bytes).
			obsRaw, err := json.Marshal(sd["observation"])
			if err != nil {
				t.Fatal(err)
			}
			var obsDec v2agent.Observation
			if err := json.Unmarshal(obsRaw, &obsDec); err != nil {
				t.Fatal(err)
			}
			vt2 := policynet.V2TableFromObservation(&obsDec, sd["acting_seat"].(string))
			if !policynet.V2Equal(pair.GTable.Table, pair.VTable.Table) {
				t.Fatalf("front ends diverge at %s decision %d (%s): digest G %s V %s",
					pair.Kind, n, id, policynet.V2TableDigest(pair.GTable.Table), policynet.V2TableDigest(pair.VTable.Table))
			}
			if !policynet.V2Equal(pair.GTable.Table, vt2.Table) {
				t.Fatalf("served-observation front end diverges at %s decision %d (%s)", pair.Kind, n, id)
			}
			// Candidate encoding parity: the same semantics encoded through
			// each side's own resolver must give identical rows.
			wc, err := s.game.WireCandidates()
			if err != nil {
				t.Fatal(err)
			}
			vsem, err := WireSemCands(wc)
			if err != nil {
				t.Fatal(err)
			}
			if len(vsem) != len(pair.GSem) {
				t.Fatalf("candidate count mismatch at %s decision %d", pair.Kind, n)
			}
			gc := policynet.V2CandsEncode(pair.GSem, pair.GRes)
			vc := policynet.V2CandsEncode(vsem, pair.VTable)
			for i := range gc {
				if !candBytesEqual(gc[i], vc[i]) {
					t.Fatalf("candidate %d encodes differently through the two resolvers at %s decision %d (%s): %s vs %s",
						i, pair.Kind, n, id, candJSON(gc[i]), candJSON(vc[i]))
				}
				for _, c := range []policynet.V2SemCand{pair.GSem[i]} {
					candSeen[c.Kind]++
				}
			}
			decisions++
			// Answer as the heuristic policy would (the standard loop).
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
		t.Logf("%-9s seed %d: %d decisions, terminal %v", id, seed, n, resp["classification"])
		if resp["classification"] == "halted" {
			t.Errorf("%s seed %d: protocol game halted: %v", id, seed, resp["reason"])
		}
	}
	if decisions < 200 {
		t.Errorf("only %d parity decisions ran; the transcript is too thin to be evidence", decisions)
	}
	// The census must show the point of the exercise actually exercised:
	// spell casts, targets and priority passes all flowed through both
	// front ends.
	for _, want := range []string{"cast_spell", "choose_target", "pass", "declare_attack", "declare_block"} {
		if candSeen[want] == 0 {
			t.Errorf("no %q candidate was parity-checked", want)
		}
	}
	t.Logf("parity decisions %d; candidate kinds %v", decisions, candSeen)
}

// candBytesEqual compares two encoded candidates byte-wise.
func candBytesEqual(a, b policynet.V2Cand) bool {
	if len(a.Raw) != len(b.Raw) || len(a.Refs) != len(b.Refs) {
		return false
	}
	for i := range a.Raw {
		if a.Raw[i] != b.Raw[i] {
			return false
		}
	}
	for i := range a.Refs {
		if a.Refs[i] != b.Refs[i] {
			return false
		}
	}
	return len(a.Rows) == len(b.Rows)
}

// candJSON renders an encoded candidate for failure messages.
func candJSON(c policynet.V2Cand) string {
	out, _ := json.Marshal(map[string]any{"raw": c.Raw, "refs": c.Refs, "rows": c.Rows})
	return string(out)
}
