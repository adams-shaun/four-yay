package v2agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Object references and semantics below are taken from the spec's examples
// (9.3's priority decision, 7.5's mulligan, scry and combat shapes).
const (
	refBolt     = `{"object_id":"o-1a7f3c9e5b2d4801","card_name":"Lightning Bolt","owner_seat":"p0","controller_seat":"p0","zone":"hand"}`
	refMountain = `{"object_id":"o-2b8e4dafc6031912","card_name":"Mountain","owner_seat":"p0","controller_seat":"p0","zone":"hand"}`
	refSwift    = `{"object_id":"o-4da06fc1e8253b34","card_name":"Monastery Swiftspear","owner_seat":"p0","controller_seat":"p0","zone":"battlefield"}`
	refSprite   = `{"object_id":"o-6fc281e30a475d56","card_name":"Spellstutter Sprite","owner_seat":"p1","controller_seat":"p1","zone":"battlefield"}`
	refPreord   = `{"object_id":"o-8c1d2e3f4a5b6c7d","card_name":"Preordain","owner_seat":"p0","controller_seat":"p0","zone":"stack"}`
	refIsland   = `{"object_id":"o-794a5cb152c9620f","card_name":"Island","owner_seat":"p0","controller_seat":"p0","zone":"library"}`
)

// decisionOf builds a Decision from semantics, candidate ids 0..n-1.
func decisionOf(t *testing.T, semantics ...string) *Decision {
	t.Helper()
	cands := make([]string, len(semantics))
	for i, s := range semantics {
		cands[i] = fmt.Sprintf(`{"candidate_id":%d,"semantic":%s,"display_text":null}`, i, s)
	}
	line := `{"request_type":"choose","protocol":"spellbench/v2","request_id":"r-1","game_id":"g","decision":{"seat_step":0,"candidates":[` + strings.Join(cands, ",") + `]}}`
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &top); err != nil {
		t.Fatalf("%v: %s", err, line)
	}
	a := &Agent{opts: Options{SkipStrictCheck: true}, logged: map[string]bool{}}
	d, err := a.readDecision(top)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestHeuristicAndFirstOnSpecDecisions pins both policies on hand-written
