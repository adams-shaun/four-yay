package v2engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// driver plays one game against a Server in-process, checking the parts of
// the host's live validator that need no python: envelope, dense ids, pass
// first, distinct semantics, seat_step/group counters, every object
// reference equal to an observation record, and no library record.
type driver struct {
	t      *testing.T
	s      *Server
	n      int
	seq    [2]int64
	group  [2]int64
	sub    [2]int64
	gcount [2]int64
	steps  int64
	groups int64
	kinds  map[string]int
}

func (dr *driver) send(msg map[string]any) map[string]any {
	dr.t.Helper()
	dr.n++
	msg["protocol"] = v2agent.Protocol
	msg["request_id"] = fmt.Sprintf("h-%d", dr.n)
	line, err := v2agent.Canonical(msg)
	if err != nil {
		dr.t.Fatal(err)
	}
	out := dr.s.HandleLine(line)
	var resp map[string]any
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber()
	if err := dec.Decode(&resp); err != nil {
		dr.t.Fatalf("response is not JSON: %s", out)
	}
	return resp
}

func num(v any) int64 {
	n, _ := asInt(v)
	return n
}

func (dr *driver) check(resp map[string]any) (sd map[string]any) {
	t := dr.t
	t.Helper()
	sd = resp["seat_decision"].(map[string]any)
	seat := 0
	if sd["acting_seat"] == "p1" {
		seat = 1
	}
	if num(sd["seat_step"]) != dr.seq[seat] {
		t.Fatalf("seat_step %v, want %d", sd["seat_step"], dr.seq[seat])
	}
	g := sd["group"].(map[string]any)
	gid, si, sc := num(g["group_id"]), num(g["substep_index"]), num(g["substep_count"])
	if si == 0 {
		if gid != dr.group[seat] {
			t.Fatalf("group_id %d, want %d", gid, dr.group[seat])
		}
	} else if gid != dr.group[seat] || si != dr.sub[seat]+1 || sc != dr.gcount[seat] {
		t.Fatalf("group %d/%d/%d does not continue %d/%d/%d", gid, si, sc, dr.group[seat], dr.sub[seat], dr.gcount[seat])
	}
	dr.sub[seat], dr.gcount[seat] = si, sc
	obs := sd["observation"].(map[string]any)
	if obs["viewer"] != sd["acting_seat"] {
		t.Fatalf("viewer %v != acting seat %v", obs["viewer"], sd["acting_seat"])
	}
	held := map[string]string{}
	hold := func(ref map[string]any) {
		raw, _ := v2agent.Canonical(map[string]any{"object_id": ref["object_id"], "card_name": ref["card_name"],
			"owner_seat": ref["owner_seat"], "controller_seat": ref["controller_seat"], "zone": ref["zone"]})
		id := ref["object_id"].(string)
		if _, dup := held[id]; dup {
			t.Fatalf("object id %s held twice", id)
		}
		held[id] = string(raw)
	}
	for pi, pv := range obs["players"].([]any) {
		p := pv.(map[string]any)
		for _, z := range []string{"hand", "battlefield", "graveyard", "exile", "command"} {
			if p[z] == nil {
				if z != "hand" || pi == seat {
					t.Fatalf("players[%d].%s is null", pi, z)
				}
				continue
			}
			if z == "hand" && pi != seat {
				t.Fatalf("the other seat's hand is visible")
			}
			for _, r := range p[z].([]any) {
				rec := r.(map[string]any)
				if rec["zone"] != z {
					t.Fatalf("record in %s has zone %v", z, rec["zone"])
				}
				hold(rec)
			}
		}
		if pi == seat && int64(len(p["hand"].([]any))) != num(p["hand_count"]) {
			t.Fatalf("hand has %d records, hand_count %v", len(p["hand"].([]any)), p["hand_count"])
		}
	}
	for _, e := range obs["stack"].([]any) {
		hold(e.(map[string]any))
	}
	for _, k := range obs["known"].([]any) {
		ke := k.(map[string]any)
		if ke["object_id"] != nil {
			hold(map[string]any{"object_id": ke["object_id"], "card_name": ke["card_name"], "owner_seat": ke["owner_seat"],
				"controller_seat": ke["owner_seat"], "zone": ke["zone"]})
		}
	}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if id, ok := x["object_id"].(string); ok && len(x) == 5 {
				raw, _ := v2agent.Canonical(x)
				if held[id] != string(raw) {
					t.Fatalf("reference %s does not equal the observation record %s", raw, held[id])
				}
				return
			}
			for _, e := range x {
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	cands := sd["candidates"].([]any)
	if len(cands) == 0 {
		t.Fatal("no candidates")
	}
	seen := map[string]bool{}
	for i, c := range cands {
		cm := c.(map[string]any)
		if num(cm["candidate_id"]) != int64(i) {
			t.Fatalf("candidate_id %v at %d", cm["candidate_id"], i)
		}
		sem := cm["semantic"].(map[string]any)
		raw, _ := v2agent.Canonical(sem)
		if seen[string(raw)] {
			t.Fatalf("duplicate semantic %s", raw)
		}
		seen[string(raw)] = true
		if sem["kind"] == "pass" && i != 0 {
			t.Fatalf("pass at %d", i)
		}
		dr.kinds[sem["kind"].(string)]++
		walk(sem)
	}
	if ctx := sd["context"].(map[string]any); ctx["source"] != nil {
		walk(ctx["source"])
	}
	return sd
}

func (dr *driver) answered(sd map[string]any) {
	seat := 0
	if sd["acting_seat"] == "p1" {
		seat = 1
	}
	dr.seq[seat]++
	dr.steps++
	if dr.sub[seat]+1 == dr.gcount[seat] {
		dr.group[seat]++
		dr.groups++
		dr.sub[seat], dr.gcount[seat] = 0, 0
	}
}

// play runs one game with pick choosing each candidate index.
func (dr *driver) play(gameID string, deckID string, secret string, pick func(sd map[string]any) int) map[string]any {
	t := dr.t
	resp := dr.send(map[string]any{"request_type": "reset", "game_id": gameID, "format": Format,
		"seats": []any{
			map[string]any{"seat": "p0", "deck": map[string]any{"deck_id": "sha256:x", "catalog_id": deckID}},
			map[string]any{"seat": "p1", "deck": map[string]any{"deck_id": "sha256:x", "catalog_id": deckID}},
		},
		"rules": map[string]any{"opponent_decklist": "visible", "mulligan": "none", "starting_player": "host_assigned",
			"starting_seat": "p0", "card_name_domain": map[string]any{"domain_id": "sha256:x", "names": []any{}},
			"extensions": []any{}, "probe": false},
		"game_secret": secret, "max_decisions": 3000, "max_steps": 6000})
	for resp["response_type"] == "decision" {
		sd := dr.check(resp)
		i := pick(sd)
		cand := sd["candidates"].([]any)[i].(map[string]any)
		dr.answered(sd)
		resp = dr.send(map[string]any{"request_type": "step", "game_id": gameID, "expected_step": resp["step"],
			"selection": map[string]any{"candidate_id": i, "semantic_echo": cand["semantic"]}})
	}
	if resp["response_type"] != "terminal" {
		t.Fatalf("game %s ended with %v", gameID, resp)
	}
	if num(resp["step_count"]) != dr.steps || num(resp["decision_count"]) != dr.groups {
		t.Fatalf("terminal counts %v/%v, driver counted %d/%d", resp["step_count"], resp["decision_count"], dr.steps, dr.groups)
	}
	return resp
}

func newTestServer(t *testing.T, mana string) *Server {
	reg := testutil.CorpusRegistry(t)
	var log bytes.Buffer
	s, err := New(Options{Registry: reg, Mana: mana, Log: &log})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(log.String())
		}
	})
	return s
}

// TestPlaysEveryPoolDeck plays one mirror per benchmark deck with a
// deterministic mixed policy (take the last candidate every third
// decision, else the first) and checks every decision and the terminal.
func TestPlaysEveryPoolDeck(t *testing.T) {
	s := newTestServer(t, "manual")
	for gi, id := range spellbench.BenchmarkPool {
		dr := &driver{t: t, s: s, kinds: map[string]int{}, n: gi * 1000000}
		if gi == 0 {
			hello := dr.send(map[string]any{"request_type": "hello", "protocol_minor": 0})
			if hello["response_type"] != "hello_ok" {
				t.Fatalf("hello: %v", hello)
			}
		}
		k := 0
		term := dr.play(fmt.Sprintf("g-%d", gi), id, strings.Repeat(fmt.Sprintf("%02x", gi+1), 32), func(sd map[string]any) int {
			k++
			n := len(sd["candidates"].([]any))
			if k%3 == 0 {
				return n - 1
			}
			return 0
		})
		if term["classification"] == "halted" {
			t.Errorf("%s: halted: %v", id, term["reason"])
		}
		t.Logf("%s: %v %v %v steps=%v kinds=%v", id, term["outcome"], term["reason"], term["classification"], term["step_count"], dr.kinds)
	}
	st := s.Stats().Snapshot()
	for k, v := range st {
		if strings.HasPrefix(k, "error:") || strings.HasPrefix(k, "halt") || k == "engine_panic" {
			t.Errorf("counter %s = %d", k, v)
		}
	}
}