// decisions. The expected heuristic answers follow python/spellbench/
// builtins/heuristic.py's order (keep, land, cast, ability, attack, no-block,
// own seat first, else candidate 0); they were checked against that module.
func TestHeuristicAndFirstOnSpecDecisions(t *testing.T) {
	cases := []struct {
		name      string
		seat      string
		semantics []string
		heuristic int
	}{
		{"spec 9.3 priority: land before spell", "p0", []string{
			`{"kind":"pass"}`,
			`{"kind":"play_land","source":` + refMountain + `,"face":0}`,
			`{"kind":"cast_spell","source":` + refBolt + `,"method":"normal"}`}, 1},
		{"cast before any ability", "p0", []string{
			`{"kind":"pass"}`,
			`{"kind":"activate_mana_ability","source":` + refSwift + `,"ability_index":0,"mana_choice":"R","cost_target":null}`,
			`{"kind":"activate_ability","source":` + refSwift + `,"ability_index":0}`,
			`{"kind":"cast_spell","source":` + refBolt + `,"method":null}`}, 3},
		{"first ability of either class", "p0", []string{
			`{"kind":"pass"}`,
			`{"kind":"special_action","source":` + refSwift + `,"action":"plot"}`,
			`{"kind":"activate_ability","source":` + refSwift + `,"ability_index":1}`,
			`{"kind":"activate_mana_ability","source":` + refSwift + `,"ability_index":0,"mana_choice":null,"cost_target":null}`}, 2},
		{"spec 7.5 mulligan: keep", "p0", []string{
			`{"kind":"mulligan","hand_size":7,"mulligans_taken":1,"keep":false}`,
			`{"kind":"mulligan","hand_size":7,"mulligans_taken":1,"keep":true}`}, 1},
		{"mulligan bottom: no preference", "p0", []string{
			`{"kind":"order_pick","purpose":"mulligan_bottom","source":null,"item":{"object":` + refBolt + `},"position":0,"count":1}`,
			`{"kind":"order_pick","purpose":"mulligan_bottom","source":null,"item":{"object":` + refMountain + `},"position":0,"count":1}`}, 0},
		{"spec 7.5 scry: no preference", "p0", []string{
			`{"kind":"arrange_card","purpose":"scry","source":` + refPreord + `,"card":` + refIsland + `,"card_index":0,"card_count":2,"destination":"bottom"}`,
			`{"kind":"arrange_card","purpose":"scry","source":` + refPreord + `,"card":` + refIsland + `,"card_index":0,"card_count":2,"destination":"top"}`}, 0},
		{"every creature attacks", "p0", []string{
			`{"kind":"declare_attack","attacker":` + refSwift + `,"defender":null}`,
			`{"kind":"declare_attack","attacker":` + refSwift + `,"defender":{"player":"p1"}}`}, 1},
		{"block only when forced", "p1", []string{
			`{"kind":"declare_block","blocker":` + refSprite + `,"attacker":` + refSwift + `}`,
			`{"kind":"declare_block","blocker":` + refSprite + `,"attacker":null}`}, 1},
		{"forced block takes the first", "p1", []string{
			`{"kind":"declare_block","blocker":` + refSprite + `,"attacker":` + refSwift + `}`,
			`{"kind":"declare_block","blocker":` + refSprite + `,"attacker":` + refBolt + `}`}, 0},
		{"starting player: own seat (p1)", "p1", []string{
			`{"kind":"choose_starting_player","player":"p0"}`,
			`{"kind":"choose_starting_player","player":"p1"}`}, 1},
		{"starting player: own seat (p0)", "p0", []string{
			`{"kind":"choose_starting_player","player":"p0"}`,
			`{"kind":"choose_starting_player","player":"p1"}`}, 0},
		{"spec 7.5 X: no preference", "p0", []string{
			`{"kind":"choose_number","purpose":"x_value","source":` + refPreord + `,"value":0,"minimum":0,"maximum":2}`,
			`{"kind":"choose_number","purpose":"x_value","source":` + refPreord + `,"value":2,"minimum":0,"maximum":2}`}, 0},
		{"unless payment: no preference", "p0", []string{
			`{"kind":"optional_cost","source":` + refPreord + `,"cost":"unless_payment","pay":false}`,
			`{"kind":"activate_mana_ability","source":` + refSwift + `,"ability_index":0,"mana_choice":"R","cost_target":null}`}, 1},
		{"lenient: keep must be the literal true", "p0", []string{
			`{"kind":"mulligan","keep":1}`,
			`{"kind":"mulligan","keep":"true"}`,
			`{"kind":"mulligan","keep":true}`}, 2},
		{"lenient: a missing defender is null, any value is not", "p0", []string{
			`{"kind":"declare_attack"}`,
			`{"kind":"declare_attack","defender":"p1"}`}, 1},
		{"lenient: a non-object semantic and a non-string kind rank last", "p0", []string{
			`7`,
			`{"kind":["play_land"]}`,
			`{"kind":"choose_boolean","value":true}`}, 0},
	}
	for _, c := range cases {
		d := decisionOf(t, c.semantics...)
		h := &Heuristic{}
		h.GameStart(&GameStart{Seat: c.seat, seatIsString: true})
		got, err := h.Choose(d)
		if err != nil || got != c.heuristic {
			t.Errorf("%s: heuristic chose %d (%v), want %d", c.name, got, err, c.heuristic)
		}
		if got, _ := (First{}).Choose(d); got != 0 {
			t.Errorf("%s: first chose %d", c.name, got)
		}
	}
}

// TestHeuristicWithoutSeat: a game_start whose seat is not a string never
// prefers a starting player (python's seat None).
func TestHeuristicWithoutSeat(t *testing.T) {
	d := decisionOf(t, `{"kind":"choose_starting_player","player":"p0"}`, `{"kind":"choose_starting_player","player":"p1"}`)
	h := &Heuristic{}
	h.GameStart(&GameStart{})
	if got, _ := h.Choose(d); got != 0 {
		t.Fatalf("chose %d", got)
	}
}

// TestSplitMix64MatchesPython: the python uniform builtin's generator.
func TestSplitMix64MatchesPython(t *testing.T) {
	s := NewSplitMix64(0)
	for i, want := range []uint64{0xe220a8397b1dcdaf, 0x6e789e6aa1b965f4, 0x06c45d188009454f} {
		if got := s.Next(); got != want {
			t.Fatalf("next #%d = %#x, want %#x", i, got, want)
		}
	}
}