// TestStepErrors: the spec 9.4 validation order and codes.
func TestStepErrors(t *testing.T) {
	s := newTestServer(t, "manual")
	dr := &driver{t: t, s: s, kinds: map[string]int{}}
	code := func(resp map[string]any) string {
		if resp["response_type"] != "error" {
			return ""
		}
		return resp["error"].(map[string]any)["code"].(string)
	}
	step := func(gid string, exp int64, cid int, echo any) map[string]any {
		return dr.send(map[string]any{"request_type": "step", "game_id": gid, "expected_step": exp,
			"selection": map[string]any{"candidate_id": cid, "semantic_echo": echo}})
	}
	if c := code(step("g", 0, 0, map[string]any{"kind": "pass"})); c != "step_before_reset" {
		t.Fatalf("step before reset: %q", c)
	}
	resp := dr.send(map[string]any{"request_type": "reset", "game_id": "g", "format": Format,
		"seats": []any{
			map[string]any{"seat": "p0", "deck": map[string]any{"deck_id": "sha256:x", "catalog_id": "Burn"}},
			map[string]any{"seat": "p1", "deck": map[string]any{"deck_id": "sha256:x", "catalog_id": "Burn"}},
		},
		"rules": map[string]any{"opponent_decklist": "visible", "mulligan": "none", "starting_player": "host_assigned",
			"starting_seat": "p1", "card_name_domain": map[string]any{"domain_id": "sha256:x", "names": []any{}},
			"extensions": []any{}, "probe": false},
		"game_secret": strings.Repeat("ab", 32), "max_decisions": 100, "max_steps": 100})
	if resp["response_type"] != "decision" {
		t.Fatalf("reset: %v", resp)
	}
	sd := resp["seat_decision"].(map[string]any)
	if obs := sd["observation"].(map[string]any); obs["active_seat"] != "p1" {
		t.Errorf("starting_seat p1 not honoured: active %v", obs["active_seat"])
	}
	first := sd["candidates"].([]any)[0].(map[string]any)["semantic"]
	for _, c := range []struct {
		gid  string
		exp  int64
		cid  int
		echo any
		want string
	}{
		{"other", 0, 0, first, "game_id_mismatch"},
		{"g", 5, 0, first, "expected_step_mismatch"},
		{"g", 0, 999, first, "candidate_id_out_of_range"},
		{"g", 0, 0, map[string]any{"kind": "nope"}, "semantic_echo_mismatch"},
	} {
		if got := code(step(c.gid, c.exp, c.cid, c.echo)); got != c.want {
			t.Errorf("%s: got %q", c.want, got)
		}
	}
	if resp := step("g", 0, 0, first); resp["response_type"] != "decision" && resp["response_type"] != "terminal" {
		t.Fatalf("a valid step after errors: %v", resp)
	}
	if c := code(dr.send(map[string]any{"request_type": "reset", "game_id": "g2", "format": Format, "seats": []any{},
		"rules": map[string]any{}, "game_secret": "", "max_decisions": 1, "max_steps": 1})); c != "game_already_active" && c != "malformed_request" {
		t.Errorf("reset while active: %q", c)
	}
}

// TestObjectIDsPerViewer: the same object has different ids for the two
// seats (spec 5.3), and ids are a function of the secret.
func TestObjectIDsPerViewer(t *testing.T) {
	g := &Game{idKey: []byte("k"), idCache: map[string]string{}, zc: map[state.ObjID]uint32{}}
	a, b := g.objectID(0, 7, ""), g.objectID(1, 7, "")
	if a == b || !strings.HasPrefix(a, "o-") || len(a) != 18 {
		t.Fatalf("ids %s %s", a, b)
	}
	g.zc[7]++
	if c := g.objectID(0, 7, ""); c == a {
		t.Fatal("a zone change must mint a fresh id")
	}
	if l1, l2 := g.objectID(0, 7, "look:1"), g.objectID(0, 7, "look:2"); l1 == l2 {
		t.Fatal("a fresh look must mint a fresh id")
	}
}