// TestRandomIsSeededFromAgentSeed: the random policy's picks are a pure
// function of agent_seed XOR Seed (python: SplitMix64(8103969398531465)
// drawn mod 7 gives 2,3,4,0,3,4,3,4,3,5).
func TestRandomIsSeededFromAgentSeed(t *testing.T) {
	sem := make([]string, 7)
	for i := range sem {
		sem[i] = fmt.Sprintf(`{"kind":"choose_number","purpose":"amount","source":null,"value":%d,"minimum":0,"maximum":6}`, i)
	}
	d := decisionOf(t, sem...)
	picks := func(agentSeed string, seed uint64) []int {
		r := &Random{Seed: seed}
		r.GameStart(&GameStart{AgentSeed: json.RawMessage(agentSeed)})
		out := make([]int, 10)
		for i := range out {
			out[i], _ = r.Choose(d)
		}
		return out
	}
	if got, want := fmt.Sprint(picks("8103969398531465", 0)), "[2 3 4 0 3 4 3 4 3 5]"; got != want {
		t.Fatalf("picks %s, want %s", got, want)
	}
	if fmt.Sprint(picks("8103969398531465", 0)) != fmt.Sprint(picks("8103969398531464", 1)) {
		t.Fatal("Seed is not XORed into agent_seed")
	}
	if fmt.Sprint(picks("null", 0)) != fmt.Sprint(picks("0", 0)) || fmt.Sprint(picks("1.5", 0)) != fmt.Sprint(picks("0", 0)) {
		t.Fatal("a missing or non-integer agent_seed must read as 0")
	}
	if fmt.Sprint(picks("-1", 0)) != fmt.Sprint(picks("18446744073709551615", 0)) {
		t.Fatal("a negative agent_seed must reduce mod 2^64 as python's & MASK64 does")
	}
}

// panicky is a policy that fails in every hook.
type panicky struct{ First }

func (panicky) Choose(*Decision) (int, error) { panic("boom") }

// TestPolicyFailuresAnswerTheFallback: a panicking policy no longer answers
// internal_error (a forfeit, spec 10.5): the fallback policy's legal choice
// answers, and the agent keeps serving.
func TestPolicyFailuresAnswerTheFallback(t *testing.T) {
	a, err := New(panicky{}, Options{Name: "t", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	start := `{"request_type":"game_start","protocol":"spellbench/v2","request_id":"r-1","game_id":"g","seat":"p0","agent_seed":1}`
	choose := `{"request_type":"choose","protocol":"spellbench/v2","request_id":"r-2","game_id":"g","decision":{"candidates":[{"candidate_id":0,"semantic":{"kind":"pass"}}]}}`
	over := `{"request_type":"game_over","protocol":"spellbench/v2","request_id":"r-3","game_id":"g","terminal":{"outcome":"draw","classification":"natural","winner":null,"reason":"x","seat_step_count":1}}`
	for _, step := range []struct{ line, want string }{
		{start, `"response_type":"ack"`},
		{choose, `"selection":{"candidate_id":0`},
		{over, `"response_type":"ack"`},
	} {
		if out := string(a.HandleLine([]byte(step.line))); !strings.Contains(out, step.want) {
			t.Errorf("%s -> %s, want %s", step.line[:40], out, step.want)
		}
	}
}

// TestUnknownAndMistypedFieldsFailSoft: an observation with a field the
// structs do not know and a field of the wrong type is still answered (spec
// 4.2); both are reported once on the diagnostics writer.
func TestUnknownAndMistypedFieldsFailSoft(t *testing.T) {
	var log strings.Builder
	a, err := New(&Heuristic{}, Options{Name: "t", Version: "1", Log: &log})
	if err != nil {
		t.Fatal(err)
	}
	start := `{"request_type":"game_start","protocol":"spellbench/v2","request_id":"r-1","game_id":"g","seat":"p0","agent_seed":1,"x_new":{}}`
	choose := `{"request_type":"choose","protocol":"spellbench/v2","request_id":"r-2","game_id":"g","decision":{"seat_step":3,` +
		`"observation":{"viewer":"p0","turn":"five","phase_step":"upkeep","future_field":[1],"players":[{"seat":"p0","life":20}]},` +
		`"candidates":[{"candidate_id":0,"semantic":{"kind":"pass"}},{"candidate_id":1,"semantic":{"kind":"play_land","source":null,"face":0,"x_hint":1}}]}}`
	for i := 0; i < 2; i++ {
		a.HandleLine([]byte(strings.Replace(start, `"r-1"`, fmt.Sprintf(`"r-1%d"`, i), 1)))
		out := string(a.HandleLine([]byte(choose)))
		if !strings.Contains(out, `"candidate_id":1`) {
			t.Fatalf("not answered: %s", out)
		}
		a.HandleLine([]byte(`{"request_type":"game_over","protocol":"spellbench/v2","request_id":"r-3","game_id":"g","terminal":{}}`))
	}
	if a.Observation == nil || a.Observation.Viewer != "p0" || a.Observation.PhaseStep != "upkeep" || a.Observation.Me().Life != 20 {
		t.Fatalf("observation not kept past the bad fields: %+v", a.Observation)
	}
	got := log.String()
	for _, want := range []string{"$.observation.future_field", "$.x_hint", "$.x_new", "observation.turn"} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostics lack %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "future_field"); n != 1 {
		t.Errorf("future_field reported %d times, want once", n)
	}
}
